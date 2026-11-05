package store

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"vortex/agent/identity"
)

func TestDurableEventSurvivesReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "agent.db")
	ctx := context.Background()
	s, err := Open(path, "agent")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.AppendEvent(ctx, "evt-1", "hello", []byte("payload")); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = Open(path, "agent")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	n, err := s.EventCount(ctx)
	if err != nil || n != 1 {
		t.Fatalf("count=%d err=%v", n, err)
	}
	i, err := s.Info(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if i.JournalMode != "wal" || i.Synchronous != "2" {
		t.Fatalf("unexpected sqlite settings: %+v", i)
	}
}

func TestUsageOutboxSurvivesRestartAndLocalAckRetainsHandoff(t *testing.T) {
	path := filepath.Join(t.TempDir(), "agent.db")
	ctx := context.Background()
	batch := UsageBatch{NodeID: "node-1", BootID: "boot-1", BatchID: "batch-1", SnapshotID: "snapshot-1", Payload: []byte("usage"), UnknownUpper: 9876543210}
	s, err := Open(path, "agent")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.PersistUsageOutbox(ctx, batch); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = Open(path, "agent")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	pending, err := s.PendingUsageOutbox(ctx, "node-1")
	if err != nil || len(pending) != 1 || pending[0].UnknownUpper != batch.UnknownUpper || string(pending[0].Payload) != "usage" {
		t.Fatalf("reopened outbox=%+v err=%v", pending, err)
	}
	if err := s.MarkUsageLocallyAcknowledged(ctx, "node-1", []string{"batch-1"}); err != nil {
		t.Fatal(err)
	}
	pending, err = s.PendingUsageOutbox(ctx, "node-1")
	if err != nil || len(pending) != 1 || pending[0].State != "locally_acked" {
		t.Fatalf("local ACK discarded handoff batch: %+v err=%v", pending, err)
	}
	if err := s.AcknowledgeUsageBatches(ctx, "node-1", []string{"batch-1"}); err != nil {
		t.Fatal(err)
	}
	if pending, err = s.PendingUsageOutbox(ctx, "node-1"); err != nil || len(pending) != 0 {
		t.Fatalf("Hub ACK did not close outbox: %+v err=%v", pending, err)
	}
}

func TestUsageBatchIdentityIsIdempotentAndPayloadBound(t *testing.T) {
	ctx := context.Background()
	s, err := Open(filepath.Join(t.TempDir(), "hub.db"), "hub")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	batch := UsageBatch{NodeID: "node-1", BootID: "boot-1", BatchID: "batch-1", SnapshotID: "snapshot-1", Payload: []byte("usage")}
	if err := s.PersistUsageLedger(ctx, batch); err != nil {
		t.Fatal(err)
	}
	// A replay from a later boot is the same liability if the immutable bytes match.
	batch.BootID = "boot-2"
	if err := s.PersistUsageLedger(ctx, batch); err != nil {
		t.Fatalf("identical usage replay conflicted: %v", err)
	}
	batch.Payload = []byte("different")
	if err := s.PersistUsageLedger(ctx, batch); !errors.Is(err, ErrUsageBatchConflict) {
		t.Fatalf("same batch ID with different payload err=%v", err)
	}
}

func TestUsageOutboxWriteFailureCannotBeSentOrAcknowledged(t *testing.T) {
	ctx := context.Background()
	s, err := Open(filepath.Join(t.TempDir(), "agent.db"), "agent")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if _, err := s.db.Exec(`CREATE TRIGGER reject_usage BEFORE INSERT ON usage_batches BEGIN SELECT RAISE(ABORT, 'simulated disk write failure'); END`); err != nil {
		t.Fatal(err)
	}
	batch := UsageBatch{NodeID: "node-1", BootID: "boot-1", BatchID: "batch-1", SnapshotID: "snapshot-1", Payload: []byte("usage")}
	if err := s.PersistUsageOutbox(ctx, batch); err == nil {
		t.Fatal("outbox persistence succeeded despite injected write failure")
	}
	if pending, err := s.PendingUsageOutbox(ctx, "node-1"); err != nil || len(pending) != 0 {
		t.Fatalf("uncommitted batch became sendable: %+v err=%v", pending, err)
	}
	if err := s.MarkUsageLocallyAcknowledged(ctx, "node-1", []string{"batch-1"}); err == nil {
		t.Fatal("unpersisted batch was acknowledged locally")
	}
}

