package store

import (
	"context"
	"testing"
)

func TestGrantSealBlocksNewWritesButPreservesLateSettlement(t *testing.T) {
	s, scope, path := runtimeStore(t)
	ctx := context.Background()
	key := DemandScope{NodeID: "node", BootID: "boot", HubID: "hub-one", DelegationID: "parent", SourceID: "funding", SnapshotID: "snapshot", RuleID: "rule", RuleRevision: 1, PathGeneration: 1, SignerEpoch: 1}
	a, err := s.OpenRuntimeSession(ctx, scope, "session", "flow")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SealIdleGrant(ctx, key, "grant"); err == nil {
		t.Fatal("sealed live session grant")
	}
	pending := runtimeReservation("pending", 100)
	if _, err := s.ReserveRuntimeUsage(ctx, pending, a.Use); err != nil {
		t.Fatal(err)
	}
	if err := s.CloseRuntimeSession(ctx, "node", "boot", "session"); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if err := s.SealIdleGrant(ctx, key, "grant"); err != nil {
			t.Fatal(err)
		}
	}
	other := key
	other.SnapshotID = "other"
	if err := s.SealIdleGrant(ctx, other, "grant"); err == nil {
		t.Fatal("conflicting seal replay accepted")
	}
	reopened, err := Open(path, "agent")
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	if _, err := reopened.OpenRuntimeSession(ctx, scope, "new", "new-flow"); err == nil {
		t.Fatal("sealed grant admitted after reopen")
	}
	use := a.Use
	use.SessionID = ""
	use.RuntimeLeaseID = ""
	use.ShareLeaseID = ""
	if _, err := reopened.ReserveAuthorizedUsage(ctx, runtimeReservation("new-batch", 1), use); err == nil {
		t.Fatal("sealed grant allowed direct reserve")
	}
	before, err := reopened.AuditAuthorizationSource(ctx, "node", "funding")
	if err != nil || before.Reserved != 100 || before.Remaining != (1<<20)-100 {
		t.Fatalf("seal changed funds: %+v %v", before, err)
	}
	if err := reopened.SettleUsageReservation(ctx, pending, "unknown", 100, []byte("unknown"), 100); err != nil {
		t.Fatal(err)
	}
	after, err := reopened.AuditAuthorizationSource(ctx, "node", "funding")
	if err != nil || after.Unknown != 100 || after.Reserved != 0 || after.Remaining != before.Remaining {
		t.Fatalf("late settlement changed unknown ownership: %+v %v", after, err)
	}
}

func TestGrantSealWriteFailureRollsBackFence(t *testing.T) {
	s, scope, _ := runtimeStore(t)
	ctx := context.Background()
	key := DemandScope{NodeID: "node", BootID: "boot", HubID: "hub-one", DelegationID: "parent", SourceID: "funding", SnapshotID: "snapshot", RuleID: "rule", RuleRevision: 1, PathGeneration: 1, SignerEpoch: 1}
	if _, err := s.db.Exec(`CREATE TRIGGER fail_seal BEFORE INSERT ON sealed_grants BEGIN SELECT RAISE(ABORT,'seal disk failure'); END`); err != nil {
		t.Fatal(err)
	}
	if err := s.SealIdleGrant(ctx, key, "grant"); err == nil {
		t.Fatal("failed seal acknowledged")
	}
	if _, err := s.OpenRuntimeSession(ctx, scope, "still-active", "flow"); err != nil {
		t.Fatalf("partial fence persisted: %v", err)
	}
}

func TestGrantSealAndAdmissionCannotBothWin(t *testing.T) {
	for i := 0; i < 10; i++ {
		s, scope, _ := runtimeStore(t)
		ctx := context.Background()
		key := DemandScope{NodeID: "node", BootID: "boot", HubID: "hub-one", DelegationID: "parent", SourceID: "funding", SnapshotID: "snapshot", RuleID: "rule", RuleRevision: 1, PathGeneration: 1, SignerEpoch: 1}
		start := make(chan struct{})
		sealed := make(chan error, 1)
		opened := make(chan error, 1)
		go func() { <-start; sealed <- s.SealIdleGrant(ctx, key, "grant") }()
		go func() { <-start; _, err := s.OpenRuntimeSession(ctx, scope, "concurrent", "flow"); opened <- err }()
		close(start)
		sealErr, openErr := <-sealed, <-opened
		if sealErr == nil && openErr == nil {
			t.Fatal("both sealed and admitted")
		}
		if sealErr != nil && openErr != nil {
			t.Fatalf("neither operation won: seal=%v open=%v", sealErr, openErr)
		}
		var state string
		if err := s.db.QueryRow(`SELECT state FROM authorization_records WHERE authorization_id='grant'`).Scan(&state); err != nil {
			t.Fatal(err)
		}
		if (sealErr == nil) != (state == "settlement_only") {
			t.Fatalf("inconsistent winner: state=%s seal=%v", state, sealErr)
		}
	}
}
