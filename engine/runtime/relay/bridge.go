package relay

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net"
	"sync"
	"time"

	engineaccounting "vortex/engine/accounting"
	"vortex/engine/vlink"
)

type TargetDialer func(context.Context, uint8, vlink.LocalTarget) (net.Conn, error)

type Service struct {
	LocalNode                   vlink.NodeID
	Policy                      vlink.OpenPolicy
	Next                        func() *vlink.MuxLink
	AcquireNext                 func(context.Context, vlink.StreamNetwork) (*vlink.MuxLink, func(), error)
	AcquireRoute                func(context.Context, vlink.OpenRequest) (*vlink.MuxLink, func(), context.Context, error)
	Dial                        TargetDialer
	Gate                        engineaccounting.Gate
	ObserveFirstTargetByte      func(time.Duration)
	ObserveRouteFirstTargetByte func(uint64, time.Duration)
	mu                          sync.Mutex
	closing                     bool
	draining                    bool
	active                      int
	idle                        chan struct{}
	workers                     sync.WaitGroup
}

func (s *Service) ServeLink(ctx context.Context, link *vlink.MuxLink) error {
	for {
		stream, err := link.Accept(ctx)
		if err != nil {
			return err
		}
		s.mu.Lock()
		if s.closing {
			s.mu.Unlock()
			return context.Canceled
		}
		if s.draining {
			s.mu.Unlock()
			_ = link.AuthorizeRemote(ctx, stream.ID, func(context.Context, vlink.NodeID, vlink.OpenRequest) (io.Closer, error) {
				return nil, errors.New("relay: draining")
			})
			continue
		}
		if s.active == 0 {
			s.idle = make(chan struct{})
		}
		s.active++
		s.workers.Add(1)
		s.mu.Unlock()
		go func() {
			defer s.workers.Done()
			defer func() {
				s.mu.Lock()
				s.active--
				if s.active == 0 {
					close(s.idle)
				}
				s.mu.Unlock()
			}()
			s.serveStream(ctx, link, stream)
		}()
	}
}

// Shutdown waits for accepted workers after their context and links are closed.
// The admission lock prevents Add racing with a zero-counter Wait.
func (s *Service) Shutdown() {
	s.mu.Lock()
	s.closing = true
	s.mu.Unlock()
	s.workers.Wait()
}

