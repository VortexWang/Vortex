package store

import (
	"context"
	"encoding/json"
	"time"
)

// SealGrantAfterIdle combines the conservative timer and local fence. Threshold
// is explicitly supplied by the caller; this API does not lock D-04 defaults.
// It cannot refund credit, and must not be used as a substitute for Hub proof
// reconciliation. Activity triggers and this transaction serialize the decision.
func (s *Store) SealGrantAfterIdle(ctx context.Context, scope DemandScope, grantID string, now IssuerTime, threshold time.Duration) error {
	if s.owner != "agent" || grantID == "" || threshold <= 0 {
		return ErrAuthorizationDenied
	}
	if err := checkIssuerTime(now); err != nil {
		return err
	}
	key, err := json.Marshal(scope)
	if err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var sinceText, lastText string
	if err := tx.QueryRowContext(ctx, `SELECT since_latest,observed_latest FROM demand_idle_observations WHERE scope=?`, key).Scan(&sinceText, &lastText); err != nil {
		return err
	}
	since, e1 := time.Parse(time.RFC3339Nano, sinceText)
	last, e2 := time.Parse(time.RFC3339Nano, lastText)
	if e1 != nil || e2 != nil || now.Latest.Before(last) || now.Latest.Before(since) {
		// Persist invalidation even though sealing fails. Clock recovery must
		// not resurrect an interval whose continuity was lost.
		if _, err := tx.ExecContext(ctx, `DELETE FROM demand_idle_observations WHERE scope=?`, key); err != nil {
			return err
		}
		if err := tx.Commit(); err != nil {
			return err
		}
		return ErrAuthorityTimeUncertain
	}
	if now.Earliest.Sub(since) < threshold {
		return ErrAuthorityTimeUncertain
	}
	// Pending work also invalidates observations via triggers. Audit pending
	// grant batches again to fail closed against pre-migration or damaged state.
	balance, err := auditAuthorizationBalanceTx(ctx, tx, scope.NodeID, grantID)
	if err != nil {
		return err
	}
	if balance.Reserved != 0 {
		return ErrAuthorizationDenied
	}
	if err := sealGrantTx(ctx, tx, scope, grantID, key); err != nil {
		return err
	}
	return tx.Commit()
}
