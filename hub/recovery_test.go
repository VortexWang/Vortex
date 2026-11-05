package hub

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"vortex/agent/store"
)

type durableLedgerBackend struct {
	db      *sql.DB
	loseACK bool
}

func (b *durableLedgerBackend) AcceptLedger(ctx context.Context, r store.LedgerRecord) ([]byte, error) {
	tx, err := b.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `INSERT INTO records(sequence,kind,id,payload,digest) VALUES(?,?,?,?,?) ON CONFLICT DO NOTHING`, r.Sequence, r.Kind, r.ID, r.Payload, r.Digest); err != nil {
		return nil, err
	}
	var kind, id string
	var payload, digest []byte
	if err = tx.QueryRowContext(ctx, `SELECT kind,id,payload,digest FROM records WHERE sequence=?`, r.Sequence).Scan(&kind, &id, &payload, &digest); err != nil {
		return nil, err
	}
	if kind != r.Kind || id != r.ID || !bytes.Equal(payload, r.Payload) || !bytes.Equal(digest, r.Digest) {
		return nil, errors.New("conflicting ledger replay")
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	if b.loseACK {
		b.loseACK = false
		return nil, errors.New("lost durable ACK")
	}
	return digest, nil
}

func TestRecoveryReplaysDurablePrefixAndTailAfterLostACK(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	s, err := store.Open(filepath.Join(dir, "hub.db"), "hub")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	now := time.Date(2026, 11, 5, 12, 0, 0, 0, time.UTC)
	d := store.HubDelegation{ID: "550e8400-e29b-41d4-a716-446655440100", HubID: "550e8400-e29b-41d4-a716-446655440101", SourceID: "550e8400-e29b-41d4-a716-446655440102", RuntimeSourceID: "550e8400-e29b-41d4-a716-446655440103", ShareSourceID: "550e8400-e29b-41d4-a716-446655440104", NodeID: "550e8400-e29b-41d4-a716-446655440105", BootID: "550e8400-e29b-41d4-a716-446655440106", BillingSnapshotID: "550e8400-e29b-41d4-a716-446655440107", RuleID: "rule", RuleRevision: 1, PathGeneration: 3, FencingFloor: 3, SignerEpoch: 1, RawBytes: 1024, RateBPS: 100000, Connections: 1, ValidFrom: now, ExpiresAt: now.Add(time.Hour), HardDeadline: now.Add(time.Hour), BackendHeartbeat: now, AutonomyDeadline: now.Add(2 * time.Hour)}
	clock := store.IssuerTime{Earliest: now.Add(time.Second), Latest: now.Add(2 * time.Second), Trusted: true}
	if err = s.InstallHubDelegation(ctx, d, clock); err != nil {
		t.Fatal(err)
	}
	if err = s.PersistUsageLedger(ctx, store.UsageBatch{NodeID: d.NodeID, BootID: d.BootID, BatchID: "prefix", SnapshotID: d.BillingSnapshotID, Payload: []byte("prefix-usage")}); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", filepath.Join(dir, "backend.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	for _, q := range []string{`PRAGMA journal_mode=WAL`, `PRAGMA synchronous=FULL`, `CREATE TABLE records(sequence INTEGER PRIMARY KEY,kind TEXT,id TEXT,payload BLOB,digest BLOB)`} {
		if _, err = db.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	backend := &durableLedgerBackend{db: db, loseACK: true}
	h := store.HandoffRequest{ID: "550e8400-e29b-41d4-a716-446655440800", HubID: d.HubID, FromEpoch: 1, ToEpoch: 2, Heartbeat: now.Add(time.Minute)}
	clock = store.IssuerTime{Earliest: h.Heartbeat, Latest: h.Heartbeat.Add(time.Second), Trusted: true}
	issuer := &Issuer{Store: s, HubID: d.HubID, Clock: func() store.IssuerTime { return clock }}
	if _, err = issuer.Recover(ctx, h, backend); err == nil {
		t.Fatal("lost ACK accepted")
	}
	// Tail arrives after S was sealed and after backend committed prefix.
	if err = s.PersistUsageLedger(ctx, store.UsageBatch{NodeID: d.NodeID, BootID: d.BootID, BatchID: "tail", SnapshotID: d.BillingSnapshotID, Payload: []byte("tail-usage")}); err != nil {
		t.Fatal(err)
	}
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = store.Open(filepath.Join(dir, "hub.db"), "hub")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	issuer.Store = s
	result, err := issuer.Recover(ctx, h, backend)
	if err != nil || result.SealedSequence != 1 {
		t.Fatalf("recover %+v %v", result, err)
	}
	if pending, err := s.PendingLedger(ctx, 100); err != nil || len(pending) != 0 {
		t.Fatalf("pending %+v %v", pending, err)
	}
	var count int
	if err = db.QueryRow(`SELECT COUNT(*) FROM records`).Scan(&count); err != nil || count != 3 {
		t.Fatalf("duplicate/lost prefix %d %v", count, err)
	}
	if _, err = issuer.Recover(ctx, h, backend); err != nil {
		t.Fatal(err)
	}
}