func (s *Service) serveStream(ctx context.Context, link *vlink.MuxLink, stream *vlink.MuxStream) {
	openedAt := time.Now()
	var requestedGeneration uint64
	err := link.AuthorizeRemote(ctx, stream.ID, func(ctx context.Context, peer vlink.NodeID, request vlink.OpenRequest) (io.Closer, error) {
		requestedGeneration = request.PathGeneration
		decision, err := s.Policy.Authorize(ctx, peer, request)
		if err != nil {
			return nil, err
		}
		if s.Next != nil || s.AcquireNext != nil || s.AcquireRoute != nil {
			var nextLink *vlink.MuxLink
			release := func() {}
			var udpContext context.Context
			if s.AcquireRoute != nil {
				var err error
				nextLink, release, udpContext, err = s.AcquireRoute(ctx, request)
				if err != nil {
					return nil, err
				}
			} else if s.AcquireNext != nil {
				var err error
				nextLink, release, err = s.AcquireNext(ctx, stream.Network)
				if err != nil {
					return nil, err
				}
			} else {
				nextLink = s.Next()
			}
			transferred := false
			defer func() {
				if !transferred {
					release()
				}
			}()
			if nextLink == nil {
				return nil, errors.New("relay: next hop is unavailable")
			}
			next := request
			next.HopIndex++
			next.Visited = append(append([]vlink.NodeID(nil), request.Visited...), s.LocalNode)
			downstream, err := nextLink.Open(ctx, next)
			if err != nil {
				return nil, err
			}
			if err := downstream.WaitOpen(ctx); err != nil {
				_ = downstream.Reset(ctx, 8)
				return nil, err
			}
			transferred = true
			return &streamResource{stream: downstream, release: release, udp: udpContext}, nil
		}
		if s.Dial == nil {
			return nil, errors.New("relay: target dialer is not configured")
		}
		if s.Gate == nil {
			return nil, engineaccounting.ErrDenied
		}
		session, err := s.Gate.Open(ctx, engineaccounting.Flow{Key: stream.FlowKey()})
		if err != nil {
			return nil, err
		}
		conn, err := s.Dial(ctx, request.Network, decision.Target)
		if err != nil {
			_ = session.Close()
			slog.Error("relay target dial failed", "target", decision.Target.Host, "port", decision.Target.Port, "err", err)
			return nil, err
		}
		slog.Info("relay target connected", "target", decision.Target.Host, "port", decision.Target.Port)
		return &connResource{conn: conn, session: session}, nil
	})
	if err != nil {
		slog.Error("relay OPEN handling failed", "stream_id", stream.ID, "err", err)
		return
	}
	slog.Info("relay OPEN authorized", "stream_id", stream.ID, "network", stream.Network)
	resource := stream.TakeResource()
	switch typed := resource.(type) {
	case *streamResource:
		defer func() {
			if s.AcquireNext != nil || s.AcquireRoute != nil {
				resetCtx, cancel := context.WithTimeout(context.Background(), time.Second)
				defer cancel()
				_ = typed.stream.Reset(resetCtx, 0)
			}
			typed.releaseLease()
		}()
		var observe func()
		if stream.Network == vlink.StreamTCP {
			if s.ObserveRouteFirstTargetByte != nil {
				observe = func() { s.ObserveRouteFirstTargetByte(requestedGeneration, time.Since(openedAt)) }
			} else if s.ObserveFirstTargetByte != nil {
				observe = func() { s.ObserveFirstTargetByte(time.Since(openedAt)) }
			}
		}
		bridgeCtx := ctx
		if stream.Network == vlink.StreamUDP && typed.udp != nil {
			bridgeCtx = typed.udp
		}
		if err := BridgeStreamsObserved(bridgeCtx, stream, typed.stream, observe); err != nil && ctx.Err() == nil {
			slog.Error("relay stream bridge failed", "stream_id", stream.ID, "err", err)
		}
	case *connResource:
		if err := bridgeConnWithSession(ctx, stream, typed.conn, typed.session, nil); err != nil && ctx.Err() == nil {
			slog.Error("relay target bridge failed", "stream_id", stream.ID, "err", err)
		}
	default:
		stream.CloseWrite(ctx)
	}
}

func BridgeStreams(ctx context.Context, left, right *vlink.MuxStream) error {
	return BridgeStreamsObserved(ctx, left, right, nil)
}

func BridgeStreamsObserved(ctx context.Context, left, right *vlink.MuxStream, onFirstTargetByte func()) error {
	if left == nil || right == nil {
		return errors.New("relay: both streams are required")
	}
	bridgeCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	errs := make(chan error, 2)
	go func() { errs <- forwardStream(bridgeCtx, left, right, nil) }()
	go func() { errs <- forwardStream(bridgeCtx, right, left, onFirstTargetByte) }()
	first := <-errs
	if first != nil {
		cancel()
		resetCtx, resetCancel := context.WithTimeout(context.Background(), time.Second)
		defer resetCancel()
		_ = left.Reset(resetCtx, 8)
		_ = right.Reset(resetCtx, 8)
	}
	second := <-errs
	if first != nil && !errors.Is(first, vlink.ErrStreamState) {
		return first
	}
	return second
}

