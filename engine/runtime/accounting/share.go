package accounting

import (
	"context"
	"sync"
	"time"

	core "vortex/engine/accounting"
	"vortex/engine/share"
)

// ShareGate injects process-local connection-share ownership around another
// gate. It does not enforce the rate component of a share revision.
type ShareGate struct {
	Manager *share.Manager
	Key     share.Key
	Inner   core.Gate
	// LeaseUntil is an optional conservative deadline. A zero value preserves
	// the process-local test adapter behavior; configured runtime shares must
	// provide a future deadline before admission or further reservations.
	LeaseUntil time.Time
	now        func() time.Time
}

func (g *ShareGate) currentTime() time.Time {
	if g.now != nil {
		return g.now()
	}
	return time.Now()
}

func (g *ShareGate) Open(ctx context.Context, flow core.Flow) (core.Session, error) {
	if g == nil || g.Manager == nil || g.Inner == nil {
		return nil, core.ErrDenied
	}
	if !g.LeaseUntil.IsZero() && !g.currentTime().Before(g.LeaseUntil) {
		return nil, core.ErrDenied
	}
	lease, err := g.Manager.Acquire(g.Key)
	if err != nil {
		return nil, err
	}
	inner, err := g.Inner.Open(ctx, flow)
	if err != nil {
		lease.Close()
		return nil, err
	}
	if !g.LeaseUntil.IsZero() && !g.currentTime().Before(g.LeaseUntil) {
		_ = inner.Close()
		lease.Close()
		return nil, core.ErrDenied
	}
	return &shareSession{inner: inner, lease: lease, leaseUntil: g.LeaseUntil, now: g.currentTime}, nil
}

type shareSession struct {
	mu         sync.Mutex
	inner      core.Session
	lease      *share.Lease
	leaseUntil time.Time
	now        func() time.Time
	closed     bool
	closeErr   error
}

func (s *shareSession) Reserve(ctx context.Context, d core.Direction, n int64) (*core.Batch, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed || (!s.leaseUntil.IsZero() && !s.now().Before(s.leaseUntil)) {
		return nil, core.ErrDenied
	}
	batch, err := s.inner.Reserve(ctx, d, n)
	if err != nil {
		return nil, err
	}
	// A policy wait inside the wrapped gate may have crossed the lease end.
	if !s.leaseUntil.IsZero() && !s.now().Before(s.leaseUntil) {
		if err := batch.Return(ctx); err != nil {
			return nil, err
		}
		return nil, core.ErrDenied
	}
	return batch, nil
}
func (s *shareSession) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.closed {
		s.closed = true
		s.closeErr = s.inner.Close()
		s.lease.Close()
	}
	return s.closeErr
}
