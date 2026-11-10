package vlink

import (
	"fmt"
	"sync"
)

// tcpReceiveQueue bounds payload bytes by the advertised stream window. Frame
// boundaries do not delimit TCP data: compact only a full queue so fragmented
// but in-window traffic never resets a slow consumer or blocks the link reader.
// Popped frames keep their own payload lease; compaction touches queued frames
// only. FIN and other control frames keep their order.
type tcpReceiveQueue struct {
	mu              sync.Mutex
	frames          []Frame
	head, count     int
	bytes, maxBytes uint64
	ready           chan struct{}
}

func newTCPReceiveQueue(window uint32, frameHint int) *tcpReceiveQueue {
	// Fully packed DATA plus boundary fragments and stream control frames fit
	// independently of how many small DATA frames the peer uses on the wire.
	slots := int((uint64(window)+MaxTLSRecordPayload-1)/MaxTLSRecordPayload) + 4
	if frameHint > slots {
		slots = frameHint
	}
	return &tcpReceiveQueue{frames: make([]Frame, slots), maxBytes: uint64(window), ready: make(chan struct{}, 1)}
}

func (q *tcpReceiveQueue) signal() {
	select {
	case q.ready <- struct{}{}:
	default:
	}
}

func (q *tcpReceiveQueue) push(frame Frame) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	if frame.Type == FrameData && uint64(len(frame.Payload)) > q.maxBytes-q.bytes {
		return fmt.Errorf("%w: TCP receive byte window exceeded", ErrStreamState)
	}
	if q.count == len(q.frames) {
		q.compact()
	}
	if q.count == len(q.frames) {
		return fmt.Errorf("%w: TCP receive control queue is full", ErrStreamState)
	}
	q.frames[(q.head+q.count)%len(q.frames)] = frame
	q.count++
	if frame.Type == FrameData {
		q.bytes += uint64(len(frame.Payload))
	}
	q.signal()
	return nil
}

func (q *tcpReceiveQueue) pop() (Frame, bool) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.count == 0 {
		return Frame{}, false
	}
	frame := q.frames[q.head]
	q.frames[q.head] = Frame{}
	q.head = (q.head + 1) % len(q.frames)
	q.count--
	if frame.Type == FrameData {
		q.bytes -= uint64(len(frame.Payload))
	}
	if q.count > 0 {
		q.signal()
	}
	return frame, true
}

// compact is called with mu held. Most full-sized traffic retains its original
// zero-copy leases; copying occurs only when small frames exhaust frame slots.
func (q *tcpReceiveQueue) compact() {
	packed := make([]Frame, len(q.frames))
	count := 0
	for i := 0; i < q.count; i++ {
		frame := q.frames[(q.head+i)%len(q.frames)]
		if count > 0 && frame.Type == FrameData {
			tail := &packed[count-1]
			if tail.Type == FrameData && len(tail.Payload) < MaxTLSRecordPayload {
				if tail.lease == nil || cap(tail.Payload) < MaxTLSRecordPayload {
					storage := muxReadBuffers.Get().(*[MaxPayloadSize]byte)
					copy(storage[:], tail.Payload)
					old := *tail
					tail.Payload = storage[:len(tail.Payload)]
					tail.lease = &framePayloadLease{storage: storage}
					old.Release()
				}
				n := len(frame.Payload)
				if space := MaxTLSRecordPayload - len(tail.Payload); n > space {
					n = space
				}
				end := len(tail.Payload)
				tail.Payload = tail.Payload[:end+n]
				copy(tail.Payload[end:], frame.Payload[:n])
				frame.Payload = frame.Payload[n:]
				if len(frame.Payload) == 0 {
					frame.Release()
					continue
				}
			}
		}
		packed[count] = frame
		count++
	}
	q.frames = packed
	q.head = 0
	q.count = count
}
