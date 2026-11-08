package service

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"strconv"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"vortex/backend/db/generated"
)

// FundRuntimeCandidates creates an initial finite delegation only after both
// Node aggregate kinds are confirmed. Existing allocations, including expired,
// unacknowledged or exhausted ones, suppress automatic refill. Their funds
// remain held until the separate delegation/child closure protocol proves
// release. A new minute or reconnect is never a reason to reserve more credit.
func (s ShareService) FundRuntimeCandidates(ctx context.Context, scope TenantScope, region, request string) error {
	if region == "" {
		return ErrShareConflict
	}
	w, e := s.AllocateWindow(ctx, scope, request)
	if e != nil {
		return e
	}
	var failures []error
	for _, n := range w.Nodes {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if n.Queued {
			continue
		}
		for _, ref := range n.References {
			if e = s.fundRuntimeCandidate(ctx, scope, region, n, ref); e != nil {
				failures = append(failures, fmt.Errorf("node %s snapshot %s: %w", n.NodeID, ref.SnapshotID, e))
			}
		}
	}
	return errors.Join(failures...)
}

func (s ShareService) fundRuntimeCandidate(ctx context.Context, scope TenantScope, region string, n NodeAllocation, ref ShareCandidateReference) error {
	q := generated.New(s.Pool)
	// Also recognize allocations prepared through the explicit administrative
	// producer, rather than reserving a second parent under a scheduler nonce.
	exists, e := q.RuntimeAllocationExists(ctx, generated.RuntimeAllocationExistsParams{TenantID: scope.TenantID(), NodeID: n.NodeID, BootID: n.BootID,
		SnapshotID: ref.SnapshotID, SignerEpoch: strconv.FormatInt(ref.SignerEpoch, 10), PathGeneration: strconv.FormatInt(ref.PathGeneration, 10), FencingFloor: strconv.FormatInt(ref.FencingGeneration, 10)})
	if e != nil || exists {
		return e
	}
	_, now, e := s.bounds()
	if e != nil {
		return e
	}
	hub, e := q.UsageNodeHub(ctx, n.NodeID)
	if e != nil {
		return e
	}
	incarnation, e := q.GetHubIncarnation(ctx, generated.GetHubIncarnationParams{HubID: hub, SignerEpoch: ref.SignerEpoch})
	if errors.Is(e, pgx.ErrNoRows) {
		return ErrShareUnknown
	}
	if e != nil {
		return e
	}
	boundary, e := q.HubSignerBoundary(ctx, hub)
	if errors.Is(e, pgx.ErrNoRows) {
		return ErrShareUnknown
	}
	if e != nil {
		return e
	}
	if boundary.SignerEpoch != ref.SignerEpoch {
		return ErrShareUnknown
	}
	signer, e := q.GetSignerAuthority(ctx, generated.GetSignerAuthorityParams{TenantID: scope.TenantID(), SignerID: hub, SignerEpoch: ref.SignerEpoch})
	if errors.Is(e, pgx.ErrNoRows) {
		return ErrShareUnknown
	}
	if e != nil {
		return e
	}
	until := minScheduleBoundary(signer.AutonomyUntil.Time, boundary.AutonomyUntil)
	for _, kind := range []string{"rate", "connections"} {
		c, e := q.ReadShareChain(ctx, generated.ReadShareChainParams{TenantID: scope.TenantID(), NodeID: n.NodeID, Kind: kind})
		if e != nil {
			if errors.Is(e, pgx.ErrNoRows) {
				return ErrShareUnknown
			}
			return e
		}
		if c.BootID != n.BootID || c.PendingGeneration != 0 || c.ActiveGeneration < 1 {
			return ErrShareUnknown
		}
		g, e := q.GetShareGeneration(ctx, generated.GetShareGenerationParams{TenantID: scope.TenantID(), NodeID: n.NodeID, Kind: kind, Generation: c.ActiveGeneration})
		if e != nil {
			return e
		}
		until = minScheduleBoundary(until, g.OwnershipUntil, g.PolicyUntil, g.AutonomyUntil)
	}
	policy, e := q.SnapshotEffectiveUntil(ctx, generated.SnapshotEffectiveUntilParams{TenantID: scope.TenantID(), ID: ref.SnapshotID})
	if e != nil {
		return e
	}
	until = minScheduleBoundary(until, policy)
	billing, e := q.GetBillingSnapshot(ctx, generated.GetBillingSnapshotParams{TenantID: scope.TenantID(), ID: ref.SnapshotID})
	if e != nil {
		return e
	}
	if billing.MultiplierMilli < 1 {
		return ErrSelfHostedExempt
	}
	sources, e := q.ListTrafficSources(ctx, scope.TenantID())
	if e != nil {
		return e
	}
	// Base then independent package, by expiry and issue order, as in the
	// authoritative source query. Never borrow balance across source accounts.
	for _, src := range sources {
		if src.SnapshotID != billing.PackageSnapshotID {
			continue
		}
		source, e := q.MeteringSource(ctx, generated.MeteringSourceParams{TenantID: scope.TenantID(), ID: src.ID, AtTime: timestamp(now)})
		if e != nil {
			return e
		}
		deadline := minScheduleBoundary(until, source.EffectiveUntil)
		if source.Expired || source.IssuedAt.Time.After(now) || !now.Before(deadline) {
			continue
		}
		balance, e := q.GetSourceProjection(ctx, generated.GetSourceProjectionParams{TenantID: scope.TenantID(), SourceID: src.ID})
		if errors.Is(e, pgx.ErrNoRows) {
			continue // ledger lifecycle worker has not initialized this source
		}
		if e != nil {
			return e
		}
		if balance.B <= balance.F {
			continue
		}
		raw := new(big.Int).Mul(big.NewInt(balance.B-balance.F), big.NewInt(1000))
		raw.Div(raw, big.NewInt(billing.MultiplierMilli))
		rawBytes := int64(1 << 20)
		if raw.Cmp(big.NewInt(rawBytes)) < 0 {
			rawBytes = raw.Int64()
		}
		if rawBytes < 1 {
			continue
		}
		key := uuid.NewSHA1(uuid.NameSpaceOID, []byte(fmt.Sprintf("runtime-bootstrap/%s/%s/%s/%s/%d/%d/%d", scope.TenantID(), n.NodeID, n.BootID, ref.SnapshotID, ref.SignerEpoch, ref.PathGeneration, ref.FencingGeneration))).String()
		_, e = s.PrepareHubAllocation(ctx, scope, HubFundingInput{NodeID: n.NodeID, BootID: n.BootID, HubID: hub, HubBootID: incarnation.BootID, Region: region,
			SourceID: src.ID, SnapshotID: ref.SnapshotID, Nonce: key, SignerEpoch: ref.SignerEpoch, PathGeneration: ref.PathGeneration, FencingFloor: ref.FencingGeneration,
			RawBytes: rawBytes, ExpiresAt: deadline}, uuid.NewString())
		return e
	}
	return ErrShareCapacity
}
