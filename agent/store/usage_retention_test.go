package store

import (
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func retentionStore(t *testing.T) (*Store, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "hub.db")
	s, err := Open(path, "hub")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s, path
}
func retentionBatch(t *testing.T, s *Store, id string, backend bool) (UsageBatch, HubUsage, LedgerRecord) {
	t.Helper()
	ctx := context.Background()
	b := UsageBatch{NodeID: "node", BootID: "boot", BatchID: id, SnapshotID: "snapshot", Payload: []byte("known-" + id)}
	if err := s.PersistUsageLedger(ctx, b); err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(b.Payload)
	u := HubUsage{NodeID: b.NodeID, BootID: b.BootID, BatchID: b.BatchID, Kind: "known", Payload: b.Payload, Digest: digest[:]}
	if backend {
		if err := s.ConfirmBackendUsage(ctx, u, u.Digest); err != nil {
			t.Fatal(err)
		}
	}
	records, err := s.PendingLedger(ctx, 1000)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range records {
		if r.ID == b.NodeID+"/"+b.BatchID {
			return b, u, r
		}
	}
	t.Fatal("journal missing")
	return b, u, LedgerRecord{}
}
func retentionTime() IssuerTime {
	now := time.Date(2026, 11, 6, 12, 0, 0, 0, time.UTC)
	return IssuerTime{Earliest: now, Latest: now.Add(time.Second), Trusted: true}
}
func shiftRetention(n IssuerTime, d time.Duration) IssuerTime {
	n.Earliest = n.Earliest.Add(d)
	n.Latest = n.Latest.Add(d)
	return n
}
func mustPrune(t *testing.T, s *Store, now IssuerTime, want int) {
	t.Helper()
	n, err := s.PruneSettledUsage(context.Background(), now, 1000)
	if err != nil || n != want {
		t.Fatalf("prune=%d err=%v want=%d", n, err, want)
	}
}
func retentionCount(t *testing.T, s *Store, table string, want int) {
	t.Helper()
	var n int
	if err := s.db.QueryRow("SELECT COUNT(*) FROM " + table).Scan(&n); err != nil || n != want {
		t.Fatalf("%s count=%d want=%d err=%v", table, n, want, err)
	}
}

func TestSettledUsageRetentionConservativeAgeAndRestartReplay(t *testing.T) {
	s, path := retentionStore(t)
	ctx := context.Background()
	b, u, r := retentionBatch(t, s, "one", true)
	if _, err := s.ConfirmLedger(ctx, r, r.Digest); err != nil {
		t.Fatal(err)
	}
	now := retentionTime()
	mustPrune(t, s, now, 0)
	// Old wall-clock creation/receipt timestamps cannot shorten the trusted interval.
	if _, err := s.db.Exec(`UPDATE usage_batches SET created_at='2000-01-01T00:00:00Z'; UPDATE usage_backend_receipts SET confirmed_at='2000-01-01T00:00:00Z'`); err != nil {
		t.Fatal(err)
	}
	mustPrune(t, s, shiftRetention(now, SettledUsageRetention), 0) // earliest is still 1s short
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	var err error
	s, err = Open(path, "hub")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	final := shiftRetention(now, SettledUsageRetention+time.Second)
	bad := final
	bad.Trusted = false
	if _, err = s.PruneSettledUsage(ctx, bad, 1000); !errors.Is(err, ErrAuthorityTimeUncertain) {
		t.Fatal(err)
	}
	mustPrune(t, s, final, 1)
	mustPrune(t, s, final, 0)
	retentionCount(t, s, "usage_batches", 0)
	retentionCount(t, s, "usage_backend_receipts", 0)
	retentionCount(t, s, "settled_usage_tombstones", 1)
	retentionCount(t, s, "hub_ledger", 1)
	// Lost receipts may replay after restart without recreating a journal/debit.
	b.BootID = "new-boot"
	if err = s.PersistUsageLedger(ctx, b); err != nil {
		t.Fatal(err)
	}
	if yes, err := s.BackendUsageConfirmed(ctx, u); err != nil || !yes {
		t.Fatalf("confirmed=%v err=%v", yes, err)
	}
	if err = s.ConfirmBackendUsage(ctx, u, u.Digest); err != nil {
		t.Fatal(err)
	}
	if checkpoint, err := s.ConfirmLedger(ctx, r, r.Digest); err != nil || checkpoint != r.Sequence {
		t.Fatalf("checkpoint=%d err=%v", checkpoint, err)
	}
	retentionCount(t, s, "usage_batches", 0)
	retentionCount(t, s, "hub_ledger", 1)
	b.Payload = []byte("changed")
	if err = s.PersistUsageLedger(ctx, b); !errors.Is(err, ErrUsageBatchConflict) {
		t.Fatal(err)
	}
	retentionCount(t, s, "usage_disputes", 1)
	if err = s.PersistUnknownUsage(ctx, "node", unknownPayload(t, "one")); !errors.Is(err, ErrUsageBatchConflict) {
		t.Fatal(err)
	}
	retentionCount(t, s, "unknown_usage_batches", 0)
	retentionCount(t, s, "unknown_usage_conflicts", 1)
}

