package vlink

import (
	"context"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"sync"
	"sync/atomic"
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
	// Admit rechecks current link identities before new stream authority.
	Admit                                    func() error
	LocalClient                              bool
	MaxPendingOpenBytes                      int
	StreamWindow, AggregateWindow, MaxWindow uint32
	UDPWindowReserve                         uint32
	StreamQueueFrames                        int
	UDPQueueFrames                           int
	UDPQueueBytes                            int64
	Limits                                   LimitConfig
	Observer                                 Observer
	Now                                      func() time.Time
	DgramScheduler                           *scheduler.Scheduler
}

type MuxLink struct {
	conn                    net.Conn
	dispatch                *Dispatcher
	windows                 *WindowManager
	config                  MuxConfig
	controlWrites           chan frameWrite
	dataWrites              chan frameWrite
	writeWake               chan struct{}
	writerStopped           chan struct{}
	mu                      sync.Mutex
	streams                 map[uint32]*MuxStream
	pingNonce               [8]byte
	pingPending             bool
	incoming                chan *MuxStream
	done                    chan struct{}
	closeOnce               sync.Once
	peerID                  NodeID
	peerBoot                BootID
	peerGeneration          uint64
	limiter                 *FrameLimiter
	observer                Observer
	dgrams                  *scheduler.Scheduler
	violations              int
	linkID                  [16]byte
	udpWindowDrops          atomic.Uint64
	udpStreamWindowDrops    atomic.Uint64
	udpAggregateWindowDrops atomic.Uint64
	udpReceiveDrops         atomic.Uint64
	windowMu                sync.Mutex
	pendingStream           map[uint32]uint64
	pendingTotal            uint64
	streamGrant             uint32
	aggregateGrant          uint32
}

type MuxStream struct {
	link        *MuxLink
	ID          uint32
	Network     StreamNetwork
	receive     chan Frame
	done        chan struct{}
	closeOnce   sync.Once
	writeClosed atomic.Bool
	udpQueued   atomic.Int64
	resourceMu  sync.Mutex
	resource    io.Closer
}

type RemoteOpenAuthorizer func(context.Context, NodeID, OpenRequest) (io.Closer, error)

func NewAuthenticatedMuxLink(ctx context.Context, conn *tls.Conn, hello HelloConfig, config MuxConfig) (*MuxLink, NodeID, error) {
	var peerBoot BootID
	var peerGeneration uint64
	verify := hello.VerifyPeer
	if verify == nil {
		return nil, NodeID{}, errors.New("vlink: peer verifier required")
	}
	hello.VerifyPeer = func(ctx context.Context, cert *x509.Certificate, message HelloMessage) error {
		if verify != nil {
			if err := verify(ctx, cert, message); err != nil {
				return err
			}
		}
		peerBoot, peerGeneration = message.LocalBootID, message.LocalGeneration
		return nil
	}
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
	mux.peerBoot, mux.peerGeneration = peerBoot, peerGeneration
	return mux, peer, nil
}

// PeerIncarnation is authenticated by the TLS-exporter-bound HELLO transcript.
func (m *MuxLink) PeerIncarnation() (BootID, uint64) { return m.peerBoot, m.peerGeneration }

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
		queueBytes := config.UDPQueueBytes
		if queueBytes <= 0 {
			queueBytes = 1 << 20
		}
		dgrams = scheduler.New(uint64(queueBytes))
	}
	aggregateGrant := config.AggregateWindow / 2
	m := &MuxLink{conn: conn, dispatch: NewDispatcherWithClock(config.LocalClient, config.MaxPendingOpenBytes, config.Now), windows: NewWindowManagerWithReserve(config.AggregateWindow, config.MaxWindow, config.UDPWindowReserve), config: config, streams: make(map[uint32]*MuxStream), incoming: make(chan *MuxStream, 64), done: make(chan struct{}), limiter: limiter, observer: config.Observer, dgrams: dgrams, controlWrites: make(chan frameWrite, 64), dataWrites: make(chan frameWrite, 64), writeWake: make(chan struct{}, 1), writerStopped: make(chan struct{}), pendingStream: make(map[uint32]uint64), streamGrant: uint32(uint64(config.StreamWindow) * 3 / 4), aggregateGrant: aggregateGrant}
	if m.streamGrant == 0 {
		m.streamGrant = 1
	}
	if m.aggregateGrant == 0 {
		m.aggregateGrant = 1
	}
	if _, err := rand.Read(m.linkID[:]); err != nil {
		return nil, err
	}
	go m.readLoop()
	go m.dgramLoop()
	return m, nil
}

