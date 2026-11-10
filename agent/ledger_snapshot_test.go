package agent

import (
	"compress/gzip"
	"context"
	"database/sql"
	"io"
	"os"
	"path/filepath"
	"testing"
	"vortex/agent/store"
)

func TestLedgerEvidenceIncludesCommittedWALWithReaderOpen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "source.sqlite")
	s, err := store.Open(path, "agent")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	reader, err := sql.Open("sqlite", "file:"+path+"?mode=ro")
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	tx, err := reader.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	var n int
	if err = tx.QueryRow("SELECT COUNT(*) FROM durable_events").Scan(&n); err != nil {
		t.Fatal(err)
	}
	if err = s.AppendEvent(context.Background(), "wal-event", "proof", []byte("committed")); err != nil {
		t.Fatal(err)
	}
	out := t.TempDir()
	t.Setenv("PHASE5_AUTHORIZATION_EVIDENCE_DIR", out)
	saveLedgerFiles(t, "retained-reader", map[string]string{"agent": path})
	f, err := os.Open(filepath.Join(out, "retained-reader-agent.sqlite.gz"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	z, err := gzip.NewReader(f)
	if err != nil {
		t.Fatal(err)
	}
	defer z.Close()
	raw, err := io.ReadAll(z)
	if err != nil {
		t.Fatal(err)
	}
	snapshot := filepath.Join(t.TempDir(), "evidence.sqlite")
	if err = os.WriteFile(snapshot, raw, 0600); err != nil {
		t.Fatal(err)
	}
	format, err := sql.Open("sqlite", "file:"+snapshot+"?mode=ro")
	if err != nil {
		t.Fatal(err)
	}
	var journal string
	err = format.QueryRow("PRAGMA journal_mode").Scan(&journal)
	closeErr := format.Close()
	if err != nil || journal != "wal" || closeErr != nil {
		t.Fatal("snapshot WAL format", journal, err, closeErr)
	}
	db, err := sql.Open("sqlite", "file:"+snapshot+"?mode=ro&immutable=1")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var integrity string
	if err = db.QueryRow("PRAGMA integrity_check").Scan(&integrity); err != nil || integrity != "ok" {
		t.Fatal(integrity, err)
	}
	if err = db.QueryRow("SELECT COUNT(*) FROM durable_events WHERE event_id='wal-event'").Scan(&n); err != nil || n != 1 {
		t.Fatal("committed WAL event missing", n, err)
	}
}