func TestSettledUsageRetentionHolesDisputesAndUnknownDebt(t *testing.T) {
	s, path := retentionStore(t)
	ctx := context.Background()
	_, _, hole := retentionBatch(t, s, "hole", true)
	_, _, tail := retentionBatch(t, s, "tail", true)
	if _, err := s.ConfirmLedger(ctx, tail, tail.Digest); err != nil {
		t.Fatal(err)
	}
	now := retentionTime()
	mustPrune(t, s, now, 0)
	retentionCount(t, s, "usage_retention_age", 0)
	if _, err := s.ConfirmLedger(ctx, hole, hole.Digest); err != nil {
		t.Fatal(err)
	}
	_, _, unconfirmed := retentionBatch(t, s, "no-backend", false)
	if _, err := s.ConfirmLedger(ctx, unconfirmed, unconfirmed.Digest); err != nil {
		t.Fatal(err)
	}
	unknown := unknownPayload(t, "unknown")
	if err := s.PersistUnknownUsage(ctx, "node", unknown); err != nil {
		t.Fatal(err)
	}
	records, err := s.PendingLedger(ctx, 1000)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range records {
		if _, err = s.ConfirmLedger(ctx, r, r.Digest); err != nil {
			t.Fatal(err)
		}
	}
	mustPrune(t, s, now, 0)
	if err := s.RecordUsageDispute(ctx, "node", "tail", []byte("dispute evidence")); err != nil {
		t.Fatal(err)
	}
	mustPrune(t, s, shiftRetention(now, 30*24*time.Hour), 1)
	retentionCount(t, s, "usage_batches", 2)
	retentionCount(t, s, "unknown_usage_batches", 1)
	retentionCount(t, s, "usage_disputes", 1)
	var liability string
	if err = s.db.QueryRow(`SELECT liability_state FROM unknown_usage_batches`).Scan(&liability); err != nil || liability != "unresolved" {
		t.Fatalf("liability=%s err=%v", liability, err)
	}
	saveRetentionLedger(t, s, path)
}

