package store

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// HubUsage retains the original wire payload until the backend has durably
// acknowledged it. Confirmation never settles or releases an unknown liability.
type HubUsage struct {
	NodeID, BootID, BatchID, Kind string
	ReportBootID                  string
	Sequence                      uint64
	Payload, Digest               []byte
}

func (s *Store) BackendUsageConfirmed(ctx context.Context, batch HubUsage) (bool, error) {
	if s.owner != "hub" {
		return false, ErrUsageBatchConflict
	}
	var kind string
	var digest []byte
	err := s.db.QueryRowContext(ctx, `SELECT kind,payload_sha256 FROM usage_backend_receipts WHERE node_id=? AND batch_id=?
UNION ALL SELECT 'known',payload_sha256 FROM settled_usage_tombstones WHERE node_id=? AND batch_id=?`, batch.NodeID, batch.BatchID, batch.NodeID, batch.BatchID).Scan(&kind, &digest)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if kind != batch.Kind || !bytes.Equal(digest, batch.Digest) {
		return false, ErrUsageBatchConflict
	}
	return true, nil
}

func (s *Store) ConfirmBackendUsage(ctx context.Context, batch HubUsage, acknowledgedDigest []byte) error {
	digest := sha256.Sum256(batch.Payload)
	if s.owner != "hub" || !bytes.Equal(digest[:], batch.Digest) || !bytes.Equal(digest[:], acknowledgedDigest) {
		return ErrUsageBatchConflict
	}
	confirmed, err := s.BackendUsageConfirmed(ctx, batch)
	if err != nil || confirmed {
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := confirmBackendUsageTx(ctx, tx, batch, acknowledgedDigest); err != nil {
		return err
	}
	return tx.Commit()
}

func confirmBackendUsageTx(ctx context.Context, tx storeTransaction, batch HubUsage, acknowledgedDigest []byte) error {
	digest := sha256.Sum256(batch.Payload)
	if !bytes.Equal(digest[:], batch.Digest) || !bytes.Equal(digest[:], acknowledgedDigest) {
		return ErrUsageBatchConflict
	}
	table := "usage_batches"
	if batch.Kind == "unknown" {
		table = "unknown_usage_batches"
	} else if batch.Kind != "known" {
		return ErrUsageBatchConflict
	}
	var compactDigest []byte
	compactErr := tx.QueryRowContext(ctx, `SELECT payload_sha256 FROM settled_usage_tombstones WHERE node_id=? AND batch_id=?`, batch.NodeID, batch.BatchID).Scan(&compactDigest)
	if compactErr == nil {
		if batch.Kind != "known" || !bytes.Equal(compactDigest, digest[:]) {
			return ErrUsageBatchConflict
		}
		return nil
	}
	if !errors.Is(compactErr, sql.ErrNoRows) {
		return compactErr
	}
	var payload, storedDigest []byte
	if err := tx.QueryRowContext(ctx, `SELECT payload,payload_sha256 FROM `+table+` WHERE node_id=? AND batch_id=?`, batch.NodeID, batch.BatchID).Scan(&payload, &storedDigest); err != nil {
		return err
	}
	if !bytes.Equal(payload, batch.Payload) || !bytes.Equal(storedDigest, digest[:]) {
		return ErrUsageBatchConflict
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO usage_backend_receipts(node_id,batch_id,kind,payload_sha256,confirmed_at) VALUES(?,?,?,?,?) ON CONFLICT DO NOTHING`, batch.NodeID, batch.BatchID, batch.Kind, digest[:], time.Now().UTC().Format(time.RFC3339Nano)); err != nil {
		return err
	}
	var kind string
	if err := tx.QueryRowContext(ctx, `SELECT kind,payload_sha256 FROM usage_backend_receipts WHERE node_id=? AND batch_id=?`, batch.NodeID, batch.BatchID).Scan(&kind, &storedDigest); err != nil {
		return err
	}
	if kind != batch.Kind || !bytes.Equal(storedDigest, digest[:]) {
		return ErrUsageBatchConflict
	}
	return nil
}

func (s *Store) PendingBackendUsage(ctx context.Context) ([]HubUsage, error) {
	if s.owner != "hub" {
		return nil, ErrUsageBatchConflict
	}
	rows, err := s.db.QueryContext(ctx, `SELECT u.node_id,u.boot_id,u.batch_id,'known',u.payload,u.payload_sha256,COALESCE(o.report_boot_id,''),COALESCE(o.sequence,'0') FROM usage_batches u LEFT JOIN usage_origin_envelopes o ON o.node_id=u.node_id AND o.batch_id=u.batch_id WHERE u.state='ledgered' AND u.unknown_upper_bound='0' AND NOT EXISTS (SELECT 1 FROM usage_backend_receipts r WHERE r.node_id=u.node_id AND r.batch_id=u.batch_id)
UNION ALL SELECT u.node_id,u.origin_boot_id,u.batch_id,'unknown',u.payload,u.payload_sha256,COALESCE(o.report_boot_id,''),COALESCE(o.sequence,'0') FROM unknown_usage_batches u LEFT JOIN usage_origin_envelopes o ON o.node_id=u.node_id AND o.batch_id=u.batch_id WHERE NOT EXISTS (SELECT 1 FROM usage_backend_receipts r WHERE r.node_id=u.node_id AND r.batch_id=u.batch_id) ORDER BY 1,3`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []HubUsage
	for rows.Next() {
		var b HubUsage
		if err := rows.Scan(&b.NodeID, &b.BootID, &b.BatchID, &b.Kind, &b.Payload, &b.Digest, &b.ReportBootID, &b.Sequence); err != nil {
			return nil, err
		}
		digest := sha256.Sum256(b.Payload)
		if !bytes.Equal(digest[:], b.Digest) {
			return nil, fmt.Errorf("hub usage: corrupt durable payload")
		}
		// The left join reads provenance in the same snapshot without an
		// individual SQL round trip for every retained report. Missing historical
		// provenance remains explicit empty/zero pending reconciliation.
		result = append(result, b)
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	if err = rows.Close(); err != nil {
		return nil, err
	}
	return result, nil
}
