package store

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"strconv"
	"time"
)

const SettledUsageRetention = 7 * 24 * time.Hour

// This additive FULL transaction has its own migration marker. Existing authority
// and renewal schema versions are unchanged; no historical wall time proves age.
func (s *Store) migrateUsageRetention() error {
	tx, err := s.db.BeginTx(context.Background(), nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	_, err = tx.Exec(`CREATE TABLE IF NOT EXISTS usage_retention_age (
 node_id TEXT NOT NULL,batch_id TEXT NOT NULL,observed_upper TEXT NOT NULL,
 PRIMARY KEY(node_id,batch_id));
 CREATE TABLE IF NOT EXISTS usage_disputes (
 node_id TEXT NOT NULL,batch_id TEXT NOT NULL,payload BLOB NOT NULL,payload_sha256 BLOB NOT NULL,
 PRIMARY KEY(node_id,batch_id,payload_sha256));
 CREATE TABLE IF NOT EXISTS settled_usage_tombstones (
 node_id TEXT NOT NULL,batch_id TEXT NOT NULL,snapshot_id TEXT NOT NULL,
 payload_sha256 BLOB NOT NULL,ledger_sequence INTEGER NOT NULL,
 PRIMARY KEY(node_id,batch_id));
 INSERT INTO schema_meta(key,value) VALUES('usage_retention_version','1') ON CONFLICT(key) DO UPDATE SET value=excluded.value;`)
	if err != nil {
		return err
	}
	return tx.Commit()
}

// RecordUsageDispute retains conflicting bytes indefinitely. There is deliberately
// no automatic resolution API: retries, expiry and ACKs cannot clear a dispute.
func (s *Store) RecordUsageDispute(ctx context.Context, node, batch string, payload []byte) error {
	if s.owner != "hub" || node == "" || batch == "" || len(payload) == 0 {
		return ErrUsageBatchConflict
	}
	digest := sha256.Sum256(payload)
	_, err := s.db.ExecContext(ctx, `INSERT INTO usage_disputes(node_id,batch_id,payload,payload_sha256) VALUES(?,?,?,?) ON CONFLICT DO NOTHING`, node, batch, payload, digest[:])
	return err
}

// PruneSettledUsage removes duplicate Hub usage payloads and transport receipts,
// retaining a receipt/identity hash tombstone and the original confirmed journal.
// It never modifies source balances, issue/handoff evidence or unknown debt.
// Age begins only when BOTH confirmations are observed at a trusted upper bound;
// deletion requires a later trusted lower bound seven days beyond that observation.
// Agent Hub ACKs are not backend settlement, so Agent stores cannot call this API.
func (s *Store) PruneSettledUsage(ctx context.Context, now IssuerTime, limit int) (int, error) {
	if s.owner != "hub" || limit < 1 || limit > 1000 {
		return 0, ErrAuthorizationDenied
	}
	if err := checkIssuerTime(now); err != nil {
		return 0, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	var checkpointText string
	err = tx.QueryRowContext(ctx, `SELECT value FROM agent_state WHERE key='ledger_checkpoint'`).Scan(&checkpointText)
	if err == sql.ErrNoRows {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	checkpoint, err := strconv.ParseInt(checkpointText, 10, 64)
	if err != nil {
		return 0, err
	}
	// Reading candidates, establishing age, recording tombstones and removing the
	// duplicate rows share one writer lock, including concurrent dispute insertion.
	rows, err := tx.QueryContext(ctx, `SELECT u.node_id,u.batch_id,u.snapshot_id,u.payload,u.payload_sha256,l.sequence,a.observed_upper
 FROM usage_batches u JOIN usage_backend_receipts r ON r.node_id=u.node_id AND r.batch_id=u.batch_id AND r.kind='known' AND r.payload_sha256=u.payload_sha256
 JOIN hub_ledger l ON l.kind='known' AND l.record_id=u.node_id||'/'||u.batch_id AND l.confirmed=1 AND l.payload=u.payload AND l.payload_sha256=u.payload_sha256
 LEFT JOIN usage_retention_age a ON a.node_id=u.node_id AND a.batch_id=u.batch_id
 WHERE u.state='ledgered' AND u.unknown_upper_bound='0' AND l.sequence<=?
 AND NOT EXISTS(SELECT 1 FROM hub_ledger hole WHERE hole.confirmed=0 AND hole.sequence<=l.sequence)
 AND NOT EXISTS(SELECT 1 FROM usage_disputes d WHERE d.node_id=u.node_id AND d.batch_id=u.batch_id)
 AND NOT EXISTS(SELECT 1 FROM unknown_usage_conflicts d WHERE d.node_id=u.node_id AND d.batch_id=u.batch_id)
 AND NOT EXISTS(SELECT 1 FROM unknown_usage_batches d WHERE d.node_id=u.node_id AND d.batch_id=u.batch_id)
 ORDER BY CASE WHEN a.observed_upper IS NULL THEN 1
 WHEN julianday(a.observed_upper)<=julianday(?) THEN 0 ELSE 2 END,l.sequence LIMIT ?`, checkpoint, now.Earliest.Add(-SettledUsageRetention).UTC().Format(time.RFC3339Nano), limit)
	if err != nil {
		return 0, err
	}
	type candidate struct {
		node, batch, snapshot string
		payload, digest       []byte
		sequence              int64
		age                   sql.NullString
	}
	var candidates []candidate
	for rows.Next() {
		var c candidate
		if err = rows.Scan(&c.node, &c.batch, &c.snapshot, &c.payload, &c.digest, &c.sequence, &c.age); err != nil {
			rows.Close()
			return 0, err
		}
		candidates = append(candidates, c)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return 0, err
	}
	pruned := 0
	for _, c := range candidates {
		digest := sha256.Sum256(c.payload)
		if !bytes.Equal(digest[:], c.digest) {
			return 0, ErrUsageBatchConflict
		}
		if !c.age.Valid {
			if _, err = tx.ExecContext(ctx, `INSERT INTO usage_retention_age(node_id,batch_id,observed_upper) VALUES(?,?,?)`, c.node, c.batch, now.Latest.UTC().Format(time.RFC3339Nano)); err != nil {
				return 0, err
			}
			continue
		}
		upper, parseErr := time.Parse(time.RFC3339Nano, c.age.String)
		if parseErr != nil {
			return 0, parseErr
		}
		if now.Earliest.Before(upper.Add(SettledUsageRetention)) {
			continue
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO settled_usage_tombstones(node_id,batch_id,snapshot_id,payload_sha256,ledger_sequence) VALUES(?,?,?,?,?)`, c.node, c.batch, c.snapshot, c.digest, c.sequence); err != nil {
			return 0, err
		}
		for _, table := range []string{"usage_batches", "usage_backend_receipts", "usage_retention_age"} {
			if _, err = tx.ExecContext(ctx, `DELETE FROM `+table+` WHERE node_id=? AND batch_id=?`, c.node, c.batch); err != nil {
				return 0, err
			}
		}
		pruned++
	}
	if err = tx.Commit(); err != nil {
		return 0, err
	}
	return pruned, nil
}
