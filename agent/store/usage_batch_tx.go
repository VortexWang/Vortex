package store

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"time"
)

// Shared by the legacy local path and authenticated Hub envelope persistence.
// The caller owns the FULL transaction and all acknowledgement boundaries.
func (s *Store) putUsageBatchTx(ctx context.Context, tx storeTransaction, batch UsageBatch, digest []byte, initialState string) error {
	var snapshot string
	var compactDigest []byte
	err := tx.QueryRowContext(ctx, `SELECT snapshot_id,payload_sha256 FROM settled_usage_tombstones WHERE node_id=? AND batch_id=?`, batch.NodeID, batch.BatchID).Scan(&snapshot, &compactDigest)
	if err == nil {
		if s.owner != "hub" || initialState != "ledgered" || batch.UnknownUpper != 0 || snapshot != batch.SnapshotID || !bytes.Equal(compactDigest, digest) {
			return ErrUsageBatchConflict
		}
		return nil
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
		return nil
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
	return nil
}
