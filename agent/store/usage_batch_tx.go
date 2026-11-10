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

// Read all possible durable identities in one snapshot. Ordering preserves
// compacted replay precedence; unknown identities never become known usage.
const usageBatchIdentityQuery = `SELECT 'compact',snapshot_id,NULL,payload_sha256,'0','' FROM settled_usage_tombstones WHERE node_id=? AND batch_id=?
UNION ALL SELECT 'live',snapshot_id,payload,payload_sha256,unknown_upper_bound,state FROM usage_batches WHERE node_id=? AND batch_id=?
UNION ALL SELECT 'unknown','',NULL,NULL,'0','' FROM unknown_usage_batches WHERE node_id=? AND batch_id=?`

// Shared by the legacy local path and authenticated Hub envelope persistence.
// The caller owns the FULL transaction and all acknowledgement boundaries.
func (s *Store) putUsageBatchTx(ctx context.Context, tx storeTransaction, batch UsageBatch, digest []byte, initialState string) error {
	var identity, snapshot, upper, state string
	var payload, previousDigest []byte
	err := tx.QueryRowContext(ctx, usageBatchIdentityQuery, batch.NodeID, batch.BatchID, batch.NodeID, batch.BatchID, batch.NodeID, batch.BatchID).
		Scan(&identity, &snapshot, &payload, &previousDigest, &upper, &state)
	if err == nil {
		if identity == "compact" {
			if s.owner != "hub" || initialState != "ledgered" || batch.UnknownUpper != 0 || snapshot != batch.SnapshotID || !bytes.Equal(previousDigest, digest) {
				return ErrUsageBatchConflict
			}
			return nil
		}
		if identity != "live" {
			return ErrUsageBatchConflict
		}
		unknown, err := strconv.ParseUint(upper, 10, 64)
		if err != nil {
			return fmt.Errorf("store: invalid usage liability: %w", err)
		}
		if snapshot != batch.SnapshotID || unknown != batch.UnknownUpper || !bytes.Equal(payload, batch.Payload) || !bytes.Equal(previousDigest, digest) {
			return ErrUsageBatchConflict
		}
		if initialState == "ledgered" && state != "ledgered" {
			if _, err := tx.ExecContext(ctx, `UPDATE usage_batches SET state='ledgered', updated_at=? WHERE node_id=? AND batch_id=?`, time.Now().UTC().Format(time.RFC3339Nano), batch.NodeID, batch.BatchID); err != nil {
				return err
			}
		}
		return nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err = tx.ExecContext(ctx, `INSERT INTO usage_batches(node_id,batch_id,boot_id,snapshot_id,payload,payload_sha256,unknown_upper_bound,state,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?)`, batch.NodeID, batch.BatchID, batch.BootID, batch.SnapshotID, batch.Payload, digest, strconv.FormatUint(batch.UnknownUpper, 10), initialState, now, now)
	if err != nil {
		return err
	}
	return nil
}
