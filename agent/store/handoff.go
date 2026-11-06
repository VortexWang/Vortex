package store

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strconv"
	"time"
)

// HandoffRequest is accepted only from the authenticated backend adapter.
// Revocations are applied in the same transaction, before any ownership moves.
// Original delegation limits and outstanding grants remain unchanged.
type HandoffRequest struct {
	ID, HubID          string
	FromEpoch, ToEpoch uint64
	Heartbeat          time.Time
	RevokeDelegations  []string
}
type HandoffResult struct {
	SealedSequence int64
	Remaining      map[string]uint64
}

func (s *Store) HandoffHub(ctx context.Context, r HandoffRequest, now IssuerTime) (HandoffResult, error) {
	result := HandoffResult{Remaining: map[string]uint64{}}
	if s.owner != "hub" || !validIssuerUUID(r.ID) || !validIssuerUUID(r.HubID) || r.FromEpoch == 0 || r.FromEpoch == math.MaxUint64 || r.ToEpoch != r.FromEpoch+1 {
		return result, ErrAuthorizationDenied
	}
	if err := checkIssuerTime(now); err != nil {
		return result, err
	}
	raw, err := json.Marshal(r)
	if err != nil {
		return result, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return result, err
	}
	defer tx.Rollback()
	var prior []byte
	err = tx.QueryRowContext(ctx, `SELECT request,sealed_sequence FROM hub_handoffs WHERE handoff_id=?`, r.ID).Scan(&prior, &result.SealedSequence)
	if err == nil {
		if !bytes.Equal(prior, raw) {
			return result, ErrAuthorizationUpdateConflict
		}
		rows, err := tx.QueryContext(ctx, `SELECT delegation_id,remaining_bytes FROM hub_transfers WHERE handoff_id=?`, r.ID)
		if err != nil {
			return result, err
		}
		for rows.Next() {
			var id, n string
			if err = rows.Scan(&id, &n); err != nil {
				rows.Close()
				return result, err
			}
			v, e := strconv.ParseUint(n, 10, 64)
			if e != nil {
				rows.Close()
				return result, e
			}
			result.Remaining[id] = v
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return result, err
		}
		return result, tx.Commit()
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return result, err
	}
	var epoch string
	if err = tx.QueryRowContext(ctx, `SELECT signer_epoch FROM issuer_epochs WHERE hub_id=?`, r.HubID).Scan(&epoch); err != nil {
		return result, err
	}
	if epoch != strconv.FormatUint(r.FromEpoch, 10) {
		return result, ErrSignerEpochClosed
	}
	oldText, err := stateValue(ctx, tx, "backend_heartbeat")
	if err != nil {
		return result, err
	}
	old, err := time.Parse(time.RFC3339Nano, oldText)
	if err != nil {
		return result, err
	}
	deadline := deadlineFromTx(ctx, tx)
	if deadline.IsZero() || !r.Heartbeat.After(old) || now.Earliest.Before(r.Heartbeat) || !now.Latest.Before(r.Heartbeat.Add(BackendLossGrace)) || !now.Latest.Before(deadline) {
		return result, ErrAuthorityExpired
	}
	for _, id := range r.RevokeDelegations {
		res, err := tx.ExecContext(ctx, `UPDATE issuer_delegations SET state='revoked' WHERE delegation_id=? AND hub_id=?`, id, r.HubID)
		if err != nil {
			return result, err
		}
		n, err := res.RowsAffected()
		if err != nil {
			return result, err
		}
		if n != 1 {
			return result, ErrAuthorizationDenied
		}
	}
	if _, err = tx.ExecContext(ctx, `UPDATE issuer_epochs SET state='closed' WHERE hub_id=?`, r.HubID); err != nil {
		return result, err
	}
	if err = tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(sequence),0) FROM hub_ledger`).Scan(&result.SealedSequence); err != nil {
		return result, err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO hub_handoffs(handoff_id,hub_id,from_epoch,to_epoch,sealed_sequence,request) VALUES(?,?,?,?,?,?)`, r.ID, r.HubID, epoch, strconv.FormatUint(r.ToEpoch, 10), result.SealedSequence, raw); err != nil {
		return result, err
	}
	rows, err := tx.QueryContext(ctx, `SELECT delegation_id,remaining_bytes FROM issuer_delegations WHERE hub_id=? AND signer_epoch=? AND state='open' ORDER BY delegation_id`, r.HubID, epoch)
	if err != nil {
		return result, err
	}
	for rows.Next() {
		var id, n string
		if err = rows.Scan(&id, &n); err != nil {
			rows.Close()
			return result, err
		}
		v, e := strconv.ParseUint(n, 10, 64)
		if e != nil {
			rows.Close()
			return result, e
		}
		result.Remaining[id] = v
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return result, err
	}
	for id, n := range result.Remaining {
		if _, err = tx.ExecContext(ctx, `INSERT INTO hub_transfers(handoff_id,delegation_id,remaining_bytes) VALUES(?,?,?)`, r.ID, id, strconv.FormatUint(n, 10)); err != nil {
			return result, err
		}
	}
	if _, err = tx.ExecContext(ctx, `UPDATE issuer_delegations SET signer_epoch=? WHERE hub_id=? AND signer_epoch=? AND state='open'`, strconv.FormatUint(r.ToEpoch, 10), r.HubID, epoch); err != nil {
		return result, err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE issuer_epochs SET signer_epoch=?,state='open' WHERE hub_id=?`, strconv.FormatUint(r.ToEpoch, 10), r.HubID); err != nil {
		return result, err
	}
	for key, value := range map[string]string{"backend_heartbeat": r.Heartbeat.UTC().Format(time.RFC3339Nano), "authority_mode": AutonomyRecovering, "signer_epoch_open": "true"} {
		if err = putState(ctx, tx, key, value, r.Heartbeat); err != nil {
			return result, err
		}
	}
	record, err := json.Marshal(struct {
		Request HandoffRequest
		Result  HandoffResult
	}{r, result})
	if err != nil {
		return result, err
	}
	sum := sha256.Sum256(record)
	if _, err = tx.ExecContext(ctx, `INSERT INTO hub_ledger(kind,record_id,payload,payload_sha256) VALUES('handoff',?,?,?)`, r.ID, record, sum[:]); err != nil {
		return result, err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO hub_recoveries(handoff_id,target_sequence,completed) SELECT ?,MAX(sequence),0 FROM hub_ledger`, r.ID); err != nil {
		return result, err
	}
	if err = putState(ctx, tx, "recovery_handoff_id", r.ID, r.Heartbeat); err != nil {
		return result, err
	}
	return result, tx.Commit()
}

