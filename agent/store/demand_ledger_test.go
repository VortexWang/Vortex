package store

import (
	"context"
	"testing"
	"time"
)

func TestDemandLedgerUsesOnlyScopedCommittedConsumption(t *testing.T) {
	s, use, _ := runtimeStore(t)
	ctx := context.Background()
	scope := DemandScope{HubID: "hub-one", DelegationID: "parent", NodeID: "node", BootID: "boot", SourceID: "funding", SnapshotID: "snapshot", RuleID: "rule", SignerEpoch: 1, RuleRevision: 1, PathGeneration: 1}
	use.AuthorizationID = "grant"
	use.Kind = "credit_grant"
	use.BillingSnapshotID = "snapshot"
	now := IssuerTime{Earliest: use.Earliest, Latest: use.Latest, Trusted: true}
	read := func() DemandLedger {
		t.Helper()
		l, e := s.ReadDemandLedger(ctx, scope, now, time.Now())
		if e != nil {
			t.Fatal(e)
		}
		return l
	}
	initial := read()
	if initial.Available != 1<<20 || initial.MinuteBytes != 0 {
		t.Fatalf("initial %+v", initial)
	}
	known := runtimeReservation("known", 1000)
	if _, err := s.ReserveAuthorizedUsage(ctx, known, use); err != nil {
		t.Fatal(err)
	}
	if got := read(); got.Available != (1<<20)-1000 || got.MinuteBytes != 0 {
		t.Fatalf("reservation became rate: %+v", got)
	}
	for i := 0; i < 2; i++ {
		if err := s.SettleUsageReservation(ctx, known, "committed", 600, []byte("actual"), 0); err != nil {
			t.Fatal(err)
		}
	}
	unknown := runtimeReservation("unknown", 1000)
	if _, err := s.ReserveAuthorizedUsage(ctx, unknown, use); err != nil {
		t.Fatal(err)
	}
	if err := s.SettleUsageReservation(ctx, unknown, "unknown", 1000, []byte("unknown"), 1000); err != nil {
		t.Fatal(err)
	}
	if got := read(); got.Available != (1<<20)-1600 || got.MinuteBytes != 600 {
		t.Fatalf("unknown/replay distorted demand: %+v", got)
	}
	for _, mutate := range []func(*DemandScope){func(s *DemandScope) { s.HubID = "other" }, func(s *DemandScope) { s.SourceID = "other" }, func(s *DemandScope) { s.SnapshotID = "other" }, func(s *DemandScope) { s.PathGeneration++ }, func(s *DemandScope) { s.SignerEpoch++ }, func(s *DemandScope) { s.BootID = "other" }, func(s *DemandScope) { s.DelegationID = "other" }} {
		altered := scope
		mutate(&altered)
		got, err := s.ReadDemandLedger(ctx, altered, now, time.Now())
		if err != nil || got != (DemandLedger{}) {
			t.Fatalf("scope leaked %+v: %+v %v", altered, got, err)
		}
	}
	expired, err := s.ReadDemandLedger(ctx, scope, now, time.Now().Add(time.Minute))
	if err != nil || expired.MinuteBytes != 0 || expired.Available != (1<<20)-1600 {
		t.Fatalf("rate expiration changed funds: %+v %v", expired, err)
	}
	now.Trusted = false
	if _, err := s.ReadDemandLedger(ctx, scope, now, time.Now()); err == nil {
		t.Fatal("untrusted authority balance returned")
	}
}

func TestDemandConsumptionFailureRollsBackSettlement(t *testing.T) {
	s, use, _ := runtimeStore(t)
	ctx := context.Background()
	use.AuthorizationID = "grant"
	use.Kind = "credit_grant"
	use.BillingSnapshotID = "snapshot"
	r := runtimeReservation("write-failure", 1000)
	if _, err := s.ReserveAuthorizedUsage(ctx, r, use); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec(`CREATE TRIGGER fail_demand_rate BEFORE INSERT ON demand_consumption BEGIN SELECT RAISE(ABORT,'rate disk failure'); END`); err != nil {
		t.Fatal(err)
	}
	if err := s.SettleUsageReservation(ctx, r, "committed", 600, []byte("actual"), 0); err == nil {
		t.Fatal("partial settlement acknowledged")
	}
	b, err := s.AuditAuthorizationSource(ctx, "node", "funding")
	if err != nil || b.Reserved != 1000 || b.Committed != 0 || b.Remaining != (1<<20)-1000 {
		t.Fatalf("partial settlement %+v %v", b, err)
	}
}

func TestDemandConsumptionUsesBoundedDurableWindow(t *testing.T) {
	s, use, _ := runtimeStore(t)
	ctx := context.Background()
	start := time.Unix(1800000000, 0)
	for i := 0; i < 121; i++ {
		tx, err := s.db.BeginTx(ctx, nil)
		if err != nil {
			t.Fatal(err)
		}
		if err := recordDemandConsumptionTx(ctx, tx, "node", "grant", 100, start.Add(time.Duration(i)*time.Second)); err != nil {
			tx.Rollback()
			t.Fatal(err)
		}
		if err := tx.Commit(); err != nil {
			t.Fatal(err)
		}
	}
	var count int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM demand_consumption`).Scan(&count); err != nil || count != 60 {
		t.Fatalf("unbounded sample rows: %d %v", count, err)
	}
	scope := DemandScope{HubID: "hub-one", DelegationID: "parent", NodeID: "node", BootID: "boot", SourceID: "funding", SnapshotID: "snapshot", RuleID: "rule", SignerEpoch: 1, RuleRevision: 1, PathGeneration: 1}
	got, err := s.ReadDemandLedger(ctx, scope, IssuerTime{Earliest: use.Earliest, Latest: use.Latest, Trusted: true}, start.Add(120*time.Second))
	if err != nil || got.MinuteBytes != 6000 {
		t.Fatalf("rolling window: %+v %v", got, err)
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if err := recordDemandConsumptionTx(ctx, tx, "node", "grant", 1, start); err == nil {
		t.Fatal("clock rollback accepted into durable window")
	}
}

func TestDemandLedgerZeroBalanceKeepsUnknownFrozen(t *testing.T) {
	s, use, _ := runtimeStore(t)
	ctx := context.Background()
	use.AuthorizationID, use.Kind, use.BillingSnapshotID = "grant", "credit_grant", "snapshot"
	r := runtimeReservation("exhaust-to-unknown", 1<<20)
	if _, err := s.ReserveAuthorizedUsage(ctx, r, use); err != nil {
		t.Fatal(err)
	}
	if err := s.SettleUsageReservation(ctx, r, "unknown", 1<<20, []byte("unknown"), 1<<20); err != nil {
		t.Fatal(err)
	}
	scope := DemandScope{HubID: "hub-one", DelegationID: "parent", NodeID: "node", BootID: "boot", SourceID: "funding", SnapshotID: "snapshot", RuleID: "rule", SignerEpoch: 1, RuleRevision: 1, PathGeneration: 1}
	got, err := s.ReadDemandLedger(ctx, scope, IssuerTime{Earliest: use.Earliest, Latest: use.Latest, Trusted: true}, time.Now())
	if err != nil || got != (DemandLedger{}) {
		t.Fatalf("unknown became demand credit: %+v %v", got, err)
	}
	b, err := s.AuditAuthorizationSource(ctx, "node", "funding")
	if err != nil || b.Initial != 1<<20 || b.Unknown != 1<<20 || b.Remaining != 0 || b.Reserved != 0 || b.Committed != 0 {
		t.Fatalf("read changed liabilities: %+v %v", b, err)
	}
}
