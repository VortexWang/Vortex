package store

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"vortex/contracts"
	controlv1 "vortex/contracts/protobuf/v1"
)

func unknownPayload(t *testing.T, id string) []byte {
	t.Helper()
	p, err := contracts.EncodeUnknownUsage(&controlv1.UnknownUsageBatch{BatchId: id, OriginBootId: "550e8400-e29b-41d4-a716-446655440001", BillingSnapshotId: "550e8400-e29b-41d4-a716-446655440002", GrantId: "550e8400-e29b-41d4-a716-446655440003", RuleId: "rule", Role: "source", Direction: "egress", UnknownUpperBound: 128})
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestUnknownSettlementIsAtomicAndAckNeverRefundsGrant(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "agent.db"), "agent")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()
	payload := unknownPayload(t, "held")
	b, _ := contracts.DecodeUnknownUsage(payload)
	now := time.Date(2026, 11, 5, 12, 0, 0, 0, time.UTC)
	a := AuthorizationRecord{AuthorizationID: b.GrantId, ParentAuthorizationID: "parent", SourceID: "source", Kind: "credit_grant", RuleID: b.RuleId, RuleRevision: 1, PathGeneration: 1, SignerEpoch: 1, BootID: b.OriginBootId, IssuedAt: now.Format(time.RFC3339Nano), NotBefore: now.Format(time.RFC3339Nano), ExpiresAt: now.Add(time.Minute).Format(time.RFC3339Nano), HardDeadline: now.Add(time.Hour).Format(time.RFC3339Nano), BillingSnapshotID: b.BillingSnapshotId, AmountBytes: 256}
	if err := s.PersistAuthorizationUpdate(ctx, AuthorizationUpdate{NodeID: "node", BootID: b.OriginBootId, UpdateID: "initial", Payload: []byte("auth"), SignerEpoch: 1, PathGeneration: 1, FencingGenerationFloor: 1}, []AuthorizationRecord{a}, nil); err != nil {
		t.Fatal(err)
	}
	u := AuthorizationUse{NodeID: "node", BootID: b.OriginBootId, AuthorizationID: b.GrantId, Kind: "credit_grant", RuleID: b.RuleId, RuleRevision: 1, PathGeneration: 1, BillingSnapshotID: b.BillingSnapshotId, Earliest: now, Latest: now, TrustedTime: true}
	r := UsageReservation{NodeID: "node", BootID: b.OriginBootId, AuthorizationID: b.GrantId, BatchID: b.BatchId, SnapshotID: b.BillingSnapshotId, RuleID: b.RuleId, FlowKey: "flow", Direction: b.Direction, Reserved: 128}
	if _, err := s.ReserveAuthorizedUsage(ctx, r, u); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec(`CREATE TRIGGER fail_unknown BEFORE INSERT ON unknown_usage_batches BEGIN SELECT RAISE(ABORT,'no disk'); END`); err != nil {
		t.Fatal(err)
	}
	if err := s.SettleUnknownUsageReservation(ctx, r, payload); err == nil {
		t.Fatal("failed unknown commit released reservation")
	}
	balance, err := s.AuditAuthorizationBalance(ctx, "node", b.GrantId)
	if err != nil || balance.Reserved != 128 || balance.Unknown != 0 || balance.Remaining != 128 {
		t.Fatalf("failed commit changed balance %+v %v", balance, err)
	}
	if _, err := s.db.Exec(`DROP TRIGGER fail_unknown`); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := s.SettleUnknownUsageReservation(ctx, r, payload); err != nil {
			t.Fatal(err)
		}
	}
	digest := sha256.Sum256(payload)
	if err := s.AcknowledgeUnknownUsage(ctx, "node", b.BatchId, digest[:]); err != nil {
		t.Fatal(err)
	}
	balance, err = s.AuditAuthorizationBalance(ctx, "node", b.GrantId)
	if err != nil || balance.Reserved != 0 || balance.Unknown != 128 || balance.Remaining != 128 || balance.Committed != 0 {
		t.Fatalf("ACK refunded/consumed unknown %+v %v", balance, err)
	}
}

