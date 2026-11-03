package vlink

import (
	"context"
	"crypto/rand"
	"crypto/tls"
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"sync"
	"time"
)

type LinkAction uint8

const (
	LinkNoAction LinkAction = iota
	LinkSendPing
	LinkPingExpired
	LinkUnavailable
)

type Liveness struct {
	mu                     sync.Mutex
	lastReceived, lastPing time.Time
	pingOutstanding        bool
	pingSent               time.Time
}

func NewLiveness(now time.Time) *Liveness { return &Liveness{lastReceived: now} }

func (l *Liveness) Received(now time.Time) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if now.After(l.lastReceived) {
		l.lastReceived = now
	}
	l.pingOutstanding = false
}

func (l *Liveness) Poll(now time.Time) LinkAction {
	l.mu.Lock()
	defer l.mu.Unlock()
	if now.Before(l.lastReceived) {
		return LinkNoAction
	}
	if now.Sub(l.lastReceived) >= 12*time.Second {
		return LinkUnavailable
	}
	if l.pingOutstanding {
		if now.Sub(l.pingSent) >= 3*time.Second {
			l.pingOutstanding = false
			l.lastPing = now
			return LinkPingExpired
		}
		return LinkNoAction
	}
	if now.Sub(l.lastReceived) >= 3*time.Second && (l.lastPing.IsZero() || now.Sub(l.lastPing) >= 3*time.Second) {
		l.pingOutstanding = true
		l.pingSent = now
		return LinkSendPing
	}
	return LinkNoAction
}

type Dispatcher struct {
	mu         sync.Mutex
	streams    *StreamTable
	liveness   *Liveness
	now        func() time.Time
	closed     chan struct{}
	closeErr   error
	closedOnce sync.Once
}

func NewDispatcher(localClient bool, maxPendingOpenBytes int) *Dispatcher {
	return NewDispatcherWithClock(localClient, maxPendingOpenBytes, time.Now)
}

func NewDispatcherWithClock(localClient bool, maxPendingOpenBytes int, now func() time.Time) *Dispatcher {
	if now == nil {
		now = time.Now
	}
	return &Dispatcher{streams: NewStreamTable(localClient, maxPendingOpenBytes), liveness: NewLiveness(now()), now: now, closed: make(chan struct{})}
}

func (d *Dispatcher) Open(network StreamNetwork) (uint32, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.closeErr != nil {
		return 0, d.closeErr
	}
	return d.streams.Open(network)
}

func (d *Dispatcher) Apply(frame Frame) (Frame, error) {
	d.mu.Lock()
	closed := d.closeErr
	d.mu.Unlock()
	if closed != nil {
		return Frame{}, closed
	}
	accepted, err := d.streams.Apply(frame)
	if err != nil && errors.Is(err, ErrProtocol) {
		d.Close(err)
	} else if err == nil {
		d.liveness.Received(d.now())
	}
	return accepted, err
}

func (d *Dispatcher) PollLiveness() LinkAction { return d.liveness.Poll(d.now()) }

func (d *Dispatcher) IdentityFailure(err error) {
	if err == nil {
		err = ErrProtocol
	}
	d.Close(err)
}

func (d *Dispatcher) Close(err error) {
	if err == nil {
		err = ErrProtocol
	}
	d.closedOnce.Do(func() { d.mu.Lock(); d.closeErr = err; d.streams.CloseAll(); close(d.closed); d.mu.Unlock() })
}

func (d *Dispatcher) Closed() <-chan struct{} { return d.closed }
func (d *Dispatcher) Err() error              { d.mu.Lock(); defer d.mu.Unlock(); return d.closeErr }
func (d *Dispatcher) StreamCount() int        { return d.streams.Count() }

type MuxConfig struct {
	LocalClient                              bool
	MaxPendingOpenBytes                      int
	StreamWindow, AggregateWindow, MaxWindow uint32
	StreamQueueFrames                        int
	Now                                      func() time.Time
}

type MuxLink struct {
	conn        net.Conn
	dispatch    *Dispatcher
	windows     *WindowManager
	config      MuxConfig
	writeMu     sync.Mutex
	mu          sync.Mutex
	streams     map[uint32]*MuxStream
	pingNonce   [8]byte
	pingPending bool
	incoming    chan *MuxStream
	done        chan struct{}
	closeOnce   sync.Once
}

