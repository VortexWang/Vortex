package store

import (
	"context"
	"testing"
	"time"
)

func TestTimedGrantSealUsesConservativeBoundaryAndActivityFence(t *testing.T) {
	s, use, _ := runtimeStore(t)
	ctx := context.Background()
	scope := DemandScope{NodeID: "node", BootID: "boot", HubID: "hub-one", DelegationID: "parent", SourceID: "funding", SnapshotID: "snapshot", RuleID: "rule", RuleRevision: 1, PathGeneration: 1, SignerEpoch: 1}
	at := IssuerTime{Earliest: use.Earliest, Latest: use.Latest.Add(time.Second), Trusted: true}
	if _, err := s.ObserveDemandIdle(ctx, scope, at); err != nil {
		t.Fatal(err)
	}
	early := IssuerTime{Earliest: at.Latest.Add(10*time.Minute - time.Nanosecond), Latest: at.Latest.Add(10*time.Minute + time.Second), Trusted: true}
	if err := s.SealGrantAfterIdle(ctx, scope, "grant", early, 10*time.Minute); err == nil {
		t.Fatal("latest time alone caused early seal")
	}
	if _, err := s.OpenRuntimeSession(ctx, use, "intervening", "flow"); err != nil {
		t.Fatal(err)
	}
	if err := s.CloseRuntimeSession(ctx, "node", "boot", "intervening"); err != nil {
		t.Fatal(err)
	}
	now := early
	now.Earliest = at.Latest.Add(10 * time.Minute)
	if err := s.SealGrantAfterIdle(ctx, scope, "grant", now, 10*time.Minute); err == nil {
		t.Fatal("stale observation survived activity")
	}
	if _, err := s.ObserveDemandIdle(ctx, scope, now); err != nil {
		t.Fatal(err)
	}
	later := IssuerTime{Earliest: now.Latest.Add(10 * time.Minute), Latest: now.Latest.Add(10*time.Minute + time.Second), Trusted: true}
	if err := s.SealGrantAfterIdle(ctx, scope, "grant", later, 10*time.Minute); err != nil {
		t.Fatal(err)
	}
	proof, _, err := s.PrepareGrantSettlementProof(ctx, scope, "grant")
	if err != nil || proof.Unused != 1<<20 {
		t.Fatalf("sealed unused proof: %+v %v", proof, err)
	}
	if _, err := s.OpenRuntimeSession(ctx, use, "after-seal", "flow"); err == nil {
		t.Fatal("timed fence admitted session")
	}
}

func TestTimedGrantSealClockRollbackInvalidatesInterval(t *testing.T) {
	s, use, _ := runtimeStore(t)
	ctx := context.Background()
	scope := DemandScope{NodeID: "node", BootID: "boot", HubID: "hub-one", DelegationID: "parent", SourceID: "funding", SnapshotID: "snapshot", RuleID: "rule", RuleRevision: 1, PathGeneration: 1, SignerEpoch: 1}
	at := IssuerTime{Earliest: use.Earliest, Latest: use.Latest.Add(time.Second), Trusted: true}
	if _, err := s.ObserveDemandIdle(ctx, scope, at); err != nil {
		t.Fatal(err)
	}
	backwards := IssuerTime{Earliest: at.Earliest.Add(-time.Minute), Latest: at.Latest.Add(-time.Minute), Trusted: true}
	if err := s.SealGrantAfterIdle(ctx, scope, "grant", backwards, time.Minute); err == nil {
		t.Fatal("clock rollback accepted")
	}
	recovered := IssuerTime{Earliest: at.Latest.Add(time.Hour), Latest: at.Latest.Add(time.Hour + time.Second), Trusted: true}
	if err := s.SealGrantAfterIdle(ctx, scope, "grant", recovered, time.Minute); err == nil {
		t.Fatal("old interval resurrected after clock recovery")
	}
	observation, err := s.ObserveDemandIdle(ctx, scope, recovered)
	if err != nil || !observation.Idle || observation.MinimumDuration != 0 {
		t.Fatalf("fresh interval: %+v %v", observation, err)
	}
	if _, err := s.OpenRuntimeSession(ctx, use, "still-active", "flow"); err != nil {
		t.Fatalf("failed timer sealed grant: %v", err)
	}
}
