package accounting

import (
	"context"
	"testing"
	"time"
	core "vortex/engine/accounting"
)

func TestDynamicTargetCountsCommittedAndRetainsUnknownOnIdle(t *testing.T) {
	c := CreditConfig{SnapshotID: "dynamic", Budget: 512 << 20, InitialBlock: 1 << 20, MaxBlock: 4 << 20, GrantTarget: 64 << 20, RenewPercent: 30, TargetSeconds: 30}
	g, err := NewCreditGate(c)
	if err != nil {
		t.Fatal(err)
	}
	at := time.Unix(1000, 0)
	g.demand.now = func() time.Time { return at }
	if got := g.State().DynamicTarget; got != 8<<20 {
		t.Fatalf("cold target %d", got)
	}
	ctx := context.Background()
	s, err := g.Open(ctx, core.Flow{Key: "dynamic"})
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 8; i++ {
		b, err := s.Reserve(ctx, core.LeftToRight, 4<<20)
		if err != nil {
			t.Fatal(err)
		}
		if err := b.Commit(ctx, 4<<20); err != nil {
			t.Fatal(err)
		}
		conserved(t, g)
	}
	b, err := s.Reserve(ctx, core.RightToLeft, 4<<20)
	if err != nil {
		t.Fatal(err)
	}
	if err := b.MarkUnknown(ctx); err != nil {
		t.Fatal(err)
	}
	if err := g.BeginGrant(ctx, "in-flight", 8<<20); err != nil {
		t.Fatal(err)
	}
	if got := g.State().DynamicTarget; got != 16<<20 {
		t.Fatalf("unknown counted as consumption: %d", got)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	before := g.State()
	at = at.Add(60 * time.Second)
	after := g.State()
	if after.DynamicTarget != 8<<20 || after.Unknown != 4<<20 || after.PendingGrant != 8<<20 || after.Unissued != before.Unissued || after.Available != before.Available {
		t.Fatalf("idle shrink changed authority: before=%+v after=%+v", before, after)
	}
	conserved(t, g)
}

func TestDynamicTargetCeilingAndConfig(t *testing.T) {
	c := CreditConfig{SnapshotID: "s", Budget: 1 << 50, InitialBlock: 1, MaxBlock: 1, GrantTarget: 16 << 20, RenewPercent: 30, TargetSeconds: 60}
	g, err := NewCreditGate(c)
	if err != nil {
		t.Fatal(err)
	}
	g.demand.observe(1 << 50)
	if got := g.State().DynamicTarget; got != c.GrantTarget {
		t.Fatalf("ceiling %d", got)
	}
	for _, seconds := range []int64{-1, 61} {
		c.TargetSeconds = seconds
		if c.Validate() == nil {
			t.Fatalf("invalid horizon %d", seconds)
		}
	}
	c.TargetSeconds = 30
	c.GrantTarget = 1
	if c.Validate() == nil {
		t.Fatal("ceiling below cold-start floor")
	}
	c.TargetSeconds = 0
	if err := c.Validate(); err != nil {
		t.Fatal("fixed-target compatibility", err)
	}
}
