package accounting

import (
	"context"
	"errors"
	"sync"
	"testing"
	core "vortex/engine/accounting"
)

type usageSinkFunc func(context.Context, core.UsageRecord) error
type usageSink struct {
	reserve func(context.Context, core.UsageRecord) error
	settle  func(context.Context, core.UsageRecord) error
}

func (s usageSink) ReserveUsage(ctx context.Context, record core.UsageRecord) error {
	if s.reserve == nil {
		return nil
	}
	return s.reserve(ctx, record)
}
func (s usageSink) SettleUsage(ctx context.Context, record core.UsageRecord) error {
	if s.settle != nil {
		return s.settle(ctx, record)
	}
	return nil
}

func (f usageSinkFunc) SettleUsage(ctx context.Context, record core.UsageRecord) error {
	return f(ctx, record)
}
func (f usageSinkFunc) ReserveUsage(context.Context, core.UsageRecord) error { return nil }

func TestCreditGatePersistsUsageBeforeSettlingReservation(t *testing.T) {
	var records []core.UsageRecord
	g, err := NewCreditGateWithUsage(CreditConfig{SnapshotID: "snapshot", Budget: 256, InitialBlock: 64, MaxBlock: 64, GrantTarget: 64, RenewPercent: 30}, usageSinkFunc(func(_ context.Context, record core.UsageRecord) error {
		records = append(records, record)
		return nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	session, err := g.Open(context.Background(), core.Flow{Key: "opaque-flow", RuleID: "rule-1"})
	if err != nil {
		t.Fatal(err)
	}
	batch, err := session.Reserve(context.Background(), core.LeftToRight, 32)
	if err != nil {
		t.Fatal(err)
	}
	if err := batch.Commit(context.Background(), 20); err != nil {
		t.Fatal(err)
	}
	if state := g.State(); state.Used != 20 || state.Unknown != 0 {
		t.Fatalf("credit settled before/without durable use: %+v", state)
	}
	if len(records) != 1 || records[0].BatchID == "" || records[0].SnapshotID != "snapshot" || records[0].RuleID != "rule-1" || records[0].Used != 20 {
		t.Fatalf("usage record=%+v", records)
	}
}

func TestCreditGateFreezesWhenDurableUsageWriteFails(t *testing.T) {
	failure := errors.New("disk full")
	g, err := NewCreditGateWithUsage(CreditConfig{SnapshotID: "snapshot", Budget: 256, InitialBlock: 64, MaxBlock: 64, GrantTarget: 64, RenewPercent: 30}, usageSinkFunc(func(context.Context, core.UsageRecord) error { return failure }))
	if err != nil {
		t.Fatal(err)
	}
	session, err := g.Open(context.Background(), core.Flow{Key: "flow-1", RuleID: "rule-1"})
	if err != nil {
		t.Fatal(err)
	}
	batch, err := session.Reserve(context.Background(), core.LeftToRight, 32)
	if err != nil {
		t.Fatal(err)
	}
	if err := batch.Commit(context.Background(), 20); !errors.Is(err, failure) {
		t.Fatalf("durability error=%v", err)
	}
	state := g.State()
	if state.Unknown != 32 || state.Used != 0 {
		t.Fatalf("failed write did not retain the full upper bound: %+v", state)
	}
	if _, err := g.Open(context.Background(), core.Flow{Key: "new", RuleID: "rule-1"}); !errors.Is(err, core.ErrDenied) {
		t.Fatalf("faulted gate admitted new flow: %v", err)
	}
	if _, err := session.Reserve(context.Background(), core.LeftToRight, 1); !errors.Is(err, core.ErrDenied) {
		t.Fatalf("faulted gate allowed existing flow to produce unrecorded usage: %v", err)
	}
}

func TestCreditGateDoesNotReturnWritableBatchWhenPrewriteFails(t *testing.T) {
	failure := errors.New("sqlite full")
	g, err := NewCreditGateWithUsage(CreditConfig{SnapshotID: "snapshot", Budget: 256, InitialBlock: 64, MaxBlock: 64, GrantTarget: 64, RenewPercent: 30}, usageSink{reserve: func(context.Context, core.UsageRecord) error { return failure }})
	if err != nil {
		t.Fatal(err)
	}
	session, err := g.Open(context.Background(), core.Flow{Key: "flow-1", RuleID: "rule-1"})
	if err != nil {
		t.Fatal(err)
	}
	if batch, err := session.Reserve(context.Background(), core.LeftToRight, 32); err == nil || batch != nil {
		t.Fatalf("prewrite failure returned writable batch=%v err=%v", batch, err)
	}
	state := g.State()
	if state.Unknown != 32 || state.Held != 32 || state.Budget != state.Unissued+state.PendingGrant+state.UnknownGrant+state.Available+state.Held+state.Used+state.Unknown {
		t.Fatalf("prewrite failure broke conservative accounting: %+v", state)
	}
}

func creditFixture(t *testing.T, budget int64) *CreditGate {
	t.Helper()
	g, e := NewCreditGate(CreditConfig{SnapshotID: "rule/execution/path-1/policy-1", Budget: budget, InitialBlock: 64, MaxBlock: 256, GrantTarget: 256, RenewPercent: 30})
	if e != nil {
		t.Fatal(e)
	}
	return g
}
func conserved(t *testing.T, g *CreditGate) {
	t.Helper()
	s := g.State()
	if s.Budget != s.Unissued+s.PendingGrant+s.UnknownGrant+s.Available+s.Held+s.Used+s.Unknown || s.Unissued < 0 || s.Available < 0 || s.Held < 0 || s.Used < 0 || s.Unknown < 0 {
		t.Fatalf("conservation: %+v", s)
	}
}
func TestCreditExhaustionRenewalAndDirections(t *testing.T) {
	g := creditFixture(t, 512)
	ctx := context.Background()
	s, e := g.Open(ctx, core.Flow{Key: "flow"})
	if e != nil {
		t.Fatal(e)
	}
	for i := 0; i < 8; i++ {
		d := core.LeftToRight
		if i%2 == 1 {
			d = core.RightToLeft
		}
		b, e := s.Reserve(ctx, d, 64)
		if e != nil {
			t.Fatal(i, e)
		}
		if e = b.Commit(ctx, 64); e != nil {
			t.Fatal(e)
		}
		conserved(t, g)
	}
	if _, e = s.Reserve(ctx, core.LeftToRight, 1); e == nil {
		t.Fatal("overspent")
	}
	s.Close()
	v := g.State()
	if v.Used != 512 || v.Grants < 2 || v.Sessions != 0 || v.Held != 0 {
		t.Fatal(v)
	}
	if _, e = g.Open(ctx, core.Flow{Key: "new"}); e == nil {
		t.Fatal("exhausted admission")
	}
}
func TestCreditUnknownAndPendingSurviveClose(t *testing.T) {
	ctx := context.Background()
	g := creditFixture(t, 256)
	s, _ := g.Open(ctx, core.Flow{Key: "f"})
	a, _ := s.Reserve(ctx, core.LeftToRight, 32)
	b, _ := s.Reserve(ctx, core.RightToLeft, 32)
	s.Close()
	conserved(t, g)
	if g.State().Held != 64 {
		t.Fatal(g.State())
	}
	if e := a.MarkUnknown(ctx); e != nil {
		t.Fatal(e)
	}
	if e := a.Return(ctx); e != core.ErrAlreadyResolved {
		t.Fatal(e)
	}
	if e := b.Commit(ctx, 7); e != nil {
		t.Fatal(e)
	}
	conserved(t, g)
	v := g.State()
	if v.Unknown != 32 || v.Used != 7 || v.Held != 0 || v.Available+v.Unissued != 217 {
		t.Fatal(v)
	}
}
func TestCreditReturnedPartialAndCancelledReservations(t *testing.T) {
	g := creditFixture(t, 256)
	ctx := context.Background()
	s, _ := g.Open(ctx, core.Flow{Key: "f"})
	b, _ := s.Reserve(ctx, core.LeftToRight, 32)
	b.Commit(ctx, 5)
	c, _ := s.Reserve(ctx, core.RightToLeft, 32)
	c.Return(ctx)
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if _, e := s.Reserve(cancelled, core.LeftToRight, 1); e == nil {
		t.Fatal("cancel ignored")
	}
	s.Close()
	s.Close()
	if _, e := s.Reserve(ctx, core.LeftToRight, 1); e == nil {
		t.Fatal("closed reserve")
	}
	conserved(t, g)
	if g.State().Used != 5 || g.State().Held != 0 {
		t.Fatal(g.State())
	}
}
func TestCreditConcurrentBudgetCannotBeReissued(t *testing.T) {
	g := creditFixture(t, 4096)
	var wg sync.WaitGroup
	ctx := context.Background()
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			s, e := g.Open(ctx, core.Flow{Key: "f"})
			if e != nil {
				return
			}
			defer s.Close()
			for j := 0; j < 8; j++ {
				b, e := s.Reserve(ctx, core.LeftToRight, 32)
				if e != nil {
					return
				}
				if j%2 == 0 {
					b.MarkUnknown(ctx)
				} else {
					b.Commit(ctx, 32)
				}
			}
		}()
	}
	wg.Wait()
	conserved(t, g)
	v := g.State()
	if v.Sessions != 0 || v.Held != 0 || v.Used+v.Unknown > v.Budget {
		t.Fatal(v)
	}
}
func TestCreditZeroBudgetAndIndependentSnapshotAuthority(t *testing.T) {
	ctx := context.Background()
	g := creditFixture(t, 0)
	if _, e := g.Open(ctx, core.Flow{Key: "f"}); e == nil {
		t.Fatal("zero budget admitted")
	}
	// Each startup-pinned authority owns a distinct finite budget; no shared map
	// allows a session to change its snapshot or acquire another gate's balance.
	a := creditFixture(t, 256)
	b := creditFixture(t, 256)
	s, _ := a.Open(ctx, core.Flow{Key: "f"})
	x, _ := s.Reserve(ctx, core.LeftToRight, 256)
	x.MarkUnknown(ctx)
	s.Close()
	if b.State().Unknown != 0 || b.State().Unissued != 256 {
		t.Fatal(b.State())
	}
	conserved(t, a)
	conserved(t, b)
}

func TestCreditReclaimsOnlyIdleTailsAcrossSessions(t *testing.T) {
	g, err := NewCreditGate(CreditConfig{SnapshotID: "reclaim", Budget: 128, InitialBlock: 64, MaxBlock: 256, GrantTarget: 256, RenewPercent: 30, Observe: true})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	a, _ := g.Open(ctx, core.Flow{Key: "a"})
	b, _ := g.Open(ctx, core.Flow{Key: "b"})
	x, _ := a.Reserve(ctx, core.LeftToRight, 1)
	x.Commit(ctx, 1)
	y, _ := b.Reserve(ctx, core.RightToLeft, 100) // Scarce pool reclaims a's unused tail.
	if y == nil {
		t.Fatal("idle credit stranded")
	}
	if e := y.MarkUnknown(ctx); e != nil {
		t.Fatal(e)
	}
	z, e := a.Reserve(ctx, core.LeftToRight, 27)
	if e != nil {
		t.Fatal(e)
	}
	if err := z.Commit(ctx, 27); err != nil {
		t.Fatal(err)
	}
	if _, e = b.Reserve(ctx, core.RightToLeft, 1); e == nil {
		t.Fatal("unknown reissued")
	}
	a.Close()
	b.Close()
	conserved(t, g)
	v := g.State()
	if v.Used != 28 || v.Unknown != 100 || v.Held != 0 {
		t.Fatal(v)
	}
	if v.Observations == nil || v.Observations.IdleReclaims == 0 || v.Observations.IdleReclaimed == 0 {
		t.Fatalf("idle reclaim observations=%+v", v.Observations)
	}
}

func TestCreditObservationsAreBoundedAndCopied(t *testing.T) {
	cfg := CreditConfig{SnapshotID: "observed", Budget: 4096, InitialBlock: 64, MaxBlock: 256, GrantTarget: 512, RenewPercent: 30, Observe: true}
	gate, err := NewCreditGate(cfg)
	if err != nil {
		t.Fatal(err)
	}
	session, err := gate.Open(context.Background(), core.Flow{Key: "observed"})
	if err != nil {
		t.Fatal(err)
	}
	for range 12 {
		batch, err := session.Reserve(context.Background(), core.LeftToRight, 32)
		if err != nil {
			t.Fatal(err)
		}
		if err := batch.Commit(context.Background(), 32); err != nil {
			t.Fatal(err)
		}
	}
	session.Close()
	state := gate.State()
	obs := state.Observations
	if obs == nil || obs.Reservations != 12 || obs.BlockAllocations != 3 || obs.PeakSessions != 1 || obs.PeakHeld < 256 || obs.IssuerCalls != state.Grants {
		t.Fatalf("invalid bounded observations: %+v, state %+v", obs, state)
	}
	var samples uint64
	for _, n := range obs.IssuerNanos {
		samples += n
	}
	if samples != obs.IssuerCalls {
		t.Fatal("timing samples lost")
	}
	obs.Reservations = 0
	if gate.State().Observations.Reservations != 12 {
		t.Fatal("State leaked mutable observations")
	}
	conserved(t, gate)
	if creditFixture(t, 256).State().Observations != nil {
		t.Fatal("diagnostics enabled by default")
	}
}
