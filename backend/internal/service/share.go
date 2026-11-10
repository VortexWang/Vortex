package service

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/x509"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"vortex/backend/db/generated"
	"vortex/backend/internal/policy"
	"vortex/backend/internal/repository"
)

var (
	ErrShareConflict    = errors.New("share_revision_conflict")
	ErrShareCapacity    = errors.New("share_capacity_unavailable")
	ErrShareUnknown     = errors.New("share_execution_unknown_occupation_retained")
	ErrLeaseBoundary    = errors.New("lease_boundary_invalid")
	ErrSelfHostedExempt = errors.New("self_hosted_path_exempt_from_platform_shares")
)

// Clock reports trusted UTC bounds supplied by deployment time authority.
// Missing or uncertain time denies new authority; wall time alone is no proof.
type ShareService struct {
	Pool             *pgxpool.Pool
	NodeCA, SignerCA *x509.Certificate
	Clock            func() (earliest, latest time.Time, trusted bool)
}

func (s ShareService) bounds() (time.Time, time.Time, error) {
	if s.Clock == nil {
		return time.Time{}, time.Time{}, ErrLeaseBoundary
	}
	lo, hi, trusted := s.Clock()
	if !trusted || lo.IsZero() || hi.Before(lo) || hi.Sub(lo) > 10*time.Second {
		return time.Time{}, time.Time{}, ErrLeaseBoundary
	}
	return lo.UTC().Truncate(time.Microsecond), hi.UTC().Truncate(time.Microsecond), nil
}

type PrepareShareInput struct {
	NodeID, BootID, Kind, SignerID, SnapshotID, Nonce                  string
	ExpectedGeneration, SignerEpoch, PathGeneration, FencingGeneration int64
	Amount, BurstBytes                                                 int64
	OwnershipUntil, PolicyUntil                                        time.Time
}
type PreparedShare struct {
	Generation  generated.ShareGeneration `json:"generation"`
	OperationID string                    `json:"operation_id"`
	State       string                    `json:"state"`
}

// AuthorizeSigner installs an explicit immutable platform decision. A tenant,
// new boot or restarted Hub cannot invent an epoch or reset its autonomy clock.
// This authority contains no traffic credit; a funded delegation is separate.
func (s ShareService) AuthorizeSigner(ctx context.Context, scope TenantScope, signer string, epoch int64, heartbeat, deadline time.Time, proof []byte, request string) (generated.SignerAuthority, error) {
	var out generated.SignerAuthority
	earliest, now, e := s.bounds()
	if e != nil {
		return out, e
	}
	if !scope.Platform() || !scope.Allows(policy.Grant) {
		return out, ErrScope
	}
	if _, e := uuid.Parse(signer); e != nil {
		return out, ErrShareConflict
	}
	if epoch < 1 || len(proof) != 32 || heartbeat.After(earliest) || now.Sub(heartbeat) > 10*time.Second || !deadline.After(now) || deadline.After(heartbeat.Add(12*time.Hour)) {
		return out, ErrLeaseBoundary
	}
	e = (repository.AuditWriter{Pool: s.Pool}).WithTransaction(ctx, scope, policy.Grant, &repository.Audit{Action: "signer.authorize", ResourceID: signer, RequestID: request, Reason: "explicit immutable signer epoch and independently bounded autonomy"}, func(q *generated.Queries) error {
		if e := q.LockUsageTuple(ctx, "hub-incarnation/"+signer); e != nil {
			return e
		}
		if _, e := q.LockTenant(ctx, scope.TenantID()); e != nil {
			return e
		}
		old, e := q.GetSignerAuthority(ctx, generated.GetSignerAuthorityParams{TenantID: scope.TenantID(), SignerID: signer, SignerEpoch: epoch})
		if e == nil {
			if !old.HeartbeatAt.Time.Equal(heartbeat) || !old.AutonomyUntil.Time.Equal(deadline) || !bytes.Equal(old.ApprovalEvidence, proof) {
				return ErrShareConflict
			}
			out = old
			return nil
		}
		if !errors.Is(e, pgx.ErrNoRows) {
			return e
		}
		incarnation, e := q.LatestHubIncarnation(ctx, signer)
		if e == nil && epoch < incarnation.SignerEpoch {
			return ErrShareConflict
		}
		if e != nil && !errors.Is(e, pgx.ErrNoRows) {
			return e
		}
		floor, e := q.LatestSignerEpoch(ctx, generated.LatestSignerEpochParams{TenantID: scope.TenantID(), SignerID: signer})
		if e != nil {
			return e
		}
		if epoch <= floor {
			return ErrShareConflict
		}
		out, e = q.AppendSignerAuthority(ctx, generated.AppendSignerAuthorityParams{TenantID: scope.TenantID(), SignerID: signer, SignerEpoch: epoch, HeartbeatAt: timestamp(heartbeat), AutonomyUntil: timestamp(deadline), ApprovalEvidence: proof})
		if e != nil {
			return e
		}
		return domainTask(ctx, q, scope, signer, "signer.authorized", "resource_change", epoch)
	})
	if e != nil {
		return generated.SignerAuthority{}, e
	}
	return out, nil
}

