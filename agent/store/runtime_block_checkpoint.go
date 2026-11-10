package store

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"strconv"
	"time"
)

// CheckpointRuntimeFundingBlock records confirmed writes, not permission to
// perform them. The caller must serialize checkpoints and supply the current
// residual reservation. On success the residual is reduced by used. A retry
// uses the same checkpoint ID, prior reservation, used amount and payload.
// The known batch and reduced crash-recovery upper bound commit together.
func (s *Store) CheckpointRuntimeFundingBlock(ctx context.Context, block UsageReservation, checkpointID string, used uint64, payload []byte) error {
	if s.owner != "agent" || checkpointID == "" || checkpointID == block.BatchID || used == 0 || used > block.Reserved || len(payload) == 0 {
		return ErrUsageBatchConflict
	}
	tx, err := s.beginRuntimeTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := checkpointRuntimeFundingBlockTx(ctx, tx, block, checkpointID, used, payload); err != nil {
		return err
	}
	return tx.Commit()
}

func checkpointRuntimeFundingBlockTx(ctx context.Context, tx storeTransaction, block UsageReservation, checkpointID string, used uint64, payload []byte) error {
	child := block
	child.BatchID, child.Reserved = checkpointID, used
	var priorBlock, priorReserved string
	err := tx.QueryRowContext(ctx, `SELECT block_id,prior_reserved FROM runtime_block_checkpoints WHERE node_id=? AND batch_id=?`, block.NodeID, checkpointID).Scan(&priorBlock, &priorReserved)
	if err == nil {
		if priorBlock != block.BatchID || priorReserved != strconv.FormatUint(block.Reserved, 10) {
			return ErrUsageBatchConflict
		}
		if err := settleUsageReservationTx(ctx, tx, child, "committed", used, payload, 0, nil); err != nil {
			return err
		}
		return nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	var boot, snapshot, flow, rule, direction, reserved, state, auth, source, held, holdState string
	err = tx.QueryRowContext(ctx, `SELECT u.boot_id,u.snapshot_id,u.flow_key,u.rule_id,u.direction,u.reserved_bytes,u.state,h.authorization_id,h.source_id,h.reserved_bytes,h.state FROM runtime_funding_blocks b JOIN usage_reservations u ON u.node_id=b.node_id AND u.batch_id=b.batch_id JOIN authorization_usage_holds h ON h.node_id=u.node_id AND h.batch_id=u.batch_id WHERE b.node_id=? AND b.batch_id=?`, block.NodeID, block.BatchID).Scan(&boot, &snapshot, &flow, &rule, &direction, &reserved, &state, &auth, &source, &held, &holdState)
	if err != nil {
		return err
	}
	if boot != block.BootID || snapshot != block.SnapshotID || flow != block.FlowKey || rule != block.RuleID || direction != block.Direction || auth != block.AuthorizationID || reserved != strconv.FormatUint(block.Reserved, 10) || held != reserved || state != "reserved" || holdState != "reserved" {
		return ErrUsageBatchConflict
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	amount := strconv.FormatUint(used, 10)
	// The parent was validated above and this child does not yet exist. Write
	// its final confirmed outcome directly, instead of reading back and updating
	// the just-inserted reservation through the general settlement path. Every
	// row below, including the reduced parent liability, commits atomically.
	if err := rejectUnknownIdentityTx(ctx, tx, block.NodeID, checkpointID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO usage_reservations(node_id,boot_id,batch_id,snapshot_id,flow_key,rule_id,direction,reserved_bytes,state,used_bytes,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?,?)`, block.NodeID, boot, checkpointID, snapshot, flow, rule, direction, amount, "committed", amount, now); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO authorization_usage_holds(node_id,batch_id,authorization_id,source_id,reserved_bytes,state) VALUES(?,?,?,?,?,?)`, block.NodeID, checkpointID, auth, source, amount, "committed"); err != nil {
		return err
	}
	residual := strconv.FormatUint(block.Reserved-used, 10)
	residualState := "reserved"
	if used == block.Reserved {
		residualState = "returned" // zero tail, no refund and no recovery liability
	}
	if _, err := tx.ExecContext(ctx, `UPDATE usage_reservations SET reserved_bytes=?,state=?,updated_at=? WHERE node_id=? AND batch_id=?`, residual, residualState, now, block.NodeID, block.BatchID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE authorization_usage_holds SET reserved_bytes=?,state=? WHERE node_id=? AND batch_id=?`, residual, residualState, block.NodeID, block.BatchID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO runtime_block_checkpoints(node_id,batch_id,block_id,prior_reserved) VALUES(?,?,?,?)`, block.NodeID, checkpointID, block.BatchID, reserved); err != nil {
		return err
	}
	digest := sha256.Sum256(payload)
	if _, err := tx.ExecContext(ctx, `INSERT INTO usage_batches(node_id,batch_id,boot_id,snapshot_id,payload,payload_sha256,unknown_upper_bound,state,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?)`, block.NodeID, checkpointID, boot, snapshot, payload, digest[:], "0", "pending", now, now); err != nil {
		return err
	}
	return recordDemandConsumptionTx(ctx, tx, block.NodeID, auth, used, time.Now())
}

// RuntimeBlockCheckpoint preserves each child's independent outbox identity.
// For multiple entries against one block, Block.Reserved must describe the
// residual immediately before that entry, in the supplied order.
type RuntimeBlockCheckpoint struct {
	Block        UsageReservation
	CheckpointID string
	Used         uint64
	Payload      []byte
}

// CheckpointRuntimeFundingBlocks commits up to 64 known outcomes together.
// The allocator must hold ownership until this returns; none of the associated
// Engine Commit callbacks may acknowledge success before the transaction does.
// Exact replay uses the original ordered residuals and payloads.
func (s *Store) CheckpointRuntimeFundingBlocks(ctx context.Context, entries []RuntimeBlockCheckpoint) error {
	if s.owner != "agent" || len(entries) == 0 || len(entries) > 64 {
		return ErrUsageBatchConflict
	}
	type key struct{ node, id string }
	seen := make(map[key]bool, len(entries))
	for _, entry := range entries {
		identity := key{entry.Block.NodeID, entry.CheckpointID}
		if entry.CheckpointID == "" || entry.CheckpointID == entry.Block.BatchID || entry.Used == 0 || entry.Used > entry.Block.Reserved || len(entry.Payload) == 0 || seen[identity] {
			return ErrUsageBatchConflict
		}
		seen[identity] = true
	}
	return s.enqueueCheckpoints(ctx, entries)
}

func (s *Store) commitRuntimeCheckpoints(ctx context.Context, entries []RuntimeBlockCheckpoint, checkMembers func() error, preowned ...func()) error {
	tx, err := s.beginRuntimeTx(ctx, nil, preowned...)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	// Only planning-rate samples are combined. Every usage payload and exact
	// residual remains independently persisted in this same FULL transaction.
	tx.authorityReads = make(map[[2]string]authorityRead)
	samples := []demandConsumptionSample{}
	tx.demandSamples = &samples
	if err := checkMembers(); err != nil {
		return err
	}
	for _, entry := range entries {
		if err := checkpointRuntimeFundingBlockTx(ctx, tx, entry.Block, entry.CheckpointID, entry.Used, entry.Payload); err != nil {
			return err
		}
	}
	tx.demandSamples = nil
	for _, sample := range samples {
		if err := recordDemandConsumptionTx(ctx, tx, sample.node, sample.grant, sample.used, sample.at); err != nil {
			return err
		}
	}
	if err := checkMembers(); err != nil {
		return err
	}
	return tx.Commit()
}
