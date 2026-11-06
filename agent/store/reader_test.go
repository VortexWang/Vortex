package store

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLedgerReaderCannotCreateMigrateOrMutate(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "ledger ? #.db")
	if r, err := OpenReader(path, "agent"); err == nil {
		r.Close()
		t.Fatal("reader created missing ledger")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("missing reader changed disk: %v", err)
	}
	s, err := Open(path, "agent")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := s.AppendEvent(ctx, "event", "kind", []byte("payload")); err != nil {
		t.Fatal(err)
	}
	r, err := OpenReader(path, "agent")
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	if n, err := r.EventCount(ctx); err != nil || n != 1 {
		t.Fatalf("reader %d %v", n, err)
	}
	if err := r.AppendEvent(ctx, "other", "kind", []byte("payload")); err == nil {
		t.Fatal("read-only observer wrote authority")
	}
	if _, err := r.db.Exec(`CREATE TABLE illicit(value TEXT)`); err == nil {
		t.Fatal("reader changed schema")
	}
	var mode string
	if err := r.db.QueryRow(`PRAGMA query_only`).Scan(&mode); err != nil || mode != "1" {
		t.Fatalf("query_only=%s %v", mode, err)
	}
	// A diagnostic snapshot must not acquire the writer reservation.
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if err := tx.QueryRow(`SELECT COUNT(*) FROM durable_events`).Scan(new(int)); err != nil {
		t.Fatal(err)
	}
	if err := s.AppendEvent(ctx, "concurrent", "kind", []byte("new")); err != nil {
		t.Fatalf("reader blocked writer: %v", err)
	}
}

func TestLedgerWriterLocksBeforeAuthoritySnapshot(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "hub.db")
	a, err := Open(path, "hub")
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	b, err := Open(path, "hub")
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	tx, err := a.db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if err := tx.QueryRow(`SELECT COUNT(*) FROM durable_events`).Scan(new(int)); err != nil {
		t.Fatal(err)
	}
	started := make(chan struct{})
	done := make(chan error, 1)
	go func() { close(started); done <- b.AppendEvent(ctx, "other-writer", "kind", []byte("payload")) }()
	<-started
	select {
	case err := <-done:
		t.Fatalf("writer overtook authority snapshot: %v", err)
	case <-time.After(30 * time.Millisecond):
	}
	if _, err := tx.Exec(`INSERT INTO durable_events VALUES('first','hub','kind',X'01','now')`); err != nil {
		t.Fatalf("authority snapshot could not promote to write: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(6 * time.Second):
		t.Fatal("bounded writer wait did not finish")
	}
	if n, err := a.EventCount(ctx); err != nil || n != 2 {
		t.Fatalf("lost event: %d %v", n, err)
	}
}
