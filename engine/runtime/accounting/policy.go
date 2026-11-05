package accounting

import (
	"context"
	"errors"
	"sync"
	"time"

	core "vortex/engine/accounting"
)

var (
	ErrPolicyPaused    = errors.New("credit: policy boundary temporarily pauses writes")
	ErrPolicyExpired   = errors.New("credit: policy hard deadline reached")
	ErrPolicyUncertain = errors.New("credit: policy clock uncertainty exceeded recovery window")
)

type TimeBounds struct {
	Earliest time.Time
	Latest   time.Time
}

// PolicyClock is injected by the caller; Engine does not select a time source.
type PolicyClock interface {
	Bounds() TimeBounds
	Wait(context.Context, time.Duration) error
}

type PolicyWindow struct {
	OldSnapshot       string
	NewSnapshot       string
	At                time.Time
	PreparedAt        time.Time
	ExpiresAt         time.Time
	PolicyLeaseUntil  time.Time
	RuntimeLeaseUntil time.Time
	ShareLeaseUntil   time.Time
	HardDeadline      time.Time
	SelectionRevision uint64
}

func (w PolicyWindow) Validate() error {
	if w.OldSnapshot == "" || w.NewSnapshot == "" || w.OldSnapshot == w.NewSnapshot || w.At.IsZero() || w.PreparedAt.IsZero() || w.ExpiresAt.IsZero() || w.PolicyLeaseUntil.IsZero() || w.RuntimeLeaseUntil.IsZero() || w.ShareLeaseUntil.IsZero() || w.HardDeadline.IsZero() || w.SelectionRevision == 0 {
		return errors.New("credit: incomplete policy window")
	}
	if w.PreparedAt.After(w.At.Add(-60*time.Second)) || w.ExpiresAt.Before(w.PreparedAt.Add(300*time.Second)) {
		return errors.New("credit: next snapshot was not pre-issued for the required window")
	}
	if w.PolicyLeaseUntil.Before(w.At) || w.RuntimeLeaseUntil.Before(w.At) || w.ShareLeaseUntil.Before(w.At) || !w.HardDeadline.After(w.At) {
		return errors.New("credit: policy/runtime/share lease does not cover boundary")
	}
	return nil
}

type PolicyBoundary struct {
	mu        sync.Mutex
	book      *CreditBook
	clock     PolicyClock
	window    PolicyWindow
	pausedAt  time.Time
	newPolicy uint64
}

func NewPolicyBoundary(book *CreditBook, clock PolicyClock, window PolicyWindow) (*PolicyBoundary, error) {
	if book == nil || clock == nil || window.Validate() != nil {
		return nil, errors.New("credit: invalid policy boundary")
	}
	state := book.State()
	if state.Active != window.OldSnapshot {
		return nil, core.ErrSnapshotMismatch
	}
	if _, ok := state.Snapshots[window.NewSnapshot]; !ok {
		return nil, core.ErrSnapshotMismatch
	}
	oldPolicy := state.Snapshots[window.OldSnapshot].Binding.PolicyRevision
	newPolicy := state.Snapshots[window.NewSnapshot].Binding.PolicyRevision
	if newPolicy <= oldPolicy {
		return nil, core.ErrSnapshotMismatch
	}
	return &PolicyBoundary{book: book, clock: clock, window: window, newPolicy: newPolicy}, nil
}

func (p *PolicyBoundary) decision(now TimeBounds) (string, bool, error) {
	if now.Earliest.IsZero() || now.Latest.IsZero() || now.Earliest.After(now.Latest) {
		return "", false, ErrPolicyUncertain
	}
	if !now.Latest.Before(p.window.HardDeadline) {
		return "", false, ErrPolicyExpired
	}
	uncertainty := now.Latest.Sub(now.Earliest)
	if now.Earliest.Before(p.window.At) && !now.Latest.Before(p.window.At) {
		if uncertainty > 10*time.Second {
			return "", true, ErrPolicyUncertain
		}
		return "", true, nil
	}
	if now.Latest.Before(p.window.At) {
		return p.window.OldSnapshot, false, nil
	}
	return p.window.NewSnapshot, false, nil
}

func (p *PolicyBoundary) Open(ctx context.Context, f core.Flow) (core.Session, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	id, paused, err := p.decision(p.clock.Bounds())
	if err != nil {
		return nil, err
	}
	if paused {
		return nil, ErrPolicyPaused
	}
	if id == p.window.NewSnapshot {
		if err := p.book.selectPolicy(p.newPolicy, p.window.SelectionRevision); err != nil {
			return nil, err
		}
	}
	session, snapshot, binding, err := p.book.openCurrent(ctx, f)
	if err != nil {
		return nil, err
	}
	return &policySession{owner: p, inner: session, flow: f, snapshot: snapshot, binding: binding}, nil
}

type policySession struct {
	mu       sync.Mutex
	flow     core.Flow
	snapshot string
	binding  Binding
	closed   bool
	owner    *PolicyBoundary
	inner    core.Session
}

func (s *policySession) Reserve(ctx context.Context, direction core.Direction, n int64) (*core.Batch, error) {
	for {
		s.mu.Lock()
		closed := s.closed
		s.mu.Unlock()
		if closed {
			return nil, core.ErrDenied
		}
		s.owner.mu.Lock()
		bounds := s.owner.clock.Bounds()
		id, paused, err := s.owner.decision(bounds)
		if err == nil && !paused && id == s.owner.window.NewSnapshot {
			if err = s.owner.book.selectPolicy(s.owner.newPolicy, s.owner.window.SelectionRevision); err == nil {
				s.owner.pausedAt = time.Time{}
			}
		}
		if paused && err == nil {
			if s.owner.pausedAt.IsZero() {
				s.owner.pausedAt = bounds.Latest
			}
			if bounds.Latest.Sub(s.owner.pausedAt) > 12*time.Second {
				err = ErrPolicyUncertain
			}
		}
		s.owner.mu.Unlock()
		if err != nil {
			return nil, err
		}
		if !paused {
			s.mu.Lock()
			defer s.mu.Unlock()
			if s.closed {
				return nil, core.ErrDenied
			}
			if id == s.owner.window.NewSnapshot && s.binding.PolicyRevision != s.owner.newPolicy {
				next, snapshot, err := s.owner.book.openPolicy(ctx, s.flow, s.binding, s.owner.newPolicy)
				if err != nil {
					return nil, err
				}
				// Outstanding batches retain their original resolver and liability.
				_ = s.inner.Close()
				s.inner, s.snapshot = next, snapshot
				s.binding.PolicyRevision = s.owner.newPolicy
			}
			return s.inner.Reserve(ctx, direction, n)
		}
		if err := s.owner.clock.Wait(ctx, 10*time.Millisecond); err != nil {
			return nil, err
		}
	}
}

func (s *policySession) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil
	}
	s.closed = true
	return s.inner.Close()
}

type SystemPolicyClock struct{ Uncertainty time.Duration }

func (c SystemPolicyClock) Bounds() TimeBounds {
	now := time.Now()
	return TimeBounds{Earliest: now.Add(-c.Uncertainty), Latest: now.Add(c.Uncertainty)}
}
func (SystemPolicyClock) Wait(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
