package store

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"time"
)

type runtimeChoices struct {
	grant, runtime, share AuthorizationRecord
	available             uint64
}

// Same-session renewal keeps the initial source/issuer/epoch and absolute
// limits. It never interprets a new lease as proof of ownership transfer.
func sameRuntimeFamily(next, initial AuthorizationRecord) bool {
	if next.AuthorizationID == initial.AuthorizationID {
		return true
	}
	oldEnd, e1 := time.Parse(time.RFC3339Nano, initial.HardDeadline)
	newEnd, e2 := time.Parse(time.RFC3339Nano, next.HardDeadline)
	if e1 != nil || e2 != nil || newEnd.After(oldEnd) || initial.IssuerID == "" || next.IssuerID != initial.IssuerID {
		return false
	}
	if next.SourceID != initial.SourceID || next.ParentAuthorizationID != initial.ParentAuthorizationID || next.Kind != initial.Kind || next.BootID != initial.BootID {
		return false
	}
	if next.RuleID != initial.RuleID || next.RuleRevision != initial.RuleRevision || next.PathGeneration != initial.PathGeneration || next.SignerEpoch != initial.SignerEpoch || next.BillingSnapshotID != initial.BillingSnapshotID {
		return false
	}
	return next.Kind != "share_lease" || next.RateBPS <= initial.RateBPS && next.Connections <= initial.Connections
}

func selectRuntimeAuthorityTx(ctx context.Context, tx *sql.Tx, scope AuthorizationUse, minimum uint64, initial *runtimeChoices) (runtimeChoices, error) {
	var result runtimeChoices
	if !scope.TrustedTime || scope.Earliest.IsZero() || scope.Latest.Before(scope.Earliest) {
		return result, ErrAuthorityTimeUncertain
	}
	// The coarse SQL filter only bounds history scanned; exact conservative
	// expiry checks still run below. A seconds-level equality is never authority.
	rows, err := tx.QueryContext(ctx, `SELECT authorization_id,issued_at FROM authorization_records WHERE node_id=? AND boot_id=? AND rule_id=? AND rule_revision=? AND path_generation=? AND state='active' AND unixepoch(expires_at)>=unixepoch(?)`, scope.NodeID, scope.BootID, scope.RuleID, strconv.FormatUint(scope.RuleRevision, 10), strconv.FormatUint(scope.PathGeneration, 10), scope.Latest.UTC().Format(time.RFC3339Nano))
	if err != nil {
		return result, err
	}
	type candidate struct {
		id     string
		issued time.Time
	}
	var candidates []candidate
	for rows.Next() {
		var id, issued string
		if err := rows.Scan(&id, &issued); err != nil {
			rows.Close()
			return result, err
		}
		at, err := time.Parse(time.RFC3339Nano, issued)
		if err != nil {
			rows.Close()
			return result, ErrAuthorizationDenied
		}
		candidates = append(candidates, candidate{id, at})
		if len(candidates) > 4096 {
			rows.Close()
			return result, ErrAuthorizationDenied
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return result, err
	}
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].issued.Equal(candidates[j].issued) {
			return candidates[i].id > candidates[j].id
		}
		return candidates[i].issued.After(candidates[j].issued)
	})
	selected := map[string]AuthorizationRecord{}
	for _, c := range candidates {
		if selected["credit_grant"].AuthorizationID != "" && selected["runtime_lease"].AuthorizationID != "" && selected["share_lease"].AuthorizationID != "" {
			break
		}
		r, state, err := readAuthorizationTx(ctx, tx, scope.NodeID, c.id)
		if err != nil {
			return result, err
		}
		if _, ok := selected[r.Kind]; ok {
			continue
		}
		if initial != nil {
			base := map[string]AuthorizationRecord{"credit_grant": initial.grant, "runtime_lease": initial.runtime, "share_lease": initial.share}[r.Kind]
			if base.AuthorizationID == "" || !sameRuntimeFamily(r, base) {
				continue
			}
		}
		use := scope
		use.SessionID = ""
		use.RuntimeLeaseID = ""
		use.ShareLeaseID = ""
		use.AuthorizationID = r.AuthorizationID
		use.Kind = r.Kind
		if scope.BillingSnapshotID == "" {
			use.BillingSnapshotID = r.BillingSnapshotID
		}
		if _, err := checkReadAuthorizationTx(ctx, tx, use, r, state); err != nil {
			if errors.Is(err, ErrAuthorizationDenied) {
				continue
			}
			return result, err
		}
		if r.Kind == "credit_grant" {
			var remainingText string
			if err := tx.QueryRowContext(ctx, `SELECT remaining_bytes FROM authorization_balances WHERE node_id=? AND authorization_id=?`, scope.NodeID, r.AuthorizationID).Scan(&remainingText); err != nil {
				return result, err
			}
			remaining, err := strconv.ParseUint(remainingText, 10, 64)
			if err != nil {
				return result, err
			}
			if remaining < minimum {
				continue
			}
			result.available = remaining
		}
		selected[r.Kind] = r
	}
	var grantOK, runtimeOK, shareOK bool
	result.grant, grantOK = selected["credit_grant"]
	result.runtime, runtimeOK = selected["runtime_lease"]
	result.share, shareOK = selected["share_lease"]
	if !grantOK || !runtimeOK || !shareOK || result.share.Connections == 0 || result.share.RateBPS == 0 {
		return runtimeChoices{}, ErrAuthorizationDenied
	}
	if result.grant.IssuerID != result.runtime.IssuerID || result.grant.IssuerID != result.share.IssuerID {
		return runtimeChoices{}, ErrAuthorityHandoffRequired
	}
	return result, nil
}

