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

type ConfigResult struct {
	TransactionID string
	Version       uint64
	State         string
	Reason        string
}

type ObjectResult struct{ ObjectID, State, Reason string }

var ErrCredentialRevoked = fmt.Errorf("store: credential revoked")

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
CREATE TABLE IF NOT EXISTS credentials (
  node_id TEXT PRIMARY KEY NOT NULL,
  certificate BLOB NOT NULL,
  valid_until TEXT NOT NULL,
  revoked INTEGER NOT NULL DEFAULT 0,
  updated_at TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS config_results (
  transaction_id TEXT PRIMARY KEY NOT NULL,
  version INTEGER NOT NULL,
  state TEXT NOT NULL,
  reason TEXT NOT NULL DEFAULT '',
  updated_at TEXT NOT NULL
);
		CREATE TABLE IF NOT EXISTS agent_state (
		  key TEXT PRIMARY KEY NOT NULL,
		  value TEXT NOT NULL,
		  updated_at TEXT NOT NULL
		);
CREATE TABLE IF NOT EXISTS config_object_results (
  transaction_id TEXT NOT NULL,
  object_id TEXT NOT NULL,
  state TEXT NOT NULL,
  reason TEXT NOT NULL DEFAULT '',
  updated_at TEXT NOT NULL,
  PRIMARY KEY(transaction_id, object_id)
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

func (s *Store) NextControlSequence(ctx context.Context) (uint64, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	var current uint64
	err = tx.QueryRowContext(ctx, `SELECT value FROM agent_state WHERE key = 'control_sequence'`).Scan(&current)
	if err != nil && err != sql.ErrNoRows {
		return 0, err
	}
	current++
	_, err = tx.ExecContext(ctx, `INSERT INTO agent_state(key, value, updated_at) VALUES ('control_sequence', ?, ?) ON CONFLICT(key) DO UPDATE SET value=excluded.value, updated_at=excluded.updated_at`, fmt.Sprint(current), time.Now().UTC().Format(time.RFC3339Nano))
	if err != nil {
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return current, nil
}

func (s *Store) PersistControlAck(ctx context.Context, messageID string, sequence, appliedVersion uint64, heartbeat time.Time) error {
	if messageID == "" || sequence == 0 {
		return fmt.Errorf("store: control acknowledgement identity is required")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	_, err = tx.ExecContext(ctx, `INSERT INTO control_receipts(message_id, sequence, received_at) VALUES (?, ?, ?) ON CONFLICT(message_id) DO NOTHING`, messageID, sequence, time.Now().UTC().Format(time.RFC3339Nano))
	if err != nil {
		return err
	}
	for key, value := range map[string]string{
		"control_sequence":  fmt.Sprint(sequence),
		"applied_version":   fmt.Sprint(appliedVersion),
		"backend_heartbeat": heartbeat.UTC().Format(time.RFC3339Nano),
	} {
		_, err = tx.ExecContext(ctx, `INSERT INTO agent_state(key, value, updated_at) VALUES (?, ?, ?) ON CONFLICT(key) DO UPDATE SET value=excluded.value, updated_at=excluded.updated_at`, key, value, time.Now().UTC().Format(time.RFC3339Nano))
		if err != nil {
			return err
		}
	}
	return tx.Commit()
}

// PersistControlReceipt records a Hub contact, which is not evidence of a
// successful backend heartbeat and cannot extend an autonomy deadline.
func (s *Store) PersistControlReceipt(ctx context.Context, messageID string, sequence uint64, contact time.Time) error {
	if messageID == "" || sequence == 0 || contact.IsZero() {
		return fmt.Errorf("store: control receipt identity and contact time are required")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var recordedSequence uint64
	lookupErr := tx.QueryRowContext(ctx, `SELECT sequence FROM control_receipts WHERE message_id = ?`, messageID).Scan(&recordedSequence)
	if lookupErr != nil && lookupErr != sql.ErrNoRows {
		return lookupErr
	}
	if lookupErr == nil && recordedSequence != sequence {
		return fmt.Errorf("store: control receipt sequence conflicts with durable receipt")
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO control_receipts(message_id, sequence, received_at) VALUES (?, ?, ?) ON CONFLICT(message_id) DO NOTHING`, messageID, sequence, time.Now().UTC().Format(time.RFC3339Nano))
	if err != nil {
		return err
	}
	for key, value := range map[string]string{
		"control_sequence": fmt.Sprint(sequence),
		"hub_contact":      contact.UTC().Format(time.RFC3339Nano),
	} {
		_, err = tx.ExecContext(ctx, `INSERT INTO agent_state(key, value, updated_at) VALUES (?, ?, ?) ON CONFLICT(key) DO UPDATE SET value=excluded.value, updated_at=excluded.updated_at`, key, value, time.Now().UTC().Format(time.RFC3339Nano))
		if err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Store) AgentState(ctx context.Context, key string) (string, error) {
	var value string
	err := s.db.QueryRowContext(ctx, `SELECT value FROM agent_state WHERE key = ?`, key).Scan(&value)
	return value, err
}

func (s *Store) Credential(ctx context.Context, nodeID string) ([]byte, time.Time, error) {
	var cert []byte
	var validUntil string
	var revoked int
	err := s.db.QueryRowContext(ctx, `SELECT certificate, valid_until, revoked FROM credentials WHERE node_id = ?`, nodeID).Scan(&cert, &validUntil, &revoked)
	if err != nil {
		return nil, time.Time{}, err
	}
	if revoked != 0 {
		return nil, time.Time{}, ErrCredentialRevoked
	}
	valid, err := time.Parse(time.RFC3339Nano, validUntil)
	if err == nil && !time.Now().Before(valid) {
		return nil, time.Time{}, fmt.Errorf("store: credential expired")
	}
	return cert, valid, err
}

func (s *Store) SaveCredential(ctx context.Context, nodeID string, certificate []byte, validUntil time.Time) error {
	if nodeID == "" || len(certificate) == 0 {
		return fmt.Errorf("store: credential identity and certificate are required")
	}
	result, err := s.db.ExecContext(ctx, `INSERT INTO credentials(node_id, certificate, valid_until, revoked, updated_at) VALUES (?, ?, ?, 0, ?) ON CONFLICT(node_id) DO UPDATE SET certificate=excluded.certificate, valid_until=excluded.valid_until, updated_at=excluded.updated_at WHERE credentials.revoked=0`, nodeID, certificate, validUntil.UTC().Format(time.RFC3339Nano), time.Now().UTC().Format(time.RFC3339Nano))
	if err != nil {
		return err
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if changed == 0 {
		return ErrCredentialRevoked
	}
	return nil
}

func (s *Store) RevokeCredential(ctx context.Context, nodeID string) error {
	if nodeID == "" {
		return fmt.Errorf("store: node identity required for revocation")
	}
	result, err := s.db.ExecContext(ctx, `UPDATE credentials SET revoked=1, updated_at=? WHERE node_id=?`, time.Now().UTC().Format(time.RFC3339Nano), nodeID)
	if err != nil {
		return err
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if changed == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func (s *Store) SaveConfigResult(ctx context.Context, transactionID string, version uint64, state, reason string) error {
	if transactionID == "" || state == "" {
		return fmt.Errorf("store: config transaction and state are required")
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO config_results(transaction_id, version, state, reason, updated_at) VALUES (?, ?, ?, ?, ?) ON CONFLICT(transaction_id) DO UPDATE SET version=excluded.version, state=excluded.state, reason=excluded.reason, updated_at=excluded.updated_at`, transactionID, version, state, reason, time.Now().UTC().Format(time.RFC3339Nano))
	return err
}

func (s *Store) GetConfigResult(ctx context.Context, transactionID string) (ConfigResult, error) {
	var result ConfigResult
	result.TransactionID = transactionID
	err := s.db.QueryRowContext(ctx, `SELECT version, state, reason FROM config_results WHERE transaction_id = ?`, transactionID).
		Scan(&result.Version, &result.State, &result.Reason)
	return result, err
}

func (s *Store) SaveObjectResults(ctx context.Context, transactionID string, results []ObjectResult) error {
	if transactionID == "" {
		return fmt.Errorf("store: transaction id required")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, result := range results {
		if result.ObjectID == "" || result.State == "" {
			return fmt.Errorf("store: object result identity and state required")
		}
		_, err := tx.ExecContext(ctx, `INSERT INTO config_object_results(transaction_id, object_id, state, reason, updated_at) VALUES (?, ?, ?, ?, ?) ON CONFLICT(transaction_id, object_id) DO UPDATE SET state=excluded.state, reason=excluded.reason, updated_at=excluded.updated_at`, transactionID, result.ObjectID, result.State, result.Reason, time.Now().UTC().Format(time.RFC3339Nano))
		if err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Store) GetObjectResults(ctx context.Context, transactionID string) ([]ObjectResult, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT object_id, state, reason FROM config_object_results WHERE transaction_id = ? ORDER BY object_id`, transactionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var results []ObjectResult
	for rows.Next() {
		var result ObjectResult
		if err := rows.Scan(&result.ObjectID, &result.State, &result.Reason); err != nil {
			return nil, err
		}
		results = append(results, result)
	}
	return results, rows.Err()
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
