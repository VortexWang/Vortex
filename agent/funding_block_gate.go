package agent

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"vortex/agent/store"
	accounting "vortex/engine/accounting"
)

// NewExperimentalFundingBlockGate explicitly opts into durable block funding.
// The default runtime gate remains per-write. Checkpoints currently flush every
// completed write; rate admission also remains durable, so this is not a
// performance-qualified mode. Engine still receives only accounting interfaces.
func (n *Node) NewExperimentalFundingBlockGate(scope RuntimeScope, clock AuthorizationClock, blockBytes uint64) (accounting.Gate, error) {
	if n == nil || n.Store == nil || clock == nil || blockBytes == 0 || blockBytes > 4<<20 || scope.RuleID == "" || scope.RuleRevision == 0 || scope.PathGeneration == 0 {
		return nil, fmt.Errorf("agent: invalid experimental funding block configuration")
	}
	return &fundingBlockGate{inner: &runtimeAuthorityGate{node: n, scope: scope, clock: clock}, size: blockBytes}, nil
}

type fundingBlockGate struct {
	inner *runtimeAuthorityGate
	size  uint64
}

func (g *fundingBlockGate) Open(ctx context.Context, flow accounting.Flow) (accounting.Session, error) {
	inner, err := g.inner.Open(ctx, flow)
	if err != nil {
		return nil, err
	}
	return &fundingBlockSession{inner: inner.(*authorizationSession), size: g.size, blocks: make(map[accounting.Direction]*sessionFundingBlock), changed: make(chan struct{}), idleTimeout: time.Second}, nil
}

type sessionFundingBlock struct {
	reservation    store.UsageReservation
	inFlight       uint64
	poisoned       bool
	idleTimer      *time.Timer
	idleGeneration uint64
}
type fundingBlockSession struct {
	mu          fundingContextMutex
	inner       *authorizationSession
	size        uint64
	blocks      map[accounting.Direction]*sessionFundingBlock
	changed     chan struct{}
	closed      bool
	idleTimeout time.Duration
}

