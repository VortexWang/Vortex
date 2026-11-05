package store

import (
	"bytes"
	"context"
	"crypto/x509"
	"database/sql"
	"errors"
	"fmt"
	"math"
	"strconv"
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
	TransactionID     string
	Version           uint64
	FencingGeneration uint64
	ExpectedVersion   uint64
	Payload           []byte
	PayloadSHA256     []byte
	State             string
	Reason            string
}

type ObjectResult struct{ ObjectID, State, Reason string }

type CredentialVersion struct {
	Certificate  []byte
	ValidUntil   time.Time
	OverlapUntil time.Time
}

var ErrCredentialRevoked = fmt.Errorf("store: credential revoked")

var (
	ErrConfigRequestConflict = errors.New("store: config transaction request conflicts with durable request")
	ErrConfigVersionStale    = errors.New("store: config expected version is stale")
	ErrConfigGenerationStale = errors.New("store: config fencing generation is stale")
	ErrConfigUnresolved      = errors.New("store: an activation outcome is unresolved")
)

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
CREATE TABLE IF NOT EXISTS credential_versions (
  node_id TEXT NOT NULL,
  serial TEXT NOT NULL,
  certificate BLOB NOT NULL,
  valid_until TEXT NOT NULL,
  overlap_until TEXT NOT NULL DEFAULT '',
  revoked INTEGER NOT NULL DEFAULT 0,
  updated_at TEXT NOT NULL,
  PRIMARY KEY(node_id, serial)
);
CREATE TABLE IF NOT EXISTS config_results (
  transaction_id TEXT PRIMARY KEY NOT NULL,
  version INTEGER NOT NULL,
  version_u64 TEXT NOT NULL DEFAULT '0',
  fencing_generation TEXT NOT NULL DEFAULT '0',
  expected_version TEXT NOT NULL DEFAULT '0',
  payload BLOB NOT NULL DEFAULT X'',
  payload_sha256 BLOB NOT NULL DEFAULT X'',
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
	for name, declaration := range map[string]string{
		"version_u64":        "TEXT NOT NULL DEFAULT '0'",
		"fencing_generation": "TEXT NOT NULL DEFAULT '0'",
		"expected_version":   "TEXT NOT NULL DEFAULT '0'",
		"payload":            "BLOB NOT NULL DEFAULT X''",
		"payload_sha256":     "BLOB NOT NULL DEFAULT X''",
	} {
		if err := s.ensureColumn("config_results", name, declaration); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) ensureColumn(table, name, declaration string) error {
	rows, err := s.db.Query("PRAGMA table_info(" + table + ")")
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var cid, notnull, pk int
		var column, kind string
		var defaultValue any
		if err := rows.Scan(&cid, &column, &kind, &notnull, &defaultValue, &pk); err != nil {
			return err
		}
		if column == name {
			return rows.Err()
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if _, err := s.db.Exec("ALTER TABLE " + table + " ADD COLUMN " + name + " " + declaration); err != nil {
		return fmt.Errorf("store: add %s.%s: %w", table, name, err)
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

func (s *Store) SaveCredentialRotation(ctx context.Context, nodeID string, activeDER []byte, activeUntil time.Time, previousDER []byte, previousUntil, overlapUntil time.Time) error {
	if nodeID == "" || len(activeDER) == 0 || activeUntil.IsZero() {
		return fmt.Errorf("store: active credential identity, certificate and expiry are required")
	}
	active, err := x509.ParseCertificate(activeDER)
	if err != nil {
		return fmt.Errorf("store: parse active certificate: %w", err)
	}
	if active.Subject.CommonName != nodeID || len(active.DNSNames) != 1 || active.DNSNames[0] != nodeID {
		return fmt.Errorf("store: active credential identity mismatch")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if len(previousDER) > 0 {
		previous, parseErr := x509.ParseCertificate(previousDER)
		if parseErr != nil {
			return fmt.Errorf("store: parse previous certificate: %w", parseErr)
		}
		if previous.Subject.CommonName != nodeID || len(previous.DNSNames) != 1 || previous.DNSNames[0] != nodeID || previousUntil.IsZero() || overlapUntil.IsZero() || overlapUntil.After(previousUntil) {
			return fmt.Errorf("store: previous credential overlap is invalid")
		}
		if previous.SerialNumber.Cmp(active.SerialNumber) == 0 {
			return fmt.Errorf("store: active and previous credential serials must differ")
		}
		if _, err := tx.ExecContext(ctx, `UPDATE credential_versions SET revoked=1, updated_at=? WHERE node_id=? AND serial<>? AND serial<>?`, time.Now().UTC().Format(time.RFC3339Nano), nodeID, active.SerialNumber.String(), previous.SerialNumber.String()); err != nil {
			return err
		}
		result, execErr := tx.ExecContext(ctx, `INSERT INTO credential_versions(node_id, serial, certificate, valid_until, overlap_until, revoked, updated_at) VALUES (?, ?, ?, ?, ?, 0, ?) ON CONFLICT(node_id, serial) DO UPDATE SET certificate=excluded.certificate, valid_until=excluded.valid_until, overlap_until=excluded.overlap_until, updated_at=excluded.updated_at WHERE credential_versions.revoked=0`, nodeID, previous.SerialNumber.String(), previousDER, previousUntil.UTC().Format(time.RFC3339Nano), overlapUntil.UTC().Format(time.RFC3339Nano), time.Now().UTC().Format(time.RFC3339Nano))
		if execErr != nil {
			return execErr
		}
		changed, execErr := result.RowsAffected()
		if execErr != nil {
			return execErr
		}
		if changed == 0 {
			return ErrCredentialRevoked
		}
	} else {
		if _, err := tx.ExecContext(ctx, `UPDATE credential_versions SET revoked=1, updated_at=? WHERE node_id=? AND serial<>?`, time.Now().UTC().Format(time.RFC3339Nano), nodeID, active.SerialNumber.String()); err != nil {
			return err
		}
	}
	result, err := tx.ExecContext(ctx, `INSERT INTO credential_versions(node_id, serial, certificate, valid_until, overlap_until, revoked, updated_at) VALUES (?, ?, ?, ?, '', 0, ?) ON CONFLICT(node_id, serial) DO UPDATE SET certificate=excluded.certificate, valid_until=excluded.valid_until, overlap_until='', updated_at=excluded.updated_at WHERE credential_versions.revoked=0`, nodeID, active.SerialNumber.String(), activeDER, activeUntil.UTC().Format(time.RFC3339Nano), time.Now().UTC().Format(time.RFC3339Nano))
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
	result, err = tx.ExecContext(ctx, `INSERT INTO credentials(node_id, certificate, valid_until, revoked, updated_at) VALUES (?, ?, ?, 0, ?) ON CONFLICT(node_id) DO UPDATE SET certificate=excluded.certificate, valid_until=excluded.valid_until, updated_at=excluded.updated_at WHERE credentials.revoked=0`, nodeID, activeDER, activeUntil.UTC().Format(time.RFC3339Nano), time.Now().UTC().Format(time.RFC3339Nano))
	if err != nil {
		return err
	}
	changed, err = result.RowsAffected()
	if err != nil {
		return err
	}
	if changed == 0 {
		return ErrCredentialRevoked
	}
	return tx.Commit()
}

func (s *Store) CredentialVersions(ctx context.Context, nodeID string, now time.Time) ([][]byte, error) {
	versions, err := s.CredentialVersionRecords(ctx, nodeID, now)
	if err != nil {
		return nil, err
	}
	result := make([][]byte, 0, len(versions))
	for _, version := range versions {
		result = append(result, version.Certificate)
	}
	return result, nil
}

func (s *Store) CredentialVersionRecords(ctx context.Context, nodeID string, now time.Time) ([]CredentialVersion, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT certificate, valid_until, overlap_until FROM credential_versions WHERE node_id=? AND revoked=0 ORDER BY CASE WHEN overlap_until='' THEN 0 ELSE 1 END, updated_at DESC`, nodeID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var results []CredentialVersion
	for rows.Next() {
		var certificate []byte
		var validUntil, overlapUntil string
		if err := rows.Scan(&certificate, &validUntil, &overlapUntil); err != nil {
			return nil, err
		}
		valid, err := time.Parse(time.RFC3339Nano, validUntil)
		if err != nil {
			return nil, err
		}
		if !now.Before(valid) {
			continue
		}
		if overlapUntil != "" {
			overlap, err := time.Parse(time.RFC3339Nano, overlapUntil)
			if err != nil {
				return nil, err
			}
			if !now.Before(overlap) {
				continue
			}
		}
		version := CredentialVersion{Certificate: certificate, ValidUntil: valid}
		if overlapUntil != "" {
			version.OverlapUntil, err = time.Parse(time.RFC3339Nano, overlapUntil)
			if err != nil {
				return nil, err
			}
		}
		results = append(results, version)
	}
	return results, rows.Err()
}

func (s *Store) RevokeCredential(ctx context.Context, nodeID string) error {
	if nodeID == "" {
		return fmt.Errorf("store: node identity required for revocation")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	updated := time.Now().UTC().Format(time.RFC3339Nano)
	result, err := tx.ExecContext(ctx, `UPDATE credentials SET revoked=1, updated_at=? WHERE node_id=?`, updated, nodeID)
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
	if _, err := tx.ExecContext(ctx, `UPDATE credential_versions SET revoked=1, updated_at=? WHERE node_id=?`, updated, nodeID); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) RevokeCredentialVersion(ctx context.Context, nodeID, serial string) error {
	if nodeID == "" || serial == "" {
		return fmt.Errorf("store: node identity and certificate serial are required for revocation")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	updated := time.Now().UTC().Format(time.RFC3339Nano)
	result, err := tx.ExecContext(ctx, `UPDATE credential_versions SET revoked=1, updated_at=? WHERE node_id=? AND serial=?`, updated, nodeID, serial)
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
	if _, err := tx.ExecContext(ctx, `UPDATE credentials SET revoked=1, updated_at=? WHERE node_id=? AND certificate=(SELECT certificate FROM credential_versions WHERE node_id=? AND serial=?)`, updated, nodeID, nodeID, serial); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) SaveConfigResult(ctx context.Context, transactionID string, version uint64, state, reason string) error {
	if transactionID == "" || state == "" {
		return fmt.Errorf("store: config transaction and state are required")
	}
	legacyVersion := int64(0)
	if version <= math.MaxInt64 {
		legacyVersion = int64(version)
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO config_results(transaction_id, version, version_u64, state, reason, updated_at) VALUES (?, ?, ?, ?, ?, ?) ON CONFLICT(transaction_id) DO UPDATE SET version=excluded.version, version_u64=excluded.version_u64, state=excluded.state, reason=excluded.reason, updated_at=excluded.updated_at`, transactionID, legacyVersion, strconv.FormatUint(version, 10), state, reason, time.Now().UTC().Format(time.RFC3339Nano))
	return err
}

func (s *Store) GetConfigResult(ctx context.Context, transactionID string) (ConfigResult, error) {
	var result ConfigResult
	result.TransactionID = transactionID
	var legacyVersion int64
	var version, generation, expected string
	err := s.db.QueryRowContext(ctx, `SELECT version, version_u64, fencing_generation, expected_version, payload, payload_sha256, state, reason FROM config_results WHERE transaction_id = ?`, transactionID).
		Scan(&legacyVersion, &version, &generation, &expected, &result.Payload, &result.PayloadSHA256, &result.State, &result.Reason)
	if err != nil {
		return result, err
	}
	var parseErr error
	result.Version, parseErr = strconv.ParseUint(version, 10, 64)
	if parseErr != nil {
		return result, parseErr
	}
	if result.Version == 0 && legacyVersion > 0 {
		result.Version = uint64(legacyVersion)
	}
	result.FencingGeneration, parseErr = strconv.ParseUint(generation, 10, 64)
	if parseErr != nil {
		return result, parseErr
	}
	result.ExpectedVersion, parseErr = strconv.ParseUint(expected, 10, 64)
	if parseErr != nil {
		return result, parseErr
	}
	return result, err
}

func (s *Store) ConfigResultCount(ctx context.Context) (int, error) {
	var count int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM config_results`).Scan(&count)
	return count, err
}

// SaveConfigRequest atomically persists the immutable request and advances the
// fencing lower bound before the Engine Prepare call can occur.
func (s *Store) SaveConfigRequest(ctx context.Context, request ConfigResult) (ConfigResult, error) {
	if request.TransactionID == "" || request.Version == 0 || request.FencingGeneration == 0 || len(request.Payload) == 0 || len(request.PayloadSHA256) != 32 {
		return ConfigResult{}, fmt.Errorf("store: incomplete config request")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return ConfigResult{}, err
	}
	defer tx.Rollback()
	var previous ConfigResult
	var versionText, generationText, expectedText string
	err = tx.QueryRowContext(ctx, `SELECT version_u64, fencing_generation, expected_version, payload, payload_sha256, state, reason FROM config_results WHERE transaction_id=?`, request.TransactionID).
		Scan(&versionText, &generationText, &expectedText, &previous.Payload, &previous.PayloadSHA256, &previous.State, &previous.Reason)
	if err == nil {
		previous.TransactionID = request.TransactionID
		previous.Version, err = strconv.ParseUint(versionText, 10, 64)
		if err == nil {
			previous.FencingGeneration, err = strconv.ParseUint(generationText, 10, 64)
		}
		if err == nil {
			previous.ExpectedVersion, err = strconv.ParseUint(expectedText, 10, 64)
		}
		if err != nil {
			return ConfigResult{}, err
		}
		if previous.Version != request.Version || previous.FencingGeneration != request.FencingGeneration || previous.ExpectedVersion != request.ExpectedVersion || !bytes.Equal(previous.Payload, request.Payload) || !bytes.Equal(previous.PayloadSHA256, request.PayloadSHA256) {
			return ConfigResult{}, ErrConfigRequestConflict
		}
		return previous, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return ConfigResult{}, err
	}
	var activeVersionText string
	err = tx.QueryRowContext(ctx, `SELECT value FROM agent_state WHERE key='applied_version'`).Scan(&activeVersionText)
	activeVersion := uint64(0)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return ConfigResult{}, err
	}
	if err == nil {
		activeVersion, err = strconv.ParseUint(activeVersionText, 10, 64)
		if err != nil {
			return ConfigResult{}, fmt.Errorf("store: applied version is invalid: %w", err)
		}
	}
	if request.ExpectedVersion != activeVersion || request.Version <= activeVersion {
		return ConfigResult{}, ErrConfigVersionStale
	}
	var currentGenerationText string
	err = tx.QueryRowContext(ctx, `SELECT value FROM agent_state WHERE key='fencing_generation'`).Scan(&currentGenerationText)
	activeGeneration := uint64(0)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return ConfigResult{}, err
	}
	if err == nil {
		activeGeneration, err = strconv.ParseUint(currentGenerationText, 10, 64)
		if err != nil {
			return ConfigResult{}, fmt.Errorf("store: fencing generation is invalid: %w", err)
		}
	}
	if request.FencingGeneration <= activeGeneration {
		return ConfigResult{}, ErrConfigGenerationStale
	}
	var unresolved int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM config_results WHERE state IN ('commit-requested','activation-unknown')`).Scan(&unresolved); err != nil {
		return ConfigResult{}, err
	}
	if unresolved != 0 {
		return ConfigResult{}, ErrConfigUnresolved
	}
	legacyVersion := int64(0)
	if request.Version <= math.MaxInt64 {
		legacyVersion = int64(request.Version)
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO config_results(transaction_id, version, version_u64, fencing_generation, expected_version, payload, payload_sha256, state, reason, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, 'received', '', ?)`, request.TransactionID, legacyVersion, strconv.FormatUint(request.Version, 10), strconv.FormatUint(request.FencingGeneration, 10), strconv.FormatUint(request.ExpectedVersion, 10), request.Payload, request.PayloadSHA256, time.Now().UTC().Format(time.RFC3339Nano))
	if err != nil {
		return ConfigResult{}, err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO agent_state(key, value, updated_at) VALUES ('fencing_generation', ?, ?) ON CONFLICT(key) DO UPDATE SET value=excluded.value, updated_at=excluded.updated_at`, strconv.FormatUint(request.FencingGeneration, 10), time.Now().UTC().Format(time.RFC3339Nano))
	if err != nil {
		return ConfigResult{}, err
	}
	if err := tx.Commit(); err != nil {
		return ConfigResult{}, err
	}
	request.State = "received"
	return request, nil
}

func (s *Store) FinalizeConfigResult(ctx context.Context, transactionID string, version uint64, objects []ObjectResult) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	updated := time.Now().UTC().Format(time.RFC3339Nano)
	for _, result := range objects {
		if result.ObjectID == "" || result.State == "" {
			return fmt.Errorf("store: object result identity and state required")
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO config_object_results(transaction_id, object_id, state, reason, updated_at) VALUES (?, ?, ?, ?, ?) ON CONFLICT(transaction_id, object_id) DO UPDATE SET state=excluded.state, reason=excluded.reason, updated_at=excluded.updated_at`, transactionID, result.ObjectID, result.State, result.Reason, updated); err != nil {
			return err
		}
	}
	legacyVersion := int64(0)
	if version <= math.MaxInt64 {
		legacyVersion = int64(version)
	}
	if _, err := tx.ExecContext(ctx, `UPDATE config_results SET version=?, version_u64=?, state='active', reason='', updated_at=? WHERE transaction_id=?`, legacyVersion, strconv.FormatUint(version, 10), updated, transactionID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO agent_state(key, value, updated_at) VALUES ('applied_version', ?, ?) ON CONFLICT(key) DO UPDATE SET value=excluded.value, updated_at=excluded.updated_at`, strconv.FormatUint(version, 10), updated); err != nil {
		return err
	}
	return tx.Commit()
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
