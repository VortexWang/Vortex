package store

import (
	"context"
	"database/sql"
	"errors"
	"sort"
	"strconv"
	"time"
)

type runtimeChoices struct{ grant, runtime, share AuthorizationRecord }

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
	if initial == nil {
		selected, err := policySnapshotTx(ctx, tx, scope)
		if err != nil {
			return result, err
		}
		if selected != "" {
			if scope.BillingSnapshotID != "" && scope.BillingSnapshotID != selected {
				return result, ErrAuthorizationDenied
			}
			scope.BillingSnapshotID = selected
		}
	}
	policyIDs := map[string]string{}
	if initial == nil {
		var err error
		policyIDs, err = policyAuthorityIDsTx(ctx, tx, scope)
		if err != nil {
			return result, err
		}
	}
	// The coarse SQL filter only bounds history scanned; exact conservative
	// expiry checks still run below. A seconds-level equality is never authority.
	rows, err := tx.QueryContext(ctx, `SELECT authorization_id FROM authorization_records WHERE node_id=? AND boot_id=? AND rule_id=? AND rule_revision=? AND path_generation=? AND state='active' AND unixepoch(expires_at)>=unixepoch(?)`, scope.NodeID, scope.BootID, scope.RuleID, strconv.FormatUint(scope.RuleRevision, 10), strconv.FormatUint(scope.PathGeneration, 10), scope.Latest.UTC().Format(time.RFC3339Nano))
	if err != nil {
		return result, err
	}
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return result, err
		}
		ids = append(ids, id)
		if len(ids) > 4096 {
			rows.Close()
			return result, ErrAuthorizationDenied
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return result, err
	}
	type candidate struct {
		record AuthorizationRecord
		issued time.Time
	}
	var candidates []candidate
	for _, id := range ids {
		r, _, err := readAuthorizationTx(ctx, tx, scope.NodeID, id)
		if err != nil {
			return result, err
		}
		at, err := time.Parse(time.RFC3339Nano, r.IssuedAt)
		if err != nil {
			return result, ErrAuthorizationDenied
		}
		candidates = append(candidates, candidate{r, at})
	}
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].issued.Equal(candidates[j].issued) {
			return candidates[i].record.AuthorizationID > candidates[j].record.AuthorizationID
		}
		return candidates[i].issued.After(candidates[j].issued)
	})
	selected := map[string]AuthorizationRecord{}
	for _, c := range candidates {
		r := c.record
		if expected := policyIDs[r.Kind]; expected != "" && r.AuthorizationID != expected {
			base, _, err := readAuthorizationTx(ctx, tx, scope.NodeID, expected)
			if err != nil {
				return result, err
			}
			compatible, err := sameRuntimeFamilyTx(ctx, tx, scope.NodeID, r, base)
			if err != nil {
				return result, err
			}
			if !compatible {
				continue
			}
		}
		if _, ok := selected[r.Kind]; ok {
			continue
		}
		if initial != nil {
			base := map[string]AuthorizationRecord{"credit_grant": initial.grant, "runtime_lease": initial.runtime, "share_lease": initial.share}[r.Kind]
			compatible, err := sameRuntimeFamilyTx(ctx, tx, scope.NodeID, r, base)
			if err != nil {
				return result, err
			}
			if base.AuthorizationID == "" || !compatible {
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
		if _, err := checkAuthorizationTx(ctx, tx, use); err != nil {
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
	if scope.SessionID == "" || r.NodeID != scope.NodeID || r.BootID != scope.BootID || r.SnapshotID != scope.BillingSnapshotID || r.RuleID != scope.RuleID || r.BatchID == "" || r.Reserved == 0 || (r.Direction != "egress" && r.Direction != "ingress") {
		return "", ErrAuthorizationDenied
	}
	if !scope.TrustedTime {
		return "", ErrAuthorityTimeUncertain
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return "", err
	}
	defer tx.Rollback()
	var boot, flow, source, state, grantID, runtimeID, shareID, firstGrant, firstRuntime, firstShare string
	if err := tx.QueryRowContext(ctx, `SELECT boot_id,flow_key,source_id,state,grant_id,runtime_lease_id,share_lease_id,initial_grant_id,initial_runtime_lease_id,initial_share_lease_id FROM runtime_sessions WHERE node_id=? AND session_id=?`, scope.NodeID, scope.SessionID).Scan(&boot, &flow, &source, &state, &grantID, &runtimeID, &shareID, &firstGrant, &firstRuntime, &firstShare); err != nil {
		return "", err
	}
	if state != "open" || boot != scope.BootID || flow != r.FlowKey {
		return "", ErrAuthorizationDenied
	}
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
	var heldID string
	err = tx.QueryRowContext(ctx, `SELECT authorization_id FROM authorization_usage_holds WHERE node_id=? AND batch_id=?`, scope.NodeID, r.BatchID).Scan(&heldID)
	if err == nil {
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
		id, err := reserveAuthorizedUsageTx(ctx, tx, r, scope)
		if err != nil {
			return "", err
		}
		if err := tx.Commit(); err != nil {
			return "", err
		}
		return id, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return "", err
	}
	scope.ExistingSession = true
	chosen, err := selectRuntimeAuthorityTx(ctx, tx, scope, r.Reserved, &initial)
	if err != nil {
		return "", err
	}
	var count uint64
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM runtime_sessions WHERE node_id=? AND source_id=? AND state='open'`, scope.NodeID, source).Scan(&count); err != nil {
		return "", err
	}
	if count > chosen.share.Connections {
		return "", ErrAuthorizationDenied
	}
	if _, err := tx.ExecContext(ctx, `UPDATE runtime_sessions SET grant_id=?,runtime_lease_id=?,share_lease_id=? WHERE node_id=? AND session_id=? AND state='open'`, chosen.grant.AuthorizationID, chosen.runtime.AuthorizationID, chosen.share.AuthorizationID, scope.NodeID, scope.SessionID); err != nil {
		return "", err
	}
	scope.AuthorizationID = chosen.grant.AuthorizationID
	scope.Kind = "credit_grant"
	scope.RuntimeLeaseID = chosen.runtime.AuthorizationID
	scope.ShareLeaseID = chosen.share.AuthorizationID
	r.AuthorizationID = chosen.grant.AuthorizationID
	id, err := reserveAuthorizedUsageTx(ctx, tx, r, scope)
	if err != nil {
		return "", err
	}
	if err := tx.Commit(); err != nil {
		return "", err
	}
	return id, nil
}

// A changed signer is eligible only with already committed parent handoff
// evidence. Ordinary renewal retains the original strict family checks.
func sameRuntimeFamilyTx(ctx context.Context, tx *sql.Tx, node string, next, initial AuthorizationRecord) (bool, error) {
	if sameRuntimeFamily(next, initial) {
		return true, nil
	}
	if next.SignerEpoch <= initial.SignerEpoch {
		return false, nil
	}
	candidate := next
	candidate.SignerEpoch = initial.SignerEpoch
	if !sameRuntimeFamily(candidate, initial) {
		return false, nil
	}
	rows, err := tx.QueryContext(ctx, `SELECT DISTINCT o.source_id,a.signer_epoch FROM authorization_source_owners o JOIN authorization_records a ON a.node_id=o.node_id AND a.authorization_id=o.authorization_id WHERE o.node_id=? AND a.parent_authorization_id=? AND a.issuer_id=? AND a.boot_id=?`, node, initial.ParentAuthorizationID, initial.IssuerID, initial.BootID)
	if err != nil {
		return false, err
	}
	type root struct {
		source string
		epoch  uint64
	}
	var roots []root
	for rows.Next() {
		var source, epoch string
		if err = rows.Scan(&source, &epoch); err != nil {
			rows.Close()
			return false, err
		}
		n, err := strconv.ParseUint(epoch, 10, 64)
		if err != nil {
			rows.Close()
			return false, err
		}
		roots = append(roots, root{source, n})
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return false, err
	}
	for _, r := range roots {
		latest, err := sourceEpochTx(ctx, tx, node, r.source, r.epoch)
		if err != nil {
			return false, err
		}
		if initial.SignerEpoch >= r.epoch && next.SignerEpoch <= latest {
			return true, nil
		}
	}
	return false, nil
}