// PrepareShare serializes the entire tenant capacity account before reserving
// max(old,new). Prepared rights cannot renew or consume; no network I/O occurs
// until the task, outbox and audit commit. Idempotent replay never extends time.
func (s ShareService) PrepareShare(ctx context.Context, scope TenantScope, in PrepareShareInput, request string) (PreparedShare, error) {
	var out PreparedShare
	_, now, boundsErr := s.bounds()
	if boundsErr != nil {
		return out, boundsErr
	}
	if in.Kind != "rate" && in.Kind != "connections" || in.ExpectedGeneration < 0 || in.SignerEpoch < 1 || in.PathGeneration < 1 || in.FencingGeneration < 1 || in.Amount < 1 || in.Nonce == "" || len(in.Nonce) > 128 || in.BurstBytes < 0 {
		return out, ErrShareConflict
	}
	for _, id := range []string{in.NodeID, in.BootID, in.SignerID, in.SnapshotID} {
		if _, e := uuid.Parse(id); e != nil {
			return out, ErrShareConflict
		}
	}
	if in.Kind == "rate" && (in.Amount > 1_000_000_000_000 || in.BurstBytes > in.Amount/8) || in.Kind == "connections" && (in.Amount > 1_000_000 || in.BurstBytes != 0) {
		return out, ErrShareCapacity
	}
	raw, e := json.Marshal(in)
	if e != nil {
		return out, e
	}
	digest := sha256.Sum256(raw)
	e = (repository.AuditWriter{Pool: s.Pool}).WithTransaction(ctx, scope, policy.Mutate, &repository.Audit{Action: "share.prepare", ResourceID: in.NodeID, RequestID: request, Reason: "linear same-boot successor with globally reserved max capacity"}, func(q *generated.Queries) error {
		if e := q.LockUsageTuple(ctx, "hub-incarnation/"+in.SignerID); e != nil {
			return e
		}
		if e := q.LockNodeAuthorityRead(ctx, in.NodeID); e != nil {
			return e
		}
		if _, e := q.LockTenant(ctx, scope.TenantID()); e != nil {
			return e
		}
		old, e := q.ShareGenerationByRequest(ctx, generated.ShareGenerationByRequestParams{TenantID: scope.TenantID(), RequestKey: in.Nonce})
		if e == nil {
			if !bytes.Equal(old.RequestDigest, digest[:]) {
				return ErrShareConflict
			}
			h, e := q.ShareHandoffByGeneration(ctx, generated.ShareHandoffByGenerationParams{TenantID: scope.TenantID(), NodeID: old.NodeID, Kind: old.Kind, Generation: old.Generation})
			out = PreparedShare{old, h.OperationID, h.State}
			return e
		}
		if !errors.Is(e, pgx.ErrNoRows) {
			return e
		}
		_, now, e = s.bounds()
		if e != nil {
			return e
		}
		current, e := q.GetNodeIncarnation(ctx, in.NodeID)
		if e != nil {
			return e
		}
		if current.BootID != in.BootID {
			return ErrShareUnknown
		}
		limits, b, e := shareEligibility(ctx, q, scope, in, now)
		if e != nil {
			return e
		}
		a, e := q.GetSignerAuthority(ctx, generated.GetSignerAuthorityParams{TenantID: scope.TenantID(), SignerID: in.SignerID, SignerEpoch: in.SignerEpoch})
		if e != nil {
			return e
		}
		if !in.OwnershipUntil.After(now) || !in.PolicyUntil.After(now) || in.OwnershipUntil.After(a.AutonomyUntil.Time) || !now.Before(a.AutonomyUntil.Time) {
			return ErrLeaseBoundary
		}
		until, e := q.SnapshotEffectiveUntil(ctx, generated.SnapshotEffectiveUntilParams{TenantID: scope.TenantID(), ID: b.ID})
		if e != nil {
			return e
		}
		if !finiteTimeBefore(in.PolicyUntil.Add(-time.Microsecond), until) {
			return ErrLeaseBoundary
		}
		_, now, e = s.bounds()
		if e != nil {
			return e
		}
		if !in.OwnershipUntil.After(now) || !in.PolicyUntil.After(now) {
			return ErrLeaseBoundary
		}
		different, e := q.NodeOtherBootHeld(ctx, generated.NodeOtherBootHeldParams{TenantID: scope.TenantID(), NodeID: in.NodeID, BootID: in.BootID})
		if e != nil {
			return e
		}
		if different {
			return ErrShareUnknown
		}
		if e = q.EnsureNodeFencing(ctx, generated.EnsureNodeFencingParams{TenantID: scope.TenantID(), NodeID: in.NodeID, BootID: in.BootID, FencingGeneration: in.FencingGeneration}); e != nil {
			return e
		}
		fence, e := q.LockNodeFencing(ctx, generated.LockNodeFencingParams{TenantID: scope.TenantID(), NodeID: in.NodeID, BootID: in.BootID})
		if e != nil {
			return e
		}
		if fence.RenewalClosed || in.FencingGeneration < fence.FencingGeneration {
			return ErrShareConflict
		}
		if e = q.EnsureShareChain(ctx, generated.EnsureShareChainParams{TenantID: scope.TenantID(), NodeID: in.NodeID, Kind: in.Kind, BootID: in.BootID}); e != nil {
			return e
		}
		chain, e := q.LockShareChain(ctx, generated.LockShareChainParams{TenantID: scope.TenantID(), NodeID: in.NodeID, Kind: in.Kind})
		if e != nil {
			return e
		}
		if chain.BootID != in.BootID || chain.HeadGeneration != in.ExpectedGeneration || chain.PendingGeneration != 0 {
			return ErrShareConflict
		}
		total, e := q.TotalShareHeld(ctx, generated.TotalShareHeldParams{TenantID: scope.TenantID(), Kind: in.Kind})
		if e != nil {
			return e
		}
		limit := limits.RateLimit
		if in.Kind == "connections" {
			limit = limits.ConnectionLimit
		}
		increase := max(chain.HeldAmount, in.Amount) - chain.HeldAmount
		if total > limit || increase > limit-total {
			return ErrShareCapacity
		}
		generation, e := q.AppendShareGeneration(ctx, generated.AppendShareGenerationParams{TenantID: scope.TenantID(), NodeID: in.NodeID, Kind: in.Kind, Generation: chain.HeadGeneration + 1, Predecessor: chain.HeadGeneration, ID: uuid.NewString(), BootID: in.BootID, SignerID: in.SignerID, SignerEpoch: in.SignerEpoch, PathGeneration: in.PathGeneration, FencingGeneration: in.FencingGeneration, Amount: in.Amount, BurstBytes: in.BurstBytes, BillingSnapshotID: in.SnapshotID, RuleID: b.RuleID, RuleRevision: b.RuleRevision, CreatedAt: timestamp(now), OwnershipUntil: timestamp(in.OwnershipUntil), PolicyUntil: timestamp(in.PolicyUntil), AutonomyUntil: a.AutonomyUntil, RequestKey: in.Nonce, RequestDigest: digest[:]})
		if e != nil {
			return e
		}
		n, e := q.ReserveShareSuccessor(ctx, generated.ReserveShareSuccessorParams{TenantID: scope.TenantID(), NodeID: in.NodeID, Kind: in.Kind, HeadGeneration: generation.Generation, HeldAmount: in.Amount, HeadGeneration_2: chain.HeadGeneration})
		if e != nil {
			return e
		}
		if n != 1 {
			return ErrShareConflict
		}
		if e = q.AppendShareRenewalAuthority(ctx, generated.AppendShareRenewalAuthorityParams{TenantID: scope.TenantID(), NodeID: in.NodeID, Kind: in.Kind, Generation: generation.Generation, SignerID: in.SignerID, SignerEpoch: in.SignerEpoch}); e != nil {
			return e
		}
		op, e := newDomainTask(ctx, q, scope, in.NodeID, "share.prepare", "share_handoff", generation.Generation)
		if e != nil {
			return e
		}
		h, e := q.CreateShareHandoff(ctx, generated.CreateShareHandoffParams{TenantID: scope.TenantID(), ID: generation.ID, NodeID: in.NodeID, Kind: in.Kind, Generation: generation.Generation, OperationID: op.ID, CreatedBy: scope.ActorID()})
		if e != nil {
			return e
		}
		out = PreparedShare{generation, op.ID, h.State}
		return resumePreparedNodeDeployments(ctx, q, scope, in.NodeID)
	})
	if e != nil {
		return PreparedShare{}, e
	}
	return out, nil
}

