package vlink

import (
	"context"
	"crypto/rand"
	"crypto/tls"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"sync"
	"time"

	"vortex/engine/scheduler"
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
	Limits                                   LimitConfig
	Observer                                 Observer
	Now                                      func() time.Time
	DgramScheduler                           *scheduler.Scheduler
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
	peerID      NodeID
	limiter     *FrameLimiter
	observer    Observer
	dgrams      *scheduler.Scheduler
	violations  int
	linkID      [16]byte
}

type MuxStream struct {
	link       *MuxLink
	ID         uint32
	Network    StreamNetwork
	receive    chan Frame
	done       chan struct{}
	closeOnce  sync.Once
	resourceMu sync.Mutex
	resource   io.Closer
}

type RemoteOpenAuthorizer func(context.Context, NodeID, OpenRequest) (io.Closer, error)

func NewAuthenticatedMuxLink(ctx context.Context, conn *tls.Conn, hello HelloConfig, config MuxConfig) (*MuxLink, NodeID, error) {
	peer, err := HandshakeHello(ctx, conn, hello)
	if err != nil {
		forceCloseTLS(conn)
		return nil, NodeID{}, err
	}
	mux, err := newMuxLink(conn, config)
	if err != nil {
		forceCloseTLS(conn)
		return nil, NodeID{}, err
	}
	mux.peerID = peer
	return mux, peer, nil
}

func newMuxLink(conn net.Conn, config MuxConfig) (*MuxLink, error) {
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
	limiter, err := NewFrameLimiter(config.Limits, config.Now)
	if err != nil {
		return nil, err
	}
	dgrams := config.DgramScheduler
	if dgrams == nil {
		dgrams = scheduler.New(1 << 20)
	}
	m := &MuxLink{conn: conn, dispatch: NewDispatcherWithClock(config.LocalClient, config.MaxPendingOpenBytes, config.Now), windows: NewWindowManager(config.AggregateWindow, config.MaxWindow), config: config, streams: make(map[uint32]*MuxStream), incoming: make(chan *MuxStream, 64), done: make(chan struct{}), limiter: limiter, observer: config.Observer, dgrams: dgrams}
	if _, err := rand.Read(m.linkID[:]); err != nil {
		return nil, err
	}
	go m.readLoop()
	go m.dgramLoop()
	return m, nil
}