type MuxStream struct {
	link      *MuxLink
	ID        uint32
	Network   StreamNetwork
	receive   chan Frame
	done      chan struct{}
	closeOnce sync.Once
}

func NewMuxLink(conn net.Conn, config MuxConfig) (*MuxLink, error) {
	if conn == nil {
		return nil, errors.New("vlink: mux connection is required")
	}
	if config.StreamWindow == 0 {
		config.StreamWindow = 64 * 1024
	}
	if config.AggregateWindow == 0 {
		config.AggregateWindow = 256 * 1024
	}
	if config.MaxWindow == 0 {
		config.MaxWindow = 16 * 1024 * 1024
	}
	if config.StreamQueueFrames < 1 {
		config.StreamQueueFrames = 32
	}
	m := &MuxLink{conn: conn, dispatch: NewDispatcherWithClock(config.LocalClient, config.MaxPendingOpenBytes, config.Now), windows: NewWindowManager(config.AggregateWindow, config.MaxWindow), config: config, streams: make(map[uint32]*MuxStream), incoming: make(chan *MuxStream, 64), done: make(chan struct{})}
	go m.readLoop()
	return m, nil
}

func (m *MuxLink) Open(ctx context.Context, request OpenRequest) (*MuxStream, error) {
	network := StreamNetwork(request.Network)
	id, err := m.dispatch.Open(network)
	if err != nil {
		return nil, err
	}
	if err = m.windows.AddStream(id, m.config.StreamWindow); err != nil {
		m.removeStream(id)
		return nil, err
	}
	stream := m.newStream(id, network)
	if stream == nil {
		m.removeStream(id)
		return nil, m.Err()
	}
	payload, err := MarshalOpen(request)
	if err != nil {
		m.removeStream(id)
		return nil, err
	}
	if err = m.write(ctx, Frame{Type: FrameOpen, StreamID: id, Payload: payload}); err != nil {
		m.removeStream(id)
		m.Close(err)
		return nil, err
	}
	return stream, nil
}

func (m *MuxLink) Accept(ctx context.Context) (*MuxStream, error) {
	select {
	case stream := <-m.incoming:
		if stream == nil {
			return nil, m.Err()
		}
		return stream, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-m.done:
		return nil, m.Err()
	}
}

func (m *MuxLink) AcceptRemote(ctx context.Context, id uint32) error {
	if err := m.dispatch.streams.AcceptRemote(id); err != nil {
		return err
	}
	if err := m.write(ctx, Frame{Type: FrameOpenOK, StreamID: id}); err != nil {
		m.Close(err)
		return err
	}
	frames, err := m.dispatch.streams.DrainPendingOpenFrames(id)
	if err != nil {
		return err
	}
	stream := m.getStream(id)
	if stream == nil {
		return ErrUnknownStream
	}
	for _, frame := range frames {
		if err := stream.enqueue(frame); err != nil {
			m.reset(id, 8)
			return err
		}
	}
	return nil
}

func (m *MuxLink) Err() error {
	if err := m.dispatch.Err(); err != nil {
		return err
	}
	return ErrProtocol
}
func (m *MuxLink) Closed() <-chan struct{} { return m.done }

func (m *MuxLink) Poll(ctx context.Context) (LinkAction, error) {
	action := m.dispatch.PollLiveness()
	switch action {
	case LinkSendPing:
		var nonce [8]byte
		if _, err := rand.Read(nonce[:]); err != nil {
			m.Close(err)
			return LinkUnavailable, err
		}
		m.mu.Lock()
		m.pingNonce = nonce
		m.pingPending = true
		m.mu.Unlock()
		if err := m.write(ctx, Frame{Type: FramePing, Payload: nonce[:]}); err != nil {
			m.Close(err)
			return LinkUnavailable, err
		}
	case LinkUnavailable:
		m.Close(ErrProtocol)
	}
	return action, nil
}

