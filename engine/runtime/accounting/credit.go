package accounting

import (
	"context"
	"errors"
	"sync"

	core "vortex/engine/accounting"
)

// CreditConfig is a test-only, startup-pinned, raw-byte authority. It is not a
// durable issuer or a billing conversion implementation. Zero budget denies.
type CreditConfig struct {
	SnapshotID   string `json:"snapshot_id"`
	Budget       int64  `json:"budget_bytes"`
	InitialBlock int64  `json:"initial_block_bytes"`
	MaxBlock     int64  `json:"max_block_bytes"`
	GrantTarget  int64  `json:"grant_target_bytes"`
	RenewPercent int64  `json:"renew_percent"`
}

func (c CreditConfig) Validate() error {
	if c.SnapshotID == "" || len(c.SnapshotID) > 256 || c.Budget < 0 || c.Budget > 1<<50 || c.InitialBlock < 1 || c.MaxBlock < c.InitialBlock || c.MaxBlock > 4<<20 || c.GrantTarget < c.MaxBlock || c.GrantTarget > 1<<40 || c.RenewPercent < 1 || c.RenewPercent > 99 {
		return errors.New("credit: invalid finite credit configuration")
	}
	return nil
}

type CreditState struct {
	SnapshotID string `json:"snapshot_id"`
	Budget     int64  `json:"budget_bytes"`
	Unissued   int64  `json:"unissued_bytes"`
	Available  int64  `json:"available_bytes"`
	Held       int64  `json:"held_bytes"`
	Used       int64  `json:"used_bytes"`
	Unknown    int64  `json:"unknown_bytes"`
	Grants     uint64 `json:"grants"`
	Denials    uint64 `json:"denials"`
	Sessions   int64  `json:"sessions"`
}

// CreditGate serializes issuance, block allocation and settlement. At all times
// budget = unissued + available + held + used + unknown. Held includes idle
// session block tails and unresolved batches; closing never frees an unknown.
type CreditGate struct {
	mu    sync.Mutex
	cfg   CreditConfig
	state CreditState
}

func NewCreditGate(c CreditConfig) (*CreditGate, error) {
	if err := c.Validate(); err != nil {
		return nil, err
	}
	return &CreditGate{cfg: c, state: CreditState{SnapshotID: c.SnapshotID, Budget: c.Budget, Unissued: c.Budget}}, nil
}
func (g *CreditGate) State() CreditState { g.mu.Lock(); defer g.mu.Unlock(); return g.state }
func (g *CreditGate) replenish() {
	if g.state.Available > g.cfg.GrantTarget*g.cfg.RenewPercent/100 || g.state.Unissued == 0 {
		return
	}
	n := min(g.cfg.GrantTarget-g.state.Available, g.state.Unissued)
	if n > 0 {
		g.state.Unissued -= n
		g.state.Available += n
		g.state.Grants++
	}
}
func (g *CreditGate) Open(ctx context.Context, f core.Flow) (core.Session, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if f.Key == "" || g.state.Unissued+g.state.Available == 0 {
		g.state.Denials++
		return nil, core.ErrDenied
	}
	g.replenish()
	g.state.Sessions++
	return &creditSession{gate: g, next: [2]int64{g.cfg.InitialBlock, g.cfg.InitialBlock}}, nil
}

type creditSession struct {
	gate   *CreditGate
	closed bool
	idle   [2]int64
	next   [2]int64
}

func (s *creditSession) Reserve(ctx context.Context, d core.Direction, n int64) (*core.Batch, error) {
	g := s.gate
	g.mu.Lock()
	defer g.mu.Unlock()
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	i := 0
	if d == core.RightToLeft {
		i = 1
	} else if d != core.LeftToRight {
		return nil, core.ErrInvalidUsage
	}
	if s.closed || n < 1 || n > g.cfg.MaxBlock {
		return nil, core.ErrDenied
	}
	if s.idle[i] < n {
		g.replenish()
		need := n - s.idle[i]
		// Reclaim only unused tail bytes, never an outstanding batch. Both
		// directions share this one pinned authority, so this avoids a false
		// denial at the finite budget boundary without creating new credit.
		other := 1 - i
		if g.state.Available < need && s.idle[other] > 0 {
			tail := s.idle[other]
			s.idle[other] = 0
			g.state.Held -= tail
			g.state.Available += tail
		}
		take := min(max(need, s.next[i]), g.state.Available)
		if take < need {
			g.state.Denials++
			return nil, core.ErrDenied
		}
		g.state.Available -= take
		g.state.Held += take
		s.idle[i] += take
		s.next[i] = min(g.cfg.MaxBlock, s.next[i]*2)
		g.replenish()
	}
	s.idle[i] -= n
	return core.NewBatch(n, func(_ context.Context, o core.Outcome) error {
		g.mu.Lock()
		defer g.mu.Unlock()
		if o.Resolution == core.Unknown {
			g.state.Held -= n
			g.state.Unknown += n
			return nil
		}
		g.state.Held -= o.Used
		g.state.Used += o.Used
		remainder := n - o.Used
		if s.closed {
			g.state.Held -= remainder
			g.state.Available += remainder
		} else {
			s.idle[i] += remainder
		}
		return nil
	})
}
func (s *creditSession) Close() error {
	g := s.gate
	g.mu.Lock()
	defer g.mu.Unlock()
	if s.closed {
		return nil
	}
	s.closed = true
	g.state.Sessions--
	for i, n := range s.idle {
		g.state.Held -= n
		g.state.Available += n
		s.idle[i] = 0
	}
	return nil
}
