package store

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"
	"time"
)

func fundedStore(t *testing.T) (*Store, AuthorizationUse) {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "agent.db"), "agent")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	now := time.Date(2026, 11, 5, 12, 0, 0, 0, time.UTC)
	r := AuthorizationRecord{AuthorizationID: "grant", ParentAuthorizationID: "parent", SourceID: "source", Kind: "credit_grant", RuleID: "rule", RuleRevision: 1, PathGeneration: 1, SignerEpoch: 1, BootID: "boot", IssuedAt: now.Format(time.RFC3339Nano), NotBefore: now.Format(time.RFC3339Nano), ExpiresAt: now.Add(5 * time.Minute).Format(time.RFC3339Nano), HardDeadline: now.Add(time.Hour).Format(time.RFC3339Nano), BillingSnapshotID: "snapshot", AmountBytes: 4096}
	if err := s.PersistAuthorizationUpdate(context.Background(), AuthorizationUpdate{IssuerID: "hub-one", NodeID: "node", BootID: "boot", UpdateID: "update", Payload: []byte("approved-fixture"), SignerEpoch: 1, PathGeneration: 1, FencingGenerationFloor: 1}, []AuthorizationRecord{r}, nil); err != nil {
		t.Fatal(err)
	}
	return s, AuthorizationUse{NodeID: "node", BootID: "boot", AuthorizationID: "grant", Kind: "credit_grant", RuleID: "rule", RuleRevision: 1, PathGeneration: 1, BillingSnapshotID: "snapshot", Earliest: now.Add(time.Second), Latest: now.Add(time.Second), TrustedTime: true, ExistingSession: true}
}

