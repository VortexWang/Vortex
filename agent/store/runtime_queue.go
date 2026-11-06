package store

import (
	"container/list"
	"context"
	"sync"
)

// Only one runtime transaction competes for the DB connection at a time.
// Control updates retain direct access to the pool and are not behind this queue.
type runtimeTransactionQueue struct {
	mu      sync.Mutex
	active  bool
	waiting list.List
}

type runtimeWaiter struct {
	ready   chan struct{}
	element *list.Element
}

func (q *runtimeTransactionQueue) acquire(ctx context.Context) (func(), error) {
	q.mu.Lock()
	if err := ctx.Err(); err != nil {
		q.mu.Unlock()
		return nil, err
	}
	if !q.active {
		q.active = true
		q.mu.Unlock()
		return q.releaseOnce(), nil
	}
	if q.waiting.Len() >= 4096 {
		q.mu.Unlock()
		return nil, ErrAuthorizationDenied
	}
	w := &runtimeWaiter{ready: make(chan struct{})}
	w.element = q.waiting.PushBack(w)
	q.mu.Unlock()
	select {
	case <-w.ready:
		if err := ctx.Err(); err != nil {
			q.release()
			return nil, err
		}
		return q.releaseOnce(), nil
	case <-ctx.Done():
		q.mu.Lock()
		granted := w.element == nil
		if !granted {
			q.waiting.Remove(w.element)
			w.element = nil
		}
		q.mu.Unlock()
		if granted {
			q.release()
		}
		return nil, ctx.Err()
	}
}

func (q *runtimeTransactionQueue) releaseOnce() func() {
	var once sync.Once
	return func() { once.Do(q.release) }
}

func (q *runtimeTransactionQueue) release() {
	q.mu.Lock()
	defer q.mu.Unlock()
	if first := q.waiting.Front(); first != nil {
		w := q.waiting.Remove(first).(*runtimeWaiter)
		w.element = nil
		close(w.ready)
	} else {
		q.active = false
	}
}
