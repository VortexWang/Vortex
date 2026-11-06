package store

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"time"

	"vortex/contracts"
)

type UnknownUsageRecord struct {
	NodeID, BatchID, OriginBootID, SnapshotID, GrantID, RuleID, Direction string
	Upper                                                                 uint64
	Payload, Digest, LegacyBeforeImage                                    []byte
	TransportState, LiabilityState                                        string
}

func unknownRecord(node string, payload []byte) (UnknownUsageRecord, error) {
	if node == "" {
		return UnknownUsageRecord{}, fmt.Errorf("unknown usage: authenticated node identity missing")
	}
	b, err := contracts.DecodeUnknownUsage(payload)
	if err != nil {
		return UnknownUsageRecord{}, err
	}
	digest := sha256.Sum256(payload)
	return UnknownUsageRecord{NodeID: node, BatchID: b.GetBatchId(), OriginBootID: b.GetOriginBootId(), SnapshotID: b.GetBillingSnapshotId(), GrantID: b.GetGrantId(), RuleID: b.GetRuleId(), Direction: b.GetDirection(), Upper: b.GetUnknownUpperBound(), Payload: payload, Digest: digest[:]}, nil
}

// insertUnknownUsageTx stores a typed unresolved liability. An optional legacy
// record permits only exact migration of an unacknowledged local before-image.
func insertUnknownUsageTx(ctx context.Context, tx *sql.Tx, r UnknownUsageRecord, state string, legacy *UsageBatch) error {
	var compact int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM settled_usage_tombstones WHERE node_id=? AND batch_id=?`, r.NodeID, r.BatchID).Scan(&compact); err != nil {
		return err
	}
	if compact != 0 {
		return ErrUsageBatchConflict
	}
	var oldPayload, oldBefore []byte
	err := tx.QueryRowContext(ctx, `SELECT payload,legacy_before_image FROM unknown_usage_batches WHERE node_id=? AND batch_id=?`, r.NodeID, r.BatchID).Scan(&oldPayload, &oldBefore)
	if err == nil {
		if !bytes.Equal(oldPayload, r.Payload) || legacy != nil && !bytes.Equal(oldBefore, legacy.Payload) {
			return ErrUsageBatchConflict
		}
		return nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	var originalPayload, originalDigest []byte
	var upper, origin, snapshot, oldState string
	err = tx.QueryRowContext(ctx, `SELECT payload,payload_sha256,unknown_upper_bound,boot_id,snapshot_id,state FROM usage_batches WHERE node_id=? AND batch_id=?`, r.NodeID, r.BatchID).Scan(&originalPayload, &originalDigest, &upper, &origin, &snapshot, &oldState)
	var before []byte
	if err == nil {
		if legacy == nil || oldState == "hub_acked" || upper != strconv.FormatUint(r.Upper, 10) || origin != r.OriginBootID || snapshot != r.SnapshotID || !bytes.Equal(originalPayload, legacy.Payload) || !bytes.Equal(originalDigest, legacy.Digest) {
			return ErrUsageBatchConflict
		}
		before = originalPayload
	} else if !errors.Is(err, sql.ErrNoRows) {
		return err
	} else if legacy != nil {
		return ErrUsageBatchConflict
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO unknown_usage_batches(node_id,batch_id,origin_boot_id,snapshot_id,grant_id,rule_id,direction,unknown_upper_bound,payload,payload_sha256,transport_state,liability_state,legacy_before_image,created_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,'unresolved',?,?)`, r.NodeID, r.BatchID, r.OriginBootID, r.SnapshotID, r.GrantID, r.RuleID, r.Direction, strconv.FormatUint(r.Upper, 10), r.Payload, r.Digest, state, before, time.Now().UTC().Format(time.RFC3339Nano))
	return err
}