type LedgerRecord struct {
	Sequence        int64
	Kind, ID        string
	Payload, Digest []byte
}

// PendingLedger includes sealed prefix and ongoing tail. Nothing is deleted by
// confirmation: unresolved and disputed liabilities remain available for audit.
func (s *Store) PendingLedger(ctx context.Context, limit int) ([]LedgerRecord, error) {
	if s.owner != "hub" || limit < 1 || limit > 1000 {
		return nil, ErrAuthorizationDenied
	}
	rows, err := s.db.QueryContext(ctx, `SELECT sequence,kind,record_id,payload,payload_sha256 FROM hub_ledger WHERE confirmed=0 ORDER BY sequence LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []LedgerRecord
	for rows.Next() {
		var r LedgerRecord
		if err = rows.Scan(&r.Sequence, &r.Kind, &r.ID, &r.Payload, &r.Digest); err != nil {
			return nil, err
		}
		sum := sha256.Sum256(r.Payload)
		if !bytes.Equal(sum[:], r.Digest) {
			return nil, fmt.Errorf("store: corrupt ledger record")
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// ConfirmLedger advances only across a contiguous confirmed prefix. ACKs bind
// the persisted payload hash and identity; out-of-order ACKs cannot skip holes.
func (s *Store) ConfirmLedger(ctx context.Context, r LedgerRecord, ack []byte) (int64, error) {
	if s.owner != "hub" || r.Sequence <= 0 || !bytes.Equal(r.Digest, ack) {
		return 0, ErrUsageBatchConflict
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	var kind, id string
	var payload, digest []byte
	if err = tx.QueryRowContext(ctx, `SELECT kind,record_id,payload,payload_sha256 FROM hub_ledger WHERE sequence=?`, r.Sequence).Scan(&kind, &id, &payload, &digest); err != nil {
		return 0, err
	}
	sum := sha256.Sum256(payload)
	if kind != r.Kind || id != r.ID || !bytes.Equal(payload, r.Payload) || !bytes.Equal(sum[:], digest) || !bytes.Equal(digest, ack) {
		return 0, ErrUsageBatchConflict
	}
	if _, err = tx.ExecContext(ctx, `UPDATE hub_ledger SET confirmed=1 WHERE sequence=?`, r.Sequence); err != nil {
		return 0, err
	}
	var checkpoint int64
	if err = tx.QueryRowContext(ctx, `SELECT COALESCE((SELECT MIN(sequence)-1 FROM hub_ledger WHERE confirmed=0),(SELECT COALESCE(MAX(sequence),0) FROM hub_ledger))`).Scan(&checkpoint); err != nil {
		return 0, err
	}
	if err = putState(ctx, tx, "ledger_checkpoint", strconv.FormatInt(checkpoint, 10), time.Now().UTC()); err != nil {
		return 0, err
	}
	return checkpoint, tx.Commit()
}