// ReserveRuntimeUsage renews the live session's authority and pre-reserves
// bytes atomically. Its snapshot and initial ownership are immutable; changing
// the selected IDs never moves old usage holds or refunds their liabilities.
func (s *Store) ReserveRuntimeUsage(ctx context.Context, r UsageReservation, scope AuthorizationUse) (string, error) {
	return s.reserveRuntimeUsage(ctx, r, scope, false, nil)
}

// ReserveRuntimeFundingBlock reserves money, not permission to forward bytes.
// Its unresolved upper bound uses the existing old-boot unknown recovery path.
// Runtime write admission must not treat the returned grant ID as a capability.
// Block consumption/checkpoint integration is deliberately a separate API.
func (s *Store) ReserveRuntimeFundingBlock(ctx context.Context, r UsageReservation, scope AuthorizationUse) (string, error) {
	if s.owner != "agent" || r.Reserved > 4<<20 {
		return "", ErrAuthorizationDenied
	}
	return s.reserveRuntimeUsage(ctx, r, scope, true, nil)
}

// ReserveRuntimeFundingBlockUpTo selects at least minimum and at most the
// requested preferred size in the same transaction as the funding debit.
// The result is funding only; write admission remains mandatory. Exact retries
// retain their original bounds and allocated size, never allocate again.
func (s *Store) ReserveRuntimeFundingBlockUpTo(ctx context.Context, r UsageReservation, scope AuthorizationUse, minimum uint64) (UsageReservation, error) {
	return s.ReserveRuntimeFundingBlockReplacingIdle(ctx, r, scope, minimum, nil)
}

// ReserveRuntimeFundingBlockReplacingIdle atomically returns caller-proven idle
// same-session tails and reserves replacement funding. The exclusive allocator
// must join all writers before listing a tail. Failed allocation returns none.
func (s *Store) ReserveRuntimeFundingBlockReplacingIdle(ctx context.Context, r UsageReservation, scope AuthorizationUse, minimum uint64, idle []UsageReservation) (UsageReservation, error) {
	return s.reserveRuntimeFundingRange(ctx, r, scope, minimum, idle, false)
}

