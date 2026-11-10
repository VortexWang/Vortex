package vlink

import (
	"errors"
	"fmt"
	"sync"
)

type StreamNetwork uint8

const (
	StreamTCP StreamNetwork = 1
	StreamUDP StreamNetwork = 2
)

type StreamLifecycle uint8

const (
	StreamOpening StreamLifecycle = iota
	StreamOpen
	StreamLocalHalfClosed
	StreamRemoteHalfClosed
	StreamClosed
	StreamReset
)

var ErrStreamState = errors.New("vlink: invalid stream state transition")

type StreamViolation struct {
	StreamID uint32
	Cause    error
}

func (e *StreamViolation) Error() string {
	return fmt.Sprintf("vlink: stream %d: %v", e.StreamID, e.Cause)
}
func (e *StreamViolation) Unwrap() error { return e.Cause }

type StreamSnapshot struct {
	ID               uint32
	Network          StreamNetwork
	Lifecycle        StreamLifecycle
	InitiatedLocal   bool
	OpenOK           bool
	LocalFIN         bool
	RemoteFIN        bool
	PendingOpenBytes int
	LocalOpenBytes   int
}
type streamRecord struct {
	StreamSnapshot
	pendingFrames []Frame
	openRequest   OpenRequest
}

type StreamTable struct {
	mu          sync.Mutex
	localClient bool
	allocator   StreamIDAllocator
	maxPending  int
	streams     map[uint32]*streamRecord
	closed      bool
}

func NewStreamTable(localClient bool, maxPendingOpenBytes int) *StreamTable {
	if maxPendingOpenBytes < 1 {
		maxPendingOpenBytes = 1
	}
	return &StreamTable{localClient: localClient, allocator: NewStreamIDAllocator(localClient), maxPending: maxPendingOpenBytes, streams: make(map[uint32]*streamRecord)}
}

func (t *StreamTable) Open(network StreamNetwork) (uint32, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.closed {
		return 0, ErrProtocol
	}
	if network != StreamTCP && network != StreamUDP {
		return 0, ErrProtocol
	}
	id, err := t.allocator.Next()
	if err != nil {
		return 0, err
	}
	t.streams[id] = &streamRecord{StreamSnapshot: StreamSnapshot{ID: id, Network: network, Lifecycle: StreamOpening, InitiatedLocal: true}}
	return id, nil
}

// Apply validates an incoming frame and returns a copy of the frame for the
// per-stream consumer. Semantic failures are scoped to one stream.
func (t *StreamTable) Apply(frame Frame) (Frame, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.closed {
		return Frame{}, ErrProtocol
	}
	if frame.Type == FrameOpen {
		request, err := ParseOpen(frame.Payload)
		if err != nil {
			return Frame{}, err
		}
		wantOdd := !t.localClient
		if frame.StreamID == 0 || (frame.StreamID%2 == 1) != wantOdd {
			return Frame{}, fmt.Errorf("%w: remote OPEN has invalid stream parity", ErrProtocol)
		}
		if _, exists := t.streams[frame.StreamID]; exists {
			return Frame{}, fmt.Errorf("%w: reused stream ID", ErrProtocol)
		}
		network := StreamNetwork(request.Network)
		t.streams[frame.StreamID] = &streamRecord{StreamSnapshot: StreamSnapshot{ID: frame.StreamID, Network: network, Lifecycle: StreamOpening}, openRequest: request}
		return frame, nil
	}
	if frame.Type == FrameWindow {
		// WINDOW may be in flight when the peer removes a completed stream.
		// Its credit cannot be applied after removal, but it is not a stream
		// operation and must not create a stream violation or RST loop.
		return frame, nil
	}
	if frame.Type == FrameRST {
		// RST is terminal and may cross a concurrent local teardown. Treating a
		// duplicate/late reset as a violation would make peers reset each other.
		return frame, nil
	}
	stream := t.streams[frame.StreamID]
	if stream == nil {
		return Frame{}, &StreamViolation{StreamID: frame.StreamID, Cause: ErrStreamState}
	}
	violation := func() (Frame, error) {
		delete(t.streams, frame.StreamID)
		return Frame{}, &StreamViolation{StreamID: frame.StreamID, Cause: ErrStreamState}
	}
	switch frame.Type {
	case FrameOpenOK:
		if !stream.InitiatedLocal || stream.OpenOK || stream.Lifecycle != StreamOpening {
			return violation()
		}
		stream.OpenOK = true
		if stream.LocalFIN {
			stream.Lifecycle = StreamLocalHalfClosed
		} else {
			stream.Lifecycle = StreamOpen
		}
	case FrameOpenErr:
		if !stream.InitiatedLocal || stream.Lifecycle != StreamOpening {
			return violation()
		}
		stream.Lifecycle = StreamReset
		delete(t.streams, frame.StreamID)
	case FrameData:
		if stream.Network != StreamTCP || stream.RemoteFIN || stream.Lifecycle == StreamReset || stream.Lifecycle == StreamClosed {
			return violation()
		}
		if stream.Lifecycle == StreamOpening {
			if stream.InitiatedLocal {
				return violation()
			}
			if len(frame.Payload) > t.maxPending-stream.PendingOpenBytes {
				return violation()
			}
			stream.PendingOpenBytes += len(frame.Payload)
			stream.pendingFrames = append(stream.pendingFrames, Frame{Type: frame.Type, Flags: frame.Flags, StreamID: frame.StreamID, Payload: append([]byte(nil), frame.Payload...)})
			return Frame{}, nil
		}
	case FrameDgram:
		if stream.Network != StreamUDP || !stream.OpenOK || stream.Lifecycle != StreamOpen {
			return violation()
		}
	case FrameFIN:
		if stream.Network != StreamTCP || stream.RemoteFIN || stream.Lifecycle == StreamOpening && stream.InitiatedLocal || stream.Lifecycle == StreamReset || stream.Lifecycle == StreamClosed {
			return violation()
		}
		if stream.Lifecycle == StreamOpening {
			stream.RemoteFIN = true
			stream.pendingFrames = append(stream.pendingFrames, frame)
			return Frame{}, nil
		}
		stream.RemoteFIN = true
		if stream.LocalFIN {
			stream.Lifecycle = StreamClosed
			delete(t.streams, frame.StreamID)
		} else {
			stream.Lifecycle = StreamRemoteHalfClosed
		}
	case FrameRST:
		stream.Lifecycle = StreamReset
		delete(t.streams, frame.StreamID)
	default:
		return violation()
	}
	return frame, nil
}

