package store

import (
	"context"
	"strconv"
)

// AdmitRuntimeFundingBlockWrite revalidates a held block and spends shared rate
// allowance in one transaction. It does not allocate any additional credit or
// track concurrent child writes: an exclusive Agent allocator must bound those
// writes by the residual. Each call spends rate anew; ambiguous commit failures
// must stop the allocator rather than assume permission or refund tokens.
// A committed admission precedes later control updates, just like ordinary
// ReserveRuntimeUsage. No cached authorization snapshot is a write capability.
func (s *Store) AdmitRuntimeFundingBlockWrite(ctx context.Context, block UsageReservation, scope AuthorizationUse, n uint64) error {
	if s.owner != "agent" || scope.SessionID == "" || block.NodeID != scope.NodeID || block.BootID != scope.BootID || block.SnapshotID != scope.BillingSnapshotID || block.RuleID != scope.RuleID || n == 0 || n > block.Reserved {
		return ErrAuthorizationDenied
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var boot, snapshot, flow, rule, direction, reserved, state, auth, held, holdState string
	var session, sessionBoot, sessionFlow, source, sessionState, grant, runtime, share string
	err = tx.QueryRowContext(ctx, `SELECT u.boot_id,u.snapshot_id,u.flow_key,u.rule_id,u.direction,u.reserved_bytes,u.state,h.authorization_id,h.reserved_bytes,h.state,b.session_id,s.boot_id,s.flow_key,s.source_id,s.state,s.grant_id,s.runtime_lease_id,s.share_lease_id FROM runtime_funding_blocks b JOIN usage_reservations u ON u.node_id=b.node_id AND u.batch_id=b.batch_id JOIN authorization_usage_holds h ON h.node_id=u.node_id AND h.batch_id=u.batch_id JOIN runtime_sessions s ON s.node_id=b.node_id AND s.session_id=b.session_id WHERE b.node_id=? AND b.batch_id=?`, block.NodeID, block.BatchID).Scan(&boot, &snapshot, &flow, &rule, &direction, &reserved, &state, &auth, &held, &holdState, &session, &sessionBoot, &sessionFlow, &source, &sessionState, &grant, &runtime, &share)
	if err != nil {
		return err
	}
	if boot != block.BootID || snapshot != block.SnapshotID || flow != block.FlowKey || rule != block.RuleID || direction != block.Direction || reserved != strconv.FormatUint(block.Reserved, 10) || auth != block.AuthorizationID || held != reserved || state != "reserved" || holdState != "reserved" || session != scope.SessionID || sessionBoot != block.BootID || sessionFlow != block.FlowKey || sessionState != "open" {
		return ErrAuthorizationDenied
	}
	// Read current IDs from the durable session, never trust caller-supplied
	// replacement leases. Validate both the current and the held grant.
	// The join above already checked session identity and selected its IDs.
	// Validate each distinct authority once in this transaction, instead of
	// recursively re-reading runtime/share for both current and held grants.
	scope.SessionID = ""
	scope.RuntimeLeaseID, scope.ShareLeaseID = "", ""
	scope.ExistingSession = true
	scope.Kind, scope.AuthorizationID = "credit_grant", grant
	current, err := checkAuthorizationTx(ctx, tx, scope)
	if err != nil {
		return err
	}
	heldGrant := current
	if auth != grant {
		scope.AuthorizationID = auth
		heldGrant, err = checkAuthorizationTx(ctx, tx, scope)
		if err != nil {
			return err
		}
	}
	if !sameRuntimeFamily(current, heldGrant) {
		return ErrAuthorityHandoffRequired
	}
	scope.Kind, scope.AuthorizationID = "runtime_lease", runtime
	if _, err := checkAuthorizationTx(ctx, tx, scope); err != nil {
		return err
	}
	scope.Kind, scope.AuthorizationID = "share_lease", share
	shareRecord, err := checkAuthorizationTx(ctx, tx, scope)
	if err != nil {
		return err
	}
	var count uint64
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM runtime_sessions WHERE node_id=? AND source_id=? AND state='open'`, block.NodeID, source).Scan(&count); err != nil {
		return err
	}
	if shareRecord.SourceID != source || count > shareRecord.Connections {
		return ErrAuthorizationDenied
	}
	if err := consumeValidatedRuntimeRateTx(ctx, tx, scope, n, shareRecord); err != nil {
		return err
	}
	return tx.Commit()
}
