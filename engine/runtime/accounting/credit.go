package accounting

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"math/bits"
	"sync"
	"time"

	core "vortex/engine/accounting"
)

// CreditConfig is a test-only, startup-pinned, raw-byte authority. It is not a
// durable issuer or a billing conversion implementation. Zero budget denies.
type CreditConfig struct {
	Observe      bool   `json:"observe,omitempty"`
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

// CreditObservations is a bounded, opt-in diagnostic for D-04 experiments.
// Issuer timings cover the process-local allocator only, never a remote Hub.
type CreditObservations struct {
	Reservations     uint64     `json:"reservations"`
	BlockAllocations uint64     `json:"block_allocations"`
	PeakSessions     int64      `json:"peak_sessions"`
	PeakHeld         int64      `json:"peak_held_bytes"`
	IssuerCalls      uint64     `json:"issuer_calls"`
	IssuerNanos      [64]uint64 `json:"issuer_nanoseconds_log2_buckets"`
}

type CreditWaitObservations struct {
	Attempts      uint64 `json:"attempts"`
	Shortfalls    uint64 `json:"shortfalls"`
	Waits         uint64 `json:"waits"`
	WaitSucceeded uint64 `json:"wait_succeeded"`
	TimedOut      uint64 `json:"timed_out"`
	Cancelled     uint64 `json:"cancelled"`
	FinalFailures uint64 `json:"final_failures"`
}

type CreditState struct {
	Waits        *CreditWaitObservations `json:"waits,omitempty"`
	Observations *CreditObservations     `json:"observations,omitempty"`
	SnapshotID   string                  `json:"snapshot_id"`
	PendingGrant int64                   `json:"pending_grant_bytes"`
	UnknownGrant int64                   `json:"unknown_grant_bytes"`
	GrantRecords uint64                  `json:"grant_records"`
	Budget       int64                   `json:"budget_bytes"`
	Unissued     int64                   `json:"unissued_bytes"`
	Available    int64                   `json:"available_bytes"`
	Held         int64                   `json:"held_bytes"`
	Used         int64                   `json:"used_bytes"`
	Unknown      int64                   `json:"unknown_bytes"`
	Grants       uint64                  `json:"grants"`
	Denials      uint64                  `json:"denials"`
	Sessions     int64                   `json:"sessions"`
}

// CreditGate serializes issuance, block allocation and settlement. At all times
// budget = unissued + pending grants + unknown grants + available + held +
// used + unknown. Held includes idle
// session block tails and unresolved batches; closing never frees an unknown.
type CreditGate struct {
	injectedIssuer bool
	waitStats      CreditWaitObservations
	observeWaits   bool
	mu             sync.Mutex
	cfg            CreditConfig
	state          CreditState
	observations   CreditObservations
	requests       map[string]grantRecord
	grantOwners    map[string]string
	sessions       map[*creditSession]struct{}
	usageSink      core.UsageSink
	faulted        bool
}

func NewCreditGate(c CreditConfig) (*CreditGate, error) {
	return NewCreditGateWithUsage(c, nil)
}

func NewCreditGateWithUsage(c CreditConfig, sink core.UsageSink) (*CreditGate, error) {
	if err := c.Validate(); err != nil {
		return nil, err
	}
	return &CreditGate{cfg: c, usageSink: sink, requests: make(map[string]grantRecord), grantOwners: make(map[string]string), sessions: make(map[*creditSession]struct{}), state: CreditState{SnapshotID: c.SnapshotID, Budget: c.Budget, Unissued: c.Budget}}, nil
}
func (g *CreditGate) State() CreditState {
	g.mu.Lock()
	defer g.mu.Unlock()
	state := g.state
	if g.observeWaits {
		observations := g.waitStats
		state.Waits = &observations
	}
	if g.cfg.Observe {
		observation := g.observations
		state.Observations = &observation
	}
	return state
}
func (g *CreditGate) replenish() {
	if g.injectedIssuer {
		return
	}
	if g.state.Available > g.cfg.GrantTarget*g.cfg.RenewPercent/100 || g.state.Unissued == 0 {
		return
	}
	n := min(g.cfg.GrantTarget-g.state.Available, g.state.Unissued)
	if n > 0 {
		if g.cfg.Observe {
			start := time.Now()
			defer func() {
				ns := max(int64(1), time.Since(start).Nanoseconds())
				g.observations.IssuerCalls++
				g.observations.IssuerNanos[bits.Len64(uint64(ns))-1]++
			}()
		}
		id := fmt.Sprintf("local:%d", len(g.requests)+1)
		if g.beginGrant(id, n) != nil {
			return
		}
		// The default test allocator knows its local result synchronously.
		_ = g.applyGrant(GrantReply{RequestID: id, SnapshotID: g.cfg.SnapshotID, GrantID: id, Amount: n, Outcome: GrantAccepted})
	}
}
func (g *CreditGate) Open(ctx context.Context, f core.Flow) (core.Session, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if g.faulted || len(g.sessions) >= 4096 || f.Key == "" || g.state.Unissued+g.state.Available == 0 && g.state.Held == 0 {
		g.state.Denials++
		return nil, core.ErrDenied
	}
	g.replenish()
	g.state.Sessions++
	if g.cfg.Observe {
		g.observations.PeakSessions = max(g.observations.PeakSessions, g.state.Sessions)
	}
	session := &creditSession{gate: g, flowKey: f.Key, ruleID: f.RuleID, next: [2]int64{g.cfg.InitialBlock, g.cfg.InitialBlock}}
	g.sessions[session] = struct{}{}
	return session, nil
}

type creditSession struct {
	gate    *CreditGate
	flowKey string
	ruleID  string
	closed  bool
	idle    [2]int64
	next    [2]int64
}

func (s *creditSession) Reserve(ctx context.Context, d core.Direction, n int64) (*core.Batch, error) {
	return s.reserve(ctx, d, n, true)
}
func (s *creditSession) reserve(ctx context.Context, d core.Direction, n int64, countDenial bool) (*core.Batch, error) {
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
	if g.faulted || s.closed || n < 1 || n > g.cfg.MaxBlock {
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
		// An idle block tail is not an unresolved write. Reclaim only these
		// unused bytes from other sessions when the finite pool is scarce.
		for other := range g.sessions {
			if g.state.Available >= need {
				break
			}
			if other == s {
				continue
			}
			for j, tail := range other.idle {
				other.idle[j] = 0
				g.state.Held -= tail
				g.state.Available += tail
			}
		}
		take := min(max(need, s.next[i]), g.state.Available)
		if take < need {
			if countDenial {
				g.state.Denials++
			}
			return nil, core.ErrDenied
		}
		g.state.Available -= take
		g.state.Held += take
		if g.cfg.Observe {
			g.observations.BlockAllocations++
			g.observations.PeakHeld = max(g.observations.PeakHeld, g.state.Held)
		}
		s.idle[i] += take
		s.next[i] = min(g.cfg.MaxBlock, s.next[i]*2)
		g.replenish()
	}
	if g.cfg.Observe {
		g.observations.Reservations++
	}
	s.idle[i] -= n
	usageID := ""
	if g.usageSink != nil {
		var err error
		usageID, err = usageBatchID()
		if err != nil {
			g.faulted = true
			s.idle[i] += n
			return nil, fmt.Errorf("credit: reservation identity unavailable; gate frozen: %w", err)
		}
		if err := g.usageSink.ReserveUsage(ctx, core.UsageRecord{BatchID: usageID, FlowKey: s.flowKey, RuleID: s.ruleID, SnapshotID: g.cfg.SnapshotID, Direction: direction(i), Reserved: n}); err != nil {
			g.faulted = true
			g.state.Held -= n
			g.state.Unknown += n
			return nil, fmt.Errorf("credit: durable reservation failed; gate frozen: %w", err)
		}
	}
	return core.NewBatch(n, func(commitCtx context.Context, o core.Outcome) error {
		g.mu.Lock()
		defer g.mu.Unlock()
		if g.usageSink != nil {
			used := o.Used
			if o.Resolution == core.Unknown {
				used = n
			}
			if err := g.usageSink.SettleUsage(commitCtx, core.UsageRecord{BatchID: usageID, FlowKey: s.flowKey, RuleID: s.ruleID, SnapshotID: g.cfg.SnapshotID, Direction: direction(i), Resolution: o.Resolution, Reserved: n, Used: used}); err != nil {
				g.faulted = true
				g.state.Held -= n
				g.state.Unknown += n
				return fmt.Errorf("credit: durable usage persistence failed; authority frozen: %w", err)
			}
		}
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

func direction(index int) core.Direction {
	if index == 1 {
		return core.RightToLeft
	}
	return core.LeftToRight
}

func usageBatchID() (string, error) {
	var id [16]byte
	if _, err := rand.Read(id[:]); err != nil {
		return "", err
	}
	id[6] = (id[6] & 0x0f) | 0x40
	id[8] = (id[8] & 0x3f) | 0x80
	encoded := hex.EncodeToString(id[:])
	return encoded[:8] + "-" + encoded[8:12] + "-" + encoded[12:16] + "-" + encoded[16:20] + "-" + encoded[20:], nil
}
func (s *creditSession) Close() error {
	g := s.gate
	g.mu.Lock()
	defer g.mu.Unlock()
	if s.closed {
		return nil
	}
	s.closed = true
	delete(g.sessions, s)
	g.state.Sessions--
	for i, n := range s.idle {
		g.state.Held -= n
		g.state.Available += n
		s.idle[i] = 0
	}
	return nil
}
