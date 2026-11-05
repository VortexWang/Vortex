package store

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	_ "modernc.org/sqlite"
)

// Store is the durable local authority for an Agent or Hub. Each role opens
// its own file; WAL files are never shared between processes.
type Store struct {
	db    *sql.DB
	owner string
}

type Info struct {
	Owner         string
	SQLiteVersion string
	JournalMode   string
	Synchronous   string
}

func Open(path, owner string) (*Store, error) {
	if path == "" || owner == "" {
		return nil, fmt.Errorf("store: path and owner are required")
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("store: open: %w", err)
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	db.SetConnMaxLifetime(0)
	s := &Store{db: db, owner: owner}
	if err := s.configure(); err != nil {
		_ = db.Close()
		return nil, err
	}
	return s, nil
}

func (s *Store) configure() error {
	for _, stmt := range []string{
		"PRAGMA journal_mode=WAL",
		"PRAGMA synchronous=FULL",
		"PRAGMA foreign_keys=ON",
		"PRAGMA busy_timeout=5000",
	} {
		if _, err := s.db.Exec(stmt); err != nil {
			return fmt.Errorf("store: %s: %w", stmt, err)
		}
	}
	_, err := s.db.Exec(`
CREATE TABLE IF NOT EXISTS schema_meta (
  key TEXT PRIMARY KEY NOT NULL,
  value TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS durable_events (
  event_id TEXT PRIMARY KEY NOT NULL,
  owner TEXT NOT NULL,
  kind TEXT NOT NULL,
  payload BLOB NOT NULL,
  created_at TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS control_receipts (
  message_id TEXT PRIMARY KEY NOT NULL,
  sequence INTEGER NOT NULL,
  received_at TEXT NOT NULL
);
INSERT INTO schema_meta(key, value) VALUES ('schema_version', '1')
  ON CONFLICT(key) DO NOTHING;
`)
	if err != nil {
		return fmt.Errorf("store: schema: %w", err)
	}
	return nil
}

func (s *Store) AppendEvent(ctx context.Context, eventID, kind string, payload []byte) error {
	if eventID == "" || kind == "" {
		return fmt.Errorf("store: event id and kind are required")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("store: begin: %w", err)
	}
	defer tx.Rollback()
	_, err = tx.ExecContext(ctx, `INSERT INTO durable_events(event_id, owner, kind, payload, created_at) VALUES (?, ?, ?, ?, ?)`, eventID, s.owner, kind, payload, time.Now().UTC().Format(time.RFC3339Nano))
	if err != nil {
		return fmt.Errorf("store: event: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("store: commit: %w", err)
	}
	return nil
}

func (s *Store) RecordReceipt(ctx context.Context, messageID string, sequence uint64) error {
	if messageID == "" {
		return fmt.Errorf("store: message id is required")
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO control_receipts(message_id, sequence, received_at) VALUES (?, ?, ?) ON CONFLICT(message_id) DO NOTHING`, messageID, sequence, time.Now().UTC().Format(time.RFC3339Nano))
	return err
}

func (s *Store) EventCount(ctx context.Context) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT count(*) FROM durable_events WHERE owner = ?`, s.owner).Scan(&n)
	return n, err
}

func (s *Store) ReceiptCount(ctx context.Context) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT count(*) FROM control_receipts`).Scan(&n)
	return n, err
}

func (s *Store) Info(ctx context.Context) (Info, error) {
	var i Info
	i.Owner = s.owner
	if err := s.db.QueryRowContext(ctx, `SELECT sqlite_version()`).Scan(&i.SQLiteVersion); err != nil {
		return i, err
	}
	if err := s.db.QueryRowContext(ctx, `PRAGMA journal_mode`).Scan(&i.JournalMode); err != nil {
		return i, err
	}
	if err := s.db.QueryRowContext(ctx, `PRAGMA synchronous`).Scan(&i.Synchronous); err != nil {
		return i, err
	}
	return i, nil
}

func (s *Store) Close() error { return s.db.Close() }
