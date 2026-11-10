package store

import (
	"bytes"
	"context"
	"database/sql"
	"math"
	"strconv"
)

// Capture the first authenticated envelope together with the original durable
// payload, in the caller's FULL transaction. Preserve known-before-unknown lookup
// order and never infer provenance for a compacted/missing payload.
const usageOriginSetQuery = `WITH wanted AS (SELECT json_extract(value,'$[0]') AS node_id,json_extract(value,'$[1]') AS batch_id FROM json_each(?))
SELECT 0,u.node_id,u.batch_id,u.payload_sha256,o.report_boot_id,o.sequence,o.payload_sha256 FROM usage_batches u JOIN wanted w ON w.node_id=u.node_id AND w.batch_id=u.batch_id LEFT JOIN usage_origin_envelopes o ON o.node_id=u.node_id AND o.batch_id=u.batch_id
UNION ALL SELECT 1,u.node_id,u.batch_id,u.payload_sha256,o.report_boot_id,o.sequence,o.payload_sha256 FROM unknown_usage_batches u JOIN wanted w ON w.node_id=u.node_id AND w.batch_id=u.batch_id LEFT JOIN usage_origin_envelopes o ON o.node_id=u.node_id AND o.batch_id=u.batch_id`

var usageOriginInsertSmall = usageGroupInsertSQL("usage_origin_envelopes", "node_id,batch_id,report_boot_id,sequence,payload_sha256", 5, 8)
var usageOriginInsertLarge = usageGroupInsertSQL("usage_origin_envelopes", "node_id,batch_id,report_boot_id,sequence,payload_sha256", 5, 32)

func recordKnownUsageOriginsTx(ctx context.Context, tx storeTransaction, batches []HubUsage) error {
	if len(batches) < 2 || len(batches) > 32 {
		return ErrUsageBatchConflict
	}
	for _, b := range batches {
		if b.ReportBootID == "" || b.Sequence == 0 || b.Sequence > math.MaxInt64 {
			return ErrUsageOriginMissing
		}
	}
	keys, err := usageGroupKeys(batches)
	if err != nil {
		return err
	}
	rows, err := tx.QueryContext(ctx, usageOriginSetQuery, string(keys))
	if err != nil {
		return err
	}
	type origin struct {
		priority                   int
		storedDigest, originDigest []byte
		boot, sequence             sql.NullString
	}
	evidence := map[[2]string]origin{}
	for rows.Next() {
		var node, batch string
		var r origin
		if err := rows.Scan(&r.priority, &node, &batch, &r.storedDigest, &r.boot, &r.sequence, &r.originDigest); err != nil {
			rows.Close()
			return err
		}
		key := [2]string{node, batch}
		if old, ok := evidence[key]; !ok || r.priority < old.priority {
			evidence[key] = r
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	var fresh []HubUsage
	for i, b := range batches {
		key := [2]string{b.NodeID, b.BatchID}
		r, ok := evidence[key]
		if !ok {
			return sql.ErrNoRows
		}
		if !bytes.Equal(r.storedDigest, b.Digest) {
			return ErrUsageBatchConflict
		}
		if r.boot.Valid {
			seq, err := strconv.ParseUint(r.sequence.String, 10, 64)
			if err != nil {
				return err
			}
			if r.boot.String == "" || seq == 0 || seq > math.MaxInt64 || !bytes.Equal(r.originDigest, b.Digest) {
				return ErrUsageBatchConflict
			}
			batches[i].ReportBootID, batches[i].Sequence = r.boot.String, seq
			continue
		}
		fresh = append(fresh, b)
		// A repeated identity in this group retains its first envelope too.
		r.boot = sql.NullString{String: b.ReportBootID, Valid: true}
		r.sequence = sql.NullString{String: strconv.FormatUint(b.Sequence, 10), Valid: true}
		r.originDigest = b.Digest
		evidence[key] = r
	}
	if len(fresh) == 0 {
		return nil
	}
	query, slots := usageOriginInsertSmall, 8
	if len(fresh) > slots {
		query, slots = usageOriginInsertLarge, 32
	}
	args := make([]any, 5*slots)
	for i, b := range fresh {
		copy(args[5*i:], []any{b.NodeID, b.BatchID, b.ReportBootID, strconv.FormatUint(b.Sequence, 10), b.Digest})
	}
	return execUsageGroupRows(ctx, tx, query, len(fresh), args...)
}