func (m *MuxLink) Close(err error) {
	if err == nil {
		err = ErrProtocol
	}
	m.closeOnce.Do(func() {
		m.dispatch.Close(err)
		m.windows.Close()
		close(m.done)
		m.mu.Lock()
		for _, stream := range m.streams {
			stream.stop()
		}
		m.streams = make(map[uint32]*MuxStream)
		m.mu.Unlock()
		if tlsConn, ok := m.conn.(*tls.Conn); ok {
			forceCloseTLS(tlsConn)
		} else {
			_ = m.conn.Close()
		}
	})
}

func (m *MuxLink) readLoop() {
	for {
		frame, err := ReadFrame(m.conn)
		if err != nil {
			m.Close(err)
			return
		}
		switch frame.Type {
		case FrameWindow:
			delta := binary.BigEndian.Uint32(frame.Payload)
			if err := m.windows.Grant(frame.StreamID, delta); err != nil {
				if frame.StreamID != 0 && errors.Is(err, ErrUnknownStream) {
					m.reset(frame.StreamID, 8)
					continue
				}
				m.Close(err)
				return
			}
			m.dispatch.liveness.Received(m.dispatch.now())
			m.clearPing()
			continue
		case FramePing:
			if err := m.write(context.Background(), Frame{Type: FramePong, Payload: frame.Payload}); err != nil {
				m.Close(err)
				return
			}
			m.dispatch.liveness.Received(m.dispatch.now())
			m.clearPing()
			continue
		case FramePong:
			m.mu.Lock()
			matches := m.pingPending && m.pingNonce == [8]byte(frame.Payload)
			m.pingPending = false
			m.mu.Unlock()
			if !matches {
				m.Close(fmt.Errorf("%w: unmatched PONG", ErrProtocol))
				return
			}
			m.dispatch.liveness.Received(m.dispatch.now())
			continue
		case FrameHello, FrameGoAway:
			m.Close(fmt.Errorf("%w: unexpected link-control frame in mux dispatcher", ErrProtocol))
			return
		}
		accepted, applyErr := m.dispatch.Apply(frame)
		if applyErr != nil {
			var streamErr *StreamViolation
			if errors.As(applyErr, &streamErr) {
				m.reset(streamErr.StreamID, 8)
				continue
			}
			m.Close(applyErr)
			return
		}
		m.clearPing()
		if frame.Type == FrameOpen {
			request, parseErr := ParseOpen(frame.Payload)
			if parseErr != nil {
				m.Close(parseErr)
				return
			}
			if err := m.windows.AddStream(frame.StreamID, m.config.StreamWindow); err != nil {
				m.Close(err)
				return
			}
			stream := m.newStream(frame.StreamID, StreamNetwork(request.Network))
			if stream == nil {
				return
			}
			select {
			case m.incoming <- stream:
			case <-m.done:
				return
			default:
				m.reset(frame.StreamID, 5)
			}
			continue
		}
		stream := m.getStream(frame.StreamID)
		if stream == nil {
			continue
		}
		if accepted.Type != 0 {
			if err := stream.enqueue(accepted); err != nil {
				m.reset(frame.StreamID, 8)
				continue
			}
		}
		if frame.Type == FrameOpenErr || frame.Type == FrameRST {
			m.removeStream(frame.StreamID)
		} else if frame.Type == FrameFIN {
			if snapshot, ok := m.dispatch.streams.Snapshot(frame.StreamID); !ok || snapshot.Lifecycle == StreamClosed {
				m.removeStream(frame.StreamID)
			}
		}
	}
}

func (m *MuxLink) clearPing() { m.mu.Lock(); m.pingPending = false; m.mu.Unlock() }

