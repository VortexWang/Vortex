package rule

import (
	"context"
	"io"
	"net"
	"sync"
	"time"
	"vortex/engine/vlink"
)

// muxConn adapts an authorized stream to the existing gated TCP/UDP relay.
// Every socket write remains behind the Agent's reservation boundary.
type muxConn struct {
	ctx                         context.Context
	cancel                      context.CancelFunc
	stream                      *vlink.MuxStream
	release                     func()
	once                        sync.Once
	readMu                      sync.Mutex
	pending                     []byte
	mu                          sync.Mutex
	readDeadline, writeDeadline time.Time
}

func newMuxConn(ctx context.Context, s *vlink.MuxStream, release func()) *muxConn {
	ctx, cancel := context.WithCancel(ctx)
	return &muxConn{ctx: ctx, cancel: cancel, stream: s, release: release}
}
func (c *muxConn) Read(buffer []byte) (int, error) {
	c.readMu.Lock()
	defer c.readMu.Unlock()
	if len(buffer) == 0 {
		return 0, nil
	}
	if len(c.pending) > 0 {
		n := copy(buffer, c.pending)
		c.pending = c.pending[n:]
		return n, nil
	}
	c.mu.Lock()
	deadline := c.readDeadline
	c.mu.Unlock()
	ctx, cancel := c.deadline(deadline)
	defer cancel()
	frame, e := c.stream.Receive(ctx)
	if e != nil {
		return 0, e
	}
	defer frame.Release()
	switch frame.Type {
	case vlink.FrameFIN:
		return 0, io.EOF
	case vlink.FrameData, vlink.FrameDgram:
		n := copy(buffer, frame.Payload)
		if frame.Type == vlink.FrameData && n < len(frame.Payload) {
			c.pending = append([]byte(nil), frame.Payload[n:]...)
		}
		if e = c.stream.Consume(ctx, uint32(len(frame.Payload))); e != nil {
			return n, e
		}
		return n, nil
	default:
		return 0, io.ErrUnexpectedEOF
	}
}
func (c *muxConn) Write(b []byte) (int, error) {
	c.mu.Lock()
	deadline := c.writeDeadline
	c.mu.Unlock()
	ctx, cancel := c.deadline(deadline)
	defer cancel()
	if c.stream.Network == vlink.StreamUDP {
		accepted, e := c.stream.WriteDgram(ctx, b)
		if !accepted || e != nil {
			return 0, e
		}
		return len(b), nil
	}
	if e := c.stream.WriteData(ctx, b); e != nil {
		return 0, e
	}
	return len(b), nil
}
func (c *muxConn) Close() error {
	c.once.Do(func() {
		c.cancel()
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		c.stream.Reset(ctx, 0)
		c.release()
	})
	return nil
}
func (c *muxConn) CloseWrite() error   { return c.stream.CloseWrite(c.ctx) }
func (c *muxConn) LocalAddr() net.Addr { return &net.TCPAddr{IP: net.IPv4zero} }
func (c *muxConn) RemoteAddr() net.Addr {
	return &net.TCPAddr{IP: net.IPv4zero, Port: int(c.stream.ID)}
}
func (c *muxConn) SetDeadline(t time.Time) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.readDeadline = t
	c.writeDeadline = t
	return nil
}
func (c *muxConn) SetReadDeadline(t time.Time) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.readDeadline = t
	return nil
}
func (c *muxConn) SetWriteDeadline(t time.Time) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.writeDeadline = t
	return nil
}
func (c *muxConn) deadline(t time.Time) (context.Context, context.CancelFunc) {
	if t.IsZero() {
		return context.WithCancel(c.ctx)
	}
	return context.WithDeadline(c.ctx, t)
}
