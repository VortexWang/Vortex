package store

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"
)

func TestRecoveryBarrierSurvivesRestartAndTail(t *testing.T) {
	s, d, clock, path := issuerFixture(t)
	ctx := context.Background()
	h := HandoffRequest{ID: "550e8400-e29b-41d4-a716-446655440800", HubID: d.HubID, FromEpoch: 1, ToEpoch: 2, Heartbeat: d.BackendHeartbeat.Add(time.Minute)}
	clock.Earliest = h.Heartbeat
	clock.Latest = h.Heartbeat.Add(time.Second)
	if _, err := s.HandoffHub(ctx, h, clock); err != nil {
		t.Fatal(err)
	}
	assertMode := func(want string) {
		t.Helper()
		v, err := s.HubControlSnapshot(ctx, d.HubID, clock.Latest)
		if err != nil || v.Mode != want {
			t.Fatalf("status=%+v err=%v want=%s", v, err, want)
		}
	}
	assertMode(AutonomyRecovering)
	records, err := s.PendingLedger(ctx, 100)
	if err != nil || len(records) != 1 {
		t.Fatalf("records %v %v", records, err)
	}
	barrier := records[0]
	// Neither time liveness nor scope expansion can bypass recovery.
	if err = s.RecordBackendHeartbeat(ctx, h.Heartbeat.Add(time.Second), d.AutonomyDeadline.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	clock.Earliest = h.Heartbeat.Add(time.Second)
	clock.Latest = clock.Earliest.Add(time.Second)
	assertMode(AutonomyRecovering)
	if status, err := s.CurrentAutonomyStatus(ctx, clock.Latest, true); err != nil || status.AllowsNewAuthorization || !status.AllowsExistingRenewal || !status.HardDeadline.Equal(d.AutonomyDeadline) {
		t.Fatalf("status %+v %v", status, err)
	}
	d2 := d
	d2.ID = "550e8400-e29b-41d4-a716-446655440999"
	d2.SignerEpoch = 2
	d2.BackendHeartbeat = h.Heartbeat.Add(time.Second)
	if err = s.InstallHubDelegation(ctx, d2, clock); !errors.Is(err, ErrNewAuthorityInAutonomy) {
		t.Fatalf("expanded recovering delegation: %v", err)
	}
	for n := 0; n < 1002; n++ {
		if err = s.PersistUsageLedger(ctx, UsageBatch{NodeID: d.NodeID, BootID: d.BootID, BatchID: fmt.Sprint(n), SnapshotID: d.BillingSnapshotID, Payload: []byte(fmt.Sprint(n))}); err != nil {
			t.Fatal(err)
		}
	}
	if ok, err := s.CompleteRecovery(ctx, clock); err != nil || ok {
		t.Fatalf("lost ack completed %v %v", ok, err)
	}
	records, err = s.PendingLedger(ctx, 1000)
	if err != nil || len(records) != 1000 {
		t.Fatalf("bounded ledger %d %v", len(records), err)
	}
	if _, err = s.ConfirmLedger(ctx, records[1], records[1].Digest); err != nil {
		t.Fatal(err)
	}
	if ok, err := s.CompleteRecovery(ctx, clock); err != nil || ok {
		t.Fatalf("gap completed %v %v", ok, err)
	}
	if _, err = s.ConfirmLedger(ctx, barrier, []byte("wrong")); !errors.Is(err, ErrUsageBatchConflict) {
		t.Fatal("wrong hash accepted", err)
	}
	forged := barrier
	forged.Sequence++
	if _, err = s.ConfirmLedger(ctx, forged, forged.Digest); err == nil {
		t.Fatal("wrong sequence accepted")
	}
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = Open(path, "hub")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	assertMode(AutonomyRecovering)
	if _, err = s.HandoffHub(ctx, h, clock); err != nil {
		t.Fatal(err)
	}
	for n := 0; n < 2; n++ {
		if _, err = s.ConfirmLedger(ctx, barrier, barrier.Digest); err != nil {
			t.Fatal(err)
		}
	}
	badClock := clock
	badClock.Trusted = false
	if ok, err := s.CompleteRecovery(ctx, badClock); err == nil || ok {
		t.Fatal("untrusted completion", ok, err)
	}
	badClock = clock
	badClock.Earliest = h.Heartbeat.Add(-time.Second)
	if ok, err := s.CompleteRecovery(ctx, badClock); err == nil || ok {
		t.Fatal("rollback completion", ok, err)
	}
	badClock = clock
	badClock.Latest = d.AutonomyDeadline
	if ok, err := s.CompleteRecovery(ctx, badClock); err == nil || ok {
		t.Fatal("expired completion", ok, err)
	}
	if ok, err := s.CompleteRecovery(ctx, clock); err != nil || !ok {
		t.Fatal("barrier completion", ok, err)
	}
	assertMode(AutonomyOnline)
	status, err := s.HubControlSnapshot(ctx, d.HubID, clock.Latest)
	if err != nil || status.Pending != 1001 {
		t.Fatalf("tail falsely settled %+v %v", status, err)
	}
	balance, err := s.AuditHubDelegation(ctx, d.ID)
	if err != nil || balance.Remaining != 1024 {
		t.Fatalf("R changed %+v %v", balance, err)
	}
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = Open(path, "hub")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	assertMode(AutonomyOnline)
}

func TestRecoveryMigrationConservativelyCapturesHistoricalHandoff(t *testing.T) {
	s, d, clock, path := issuerFixture(t)
	ctx := context.Background()
	h := HandoffRequest{ID: "550e8400-e29b-41d4-a716-446655440800", HubID: d.HubID, FromEpoch: 1, ToEpoch: 2, Heartbeat: d.BackendHeartbeat.Add(time.Minute)}
	clock.Earliest = h.Heartbeat
	clock.Latest = h.Heartbeat.Add(time.Second)
	if _, err := s.HandoffHub(ctx, h, clock); err != nil {
		t.Fatal(err)
	}
	// Simulate the previous additive schema, which has no recovery witness.
	if _, err := s.db.Exec(`DROP TABLE hub_recoveries;DELETE FROM schema_meta WHERE key='recovery_mode_version';DELETE FROM agent_state WHERE key='recovery_handoff_id';UPDATE agent_state SET value='online' WHERE key='authority_mode'`); err != nil {
		t.Fatal(err)
	}
	s.Close()
	reopened, err := Open(path, "hub")
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	v, err := reopened.HubControlSnapshot(ctx, d.HubID, clock.Latest)
	if err != nil || v.Mode != AutonomyRecovering {
		t.Fatalf("historical online inferred %+v %v", v, err)
	}
	if ok, err := reopened.CompleteRecovery(ctx, clock); err != nil || ok {
		t.Fatal("historical ack inferred", ok, err)
	}
	balance, err := reopened.AuditHubDelegation(ctx, d.ID)
	if err != nil || balance.Remaining != 1024 {
		t.Fatalf("migration changed R %+v %v", balance, err)
	}
}

func TestRecoveryAtomicFailureAndConcurrentCompletion(t *testing.T) {
	s, d, clock, _ := issuerFixture(t)
	ctx := context.Background()
	h := HandoffRequest{ID: "550e8400-e29b-41d4-a716-446655440800", HubID: d.HubID, FromEpoch: 1, ToEpoch: 2, Heartbeat: d.BackendHeartbeat.Add(time.Minute)}
	clock.Earliest = h.Heartbeat
	clock.Latest = h.Heartbeat.Add(time.Second)
	if _, err := s.db.Exec(`CREATE TRIGGER fail_barrier BEFORE INSERT ON hub_recoveries BEGIN SELECT RAISE(ABORT,'disk write failure'); END`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.HandoffHub(ctx, h, clock); err == nil {
		t.Fatal("failed barrier committed")
	}
	var epoch string
	var n int
	if err := s.db.QueryRow(`SELECT signer_epoch FROM issuer_epochs WHERE hub_id=?`, d.HubID).Scan(&epoch); err != nil || epoch != "1" {
		t.Fatalf("partial epoch %s %v", epoch, err)
	}
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM hub_transfers`).Scan(&n); err != nil || n != 0 {
		t.Fatal("partial R transfer", n, err)
	}
	if _, err := s.db.Exec(`DROP TRIGGER fail_barrier`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.HandoffHub(ctx, h, clock); err != nil {
		t.Fatal(err)
	}
	records, err := s.PendingLedger(ctx, 100)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range records {
		if _, err = s.ConfirmLedger(ctx, r, r.Digest); err != nil {
			t.Fatal(err)
		}
	}
	if _, err = s.db.Exec(`CREATE TRIGGER fail_online BEFORE UPDATE ON agent_state WHEN NEW.key='authority_mode' AND NEW.value='online' BEGIN SELECT RAISE(ABORT,'disk write failure'); END`); err != nil {
		t.Fatal(err)
	}
	if ok, err := s.CompleteRecovery(ctx, clock); err == nil || ok {
		t.Fatal("failed completion committed", ok, err)
	}
	if err = s.db.QueryRow(`SELECT completed FROM hub_recoveries WHERE handoff_id=?`, h.ID).Scan(&n); err != nil || n != 0 {
		t.Fatal("partial completion", n, err)
	}
	if _, err = s.db.Exec(`DROP TRIGGER fail_online`); err != nil {
		t.Fatal(err)
	}
	results := make(chan bool, 2)
	errs := make(chan error, 2)
	for j := 0; j < 2; j++ {
		go func() { ok, err := s.CompleteRecovery(ctx, clock); results <- ok; errs <- err }()
	}
	successes := 0
	for j := 0; j < 2; j++ {
		if <-results {
			successes++
		}
		if err := <-errs; err != nil {
			t.Fatal(err)
		}
	}
	if successes != 1 {
		t.Fatal("nonunique completion", successes)
	}
}
