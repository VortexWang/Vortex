package store

import (
	"context"
	"errors"
	"math"
	"strconv"
	"time"

	controlv1 "vortex/contracts/protobuf/v1"
)

type RenewalCandidate struct {
	GrantID string
	Request *controlv1.RenewalRequest
}

// DueRenewals observes funded, current-boot authority for an installed Engine
// rule. It never spends, releases, or treats a wall-clock reading as trust.
// The result needs an ID/digest and a durable trigger before transport.
func (s *Store) DueRenewals(ctx context.Context, scope AuthorizationUse, issuer string, percent int) ([]RenewalCandidate, error) {
	if s.owner != "agent" || !validIssuerUUID(issuer) || percent < 1 || percent > 100 {
		return nil, ErrAuthorizationDenied
	}
	if !scope.TrustedTime || scope.Earliest.IsZero() || scope.Latest.Before(scope.Earliest) {
		return nil, ErrAuthorityTimeUncertain
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	rows, err := tx.QueryContext(ctx, `SELECT authorization_id FROM authorization_records WHERE node_id=? AND boot_id=? AND issuer_id=? AND kind='credit_grant' AND state='active' AND rule_id=? AND rule_revision=? AND path_generation=? AND share_reference_id<>'' LIMIT 4097`, scope.NodeID, scope.BootID, issuer, scope.RuleID, strconv.FormatUint(scope.RuleRevision, 10), strconv.FormatUint(scope.PathGeneration, 10))
	if err != nil {
		return nil, err
	}
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return nil, err
		}
		ids = append(ids, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	if len(ids) > 4096 {
		return nil, ErrAuthorizationDenied
	}
	type lineage struct {
		grant           AuthorizationRecord
		epoch           uint64
		issued, expires time.Time
		remaining       uint64
	}
	groups := map[string]*lineage{}
	for _, id := range ids {
		a, _, err := readAuthorizationTx(ctx, tx, scope.NodeID, id)
		if err != nil {
			return nil, err
		}
		use := scope
		use.AuthorizationID = id
		use.Kind = "credit_grant"
		// Retained old revisions are evidence for a trigger, not new admission.
		// The helper below independently requires the current path and exact
		// funded source handoff/root chain.
		use.ExistingSession = true
		use.BillingSnapshotID = a.BillingSnapshotID
		if _, err := checkAuthorizationTx(ctx, tx, use); err != nil {
			if errors.Is(err, ErrAuthorizationDenied) || errors.Is(err, ErrAuthorizationGenerationStale) {
				continue
			}
			return nil, err
		}
		epoch, err := renewalTriggerEpochTx(ctx, tx, scope.NodeID, a)
		if err != nil {
			if errors.Is(err, ErrAuthorizationDenied) || errors.Is(err, ErrAuthorizationGenerationStale) || errors.Is(err, ErrAuthorityHandoffRequired) {
				continue
			}
			return nil, err
		}
		var amount string
		if err := tx.QueryRowContext(ctx, `SELECT remaining_bytes FROM authorization_balances WHERE node_id=? AND authorization_id=?`, scope.NodeID, id).Scan(&amount); err != nil {
			return nil, err
		}
		remaining, err := strconv.ParseUint(amount, 10, 63)
		if err != nil {
			return nil, err
		}
		issued, e1 := time.Parse(time.RFC3339Nano, a.IssuedAt)
		expires, e2 := time.Parse(time.RFC3339Nano, a.ExpiresAt)
		if e1 != nil || e2 != nil {
			return nil, ErrAuthorizationDenied
		}
		key := a.DelegationID + "/" + a.SourceID + "/" + a.BillingSnapshotID
		g := groups[key]
		if g == nil {
			g = &lineage{}
			groups[key] = g
		}
		if remaining > math.MaxInt64-g.remaining {
			return nil, ErrAuthorizationDenied
		}
		g.remaining += remaining
		if g.grant.AuthorizationID == "" || issued.After(g.issued) || issued.Equal(g.issued) && id > g.grant.AuthorizationID {
			g.grant = a
			g.epoch = epoch
			g.issued = issued
			g.expires = expires
		}
	}
	var due []RenewalCandidate
	for _, g := range groups {
		a := g.grant
		var existing int
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM renewal_requests WHERE node_id=? AND (trigger_authorization_id=? OR (delegation_id=? AND source_id=? AND rule_id=? AND snapshot_id=? AND state IN ('pending','received')))`, scope.NodeID, a.AuthorizationID, a.DelegationID, a.SourceID, a.RuleID, a.BillingSnapshotID).Scan(&existing); err != nil {
			return nil, err
		}
		if existing > 0 {
			continue
		}
		ttl := g.expires.Sub(g.issued)
		threshold := ttl / 3
		if threshold < 5*time.Second {
			threshold = 5 * time.Second
		}
		low := a.AmountBytes/100*uint64(percent) + a.AmountBytes%100*uint64(percent)/100
		if g.remaining > low && g.expires.Sub(scope.Latest) > threshold {
			continue
		}
		seconds := uint64(ttl / time.Second)
		if seconds < 1 || seconds > 300 {
			continue
		}
		var fenceText string
		if err := tx.QueryRowContext(ctx, `SELECT value FROM agent_state WHERE key='authorization_fencing_floor'`).Scan(&fenceText); err != nil {
			return nil, err
		}
		fence, err := strconv.ParseUint(fenceText, 10, 63)
		if err != nil || fence == 0 {
			return nil, ErrAuthorizationDenied
		}
		if scope.FencingGeneration != 0 && scope.FencingGeneration != fence {
			continue
		}
		due = append(due, RenewalCandidate{GrantID: a.AuthorizationID, Request: &controlv1.RenewalRequest{NodeId: scope.NodeID, BootId: scope.BootID, DelegationId: a.DelegationID, SourceId: a.SourceID, RuleId: a.RuleID, RuleRevision: a.RuleRevision, PathGeneration: a.PathGeneration, FencingGeneration: fence, SignerEpoch: g.epoch, BillingSnapshotId: a.BillingSnapshotID, RequestedRawBytes: a.AmountBytes, RequestedTtlSeconds: seconds}})
		if len(due) > 128 {
			return nil, ErrAuthorizationDenied
		}
	}
	return due, tx.Commit()
}