func (s *fundingBlockSession) signal() { close(s.changed); s.changed = make(chan struct{}) }
func (s *fundingBlockSession) Reserve(ctx context.Context, direction accounting.Direction, n int64) (result *accounting.Batch, resultErr error) {
	started := time.Now()
	defer func() {
		if resultErr != nil {
			slog.Warn("funding block admission failed", "direction", direction, "requested_bytes", n, "elapsed", time.Since(started), "error", resultErr)
		}
	}()
	for {
		type attemptResult struct {
			batch *accounting.Batch
			wait  <-chan struct{}
		}
		config := s.inner.gate.config
		result, err := withAuthorityWait(ctx, s.inner.gate.node, RuntimeScope{RuleID: config.RuleID, RuleRevision: config.RuleRevision, PathGeneration: config.PathGeneration}, func(attemptCtx context.Context) (attemptResult, error) {
			batch, wait, err := s.reserve(attemptCtx, direction, n)
			if errors.Is(err, errRefreshFundingBlock) {
				batch, wait, err = s.reserve(attemptCtx, direction, n)
			}
			return attemptResult{batch, wait}, err
		})
		batch, wait := result.batch, result.wait
		if wait != nil {
			select {
			case <-wait:
				continue
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}
		var limited *store.RateLimited
		if errors.As(err, &limited) {
			timer := time.NewTimer(limited.RetryAfter)
			select {
			case <-timer.C:
				continue
			case <-ctx.Done():
				timer.Stop()
				return nil, ctx.Err()
			}
		}
		return batch, err
	}
}
func (s *fundingBlockSession) reserve(ctx context.Context, direction accounting.Direction, n int64) (*accounting.Batch, <-chan struct{}, error) {
	if err := s.mu.LockContext(ctx); err != nil {
		return nil, nil, err
	}
	defer s.mu.Unlock()
	if s.closed || s.inner.gate.isFaulted() || n <= 0 || uint64(n) > s.size {
		return nil, nil, accounting.ErrDenied
	}
	dir, err := usageDirection(direction)
	if err != nil {
		return nil, nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	b := s.blocks[direction]
	existing := b != nil
	if b != nil && b.reservation.Reserved-b.inFlight < uint64(n) {
		if b.inFlight > 0 {
			return nil, s.changed, nil
		}
		if err := s.release(ctx, b); err != nil {
			return nil, nil, s.fail(err)
		}
		delete(s.blocks, direction)
		b = nil
		existing = false
	}
	early, late, trusted := s.inner.gate.config.Clock()
	use := s.inner.gate.use(early, late, trusted, true)
	if b == nil {
		id, err := authorizationBatchID()
		if err != nil {
			return nil, nil, s.fail(err)
		}
		r := store.UsageReservation{NodeID: s.inner.gate.node.NodeID, BootID: s.inner.gate.node.BootID, BatchID: id, FlowKey: s.inner.flowKey, RuleID: use.RuleID, SnapshotID: use.BillingSnapshotID, Direction: dir, Reserved: s.size}
		var idle []store.UsageReservation
		var idleDirection accounting.Direction
		for otherDirection, other := range s.blocks {
			if otherDirection != direction && other.inFlight == 0 && !other.poisoned {
				idle = append(idle, other.reservation)
				idleDirection = otherDirection
			}
		}
		funded, err := s.inner.gate.node.Store.ReserveAndAdmitRuntimeFundingBlock(ctx, r, use, uint64(n), idle)
		if errors.Is(err, store.ErrAuthorizationDenied) {
			// A UDP response can arrive before the outbound write's commit
			// callback takes the mutex. Wait for that known in-flight bound
			// to resolve before deciding that idle-tail reclamation is impossible.
			for otherDirection, other := range s.blocks {
				if otherDirection != direction && other.inFlight > 0 && !other.poisoned {
					return nil, s.changed, nil
				}
			}
		}
		if err != nil {
			return nil, nil, s.admissionError(err)
		}
		if len(idle) > 0 {
			s.stopIdle(s.blocks[idleDirection])
			delete(s.blocks, idleDirection)
		}
		b = &sessionFundingBlock{reservation: funded}
		s.blocks[direction] = b
		s.armIdle(direction, b)
	}
	if existing {
		if err := s.inner.gate.node.Store.AdmitRuntimeFundingBlockWrite(ctx, b.reservation, use, uint64(n)); err != nil {
			if existing && b.inFlight == 0 && errors.Is(err, store.ErrAuthorizationDenied) {
				if releaseErr := s.release(ctx, b); releaseErr != nil {
					return nil, nil, s.fail(releaseErr)
				}
				s.stopIdle(b)
				delete(s.blocks, direction)
				return nil, nil, errRefreshFundingBlock
			}
			return nil, nil, s.admissionError(err)
		}
	}
	s.stopIdle(b)
	b.inFlight += uint64(n)
	batch, err := accounting.NewBatch(n, func(ctx context.Context, out accounting.Outcome) error { return s.resolve(ctx, direction, b, out) })
	return batch, nil, err
}

var errRefreshFundingBlock = errors.New("agent: refresh idle funding block")

func (s *fundingBlockSession) stopIdle(b *sessionFundingBlock) {
	b.idleGeneration++
	if b.idleTimer != nil {
		b.idleTimer.Stop()
		b.idleTimer = nil
	}
}
func (s *fundingBlockSession) armIdle(direction accounting.Direction, b *sessionFundingBlock) {
	s.stopIdle(b)
	if s.closed || b.poisoned || b.inFlight != 0 {
		return
	}
	generation := b.idleGeneration
	b.idleTimer = time.AfterFunc(s.idleTimeout, func() {
		s.mu.Lock()
		defer s.mu.Unlock()
		if s.closed || b.poisoned || b.inFlight != 0 || b.idleGeneration != generation || s.blocks[direction] != b {
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if err := s.release(ctx, b); err != nil {
			b.poisoned = true
			s.fail(err)
			return
		}
		delete(s.blocks, direction)
		s.signal()
	})
}
func (s *fundingBlockSession) admissionError(err error) error {
	var limited *store.RateLimited
	if !errors.Is(err, store.ErrAuthorizationDenied) && !errors.Is(err, store.ErrAuthorityTimeUncertain) && !errors.As(err, &limited) {
		s.fail(err)
	}
	return err
}
func (s *fundingBlockSession) fail(err error) error { s.inner.gate.fail(); s.signal(); return err }
func (s *fundingBlockSession) release(ctx context.Context, b *sessionFundingBlock) error {
	s.stopIdle(b)
	if b.poisoned || b.reservation.Reserved == 0 {
		return nil
	}
	return s.inner.gate.node.Store.SettleUsageReservation(ctx, b.reservation, "returned", 0, nil, 0)
}
func (s *fundingBlockSession) resolve(ctx context.Context, direction accounting.Direction, b *sessionFundingBlock, out accounting.Outcome) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	defer s.signal()
	b.inFlight -= uint64(out.Reserved)
	if b.poisoned {
		return accounting.ErrDenied
	}
	node := s.inner.gate.node
	if out.Resolution == accounting.Unknown {
		b.poisoned = true
		s.inner.gate.fail()
		record := accounting.UsageRecord{BatchID: b.reservation.BatchID, FlowKey: b.reservation.FlowKey, RuleID: b.reservation.RuleID, SnapshotID: b.reservation.SnapshotID, AuthorizationID: b.reservation.AuthorizationID, Direction: direction, Reserved: int64(b.reservation.Reserved), Used: int64(b.reservation.Reserved), Resolution: accounting.Unknown}
		return node.SettleUsage(ctx, record)
	}
	if out.Resolution == accounting.Committed && out.Used > 0 {
		id, err := authorizationBatchID()
		if err != nil {
			b.poisoned = true
			return s.fail(err)
		}
		record := accounting.UsageRecord{BatchID: id, RuleID: b.reservation.RuleID, SnapshotID: b.reservation.SnapshotID, AuthorizationID: b.reservation.AuthorizationID}
		payload, err := marshalUsage(record, b.reservation.Direction, uint64(out.Used))
		if err == nil {
			err = node.Store.CheckpointRuntimeFundingBlock(ctx, b.reservation, id, uint64(out.Used), payload)
		}
		if err != nil {
			b.poisoned = true
			return s.fail(err)
		}
		b.reservation.Reserved -= uint64(out.Used)
	}
	if b.reservation.Reserved == 0 {
		delete(s.blocks, direction)
	} else if s.closed && b.inFlight == 0 {
		if err := s.release(ctx, b); err != nil {
			b.poisoned = true
			return s.fail(err)
		}
		delete(s.blocks, direction)
	}
	if b.inFlight == 0 && s.blocks[direction] == b {
		s.armIdle(direction, b)
	}
	return nil
}
func (s *fundingBlockSession) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil
	}
	s.closed = true
	s.signal()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	var result error
	for direction, b := range s.blocks {
		s.stopIdle(b)
		if b.inFlight == 0 {
			if err := s.release(ctx, b); err != nil {
				b.poisoned = true
				result = errors.Join(result, s.fail(err))
			} else {
				delete(s.blocks, direction)
			}
		}
	}
	// Active child batches retain their durable upper bounds. Their final
	// callbacks release tails only after every writer has resolved.
	return errors.Join(result, s.inner.Close())
}

// A queued admission must honor its original deadline before it owns the
// allocator. Settlement and close retain blocking ownership to preserve bounds.
type fundingContextMutex struct {
	once  sync.Once
	token chan struct{}
}

func (m *fundingContextMutex) LockContext(ctx context.Context) error {
	m.once.Do(func() { m.token = make(chan struct{}, 1) })
	if err := ctx.Err(); err != nil {
		return err
	}
	select {
	case m.token <- struct{}{}:
		if err := ctx.Err(); err != nil {
			<-m.token
			return err
		}
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
func (m *fundingContextMutex) Lock()   { _ = m.LockContext(context.Background()) }
func (m *fundingContextMutex) Unlock() { <-m.token }