func TestUsageReservationIsDurableAndSettlementCreatesOutboxAtomically(t *testing.T) {
	ctx := context.Background()
	s, err := Open(filepath.Join(t.TempDir(), "agent.db"), "agent")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	r := UsageReservation{NodeID: "node-1", BootID: "boot-1", BatchID: "batch-1", SnapshotID: "snapshot-1", FlowKey: "flow-1", RuleID: "rule-1", Direction: "egress", Reserved: 64}
	if err := s.ReserveUsage(ctx, r); err != nil {
		t.Fatal(err)
	}
	if pending, err := s.PendingUsageOutbox(ctx, "node-1"); err != nil || len(pending) != 0 {
		t.Fatalf("unsettled reservation was exposed as usage: %+v err=%v", pending, err)
	}
	payload := []byte("settled-usage")
	if _, err := s.db.Exec(`CREATE TRIGGER reject_settled_outbox BEFORE INSERT ON usage_batches BEGIN SELECT RAISE(ABORT, 'simulated outbox failure'); END`); err != nil {
		t.Fatal(err)
	}
	if err := s.SettleUsageReservation(ctx, r, "committed", 20, payload, 0); err == nil {
		t.Fatal("settlement succeeded despite outbox write failure")
	}
	var state string
	if err := s.db.QueryRow(`SELECT state FROM usage_reservations WHERE node_id=? AND batch_id=?`, r.NodeID, r.BatchID).Scan(&state); err != nil || state != "reserved" {
		t.Fatalf("failed outbox insert released reservation: state=%q err=%v", state, err)
	}
	if _, err := s.db.Exec(`DROP TRIGGER reject_settled_outbox`); err != nil {
		t.Fatal(err)
	}
	if err := s.SettleUsageReservation(ctx, r, "committed", 20, payload, 0); err != nil {
		t.Fatal(err)
	}
	if err := s.SettleUsageReservation(ctx, r, "committed", 20, payload, 0); err != nil {
		t.Fatalf("identical settlement retry was not idempotent: %v", err)
	}
	if err := s.SettleUsageReservation(ctx, r, "committed", 20, []byte("different-payload"), 0); !errors.Is(err, ErrUsageBatchConflict) {
		t.Fatalf("changed settled payload was accepted: %v", err)
	}
	if pending, err := s.PendingUsageOutbox(ctx, "node-1"); err != nil || len(pending) != 1 || string(pending[0].Payload) != string(payload) {
		t.Fatalf("settled outbox=%+v err=%v", pending, err)
	}
}

func TestAuthorizationUpdateFencesEpochsAndIsPayloadBound(t *testing.T) {
	ctx := context.Background()
	s, err := Open(filepath.Join(t.TempDir(), "agent.db"), "agent")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	u := AuthorizationUpdate{NodeID: "node-1", BootID: "boot-1", UpdateID: "update-1", Payload: []byte("payload"), SignerEpoch: 2, PathGeneration: 4, FencingGenerationFloor: 8}
	if err := s.PersistAuthorizationUpdate(ctx, u, nil, nil); err != nil {
		t.Fatal(err)
	}
	if err := s.PersistAuthorizationUpdate(ctx, u, nil, nil); err != nil {
		t.Fatalf("identical update replay failed: %v", err)
	}
	u.Payload = []byte("changed")
	if err := s.PersistAuthorizationUpdate(ctx, u, nil, nil); !errors.Is(err, ErrAuthorizationUpdateConflict) {
		t.Fatalf("changed replay err=%v", err)
	}
	u.UpdateID = "update-2"
	u.SignerEpoch = 1
	if err := s.PersistAuthorizationUpdate(ctx, u, nil, nil); !errors.Is(err, ErrAuthorizationGenerationStale) {
		t.Fatalf("stale signer epoch err=%v", err)
	}
}

func TestReceiptIsIdempotent(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "hub.db"), "hub")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := s.RecordReceipt(context.Background(), "m-1", 1); err != nil {
		t.Fatal(err)
	}
	if err := s.RecordReceipt(context.Background(), "m-1", 1); err != nil {
		t.Fatal(err)
	}
}

func TestControlSequenceAndStateSurviveReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "agent.db")
	s, err := Open(path, "agent")
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if sequence, err := s.NextControlSequence(ctx); err != nil || sequence != 1 {
		t.Fatalf("sequence=%d err=%v", sequence, err)
	}
	if err := s.PersistControlAck(ctx, "ack-1", 1, 7, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = Open(path, "agent")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if sequence, err := s.NextControlSequence(ctx); err != nil || sequence != 2 {
		t.Fatalf("reopened sequence=%d err=%v", sequence, err)
	}
	if version, err := s.AgentState(ctx, "applied_version"); err != nil || version != "7" {
		t.Fatalf("version=%q err=%v", version, err)
	}
}

func TestControlReceiptCommitsHeartbeatAndRejectsSequenceConflict(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "agent.db"), "agent")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()
	heartbeat := time.Now().UTC()
	if err := s.PersistControlReceipt(ctx, "message-1", 4, heartbeat); err != nil {
		t.Fatal(err)
	}
	if err := s.PersistControlReceipt(ctx, "message-1", 4, heartbeat); err != nil {
		t.Fatalf("same receipt retry should be idempotent: %v", err)
	}
	if err := s.PersistControlReceipt(ctx, "message-1", 5, heartbeat); err == nil {
		t.Fatal("same message ID with conflicting sequence was accepted")
	}
	if _, err := s.AgentState(ctx, "hub_contact"); err != nil {
		t.Fatalf("Hub contact not persisted with receipt: %v", err)
	}
	if _, err := s.AgentState(ctx, "backend_heartbeat"); err == nil {
		t.Fatal("Hub receipt must not advance backend heartbeat")
	}
	if _, err := s.AgentState(ctx, "applied_version"); err == nil {
		t.Fatal("control receipt must not advance applied config version")
	}
}

func TestRevokedCredentialCannotBeReplacedByOrdinarySave(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "agent.db"), "agent")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()
	if err := s.SaveCredential(ctx, "node-1", []byte("old"), time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err := s.RevokeCredential(ctx, "node-1"); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveCredential(ctx, "node-1", []byte("replacement"), time.Now().Add(time.Hour)); err != ErrCredentialRevoked {
		t.Fatalf("ordinary credential save cleared revocation: %v", err)
	}
	if _, _, err := s.Credential(ctx, "node-1"); err != ErrCredentialRevoked {
		t.Fatalf("revoked credential became readable: %v", err)
	}
}

func TestRevokedCredentialCannotBeReactivatedByRotation(t *testing.T) {
	ctx := context.Background()
	s, err := Open(filepath.Join(t.TempDir(), "agent.db"), "agent")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	now := time.Now().UTC()
	ca, _, err := identity.NewCA(now)
	if err != nil {
		t.Fatal(err)
	}
	issue := func() identity.Credential {
		key, csr, err := identity.GenerateCSR("node-1")
		if err != nil {
			t.Fatal(err)
		}
		credential, err := ca.SignCSR(csr, "node-1", now, time.Hour)
		if err != nil {
			t.Fatal(err)
		}
		credential, err = identity.AttachKey(credential, key, nil)
		if err != nil {
			t.Fatal(err)
		}
		return credential
	}
	oldCredential, nextCredential := issue(), issue()
	if err := s.SaveCredentialRotation(ctx, "node-1", oldCredential.Leaf.Raw, oldCredential.Leaf.NotAfter, nil, time.Time{}, time.Time{}); err != nil {
		t.Fatal(err)
	}
	if err := s.RevokeCredential(ctx, "node-1"); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveCredentialRotation(ctx, "node-1", nextCredential.Leaf.Raw, nextCredential.Leaf.NotAfter, oldCredential.Leaf.Raw, oldCredential.Leaf.NotAfter, now.Add(5*time.Minute)); err != ErrCredentialRevoked {
		t.Fatalf("rotation reactivated revoked identity: %v", err)
	}
	if _, _, err := s.Credential(ctx, "node-1"); err != ErrCredentialRevoked {
		t.Fatalf("revoked identity became readable after rejected rotation: %v", err)
	}
}
