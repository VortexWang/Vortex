package accounting

import (
	"context"
	"fmt"
	"sync"
	"testing"
	core "vortex/engine/accounting"
)

func TestInjectedRenewalWaterlineDelayAndUnknown(t *testing.T) {
	for _, percent := range []int64{20, 30, 50} {
		t.Run(fmt.Sprint(percent), func(t *testing.T) {
			ctx := context.Background()
			g, err := NewInjectedCreditGate(CreditConfig{SnapshotID: "s", Budget: 1000, InitialBlock: 1, MaxBlock: 1, GrantTarget: 100, RenewPercent: percent, Observe: true})
			if err != nil {
				t.Fatal(err)
			}
			id, n, err := g.BeginRenewal(ctx, "initial")
			if err != nil || n != 100 {
				t.Fatalf("initial %s %d %v", id, n, err)
			}
			reply := GrantReply{RequestID: id, SnapshotID: "s", GrantID: "g-initial", Amount: n, Outcome: GrantAccepted}
			if err := g.ApplyGrant(ctx, reply); err != nil {
				t.Fatal(err)
			}
			session, err := g.Open(ctx, core.Flow{Key: "flow"})
			if err != nil {
				t.Fatal(err)
			}
			defer session.Close()
			consume := func(count int64) {
				t.Helper()
				for i := int64(0); i < count; i++ {
					b, e := session.Reserve(ctx, core.LeftToRight, 1)
					if e != nil {
						t.Fatal(e)
					}
					if e = b.Commit(ctx, 1); e != nil {
						t.Fatal(e)
					}
					conserved(t, g)
				}
			}
			consume(100 - percent - 1)
			if _, amount, e := g.BeginRenewal(ctx, "too-early"); e != nil || amount != 0 {
				t.Fatalf("above waterline: %d %v", amount, e)
			}
			consume(1)
			var wg sync.WaitGroup
			var mu sync.Mutex
			ids := map[string]bool{}
			for i := 0; i < 16; i++ {
				wg.Add(1)
				go func(i int) {
					defer wg.Done()
					id, amount, e := g.BeginRenewal(ctx, fmt.Sprintf("renew-%d", i))
					if e != nil || amount != 100-percent {
						t.Errorf("renewal: %d %v", amount, e)
					}
					mu.Lock()
					ids[id] = true
					mu.Unlock()
				}(i)
			}
			wg.Wait()
			if len(ids) != 1 || g.State().GrantRecords != 2 {
				t.Fatalf("request did not coalesce: %+v %+v", ids, g.State())
			}
			observations := g.State().Observations
			if observations == nil || observations.RenewalAttempts != 18 || observations.RenewalMerged != 15 || observations.PeakPending != 100 {
				t.Fatalf("renewal observations=%+v", observations)
			}
			for id = range ids {
			}
			consume(percent)
			// Reply is deliberately withheld until all usable credit is consumed.
			if _, e := session.Reserve(ctx, core.LeftToRight, 1); e == nil {
				t.Fatal("pending grant spent before response")
			}
			state := g.State()
			if state.PendingGrant != 100-percent || state.Available != 0 || state.Used != 100 || state.Grants != 1 {
				t.Fatal(state)
			}
			reply = GrantReply{RequestID: id, SnapshotID: "s", Amount: 100 - percent, Outcome: GrantUnknown}
			if e := g.ApplyGrant(ctx, reply); e != nil {
				t.Fatal(e)
			}
			if observations := g.State().Observations; observations == nil || observations.PeakUnknown != 100-percent {
				t.Fatalf("unknown occupancy observations=%+v", observations)
			}
			if _, _, e := g.BeginRenewal(ctx, "replacement"); e == nil {
				t.Fatal("unknown replaced")
			}
			rejected := reply
			rejected.Outcome = GrantRejected
			if e := g.ApplyGrant(ctx, rejected); e == nil {
				t.Fatal("unknown released without proof")
			}
			reply.Outcome = GrantAccepted
			reply.GrantID = "late-grant"
			for i := 0; i < 2; i++ {
				if e := g.ApplyGrant(ctx, reply); e != nil {
					t.Fatal(e)
				}
			}
			state = g.State()
			if state.Available != 100-percent || state.Grants != 2 || state.UnknownGrant != 0 || state.PendingGrant != 0 {
				t.Fatal(state)
			}
			consume(100 - percent)
			conserved(t, g)
		})
	}
}

func TestInjectedRenewalRejectionAndCancellation(t *testing.T) {
	ctx := context.Background()
	g, err := NewInjectedCreditGate(CreditConfig{SnapshotID: "s", Budget: 100, InitialBlock: 1, MaxBlock: 1, GrantTarget: 100, RenewPercent: 30})
	if err != nil {
		t.Fatal(err)
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if _, _, e := g.BeginRenewal(cancelled, "cancelled"); e == nil {
		t.Fatal("cancelled renewal created")
	}
	if g.State().GrantRecords != 0 {
		t.Fatal(g.State())
	}
	id, n, err := g.BeginRenewal(ctx, "first")
	if err != nil {
		t.Fatal(err)
	}
	r := GrantReply{RequestID: id, SnapshotID: "s", Amount: n, Outcome: GrantRejected}
	if e := g.ApplyGrant(ctx, r); e != nil {
		t.Fatal(e)
	}
	if _, _, e := g.BeginRenewal(ctx, id); e == nil {
		t.Fatal("terminal request reused")
	}
	if _, n, e := g.BeginRenewal(ctx, "second"); e != nil || n != 100 {
		t.Fatalf("retry %d %v", n, e)
	}
	if g.State().Unissued != 0 || g.State().PendingGrant != 100 || g.State().Available != 0 {
		t.Fatal(g.State())
	}
	conserved(t, g)
}
