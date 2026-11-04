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
	LocalNode vlink.NodeID
	Policy    vlink.OpenPolicy
	Next      func() *vlink.MuxLink
	Dial      TargetDialer
	Gate      engineaccounting.Gate
}

func (s Service) ServeLink(ctx context.Context, link *vlink.MuxLink) error {
	for {
		stream, err := link.Accept(ctx)
		if err != nil {
			return err
		}
		go s.serveStream(ctx, link, stream)
	}
}

func (s Service) serveStream(ctx context.Context, link *vlink.MuxLink, stream *vlink.MuxStream) {
	err := link.AuthorizeRemote(ctx, stream.ID, func(ctx context.Context, peer vlink.NodeID, request vlink.OpenRequest) (io.Closer, error) {
		decision, err := s.Policy.Authorize(ctx, peer, request)
		if err != nil {
			return nil, err
		}
		if s.Next != nil {
			nextLink := s.Next()
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
				return nil, err
			}
			return &streamResource{stream: downstream}, nil
		}
		if s.Dial == nil {
			return nil, errors.New("relay: target dialer is not configured")
		}
		conn, err := s.Dial(ctx, request.Network, decision.Target)
		if err != nil {
			slog.Error("relay target dial failed", "target", decision.Target.Host, "port", decision.Target.Port, "err", err)
			return nil, err
		}
		slog.Info("relay target connected", "target", decision.Target.Host, "port", decision.Target.Port)
		return &connResource{conn: conn}, nil
	})
	if err != nil {
		slog.Error("relay OPEN handling failed", "stream_id", stream.ID, "err", err)
		return
	}
	slog.Info("relay OPEN authorized", "stream_id", stream.ID, "network", stream.Network)
	resource := stream.TakeResource()
	switch typed := resource.(type) {
	case *streamResource:
		if err := BridgeStreams(ctx, stream, typed.stream); err != nil && ctx.Err() == nil {
			slog.Error("relay stream bridge failed", "stream_id", stream.ID, "err", err)
		}
	case *connResource:
		if err := BridgeConnWithGate(ctx, stream, typed.conn, s.Gate); err != nil && ctx.Err() == nil {
			slog.Error("relay target bridge failed", "stream_id", stream.ID, "err", err)
		}
	default:
		stream.CloseWrite(ctx)
	}
}

func BridgeStreams(ctx context.Context, left, right *vlink.MuxStream) error {
	if left == nil || right == nil {
		return errors.New("relay: both streams are required")
	}
	errs := make(chan error, 2)
	go func() { errs <- forwardStream(ctx, left, right) }()
	go func() { errs <- forwardStream(ctx, right, left) }()
	first := <-errs
	second := <-errs
	if first != nil && !errors.Is(first, vlink.ErrStreamState) {
		return first
	}
	return second
}

func forwardStream(ctx context.Context, source, destination *vlink.MuxStream) error {
	for {
		frame, err := source.Receive(ctx)
		if err != nil {
			return err
		}
		switch frame.Type {
		case vlink.FrameData:
			if err := destination.WriteData(ctx, frame.Payload); err != nil {
				return err
			}
			if err := source.Consume(ctx, uint32(len(frame.Payload))); err != nil {
				return err
			}
		case vlink.FrameDgram:
			accepted, err := destination.WriteDgram(ctx, frame.Payload)
			if err != nil {
				return err
			}
			if !accepted {
				slog.Warn("vlink UDP forwarding dropped datagram", "reason", "downstream_window_or_queue_full")
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

func BridgeConn(ctx context.Context, stream *vlink.MuxStream, conn net.Conn) error {
	return BridgeConnWithGate(ctx, stream, conn, nil)
}

func BridgeConnWithGate(ctx context.Context, stream *vlink.MuxStream, conn net.Conn, gate engineaccounting.Gate) error {
	if stream == nil || conn == nil {
		return errors.New("relay: stream and connection are required")
	}
	defer conn.Close()
	var session engineaccounting.Session
	if gate != nil {
		var err error
		session, err = gate.Open(ctx, engineaccounting.Flow{Key: "qualification"})
		if err != nil {
			return err
		}
		defer session.Close()
	}
	bridgeCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	if stream.Network == vlink.StreamUDP {
		if err := conn.SetReadDeadline(time.Now().Add(60 * time.Second)); err != nil {
			return err
		}
	}
	var wg sync.WaitGroup
	errCh := make(chan error, 2)
	wg.Add(2)
	go func() {
		defer wg.Done()
		for {
			frame, err := stream.Receive(bridgeCtx)
			if err != nil {
				errCh <- err
				return
			}
			switch frame.Type {
			case vlink.FrameData, vlink.FrameDgram:
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
		buffer := make([]byte, vlink.MaxPayloadSize)
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
						slog.Warn("udp target response dropped", "reason", "window_or_queue_full")
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
	}
	wg.Wait()
	return first
}

func reserve(ctx context.Context, session engineaccounting.Session, direction engineaccounting.Direction, size int) (*engineaccounting.Batch, error) {
	if session == nil {
		return nil, nil
	}
	return session.Reserve(ctx, direction, int64(size))
}

type streamResource struct{ stream *vlink.MuxStream }

func (r *streamResource) Close() error {
	if r == nil || r.stream == nil {
		return nil
	}
	return r.stream.CloseWrite(context.Background())
}

type connResource struct{ conn net.Conn }

func (r *connResource) Close() error {
	if r == nil || r.conn == nil {
		return nil
	}
	return r.conn.Close()
}
