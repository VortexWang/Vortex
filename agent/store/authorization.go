package store

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"
)

var ErrAuthorizationDenied = errors.New("store: durable authorization does not permit this operation")

// AuthorizationUse pins the caller's local rule and billing identity. The time
// interval must come from a trusted conservative clock. A single wall-clock
// sample after restart does not establish trusted time.
type AuthorizationUse struct {
	NodeID, BootID, AuthorizationID string
	Kind, RuleID, BillingSnapshotID string
	RuleRevision, PathGeneration    uint64
	Earliest, Latest                time.Time
	TrustedTime                     bool
	ExistingSession                 bool
}

// CheckStoredAuthorization checks a persisted lease or grant without issuing,
// renewing, spending or releasing it. Credit amounts returned here are limits,
// not proof of remaining quota; callers still require an atomic reservation.
// The result is a snapshot, not a capability to write after a later revocation.
func (s *Store) CheckStoredAuthorization(ctx context.Context, use AuthorizationUse) (AuthorizationRecord, error) {
	var record AuthorizationRecord
	if !use.TrustedTime || use.Earliest.IsZero() || use.Latest.Before(use.Earliest) {
		return record, ErrAuthorityTimeUncertain
	}
	if use.NodeID == "" || use.BootID == "" || use.AuthorizationID == "" || use.Kind == "" || use.RuleID == "" || use.RuleRevision == 0 || use.PathGeneration == 0 {
		return record, ErrAuthorizationDenied
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return record, err
	}
	defer tx.Rollback()
	var state, ruleRevision, pathGeneration, signerEpoch, amount, rate, connections string
	err = tx.QueryRowContext(ctx, `SELECT authorization_id,parent_authorization_id,source_id,kind,rule_id,rule_revision,path_generation,signer_epoch,boot_id,issued_at,not_before,expires_at,hard_deadline,billing_snapshot_id,amount_bytes,rate_bps,connections,state FROM authorization_records WHERE node_id=? AND authorization_id=?`, use.NodeID, use.AuthorizationID).Scan(
		&record.AuthorizationID, &record.ParentAuthorizationID, &record.SourceID, &record.Kind, &record.RuleID,
		&ruleRevision, &pathGeneration, &signerEpoch, &record.BootID, &record.IssuedAt, &record.NotBefore,
		&record.ExpiresAt, &record.HardDeadline, &record.BillingSnapshotID, &amount, &rate, &connections, &state)
	if err != nil {
		return AuthorizationRecord{}, fmt.Errorf("%w: %w", ErrAuthorizationDenied, err)
	}
	for _, field := range []struct {
		text  string
		value *uint64
	}{
		{ruleRevision, &record.RuleRevision}, {pathGeneration, &record.PathGeneration},
		{signerEpoch, &record.SignerEpoch}, {amount, &record.AmountBytes},
		{rate, &record.RateBPS}, {connections, &record.Connections},
	} {
		*field.value, err = strconv.ParseUint(field.text, 10, 64)
		if err != nil {
			return AuthorizationRecord{}, ErrAuthorizationDenied
		}
	}
	if state != "active" || record.BootID != use.BootID || record.Kind != use.Kind || record.RuleID != use.RuleID || record.RuleRevision != use.RuleRevision || record.PathGeneration != use.PathGeneration || record.BillingSnapshotID != use.BillingSnapshotID {
		return AuthorizationRecord{}, ErrAuthorizationDenied
	}
	notBefore, e1 := time.Parse(time.RFC3339Nano, record.NotBefore)
	expires, e2 := time.Parse(time.RFC3339Nano, record.ExpiresAt)
	deadline, e3 := time.Parse(time.RFC3339Nano, record.HardDeadline)
	if e1 != nil || e2 != nil || e3 != nil || use.Earliest.Before(notBefore) || !use.Latest.Before(expires) || !use.Latest.Before(deadline) {
		return AuthorizationRecord{}, ErrAuthorizationDenied
	}
	// Signer epoch and path generation are intentionally independent. Epoch
	// handoff does not invalidate an otherwise valid outstanding authorization.
	// Old paths can only drain sessions that already existed.
	if !use.ExistingSession {
		current, err := stateValue(ctx, tx, "authorization_path_generation")
		if err != nil {
			return AuthorizationRecord{}, err
		}
		generation, err := strconv.ParseUint(current, 10, 64)
		if err != nil || generation != use.PathGeneration {
			return AuthorizationRecord{}, ErrAuthorizationDenied
		}
	}
	if err := tx.Commit(); err != nil {
		return AuthorizationRecord{}, err
	}
	return record, nil
}