func TestUnknownUsageCommitReplayAndTypeIsolation(t *testing.T) {
	ctx := context.Background()
	for _, role := range []string{"agent", "hub"} {
		t.Run(role, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), role+".db")
			s, err := Open(path, role)
			if err != nil {
				t.Fatal(err)
			}
			defer s.Close()
			payload := unknownPayload(t, "unknown")
			if _, err := s.db.Exec(`CREATE TRIGGER fail_unknown BEFORE INSERT ON unknown_usage_batches BEGIN SELECT RAISE(ABORT,'write failed'); END`); err != nil {
				t.Fatal(err)
			}
			if err := s.PersistUnknownUsage(ctx, "node", payload); err == nil {
				t.Fatal("failed commit accepted")
			}
			var count int
			_ = s.db.QueryRow(`SELECT COUNT(*) FROM unknown_usage_batches`).Scan(&count)
			if count != 0 {
				t.Fatal("failed commit left unknown record")
			}
			if _, err := s.db.Exec(`DROP TRIGGER fail_unknown`); err != nil {
				t.Fatal(err)
			}
			if err := s.PersistUnknownUsage(ctx, "node", payload); err != nil {
				t.Fatal(err)
			}
			reopened, err := Open(path, role)
			if err != nil {
				t.Fatal(err)
			}
			defer reopened.Close()
			if err := reopened.PersistUnknownUsage(ctx, "node", payload); err != nil {
				t.Fatal(err)
			}
			changed, _ := contracts.DecodeUnknownUsage(payload)
			changed.UnknownUpperBound++
			other, _ := contracts.EncodeUnknownUsage(changed)
			if err := s.PersistUnknownUsage(ctx, "node", other); !errors.Is(err, ErrUsageBatchConflict) {
				t.Fatal("changed unknown replay accepted", err)
			}
			if err := s.db.QueryRow(`SELECT COUNT(*) FROM unknown_usage_conflicts WHERE batch_id='unknown'`).Scan(&count); err != nil || count != 1 {
				t.Fatal("rejected payload evidence missing", err)
			}
			known := UsageBatch{NodeID: "node", BootID: "boot", BatchID: "unknown", SnapshotID: "snapshot", Payload: []byte("known")}
			if err := s.PersistUsageLedger(ctx, known); !errors.Is(err, ErrUsageBatchConflict) {
				t.Fatal("unknown became actual usage", err)
			}
			known.BatchID = "known"
			if err := s.PersistUsageLedger(ctx, known); err != nil {
				t.Fatal(err)
			}
			if err := s.PersistUnknownUsage(ctx, "node", unknownPayload(t, "known")); !errors.Is(err, ErrUsageBatchConflict) {
				t.Fatal("actual usage became unknown", err)
			}
			if role == "agent" {
				digest := sha256.Sum256(payload)
				bad := digest
				bad[0] ^= 255
				if err := s.AcknowledgeUnknownUsage(ctx, "node", "unknown", bad[:]); err == nil {
					t.Fatal("wrong hash acknowledged")
				}
				if err := s.AcknowledgeUnknownUsage(ctx, "node", "known", digest[:]); err == nil {
					t.Fatal("wrong type acknowledged")
				}
				if err := s.AcknowledgeUsageBatches(ctx, "node", []string{"unknown"}); err == nil {
					t.Fatal("ordinary ACK accepted unknown")
				}
				for range 2 {
					if err := s.AcknowledgeUnknownUsage(ctx, "node", "unknown", digest[:]); err != nil {
						t.Fatal(err)
					}
				}
				if pending, err := s.PendingUnknownUsage(ctx, "node"); err != nil || len(pending) != 0 {
					t.Fatal("ACK failed to stop retries", err)
				}
				var liability, upper string
				if err := s.db.QueryRow(`SELECT liability_state,unknown_upper_bound FROM unknown_usage_batches WHERE batch_id='unknown'`).Scan(&liability, &upper); err != nil || liability != "unresolved" || upper != "128" {
					t.Fatal("ACK released liability", err)
				}
			}
		})
	}
}

func TestLegacyUnknownMigrationPreservesBeforeImageAndNeverReplaysAcknowledged(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "agent.db"), "agent")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()
	payload := unknownPayload(t, "legacy")
	b, _ := contracts.DecodeUnknownUsage(payload)
	legacy := UsageBatch{NodeID: "node", BootID: b.OriginBootId, BatchID: b.BatchId, SnapshotID: b.BillingSnapshotId, Payload: []byte("historical-wire-evidence"), UnknownUpper: 128}
	if err := s.PersistUsageOutbox(ctx, legacy); err != nil {
		t.Fatal(err)
	}
	rows, _ := s.PendingUsageOutbox(ctx, "node")
	legacy = rows[0]
	for range 2 {
		if err := s.MigrateLegacyUnknownUsage(ctx, legacy, payload); err != nil {
			t.Fatal(err)
		}
	}
	pending, err := s.PendingUnknownUsage(ctx, "node")
	if err != nil || len(pending) != 1 || !bytes.Equal(pending[0].LegacyBeforeImage, legacy.Payload) {
		t.Fatal("legacy evidence changed", err)
	}
	// Historical ordinary ACKs remain quarantined. Do not turn one accepted
	// financial identity into a second financial event on the upgraded wire.
	payload = unknownPayload(t, "old-acked")
	legacy.BatchID = "old-acked"
	if err := s.PersistUsageOutbox(ctx, legacy); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec(`UPDATE usage_batches SET state='hub_acked' WHERE batch_id='old-acked'`); err != nil {
		t.Fatal(err)
	}
	if err := s.MigrateLegacyUnknownUsage(ctx, legacy, payload); !errors.Is(err, ErrUsageBatchConflict) {
		t.Fatal("previously acknowledged liability resent", err)
	}
}
