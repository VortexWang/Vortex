package store

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"math"

	"google.golang.org/protobuf/proto"
	controlv1 "vortex/contracts/protobuf/v1"
)

func decodeKnownHubUsage(b HubUsage) (UsageBatch, error) {
	var wire controlv1.UsageBatch
	if err := proto.Unmarshal(b.Payload, &wire); err != nil {
		return UsageBatch{}, err
	}
	if wire.BatchId != b.BatchID || wire.BillingSnapshotId == "" {
		return UsageBatch{}, ErrUsageBatchConflict
	}
	return UsageBatch{NodeID: b.NodeID, BootID: b.BootID, BatchID: b.BatchID, SnapshotID: wire.BillingSnapshotId, Payload: b.Payload, Digest: b.Digest}, nil
}

// PersistKnownUsageGroup retains individual payloads and provenance while
// sharing one FULL commit. A failure exposes no partial group as durable.
// Origin-less callers are backend adapters which already verified provenance.
func (s *Store) PersistKnownUsageGroup(ctx context.Context, batches []HubUsage, withOrigin bool) ([]HubUsage, error) {
	if s.owner != "hub" || len(batches) == 0 || len(batches) > 32 {
		return nil, ErrUsageBatchConflict
	}
	result := append([]HubUsage(nil), batches...)
	decoded := make([]UsageBatch, len(result))
	for i, b := range result {
		digest := sha256.Sum256(b.Payload)
		if b.Kind != "known" || b.NodeID == "" || b.BootID == "" || b.BatchID == "" || len(b.Payload) == 0 || len(b.Payload) > 1<<20 || !bytes.Equal(digest[:], b.Digest) {
			return nil, ErrUsageBatchConflict
		}
		batch, err := decodeKnownHubUsage(b)
		if err != nil {
			return nil, err
		}
		decoded[i] = batch
	}
	tx, err := s.beginRuntimeTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	conflictIndex := 0
	if len(result) == 1 {
		err = s.putUsageBatchTx(ctx, tx, decoded[0], result[0].Digest, "ledgered")
	} else {
		conflictIndex, err = persistKnownUsageSet(ctx, tx, result, decoded)
	}
	if err != nil {
		if errors.Is(err, ErrUsageBatchConflict) {
			_ = tx.Rollback()
			b := result[conflictIndex]
			if evidenceErr := s.RecordUsageDispute(ctx, b.NodeID, b.BatchID, b.Payload); evidenceErr != nil {
				return nil, fmt.Errorf("store: retain usage conflict: %w", evidenceErr)
			}
		}
		return nil, err
	}
	for i, b := range result {
		if withOrigin {
			if b.ReportBootID == "" || b.Sequence == 0 || b.Sequence > math.MaxInt64 {
				return nil, ErrUsageOriginMissing
			}
			result[i].ReportBootID, result[i].Sequence, err = recordUsageOriginTx(ctx, tx, b.NodeID, b.BatchID, b.ReportBootID, b.Sequence, b.Digest)
			if err != nil {
				return nil, err
			}
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return result, nil
}

// ConfirmBackendUsageGroup is called only after every backend acceptance has
// returned its exact digest. Receipt visibility follows this FULL commit.
func (s *Store) ConfirmBackendUsageGroup(ctx context.Context, batches []HubUsage) error {
	if s.owner != "hub" || len(batches) == 0 || len(batches) > 32 {
		return ErrUsageBatchConflict
	}
	tx, err := s.beginRuntimeTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	allKnown := true
	for _, b := range batches {
		allKnown = allKnown && b.Kind == "known"
	}
	if allKnown && len(batches) > 1 {
		if err := confirmKnownUsageSet(ctx, tx, batches); err != nil {
			return err
		}
		return tx.Commit()
	}
	for _, b := range batches {
		if err := confirmBackendUsageTx(ctx, tx, b, b.Digest); err != nil {
			return err
		}
	}
	return tx.Commit()
}
