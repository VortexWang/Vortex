package store

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"math"
	"time"
)

// PersistUsageLedgerWithOrigin commits a known Hub ledger row, its journal
// entry and the first authenticated report identity in one FULL transaction.
// The returned provenance is usable only after that transaction succeeds.
func (s *Store) PersistUsageLedgerWithOrigin(ctx context.Context, batch UsageBatch, reportBoot string, sequence uint64) (string, uint64, error) {
	if batch.NodeID == "" || batch.BootID == "" || batch.BatchID == "" || batch.SnapshotID == "" || len(batch.Payload) == 0 {
		return "", 0, fmt.Errorf("store: usage batch identity, snapshot and payload are required")
	}
	if len(batch.Payload) > 1<<20 {
		return "", 0, fmt.Errorf("store: usage batch payload exceeds 1 MiB")
	}
	digest := sha256.Sum256(batch.Payload)
	if len(batch.Digest) != 0 && !bytes.Equal(batch.Digest, digest[:]) {
		return "", 0, fmt.Errorf("store: usage batch digest mismatch")
	}
	boot, seq, err := s.persistUsageWithOrigin(ctx, batch.NodeID, batch.BatchID, reportBoot, sequence, digest[:], func(tx storeTransaction) error {
		return s.putUsageBatchTx(ctx, tx, batch, digest[:], "ledgered")
	})
	if errors.Is(err, ErrUsageBatchConflict) {
		if evidenceErr := s.RecordUsageDispute(ctx, batch.NodeID, batch.BatchID, batch.Payload); evidenceErr != nil {
			return "", 0, fmt.Errorf("store: retain usage conflict: %w", evidenceErr)
		}
	}
	return boot, seq, err
}

// PersistUnknownUsageWithOrigin records the received upper bound as unresolved
// debt. It never books that upper bound as consumption or changes any balance.
func (s *Store) PersistUnknownUsageWithOrigin(ctx context.Context, node, reportBoot string, sequence uint64, payload []byte) (string, uint64, error) {
	r, err := unknownRecord(node, payload)
	if err != nil {
		return "", 0, err
	}
	boot, seq, err := s.persistUsageWithOrigin(ctx, node, r.BatchID, reportBoot, sequence, r.Digest, func(tx storeTransaction) error {
		return insertUnknownUsageTx(ctx, tx, r, "received", nil)
	})
	if errors.Is(err, ErrUsageBatchConflict) {
		if _, evidenceErr := s.db.ExecContext(ctx, `INSERT INTO unknown_usage_conflicts(node_id,batch_id,payload,payload_sha256,created_at) VALUES(?,?,?,?,?) ON CONFLICT DO NOTHING`, r.NodeID, r.BatchID, r.Payload, r.Digest, time.Now().UTC().Format(time.RFC3339Nano)); evidenceErr != nil {
			return "", 0, fmt.Errorf("unknown conflict evidence: %w", evidenceErr)
		}
	}
	return boot, seq, err
}

func (s *Store) persistUsageWithOrigin(ctx context.Context, node, batch, reportBoot string, sequence uint64, digest []byte, write func(storeTransaction) error) (string, uint64, error) {
	if s.owner != "hub" || node == "" || batch == "" || reportBoot == "" || sequence == 0 || sequence > math.MaxInt64 {
		return "", 0, ErrUsageOriginMissing
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return "", 0, err
	}
	defer tx.Rollback()
	if err := write(tx); err != nil {
		return "", 0, err
	}
	boot, seq, err := recordUsageOriginTx(ctx, tx, node, batch, reportBoot, sequence, digest)
	if err != nil {
		return "", 0, err
	}
	if err := tx.Commit(); err != nil {
		return "", 0, err
	}
	return boot, seq, nil
}
