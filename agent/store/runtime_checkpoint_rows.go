package store

import (
	"context"
	"crypto/sha256"
)

// Only fresh child inserts are staged. Parent identity/residual validation and
// ordered parent updates still run for each entry in the same writer transaction.
// No child becomes visible, or callback succeeds, before the FULL commit.
type runtimeCheckpointRow struct {
	block                          UsageReservation
	id, amount, source, prior, now string
	payload                        []byte
}

type runtimeCheckpointInsert struct {
	queries [4]string
	width   int
}

func checkpointInsert(table, columns string, width int) runtimeCheckpointInsert {
	result := runtimeCheckpointInsert{width: width}
	for i, slots := range []int{8, 16, 32, 64} {
		result.queries[i] = usageGroupInsertSQL(table, columns, width, slots)
	}
	return result
}

var runtimeCheckpointInserts = [...]runtimeCheckpointInsert{
	checkpointInsert("usage_reservations", "node_id,boot_id,batch_id,snapshot_id,flow_key,rule_id,direction,reserved_bytes,state,used_bytes,updated_at", 11),
	checkpointInsert("authorization_usage_holds", "node_id,batch_id,authorization_id,source_id,reserved_bytes,state", 6),
	checkpointInsert("runtime_block_checkpoints", "node_id,batch_id,block_id,prior_reserved", 4),
	checkpointInsert("usage_batches", "node_id,batch_id,boot_id,snapshot_id,payload,payload_sha256,unknown_upper_bound,state,created_at,updated_at", 10),
}

func (r runtimeCheckpointRow) values(table int) []any {
	b := r.block
	switch table {
	case 0:
		return []any{b.NodeID, b.BootID, r.id, b.SnapshotID, b.FlowKey, b.RuleID, b.Direction, r.amount, "committed", r.amount, r.now}
	case 1:
		return []any{b.NodeID, r.id, b.AuthorizationID, r.source, r.amount, "committed"}
	case 2:
		return []any{b.NodeID, r.id, b.BatchID, r.prior}
	default:
		digest := sha256.Sum256(r.payload)
		return []any{b.NodeID, r.id, b.BootID, b.SnapshotID, r.payload, digest[:], "0", "pending", r.now, r.now}
	}
}

func insertRuntimeCheckpointChild(ctx context.Context, tx storeTransaction, r runtimeCheckpointRow) error {
	if _, err := tx.ExecContext(ctx, `INSERT INTO usage_reservations(node_id,boot_id,batch_id,snapshot_id,flow_key,rule_id,direction,reserved_bytes,state,used_bytes,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?,?)`, r.values(0)...); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx, `INSERT INTO authorization_usage_holds(node_id,batch_id,authorization_id,source_id,reserved_bytes,state) VALUES(?,?,?,?,?,?)`, r.values(1)...)
	return err
}

func insertRuntimeCheckpointEvidence(ctx context.Context, tx storeTransaction, r runtimeCheckpointRow) error {
	if _, err := tx.ExecContext(ctx, `INSERT INTO runtime_block_checkpoints(node_id,batch_id,block_id,prior_reserved) VALUES(?,?,?,?)`, r.values(2)...); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx, `INSERT INTO usage_batches(node_id,batch_id,boot_id,snapshot_id,payload,payload_sha256,unknown_upper_bound,state,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?)`, r.values(3)...)
	return err
}

func insertRuntimeCheckpointRows(ctx context.Context, tx storeTransaction, rows []runtimeCheckpointRow) error {
	if len(rows) == 0 {
		return nil
	}
	if len(rows) > 64 {
		return ErrUsageBatchConflict
	}
	for table, insert := range runtimeCheckpointInserts {
		shape, slots := 0, 8
		for len(rows) > slots {
			shape++
			slots *= 2
		}
		query := insert.queries[shape]
		args := make([]any, slots*insert.width)
		for i, row := range rows {
			copy(args[i*insert.width:], row.values(table))
		}
		if err := execUsageGroupRows(ctx, tx, query, len(rows), args...); err != nil {
			return err
		}
	}
	return nil
}
