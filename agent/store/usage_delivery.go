package store

import (
	"bytes"
	"context"
	"strconv"
	"time"
)

const UsageDeliveryPageSize = 32

// Delivery positions are connection-local scan hints. These append-only tables
// retain their rows for replay/audit, so new inserts follow earlier positions
// even when their random batch IDs sort before an earlier delivered batch.
// A fresh connection or periodic replay starts from zero. No position is ACK.
type UsageDeliveryRecord struct {
	Position int64
	Batch    UsageBatch
}

type UnknownUsageDeliveryRecord struct {
	Position int64
	Batch    UnknownUsageRecord
}

func (s *Store) KnownUsageDeliveryPage(ctx context.Context, node string, after int64) ([]UsageDeliveryRecord, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT rowid,node_id,boot_id,batch_id,snapshot_id,payload,payload_sha256,unknown_upper_bound,state,created_at FROM usage_batches WHERE node_id=? AND rowid>? AND state IN ('pending','locally_acked') ORDER BY rowid LIMIT 32`, node, after)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []UsageDeliveryRecord
	for rows.Next() {
		var item UsageDeliveryRecord
		var upper, created string
		b := &item.Batch
		if err := rows.Scan(&item.Position, &b.NodeID, &b.BootID, &b.BatchID, &b.SnapshotID, &b.Payload, &b.Digest, &upper, &b.State, &created); err != nil {
			return nil, err
		}
		if b.UnknownUpper, err = strconv.ParseUint(upper, 10, 64); err != nil {
			return nil, err
		}
		if b.CreatedAt, err = time.Parse(time.RFC3339Nano, created); err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

func (s *Store) UnknownUsageDeliveryPage(ctx context.Context, node string, after int64) ([]UnknownUsageDeliveryRecord, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT rowid,batch_id,payload,payload_sha256,legacy_before_image,transport_state,liability_state FROM unknown_usage_batches WHERE node_id=? AND rowid>? AND transport_state='pending' ORDER BY rowid LIMIT 32`, node, after)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []UnknownUsageDeliveryRecord
	for rows.Next() {
		var position int64
		var id, state, liability string
		var payload, digest, before []byte
		if err := rows.Scan(&position, &id, &payload, &digest, &before, &state, &liability); err != nil {
			return nil, err
		}
		batch, err := unknownRecord(node, payload)
		if err != nil {
			return nil, err
		}
		if batch.BatchID != id || !bytes.Equal(batch.Digest, digest) || liability != "unresolved" {
			return nil, ErrUsageBatchConflict
		}
		batch.LegacyBeforeImage, batch.TransportState, batch.LiabilityState = before, state, liability
		result = append(result, UnknownUsageDeliveryRecord{Position: position, Batch: batch})
	}
	return result, rows.Err()
}
