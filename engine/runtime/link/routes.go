package link

import (
	"context"
	"errors"
	"log/slog"
	"reflect"
	"sync"
	"time"

	"vortex/engine/runtime/config"
	"vortex/engine/vlink"
)

// Routes activates only startup-authorized segments. References retain the
// previous transport for TCP drain; a generation's UDP context is cancelled.
type Routes struct {
	mu      sync.Mutex
	apply   sync.Mutex
	base    config.Config
	current *routeEntry
	entries map[uint64]*routeEntry
	ctx     context.Context
	closed  bool
}
type routeEntry struct {
	cfg      config.Config
	owner    *Supervisor
	udp      context.Context
	cancel   context.CancelFunc
	refs     int
	draining bool
}

func NewRoutes(ctx context.Context, cfg config.Config, owner *Supervisor) (*Routes, error) {
	udp, cancel := context.WithCancel(ctx)
	return &Routes{base: cfg, ctx: ctx, current: &routeEntry{cfg: cfg.Effective(), owner: owner, udp: udp, cancel: cancel}, entries: map[uint64]*routeEntry{}}, nil
}
func (r *Routes) Snapshot() config.Config { r.mu.Lock(); defer r.mu.Unlock(); return r.current.cfg }
func (r *Routes) Apply(ctx context.Context, cfg config.Config) error {
	r.apply.Lock()
	defer r.apply.Unlock()
	if err := cfg.Validate(); err != nil {
		return err
	}
	// Only selection and generation are mutable; reject all other config edits.
	base, next := r.base, cfg
	base.ActiveRoute = ""
	next.ActiveRoute = ""
	base.PathGeneration = 0
	next.PathGeneration = 0
	if len(cfg.Routes) == 0 || !reflect.DeepEqual(base, next) {
		return errors.New("routes: reload changes pinned configuration")
	}
	r.mu.Lock()
	if r.closed || len(r.entries) >= 16 || cfg.PathGeneration <= r.current.cfg.PathGeneration {
		r.mu.Unlock()
		return errors.New("routes: stale generation or closed")
	}
	r.mu.Unlock()
	effective := cfg.Effective()
	owner, err := New(effective)
	if err != nil {
		return err
	}
	if cfg.Role != config.RoleDestination {
		owner.Transport, err = NewTransport(r.ctx, owner, cfg.TransportMode, cfg.PoolBounds())
		if err != nil {
			return err
		}
		if err = owner.Transport.Warm(ctx); err != nil {
			owner.Transport.Close()
			owner.Close()
			return err
		}
	}
	udp, cancel := context.WithCancel(r.ctx)
	e := &routeEntry{cfg: effective, owner: owner, udp: udp, cancel: cancel}
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		cancel()
		closeRoute(e)
		return context.Canceled
	}
	old := r.current
	old.draining = true
	old.cancel()
	r.current = e
	closeOld := old.refs == 0
	if !closeOld {
		r.entries[old.cfg.PathGeneration] = old
	}
	r.mu.Unlock()
	if closeOld {
		closeRoute(old)
	}
	slog.Info("runtime route activated", "route", cfg.ActiveRoute, "generation", cfg.PathGeneration)
	return nil
}
func closeRoute(e *routeEntry) {
	if e.owner.Transport != nil {
		e.owner.Transport.Close()
	}
}
func (r *Routes) Acquire(ctx context.Context, gen uint64, network vlink.StreamNetwork) (*vlink.MuxLink, func(), context.Context, error) {
	r.mu.Lock()
	e := r.current
	if r.closed || gen != e.cfg.PathGeneration {
		r.mu.Unlock()
		return nil, nil, nil, errors.New("routes: stale generation")
	}
	e.refs++
	r.mu.Unlock()
	mux, release, err := e.owner.Transport.Acquire(ctx, network)
	if err != nil {
		r.release(e)
		return nil, nil, nil, err
	}
	r.mu.Lock()
	stale := r.closed || e.draining
	r.mu.Unlock()
	if stale {
		release()
		r.release(e)
		return nil, nil, nil, errors.New("routes: activation raced acquisition")
	}
	var once sync.Once
	return mux, func() { once.Do(func() { release(); r.release(e) }) }, e.udp, nil
}
func (r *Routes) release(e *routeEntry) {
	r.mu.Lock()
	e.refs--
	closeOld := e.draining && e.refs == 0
	if closeOld {
		delete(r.entries, e.cfg.PathGeneration)
	}
	r.mu.Unlock()
	if closeOld {
		closeRoute(e)
		slog.Info("runtime route drained", "generation", e.cfg.PathGeneration)
	}
}
func (r *Routes) Close() {
	r.apply.Lock()
	defer r.apply.Unlock()
	r.mu.Lock()
	r.closed = true
	all := []*routeEntry{r.current}
	for _, e := range r.entries {
		all = append(all, e)
	}
	r.mu.Unlock()
	for _, e := range all {
		e.cancel()
		closeRoute(e)
	}
}

func (r *Routes) Observe(gen uint64, latency time.Duration) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.closed && gen == r.current.cfg.PathGeneration && r.current.owner.Transport != nil {
		_ = r.current.owner.Transport.ObserveFirstPacket(latency)
	}
}

func (r *Routes) Admit(gen uint64) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed || gen != r.current.cfg.PathGeneration {
		return errors.New("routes: stale generation")
	}
	return nil
}