func forwardStream(ctx context.Context, source, destination *vlink.MuxStream, onFirstTargetByte func()) error {
	if source.Network == vlink.StreamTCP {
		return forwardTCP(ctx, source, destination, onFirstTargetByte)
	}
	var owned vlink.Frame
	defer func() { owned.Release() }()
	firstTargetByte := true
	for {
		owned.Release()
		frame, err := source.Receive(ctx)
		owned = frame
		if err != nil {
			return err
		}
		switch frame.Type {
		case vlink.FrameData:
			if firstTargetByte {
				firstTargetByte = false
				if onFirstTargetByte != nil {
					onFirstTargetByte()
				}
			}
			if err := destination.WriteData(ctx, frame.Payload); err != nil {
				return err
			}
			if err := source.Consume(ctx, uint32(len(frame.Payload))); err != nil {
				return err
			}
		case vlink.FrameDgram:
			if firstTargetByte {
				firstTargetByte = false
				if onFirstTargetByte != nil {
					onFirstTargetByte()
				}
			}
			_, err := destination.WriteDgram(ctx, frame.Payload)
			if err != nil {
				return err
			}
			if err := source.Consume(ctx, uint32(len(frame.Payload))); err != nil {
				return err
			}

		case vlink.FrameFIN:
			if err := destination.CloseWrite(ctx); err != nil {
				return err
			}
			return nil
		case vlink.FrameRST:
			return errors.New("relay: stream reset")
		default:
			return errors.New("relay: unexpected stream frame")
		}
	}
}

func forwardTCP(ctx context.Context, source, destination *vlink.MuxStream, onFirstTargetByte func()) error {
	var frames [4]vlink.Frame
	firstTargetByte := true
	for {
		n, err := source.ReceiveBatch(ctx, frames[:])
		if err != nil {
			return err
		}
		for _, frame := range frames[:n] {
			if frame.Type == vlink.FrameData && firstTargetByte {
				firstTargetByte = false
				if onFirstTargetByte != nil {
					onFirstTargetByte()
				}
				break
			}
		}
		finished, err := forwardTCPBatch(ctx, source, destination, frames[:n])
		for i := 0; i < n; i++ {
			frames[i] = vlink.Frame{}
		}
		if err != nil || finished {
			return err
		}
	}
}

func forwardTCPBatch(ctx context.Context, source, destination *vlink.MuxStream, frames []vlink.Frame) (bool, error) {
	defer func() {
		for _, frame := range frames {
			frame.Release()
		}
	}()
	dataCount := 0
	var size uint32
	for _, frame := range frames {
		if frame.Type != vlink.FrameData {
			break
		}
		dataCount++
		size += uint32(len(frame.Payload))
	}
	if dataCount > 0 {
		if err := destination.WriteDataFrames(ctx, frames[:dataCount]); err != nil {
			return false, err
		}
		if err := source.Consume(ctx, size); err != nil {
			return false, err
		}
	}
	for _, frame := range frames[dataCount:] {
		switch frame.Type {
		case vlink.FrameFIN:
			return true, destination.CloseWrite(ctx)
		case vlink.FrameRST:
			return true, errors.New("relay: stream reset")
		default:
			return true, errors.New("relay: unexpected stream frame")
		}
	}
	return false, nil
}

func BridgeConn(ctx context.Context, stream *vlink.MuxStream, conn net.Conn) error {
	return BridgeConnWithGate(ctx, stream, conn, nil)
}

func BridgeConnWithGate(ctx context.Context, stream *vlink.MuxStream, conn net.Conn, gate engineaccounting.Gate) error {
	return BridgeConnWithGateObserver(ctx, stream, conn, gate, nil)
}

func BridgeConnWithGateObserver(ctx context.Context, stream *vlink.MuxStream, conn net.Conn, gate engineaccounting.Gate, onFirstTargetByte func()) error {
	if stream == nil || conn == nil {
		return errors.New("relay: stream and connection are required")
	}
	defer conn.Close()
	if gate == nil {
		return errors.New("relay: source authorization gate is required")
	}
	var session engineaccounting.Session
	var err error
	session, err = gate.Open(ctx, engineaccounting.Flow{Key: stream.FlowKey()})
	if err != nil {
		return err
	}
	return bridgeConnWithSession(ctx, stream, conn, session, onFirstTargetByte)
}