// shareEligibility resolves the immutable snapshot against today's tenant,
// subscription, desired rule, grants and member boot, never actual_version.
func shareEligibility(ctx context.Context, q *generated.Queries, scope TenantScope, in PrepareShareInput, at time.Time) (PackageTerms, generated.BillingSnapshot, error) {
	var terms PackageTerms
	var b generated.BillingSnapshot
	tenant, e := q.GetTenant(ctx, scope.TenantID())
	if e != nil {
		return terms, b, e
	}
	if tenant.AccountState != "active" {
		return terms, b, ErrScope
	}
	sub, e := q.GetSubscription(ctx, scope.TenantID())
	if e != nil {
		return terms, b, e
	}
	if !at.Before(sub.ExpiresAt.Time) || at.Before(sub.AnchorAt.Time) {
		return terms, b, ErrLeaseBoundary
	}
	snap, e := q.GetPackageSnapshot(ctx, generated.GetPackageSnapshotParams{TenantID: scope.TenantID(), ID: sub.SnapshotID})
	if e != nil {
		return terms, b, e
	}
	if e = json.Unmarshal(snap.Terms, &terms); e != nil {
		return terms, b, e
	}
	b, e = q.GetBillingSnapshot(ctx, generated.GetBillingSnapshotParams{TenantID: scope.TenantID(), ID: in.SnapshotID})
	if e != nil {
		return terms, b, e
	}
	if at.Before(b.ValidFrom.Time) || !finiteTimeBefore(at, b.ValidUntil) {
		return terms, b, ErrLeaseBoundary
	}
	if in.PolicyUntil.After(sub.ExpiresAt.Time) {
		return terms, b, ErrLeaseBoundary
	}
	if b.MultiplierMilli == 0 {
		return terms, b, ErrSelfHostedExempt
	}
	if b.PathGeneration != in.PathGeneration || b.PackageSnapshotID != sub.SnapshotID {
		return terms, b, ErrShareConflict
	}
	h, e := q.LockRuleHead(ctx, generated.LockRuleHeadParams{TenantID: scope.TenantID(), ID: b.RuleID})
	if e != nil {
		return terms, b, e
	}
	if h.DesiredRevision != b.RuleRevision {
		return terms, b, ErrShareConflict
	}
	rev, e := q.GetRuleRevision(ctx, generated.GetRuleRevisionParams{TenantID: scope.TenantID(), RuleID: b.RuleID, Revision: b.RuleRevision})
	if e != nil {
		return terms, b, e
	}
	var rule RuleInput
	if e = json.Unmarshal(rev.Payload, &rule); e != nil {
		return terms, b, e
	}
	if !rule.Enabled {
		return terms, b, ErrScope
	}
	restrictions, e := q.ListCurrentRestrictions(ctx, generated.ListCurrentRestrictionsParams{TenantID: scope.TenantID(), AtTime: timestamp(at)})
	if e != nil {
		return terms, b, e
	}
	for _, r := range restrictions {
		if !r.ResourceID.Valid || r.ResourceID.String == b.RuleID {
			return terms, b, ErrScope
		}
	}
	resolved, e := resolveRule(ctx, q, scope, rule)
	if e != nil {
		return terms, b, e
	}
	for _, grant := range resolved.grants {
		if in.PolicyUntil.After(grant.ValidUntil.Time) {
			return terms, b, ErrLeaseBoundary
		}
	}
	member := false
	var groups []BillingGroup
	if e = json.Unmarshal(b.PathVersions, &groups); e != nil {
		return terms, b, e
	}
	executionGroup := ""
	for _, g := range groups {
		if g.Multiplier > 0 {
			executionGroup = g.Group
			break
		}
	}
	for groupID, group := range resolved.members {
		if groupID != executionGroup {
			continue
		}
		for _, n := range group.Members {
			if n.ID == in.NodeID && n.BootID == in.BootID {
				member = true
			}
		}
	}
	if !member {
		return terms, b, ErrScope
	}
	return terms, b, nil
}