func (m *MuxLink) dgramLoop() {
	for {
		item, err := m.dgrams.Next(context.Background())
		if err != nil {
			return
		}
		if item.Kind != scheduler.UDP {
			continue
		}
		if err := m.write(context.Background(), Frame{Type: FrameDgram, StreamID: item.StreamID, Payload: item.Payload}); err != nil {
			m.Close(err)
			return
		}
	}
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

func (m *MuxLink) AuthorizeRemote(ctx context.Context, id uint32, authorize RemoteOpenAuthorizer) error {
	request, ok := m.dispatch.streams.RemoteOpenRequest(id)
	if !ok {
		return &StreamViolation{StreamID: id, Cause: ErrStreamState}
	}
	var resource io.Closer
	var err error
	if authorize == nil {
		rejection := &OpenRejection{Code: OpenUnauthorized}
		_, _ = rand.Read(rejection.CorrelationID[:])
		err = rejection
	} else {
		resource, err = authorize(ctx, m.peerID, request)
	}
	if err != nil {
		if resource != nil {
			_ = resource.Close()
		}
		var rejection *OpenRejection
		if !errors.As(err, &rejection) {
			rejection = &OpenRejection{Code: OpenUnauthorized}
			_, _ = rand.Read(rejection.CorrelationID[:])
		}
		if rejection.CorrelationID == ([16]byte{}) {
			_, _ = rand.Read(rejection.CorrelationID[:])
		}
		payload, payloadErr := OpenErrorPayload(rejection)
		if payloadErr != nil {
			return payloadErr
		}
		writeErr := m.write(ctx, Frame{Type: FrameOpenErr, StreamID: id, Payload: payload})
		m.removeStream(id)
		if writeErr != nil {
			m.Close(writeErr)
			return writeErr
		}
		return err
	}
	stream := m.getStream(id)
	if stream == nil {
		if resource != nil {
			_ = resource.Close()
		}
		return ErrUnknownStream
	}
	stream.resourceMu.Lock()
	stream.resource = resource
	stream.resourceMu.Unlock()
	if acceptErr := m.acceptRemote(ctx, id); acceptErr != nil {
		stream.closeResource()
		return acceptErr
	}
	return nil
}

func (m *MuxLink) acceptRemote(ctx context.Context, id uint32) error {
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
		m.observeClose(err)
		m.dispatch.Close(err)
		m.windows.Close()
		close(m.done)
		m.mu.Lock()
		for _, stream := range m.streams {
			stream.stop()
			stream.closeResource()
		}
		m.streams = make(map[uint32]*MuxStream)
		m.mu.Unlock()
		m.dgrams.Close()
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
		if m.observer != nil {
			m.observer.Frame(m.peerRole(), frame.Type, 1, len(frame.Payload))
		}
		allowed, overLimit, limitErr := m.limiter.Allow(frame.Type)
		if limitErr != nil {
			m.Close(limitErr)
			return
		}
		if !allowed {
			if overLimit {
				if m.recordViolation(ErrorClassRateLimited) {
					return
				}
			}
			if frame.Type == FrameOpen && frame.StreamID != 0 {
				m.reset(frame.StreamID, uint16(OpenRateLimited))
				continue
			}
			continue
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
				if m.recordViolation(ErrorClassStreamViolation) {
					return
				}
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
	count := len(m.streams)
	m.mu.Unlock()
	if m.observer != nil {
		m.observer.ActiveStreams(count)
	}
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
	count := len(m.streams)
	m.mu.Unlock()
	if stream != nil {
		stream.stop()
		stream.closeResource()
	}
	m.dispatch.streams.Drop(id)
	m.windows.RemoveStream(id)
	if m.observer != nil {
		m.observer.ActiveStreams(count)
	}
}
func (m *MuxLink) write(ctx context.Context, frame Frame) error {
	m.writeMu.Lock()
	defer m.writeMu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := WriteFrame(m.conn, frame); err != nil {
		return err
	}
	if m.observer != nil {
		m.observer.Frame(m.peerRole(), frame.Type, 2, len(frame.Payload))
	}
	return nil
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
func (s *MuxStream) closeResource() {
	s.resourceMu.Lock()
	resource := s.resource
	s.resource = nil
	s.resourceMu.Unlock()
	if resource != nil {
		_ = resource.Close()
	}
}
func (s *MuxStream) TakeResource() io.Closer {
	s.resourceMu.Lock()
	defer s.resourceMu.Unlock()
	resource := s.resource
	s.resource = nil
	return resource
}
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
		if size > MaxTLSRecordPayload {
			size = MaxTLSRecordPayload
		}
		waitStarted := time.Now()
		if err := s.link.windows.Acquire(ctx, s.ID, uint32(size)); err != nil {
			return err
		}
		if s.link.observer != nil {
			s.link.observer.WindowWait(time.Since(waitStarted))
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
	snapshot, open := s.link.dispatch.streams.Snapshot(s.ID)
	if !open || !snapshot.OpenOK {
		return false, ErrStreamState
	}
	if err := ctx.Err(); err != nil {
		return false, err
	}
	// Preserve the existing per-stream admission boundary; the scheduler adds
	// an independent bounded queue after this non-blocking check.
	if ok, err := s.link.windows.TryAcquire(s.ID, uint32(len(payload))); err != nil || !ok {
		return ok, err
	}
	ok, reason := s.link.dgrams.Enqueue(scheduler.Item{Kind: scheduler.UDP, StreamID: s.ID, Payload: payload})
	if !ok {
		return false, fmt.Errorf("vlink: UDP datagram dropped: %s", reason)
	}
	return true, nil
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

func (m *MuxLink) peerRole() PeerRole {
	if m.config.LocalClient {
		return PeerRoleServer
	}
	return PeerRoleClient
}

func (m *MuxLink) recordViolation(class ErrorClass) bool {
	m.mu.Lock()
	m.violations++
	thresholdReached := m.violations >= m.limiter.Threshold()
	m.mu.Unlock()
	if m.observer != nil {
		role := m.peerRole()
		m.observer.Rejected(role, class)
		m.observer.Event(VLinkEvent{At: time.Now(), Kind: "security_rejection", LinkID: m.linkID, PeerRole: role, ErrorClass: class})
	}
	if thresholdReached {
		m.Close(ErrRateLimited)
		return true
	}
	return false
}

func (m *MuxLink) observeClose(err error) {
	if m.observer == nil {
		return
	}
	class := ErrorClassTransport
	if errors.Is(err, ErrRateLimited) {
		class = ErrorClassRateLimited
	} else if errors.Is(err, ErrProtocol) {
		class = ErrorClassProtocol
	}
	role := m.peerRole()
	m.observer.Closed(role, class)
	m.observer.Event(VLinkEvent{At: time.Now(), Kind: "link_close", LinkID: m.linkID, PeerRole: role, ErrorClass: class})
}