func bridgeConnWithSession(ctx context.Context, stream *vlink.MuxStream, conn net.Conn, session engineaccounting.Session, onFirstTargetByte func()) error {
	if session == nil {
		conn.Close()
		return engineaccounting.ErrDenied
	}
	defer session.Close()
	defer conn.Close()
	bridgeCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	if stream.Network == vlink.StreamUDP {
		if udp, ok := conn.(*net.UDPConn); ok {
			if err := udp.SetReadBuffer(32 * 1024 * 1024); err != nil {
				return err
			}
			if err := udp.SetWriteBuffer(32 * 1024 * 1024); err != nil {
				return err
			}
		}
		if err := conn.SetReadDeadline(time.Now().Add(60 * time.Second)); err != nil {
			return err
		}
	}
	var wg sync.WaitGroup
	errCh := make(chan error, 2)
	wg.Add(2)
	go func() {
		defer wg.Done()
		if stream.Network == vlink.StreamTCP {
			errCh <- bridgeTCPToConn(bridgeCtx, stream, conn, session, onFirstTargetByte)
			return
		}
		var owned vlink.Frame
		firstTargetByte := true
		defer func() { owned.Release() }()
		for {
			owned.Release()
			frame, err := stream.Receive(bridgeCtx)
			owned = frame
			if err != nil {
				errCh <- err
				return
			}
			switch frame.Type {
			case vlink.FrameData, vlink.FrameDgram:
				if firstTargetByte {
					firstTargetByte = false
					if onFirstTargetByte != nil {
						onFirstTargetByte()
					}
				}
				batch, err := reserve(bridgeCtx, session, engineaccounting.LeftToRight, len(frame.Payload))
				if err != nil {
					errCh <- err
					return
				}
				n, err := conn.Write(frame.Payload)
				if err != nil || n != len(frame.Payload) {
					if batch != nil {
						_ = batch.MarkUnknown(bridgeCtx)
					}
					if err == nil {
						err = io.ErrShortWrite
					}
					errCh <- err
					return
				}
				if batch != nil {
					if err := batch.Commit(bridgeCtx, int64(n)); err != nil {
						errCh <- err
						return
					}
				}
				if stream.Network == vlink.StreamUDP {
					_ = conn.SetReadDeadline(time.Now().Add(60 * time.Second))
				}
				if err := stream.Consume(bridgeCtx, uint32(len(frame.Payload))); err != nil {
					errCh <- err
					return
				}
			case vlink.FrameFIN:
				if tcp, ok := conn.(*net.TCPConn); ok {
					_ = tcp.CloseWrite()
				}
				return
			case vlink.FrameRST:
				errCh <- errors.New("relay: stream reset")
				return
			default:
				errCh <- errors.New("relay: unexpected stream frame")
				return
			}
		}
	}()
	go func() {
		defer wg.Done()
		bufferSize := 4 * vlink.MaxTLSRecordPayload
		if stream.Network == vlink.StreamUDP {
			bufferSize = vlink.MaxPayloadSize
		}
		buffer := make([]byte, bufferSize)
		for {
			n, err := conn.Read(buffer)
			if n > 0 {
				batch, reserveErr := reserve(bridgeCtx, session, engineaccounting.RightToLeft, n)
				if reserveErr != nil {
					errCh <- reserveErr
					return
				}
				writeErr := error(nil)
				if stream.Network == vlink.StreamUDP {
					var accepted bool
					accepted, writeErr = stream.WriteDgram(bridgeCtx, buffer[:n])
					if writeErr == nil && !accepted {
						if batch != nil {
							_ = batch.Return(bridgeCtx)
						}
						continue
					}
				} else {
					writeErr = stream.WriteData(bridgeCtx, buffer[:n])
				}
				if writeErr != nil {
					if batch != nil {
						_ = batch.MarkUnknown(bridgeCtx)
					}
					errCh <- writeErr
					return
				}
				if batch != nil {
					if commitErr := batch.Commit(bridgeCtx, int64(n)); commitErr != nil {
						errCh <- commitErr
						return
					}
				}
			}
			if stream.Network == vlink.StreamUDP && err == nil {
				_ = conn.SetReadDeadline(time.Now().Add(60 * time.Second))
			}
			if err != nil {
				if errors.Is(err, io.EOF) {
					errCh <- stream.CloseWrite(bridgeCtx)
				} else {
					errCh <- err
				}
				return
			}
		}
	}()
	first := <-errCh
	if first != nil {
		cancel()
		_ = conn.Close()
		_ = stream.Reset(context.Background(), 8)
	}
	wg.Wait()
	return first
}

