package accounting

import (
	"context"
	"sync"

	core "vortex/engine/accounting"
	"vortex/engine/share"
)

// ShareGate injects process-local connection-share ownership around another
// gate. It does not enforce the rate component of a share revision.
type ShareGate struct {
	Manager *share.Manager
	Key     share.Key
	Inner   core.Gate
}

func (g *ShareGate) Open(ctx context.Context, flow core.Flow) (core.Session, error) {
	if g == nil || g.Manager == nil || g.Inner == nil {
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
	return &shareSession{inner: inner, lease: lease}, nil
}

type shareSession struct {
	mu       sync.Mutex
	inner    core.Session
	lease    *share.Lease
	closed   bool
	closeErr error
}

func (s *shareSession) Reserve(ctx context.Context, d core.Direction, n int64) (*core.Batch, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil, core.ErrDenied
	}
	return s.inner.Reserve(ctx, d, n)
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
