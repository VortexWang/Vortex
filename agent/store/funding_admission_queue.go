package store

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"
)

type fundingAdmissionRequest struct {
	ctx    context.Context
	size   int
	run    func(*runtimeTransaction) error
	result chan error
}

// The Store owns the single writer across all runtime sessions. Group only
// transaction commits here; every block retains its own authority/rate checks
// and the caller retains exclusive ownership until its result is known.
type fundingAdmissionQueue struct {
	mu      sync.Mutex
	pending []*fundingAdmissionRequest
	count   int
	running bool
}

func (s *Store) enqueueFundingAdmissions(ctx context.Context, writes []RuntimeBlockAdmission, clocks ...func() (time.Time, time.Time, bool)) error {
	return s.enqueueFundingOperation(ctx, len(writes), func(tx *runtimeTransaction) error {
		var low, high time.Time
		var trusted bool
		if len(clocks) == 1 {
			low, high, trusted = clocks[0]()
		}
		for _, write := range writes {
			if len(clocks) == 1 {
				write.Scope.Earliest, write.Scope.Latest, write.Scope.TrustedTime = low, high, trusted
			}
			if err := admitRuntimeFundingBlockWriteTx(ctx, tx, write.Block, write.Scope, write.Bytes); err != nil {
				return err
			}
		}
		return nil
	})
}

func (s *Store) enqueueFundingOperation(ctx context.Context, size int, run func(*runtimeTransaction) error) error {
	return s.enqueueBatchedOperation(ctx, size, run, &s.fundingAdmissions)
}

func (s *Store) enqueueBatchedOperation(ctx context.Context, size int, run func(*runtimeTransaction) error, q *fundingAdmissionQueue) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	q.mu.Lock()
	if q.count >= 64 {
		q.mu.Unlock()
		return ErrAuthorizationDenied
	}
	r := &fundingAdmissionRequest{ctx: ctx, size: size, run: run, result: make(chan error, 1)}
	q.pending = append(q.pending, r)
	q.count++
	if !q.running {
		q.running = true
		go s.drainBatchedOperations(q)
	}
	q.mu.Unlock()
	// A cancellation cannot abandon a transaction whose commit might have won.
	// The caller must classify the actual result before issuing any capability.
	return <-r.result
}

func (s *Store) drainFundingAdmissions() { s.drainBatchedOperations(&s.fundingAdmissions) }

func (s *Store) drainBatchedOperations(q *fundingAdmissionQueue) {
	timer := time.NewTimer(time.Millisecond)
	<-timer.C
	for {
		q.mu.Lock()
		if len(q.pending) == 0 {
			q.running = false
			q.mu.Unlock()
			return
		}
		first := q.pending[0]
		q.mu.Unlock()
		// Leave requests attached while waiting. New arrivals can join this
		// commit once the writer is ours instead of queuing a second sync.
		release, err := s.runtimeQueue.acquire(first.ctx)
		if err != nil {
			q.mu.Lock()
			q.pending = q.pending[1:]
			q.count--
			q.mu.Unlock()
			first.result <- err
			continue
		}
		q.mu.Lock()
		count, size := 0, 0
		for _, r := range q.pending {
			if size+r.size > 64 {
				break
			}
			size += r.size
			count++
		}
		group := append([]*fundingAdmissionRequest(nil), q.pending[:count]...)
		q.pending = q.pending[count:]
		q.mu.Unlock()
		errs := s.commitFundingAdmissionRequests(group, release)
		q.mu.Lock()
		q.count -= count
		q.mu.Unlock()
		for i, r := range group {
			r.result <- errs[i]
		}
	}
}

func (s *Store) commitFundingAdmissionRequests(group []*fundingAdmissionRequest, preowned ...func()) (results []error) {
	if len(preowned) > 0 {
		// Also release when member validation fails before BeginTx. Runtime
		// queue releases are idempotent, so tx.finish may safely call it too.
		defer preowned[0]()
	}
	started := time.Now()
	defer func() {
		if elapsed := time.Since(started); elapsed >= 100*time.Millisecond {
			slog.Warn("funding admission group slow", "members", len(group), "elapsed", elapsed, "results", results)
		}
	}()
	results = make([]error, len(group))
	failAll := func(err error) []error {
		for i := range results {
			results[i] = err
		}
		return results
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var callbacks sync.WaitGroup
	var stops []func() bool
	defer func() {
		for _, stop := range stops {
			if stop() {
				callbacks.Done()
			}
		}
		callbacks.Wait()
	}()
	for _, r := range group {
		if err := r.ctx.Err(); err != nil {
			return failAll(err)
		}
		callbacks.Add(1)
		stops = append(stops, context.AfterFunc(r.ctx, func() { cancel(); callbacks.Done() }))
	}
	checkMembers := func() error {
		// AfterFunc propagates cancellation asynchronously. Check each original
		// context synchronously after acquiring the writer and before commit.
		for _, r := range group {
			if err := r.ctx.Err(); err != nil {
				return err
			}
		}
		return nil
	}
	tx, err := s.beginRuntimeTx(ctx, nil, preowned...)
	if err != nil {
		return failAll(err)
	}
	defer tx.Rollback()
	tx.authorityReads = make(map[[2]string]authorityRead)
	if err := checkMembers(); err != nil {
		return failAll(err)
	}
	accepted := false
	for i, r := range group {
		if _, err := tx.ExecContext(ctx, "SAVEPOINT funding_member"); err != nil {
			return failAll(err)
		}
		err := r.run(tx)
		if err != nil {
			var limited *RateLimited
			if !errors.Is(err, ErrAuthorizationDenied) && !errors.Is(err, ErrAuthorityTimeUncertain) && !errors.As(err, &limited) {
				return failAll(err)
			}
			// A definitive denial affects only its own request. Roll back every
			// mutation from that request before considering the next caller.
			if _, rollbackErr := tx.ExecContext(ctx, "ROLLBACK TO funding_member"); rollbackErr != nil {
				return failAll(rollbackErr)
			}
			results[i] = err
		} else {
			accepted = true
		}
		if _, err := tx.ExecContext(ctx, "RELEASE funding_member"); err != nil {
			return failAll(err)
		}
	}
	if err := checkMembers(); err != nil {
		return failAll(err)
	}
	if !accepted {
		// Every member's mutations were rolled back to its savepoint. An
		// outer COMMIT can still sync pages dirtied by those speculative writes.
		// No capability is being issued, so finish with a complete rollback.
		if err := tx.Rollback(); err != nil {
			return failAll(err)
		}
		return results
	}
	if err := tx.Commit(); err != nil {
		return failAll(err)
	}
	return results
}
