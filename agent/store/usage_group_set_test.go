package store

import (
	"context"
	"crypto/sha256"
	"fmt"
	"path/filepath"
	"testing"

	"google.golang.org/protobuf/proto"
	controlv1 "vortex/contracts/protobuf/v1"
)

func knownGroupFixture(t *testing.T, node, id string) HubUsage {
	t.Helper()
	payload, err := proto.Marshal(&controlv1.UsageBatch{BatchId: id, BillingSnapshotId: "snapshot"})
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(payload)
	return HubUsage{NodeID: node, BootID: "boot", BatchID: id, Kind: "known", Payload: payload, Digest: digest[:]}
}

func TestKnownUsageSetLastMemberFailureRollsBackEveryRow(t *testing.T) {
	for _, fault := range []string{"abort", "ignore", "unknown", "conflict"} {
		t.Run(fault, func(t *testing.T) {
			s, err := Open(filepath.Join(t.TempDir(), "hub.db"), "hub")
			if err != nil {
				t.Fatal(err)
			}
			defer s.Close()
			ctx := context.Background()
			var batches []HubUsage
			for i := 0; i < 32; i++ {
				batches = append(batches, knownGroupFixture(t, "node", fmt.Sprintf("batch-%02d", i)))
			}
			want := 0
			switch fault {
			case "abort", "ignore":
				body := "RAISE(ABORT,'disk error')"
				if fault == "ignore" {
					body = "RAISE(IGNORE)"
				}
				if _, err := s.db.Exec(`CREATE TRIGGER reject_last_usage BEFORE INSERT ON usage_batches WHEN NEW.batch_id='batch-31' BEGIN SELECT ` + body + `; END`); err != nil {
					t.Fatal(err)
				}
			case "unknown":
				if err := s.PersistUnknownUsage(ctx, "node", unknownPayload(t, "batch-31")); err != nil {
					t.Fatal(err)
				}
			case "conflict":
				if _, err := s.PersistKnownUsageGroup(ctx, batches[31:], false); err != nil {
					t.Fatal(err)
				}
				want = 1
				payload, err := proto.Marshal(&controlv1.UsageBatch{BatchId: "batch-31", BillingSnapshotId: "different"})
				if err != nil {
					t.Fatal(err)
				}
				digest := sha256.Sum256(payload)
				batches[31].Payload, batches[31].Digest = payload, digest[:]
			}
			if got, err := s.PersistKnownUsageGroup(ctx, batches, false); err == nil || got != nil {
				t.Fatal("failed group exposed usage", got, err)
			}
			var count int
			if err := s.db.QueryRow(`SELECT COUNT(*) FROM usage_batches`).Scan(&count); err != nil || count != want {
				t.Fatal("partial group survived", count, err)
			}
			if fault == "unknown" {
				var state, upper string
				if err := s.db.QueryRow(`SELECT liability_state,unknown_upper_bound FROM unknown_usage_batches`).Scan(&state, &upper); err != nil || state != "unresolved" || upper != "128" {
					t.Fatal("unknown changed", state, upper, err)
				}
			}
		})
	}
}

func TestKnownUsageSetScopesDuplicatesCompactionAndReceipts(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "hub.db"), "hub")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()
	a := knownGroupFixture(t, "one", `same'quoted"id`)
	b := knownGroupFixture(t, "two", a.BatchID)
	compact := knownGroupFixture(t, "one", "compacted")
	if _, err := s.db.Exec(`INSERT INTO settled_usage_tombstones(node_id,batch_id,snapshot_id,payload_sha256,ledger_sequence) VALUES(?,?,?,?,?)`, compact.NodeID, compact.BatchID, "snapshot", compact.Digest, 1); err != nil {
		t.Fatal(err)
	}
	batches := []HubUsage{a, b, a, compact}
	for range 2 {
		if _, err := s.PersistKnownUsageGroup(ctx, batches, false); err != nil {
			t.Fatal(err)
		}
	}
	var count int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM usage_batches`).Scan(&count); err != nil || count != 2 {
		t.Fatal("duplicate or compact replay recreated usage", count, err)
	}
	missing := knownGroupFixture(t, "three", a.BatchID)
	if err := s.ConfirmBackendUsageGroup(ctx, []HubUsage{a, missing}); err == nil {
		t.Fatal("missing scoped payload confirmed")
	}
	bad := b
	bad.Digest = []byte("forged")
	if err := s.ConfirmBackendUsageGroup(ctx, []HubUsage{a, bad}); err == nil {
		t.Fatal("invalid late digest confirmed prefix")
	}
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM usage_backend_receipts`).Scan(&count); err != nil || count != 0 {
		t.Fatal("partial confirmation", count, err)
	}
	if err := s.ConfirmBackendUsageGroup(ctx, []HubUsage{a, a, compact}); err != nil {
		t.Fatal(err)
	}
	pending, err := s.PendingKnownUsageGroup(ctx, batches)
	if err != nil || len(pending) != 1 || pending[0].NodeID != "two" {
		t.Fatal("receipt crossed node scope or compaction", pending, err)
	}
	if _, err := s.PendingKnownUsageGroup(ctx, []HubUsage{{NodeID: a.NodeID, BatchID: a.BatchID, Kind: "known", Digest: []byte("forged")}}); err == nil {
		t.Fatal("changed digest suppressed delivery")
	}
	if err := s.ConfirmBackendUsageGroup(ctx, batches); err != nil {
		t.Fatal(err)
	}
	pending, err = s.PendingKnownUsageGroup(ctx, batches)
	if err != nil || len(pending) != 0 {
		t.Fatal("exact replay remained pending", pending, err)
	}
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM usage_backend_receipts`).Scan(&count); err != nil || count != 2 {
		t.Fatal("duplicate or compact receipt inserted", count, err)
	}
}