func (m *MuxLink) Open(ctx context.Context, request OpenRequest) (*MuxStream, error) {
	if m.config.Admit != nil {
		if err := m.config.Admit(); err != nil {
			return nil, err
		}
	}
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
	if m.config.Admit != nil {
		err = m.config.Admit()
	}
	if err != nil {
		// Preserve existing stream liabilities; only reject this new OPEN.
	} else if authorize == nil {
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
	if errors.Is(err, ErrRateLimited) {
		slog.Warn("vlink link closed after repeated violations", "reason", err)
	}
	m.closeOnce.Do(func() {
		defer func() {
			slog.Info("udp link drop summary", "link_id", fmt.Sprintf("%x", m.linkID), "window_drops", m.udpWindowDrops.Load(), "queue_drops", m.dgrams.Drops(scheduler.DropQueueFull), "receive_queue_drops", m.udpReceiveDrops.Load(), "stream_credit_drops", m.udpStreamWindowDrops.Load(), "aggregate_credit_drops", m.udpAggregateWindowDrops.Load())
		}()
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
	var owned Frame
	defer func() { owned.Release() }()
	for {
		owned.Release()
		frame, err := readMuxFrame(m.conn)
		owned = frame
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
			// Losing WINDOW loses credit permanently. A throttled credit
			// update must fail the transport, never leave a live stalled link.
			if frame.Type == FrameWindow {
				m.Close(fmt.Errorf("%w: WINDOW rate exceeded", ErrRateLimited))
				return
			}
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
			if frame.StreamID != 0 {
				if _, ok := m.dispatch.streams.Snapshot(frame.StreamID); !ok {
					continue
				}
			}
			if err := m.windows.Grant(frame.StreamID, delta); err != nil {
				if frame.StreamID != 0 && errors.Is(err, ErrUnknownStream) {
					// Credit for a stream removed after a concurrent close is stale.
					// It cannot create capacity, and replying with RST would create a
					// reset loop with the peer.
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
				slog.Warn("vlink stream violation", "stream_id", streamErr.StreamID, "frame_type", frame.Type, "cause", streamErr.Cause)
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
				if accepted.Type == FrameDgram {
					m.udpReceiveDrops.Add(1)
					if err := stream.Consume(context.Background(), uint32(len(accepted.Payload))); err != nil {
						return
					}
					continue
				}
				m.reset(frame.StreamID, 8)
				continue
			}
			owned = Frame{} // receive queue now owns the lease
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
	queueFrames := m.config.StreamQueueFrames
	if network == StreamUDP && m.config.UDPQueueFrames > 0 {
		queueFrames = m.config.UDPQueueFrames
	}
	stream := &MuxStream{link: m, ID: id, Network: network, receive: make(chan Frame, queueFrames), done: make(chan struct{})}
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
	m.windowMu.Lock()
	delete(m.pendingStream, id)
	m.windowMu.Unlock()
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
		return s.dequeued(frame), nil
	default:
	}
	select {
	case frame := <-s.receive:
		return s.dequeued(frame), nil
	case <-s.done:
		select {
		case frame := <-s.receive:
			return s.dequeued(frame), nil
		default:
			return Frame{}, ErrStreamState
		}
	case <-ctx.Done():
		return Frame{}, ctx.Err()
	case <-s.link.done:
		return Frame{}, s.link.Err()
	}
}

// WaitOpen blocks until the remote side authorizes this locally opened stream.
// DATA and DGRAM must not be written before OPEN_OK has been consumed.
func (s *MuxStream) WaitOpen(ctx context.Context) error {
	frame, err := s.Receive(ctx)
	if err != nil {
		return err
	}
	switch frame.Type {
	case FrameOpenOK:
		return nil
	case FrameOpenErr:
		if len(frame.Payload) >= 2 {
			return fmt.Errorf("vlink: remote OPEN rejected (class %d)", binary.BigEndian.Uint16(frame.Payload[:2]))
		}
		return fmt.Errorf("vlink: remote OPEN rejected")
	case FrameRST:
		return ErrStreamState
	default:
		return fmt.Errorf("%w: expected OPEN_OK, got frame type %d", ErrProtocol, frame.Type)
	}
}
func (s *MuxStream) WriteData(ctx context.Context, payload []byte) error {
	if s.Network != StreamTCP || len(payload) == 0 {
		return ErrProtocol
	}
	for len(payload) > 0 {
		groupSize := len(payload)
		if groupSize > 4*MaxTLSRecordPayload {
			groupSize = 4 * MaxTLSRecordPayload
		}
		// Queue at most four independently windowed frames before waiting.
		// waitWrite resolves ownership before this method returns, including
		// transport closure, so the caller may safely reuse its input then.
		owned := payload[:groupSize]
		var pending [4]frameWrite
		count := 0
		for len(owned) > 0 {
			size := len(owned)
			if size > MaxTLSRecordPayload {
				size = MaxTLSRecordPayload
			}
			// A TCP sender may pipeline a bounded prefix while OPEN is in
			// flight. The peer buffers that prefix until authorization; the
			// local check prevents an unbounded pre-authorization queue.
			if err := s.reserveEarlyData(size); err != nil {
				resetCtx, cancel := context.WithTimeout(ctx, time.Second)
				_ = s.Reset(resetCtx, 8)
				cancel()
				return err
			}
			waitStarted := time.Now()
			if err := s.link.windows.Acquire(ctx, s.ID, uint32(size)); err != nil {
				s.link.Close(err)
				<-s.link.writerStopped
				return err
			}
			if s.link.observer != nil {
				s.link.observer.WindowWait(time.Since(waitStarted))
			}
			request, err := s.link.enqueueWrite(ctx, Frame{Type: FrameData, StreamID: s.ID, Payload: owned[:size]})
			if err != nil {
				s.link.Close(err)
				<-s.link.writerStopped
				return err
			}
			pending[count] = request
			count++
			owned = owned[size:]
		}
		for i := 0; i < count; i++ {
			if err := s.link.waitWrite(pending[i]); err != nil {
				s.link.Close(err)
				<-s.link.writerStopped
				return err
			}
		}
		payload = payload[groupSize:]
	}
	return nil
}

func (s *MuxStream) reserveEarlyData(bytes int) error {
	snapshot, open := s.link.dispatch.streams.Snapshot(s.ID)
	if !open {
		return ErrStreamState
	}
	if snapshot.Lifecycle != StreamOpening {
		return nil
	}
	if err := s.link.dispatch.streams.MarkLocalData(s.ID, bytes); err != nil {
		// OPEN_OK may have raced the snapshot. Re-check before rejecting a
		// legitimate post-authorization write.
		if current, stillOpen := s.link.dispatch.streams.Snapshot(s.ID); stillOpen && current.OpenOK {
			return nil
		}
		return err
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
	if ok, rejected, err := s.link.windows.tryAcquire(s.ID, uint32(len(payload))); err != nil || !ok {
		if err == nil && !ok {
			s.link.udpWindowDrops.Add(1)
			if rejected.stream {
				s.link.udpStreamWindowDrops.Add(1)
			}
			if rejected.aggregate {
				s.link.udpAggregateWindowDrops.Add(1)
			}
		}
		return ok, err
	}
	ok, reason := s.link.dgrams.Enqueue(scheduler.Item{Kind: scheduler.UDP, StreamID: s.ID, Payload: payload})
	if !ok {
		_ = s.link.windows.Grant(s.ID, uint32(len(payload)))
		_ = s.link.windows.Grant(0, uint32(len(payload)))
		if reason == scheduler.DropQueueFull {
			return false, nil
		}
		return false, fmt.Errorf("vlink: UDP datagram rejected: %s", reason)
	}
	s.link.wakeWriter()
	return true, nil
}

func (m *MuxLink) UDPWindowDrops() uint64 {
	if m == nil {
		return 0
	}
	return m.udpWindowDrops.Load()
}

func (m *MuxLink) UDPQueueDrops() uint64 {
	if m == nil {
		return 0
	}
	return m.dgrams.Drops(scheduler.DropQueueFull) + m.udpReceiveDrops.Load()
}

func (s *MuxStream) UDPDropCounts() (window, queue uint64) {
	if s == nil || s.link == nil {
		return 0, 0
	}
	return s.link.UDPWindowDrops(), s.link.UDPQueueDrops()
}
func (s *MuxStream) CloseWrite(ctx context.Context) error {
	if s.Network != StreamTCP {
		return ErrProtocol
	}
	if !s.writeClosed.CompareAndSwap(false, true) {
		return nil
	}
	if err := s.link.flushWindows(ctx, s.ID); err != nil {
		s.writeClosed.Store(false)
		return err
	}
	if err := s.link.dispatch.streams.MarkLocalFIN(s.ID); err != nil {
		s.writeClosed.Store(false)
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

// Reset terminates one stream and releases its local resources. The reset is
// best-effort with respect to link shutdown; the stream is removed locally in
// either case so a failed peer cannot retain it indefinitely.
func (s *MuxStream) Reset(ctx context.Context, code uint16) error {
	if s == nil || s.link == nil {
		return ErrStreamState
	}
	var payload [2]byte
	binary.BigEndian.PutUint16(payload[:], code)
	err := s.link.write(ctx, Frame{Type: FrameRST, StreamID: s.ID, Payload: payload[:]})
	s.link.removeStream(s.ID)
	return err
}

func (s *MuxStream) Consume(ctx context.Context, bytes uint32) error {
	if bytes == 0 {
		return nil
	}
	grant := s.link.streamGrant
	if s.Network == StreamUDP {
		// UDP cannot wait for credit: return it before a scheduling pause can
		// exhaust the remaining stream window. TCP retains larger batches.
		grant = s.link.config.StreamWindow / 4
		if grant == 0 {
			grant = 1
		}
	}
	return s.link.consume(ctx, s.ID, bytes, grant)
}

func (m *MuxLink) consume(ctx context.Context, streamID, bytes, streamGrant uint32) error {
	m.windowMu.Lock()
	m.pendingStream[streamID] += uint64(bytes)
	m.pendingTotal += uint64(bytes)
	var streamDelta, aggregateDelta uint32
	if m.pendingStream[streamID] >= uint64(streamGrant) {
		streamDelta = uint32(m.pendingStream[streamID])
		m.pendingStream[streamID] = 0
	}
	if m.pendingTotal >= uint64(m.aggregateGrant) {
		aggregateDelta = uint32(m.pendingTotal)
		m.pendingTotal = 0
	}
	m.windowMu.Unlock()
	return m.writeWindows(ctx, streamID, streamDelta, aggregateDelta)
}

func (m *MuxLink) flushWindows(ctx context.Context, streamID uint32) error {
	m.windowMu.Lock()
	streamDelta := uint32(m.pendingStream[streamID])
	m.pendingStream[streamID] = 0
	aggregateDelta := uint32(m.pendingTotal)
	m.pendingTotal = 0
	m.windowMu.Unlock()
	return m.writeWindows(ctx, streamID, streamDelta, aggregateDelta)
}

func (m *MuxLink) writeWindows(ctx context.Context, streamID, streamDelta, aggregateDelta uint32) error {
	write := func(id, delta uint32) error {
		if delta == 0 {
			return nil
		}
		var payload [4]byte
		binary.BigEndian.PutUint32(payload[:], delta)
		return m.write(ctx, Frame{Type: FrameWindow, StreamID: id, Payload: payload[:]})
	}
	if err := write(streamID, streamDelta); err != nil {
		m.Close(err)
		return err
	}
	if err := write(0, aggregateDelta); err != nil {
		m.Close(err)
		return err
	}
	return nil
}

func (s *MuxStream) enqueue(frame Frame) error {
	if frame.Type == FrameDgram {
		limit := s.link.config.UDPQueueBytes
		if limit <= 0 {
			limit = int64(s.link.config.StreamQueueFrames) * MaxPayloadSize
		}
		if s.udpQueued.Add(int64(len(frame.Payload))) > limit {
			s.udpQueued.Add(-int64(len(frame.Payload)))
			return fmt.Errorf("%w: UDP receive byte queue is full", ErrStreamState)
		}
	}
	select {
	case s.receive <- frame:
		return nil
	case <-s.done:
		if frame.Type == FrameDgram {
			s.udpQueued.Add(-int64(len(frame.Payload)))
		}
		return ErrStreamState
	case <-s.link.done:
		if frame.Type == FrameDgram {
			s.udpQueued.Add(-int64(len(frame.Payload)))
		}
		return s.link.Err()
	default:
		if frame.Type == FrameDgram {
			s.udpQueued.Add(-int64(len(frame.Payload)))
		}
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
		slog.Warn("vlink link closed after repeated violations", "class", class, "count", m.violations)
		m.Close(fmt.Errorf("%w: repeated %s violations", ErrRateLimited, class))
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

// FlowKey is an opaque connection-lifetime accounting identity. Stream IDs are
// unique only within a link; including the random link ID prevents collisions.
func (s *MuxStream) FlowKey() string {
	return fmt.Sprintf("%x/%d/%d", s.link.linkID, s.ID, s.Network)
}

func (s *MuxStream) dequeued(frame Frame) Frame {
	if frame.Type == FrameDgram {
		s.udpQueued.Add(-int64(len(frame.Payload)))
	}
	return frame
}
