package vlink

import (
	"context"
	"errors"
	"fmt"
	"math"
	"sync"
)

var (
	ErrWindowClosed  = errors.New("vlink: byte window is closed")
	ErrUnknownStream = errors.New("vlink: byte window references unknown stream")
)

type WindowSnapshot struct {
	AggregateAvailable uint32
	StreamAvailable    map[uint32]uint32
}
type byteCredit struct{ available uint32 }

type WindowManager struct {
	mu               sync.Mutex
	aggregate        uint32
	aggregateReserve uint32
	max              uint32
	streams          map[uint32]*byteCredit
	changed          chan struct{}
	closed           bool
}

func NewWindowManager(aggregateInitial, maxCredit uint32) *WindowManager {
	return NewWindowManagerWithReserve(aggregateInitial, maxCredit, 0)
}

func NewWindowManagerWithReserve(aggregateInitial, maxCredit, aggregateReserve uint32) *WindowManager {
	if maxCredit == 0 {
		maxCredit = math.MaxUint32
	}
	if aggregateInitial > maxCredit {
		aggregateInitial = maxCredit
	}
	if aggregateReserve > aggregateInitial {
		aggregateReserve = aggregateInitial
	}
	return &WindowManager{aggregate: aggregateInitial, aggregateReserve: aggregateReserve, max: maxCredit, streams: make(map[uint32]*byteCredit), changed: make(chan struct{})}
}

func (w *WindowManager) AddStream(id, initial uint32) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return ErrWindowClosed
	}
	if id == 0 || initial > w.max {
		return fmt.Errorf("%w: invalid initial stream credit", ErrProtocol)
	}
	if _, exists := w.streams[id]; exists {
		return fmt.Errorf("%w: duplicate stream window", ErrProtocol)
	}
	w.streams[id] = &byteCredit{available: initial}
	w.broadcastLocked()
	return nil
}

func (w *WindowManager) Acquire(ctx context.Context, id, amount uint32) error {
	if amount == 0 {
		return nil
	}
	if amount > w.max {
		return fmt.Errorf("%w: requested credit exceeds hard bound", ErrProtocol)
	}
	for {
		w.mu.Lock()
		if w.closed {
			w.mu.Unlock()
			return ErrWindowClosed
		}
		stream := w.streams[id]
		if stream == nil {
			w.mu.Unlock()
			return ErrUnknownStream
		}
		if stream.available >= amount && w.aggregate >= amount && w.aggregate-amount >= w.aggregateReserve {
			stream.available -= amount
			w.aggregate -= amount
			w.mu.Unlock()
			return nil
		}
		changed := w.changed
		w.mu.Unlock()
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-changed:
		}
	}
}

// TryAcquire is the non-blocking path used by UDP; lack of credit is a drop,
// while a closed or unknown window remains a caller-visible state error.
func (w *WindowManager) TryAcquire(id, amount uint32) (bool, error) {
	ok, _, err := w.tryAcquire(id, amount)
	return ok, err
}

// windowRejection attributes an admission failure under the same lock as the
// credit check. Both can be true; these are diagnostic subsets, not extra drops.
type windowRejection struct{ stream, aggregate bool }

func (w *WindowManager) tryAcquire(id, amount uint32) (bool, windowRejection, error) {
	if amount == 0 {
		return true, windowRejection{}, nil
	}
	if amount > w.max {
		return false, windowRejection{}, fmt.Errorf("%w: requested credit exceeds hard bound", ErrProtocol)
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return false, windowRejection{}, ErrWindowClosed
	}
	stream := w.streams[id]
	if stream == nil {
		return false, windowRejection{}, ErrUnknownStream
	}
	if stream.available < amount || w.aggregate < amount {
		return false, windowRejection{stream: stream.available < amount, aggregate: w.aggregate < amount}, nil
	}
	stream.available -= amount
	w.aggregate -= amount
	return true, windowRejection{}, nil
}

func (w *WindowManager) Grant(streamID, delta uint32) error {
	if delta == 0 {
		return fmt.Errorf("%w: WINDOW delta must be positive", ErrProtocol)
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return ErrWindowClosed
	}
	if streamID == 0 {
		if uint64(w.aggregate)+uint64(delta) > uint64(w.max) {
			return fmt.Errorf("%w: aggregate window overflow", ErrProtocol)
		}
		w.aggregate += delta
	} else {
		stream := w.streams[streamID]
		if stream == nil {
			return ErrUnknownStream
		}
		if uint64(stream.available)+uint64(delta) > uint64(w.max) {
			return fmt.Errorf("%w: stream window overflow", ErrProtocol)
		}
		stream.available += delta
	}
	w.broadcastLocked()
	return nil
}

func (w *WindowManager) RemoveStream(streamID uint32) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if _, exists := w.streams[streamID]; exists {
		delete(w.streams, streamID)
		w.broadcastLocked()
	}
}

func (w *WindowManager) Snapshot() WindowSnapshot {
	w.mu.Lock()
	defer w.mu.Unlock()
	snapshot := WindowSnapshot{AggregateAvailable: w.aggregate, StreamAvailable: make(map[uint32]uint32, len(w.streams))}
	for id, stream := range w.streams {
		snapshot.StreamAvailable[id] = stream.available
	}
	return snapshot
}

func (w *WindowManager) Close() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if !w.closed {
		w.closed = true
		w.broadcastLocked()
	}
}

func (w *WindowManager) broadcastLocked() { close(w.changed); w.changed = make(chan struct{}) }
