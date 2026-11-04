package accounting

import (
	"context"
	"fmt"
	"sync"
	"testing"
	core "vortex/engine/accounting"
)

func TestGrantUnknownLateResponseAndIdentity(t *testing.T) {
	g := creditFixture(t, 256)
	ctx := context.Background()
	if err := g.BeginGrant(ctx, "r1", 256); err != nil {
		t.Fatal(err)
	}
	if err := g.BeginGrant(ctx, "r1", 256); err != nil {
		t.Fatal(err)
	}
	reply := GrantReply{RequestID: "r1", SnapshotID: g.cfg.SnapshotID, Amount: 256, Outcome: GrantUnknown}
	if err := g.ApplyGrant(ctx, reply); err != nil {
		t.Fatal(err)
	}
	if err := g.ApplyGrant(ctx, reply); err != nil {
		t.Fatal(err)
	}
	conserved(t, g)
	if g.State().UnknownGrant != 256 || g.State().PendingGrant != 0 {
		t.Fatal(g.State())
	}
	if _, err := g.Open(ctx, core.Flow{Key: "f"}); err == nil {
		t.Fatal("unknown issuance consumed")
	}
	denied := reply
	denied.Outcome = GrantRejected
	if err := g.ApplyGrant(ctx, denied); err == nil {
		t.Fatal("unknown released without proof")
	}
	accepted := reply
	accepted.Outcome = GrantAccepted
	accepted.GrantID = "g1"
	for _, mutate := range []func(*GrantReply){func(r *GrantReply) { r.SnapshotID = "other" }, func(r *GrantReply) { r.Amount = 255 }, func(r *GrantReply) { r.RequestID = "other" }, func(r *GrantReply) { r.GrantID = "" }} {
		bad := accepted
		mutate(&bad)
		if g.ApplyGrant(ctx, bad) == nil {
			t.Fatal("identity mismatch accepted", bad)
		}
	}
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := g.ApplyGrant(ctx, accepted); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	conserved(t, g)
	v := g.State()
	if v.Available != 256 || v.UnknownGrant != 0 || v.Grants != 1 || v.GrantRecords != 1 {
		t.Fatal(v)
	}
	if g.ApplyGrant(ctx, reply) == nil {
		t.Fatal("accepted grant rolled back to unknown")
	}
	s, err := g.Open(ctx, core.Flow{Key: "f"})
	if err != nil {
		t.Fatal(err)
	}
	b, err := s.Reserve(ctx, core.LeftToRight, 256)
	if err != nil {
		t.Fatal(err)
	}
	b.MarkUnknown(ctx)
	s.Close()
	conserved(t, g)
	if g.ApplyGrant(ctx, accepted) != nil || g.State().Unknown != 256 || g.State().Available != 0 {
		t.Fatal("replay reissued consumed unknown grant", g.State())
	}
}
func TestGrantRejectedProofAndGrantIDReuse(t *testing.T) {
	g := creditFixture(t, 512)
	ctx := context.Background()
	for _, id := range []string{"a", "b"} {
		if e := g.BeginGrant(ctx, id, 256); e != nil {
			t.Fatal(e)
		}
	}
	a := GrantReply{RequestID: "a", SnapshotID: g.cfg.SnapshotID, Amount: 256, Outcome: GrantAccepted, GrantID: "same"}
	if e := g.ApplyGrant(ctx, a); e != nil {
		t.Fatal(e)
	}
	b := a
	b.RequestID = "b"
	if g.ApplyGrant(ctx, b) == nil {
		t.Fatal("grant ID reused for another request")
	}
	b.GrantID = ""
	b.Outcome = GrantUnknown
	if e := g.ApplyGrant(ctx, b); e != nil {
		t.Fatal(e)
	}
	b.Outcome = GrantRejected
	b.NotIssuedProof = true
	if e := g.ApplyGrant(ctx, b); e != nil {
		t.Fatal(e)
	}
	if e := g.ApplyGrant(ctx, b); e != nil {
		t.Fatal(e)
	}
	if e := g.BeginGrant(ctx, "b", 256); e != nil {
		t.Fatal(e)
	}
	conserved(t, g)
	v := g.State()
	if v.Unissued != 256 || v.Available != 256 || v.PendingGrant != 0 || v.UnknownGrant != 0 || v.GrantRecords != 2 {
		t.Fatal(v)
	}
	b.Outcome = GrantAccepted
	b.GrantID = "new"
	if g.ApplyGrant(ctx, b) == nil {
		t.Fatal("rejected transaction resurrected")
	}
}
func TestGrantRequestsBoundedAndCancelled(t *testing.T) {
	ctx := context.Background()
	g := creditFixture(t, 1<<20)
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if g.BeginGrant(cancelled, "cancel", 1) == nil {
		t.Fatal("cancelled request")
	}
	if g.BeginGrant(ctx, "local:spoof", 1) == nil {
		t.Fatal("reserved namespace")
	}
	for i := 0; i < maxGrantRecords; i++ {
		id := fmt.Sprint(i)
		if e := g.BeginGrant(ctx, id, 1); e != nil {
			t.Fatal(e)
		}
	}
	if g.BeginGrant(ctx, "overflow", 1) == nil {
		t.Fatal("unbounded records")
	}
	conserved(t, g)
	if g.State().PendingGrant != maxGrantRecords {
		t.Fatal(g.State())
	}
}