// ReserveAndAdmitRuntimeFundingBlock atomically funds a new block and consumes
// rate for its first minimum-byte write. Allocation identities cannot be reused
// for this operation: uncertain commit results must fault the caller.
func (s *Store) ReserveAndAdmitRuntimeFundingBlock(ctx context.Context, r UsageReservation, scope AuthorizationUse, minimum uint64, idle []UsageReservation) (UsageReservation, error) {
	return s.reserveRuntimeFundingRange(ctx, r, scope, minimum, idle, true)
}
func (s *Store) reserveRuntimeFundingRange(ctx context.Context, r UsageReservation, scope AuthorizationUse, minimum uint64, idle []UsageReservation, admit bool) (UsageReservation, error) {
	if len(idle) > 1 || s.owner != "agent" || minimum == 0 || minimum > r.Reserved || r.Reserved > 4<<20 {
		return UsageReservation{}, ErrAuthorizationDenied
	}
	encoded, _ := json.Marshal(idle)
	digest := fmt.Sprintf("%x", sha256.Sum256(encoded))
	bounds := &fundingBlockRange{minimum: minimum, preferred: r.Reserved, idle: idle, idleDigest: digest, admit: admit}
	id, err := s.reserveRuntimeUsage(ctx, r, scope, true, bounds)
	if err != nil {
		return UsageReservation{}, err
	}
	r.AuthorizationID = id
	r.Reserved = bounds.allocated
	return r, nil
}

// ReserveRuntimeUsageUpTo durably admits a positive fragment no larger than
// the requested write. The caller must collect the full write before granting
// Engine permission, and return all proven-unused fragments on admission failure.
// This is a one-shot identity: ambiguous errors require recovery, never replay.
func (s *Store) ReserveRuntimeUsageUpTo(ctx context.Context, r UsageReservation, scope AuthorizationUse) (UsageReservation, error) {
	if s.owner != "agent" || r.Reserved == 0 || r.Reserved > 4<<20 {
		return UsageReservation{}, ErrAuthorizationDenied
	}
	bounds := &fundingBlockRange{minimum: 1, preferred: r.Reserved}
	id, err := s.reserveRuntimeUsage(ctx, r, scope, false, bounds)
	if err != nil {
		return UsageReservation{}, err
	}
	r.AuthorizationID, r.Reserved = id, bounds.allocated
	return r, nil
}

type fundingBlockRange struct {
	minimum, preferred, allocated uint64
	idle                          []UsageReservation
	idleDigest                    string
	admit                         bool
}

