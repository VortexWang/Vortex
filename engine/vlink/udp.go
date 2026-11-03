package vlink

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/netip"
	"sync"

	"vortex/engine/accounting"
)

type UDPAdmission struct {
	Policy   OpenPolicy
	Resolver TargetResolver
	Gate     accounting.Gate
	Dial     func(context.Context, netip.AddrPort) (net.Conn, error)
	Meter    TrafficMeter
}

func (a UDPAdmission) Authorize(ctx context.Context, peer NodeID, request OpenRequest) (io.Closer, error) {
	if request.Network != 2 {
		return nil, newOpenRejection(OpenProtocolError)
	}
	decision, err := a.Policy.Authorize(ctx, peer, request)
	if err != nil {
		return nil, err
	}
	endpoint, err := ResolveLocalTarget(ctx, decision.Target, a.Resolver)
	if err != nil {
		return nil, newOpenRejection(OpenTargetUnreachable)
	}
	dial := a.Dial
	if dial == nil {
		dial = func(ctx context.Context, endpoint netip.AddrPort) (net.Conn, error) {
			return DialLocalTarget(ctx, "udp", endpoint)
		}
	}
	conn, err := dial(ctx, endpoint)
	if err != nil {
		return nil, newOpenRejection(OpenTargetUnreachable)
	}
	resource := &UDPResource{Conn: conn, Meter: a.Meter}
	if a.Gate != nil {
		resource.Policy, err = a.Gate.Open(ctx, accounting.Flow{Key: peer.String() + "/" + fmt.Sprintf("%x", request.RuleID)})
		if err != nil {
			_ = resource.Close()
			if errors.Is(err, accounting.ErrDenied) {
				return nil, newOpenRejection(OpenQuotaExhausted)
			}
			return nil, newOpenRejection(OpenUnauthorized)
		}
		if resource.Policy == nil {
			_ = resource.Close()
			return nil, newOpenRejection(OpenUnauthorized)
		}
	}
	return resource, nil
}

type UDPResource struct {
	Conn     net.Conn
	Policy   accounting.Session
	Meter    TrafficMeter
	once     sync.Once
	closeErr error
}

func (r *UDPResource) Close() error {
	r.once.Do(func() {
		if r.Conn != nil {
			r.closeErr = errors.Join(r.closeErr, r.Conn.Close())
		}
		if r.Policy != nil {
			r.closeErr = errors.Join(r.closeErr, r.Policy.Close())
		}
	})
	return r.closeErr
}

// Run owns one authorized UDP stream. Datagram windows are non-blocking: an
// over-window or oversize packet is dropped and reported without replay.
func (r *UDPResource) Run(ctx context.Context, stream *MuxStream, onDrop func()) error {
	if r == nil || r.Conn == nil || stream == nil || stream.Network != StreamUDP {
		return ErrProtocol
	}
	stop := context.AfterFunc(ctx, func() { _ = r.Close() })
	defer stop()
	results := make(chan error, 2)
	go func() {
		for {
			frame, err := stream.Receive(ctx)
			if err != nil {
				if errors.Is(err, ErrStreamState) {
					results <- nil
				} else {
					results <- err
				}
				return
			}
			if frame.Type == FrameRST {
				results <- nil
				return
			}
			if frame.Type != FrameDgram {
				results <- ErrProtocol
				return
			}
			batch, err := r.reserve(ctx, accounting.LeftToRight, len(frame.Payload))
			if err != nil {
				results <- err
				return
			}
			n, writeErr := r.Conn.Write(frame.Payload)
			if writeErr != nil || n != len(frame.Payload) {
				if batch != nil {
					_ = batch.MarkUnknown(ctx)
				}
				if writeErr == nil {
					writeErr = io.ErrShortWrite
				}
				results <- writeErr
				return
			}
			if batch != nil {
				if err := batch.Commit(ctx, int64(n)); err != nil {
					results <- err
					return
				}
			}
			if r.Meter != nil {
				if err := r.Meter(accounting.LeftToRight, int64(n)); err != nil {
					results <- err
					return
				}
			}
			if err := stream.Consume(ctx, uint32(n)); err != nil {
				results <- err
				return
			}
		}
	}()
	go func() {
		buffer := make([]byte, MaxPayloadSize+1)
		for {
			n, err := r.Conn.Read(buffer)
			if err != nil {
				results <- err
				return
			}
			if n == 0 || n > MaxPayloadSize {
				if onDrop != nil {
					onDrop()
				}
				continue
			}
			batch, reserveErr := r.reserve(ctx, accounting.RightToLeft, n)
			if reserveErr != nil {
				results <- reserveErr
				return
			}
			accepted, sendErr := stream.WriteDgram(ctx, buffer[:n])
			if sendErr != nil {
				if batch != nil {
					_ = batch.MarkUnknown(ctx)
				}
				results <- sendErr
				return
			}
			if !accepted {
				if batch != nil {
					if err := batch.Return(ctx); err != nil {
						results <- err
						return
					}
				}
				if onDrop != nil {
					onDrop()
				}
				continue
			}
			if batch != nil {
				if err := batch.Commit(ctx, int64(n)); err != nil {
					results <- err
					return
				}
			}
			if r.Meter != nil {
				if err := r.Meter(accounting.RightToLeft, int64(n)); err != nil {
					results <- err
					return
				}
			}
		}
	}()
	first := <-results
	_ = r.Close()
	second := <-results
	if first != nil {
		return first
	}
	return second
}

func (r *UDPResource) reserve(ctx context.Context, direction accounting.Direction, bytes int) (*accounting.Batch, error) {
	if r.Policy == nil {
		return nil, nil
	}
	batch, err := r.Policy.Reserve(ctx, direction, int64(bytes))
	if err != nil {
		return nil, err
	}
	if batch == nil || batch.Reserved() < int64(bytes) {
		if batch != nil {
			_ = batch.MarkUnknown(ctx)
		}
		return nil, errors.New("vlink: UDP gate returned an undersized reservation")
	}
	return batch, nil
}
