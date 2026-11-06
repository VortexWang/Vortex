package accounting

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"
	core "vortex/engine/accounting"
)

// DelayedCreditGate is an explicit process-local qualification issuer. It adds
// measured response delay, not a network or durable authority. Caller timeout
// does not prove non-issuance; the delayed reply can still settle its request.
type DelayedCreditGate struct {
	Credit  *CreditGate
	delay   time.Duration
	ctx     context.Context
	cancel  context.CancelFunc
	mu      sync.Mutex
	pending string
	changed chan struct{}
	serial  uint64
	workers sync.WaitGroup
	closed  bool
	waiters chan struct{}
}

func NewDelayedCreditGate(parent context.Context, c CreditConfig, delay time.Duration) (*DelayedCreditGate, error) {
	if delay <= 0 || delay > 5*time.Second {
		return nil, fmt.Errorf("credit: diagnostic issuer delay must be within (0,5s]")
	}
	credit, err := NewInjectedCreditGate(c)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithCancel(parent)
	return &DelayedCreditGate{Credit: credit, delay: delay, ctx: ctx, cancel: cancel, changed: make(chan struct{}), waiters: make(chan struct{}, 128)}, nil
}
func (g *DelayedCreditGate) request() (chan struct{}, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.closed || g.ctx.Err() != nil {
		return nil, core.ErrDenied
	}
	if g.pending != "" {
		return g.changed, nil
	}
	g.serial++
	id, n, err := g.Credit.BeginRenewal(g.ctx, fmt.Sprintf("delayed:%d", g.serial))
	if err != nil {
		return nil, err
	}
	if n == 0 {
		return nil, nil
	}
	g.pending = id
	changed := g.changed
	g.workers.Add(1)
	go func() {
		defer g.workers.Done()
		timer := time.NewTimer(g.delay)
		defer timer.Stop()
		outcome := GrantAccepted
		select {
		case <-timer.C:
		case <-g.ctx.Done():
			outcome = GrantUnknown
		}
		reply := GrantReply{RequestID: id, SnapshotID: g.Credit.cfg.SnapshotID, Amount: n, Outcome: outcome}
		if outcome == GrantAccepted {
			reply.GrantID = "grant:" + id
		}
		err := g.Credit.ApplyGrant(context.Background(), reply)
		g.mu.Lock()
		if err != nil {
			g.closed = true
		}
		g.pending = ""
		close(changed)
		g.changed = make(chan struct{})
		g.mu.Unlock()
	}()
	return changed, nil
}
func (g *DelayedCreditGate) Close() {
	g.mu.Lock()
	g.closed = true
	g.cancel()
	g.mu.Unlock()
	g.workers.Wait()
}
func (g *DelayedCreditGate) Open(ctx context.Context, f core.Flow) (core.Session, error) {
	if _, err := g.request(); err != nil {
		return nil, err
	}
	s, err := g.Credit.Open(ctx, f)
	if err != nil {
		return nil, err
	}
	return &delayedSession{gate: g, inner: s}, nil
}

type delayedSession struct {
	gate  *DelayedCreditGate
	inner core.Session
}

func (s *delayedSession) Close() error { return s.inner.Close() }
func (s *delayedSession) Reserve(ctx context.Context, d core.Direction, n int64) (*core.Batch, error) {
	if s.gate.ctx.Err() != nil {
		return nil, core.ErrDenied
	}
	// One deadline for the whole attempt; never reset on a wakeup.
	attempt, cancel := context.WithTimeout(ctx, 200*time.Millisecond)
	defer cancel()
	b, err := s.inner.Reserve(attempt, d, n)
	if err == nil {
		_, _ = s.gate.request()
		return b, nil
	}
	if !errors.Is(err, core.ErrDenied) {
		return nil, err
	}
	if attempt.Err() != nil {
		return nil, attempt.Err()
	}
	select {
	case s.gate.waiters <- struct{}{}:
		defer func() { <-s.gate.waiters }()
	default:
		return nil, core.ErrDenied
	}
	changed, requestErr := s.gate.request()
	if requestErr != nil {
		return nil, requestErr
	}
	if changed == nil {
		return nil, err
	}
	select {
	case <-attempt.Done():
		return nil, attempt.Err()
	case <-changed:
	}
	b, err = s.inner.Reserve(attempt, d, n)
	if err == nil {
		_, _ = s.gate.request()
	}
	return b, err
}
