package agent

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"

	"vortex/agent/store"
	accounting "vortex/engine/accounting"
)

type fundingAdmissionRequest struct {
	enqueued                                  time.Time
	queueWait, existingAttempt, directAttempt time.Duration
	existingStarted                           time.Time
	ctx                                       context.Context
	direction                                 accounting.Direction
	bytes                                     int64
	result                                    chan fundingAdmissionResult
}
type fundingAdmissionResult struct {
	batch *accounting.Batch
	err   error
}

// Reserve bounds queue occupancy and keeps the deadline anchored at enqueue.
// Successful admissions retain independent durable settlement callbacks.
func (s *fundingBlockSession) Reserve(ctx context.Context, direction accounting.Direction, n int64) (*accounting.Batch, error) {
	if n <= 0 || uint64(n) > s.size {
		return nil, accounting.ErrDenied
	}
	if _, err := usageDirection(direction); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, missingAuthorityWait)
	defer cancel()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	r := &fundingAdmissionRequest{enqueued: time.Now(), ctx: ctx, direction: direction, bytes: n, result: make(chan fundingAdmissionResult, 1)}
	s.queueMu.Lock()
	if s.queueCount >= 64 {
		s.queueMu.Unlock()
		return nil, accounting.ErrDenied
	}
	direct := s.queueCount == 0 && !s.queueRunning
	s.queueCount++
	if direct {
		s.queueMu.Unlock()
		defer func() { s.queueMu.Lock(); s.queueCount--; s.queueMu.Unlock() }()
		// Store now groups independent sessions. An uncontended session needs
		// no extra worker, timer or result handoff before entering that queue.
		batch, err := s.reserveDirect(ctx, direction, n)
		if ctx.Err() != nil && batch != nil {
			slog.Warn("funding admission committed after caller deadline", "elapsed", time.Since(r.enqueued), "direction", direction, "error", ctx.Err())
			cleanup, stop := context.WithTimeout(context.Background(), 2*time.Second)
			defer stop()
			if returnErr := batch.Return(cleanup); returnErr != nil {
				return nil, errors.Join(ctx.Err(), returnErr)
			}
			return nil, ctx.Err()
		}
		return batch, err
	}
	s.queue = append(s.queue, r)
	if !s.queueRunning {
		s.queueRunning = true
		go s.drainAdmissionQueue()
	}
	s.queueMu.Unlock()
	select {
	case result := <-r.result:
		return result.batch, result.err
	case <-ctx.Done():
		// If commit won the cancellation race, nobody received permission to write.
		// Return that child's unused allocation without refunding spent rate tokens.
		go func() {
			result := <-r.result
			if result.batch != nil {
				cleanup, stop := context.WithTimeout(context.Background(), 2*time.Second)
				defer stop()
				_ = result.batch.Return(cleanup)
			}
		}()
		return nil, ctx.Err()
	}
}
func (s *fundingBlockSession) deliverAdmission(r *fundingAdmissionRequest, b *accounting.Batch, err error) {
	if !r.existingStarted.IsZero() {
		r.existingAttempt += time.Since(r.existingStarted)
		r.existingStarted = time.Time{}
	}
	if err != nil {
		slog.Warn("funding admission timeline", "direction", r.direction, "requested_bytes", r.bytes,
			"total", time.Since(r.enqueued), "queue_wait", r.queueWait, "existing_attempt", r.existingAttempt,
			"direct_attempt", r.directAttempt, "error", err)
	}
	r.result <- fundingAdmissionResult{b, err}
	s.queueMu.Lock()
	s.queueCount--
	s.queueMu.Unlock()
}
func (s *fundingBlockSession) drainAdmissionQueue() {
	// A bounded coalescing window, included in every request's deadline.
	timer := time.NewTimer(time.Millisecond)
	<-timer.C
	for {
		s.queueMu.Lock()
		if len(s.queue) == 0 {
			s.queueRunning = false
			s.queueMu.Unlock()
			return
		}
		pending := s.queue
		s.queue = nil
		s.queueMu.Unlock()
		for len(pending) > 0 {
			first := pending[0]
			first.queueWait = time.Since(first.enqueued)
			if err := first.ctx.Err(); err != nil {
				s.deliverAdmission(first, nil, err)
				pending = pending[1:]
				continue
			}
			// Group existing funded capacity. Allocation/renewal still uses its atomic
			// first-write path, then subsequent queued requests can share admission.
			used, handled := s.admitQueuedExisting(pending)
			if handled {
				pending = pending[used:]
				continue
			}
			started := time.Now()
			b, err := s.reserveDirect(first.ctx, first.direction, first.bytes)
			first.directAttempt = time.Since(started)
			s.deliverAdmission(first, b, err)
			pending = pending[1:]
		}
	}
}
func (s *fundingBlockSession) admitQueuedExisting(pending []*fundingAdmissionRequest) (int, bool) {
	// A singleton cannot form a batch. Its direct path performs all required
	// session, authority and funding checks under the session mutex.
	if len(pending) < 2 {
		return 0, false
	}
	first := pending[0]
	first.existingStarted = time.Now()
	// Include session mutex wait even when no batch can be formed.
	defer func() {
		if !first.existingStarted.IsZero() {
			first.existingAttempt += time.Since(first.existingStarted)
			first.existingStarted = time.Time{}
		}
	}()
	if err := s.mu.LockContext(first.ctx); err != nil {
		s.deliverAdmission(first, nil, err)
		return 1, true
	}
	defer s.mu.Unlock()
	if s.closed || s.inner.gate.isFaulted() {
		s.deliverAdmission(first, nil, accounting.ErrDenied)
		return 1, true
	}
	block := s.blocks[first.direction]
	if block == nil || block.poisoned || block.checkpointDone != nil {
		return 0, false
	}
	available := block.reservation.Reserved - block.inFlight
	count := 0
	var total uint64
	deadline, _ := first.ctx.Deadline()
	for _, r := range pending {
		if r.direction != first.direction || r.ctx.Err() != nil || uint64(r.bytes) > available-total {
			break
		}
		total += uint64(r.bytes)
		count++
		end, _ := r.ctx.Deadline()
		if end.Before(deadline) {
			deadline = end
		}
	}
	if count < 2 {
		return 0, false
	}
	low, high, trusted := s.inner.gate.config.Clock()
	scope := s.inner.gate.use(low, high, trusted, true)
	writes := make([]store.RuntimeBlockAdmission, count)
	for i, r := range pending[:count] {
		writes[i] = store.RuntimeBlockAdmission{Block: block.reservation, Scope: scope, Bytes: uint64(r.bytes)}
	}
	ctx, cancel := context.WithDeadline(context.Background(), deadline)
	defer cancel()
	// Cancel the transaction when any grouped request is canceled. No member
	// receives a capability from a partially successful group.
	var callbacks sync.WaitGroup
	stops := make([]func() bool, 0, count)
	for _, r := range pending[:count] {
		callbacks.Add(1)
		stops = append(stops, context.AfterFunc(r.ctx, func() { cancel(); callbacks.Done() }))
	}
	err := s.inner.gate.node.Store.AdmitRuntimeFundingBlockBatch(ctx, writes, s.inner.gate.config.Clock)
	for _, stop := range stops {
		if stop() {
			callbacks.Done()
		}
	}
	callbacks.Wait()
	if err != nil {
		var limited *store.RateLimited
		if errors.Is(err, store.ErrAuthorizationDenied) || errors.Is(err, store.ErrAuthorityTimeUncertain) || errors.As(err, &limited) {
			return 0, false
		}
		// An ambiguous storage/commit failure must never trigger a fresh admission.
		s.fail(err)
		groupStarted := first.existingStarted
		for _, r := range pending[:count] {
			if r != first {
				r.queueWait = groupStarted.Sub(r.enqueued)
				r.existingStarted = groupStarted
			}
			s.deliverAdmission(r, nil, err)
		}
		return count, true
	}

	s.stopIdle(block)
	block.inFlight += total
	for _, r := range pending[:count] {
		b, err := accounting.NewBatch(r.bytes, func(ctx context.Context, out accounting.Outcome) error {
			return s.resolve(ctx, first.direction, block, out)
		})
		s.deliverAdmission(r, b, err)
	}
	return count, true
}