// RecordNodeFencing only raises a same-boot floor. New boots may not use this
// endpoint to bypass a retained chain or revive renewal authority.
func (s ShareService) RecordNodeFencing(ctx context.Context, scope TenantScope, node, boot string, floor int64, request string) error {
	if _, e := (PortService{NodeCA: s.NodeCA}).verifiedNode(ctx, node); e != nil {
		return e
	}
	if floor < 1 {
		return ErrShareConflict
	}
	return (repository.AuditWriter{Pool: s.Pool}).WithTransaction(ctx, scope, policy.Mutate, &repository.Audit{Action: "share.fence", ResourceID: node, RequestID: request, Reason: "monotonic durable same-boot fencing floor"}, func(q *generated.Queries) error {
		if e := q.LockNodeAuthorityRead(ctx, node); e != nil {
			return e
		}
		if _, e := q.LockTenant(ctx, scope.TenantID()); e != nil {
			return e
		}
		current, e := q.GetNodeIncarnation(ctx, node)
		if e != nil {
			return e
		}
		if current.BootID != boot {
			return ErrShareConflict
		}
		other, e := q.NodeOtherBootHeld(ctx, generated.NodeOtherBootHeldParams{TenantID: scope.TenantID(), NodeID: node, BootID: boot})
		if e != nil {
			return e
		}
		if other {
			return ErrShareUnknown
		}
		fence, e := q.LockNodeFencing(ctx, generated.LockNodeFencingParams{TenantID: scope.TenantID(), NodeID: node, BootID: boot})
		if e != nil {
			return e
		}
		if floor < fence.FencingGeneration || fence.RenewalClosed {
			return ErrShareConflict
		}
		if _, e = q.RaiseNodeFencing(ctx, generated.RaiseNodeFencingParams{TenantID: scope.TenantID(), NodeID: node, BootID: boot, FencingGeneration: floor}); e != nil {
			return e
		}
		return domainTask(ctx, q, scope, node, "share.fence", "resource_change", floor)
	})
}
