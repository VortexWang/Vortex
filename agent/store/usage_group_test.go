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

func TestKnownUsageGroupFULLRollbackReplayAndReceiptBoundary(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "hub.db")
	s, err := Open(path, "hub")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { s.Close() }()
	var batches []HubUsage
	for i := 0; i < 16; i++ {
		id := fmt.Sprintf("batch-%02d", i)
		payload, err := proto.Marshal(&controlv1.UsageBatch{BatchId: id, BillingSnapshotId: "snapshot"})
		if err != nil {
			t.Fatal(err)
		}
		digest := sha256.Sum256(payload)
		batches = append(batches, HubUsage{NodeID: "node", BootID: "boot", BatchID: id, Kind: "known", ReportBootID: "report-one", Sequence: uint64(i + 1), Payload: payload, Digest: digest[:]})
	}
	count := func(table string, want int) {
		t.Helper()
		var got int
		if err := s.db.QueryRow("SELECT COUNT(*) FROM " + table).Scan(&got); err != nil || got != want {
			t.Fatalf("%s=%d want=%d err=%v", table, got, want, err)
		}
	}
	if _, err := s.db.Exec(`CREATE TRIGGER fail_group_origin BEFORE INSERT ON usage_origin_envelopes WHEN NEW.batch_id='batch-08' BEGIN SELECT RAISE(ABORT,'disk error'); END`); err != nil {
		t.Fatal(err)
	}
	if got, err := s.PersistKnownUsageGroup(ctx, batches, true); err == nil || got != nil {
		t.Fatal("failed FULL exposed group", got, err)
	}
	for _, table := range []string{"usage_batches", "usage_origin_envelopes", "hub_ledger", "usage_backend_receipts"} {
		count(table, 0)
	}
	if _, err := s.db.Exec(`DROP TRIGGER fail_group_origin`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.PersistKnownUsageGroup(ctx, batches, true); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = Open(path, "hub")
	if err != nil {
		t.Fatal(err)
	}
	for i := range batches {
		batches[i].ReportBootID = "report-two"
		batches[i].Sequence += 100
	}
	replayed, err := s.PersistKnownUsageGroup(ctx, batches, true)
	if err != nil {
		t.Fatal(err)
	}
	for i, b := range replayed {
		if b.ReportBootID != "report-one" || b.Sequence != uint64(i+1) {
			t.Fatal("replay changed provenance", b)
		}
	}
	count("hub_ledger", 16)
	count("usage_backend_receipts", 0)
	if _, err := s.db.Exec(`CREATE TRIGGER fail_group_receipt BEFORE INSERT ON usage_backend_receipts WHEN NEW.batch_id='batch-08' BEGIN SELECT RAISE(ABORT,'disk error'); END`); err != nil {
		t.Fatal(err)
	}
	if err := s.ConfirmBackendUsageGroup(ctx, replayed); err == nil {
		t.Fatal("failed receipt FULL acknowledged")
	}
	count("usage_backend_receipts", 0)
	if _, err := s.db.Exec(`DROP TRIGGER fail_group_receipt`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec(`CREATE TRIGGER ignore_group_receipt BEFORE INSERT ON usage_backend_receipts WHEN NEW.batch_id='batch-08' BEGIN SELECT RAISE(IGNORE); END`); err != nil {
		t.Fatal(err)
	}
	if err := s.ConfirmBackendUsageGroup(ctx, replayed); err == nil {
		t.Fatal("silently ignored receipt acknowledged")
	}
	count("usage_backend_receipts", 0)
	if _, err := s.db.Exec(`DROP TRIGGER ignore_group_receipt`); err != nil {
		t.Fatal(err)
	}
	if err := s.ConfirmBackendUsageGroup(ctx, replayed); err != nil {
		t.Fatal(err)
	}
	if err := s.ConfirmBackendUsageGroup(ctx, replayed); err != nil {
		t.Fatal("exact receipt replay", err)
	}
	count("usage_backend_receipts", 16)
	count("hub_ledger", 16)
	count("unknown_usage_batches", 0)
}
