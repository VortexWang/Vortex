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
	var original []byte
	e = tx.QueryRowContext(ctx, `SELECT payload_sha256 FROM usage_batches WHERE node_id=? AND batch_id=? UNION ALL SELECT payload_sha256 FROM unknown_usage_batches WHERE node_id=? AND batch_id=?`, node, batch, node, batch).Scan(&original)
	if e != nil {
		return e
	}
	if !bytes.Equal(original, digest[:]) {
		return ErrUsageBatchConflict
	}
	var oldBoot string
	var oldDigest []byte
	e = tx.QueryRowContext(ctx, `SELECT report_boot_id,payload_sha256 FROM usage_origin_envelopes WHERE node_id=? AND batch_id=?`, node, batch).Scan(&oldBoot, &oldDigest)
	if e == nil {
		if oldBoot == "" || !bytes.Equal(oldDigest, digest[:]) {
			return ErrUsageBatchConflict
		}
		// An unknown report can survive another Agent reboot after its receipt
		// was lost. Reuse the first captured identity for identical payloads;
		// the retry's current boot and sequence never overwrite that provenance.
		return tx.Commit()
	}
	if !errors.Is(e, sql.ErrNoRows) {
		return e
	}
	if _, e = tx.ExecContext(ctx, `INSERT INTO usage_origin_envelopes(node_id,batch_id,report_boot_id,sequence,payload_sha256) VALUES(?,?,?,?,?)`, node, batch, boot, seq, digest[:]); e != nil {
		return e
	}
	return tx.Commit()
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
