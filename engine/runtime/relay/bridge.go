package relay

import (
	"context"
	"errors"
	"io"
	"net"
	"sync"

	"vortex/engine/vlink"
)

type TargetDialer func(context.Context, vlink.LocalTarget) (net.Conn, error)

type Service struct {
	LocalNode vlink.NodeID
	Policy    vlink.OpenPolicy
	Next      *vlink.MuxLink
	Dial      TargetDialer
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
		if request.Network == 1 && s.Next != nil {
			next := request
			next.HopIndex++
			next.Visited = append(append([]vlink.NodeID(nil), request.Visited...), s.LocalNode)
			downstream, err := s.Next.Open(ctx, next)
			if err != nil {
				return nil, err
			}
			return &streamResource{stream: downstream}, nil
		}
		if request.Network == 2 {
			return nil, errors.New("relay: UDP requires the UDP admission path")
		}
		if s.Dial == nil {
			return nil, errors.New("relay: target dialer is not configured")
		}
		conn, err := s.Dial(ctx, decision.Target)
		if err != nil {
			return nil, err
		}
		return &connResource{conn: conn}, nil
	})
	if err != nil {
		return
	}
	resource := stream.TakeResource()
	switch typed := resource.(type) {
	case *streamResource:
		_ = BridgeStreams(ctx, stream, typed.stream)
	case *connResource:
		_ = BridgeConn(ctx, stream, typed.conn)
	default:
		stream.CloseWrite(ctx)
	}
}

func BridgeStreams(ctx context.Context, left, right *vlink.MuxStream) error {
	if left == nil || right == nil {
		return errors.New("relay: both streams are required")
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	errs := make(chan error, 2)
	go func() { errs <- forwardStream(ctx, left, right) }()
	go func() { errs <- forwardStream(ctx, right, left) }()
	return <-errs
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
		case vlink.FrameFIN:
			return destination.CloseWrite(ctx)
		case vlink.FrameRST:
			return errors.New("relay: stream reset")
		default:
			return errors.New("relay: unexpected stream frame")
		}
	}
}

func BridgeConn(ctx context.Context, stream *vlink.MuxStream, conn net.Conn) error {
	if stream == nil || conn == nil {
		return errors.New("relay: stream and connection are required")
	}
	defer conn.Close()
	var wg sync.WaitGroup
	errCh := make(chan error, 2)
	wg.Add(2)
	go func() {
		defer wg.Done()
		for {
			frame, err := stream.Receive(ctx)
			if err != nil {
				errCh <- err
				return
			}
			switch frame.Type {
			case vlink.FrameData:
				if _, err := conn.Write(frame.Payload); err != nil {
					errCh <- err
					return
				}
				if err := stream.Consume(ctx, uint32(len(frame.Payload))); err != nil {
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
		buffer := make([]byte, vlink.MaxTLSRecordPayload)
		for {
			n, err := conn.Read(buffer)
			if n > 0 {
				if writeErr := stream.WriteData(ctx, buffer[:n]); writeErr != nil {
					errCh <- writeErr
					return
				}
			}
			if err != nil {
				if errors.Is(err, io.EOF) {
					errCh <- stream.CloseWrite(ctx)
				} else {
					errCh <- err
				}
				return
			}
		}
	}()
	first := <-errCh
	wg.Wait()
	return first
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
