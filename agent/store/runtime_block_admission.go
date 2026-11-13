package store

import (
	"context"
	"strconv"
	"time"
)

// AdmitRuntimeFundingBlockWrite revalidates a held block and spends shared rate
// allowance in one transaction. It does not allocate any additional credit or
// track concurrent child writes: an exclusive Agent allocator must bound those
// writes by the residual. Each call spends rate anew; ambiguous commit failures
// must stop the allocator rather than assume permission or refund tokens.
// A committed admission precedes later control updates, just like ordinary
// ReserveRuntimeUsage. No cached authorization snapshot is a write capability.
func (s *Store) AdmitRuntimeFundingBlockWrite(ctx context.Context, block UsageReservation, scope AuthorizationUse, n uint64, clocks ...func() (time.Time, time.Time, bool)) error {
	if s.owner != "agent" || scope.SessionID == "" || block.NodeID != scope.NodeID || block.BootID != scope.BootID || block.SnapshotID != scope.BillingSnapshotID || block.RuleID != scope.RuleID || n == 0 || n > block.Reserved {
		return ErrAuthorizationDenied
	}
	return s.AdmitRuntimeFundingBlockBatch(ctx, []RuntimeBlockAdmission{{Block: block, Scope: scope, Bytes: n}}, clocks...)
}

// Evaluate the original identity comparisons inside SQLite; return only the
// authority IDs needed below, avoiding 19 per-column driver conversions. The
// same joined row and current transaction govern every check.
const fundingBlockAdmissionQuery = `SELECT u.boot_id=? AND u.snapshot_id=? AND u.flow_key=? AND u.rule_id=? AND u.direction=? AND u.reserved_bytes=? AND h.authorization_id=? AND h.reserved_bytes=u.reserved_bytes AND u.state='reserved' AND h.state='reserved' AND b.session_id=? AND s.boot_id=u.boot_id AND s.flow_key=u.flow_key AND s.state='open',h.authorization_id,s.source_id,s.grant_id,s.runtime_lease_id,s.share_lease_id FROM runtime_funding_blocks b JOIN usage_reservations u ON u.node_id=b.node_id AND u.batch_id=b.batch_id JOIN authorization_usage_holds h ON h.node_id=u.node_id AND h.batch_id=u.batch_id JOIN runtime_sessions s ON s.node_id=b.node_id AND s.session_id=b.session_id WHERE b.node_id=? AND b.batch_id=?`

func admitRuntimeFundingBlockWriteTx(ctx context.Context, tx storeTransaction, block UsageReservation, scope AuthorizationUse, n uint64) error {
	var valid bool
	var auth, source, grant, runtime, share string
	err := tx.QueryRowContext(ctx, fundingBlockAdmissionQuery, block.BootID, block.SnapshotID, block.FlowKey, block.RuleID, block.Direction, strconv.FormatUint(block.Reserved, 10), block.AuthorizationID, scope.SessionID, block.NodeID, block.BatchID).Scan(&valid, &auth, &source, &grant, &runtime, &share)
	if err != nil {
		return err
	}
	if !valid {
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
	return nil
}

// RuntimeBlockAdmission is one bounded write against an already funded block.
// The allocator must retain exclusive ownership of all blocks until the batch
// returns and include existing in-flight bytes in its own capacity accounting.
type RuntimeBlockAdmission struct {
	Block UsageReservation
	Scope AuthorizationUse
	Bytes uint64
}

// AdmitRuntimeFundingBlockBatch checks at most 64 writes and commits their rate
// debits together. Any denial or error rolls the entire batch back. Successful
// calls are not retry capabilities: repeated calls debit rate again. No caller
// may begin a write before this method successfully returns.
func (s *Store) AdmitRuntimeFundingBlockBatch(ctx context.Context, writes []RuntimeBlockAdmission, clocks ...func() (time.Time, time.Time, bool)) error {
	if len(clocks) > 1 || len(clocks) == 1 && clocks[0] == nil || s.owner != "agent" || len(writes) == 0 || len(writes) > 64 {
		return ErrAuthorizationDenied
	}
	// Coalesce only exact block/scope identities. Use conservative time union so
	// batching cannot grant validity or rate refill outside any caller's bounds.
	type identity struct{ node, batch string }
	grouped := make([]RuntimeBlockAdmission, 0, len(writes))
	positions := make(map[identity]int, len(writes))
	for _, write := range writes {
		b, u := write.Block, write.Scope
		if u.SessionID == "" || b.NodeID != u.NodeID || b.BootID != u.BootID || b.SnapshotID != u.BillingSnapshotID || b.RuleID != u.RuleID || write.Bytes == 0 || write.Bytes > b.Reserved {
			return ErrAuthorizationDenied
		}
		if !u.TrustedTime || u.Earliest.IsZero() || u.Latest.Before(u.Earliest) {
			return ErrAuthorityTimeUncertain
		}
		key := identity{b.NodeID, b.BatchID}
		if index, ok := positions[key]; ok {
			prior := &grouped[index]
			normalized := u
			normalized.Earliest, normalized.Latest = prior.Scope.Earliest, prior.Scope.Latest
			if prior.Block != b || prior.Scope != normalized {
				return ErrUsageBatchConflict
			}
			if write.Bytes > b.Reserved-prior.Bytes {
				return ErrAuthorizationDenied
			}
			prior.Bytes += write.Bytes
			if u.Earliest.Before(prior.Scope.Earliest) {
				prior.Scope.Earliest = u.Earliest
			}
			if u.Latest.After(prior.Scope.Latest) {
				prior.Scope.Latest = u.Latest
			}
		} else {
			positions[key] = len(grouped)
			grouped = append(grouped, write)
		}
	}
	return s.enqueueFundingAdmissions(ctx, grouped, clocks...)
}
