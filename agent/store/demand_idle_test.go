package store

import (
	"context"
	"testing"
	"time"
)

func TestDemandIdleDoesNotMissActivityBetweenScans(t *testing.T) {
	s, use, path := runtimeStore(t)
	ctx := context.Background()
	scope := DemandScope{NodeID: "node", BootID: "boot", HubID: "hub-one", DelegationID: "parent", SourceID: "funding", SnapshotID: "snapshot", RuleID: "rule", RuleRevision: 1, PathGeneration: 1, SignerEpoch: 1}
	now := IssuerTime{Earliest: use.Earliest, Latest: use.Latest.Add(time.Second), Trusted: true}
	first, err := s.ObserveDemandIdle(ctx, scope, now)
	if err != nil || !first.Idle || first.MinimumDuration != 0 {
		t.Fatalf("initial: %+v %v", first, err)
	}
	later := IssuerTime{Earliest: now.Latest.Add(10 * time.Minute), Latest: now.Latest.Add(10*time.Minute + time.Second), Trusted: true}
	age, err := s.ObserveDemandIdle(ctx, scope, later)
	if err != nil || age.MinimumDuration != 10*time.Minute {
		t.Fatalf("conservative interval: %+v %v", age, err)
	}
	// Session exists and closes entirely between observations; triggers must
	// erase the old interval, even though the next scan sees zero sessions.
	if _, err := s.OpenRuntimeSession(ctx, use, "between-scans", "flow"); err != nil {
		t.Fatal(err)
	}
	if err := s.CloseRuntimeSession(ctx, "node", "boot", "between-scans"); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(path, "agent")
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	reset, err := reopened.ObserveDemandIdle(ctx, scope, later)
	if err != nil || !reset.Idle || reset.MinimumDuration != 0 {
		t.Fatalf("missed short session: %+v %v", reset, err)
	}
	use.Kind = "credit_grant"
	use.AuthorizationID = "grant"
	use.BillingSnapshotID = "snapshot"
	reservation := runtimeReservation("between-batches", 100)
	if _, err := s.ReserveAuthorizedUsage(ctx, reservation, use); err != nil {
		t.Fatal(err)
	}
	busy, err := reopened.ObserveDemandIdle(ctx, scope, later)
	if err != nil || busy.Idle {
		t.Fatalf("pending hold considered idle: %+v %v", busy, err)
	}
	if err := s.SettleUsageReservation(ctx, reservation, "unknown", 100, []byte("unknown"), 100); err != nil {
		t.Fatal(err)
	}
	reset, err = reopened.ObserveDemandIdle(ctx, scope, later)
	if err != nil || !reset.Idle || reset.MinimumDuration != 0 {
		t.Fatalf("settlement did not restart interval: %+v %v", reset, err)
	}
	balance, err := s.AuditAuthorizationSource(ctx, "node", "funding")
	if err != nil || balance.Unknown != 100 {
		t.Fatalf("idle observation freed unknown: %+v %v", balance, err)
	}
	if _, err := s.ObserveDemandIdle(ctx, scope, now); err == nil {
		t.Fatal("clock rollback accepted")
	}
	reset, err = s.ObserveDemandIdle(ctx, scope, later)
	if err != nil || reset.MinimumDuration != 0 {
		t.Fatalf("rollback kept old idle start: %+v %v", reset, err)
	}
}