func TestSettledUsageRetentionAtomicFailureAndMigration(t *testing.T) {
	s, path := retentionStore(t)
	ctx := context.Background()
	b, _, r := retentionBatch(t, s, "one", true)
	if _, err := s.ConfirmLedger(ctx, r, r.Digest); err != nil {
		t.Fatal(err)
	}
	// Simulate pre-retention schema without deleting any old financial records.
	if _, err := s.db.Exec(`DROP TABLE usage_retention_age;DROP TABLE usage_disputes;DROP TABLE settled_usage_tombstones;DELETE FROM schema_meta WHERE key='usage_retention_version';CREATE TRIGGER fail_retention BEFORE INSERT ON schema_meta WHEN NEW.key='usage_retention_version' BEGIN SELECT RAISE(ABORT,'disk failure'); END`); err != nil {
		t.Fatal(err)
	}
	if err := s.migrateUsageRetention(); err == nil {
		t.Fatal("migration unexpectedly passed")
	}
	var n int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE name IN ('usage_retention_age','usage_disputes','settled_usage_tombstones')`).Scan(&n); err != nil || n != 0 {
		t.Fatalf("partial migration=%d err=%v", n, err)
	}
	if _, err := s.db.Exec(`DROP TRIGGER fail_retention`); err != nil {
		t.Fatal(err)
	}
	s.Close()
	var err error
	s, err = Open(path, "hub")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	now := retentionTime()
	mustPrune(t, s, now, 0)
	if _, err := s.db.Exec(`CREATE TRIGGER fail_prune BEFORE DELETE ON usage_backend_receipts BEGIN SELECT RAISE(ABORT,'disk failure'); END`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.PruneSettledUsage(ctx, shiftRetention(now, 8*24*time.Hour), 1000); err == nil {
		t.Fatal("deletion unexpectedly passed")
	}
	retentionCount(t, s, "usage_batches", 1)
	retentionCount(t, s, "usage_backend_receipts", 1)
	retentionCount(t, s, "settled_usage_tombstones", 0)
	if err = s.PersistUsageLedger(ctx, b); err != nil {
		t.Fatal(err)
	}
	if _, err = s.db.Exec(`DROP TRIGGER fail_prune`); err != nil {
		t.Fatal(err)
	}
	mustPrune(t, s, shiftRetention(now, 8*24*time.Hour), 1)
	agent, err := Open(filepath.Join(t.TempDir(), "agent.db"), "agent")
	if err != nil {
		t.Fatal(err)
	}
	defer agent.Close()
	if _, err = agent.PruneSettledUsage(ctx, now, 1); !errors.Is(err, ErrAuthorizationDenied) {
		t.Fatal(err)
	}
}

func TestSettledUsageRetentionBoundedScanDoesNotStarveUnaged(t *testing.T) {
	s, _ := retentionStore(t)
	ctx := context.Background()
	now := retentionTime()
	for _, id := range []string{"a", "b", "c"} {
		_, _, r := retentionBatch(t, s, id, true)
		if _, err := s.ConfirmLedger(ctx, r, r.Digest); err != nil {
			t.Fatal(err)
		}
	}
	for i := 0; i < 3; i++ {
		if n, err := s.PruneSettledUsage(ctx, now, 1); err != nil || n != 0 {
			t.Fatalf("n=%d err=%v", n, err)
		}
	}
	retentionCount(t, s, "usage_retention_age", 3)
}

func TestSettledUsageRetentionConcurrentReplayAndPrune(t *testing.T) {
	s, _ := retentionStore(t)
	ctx := context.Background()
	b, u, r := retentionBatch(t, s, "one", true)
	if _, err := s.ConfirmLedger(ctx, r, r.Digest); err != nil {
		t.Fatal(err)
	}
	now := retentionTime()
	mustPrune(t, s, now, 0)
	later := shiftRetention(now, 8*24*time.Hour)
	results := make(chan error, 12)
	for i := 0; i < 4; i++ {
		go func() { _, err := s.PruneSettledUsage(ctx, later, 1); results <- err }()
		go func() { results <- s.PersistUsageLedger(ctx, b) }()
		go func() { results <- s.ConfirmBackendUsage(ctx, u, u.Digest) }()
	}
	for i := 0; i < 12; i++ {
		if err := <-results; err != nil {
			t.Fatal(err)
		}
	}
	retentionCount(t, s, "usage_batches", 0)
	retentionCount(t, s, "usage_backend_receipts", 0)
	retentionCount(t, s, "settled_usage_tombstones", 1)
	retentionCount(t, s, "hub_ledger", 1)
	records, err := s.PendingLedger(ctx, 1000)
	if err != nil || len(records) != 0 {
		t.Fatalf("pending=%v err=%v", records, err)
	}
	batches, err := s.PendingBackendUsage(ctx)
	if err != nil || len(batches) != 0 {
		t.Fatalf("pending=%v err=%v", batches, err)
	}
	// A new tail starts after the retained high-water sequence, even after reopen.
	_, _, tail := retentionBatch(t, s, "tail", true)
	if tail.Sequence <= r.Sequence {
		t.Fatal("sequence reused")
	}
}

// Test-clock evidence proves conservative boundary logic; it is not a claim of
// seven days of wall-clock operation or production backend confirmation.
func saveRetentionLedger(t *testing.T, s *Store, path string) {
	t.Helper()
	out := os.Getenv("PHASE5_RETENTION_EVIDENCE_DIR")
	if out == "" {
		return
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(raw)
	if err = os.MkdirAll(out, 0755); err != nil {
		t.Fatal(err)
	}
	f, err := os.Create(filepath.Join(out, "settled-hub.sqlite.gz"))
	if err != nil {
		t.Fatal(err)
	}
	z := gzip.NewWriter(f)
	if _, err = z.Write(raw); err != nil {
		t.Fatal(err)
	}
	if err = z.Close(); err != nil {
		t.Fatal(err)
	}
	if err = f.Close(); err != nil {
		t.Fatal(err)
	}
	meta, err := json.Marshal(map[string]any{"schema": 1, "sha256": hex.EncodeToString(sum[:]), "scope": "protocol-test-only", "clock": "deterministic-conservative-bounds", "production_backend_authority": false})
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(out, "settled-hub.json"), meta, 0644); err != nil {
		t.Fatal(err)
	}
}
