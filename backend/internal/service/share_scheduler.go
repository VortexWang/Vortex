package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"vortex/backend/db/generated"
)

// ScheduleShares consumes the current immutable minute window. Each mutation
// uses the existing audited PG capacity transaction; no network I/O happens
// here. A crash between kinds or before QueueHandoff resumes the same pending
// generation. Signer epochs must already have explicit platform approval.
func (s ShareService) ScheduleShares(ctx context.Context, scope TenantScope, request string) (AllocationWindow, error) {
	w, e := s.AllocateWindow(ctx, scope, request)
	if e != nil {
		return AllocationWindow{}, e
	}
	var failures []error
	for _, n := range w.Nodes {
		if ctx.Err() != nil {
			return w, ctx.Err()
		}
		if n.Queued || len(n.References) == 0 {
			continue // a queue target is not executable capacity
		}
		for _, kind := range []string{"rate", "connections"} {
			if e := s.scheduleShareKind(ctx, scope, w.WindowAt, n, kind); e != nil {
				failures = append(failures, fmt.Errorf("node %s %s: %w", n.NodeID, kind, e))
			}
		}
	}
	return w, errors.Join(failures...)
}

func (s ShareService) scheduleShareKind(ctx context.Context, scope TenantScope, window time.Time, n NodeAllocation, kind string) error {
	q := generated.New(s.Pool)
	chain, e := q.ReadShareChain(ctx, generated.ReadShareChainParams{TenantID: scope.TenantID(), NodeID: n.NodeID, Kind: kind})
	if e != nil && !errors.Is(e, pgx.ErrNoRows) {
		return e
	}
	if e == nil {
		if chain.BootID != n.BootID {
			return ErrShareUnknown
		}
		if chain.PendingGeneration > 0 {
			g, e := q.GetShareGeneration(ctx, generated.GetShareGenerationParams{TenantID: scope.TenantID(), NodeID: n.NodeID, Kind: kind, Generation: chain.PendingGeneration})
			if e != nil {
				return e
			}
			// This never manufactures predecessor closure. QueueHandoff itself
			// requires every real signer proof before committing a CAS task.
			return s.QueueHandoff(ctx, scope, g.ID, uuid.NewString())
		}
	}
	hub, e := q.UsageNodeHub(ctx, n.NodeID)
	if e != nil {
		return e
	}
	ref := n.References[0] // stable rule/digest ordering from PreparedShareArtifacts
	a, e := q.GetSignerAuthority(ctx, generated.GetSignerAuthorityParams{TenantID: scope.TenantID(), SignerID: hub, SignerEpoch: ref.SignerEpoch})
	if e != nil {
		return e
	}
	until, e := q.SnapshotEffectiveUntil(ctx, generated.SnapshotEffectiveUntilParams{TenantID: scope.TenantID(), ID: ref.SnapshotID})
	if e != nil {
		return e
	}
	sub, e := q.GetSubscription(ctx, scope.TenantID())
	if e != nil {
		return e
	}
	policyUntil := minScheduleBoundary(window.Add(12*time.Hour), a.AutonomyUntil, until, sub.ExpiresAt)
	revision, e := q.GetRuleRevision(ctx, generated.GetRuleRevisionParams{TenantID: scope.TenantID(), RuleID: ref.RuleID, Revision: ref.RuleRevision})
	if e != nil {
		return e
	}
	var rule RuleInput
	if e = json.Unmarshal(revision.Payload, &rule); e != nil {
		return e
	}
	resolved, e := resolveRule(ctx, q, scope, rule)
	if e != nil {
		return e
	}
	for _, grant := range resolved.grants {
		policyUntil = minScheduleBoundary(policyUntil, grant.ValidUntil)
	}
	amount := n.RateBPS
	burst := amount / 8
	if kind == "connections" {
		amount, burst = n.Connections, 0
	}
	if chain.HeadGeneration > 0 {
		g, e := q.GetShareGeneration(ctx, generated.GetShareGenerationParams{TenantID: scope.TenantID(), NodeID: n.NodeID, Kind: kind, Generation: chain.HeadGeneration})
		if e != nil {
			return e
		}
		_, now, e := s.bounds()
		if e != nil {
			return e
		}
		if g.Amount == amount && g.BurstBytes == burst && g.SignerID == hub && g.SignerEpoch == ref.SignerEpoch && g.PathGeneration == ref.PathGeneration && g.FencingGeneration == ref.FencingGeneration && g.BillingSnapshotID == ref.SnapshotID && now.Before(g.OwnershipUntil.Time) && now.Before(g.PolicyUntil.Time) && now.Before(g.AutonomyUntil.Time) {
			return nil // an unchanged target must not mint a new generation each tick
		}
	}
	p, e := s.PrepareShare(ctx, scope, PrepareShareInput{NodeID: n.NodeID, BootID: n.BootID, Kind: kind, SignerID: hub, SnapshotID: ref.SnapshotID,
		Nonce: fmt.Sprintf("schedule/%s/%s/%d", n.NodeID, kind, window.Unix()), ExpectedGeneration: chain.HeadGeneration,
		SignerEpoch: ref.SignerEpoch, PathGeneration: ref.PathGeneration, FencingGeneration: ref.FencingGeneration,
		Amount: amount, BurstBytes: burst, OwnershipUntil: policyUntil, PolicyUntil: policyUntil}, uuid.NewString())
	if e != nil {
		return e
	}
	return s.QueueHandoff(ctx, scope, p.Generation.ID, uuid.NewString())
}

func minScheduleBoundary(until time.Time, bounds ...pgtype.Timestamptz) time.Time {
	for _, b := range bounds {
		if b.Valid && b.InfinityModifier == pgtype.Finite && b.Time.Before(until) {
			until = b.Time
		}
	}
	return until.UTC().Truncate(time.Microsecond)
}
