package store

import (
	"bytes"
	"context"
	"crypto/sha256"
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

type UsageBatch struct {
	NodeID       string
	BootID       string
	BatchID      string
	SnapshotID   string
	Payload      []byte
	Digest       []byte
	UnknownUpper uint64
	State        string
	CreatedAt    time.Time
}

type UsageReservation struct {
	NodeID          string
	BootID          string
	BatchID         string
	AuthorizationID string
	SnapshotID      string
	FlowKey         string
	RuleID          string
	Direction       string
	Reserved        uint64
	Resolution      string
}

type AuthorizationRecord struct {
	DelegationID          string
	ShareReferenceID      string
	ShareRevision         uint64
	IssuerID              string
	AuthorizationID       string
	ParentAuthorizationID string
	SourceID              string
	Kind                  string
	RuleID                string
	RuleRevision          uint64
	PathGeneration        uint64
	SignerEpoch           uint64
	BootID                string
	IssuedAt              string
	NotBefore             string
	ExpiresAt             string
	HardDeadline          string
	BillingSnapshotID     string
	AmountBytes           uint64
	RateBPS               uint64
	Connections           uint64
}

type AuthorizationUpdate struct {
	ShareReferences        []ShareReference
	Handoffs               []EpochHandoff
	IssuerID               string
	BackendHeartbeat       time.Time
	AutonomyDeadline       time.Time
	NodeID                 string
	BootID                 string
	UpdateID               string
	Payload                []byte
	Digest                 []byte
	SignerEpoch            uint64
	PathGeneration         uint64
	FencingGenerationFloor uint64
}

var ErrCredentialRevoked = fmt.Errorf("store: credential revoked")

var (
	ErrConfigRequestConflict        = errors.New("store: config transaction request conflicts with durable request")
	ErrConfigVersionStale           = errors.New("store: config expected version is stale")
	ErrConfigGenerationStale        = errors.New("store: config fencing generation is stale")
	ErrConfigUnresolved             = errors.New("store: an activation outcome is unresolved")
	ErrUsageBatchConflict           = errors.New("store: usage batch identity conflicts with durable payload")
	ErrAuthorizationUpdateConflict  = errors.New("store: authorization update identity conflicts with durable payload")
	ErrAuthorizationGenerationStale = errors.New("store: authorization signer or path generation is stale")
)

func Open(path, owner string) (*Store, error) {
	if path == "" || owner == "" {
		return nil, fmt.Errorf("store: path and owner are required")
	}
	dsn, err := ledgerDSN(path, false)
	if err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", dsn)
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
CREATE TABLE IF NOT EXISTS activation_holds (
 transaction_id TEXT PRIMARY KEY NOT NULL, generation TEXT NOT NULL,
 state TEXT NOT NULL, FOREIGN KEY(transaction_id) REFERENCES config_results(transaction_id)
);
CREATE TABLE IF NOT EXISTS activation_processes (
 transaction_id TEXT PRIMARY KEY NOT NULL, witness BLOB NOT NULL,
 FOREIGN KEY(transaction_id) REFERENCES config_results(transaction_id)
);
CREATE TABLE IF NOT EXISTS activation_executors (
 transaction_id TEXT PRIMARY KEY NOT NULL, execution_id TEXT NOT NULL,
 FOREIGN KEY(transaction_id) REFERENCES config_results(transaction_id)
);
CREATE TABLE IF NOT EXISTS activation_observations (
 sequence INTEGER PRIMARY KEY AUTOINCREMENT, transaction_id TEXT NOT NULL,
 generation TEXT NOT NULL, execution_state TEXT NOT NULL, observed_at TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS usage_batches (
  node_id TEXT NOT NULL,
  batch_id TEXT NOT NULL,
  boot_id TEXT NOT NULL,
  snapshot_id TEXT NOT NULL,
  payload BLOB NOT NULL,
  payload_sha256 BLOB NOT NULL,
  unknown_upper_bound TEXT NOT NULL,
  state TEXT NOT NULL,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  PRIMARY KEY(node_id, batch_id)
);
CREATE TABLE IF NOT EXISTS usage_reservations (
  node_id TEXT NOT NULL,
  boot_id TEXT NOT NULL,
  batch_id TEXT NOT NULL,
  snapshot_id TEXT NOT NULL,
  flow_key TEXT NOT NULL,
  rule_id TEXT NOT NULL,
  direction TEXT NOT NULL,
  reserved_bytes TEXT NOT NULL,
  state TEXT NOT NULL,
  used_bytes TEXT NOT NULL DEFAULT '0',
  updated_at TEXT NOT NULL,
  PRIMARY KEY(node_id, batch_id)
);
CREATE TABLE IF NOT EXISTS authorization_updates (
  node_id TEXT NOT NULL,
  update_id TEXT NOT NULL,
  issuer_id TEXT NOT NULL DEFAULT '',
  boot_id TEXT NOT NULL,
  payload BLOB NOT NULL,
  payload_sha256 BLOB NOT NULL,
  signer_epoch TEXT NOT NULL,
  path_generation TEXT NOT NULL,
  fencing_generation_floor TEXT NOT NULL,
  created_at TEXT NOT NULL,
  PRIMARY KEY(node_id, update_id)
);
CREATE TABLE IF NOT EXISTS authorization_records (
  node_id TEXT NOT NULL,
  authorization_id TEXT NOT NULL,
  issuer_id TEXT NOT NULL DEFAULT '',
  parent_authorization_id TEXT NOT NULL,
  source_id TEXT NOT NULL,
  kind TEXT NOT NULL,
  rule_id TEXT NOT NULL,
  rule_revision TEXT NOT NULL,
  path_generation TEXT NOT NULL,
  signer_epoch TEXT NOT NULL,
  boot_id TEXT NOT NULL,
  issued_at TEXT NOT NULL,
  not_before TEXT NOT NULL,
  expires_at TEXT NOT NULL,
  hard_deadline TEXT NOT NULL,
  billing_snapshot_id TEXT NOT NULL,
  amount_bytes TEXT NOT NULL,
  rate_bps TEXT NOT NULL,
  connections TEXT NOT NULL,
  state TEXT NOT NULL,
  PRIMARY KEY(node_id, authorization_id)
);
CREATE TABLE IF NOT EXISTS authorization_balances (
  node_id TEXT NOT NULL,
  authorization_id TEXT NOT NULL,
  source_id TEXT NOT NULL,
  initial_bytes TEXT NOT NULL,
  remaining_bytes TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  PRIMARY KEY(node_id, authorization_id),
  FOREIGN KEY(node_id, authorization_id) REFERENCES authorization_records(node_id, authorization_id)
);
CREATE TABLE IF NOT EXISTS authorization_source_owners (
  node_id TEXT NOT NULL,
  source_id TEXT NOT NULL,
  authorization_id TEXT NOT NULL,
  initial_bytes TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  PRIMARY KEY(node_id, source_id),
  UNIQUE(node_id, authorization_id),
  FOREIGN KEY(node_id, authorization_id) REFERENCES authorization_records(node_id, authorization_id)
);
CREATE TABLE IF NOT EXISTS authorization_usage_holds (
  node_id TEXT NOT NULL,
  batch_id TEXT NOT NULL,
  authorization_id TEXT NOT NULL,
  source_id TEXT NOT NULL,
  reserved_bytes TEXT NOT NULL,
  state TEXT NOT NULL,
  PRIMARY KEY(node_id, batch_id),
  FOREIGN KEY(node_id, authorization_id) REFERENCES authorization_records(node_id, authorization_id)
);
CREATE TABLE IF NOT EXISTS authorization_revocations (
  node_id TEXT NOT NULL,
  authorization_id TEXT NOT NULL,
  signer_epoch TEXT NOT NULL,
  created_at TEXT NOT NULL,
  PRIMARY KEY(node_id, authorization_id)
);
CREATE TABLE IF NOT EXISTS autonomy_transitions (
  transition_id INTEGER PRIMARY KEY AUTOINCREMENT,
  from_mode TEXT NOT NULL,
  to_mode TEXT NOT NULL,
  occurred_at TEXT NOT NULL,
  backend_heartbeat TEXT NOT NULL,
  hard_deadline TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS runtime_sessions (
 node_id TEXT NOT NULL, session_id TEXT NOT NULL, boot_id TEXT NOT NULL,
 flow_key TEXT NOT NULL, source_id TEXT NOT NULL, grant_id TEXT NOT NULL,
 runtime_lease_id TEXT NOT NULL, share_lease_id TEXT NOT NULL,
 initial_grant_id TEXT NOT NULL DEFAULT '', initial_runtime_lease_id TEXT NOT NULL DEFAULT '',
 initial_share_lease_id TEXT NOT NULL DEFAULT '',
 state TEXT NOT NULL, PRIMARY KEY(node_id,session_id), UNIQUE(node_id,boot_id,flow_key)
);
CREATE TABLE IF NOT EXISTS runtime_rate_buckets (
 node_id TEXT NOT NULL, source_id TEXT NOT NULL, boot_id TEXT NOT NULL,
 tokens INTEGER NOT NULL, last_tick INTEGER NOT NULL, PRIMARY KEY(node_id,source_id)
);
CREATE TABLE IF NOT EXISTS issuer_epochs (
 hub_id TEXT PRIMARY KEY NOT NULL, signer_epoch TEXT NOT NULL, state TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS issuer_delegations (
 delegation_id TEXT PRIMARY KEY NOT NULL, hub_id TEXT NOT NULL,
 source_id TEXT NOT NULL, signer_epoch TEXT NOT NULL,
 payload BLOB NOT NULL, payload_sha256 BLOB NOT NULL,
 remaining_bytes TEXT NOT NULL, state TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS issuer_issues (
 issue_id TEXT PRIMARY KEY NOT NULL, delegation_id TEXT NOT NULL,
 request BLOB NOT NULL, payload BLOB NOT NULL, payload_sha256 BLOB NOT NULL,
 amount_bytes TEXT NOT NULL, created_at TEXT NOT NULL,
 FOREIGN KEY(delegation_id) REFERENCES issuer_delegations(delegation_id)
);
CREATE TABLE IF NOT EXISTS renewal_requests (
 request_id TEXT PRIMARY KEY NOT NULL, issuer_id TEXT NOT NULL,
 node_id TEXT NOT NULL, boot_id TEXT NOT NULL, delegation_id TEXT NOT NULL,
 source_id TEXT NOT NULL, rule_id TEXT NOT NULL, snapshot_id TEXT NOT NULL,
 payload BLOB NOT NULL, payload_sha256 BLOB NOT NULL,
 state TEXT NOT NULL CHECK(state IN ('pending','received','installed','rejected')),
 result BLOB NOT NULL DEFAULT X'', result_sha256 BLOB NOT NULL DEFAULT X'',
 created_at TEXT NOT NULL, updated_at TEXT NOT NULL
);
CREATE UNIQUE INDEX IF NOT EXISTS renewal_one_outstanding_lineage ON renewal_requests
 (node_id,delegation_id,source_id,rule_id,snapshot_id)
 WHERE state IN ('pending','received');
CREATE TABLE IF NOT EXISTS unknown_usage_batches (
 node_id TEXT NOT NULL, batch_id TEXT NOT NULL, origin_boot_id TEXT NOT NULL,
 snapshot_id TEXT NOT NULL, grant_id TEXT NOT NULL, rule_id TEXT NOT NULL,
 direction TEXT NOT NULL, unknown_upper_bound TEXT NOT NULL,
 payload BLOB NOT NULL, payload_sha256 BLOB NOT NULL,
 transport_state TEXT NOT NULL, liability_state TEXT NOT NULL,
 legacy_before_image BLOB, created_at TEXT NOT NULL,
 PRIMARY KEY(node_id,batch_id)
);
CREATE TABLE IF NOT EXISTS unknown_usage_conflicts (
 node_id TEXT NOT NULL, batch_id TEXT NOT NULL, payload BLOB NOT NULL,
 payload_sha256 BLOB NOT NULL, created_at TEXT NOT NULL,
 PRIMARY KEY(node_id,batch_id,payload_sha256)
);
CREATE TABLE IF NOT EXISTS usage_backend_receipts (
 node_id TEXT NOT NULL, batch_id TEXT NOT NULL, kind TEXT NOT NULL,
 payload_sha256 BLOB NOT NULL, confirmed_at TEXT NOT NULL,
 PRIMARY KEY(node_id,batch_id)
);
CREATE TABLE IF NOT EXISTS hub_recovery_updates (
 handoff_id TEXT NOT NULL,node_id TEXT NOT NULL,boot_id TEXT NOT NULL,path_generation TEXT NOT NULL,
 payload BLOB NOT NULL,payload_sha256 BLOB NOT NULL,
 PRIMARY KEY(handoff_id,node_id,boot_id,path_generation)
);
CREATE TABLE IF NOT EXISTS authorization_handoffs (
 node_id TEXT NOT NULL, source_id TEXT NOT NULL, from_epoch TEXT NOT NULL,
 to_epoch TEXT NOT NULL, transfer_id TEXT NOT NULL,
 payload BLOB NOT NULL, remaining_bytes TEXT NOT NULL,
 PRIMARY KEY(node_id,source_id,from_epoch), UNIQUE(node_id,source_id,to_epoch),
 UNIQUE(node_id,source_id,transfer_id)
);
CREATE TABLE IF NOT EXISTS hub_ledger (
 sequence INTEGER PRIMARY KEY AUTOINCREMENT,
 kind TEXT NOT NULL, record_id TEXT NOT NULL, payload BLOB NOT NULL,
 payload_sha256 BLOB NOT NULL, confirmed INTEGER NOT NULL DEFAULT 0,
 UNIQUE(kind,record_id)
);
CREATE TABLE IF NOT EXISTS hub_handoffs (
 handoff_id TEXT PRIMARY KEY, hub_id TEXT NOT NULL, from_epoch TEXT NOT NULL,
 to_epoch TEXT NOT NULL, sealed_sequence INTEGER NOT NULL,
 request BLOB NOT NULL, UNIQUE(hub_id,from_epoch), UNIQUE(hub_id,to_epoch)
);
CREATE TABLE IF NOT EXISTS hub_transfers (
 handoff_id TEXT NOT NULL, delegation_id TEXT NOT NULL, remaining_bytes TEXT NOT NULL,
 PRIMARY KEY(handoff_id,delegation_id),
 FOREIGN KEY(handoff_id) REFERENCES hub_handoffs(handoff_id)
);
CREATE TRIGGER IF NOT EXISTS hub_journal_issue AFTER INSERT ON issuer_issues BEGIN
 INSERT INTO hub_ledger(kind,record_id,payload,payload_sha256)
 VALUES('issue',NEW.issue_id,NEW.payload,NEW.payload_sha256);
END;
CREATE TRIGGER IF NOT EXISTS hub_journal_usage AFTER INSERT ON usage_batches
 WHEN NEW.state='ledgered' AND NEW.unknown_upper_bound='0' BEGIN
 INSERT INTO hub_ledger(kind,record_id,payload,payload_sha256)
 VALUES('known',NEW.node_id || '/' || NEW.batch_id,NEW.payload,NEW.payload_sha256);
END;
CREATE TRIGGER IF NOT EXISTS hub_journal_usage_update AFTER UPDATE OF state ON usage_batches
 WHEN NEW.state='ledgered' AND NEW.unknown_upper_bound='0' BEGIN
 INSERT OR IGNORE INTO hub_ledger(kind,record_id,payload,payload_sha256)
 VALUES('known',NEW.node_id || '/' || NEW.batch_id,NEW.payload,NEW.payload_sha256);
END;
CREATE TRIGGER IF NOT EXISTS hub_journal_unknown AFTER INSERT ON unknown_usage_batches BEGIN
 INSERT INTO hub_ledger(kind,record_id,payload,payload_sha256)
 VALUES('unknown',NEW.node_id || '/' || NEW.batch_id,NEW.payload,NEW.payload_sha256);
END;
INSERT OR IGNORE INTO hub_ledger(kind,record_id,payload,payload_sha256)
 SELECT 'issue',issue_id,payload,payload_sha256 FROM issuer_issues i WHERE NOT EXISTS (SELECT 1 FROM hub_ledger l WHERE l.kind='issue' AND l.record_id=i.issue_id) ORDER BY rowid;
INSERT OR IGNORE INTO hub_ledger(kind,record_id,payload,payload_sha256)
 SELECT 'known',node_id || '/' || batch_id,payload,payload_sha256 FROM usage_batches u WHERE state='ledgered' AND unknown_upper_bound='0' AND NOT EXISTS (SELECT 1 FROM hub_ledger l WHERE l.kind='known' AND l.record_id=u.node_id || '/' || u.batch_id) ORDER BY rowid;
INSERT OR IGNORE INTO hub_ledger(kind,record_id,payload,payload_sha256)
 SELECT 'unknown',node_id || '/' || batch_id,payload,payload_sha256 FROM unknown_usage_batches u WHERE NOT EXISTS (SELECT 1 FROM hub_ledger l WHERE l.kind='unknown' AND l.record_id=u.node_id || '/' || u.batch_id) ORDER BY rowid;
INSERT INTO schema_meta(key, value) VALUES ('schema_version', '14')
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
	for _, table := range []string{"authorization_updates", "authorization_records"} {
		if err := s.ensureColumn(table, "issuer_id", "TEXT NOT NULL DEFAULT ''"); err != nil {
			return err
		}
	}
	for _, column := range []string{"initial_grant_id", "initial_runtime_lease_id", "initial_share_lease_id"} {
		if err := s.ensureColumn("runtime_sessions", column, "TEXT NOT NULL DEFAULT ''"); err != nil {
			return err
		}
	}
	if _, err := s.db.Exec(`UPDATE runtime_sessions SET initial_grant_id=grant_id,initial_runtime_lease_id=runtime_lease_id,initial_share_lease_id=share_lease_id WHERE initial_grant_id=''`); err != nil {
		return err
	}
	if err := s.migrateRenewalAuthority(); err != nil {
		return err
	}
	return s.migrateUsageRetention()
}

// PersistUsageOutbox stores exact serialized usage bytes and the conservative
// unknown-liability upper bound before the caller may send or locally ACK them.
func (s *Store) PersistUsageOutbox(ctx context.Context, batch UsageBatch) error {
	if batch.NodeID == "" || batch.BootID == "" || batch.BatchID == "" || batch.SnapshotID == "" || len(batch.Payload) == 0 {
		return fmt.Errorf("store: usage batch identity, snapshot and payload are required")
	}
	if len(batch.Payload) > 1<<20 {
		return fmt.Errorf("store: usage batch payload exceeds 1 MiB")
	}
	digest := sha256.Sum256(batch.Payload)
	if len(batch.Digest) != 0 && !bytes.Equal(batch.Digest, digest[:]) {
		return fmt.Errorf("store: usage batch digest mismatch")
	}
	return s.putUsageBatch(ctx, batch, digest[:], "pending")
}

// ReserveUsage durably records the full upper bound before Engine receives a
// writable batch. Repeating the same identity is idempotent.
func (s *Store) ReserveUsage(ctx context.Context, reservation UsageReservation) error {
	if reservation.NodeID == "" || reservation.BootID == "" || reservation.BatchID == "" || reservation.SnapshotID == "" || reservation.RuleID == "" || reservation.FlowKey == "" || reservation.Reserved == 0 || (reservation.Direction != "egress" && reservation.Direction != "ingress") {
		return fmt.Errorf("store: incomplete usage reservation")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var previous UsageReservation
	var reserved string
	err = tx.QueryRowContext(ctx, `SELECT boot_id,snapshot_id,flow_key,rule_id,direction,reserved_bytes,state FROM usage_reservations WHERE node_id=? AND batch_id=?`, reservation.NodeID, reservation.BatchID).
		Scan(&previous.BootID, &previous.SnapshotID, &previous.FlowKey, &previous.RuleID, &previous.Direction, &reserved, &previous.Resolution)
	if err == nil {
		previous.NodeID, previous.BatchID = reservation.NodeID, reservation.BatchID
		previous.Reserved, err = strconv.ParseUint(reserved, 10, 64)
		if err != nil {
			return fmt.Errorf("store: invalid durable reservation amount: %w", err)
		}
		if previous.BootID != reservation.BootID || previous.SnapshotID != reservation.SnapshotID || previous.FlowKey != reservation.FlowKey || previous.RuleID != reservation.RuleID || previous.Direction != reservation.Direction || previous.Reserved != reservation.Reserved {
			return ErrUsageBatchConflict
		}
		if previous.Resolution != "reserved" {
			return fmt.Errorf("store: usage reservation already resolved as %s", previous.Resolution)
		}
		return tx.Commit()
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO usage_reservations(node_id,boot_id,batch_id,snapshot_id,flow_key,rule_id,direction,reserved_bytes,state,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?)`, reservation.NodeID, reservation.BootID, reservation.BatchID, reservation.SnapshotID, reservation.FlowKey, reservation.RuleID, reservation.Direction, strconv.FormatUint(reservation.Reserved, 10), "reserved", time.Now().UTC().Format(time.RFC3339Nano))
	if err != nil {
		return err
	}
	return tx.Commit()
}

// SettleUsageReservation atomically resolves a pre-write hold and, for actual
// or unknown usage, places its immutable payload in the durable handoff outbox.
func (s *Store) SettleUsageReservation(ctx context.Context, reservation UsageReservation, resolution string, used uint64, payload []byte, unknownUpper uint64) error {
	return s.settleUsageReservation(ctx, reservation, resolution, used, payload, unknownUpper, nil)
}

func (s *Store) SettleUnknownUsageReservation(ctx context.Context, reservation UsageReservation, payload []byte) error {
	r, err := unknownRecord(reservation.NodeID, payload)
	if err != nil {
		return err
	}
	if r.BatchID != reservation.BatchID || r.OriginBootID != reservation.BootID || r.SnapshotID != reservation.SnapshotID || r.GrantID != reservation.AuthorizationID || r.RuleID != reservation.RuleID || r.Direction != reservation.Direction || r.Upper != reservation.Reserved {
		return ErrUsageBatchConflict
	}
	return s.settleUsageReservation(ctx, reservation, "unknown", reservation.Reserved, payload, reservation.Reserved, &r)
}

func (s *Store) settleUsageReservation(ctx context.Context, reservation UsageReservation, resolution string, used uint64, payload []byte, unknownUpper uint64, typed *UnknownUsageRecord) error {
	if resolution != "committed" && resolution != "returned" && resolution != "unknown" {
		return fmt.Errorf("store: invalid usage reservation resolution")
	}
	if resolution == "committed" && (used > reservation.Reserved || len(payload) == 0 || unknownUpper != 0) || resolution == "unknown" && (used != reservation.Reserved || unknownUpper != reservation.Reserved || len(payload) == 0) || resolution == "returned" && (used != 0 || unknownUpper != 0 || len(payload) != 0) {
		return fmt.Errorf("store: inconsistent usage settlement")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var bootID, snapshotID, flowKey, ruleID, direction, reservedText, state, usedText string
	err = tx.QueryRowContext(ctx, `SELECT boot_id,snapshot_id,flow_key,rule_id,direction,reserved_bytes,state,used_bytes FROM usage_reservations WHERE node_id=? AND batch_id=?`, reservation.NodeID, reservation.BatchID).
		Scan(&bootID, &snapshotID, &flowKey, &ruleID, &direction, &reservedText, &state, &usedText)
	if err != nil {
		return err
	}
	storedReserved, err := strconv.ParseUint(reservedText, 10, 64)
	if err != nil {
		return err
	}
	if bootID != reservation.BootID || snapshotID != reservation.SnapshotID || flowKey != reservation.FlowKey || ruleID != reservation.RuleID || direction != reservation.Direction || storedReserved != reservation.Reserved {
		return ErrUsageBatchConflict
	}
	var durableAuthorizationID string
	authorityErr := tx.QueryRowContext(ctx, `SELECT authorization_id FROM authorization_usage_holds WHERE node_id=? AND batch_id=?`, reservation.NodeID, reservation.BatchID).Scan(&durableAuthorizationID)
	if authorityErr != nil && !errors.Is(authorityErr, sql.ErrNoRows) {
		return authorityErr
	}
	if durableAuthorizationID != reservation.AuthorizationID {
		return ErrUsageBatchConflict
	}
	if state != "reserved" {
		storedUsed, parseErr := strconv.ParseUint(usedText, 10, 64)
		if parseErr != nil || state != resolution || storedUsed != used {
			return ErrUsageBatchConflict
		}
		if resolution != "returned" {
			if typed != nil {
				var previousPayload []byte
				if err := tx.QueryRowContext(ctx, `SELECT payload FROM unknown_usage_batches WHERE node_id=? AND batch_id=?`, reservation.NodeID, reservation.BatchID).Scan(&previousPayload); err != nil {
					return err
				}
				if !bytes.Equal(previousPayload, payload) {
					return ErrUsageBatchConflict
				}
				return tx.Commit()
			}
			var storedPayload []byte
			var storedUpper string
			var storedDigest []byte
			if err := tx.QueryRowContext(ctx, `SELECT payload,payload_sha256,unknown_upper_bound FROM usage_batches WHERE node_id=? AND batch_id=?`, reservation.NodeID, reservation.BatchID).Scan(&storedPayload, &storedDigest, &storedUpper); err != nil {
				return err
			}
			digest := sha256.Sum256(payload)
			upper, parseErr := strconv.ParseUint(storedUpper, 10, 64)
			if parseErr != nil || upper != unknownUpper || !bytes.Equal(storedPayload, payload) || !bytes.Equal(storedDigest, digest[:]) {
				return ErrUsageBatchConflict
			}
		}
		return tx.Commit()
	}
	var holdReserved, holdState string
	holdErr := tx.QueryRowContext(ctx, `SELECT reserved_bytes,state FROM authorization_usage_holds WHERE node_id=? AND batch_id=?`, reservation.NodeID, reservation.BatchID).Scan(&holdReserved, &holdState)
	if holdErr == nil {
		if holdState != "reserved" {
			return ErrUsageBatchConflict
		}
		held, parseErr := strconv.ParseUint(holdReserved, 10, 64)
		if parseErr != nil || held != reservation.Reserved || (resolution != "returned" && resolution != "committed" && resolution != "unknown") {
			return ErrUsageBatchConflict
		}
		if resolution != "unknown" {
			refund := held - used
			if resolution == "returned" {
				refund = held
			}
			if refund > 0 {
				var sourceID, authID, remainingText string
				if err := tx.QueryRowContext(ctx, `SELECT source_id,authorization_id FROM authorization_usage_holds WHERE node_id=? AND batch_id=?`, reservation.NodeID, reservation.BatchID).Scan(&sourceID, &authID); err != nil {
					return err
				}
				if err := tx.QueryRowContext(ctx, `SELECT remaining_bytes FROM authorization_balances WHERE node_id=? AND authorization_id=? AND source_id=?`, reservation.NodeID, authID, sourceID).Scan(&remainingText); err != nil {
					return err
				}
				remaining, parseErr := strconv.ParseUint(remainingText, 10, 64)
				if parseErr != nil || remaining > math.MaxUint64-refund {
					return ErrUsageBatchConflict
				}
				var initialText string
				if err := tx.QueryRowContext(ctx, `SELECT initial_bytes FROM authorization_balances WHERE node_id=? AND authorization_id=?`, reservation.NodeID, authID).Scan(&initialText); err != nil {
					return err
				}
				initial, parseErr := strconv.ParseUint(initialText, 10, 64)
				if parseErr != nil || remaining+refund > initial {
					return ErrUsageBatchConflict
				}
				if _, err := tx.ExecContext(ctx, `UPDATE authorization_balances SET remaining_bytes=?,updated_at=? WHERE node_id=? AND authorization_id=?`, strconv.FormatUint(remaining+refund, 10), time.Now().UTC().Format(time.RFC3339Nano), reservation.NodeID, authID); err != nil {
					return err
				}
			}
		}
		if _, err := tx.ExecContext(ctx, `UPDATE authorization_usage_holds SET state=? WHERE node_id=? AND batch_id=? AND state='reserved'`, resolution, reservation.NodeID, reservation.BatchID); err != nil {
			return err
		}
	} else if !errors.Is(holdErr, sql.ErrNoRows) {
		return holdErr
	}
	if typed != nil {
		if err := insertUnknownUsageTx(ctx, tx, *typed, "pending", nil); err != nil {
			return err
		}
	} else if resolution != "returned" {
		if err := rejectUnknownIdentityTx(ctx, tx, reservation.NodeID, reservation.BatchID); err != nil {
			return err
		}
		digest := sha256.Sum256(payload)
		now := time.Now().UTC().Format(time.RFC3339Nano)
		_, err = tx.ExecContext(ctx, `INSERT INTO usage_batches(node_id,batch_id,boot_id,snapshot_id,payload,payload_sha256,unknown_upper_bound,state,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?)`, reservation.NodeID, reservation.BatchID, reservation.BootID, reservation.SnapshotID, payload, digest[:], strconv.FormatUint(unknownUpper, 10), "pending", now, now)
		if err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, `UPDATE usage_reservations SET state=?,used_bytes=?,updated_at=? WHERE node_id=? AND batch_id=? AND state='reserved'`, resolution, strconv.FormatUint(used, 10), time.Now().UTC().Format(time.RFC3339Nano), reservation.NodeID, reservation.BatchID); err != nil {
		return err
	}
	return tx.Commit()
}

// UnresolvedUsageReservations returns reservations from earlier boots. The
// caller must settle each as unknown before starting Engine traffic.
func (s *Store) UnresolvedUsageReservations(ctx context.Context, nodeID, currentBootID string) ([]UsageReservation, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT u.node_id,u.boot_id,u.batch_id,u.snapshot_id,u.flow_key,u.rule_id,u.direction,u.reserved_bytes,COALESCE(h.authorization_id,'') FROM usage_reservations u LEFT JOIN authorization_usage_holds h ON h.node_id=u.node_id AND h.batch_id=u.batch_id WHERE u.node_id=? AND u.boot_id<>? AND u.state='reserved' ORDER BY u.updated_at,u.batch_id`, nodeID, currentBootID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []UsageReservation
	for rows.Next() {
		var r UsageReservation
		var reserved string
		if err := rows.Scan(&r.NodeID, &r.BootID, &r.BatchID, &r.SnapshotID, &r.FlowKey, &r.RuleID, &r.Direction, &reserved, &r.AuthorizationID); err != nil {
			return nil, err
		}
		if r.Reserved, err = strconv.ParseUint(reserved, 10, 64); err != nil {
			return nil, err
		}
		r.Resolution = "reserved"
		result = append(result, r)
	}
	return result, rows.Err()
}

// PersistAuthorizationUpdate applies an authenticated update once. It stores
// authority epoch and path fencing independently and preserves revocations.
func (s *Store) PersistAuthorizationUpdate(ctx context.Context, update AuthorizationUpdate, records []AuthorizationRecord, revocations []string) error {
	if update.NodeID == "" || update.BootID == "" || update.UpdateID == "" || len(update.Payload) == 0 || update.SignerEpoch == 0 || update.PathGeneration == 0 || update.FencingGenerationFloor == 0 {
		return fmt.Errorf("store: incomplete authorization update")
	}
	digest := sha256.Sum256(update.Payload)
	if len(update.Digest) != 0 && !bytes.Equal(update.Digest, digest[:]) {
		return fmt.Errorf("store: authorization payload digest mismatch")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var previousDigest []byte
	var previousIssuer string
	err = tx.QueryRowContext(ctx, `SELECT payload_sha256,issuer_id FROM authorization_updates WHERE node_id=? AND update_id=?`, update.NodeID, update.UpdateID).Scan(&previousDigest, &previousIssuer)
	if err == nil {
		if !bytes.Equal(previousDigest, digest[:]) || previousIssuer != update.IssuerID {
			return ErrAuthorizationUpdateConflict
		}
		return tx.Commit()
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if err := persistShareReferencesTx(ctx, tx, update); err != nil {
		return err
	}
	for _, id := range revocations {
		if _, err := tx.ExecContext(ctx, `INSERT INTO authorization_revocations(node_id,authorization_id,signer_epoch,created_at) VALUES(?,?,?,?) ON CONFLICT(node_id,authorization_id) DO UPDATE SET signer_epoch=excluded.signer_epoch`, update.NodeID, id, strconv.FormatUint(update.SignerEpoch, 10), time.Now().UTC().Format(time.RFC3339Nano)); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE authorization_records SET state='revoked' WHERE node_id=? AND authorization_id=?`, update.NodeID, id); err != nil {
			return err
		}
	}
	for _, h := range update.Handoffs {
		if err := persistEpochHandoffTx(ctx, tx, update, h); err != nil {
			return err
		}
	}
	if (len(records) > 0 || len(update.Handoffs) > 0 || len(revocations) == 0) && (!update.BackendHeartbeat.IsZero() || !update.AutonomyDeadline.IsZero()) {
		if err := persistAuthorizationHeartbeat(ctx, tx, update.BackendHeartbeat, update.AutonomyDeadline); err != nil {
			if !errors.Is(err, ErrAuthorityHandoffRequired) || len(update.Handoffs) == 0 {
				return err
			}
			if err := resumeHandoffHeartbeatTx(ctx, tx, update); err != nil {
				return err
			}
		}
	}
	for key, incoming := range map[string]uint64{
		"authorization_signer_epoch":    update.SignerEpoch,
		"authorization_path_generation": update.PathGeneration,
		"authorization_fencing_floor":   update.FencingGenerationFloor,
	} {
		var previous string
		err := tx.QueryRowContext(ctx, `SELECT value FROM agent_state WHERE key=?`, key).Scan(&previous)
		if err == nil {
			value, parseErr := strconv.ParseUint(previous, 10, 64)
			if parseErr != nil || incoming < value {
				return ErrAuthorizationGenerationStale
			}
		} else if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if err := putState(ctx, tx, key, strconv.FormatUint(incoming, 10), time.Now().UTC()); err != nil {
			return err
		}
	}
	for _, record := range records {
		record.IssuerID = update.IssuerID
		if update.ShareReferences != nil && (record.ShareReferenceID == "" || record.DelegationID == "") {
			return ErrAuthorizationDenied
		}
		if record.ShareReferenceID != "" {
			record.ShareRevision, err = shareHeadTx(ctx, tx, update.NodeID, record.ShareReferenceID)
			if err != nil {
				return err
			}
			if err := checkAuthorizationShareTx(ctx, tx, update.NodeID, record, true); err != nil {
				return err
			}
		}
		if record.AuthorizationID == "" || record.BootID != update.BootID || record.SignerEpoch != update.SignerEpoch || record.PathGeneration != update.PathGeneration {
			return fmt.Errorf("store: authorization record does not match update identity or fencing")
		}
		var existing string
		err := tx.QueryRowContext(ctx, `SELECT source_id FROM authorization_records WHERE node_id=? AND authorization_id=?`, update.NodeID, record.AuthorizationID).Scan(&existing)
		if err == nil {
			return ErrAuthorizationUpdateConflict
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		var revoked int
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM authorization_revocations WHERE node_id=? AND authorization_id=?`, update.NodeID, record.AuthorizationID).Scan(&revoked); err != nil {
			return err
		}
		state := "active"
		if revoked > 0 {
			state = "revoked"
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO authorization_records(node_id,authorization_id,parent_authorization_id,source_id,kind,rule_id,rule_revision,path_generation,signer_epoch,boot_id,issued_at,not_before,expires_at,hard_deadline,billing_snapshot_id,amount_bytes,rate_bps,connections,state) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, update.NodeID, record.AuthorizationID, record.ParentAuthorizationID, record.SourceID, record.Kind, record.RuleID, strconv.FormatUint(record.RuleRevision, 10), strconv.FormatUint(record.PathGeneration, 10), strconv.FormatUint(record.SignerEpoch, 10), record.BootID, record.IssuedAt, record.NotBefore, record.ExpiresAt, record.HardDeadline, record.BillingSnapshotID, strconv.FormatUint(record.AmountBytes, 10), strconv.FormatUint(record.RateBPS, 10), strconv.FormatUint(record.Connections, 10), state)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE authorization_records SET issuer_id=? WHERE node_id=? AND authorization_id=?`, update.IssuerID, update.NodeID, record.AuthorizationID); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE authorization_records SET delegation_id=?,share_reference_id=?,share_revision=? WHERE node_id=? AND authorization_id=?`, record.DelegationID, record.ShareReferenceID, strconv.FormatUint(record.ShareRevision, 10), update.NodeID, record.AuthorizationID); err != nil {
			return err
		}
		if record.Kind == "credit_grant" {
			if record.AmountBytes == 0 {
				return fmt.Errorf("store: credit grant has no authorized amount")
			}
			now := time.Now().UTC().Format(time.RFC3339Nano)
			var currentOwner string
			ownerErr := tx.QueryRowContext(ctx, `SELECT authorization_id FROM authorization_source_owners WHERE node_id=? AND source_id=?`, update.NodeID, record.SourceID).Scan(&currentOwner)
			if errors.Is(ownerErr, sql.ErrNoRows) {
				if _, err := tx.ExecContext(ctx, `INSERT INTO authorization_source_owners(node_id,source_id,authorization_id,initial_bytes,updated_at) VALUES(?,?,?,?,?)`, update.NodeID, record.SourceID, record.AuthorizationID, strconv.FormatUint(record.AmountBytes, 10), now); err != nil {
					return err
				}
				if _, err := tx.ExecContext(ctx, `INSERT INTO authorization_balances(node_id,authorization_id,source_id,initial_bytes,remaining_bytes,updated_at) VALUES(?,?,?,?,?,?)`, update.NodeID, record.AuthorizationID, record.SourceID, strconv.FormatUint(record.AmountBytes, 10), strconv.FormatUint(record.AmountBytes, 10), now); err != nil {
					return err
				}
			} else if ownerErr != nil {
				return ownerErr
			} else {
				if err := checkSourceLineageTx(ctx, tx, update.NodeID, record); err != nil {
					return err
				}
				if err := spendHandoffBudgetTx(ctx, tx, update.NodeID, record); err != nil {
					return err
				}
				var initialText string
				if err := tx.QueryRowContext(ctx, `SELECT initial_bytes FROM authorization_source_owners WHERE node_id=? AND source_id=?`, update.NodeID, record.SourceID).Scan(&initialText); err != nil {
					return err
				}
				initial, err := strconv.ParseUint(initialText, 10, 64)
				if err != nil || record.AmountBytes > math.MaxUint64-initial {
					return ErrUsageBatchConflict
				}
				if _, err := tx.ExecContext(ctx, `UPDATE authorization_source_owners SET initial_bytes=?,updated_at=? WHERE node_id=? AND source_id=?`, strconv.FormatUint(initial+record.AmountBytes, 10), now, update.NodeID, record.SourceID); err != nil {
					return err
				}
				if _, err := tx.ExecContext(ctx, `INSERT INTO authorization_balances(node_id,authorization_id,source_id,initial_bytes,remaining_bytes,updated_at) VALUES(?,?,?,?,?,?)`, update.NodeID, record.AuthorizationID, record.SourceID, strconv.FormatUint(record.AmountBytes, 10), strconv.FormatUint(record.AmountBytes, 10), now); err != nil {
					return err
				}
			}
		}
	}

	_, err = tx.ExecContext(ctx, `INSERT INTO authorization_updates(node_id,update_id,boot_id,payload,payload_sha256,signer_epoch,path_generation,fencing_generation_floor,created_at) VALUES(?,?,?,?,?,?,?,?,?)`, update.NodeID, update.UpdateID, update.BootID, update.Payload, digest[:], strconv.FormatUint(update.SignerEpoch, 10), strconv.FormatUint(update.PathGeneration, 10), strconv.FormatUint(update.FencingGenerationFloor, 10), time.Now().UTC().Format(time.RFC3339Nano))
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE authorization_updates SET issuer_id=? WHERE node_id=? AND update_id=?`, update.IssuerID, update.NodeID, update.UpdateID); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) MarkPreviousBootAuthorizationsSettlementOnly(ctx context.Context, nodeID, bootID string) error {
	if nodeID == "" || bootID == "" {
		return fmt.Errorf("store: node and boot identity are required")
	}
	_, err := s.db.ExecContext(ctx, `UPDATE authorization_records SET state='settlement_only' WHERE node_id=? AND boot_id<>? AND state='active'`, nodeID, bootID)
	return err
}

// PersistUsageLedger commits a Hub-side batch before its acknowledgement is sent.
// Replaying identical bytes is idempotent; reusing an identity for new bytes fails.
func (s *Store) PersistUsageLedger(ctx context.Context, batch UsageBatch) error {
	if batch.NodeID == "" || batch.BootID == "" || batch.BatchID == "" || batch.SnapshotID == "" || len(batch.Payload) == 0 {
		return fmt.Errorf("store: usage batch identity, snapshot and payload are required")
	}
	if len(batch.Payload) > 1<<20 {
		return fmt.Errorf("store: usage batch payload exceeds 1 MiB")
	}
	digest := sha256.Sum256(batch.Payload)
	if len(batch.Digest) != 0 && !bytes.Equal(batch.Digest, digest[:]) {
		return fmt.Errorf("store: usage batch digest mismatch")
	}
	err := s.putUsageBatch(ctx, batch, digest[:], "ledgered")
	if errors.Is(err, ErrUsageBatchConflict) && s.owner == "hub" {
		if evidenceErr := s.RecordUsageDispute(ctx, batch.NodeID, batch.BatchID, batch.Payload); evidenceErr != nil {
			return fmt.Errorf("store: retain usage conflict: %w", evidenceErr)
		}
	}
	return err
}

func (s *Store) putUsageBatch(ctx context.Context, batch UsageBatch, digest []byte, initialState string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var snapshot string
	var compactDigest []byte
	err = tx.QueryRowContext(ctx, `SELECT snapshot_id,payload_sha256 FROM settled_usage_tombstones WHERE node_id=? AND batch_id=?`, batch.NodeID, batch.BatchID).Scan(&snapshot, &compactDigest)
	if err == nil {
		if s.owner != "hub" || initialState != "ledgered" || batch.UnknownUpper != 0 || snapshot != batch.SnapshotID || !bytes.Equal(compactDigest, digest) {
			return ErrUsageBatchConflict
		}
		return tx.Commit()
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	var previous UsageBatch
	var upper, created string
	err = tx.QueryRowContext(ctx, `SELECT boot_id, snapshot_id, payload, payload_sha256, unknown_upper_bound, state, created_at FROM usage_batches WHERE node_id=? AND batch_id=?`, batch.NodeID, batch.BatchID).
		Scan(&previous.BootID, &previous.SnapshotID, &previous.Payload, &previous.Digest, &upper, &previous.State, &created)
	if err == nil {
		previous.NodeID, previous.BatchID = batch.NodeID, batch.BatchID
		previous.UnknownUpper, err = strconv.ParseUint(upper, 10, 64)
		if err != nil {
			return fmt.Errorf("store: invalid usage liability: %w", err)
		}
		if previous.SnapshotID != batch.SnapshotID || previous.UnknownUpper != batch.UnknownUpper || !bytes.Equal(previous.Payload, batch.Payload) || !bytes.Equal(previous.Digest, digest) {
			return ErrUsageBatchConflict
		}
		if initialState == "ledgered" && previous.State != "ledgered" {
			if _, err := tx.ExecContext(ctx, `UPDATE usage_batches SET state='ledgered', updated_at=? WHERE node_id=? AND batch_id=?`, time.Now().UTC().Format(time.RFC3339Nano), batch.NodeID, batch.BatchID); err != nil {
				return err
			}
		}
		return tx.Commit()
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if err := rejectUnknownIdentityTx(ctx, tx, batch.NodeID, batch.BatchID); err != nil {
		return err
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err = tx.ExecContext(ctx, `INSERT INTO usage_batches(node_id,batch_id,boot_id,snapshot_id,payload,payload_sha256,unknown_upper_bound,state,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?)`, batch.NodeID, batch.BatchID, batch.BootID, batch.SnapshotID, batch.Payload, digest, strconv.FormatUint(batch.UnknownUpper, 10), initialState, now, now)
	if err != nil {
		return err
	}
	return tx.Commit()
}

// PendingUsageOutbox includes locally acknowledged batches: local autonomy ACK
// permits metering to continue, but does not discharge the Hub handoff liability.
func (s *Store) PendingUsageOutbox(ctx context.Context, nodeID string) ([]UsageBatch, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT node_id,boot_id,batch_id,snapshot_id,payload,payload_sha256,unknown_upper_bound,state,created_at FROM usage_batches WHERE node_id=? AND state IN ('pending','locally_acked') ORDER BY created_at,batch_id`, nodeID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var batches []UsageBatch
	for rows.Next() {
		var batch UsageBatch
		var upper, created string
		if err := rows.Scan(&batch.NodeID, &batch.BootID, &batch.BatchID, &batch.SnapshotID, &batch.Payload, &batch.Digest, &upper, &batch.State, &created); err != nil {
			return nil, err
		}
		if batch.UnknownUpper, err = strconv.ParseUint(upper, 10, 64); err != nil {
			return nil, err
		}
		if batch.CreatedAt, err = time.Parse(time.RFC3339Nano, created); err != nil {
			return nil, err
		}
		batches = append(batches, batch)
	}
	return batches, rows.Err()
}

func (s *Store) MarkUsageLocallyAcknowledged(ctx context.Context, nodeID string, batchIDs []string) error {
	return s.transitionUsageBatches(ctx, nodeID, batchIDs, "locally_acked")
}

func (s *Store) AcknowledgeUsageBatches(ctx context.Context, nodeID string, batchIDs []string) error {
	return s.transitionUsageBatches(ctx, nodeID, batchIDs, "hub_acked")
}

func (s *Store) transitionUsageBatches(ctx context.Context, nodeID string, batchIDs []string, target string) error {
	if nodeID == "" || len(batchIDs) == 0 {
		return fmt.Errorf("store: usage acknowledgement identity is required")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	seen := make(map[string]struct{}, len(batchIDs))
	for _, batchID := range batchIDs {
		if batchID == "" {
			return fmt.Errorf("store: empty usage batch id")
		}
		if _, duplicate := seen[batchID]; duplicate {
			continue
		}
		seen[batchID] = struct{}{}
		var state, upper string
		if err := tx.QueryRowContext(ctx, `SELECT state,unknown_upper_bound FROM usage_batches WHERE node_id=? AND batch_id=?`, nodeID, batchID).Scan(&state, &upper); err != nil {
			return err
		}
		unknown, err := strconv.ParseUint(upper, 10, 64)
		if err != nil {
			return err
		}
		if target == "hub_acked" && unknown != 0 {
			return fmt.Errorf("store: ordinary usage acknowledgement cannot acknowledge unknown liability")
		}
		if state == "hub_acked" && target == "hub_acked" || state == "locally_acked" && target == "locally_acked" {
			continue
		}
		if state != "pending" && !(target == "hub_acked" && state == "locally_acked") {
			return fmt.Errorf("store: invalid usage acknowledgement transition %s to %s", state, target)
		}
		if _, err := tx.ExecContext(ctx, `UPDATE usage_batches SET state=?,updated_at=? WHERE node_id=? AND batch_id=?`, target, time.Now().UTC().Format(time.RFC3339Nano), nodeID, batchID); err != nil {
			return err
		}
	}
	return tx.Commit()
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

var ErrEventConflict = errors.New("store: durable event identity conflicts with prior content")

// AppendEvent accepts exact replay after reconnect or restart. An identity
// cannot overwrite an earlier event's owner, kind or payload.
func (s *Store) AppendEvent(ctx context.Context, eventID, kind string, payload []byte) error {
	if eventID == "" || kind == "" {
		return fmt.Errorf("store: event id and kind are required")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("store: begin: %w", err)
	}
	defer tx.Rollback()
	var oldOwner, oldKind string
	var oldPayload []byte
	err = tx.QueryRowContext(ctx, `SELECT owner,kind,payload FROM durable_events WHERE event_id=?`, eventID).Scan(&oldOwner, &oldKind, &oldPayload)
	if err == nil {
		if oldOwner != s.owner || oldKind != kind || !bytes.Equal(oldPayload, payload) {
			return ErrEventConflict
		}
		return tx.Commit()
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return err
	}
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
	if current >= uint64(1<<63-1) {
		return 0, fmt.Errorf("store: control sequence exhausted")
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
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var priorVersion, priorState, generation string
	err = tx.QueryRowContext(ctx, `SELECT version_u64,state,fencing_generation FROM config_results WHERE transaction_id=?`, transactionID).Scan(&priorVersion, &priorState, &generation)
	if err == nil {
		if priorVersion != strconv.FormatUint(version, 10) {
			return ErrConfigRequestConflict
		}
		if generation != "0" && !validConfigTransition(priorState, state) {
			return ErrConfigRequestConflict
		}
		if _, err = tx.ExecContext(ctx, `UPDATE config_results SET state=?,reason=?,updated_at=? WHERE transaction_id=?`, state, reason, time.Now().UTC().Format(time.RFC3339Nano), transactionID); err != nil {
			return err
		}
	} else if errors.Is(err, sql.ErrNoRows) {
		// Legacy/import records carry no execution proof and cannot be released by
		// recovery. New execution always starts with SaveConfigRequest.
		if _, err = tx.ExecContext(ctx, `INSERT INTO config_results(transaction_id,version,version_u64,state,reason,updated_at) VALUES(?,0,?,?,?,?)`, transactionID, strconv.FormatUint(version, 10), state, reason, time.Now().UTC().Format(time.RFC3339Nano)); err != nil {
			return err
		}
	} else {
		return err
	}
	return tx.Commit()
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
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM config_results WHERE state IN ('received','prepared','committing','commit-requested','activation-unknown')`).Scan(&unresolved); err != nil {
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
	if _, err := tx.ExecContext(ctx, `INSERT INTO activation_holds(transaction_id,generation,state) VALUES(?,?,'held')`, request.TransactionID, strconv.FormatUint(request.FencingGeneration, 10)); err != nil {
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
	var storedVersion, state string
	if err = tx.QueryRowContext(ctx, `SELECT version_u64,state FROM config_results WHERE transaction_id=?`, transactionID).Scan(&storedVersion, &state); err != nil {
		return err
	}
	if storedVersion != strconv.FormatUint(version, 10) || !validConfigTransition(state, "active") {
		return ErrConfigRequestConflict
	}
	var current string
	err = tx.QueryRowContext(ctx, `SELECT value FROM agent_state WHERE key='applied_version'`).Scan(&current)
	if err == nil {
		n, e := strconv.ParseUint(current, 10, 64)
		if e != nil || n > version {
			return ErrConfigVersionStale
		}
	} else if !errors.Is(err, sql.ErrNoRows) {
		return err
	}
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