func TestAuthorizedReservationRollbackOnDiskWriteFailure(t *testing.T) {
	s, use := fundedStore(t)
	ctx := context.Background()
	if _, err := s.db.Exec(`CREATE TRIGGER fail_authority_hold BEFORE INSERT ON authorization_usage_holds BEGIN SELECT RAISE(ABORT, 'injected disk write failure'); END`); err != nil {
		t.Fatal(err)
	}
	r := UsageReservation{AuthorizationID: "grant", NodeID: "node", BootID: "boot", BatchID: "hold", SnapshotID: "snapshot", FlowKey: "flow", RuleID: "rule", Direction: "egress", Reserved: 1000}
	if _, err := s.ReserveAuthorizedUsage(ctx, r, use); err == nil {
		t.Fatal("write failure returned a writable reservation")
	}
	balance, err := s.AuditAuthorizationSource(ctx, "node", "source")
	if err != nil || balance.Remaining != 4096 || balance.Reserved != 0 {
		t.Fatalf("rollback balance=%+v err=%v", balance, err)
	}
	var count int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM usage_reservations`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("partial prewrite record=%d err=%v", count, err)
	}
}

func TestAuthorizedSettlementFailureRetainsWholeUpperBound(t *testing.T) {
	s, use := fundedStore(t)
	ctx := context.Background()
	r := UsageReservation{AuthorizationID: "grant", NodeID: "node", BootID: "boot", BatchID: "hold", SnapshotID: "snapshot", FlowKey: "flow", RuleID: "rule", Direction: "egress", Reserved: 1000}
	if _, err := s.ReserveAuthorizedUsage(ctx, r, use); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec(`CREATE TRIGGER fail_outbox BEFORE INSERT ON usage_batches BEGIN SELECT RAISE(ABORT, 'injected disk write failure'); END`); err != nil {
		t.Fatal(err)
	}
	if err := s.SettleUsageReservation(ctx, r, "committed", 300, []byte("usage"), 0); err == nil {
		t.Fatal("failed outbox acknowledged settlement")
	}
	balance, err := s.AuditAuthorizationSource(ctx, "node", "source")
	if err != nil || balance.Remaining != 3096 || balance.Reserved != 1000 || balance.Committed != 0 {
		t.Fatalf("failed settlement released upper bound=%+v err=%v", balance, err)
	}
	if _, err := s.db.Exec(`DROP TRIGGER fail_outbox`); err != nil {
		t.Fatal(err)
	}
	if err := s.SettleUsageReservation(ctx, r, "unknown", 1000, []byte("unknown"), 1000); err != nil {
		t.Fatal(err)
	}
	balance, err = s.AuditAuthorizationSource(ctx, "node", "source")
	if err != nil || balance.Unknown != 1000 || balance.Remaining != 3096 {
		t.Fatalf("recovered unknown balance=%+v err=%v", balance, err)
	}
}

func TestAuthorizedReservationDuplicatePayloadAndConservation(t *testing.T) {
	s, use := fundedStore(t)
	ctx := context.Background()
	for i := 0; i < 16; i++ {
		r := UsageReservation{AuthorizationID: "grant", NodeID: "node", BootID: "boot", BatchID: fmt.Sprintf("hold-%d", i), SnapshotID: "snapshot", FlowKey: "flow", RuleID: "rule", Direction: "egress", Reserved: 128}
		for retry := 0; retry < 2; retry++ {
			if _, err := s.ReserveAuthorizedUsage(ctx, r, use); err != nil {
				t.Fatal(err)
			}
		}
		changed := r
		changed.FlowKey = "changed"
		if _, err := s.ReserveAuthorizedUsage(ctx, changed, use); err == nil {
			t.Fatal("changed reservation replay accepted")
		}
		resolution, used, payload, unknown := "committed", uint64(i), []byte(fmt.Sprintf("usage-%d", i)), uint64(0)
		if i%3 == 0 {
			resolution, used, payload, unknown = "unknown", 128, []byte("unknown"), 128
		} else if i%3 == 1 {
			resolution, used, payload = "returned", 0, nil
		}
		for retry := 0; retry < 2; retry++ {
			if err := s.SettleUsageReservation(ctx, r, resolution, used, payload, unknown); err != nil {
				t.Fatal(err)
			}
		}
		if _, err := s.AuditAuthorizationSource(ctx, "node", "source"); err != nil {
			t.Fatalf("step %d: %v", i, err)
		}
	}
}

func TestAuthorizationAuditRejectsOverflow(t *testing.T) {
	s, use := fundedStore(t)
	ctx := context.Background()
	for i := 0; i < 2; i++ {
		id := fmt.Sprintf("corrupt-%d", i)
		_, err := s.db.Exec(`INSERT INTO usage_reservations(node_id,boot_id,batch_id,snapshot_id,flow_key,rule_id,direction,reserved_bytes,state,updated_at) VALUES('node','boot',?,'snapshot','flow','rule','egress','18446744073709551615','reserved','now')`, id)
		if err != nil {
			t.Fatal(err)
		}
		_, err = s.db.Exec(`INSERT INTO authorization_usage_holds(node_id,batch_id,authorization_id,source_id,reserved_bytes,state) VALUES('node',?,'grant','source','18446744073709551615','reserved')`, id)
		if err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.AuditAuthorizationBalance(ctx, use.NodeID, use.AuthorizationID); err == nil {
		t.Fatal("overflowing grant audit passed")
	}
	if _, err := s.AuditAuthorizationSource(ctx, use.NodeID, "source"); err == nil {
		t.Fatal("overflowing source audit passed")
	}
}

func TestDuplicateAuthorizedReservationCannotReviveRevokedGrant(t *testing.T) {
	s, use := fundedStore(t)
	ctx := context.Background()
	r := UsageReservation{AuthorizationID: "grant", NodeID: "node", BootID: "boot", BatchID: "hold", SnapshotID: "snapshot", FlowKey: "flow", RuleID: "rule", Direction: "egress", Reserved: 1000}
	if _, err := s.ReserveAuthorizedUsage(ctx, r, use); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec(`UPDATE authorization_records SET state='revoked' WHERE authorization_id='grant'`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ReserveAuthorizedUsage(ctx, r, use); err == nil {
		t.Fatal("duplicate hold revived revoked authority")
	}
	b, err := s.AuditAuthorizationSource(ctx, "node", "source")
	if err != nil || b.Reserved != 1000 || b.Remaining != 3096 {
		t.Fatalf("revocation released hold: %+v %v", b, err)
	}
}

func TestAuthorizedSettlementCannotChangeGrantIdentity(t *testing.T) {
	s, use := fundedStore(t)
	ctx := context.Background()
	r := UsageReservation{AuthorizationID: "grant", NodeID: "node", BootID: "boot", BatchID: "hold", SnapshotID: "snapshot", FlowKey: "flow", RuleID: "rule", Direction: "egress", Reserved: 1000}
	if _, err := s.ReserveAuthorizedUsage(ctx, r, use); err != nil {
		t.Fatal(err)
	}
	r.AuthorizationID = "another-grant"
	if err := s.SettleUsageReservation(ctx, r, "committed", 300, []byte("usage"), 0); err == nil {
		t.Fatal("changed settlement grant identity accepted")
	}
	b, err := s.AuditAuthorizationSource(ctx, "node", "source")
	if err != nil || b.Reserved != 1000 || b.Remaining != 3096 {
		t.Fatalf("invalid settlement released hold: %+v %v", b, err)
	}
}

func TestAuthorizationHeartbeatCommitsAtomicallyAndCannotExtendByReplay(t *testing.T) {
	s, _ := fundedStore(t)
	ctx := context.Background()
	base := time.Date(2026, 11, 5, 12, 0, 0, 0, time.UTC)
	update := AuthorizationUpdate{NodeID: "node", BootID: "boot", UpdateID: "heartbeat", Payload: []byte("heartbeat"), SignerEpoch: 1, PathGeneration: 1, FencingGenerationFloor: 1, BackendHeartbeat: base, AutonomyDeadline: base.Add(time.Hour)}
	if _, err := s.db.Exec(`CREATE TRIGGER fail_update BEFORE INSERT ON authorization_updates BEGIN SELECT RAISE(ABORT, 'write failure'); END`); err != nil {
		t.Fatal(err)
	}
	if err := s.PersistAuthorizationUpdate(ctx, update, nil, nil); err == nil {
		t.Fatal("failed update acknowledged heartbeat")
	}
	if _, err := s.AgentState(ctx, "backend_heartbeat"); err == nil {
		t.Fatal("heartbeat escaped failed authorization transaction")
	}
	if _, err := s.db.Exec(`DROP TRIGGER fail_update`); err != nil {
		t.Fatal(err)
	}
	if err := s.PersistAuthorizationUpdate(ctx, update, nil, nil); err != nil {
		t.Fatal(err)
	}
	status, err := s.CurrentAutonomyStatus(ctx, base.Add(30*time.Second), true)
	if err != nil || status.Mode != AutonomyOffline {
		t.Fatalf("status=%+v err=%v", status, err)
	}
	replay := update
	replay.UpdateID = "same-heartbeat"
	replay.Payload = []byte("same-heartbeat")
	if err := s.PersistAuthorizationUpdate(ctx, replay, nil, nil); err != nil {
		t.Fatal(err)
	}
	if status, err = s.CurrentAutonomyStatus(ctx, base.Add(time.Minute), true); err != nil || status.SignerEpochOpen {
		t.Fatalf("replay reopened signer: %+v %v", status, err)
	}
	replay.UpdateID = "extended"
	replay.Payload = []byte("extended")
	replay.AutonomyDeadline = base.Add(2 * time.Hour)
	if err := s.PersistAuthorizationUpdate(ctx, replay, nil, nil); err == nil {
		t.Fatal("replay extended fixed deadline")
	}
	replay.UpdateID = "new-heartbeat"
	replay.Payload = []byte("new-heartbeat")
	replay.BackendHeartbeat = base.Add(time.Minute)
	if err := s.PersistAuthorizationUpdate(ctx, replay, nil, nil); err == nil {
		t.Fatal("new heartbeat bypassed closed signer handoff")
	}
	status, err = s.CurrentAutonomyStatus(ctx, base.Add(time.Minute), true)
	if err != nil || status.HardDeadline != base.Add(time.Hour) {
		t.Fatalf("failed updates changed deadline: %+v %v", status, err)
	}
}
