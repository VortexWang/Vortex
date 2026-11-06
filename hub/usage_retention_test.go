package hub

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"net/url"
	"path/filepath"
	"testing"
	"time"

	"vortex/agent/store"
)

// This exercises the real replay/cleanup adapter and two durable databases, not
// a production backend transport (which remains Phase 6 work).
func TestRecoveryRetainsReceiptIdentityAfterUsageCompaction(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	path := filepath.Join(dir, "hub.db")
	s, err := store.Open(path, "hub")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	b := store.UsageBatch{NodeID: "node", BootID: "boot", BatchID: "batch", SnapshotID: "snapshot", Payload: []byte("known")}
	if err = s.PersistUsageLedger(ctx, b); err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(b.Payload)
	u := store.HubUsage{NodeID: b.NodeID, BootID: b.BootID, BatchID: b.BatchID, Kind: "known", Payload: b.Payload, Digest: digest[:]}
	if err = s.ConfirmBackendUsage(ctx, u, u.Digest); err != nil {
		t.Fatal(err)
	}
	backendDB, err := sql.Open("sqlite", filepath.Join(dir, "backend.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer backendDB.Close()
	backendDB.SetMaxOpenConns(1)
	for _, q := range []string{`PRAGMA journal_mode=WAL`, `PRAGMA synchronous=FULL`, `CREATE TABLE records(sequence INTEGER PRIMARY KEY,kind TEXT,id TEXT,payload BLOB,digest BLOB)`} {
		if _, err = backendDB.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	backend := &durableLedgerBackend{db: backendDB}
	now := time.Date(2026, 11, 6, 12, 0, 0, 0, time.UTC)
	clock := store.IssuerTime{Earliest: now, Latest: now.Add(time.Second), Trusted: true}
	issuer := &Issuer{Store: s, Clock: func() store.IssuerTime { return clock }}
	if err = issuer.ReplayLedger(ctx, backend); err != nil {
		t.Fatal(err)
	}
	observer, err := sql.Open("sqlite", (&url.URL{Scheme: "file", Path: path, RawQuery: "mode=ro&_pragma=busy_timeout(5000)"}).String())
	if err != nil {
		t.Fatal(err)
	}
	defer observer.Close()
	observer.SetMaxOpenConns(1)
	count := func(table string, want int) {
		t.Helper()
		var n int
		if err := observer.QueryRow("SELECT COUNT(*) FROM " + table).Scan(&n); err != nil || n != want {
			t.Fatalf("%s=%d want=%d err=%v", table, n, want, err)
		}
	}
	count("usage_retention_age", 1)
	count("usage_batches", 1)
	clock.Earliest = now.Add(30 * 24 * time.Hour)
	clock.Latest = clock.Earliest.Add(time.Second)
	clock.Trusted = false
	if err = issuer.ReplayLedger(ctx, backend); err != nil {
		t.Fatal(err)
	}
	count("usage_batches", 1)
	clock.Trusted = true
	if err = issuer.ReplayLedger(ctx, backend); err != nil {
		t.Fatal(err)
	}
	count("usage_batches", 0)
	count("usage_backend_receipts", 0)
	count("settled_usage_tombstones", 1)
	count("hub_ledger", 1)
	if err = s.PersistUsageLedger(ctx, b); err != nil {
		t.Fatal(err)
	}
	if yes, err := s.BackendUsageConfirmed(ctx, u); err != nil || !yes {
		t.Fatalf("confirmed=%v err=%v", yes, err)
	}
	if err = issuer.ReplayLedger(ctx, backend); err != nil {
		t.Fatal(err)
	}
	var n int
	if err = backendDB.QueryRow(`SELECT COUNT(*) FROM records`).Scan(&n); err != nil || n != 1 {
		t.Fatalf("duplicate backend record %d %v", n, err)
	}
}
