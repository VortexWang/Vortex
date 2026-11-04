package accounting

import (
	"context"
	"sync"
	"testing"
	core "vortex/engine/accounting"
)

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
	if s.Budget != s.Unissued+s.Available+s.Held+s.Used+s.Unknown || s.Unissued < 0 || s.Available < 0 || s.Held < 0 || s.Used < 0 || s.Unknown < 0 {
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
