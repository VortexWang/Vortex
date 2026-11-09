package store

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"math"
)

var ErrUsageOriginMissing = errors.New("usage original envelope missing; retain pending reconciliation")

func (s *Store) migrateUsageOrigin() error {
	_, e := s.db.Exec(`CREATE TABLE IF NOT EXISTS usage_origin_envelopes(node_id TEXT NOT NULL,batch_id TEXT NOT NULL,report_boot_id TEXT NOT NULL,sequence TEXT NOT NULL,payload_sha256 BLOB NOT NULL,PRIMARY KEY(node_id,batch_id),UNIQUE(node_id,report_boot_id,sequence))`)
	return e
}

// RecordUsageOrigin is called with a real authenticated NodeMessage, before
// any backend confirmation. Old bytes without an envelope remain pending.
func (s *Store) RecordUsageOrigin(ctx context.Context, node, batch, boot string, seq uint64, payload []byte) error {
	if s.owner != "hub" || node == "" || batch == "" || boot == "" || seq == 0 || seq > math.MaxInt64 {
		return ErrUsageOriginMissing
	}
	digest := sha256.Sum256(payload)
	tx, e := s.db.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	if _, _, e := recordUsageOriginTx(ctx, tx, node, batch, boot, seq, digest[:]); e != nil {
		return e
	}
	return tx.Commit()
}

// recordUsageOriginTx returns the first authenticated provenance, including
// exact retries from a later report boot. Its caller commits the same transaction
// as the usage payload before exposing either fact to an acknowledgement path.
func recordUsageOriginTx(ctx context.Context, tx storeTransaction, node, batch, boot string, seq uint64, digest []byte) (string, uint64, error) {
	var original []byte
	e := tx.QueryRowContext(ctx, `SELECT payload_sha256 FROM usage_batches WHERE node_id=? AND batch_id=? UNION ALL SELECT payload_sha256 FROM unknown_usage_batches WHERE node_id=? AND batch_id=?`, node, batch, node, batch).Scan(&original)
	if e != nil {
		return "", 0, e
	}
	if !bytes.Equal(original, digest) {
		return "", 0, ErrUsageBatchConflict
	}
	var oldBoot string
	var oldSequence uint64
	var oldDigest []byte
	e = tx.QueryRowContext(ctx, `SELECT report_boot_id,sequence,payload_sha256 FROM usage_origin_envelopes WHERE node_id=? AND batch_id=?`, node, batch).Scan(&oldBoot, &oldSequence, &oldDigest)
	if e == nil {
		if oldBoot == "" || oldSequence == 0 || oldSequence > math.MaxInt64 || !bytes.Equal(oldDigest, digest) {
			return "", 0, ErrUsageBatchConflict
		}
		// An unknown report can survive another Agent reboot after its receipt
		// was lost. Reuse the first captured identity for identical payloads;
		// the retry's current boot and sequence never overwrite that provenance.
		return oldBoot, oldSequence, nil
	}
	if !errors.Is(e, sql.ErrNoRows) {
		return "", 0, e
	}
	if _, e = tx.ExecContext(ctx, `INSERT INTO usage_origin_envelopes(node_id,batch_id,report_boot_id,sequence,payload_sha256) VALUES(?,?,?,?,?)`, node, batch, boot, seq, digest); e != nil {
		return "", 0, e
	}
	return boot, seq, nil
}
func (s *Store) UsageOrigin(ctx context.Context, node, batch string) (string, uint64, error) {
	var boot string
	var seq uint64
	e := s.db.QueryRowContext(ctx, `SELECT report_boot_id,sequence FROM usage_origin_envelopes WHERE node_id=? AND batch_id=?`, node, batch).Scan(&boot, &seq)
	if errors.Is(e, sql.ErrNoRows) {
		e = ErrUsageOriginMissing
	}
	return boot, seq, e
}