// AcceptRemote marks that local OPEN authorization has succeeded and OPEN_OK
// may now be sent. Buffered TCP bytes remain bounded until the caller drains.
func (t *StreamTable) AcceptRemote(id uint32) error {
	return t.acceptRemote(id, nil)
}

// acceptRemote publishes pending frames before allowing new DATA to bypass
// the opening queue. enqueue must be nonblocking and must not reenter t.
func (t *StreamTable) acceptRemote(id uint32, enqueue func(Frame) error) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	stream := t.streams[id]
	if stream == nil || stream.InitiatedLocal || stream.Lifecycle != StreamOpening {
		return &StreamViolation{StreamID: id, Cause: ErrStreamState}
	}
	if enqueue != nil {
		for _, frame := range stream.pendingFrames {
			if err := enqueue(frame); err != nil {
				return err
			}
		}
		stream.pendingFrames = nil
		stream.PendingOpenBytes = 0
	}
	stream.OpenOK = true
	if stream.RemoteFIN {
		stream.Lifecycle = StreamRemoteHalfClosed
	} else {
		stream.Lifecycle = StreamOpen
	}
	return nil
}

func (t *StreamTable) RemoteOpenRequest(id uint32) (OpenRequest, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	stream := t.streams[id]
	if stream == nil || stream.InitiatedLocal {
		return OpenRequest{}, false
	}
	return stream.openRequest, true
}

func (t *StreamTable) DrainPendingOpenFrames(id uint32) ([]Frame, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	stream := t.streams[id]
	if stream == nil || stream.InitiatedLocal || !stream.OpenOK {
		return nil, &StreamViolation{StreamID: id, Cause: ErrStreamState}
	}
	frames := make([]Frame, len(stream.pendingFrames))
	for i, frame := range stream.pendingFrames {
		frames[i] = frame
		frames[i].Payload = append([]byte(nil), frame.Payload...)
	}
	stream.pendingFrames = nil
	stream.PendingOpenBytes = 0
	return frames, nil
}

func (t *StreamTable) MarkLocalFIN(id uint32) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	stream := t.streams[id]
	if stream == nil || stream.Network != StreamTCP || stream.LocalFIN || stream.Lifecycle == StreamReset || stream.Lifecycle == StreamOpening && !stream.InitiatedLocal {
		return &StreamViolation{StreamID: id, Cause: ErrStreamState}
	}
	stream.LocalFIN = true
	if stream.Lifecycle == StreamOpening {
		return nil
	}
	if stream.RemoteFIN {
		stream.Lifecycle = StreamClosed
		delete(t.streams, id)
	} else {
		stream.Lifecycle = StreamLocalHalfClosed
	}
	return nil
}

func (t *StreamTable) MarkLocalData(id uint32, bytes int) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	stream := t.streams[id]
	if stream == nil || !stream.InitiatedLocal || stream.Network != StreamTCP || stream.Lifecycle != StreamOpening {
		// OPEN_OK may have won after reserveEarlyData's snapshot. Let the
		// caller recheck that state without deleting the authorized stream.
		return &StreamViolation{StreamID: id, Cause: ErrStreamState}
	}
	if bytes < 1 || bytes > t.maxPending-stream.LocalOpenBytes {
		delete(t.streams, id)
		return &StreamViolation{StreamID: id, Cause: ErrStreamState}
	}
	stream.LocalOpenBytes += bytes
	return nil
}

func (t *StreamTable) Snapshot(id uint32) (StreamSnapshot, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	stream, ok := t.streams[id]
	if !ok {
		return StreamSnapshot{}, false
	}
	return stream.StreamSnapshot, true
}
func (t *StreamTable) Count() int     { t.mu.Lock(); defer t.mu.Unlock(); return len(t.streams) }
func (t *StreamTable) Drop(id uint32) { t.mu.Lock(); delete(t.streams, id); t.mu.Unlock() }
func (t *StreamTable) CloseAll() []uint32 {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.closed = true
	ids := make([]uint32, 0, len(t.streams))
	for id := range t.streams {
		ids = append(ids, id)
		delete(t.streams, id)
	}
	return ids
}