// bridgeTCPToConn settles a batch of already-framed DATA before releasing the
// stream window. This keeps accounting semantics byte exact while avoiding a
// lock and event commit for every 16 KiB frame at the destination.
func bridgeTCPToConn(ctx context.Context, stream *vlink.MuxStream, conn net.Conn, session engineaccounting.Session, onFirstTargetByte func()) error {
	var frames [4]vlink.Frame
	first := true
	for {
		n, err := stream.ReceiveBatch(ctx, frames[:])
		if err != nil {
			return err
		}
		dataCount := 0
		var total int
		for _, frame := range frames[:n] {
			if frame.Type != vlink.FrameData {
				break
			}
			dataCount++
			total += len(frame.Payload)
		}
		if dataCount > 0 {
			if first && onFirstTargetByte != nil {
				onFirstTargetByte()
			}
			first = false
			batch, reserveErr := reserve(ctx, session, engineaccounting.LeftToRight, total)
			if reserveErr != nil {
				releaseFrames(frames[:n])
				return reserveErr
			}
			written := 0
			var writeErr error
			for _, frame := range frames[:dataCount] {
				var nw int
				nw, writeErr = conn.Write(frame.Payload)
				written += nw
				if writeErr != nil || nw != len(frame.Payload) {
					if writeErr == nil {
						writeErr = io.ErrShortWrite
					}
					break
				}
			}
			if writeErr != nil {
				if batch != nil {
					_ = batch.MarkUnknown(ctx)
				}
				releaseFrames(frames[:n])
				return writeErr
			}
			if batch != nil {
				if err := batch.Commit(ctx, int64(written)); err != nil {
					releaseFrames(frames[:n])
					return err
				}
			}
			if err := stream.Consume(ctx, uint32(written)); err != nil {
				releaseFrames(frames[:n])
				return err
			}
		}
		for _, frame := range frames[dataCount:n] {
			switch frame.Type {
			case vlink.FrameFIN:
				releaseFrames(frames[:n])
				if tcp, ok := conn.(*net.TCPConn); ok {
					return tcp.CloseWrite()
				}
				return errors.New("relay: destination does not support half-close")
			case vlink.FrameRST:
				releaseFrames(frames[:n])
				return errors.New("relay: stream reset")
			default:
				releaseFrames(frames[:n])
				return errors.New("relay: unexpected stream frame")
			}
		}
		releaseFrames(frames[:n])
	}
}

func releaseFrames(frames []vlink.Frame) {
	for i := range frames {
		frames[i].Release()
		frames[i] = vlink.Frame{}
	}
}

func reserve(ctx context.Context, session engineaccounting.Session, direction engineaccounting.Direction, size int) (*engineaccounting.Batch, error) {
	if session == nil {
		return nil, nil
	}
	return session.Reserve(ctx, direction, int64(size))
}

type streamResource struct {
	udp     context.Context
	stream  *vlink.MuxStream
	release func()
	once    sync.Once
}

func (r *streamResource) releaseLease() {
	r.once.Do(func() {
		if r.release != nil {
			r.release()
		}
	})
}

func (r *streamResource) Close() error {
	if r != nil {
		defer r.releaseLease()
	}
	if r == nil || r.stream == nil {
		return nil
	}
	return r.stream.CloseWrite(context.Background())
}

type connResource struct {
	conn    net.Conn
	session engineaccounting.Session
}

func (r *connResource) Close() error {
	if r == nil || r.conn == nil {
		return nil
	}
	err := r.conn.Close()
	if r.session != nil {
		if closeErr := r.session.Close(); err == nil {
			err = closeErr
		}
	}
	return err
}
