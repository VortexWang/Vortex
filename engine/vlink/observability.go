package vlink

import (
	"errors"
	"sync"
	"time"
)

type PeerRole string
type ErrorClass string

const (
	PeerRoleClient PeerRole = "client"
	PeerRoleServer PeerRole = "server"
	PeerRolePeer   PeerRole = "peer"

	ErrorClassNone            ErrorClass = "none"
	ErrorClassProtocol        ErrorClass = "protocol"
	ErrorClassIdentity        ErrorClass = "identity"
	ErrorClassStreamViolation ErrorClass = "stream_violation"
	ErrorClassRateLimited     ErrorClass = "rate_limited"
	ErrorClassTransport       ErrorClass = "transport"
	ErrorClassAuthorization   ErrorClass = "authorization"
)

type MetricSnapshot struct {
	FramesIn   map[FrameType]uint64
	FramesOut  map[FrameType]uint64
	BytesIn    map[FrameType]uint64
	BytesOut   map[FrameType]uint64
	CloseCount map[ErrorClass]uint64
	Rejects    map[ErrorClass]uint64
	WindowWait time.Duration
	Pool       PoolSnapshot
	Streams    int
}

type VLinkEvent struct {
	At            time.Time
	Kind          string
	TraceID       [16]byte
	LinkID        [16]byte
	PeerRole      PeerRole
	FrameType     FrameType
	ErrorClass    ErrorClass
	Bytes         uint64
	Duration      time.Duration
	CorrelationID [16]byte
}

type Observer interface {
	Frame(PeerRole, FrameType, uint8, int)
	Event(VLinkEvent)
	Closed(PeerRole, ErrorClass)
	Rejected(PeerRole, ErrorClass)
	WindowWait(time.Duration)
	ActiveStreams(int)
}

type MemoryObserver struct {
	mu        sync.Mutex
	maxEvents int
	events    []VLinkEvent
	snapshot  MetricSnapshot
	closed    bool
}

func NewMemoryObserver(maxEvents int) (*MemoryObserver, error) {
	if maxEvents < 1 {
		return nil, errors.New("vlink: event retention must be positive")
	}
	return &MemoryObserver{maxEvents: maxEvents, snapshot: MetricSnapshot{
		FramesIn: make(map[FrameType]uint64), FramesOut: make(map[FrameType]uint64),
		BytesIn: make(map[FrameType]uint64), BytesOut: make(map[FrameType]uint64),
		CloseCount: make(map[ErrorClass]uint64), Rejects: make(map[ErrorClass]uint64),
	}}, nil
}

func (o *MemoryObserver) Frame(role PeerRole, frameType FrameType, direction uint8, bytes int) {
	if bytes < 0 || !validPeerRole(role) || frameType < FrameHello || frameType > FrameGoAway || direction < 1 || direction > 2 {
		return
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.closed {
		return
	}
	if direction == 1 {
		o.snapshot.FramesIn[frameType]++
		o.snapshot.BytesIn[frameType] += uint64(bytes)
	} else {
		o.snapshot.FramesOut[frameType]++
		o.snapshot.BytesOut[frameType] += uint64(bytes)
	}
}

func (o *MemoryObserver) Event(event VLinkEvent) {
	if !validEventKind(event.Kind) || !validPeerRole(event.PeerRole) || !validErrorClass(event.ErrorClass) || event.FrameType > FrameGoAway || event.Bytes > uint64(MaxPayloadSize) {
		return
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.closed {
		return
	}
	if len(o.events) == o.maxEvents {
		copy(o.events, o.events[1:])
		o.events[len(o.events)-1] = event
		return
	}
	o.events = append(o.events, event)
}

func (o *MemoryObserver) Closed(role PeerRole, class ErrorClass) {
	if !validPeerRole(role) || !validErrorClass(class) {
		return
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	if !o.closed {
		o.snapshot.CloseCount[class]++
	}
}

func (o *MemoryObserver) Rejected(role PeerRole, class ErrorClass) {
	if !validPeerRole(role) || !validErrorClass(class) {
		return
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	if !o.closed {
		o.snapshot.Rejects[class]++
	}
}

func (o *MemoryObserver) WindowWait(duration time.Duration) {
	if duration <= 0 {
		return
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	if !o.closed {
		o.snapshot.WindowWait += duration
	}
}

func (o *MemoryObserver) ActiveStreams(count int) {
	if count < 0 {
		return
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	if !o.closed {
		o.snapshot.Streams = count
	}
}

func (o *MemoryObserver) SetPool(snapshot PoolSnapshot) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if !o.closed {
		o.snapshot.Pool = snapshot
	}
}

func (o *MemoryObserver) Snapshot() MetricSnapshot {
	o.mu.Lock()
	defer o.mu.Unlock()
	return cloneMetrics(o.snapshot)
}

func (o *MemoryObserver) Events() []VLinkEvent {
	o.mu.Lock()
	defer o.mu.Unlock()
	return append([]VLinkEvent(nil), o.events...)
}

func (o *MemoryObserver) Shutdown() {
	o.mu.Lock()
	o.closed = true
	o.mu.Unlock()
}

func cloneMetrics(source MetricSnapshot) MetricSnapshot {
	copyFrameMap := func(input map[FrameType]uint64) map[FrameType]uint64 {
		out := make(map[FrameType]uint64, len(input))
		for key, value := range input {
			out[key] = value
		}
		return out
	}
	copyErrorMap := func(input map[ErrorClass]uint64) map[ErrorClass]uint64 {
		out := make(map[ErrorClass]uint64, len(input))
		for key, value := range input {
			out[key] = value
		}
		return out
	}
	source.FramesIn = copyFrameMap(source.FramesIn)
	source.FramesOut = copyFrameMap(source.FramesOut)
	source.BytesIn = copyFrameMap(source.BytesIn)
	source.BytesOut = copyFrameMap(source.BytesOut)
	source.CloseCount = copyErrorMap(source.CloseCount)
	source.Rejects = copyErrorMap(source.Rejects)
	return source
}

func validPeerRole(role PeerRole) bool {
	return role == PeerRoleClient || role == PeerRoleServer || role == PeerRolePeer
}
func validErrorClass(class ErrorClass) bool {
	switch class {
	case ErrorClassNone, ErrorClassProtocol, ErrorClassIdentity, ErrorClassStreamViolation, ErrorClassRateLimited, ErrorClassTransport, ErrorClassAuthorization:
		return true
	default:
		return false
	}
}
func validEventKind(kind string) bool {
	switch kind {
	case "link_state", "security_rejection", "stream_reset", "link_close", "pool_state":
		return true
	default:
		return false
	}
}