// PersistUnknownUsage commits before a Hub can ACK. It never changes a grant
// balance or records the upper bound as actual consumed bytes.
func (s *Store) PersistUnknownUsage(ctx context.Context, node string, payload []byte) error {
	r, err := unknownRecord(node, payload)
	if err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	state := "pending"
	if s.owner == "hub" {
		state = "received"
	}
	if err := insertUnknownUsageTx(ctx, tx, r, state, nil); err != nil {
		if errors.Is(err, ErrUsageBatchConflict) {
			_ = tx.Rollback()
			if _, writeErr := s.db.ExecContext(ctx, `INSERT INTO unknown_usage_conflicts(node_id,batch_id,payload,payload_sha256,created_at) VALUES(?,?,?,?,?) ON CONFLICT DO NOTHING`, r.NodeID, r.BatchID, r.Payload, r.Digest, time.Now().UTC().Format(time.RFC3339Nano)); writeErr != nil {
				return fmt.Errorf("unknown conflict evidence: %w", writeErr)
			}
		}
		return err
	}
	return tx.Commit()
}

func (s *Store) MigrateLegacyUnknownUsage(ctx context.Context, original UsageBatch, payload []byte) error {
	if s.owner != "agent" || original.UnknownUpper == 0 {
		return ErrUsageBatchConflict
	}
	digest := sha256.Sum256(original.Payload)
	if !bytes.Equal(digest[:], original.Digest) {
		return ErrUsageBatchConflict
	}
	r, err := unknownRecord(original.NodeID, payload)
	if err != nil {
		return err
	}
	if r.BatchID != original.BatchID || r.OriginBootID != original.BootID || r.SnapshotID != original.SnapshotID || r.Upper != original.UnknownUpper {
		return ErrUsageBatchConflict
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := insertUnknownUsageTx(ctx, tx, r, "pending", &original); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) PendingUnknownUsage(ctx context.Context, node string) ([]UnknownUsageRecord, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT batch_id,payload,payload_sha256,legacy_before_image,transport_state,liability_state FROM unknown_usage_batches WHERE node_id=? AND transport_state='pending' ORDER BY created_at,batch_id`, node)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []UnknownUsageRecord
	for rows.Next() {
		var id, state, liability string
		var payload, digest, before []byte
		if err := rows.Scan(&id, &payload, &digest, &before, &state, &liability); err != nil {
			return nil, err
		}
		r, err := unknownRecord(node, payload)
		if err != nil {
			return nil, err
		}
		if r.BatchID != id || !bytes.Equal(r.Digest, digest) || liability != "unresolved" {
			return nil, ErrUsageBatchConflict
		}
		r.LegacyBeforeImage, r.TransportState, r.LiabilityState = before, state, liability
		result = append(result, r)
	}
	return result, rows.Err()
}

func (s *Store) AcknowledgeUnknownUsage(ctx context.Context, node, batch string, digest []byte) error {
	if s.owner != "agent" || len(digest) != sha256.Size {
		return ErrUsageBatchConflict
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var expected []byte
	var state, liability string
	if err := tx.QueryRowContext(ctx, `SELECT payload_sha256,transport_state,liability_state FROM unknown_usage_batches WHERE node_id=? AND batch_id=?`, node, batch).Scan(&expected, &state, &liability); err != nil {
		return err
	}
	if !bytes.Equal(expected, digest) || liability != "unresolved" || (state != "pending" && state != "acknowledged") {
		return ErrUsageBatchConflict
	}
	if _, err := tx.ExecContext(ctx, `UPDATE unknown_usage_batches SET transport_state='acknowledged' WHERE node_id=? AND batch_id=?`, node, batch); err != nil {
		return err
	}
	return tx.Commit()
}

func rejectUnknownIdentityTx(ctx context.Context, tx *sql.Tx, node, batch string) error {
	var count int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM unknown_usage_batches WHERE node_id=? AND batch_id=?`, node, batch).Scan(&count); err != nil {
		return err
	}
	if count != 0 {
		return ErrUsageBatchConflict
	}
	return nil
}