func (m *MuxLink) newStream(id uint32, network StreamNetwork) *MuxStream {
	select {
	case <-m.done:
		return nil
	default:
	}
	stream := &MuxStream{link: m, ID: id, Network: network, receive: make(chan Frame, m.config.StreamQueueFrames), done: make(chan struct{})}
	m.mu.Lock()
	select {
	case <-m.done:
		m.mu.Unlock()
		return nil
	default:
	}
	m.streams[id] = stream
	m.mu.Unlock()
	return stream
}
func (m *MuxLink) getStream(id uint32) *MuxStream {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.streams[id]
}
func (m *MuxLink) removeStream(id uint32) {
	m.mu.Lock()
	stream := m.streams[id]
	delete(m.streams, id)
	m.mu.Unlock()
	if stream != nil {
		stream.stop()
	}
	m.dispatch.streams.Drop(id)
	m.windows.RemoveStream(id)
}
func (m *MuxLink) write(ctx context.Context, frame Frame) error {
	m.writeMu.Lock()
	defer m.writeMu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	return WriteFrame(m.conn, frame)
}
func (m *MuxLink) reset(id uint32, code uint16) {
	var payload [2]byte
	binary.BigEndian.PutUint16(payload[:], code)
	if err := m.write(context.Background(), Frame{Type: FrameRST, StreamID: id, Payload: payload[:]}); err != nil {
		m.Close(err)
		return
	}
	m.removeStream(id)
}

func (s *MuxStream) stop() { s.closeOnce.Do(func() { close(s.done) }) }
func (s *MuxStream) Receive(ctx context.Context) (Frame, error) {
	select {
	case frame := <-s.receive:
		return frame, nil
	default:
	}
	select {
	case frame := <-s.receive:
		return frame, nil
	case <-s.done:
		return Frame{}, ErrStreamState
	case <-ctx.Done():
		return Frame{}, ctx.Err()
	case <-s.link.done:
		return Frame{}, s.link.Err()
	}
}
func (s *MuxStream) WriteData(ctx context.Context, payload []byte) error {
	if s.Network != StreamTCP || len(payload) == 0 {
		return ErrProtocol
	}
	for len(payload) > 0 {
		size := len(payload)
		if size > MaxPayloadSize {
			size = MaxPayloadSize
		}
		if err := s.link.windows.Acquire(ctx, s.ID, uint32(size)); err != nil {
			return err
		}
		chunk := append([]byte(nil), payload[:size]...)
		if err := s.link.write(ctx, Frame{Type: FrameData, StreamID: s.ID, Payload: chunk}); err != nil {
			s.link.Close(err)
			return err
		}
		payload = payload[size:]
	}
	return nil
}
func (s *MuxStream) WriteDgram(ctx context.Context, payload []byte) (bool, error) {
	if s.Network != StreamUDP || len(payload) == 0 || len(payload) > MaxPayloadSize {
		return false, ErrProtocol
	}
	ok, err := s.link.windows.TryAcquire(s.ID, uint32(len(payload)))
	if err != nil || !ok {
		return ok, err
	}
	err = s.link.write(ctx, Frame{Type: FrameDgram, StreamID: s.ID, Payload: append([]byte(nil), payload...)})
	if err != nil {
		s.link.Close(err)
	}
	return true, err
}
func (s *MuxStream) CloseWrite(ctx context.Context) error {
	if s.Network != StreamTCP {
		return ErrProtocol
	}
	if err := s.link.dispatch.streams.MarkLocalFIN(s.ID); err != nil {
		return err
	}
	err := s.link.write(ctx, Frame{Type: FrameFIN, StreamID: s.ID})
	if err != nil {
		s.link.Close(err)
	} else if _, open := s.link.dispatch.streams.Snapshot(s.ID); !open {
		s.link.removeStream(s.ID)
	}
	return err
}
func (s *MuxStream) Consume(ctx context.Context, bytes uint32) error {
	if bytes == 0 {
		return nil
	}
	var payload [4]byte
	binary.BigEndian.PutUint32(payload[:], bytes)
	if err := s.link.write(ctx, Frame{Type: FrameWindow, StreamID: s.ID, Payload: payload[:]}); err != nil {
		s.link.Close(err)
		return err
	}
	if err := s.link.write(ctx, Frame{Type: FrameWindow, StreamID: 0, Payload: payload[:]}); err != nil {
		s.link.Close(err)
		return err
	}
	return nil
}

func (s *MuxStream) enqueue(frame Frame) error {
	select {
	case s.receive <- frame:
		return nil
	case <-s.done:
		return ErrStreamState
	case <-s.link.done:
		return s.link.Err()
	default:
		return fmt.Errorf("%w: stream receive queue is full", ErrStreamState)
	}
}