func (s *Store) reserveRuntimeUsage(ctx context.Context, r UsageReservation, scope AuthorizationUse, fundingBlock bool, bounds *fundingBlockRange) (result string, resultErr error) {
	started := time.Now()
	stage := "validate"
	defer func() {
		if resultErr != nil && (errors.Is(resultErr, context.DeadlineExceeded) || errors.Is(resultErr, context.Canceled)) {
			resultErr = fmt.Errorf("store: runtime reservation stage=%s elapsed=%s: %w", stage, time.Since(started), resultErr)
		}
	}()
	if scope.SessionID == "" || r.NodeID != scope.NodeID || r.BootID != scope.BootID || r.SnapshotID != scope.BillingSnapshotID || r.RuleID != scope.RuleID || r.BatchID == "" || r.Reserved == 0 || (r.Direction != "egress" && r.Direction != "ingress") {
		return "", ErrAuthorizationDenied
	}
	if !scope.TrustedTime {
		return "", ErrAuthorityTimeUncertain
	}
	stage = "begin-transaction"
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return "", err
	}
	defer tx.Rollback()
	stage = "read-session"
	var boot, flow, source, state, grantID, runtimeID, shareID, firstGrant, firstRuntime, firstShare string
	if err := tx.QueryRowContext(ctx, `SELECT boot_id,flow_key,source_id,state,grant_id,runtime_lease_id,share_lease_id,initial_grant_id,initial_runtime_lease_id,initial_share_lease_id FROM runtime_sessions WHERE node_id=? AND session_id=?`, scope.NodeID, scope.SessionID).Scan(&boot, &flow, &source, &state, &grantID, &runtimeID, &shareID, &firstGrant, &firstRuntime, &firstShare); err != nil {
		return "", err
	}
	if state != "open" || boot != scope.BootID || flow != r.FlowKey {
		return "", ErrAuthorizationDenied
	}
	stage = "read-lineage"
	var initial runtimeChoices
	for _, entry := range []struct {
		id     string
		target *AuthorizationRecord
	}{{firstGrant, &initial.grant}, {firstRuntime, &initial.runtime}, {firstShare, &initial.share}} {
		*entry.target, _, err = readAuthorizationTx(ctx, tx, scope.NodeID, entry.id)
		if err != nil {
			return "", err
		}
	}
	if initial.grant.BillingSnapshotID != scope.BillingSnapshotID || initial.grant.RuleID != scope.RuleID || initial.grant.RuleRevision != scope.RuleRevision || initial.grant.PathGeneration != scope.PathGeneration || initial.share.SourceID != source {
		return "", ErrAuthorizationDenied
	}
	stage = "read-current-authority"
	for _, id := range []string{grantID, runtimeID, shareID} {
		_, state, err := readAuthorizationTx(ctx, tx, scope.NodeID, id)
		if err != nil {
			return "", err
		}
		if state != "active" {
			return "", ErrAuthorizationDenied
		}
	}
	// An outstanding retry keeps its original grant identity even when another
	// direction already renewed this session. It cannot debit or migrate twice.
	stage = "read-existing-hold"
	var heldID string
	var blockSession, blockDirection sql.NullString
	err = tx.QueryRowContext(ctx, `SELECT h.authorization_id,b.session_id,b.direction FROM authorization_usage_holds h LEFT JOIN runtime_funding_blocks b ON b.node_id=h.node_id AND b.batch_id=h.batch_id WHERE h.node_id=? AND h.batch_id=?`, scope.NodeID, r.BatchID).Scan(&heldID, &blockSession, &blockDirection)
	if err == nil {
		if bounds != nil && (bounds.admit || !fundingBlock) {
			return "", ErrUsageBatchConflict
		}
		// Allocation identities never serve as ordinary write admission.
		if blockSession.Valid != fundingBlock || fundingBlock && (blockSession.String != scope.SessionID || blockDirection.String != r.Direction) {
			return "", ErrUsageBatchConflict
		}
		if fundingBlock {
			var minimum, preferred, allocated, idleDigest string
			requestErr := tx.QueryRowContext(ctx, `SELECT minimum_bytes,preferred_bytes,allocated_bytes,idle_digest FROM runtime_funding_requests WHERE node_id=? AND batch_id=?`, r.NodeID, r.BatchID).Scan(&minimum, &preferred, &allocated, &idleDigest)
			if bounds == nil {
				if requestErr == nil {
					return "", ErrUsageBatchConflict
				}
				if !errors.Is(requestErr, sql.ErrNoRows) {
					return "", requestErr
				}
			} else {
				if requestErr != nil {
					if errors.Is(requestErr, sql.ErrNoRows) {
						return "", ErrUsageBatchConflict
					}
					return "", requestErr
				}
				if minimum != strconv.FormatUint(bounds.minimum, 10) || preferred != strconv.FormatUint(bounds.preferred, 10) || idleDigest != bounds.idleDigest {
					return "", ErrUsageBatchConflict
				}
				amount, err := strconv.ParseUint(allocated, 10, 64)
				if err != nil || amount < bounds.minimum || amount > bounds.preferred {
					return "", ErrUsageBatchConflict
				}
				r.Reserved = amount
				bounds.allocated = amount
			}
		}
		held, _, err := readAuthorizationTx(ctx, tx, scope.NodeID, heldID)
		if err != nil {
			return "", err
		}
		if !sameRuntimeFamily(held, initial.grant) {
			return "", ErrAuthorityHandoffRequired
		}
		r.AuthorizationID = heldID
		scope.AuthorizationID = heldID
		scope.Kind = "credit_grant"
		scope.RuntimeLeaseID = runtimeID
		scope.ShareLeaseID = shareID
		scope.ExistingSession = true
		scope.SessionID = ""
		stage = "reserve-usage"
		id, err := reserveAuthorizedUsageWithRateTx(ctx, tx, r, scope, !fundingBlock)
		if err != nil {
			return "", err
		}
		stage = "commit"
		if err := tx.Commit(); err != nil {
			return "", err
		}
		return id, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return "", err
	}
	if bounds != nil {
		for _, tail := range bounds.idle {
			if tail.NodeID != r.NodeID || tail.BootID != r.BootID || tail.FlowKey != r.FlowKey || tail.RuleID != r.RuleID || tail.SnapshotID != r.SnapshotID || tail.Direction == r.Direction || tail.BatchID == r.BatchID {
				return "", ErrUsageBatchConflict
			}
			var session string
			if err := tx.QueryRowContext(ctx, `SELECT session_id FROM runtime_funding_blocks WHERE node_id=? AND batch_id=?`, tail.NodeID, tail.BatchID).Scan(&session); err != nil {
				return "", err
			}
			if session != scope.SessionID {
				return "", ErrUsageBatchConflict
			}
			if err := settleUsageReservationTx(ctx, tx, tail, "returned", 0, nil, 0, nil); err != nil {
				return "", err
			}
		}
	}
	if fundingBlock {
		// One unresolved block per direction, plus a per-node durable cap.
		// Resolved rows remain as immutable replay evidence, not live capacity.
		var live, sameDirection int
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*),COALESCE(SUM(CASE WHEN b.session_id=? AND b.direction=? THEN 1 ELSE 0 END),0) FROM runtime_funding_blocks b JOIN usage_reservations u ON u.node_id=b.node_id AND u.batch_id=b.batch_id WHERE b.node_id=? AND u.state='reserved'`, scope.SessionID, r.Direction, r.NodeID).Scan(&live, &sameDirection); err != nil {
			return "", err
		}
		if live >= 4096 || sameDirection != 0 {
			return "", ErrAuthorizationDenied
		}
	}
	scope.ExistingSession = true
	stage = "select-authority"
	minimum := r.Reserved
	if bounds != nil {
		minimum = bounds.minimum
	}
	chosen, err := selectRuntimeAuthorityTx(ctx, tx, scope, minimum, &initial)
	if err != nil {
		return "", err
	}
	if bounds != nil {
		r.Reserved = bounds.preferred
		if chosen.available < r.Reserved {
			r.Reserved = chosen.available
		}
		bounds.allocated = r.Reserved
	}
	stage = "check-session-limit"
	var count uint64
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM runtime_sessions WHERE node_id=? AND source_id=? AND state='open'`, scope.NodeID, source).Scan(&count); err != nil {
		return "", err
	}
	if count > chosen.share.Connections {
		return "", ErrAuthorizationDenied
	}
	stage = "update-session"
	if _, err := tx.ExecContext(ctx, `UPDATE runtime_sessions SET grant_id=?,runtime_lease_id=?,share_lease_id=? WHERE node_id=? AND session_id=? AND state='open'`, chosen.grant.AuthorizationID, chosen.runtime.AuthorizationID, chosen.share.AuthorizationID, scope.NodeID, scope.SessionID); err != nil {
		return "", err
	}
	scope.AuthorizationID = chosen.grant.AuthorizationID
	scope.Kind = "credit_grant"
	scope.RuntimeLeaseID = chosen.runtime.AuthorizationID
	scope.ShareLeaseID = chosen.share.AuthorizationID
	r.AuthorizationID = chosen.grant.AuthorizationID
	stage = "reserve-usage"
	id, err := reserveAuthorizedUsageWithRateTx(ctx, tx, r, scope, !fundingBlock)
	if err != nil {
		return "", err
	}
	if bounds != nil && bounds.admit {
		stage = "first-write-rate"
		if err := consumeRuntimeRate(ctx, tx, scope, bounds.minimum); err != nil {
			return "", err
		}
	}
	if fundingBlock {
		if _, err := tx.ExecContext(ctx, `INSERT INTO runtime_funding_blocks(node_id,batch_id,session_id,direction) VALUES(?,?,?,?)`, r.NodeID, r.BatchID, scope.SessionID, r.Direction); err != nil {
			return "", err
		}
		if bounds != nil {
			if _, err := tx.ExecContext(ctx, `INSERT INTO runtime_funding_requests(node_id,batch_id,minimum_bytes,preferred_bytes,allocated_bytes,idle_digest) VALUES(?,?,?,?,?,?)`, r.NodeID, r.BatchID, strconv.FormatUint(bounds.minimum, 10), strconv.FormatUint(bounds.preferred, 10), strconv.FormatUint(r.Reserved, 10), bounds.idleDigest); err != nil {
				return "", err
			}
		}
	}
	stage = "commit"
	if err := tx.Commit(); err != nil {
		return "", err
	}
	return id, nil
}
