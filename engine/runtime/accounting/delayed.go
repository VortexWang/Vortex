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
	Credit         *CreditGate
	delay          time.Duration
	initialDelay   time.Duration
	renewalOutcome GrantOutcome
	issued         uint64
	ctx            context.Context
	cancel         context.CancelFunc
	mu             sync.Mutex
	pending        string
	changed        chan struct{}
	serial         uint64
	workers        sync.WaitGroup
	closed         bool
	waiters        chan struct{}
}

func NewDelayedCreditGate(parent context.Context, c CreditConfig, delay time.Duration) (*DelayedCreditGate, error) {
	if delay <= 0 || delay > 5*time.Second {
		return nil, fmt.Errorf("credit: diagnostic issuer delay must be within (0,5s]")
	}
	credit, err := NewInjectedCreditGate(c)
	if err != nil {
		return nil, err
	}
	credit.observeWaits = true
	ctx, cancel := context.WithCancel(parent)
	return &DelayedCreditGate{Credit: credit, delay: delay, initialDelay: delay, renewalOutcome: GrantAccepted, ctx: ctx, cancel: cancel, changed: make(chan struct{}), waiters: make(chan struct{}, 128)}, nil
}

// NewFaultedRenewalCreditGate is a qualification fault fixture: the first finite
// grant succeeds; later requests receive the selected trusted outcome. Unknown
// remains frozen; rejected is a definitive non-issuance response, not a timeout.
func NewFaultedRenewalCreditGate(parent context.Context, c CreditConfig, delay time.Duration, outcome GrantOutcome) (*DelayedCreditGate, error) {
	if outcome != GrantUnknown && outcome != GrantRejected {
		return nil, fmt.Errorf("credit: invalid diagnostic renewal outcome")
	}
	g, err := NewDelayedRenewalCreditGate(parent, c, delay)
	if err != nil {
		return nil, err
	}
	g.renewalOutcome = outcome
	return g, nil
}

// NewDelayedRenewalCreditGate supplies the first finite grant without injected
// delay, isolating subsequent renewal pressure from cold-start timeout.
// It is an experiment mode, not a change to the 200ms admission deadline.
func NewDelayedRenewalCreditGate(parent context.Context, c CreditConfig, delay time.Duration) (*DelayedCreditGate, error) {
	g, err := NewDelayedCreditGate(parent, c, delay)
	if err != nil {
		return nil, err
	}
	g.initialDelay = 0
	return g, nil
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
	delay := g.delay
	responseOutcome := g.renewalOutcome
	if g.issued == 0 {
		delay = g.initialDelay
		responseOutcome = GrantAccepted
	}
	g.issued++
	changed := g.changed
	g.workers.Add(1)
	go func() {
		defer g.workers.Done()
		timer := time.NewTimer(delay)
		defer timer.Stop()
		outcome := responseOutcome
		select {
		case <-timer.C:
		case <-g.ctx.Done():
			outcome = GrantUnknown
		}
		reply := GrantReply{RequestID: id, SnapshotID: g.Credit.cfg.SnapshotID, Amount: n, Outcome: outcome}
		if outcome == GrantAccepted {
			reply.GrantID = "grant:" + id
		}
		// Publish grant availability and completion under the same issuer lock.
		// A fast consumer must not observe credit while request() still returns
		// the completed initial request instead of starting its renewal.
		g.mu.Lock()
		err := g.Credit.ApplyGrant(context.Background(), reply)
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
	return &delayedSession{gate: g, inner: s.(*creditSession)}, nil
}

type delayedSession struct {
	gate  *DelayedCreditGate
	inner *creditSession
}

func (s *delayedSession) Close() error { return s.inner.Close() }
func (s *delayedSession) Reserve(ctx context.Context, d core.Direction, n int64) (result *core.Batch, resultErr error) {
	shortfall, waited := false, false
	defer func() {
		g := s.gate.Credit
		g.mu.Lock()
		defer g.mu.Unlock()
		g.waitStats.Attempts++
		if shortfall {
			g.waitStats.Shortfalls++
		}
		if waited {
			g.waitStats.Waits++
		}
		if waited && resultErr == nil {
			g.waitStats.WaitSucceeded++
		}
		if resultErr != nil {
			g.waitStats.FinalFailures++
			g.state.Denials++
			if errors.Is(resultErr, context.DeadlineExceeded) {
				g.waitStats.TimedOut++
			}
			if errors.Is(resultErr, context.Canceled) {
				g.waitStats.Cancelled++
			}
		}
	}()

	if s.gate.ctx.Err() != nil {
		return nil, core.ErrDenied
	}
	// One deadline for the whole attempt; never reset on a wakeup.
	attempt, cancel := context.WithTimeout(ctx, 200*time.Millisecond)
	defer cancel()
	b, err := s.inner.reserve(attempt, d, n, false)
	if err == nil {
		_, _ = s.gate.request()
		return b, nil
	}
	if !errors.Is(err, core.ErrDenied) {
		return nil, err
	}
	shortfall = true
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
	// A grant may arrive after the failed reserve but before request() checks
	// the waterline. Recheck even when another renewal is now pending: other
	// consumers may have lowered the balance while leaving enough for this write.
	// Only the real gate can authorize; the original deadline still applies.
	b, err = s.inner.reserve(attempt, d, n, false)
	if err == nil {
		_, _ = s.gate.request()
		return b, nil
	}
	if !errors.Is(err, core.ErrDenied) || changed == nil {
		return nil, err
	}
	waited = true
	select {
	case <-attempt.Done():
		return nil, attempt.Err()
	case <-changed:
	}
	b, err = s.inner.reserve(attempt, d, n, false)
	if err == nil {
		_, _ = s.gate.request()
	}
	return b, err
}
