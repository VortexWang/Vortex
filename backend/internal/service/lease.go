package service

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"vortex/backend/db/generated"
	"vortex/backend/internal/policy"
	"vortex/backend/internal/repository"
)

// LeaseRequest is a renewal of an enforced, same-boot share. It cannot transfer
// ownership, change amount or increase any independently granted deadline.
type LeaseRequest struct {
	NodeID, BootID, Kind, Nonce, ConnectionID, SignerID        string
	Generation, SignerEpoch, PathGeneration, FencingGeneration int64
	RequestedAt                                                time.Time
}

// EffectiveLeaseDeadline intersects independent authority boundaries. Only the
// relative TTL has the ENG-025 30-second safety deduction, exactly once.
func EffectiveLeaseDeadline(l generated.LeaseExpectation) time.Time {
	d := l.RelativeDeadline.Time
	for _, t := range []time.Time{l.OwnershipUntil.Time, l.PolicyUntil.Time, l.AutonomyUntil.Time} {
		if t.Before(d) {
			d = t
		}
	}
	return d
}

func (s ShareService) RenewShare(ctx context.Context, scope TenantScope, in LeaseRequest, request string) (generated.LeaseExpectation, error) {
	var out generated.LeaseExpectation
	if in.Kind != "rate" && in.Kind != "connections" || !validID(in.NodeID) || !validID(in.BootID) || !validID(in.ConnectionID) || !validID(in.SignerID) || in.Nonce == "" || len(in.Nonce) > 128 || in.Generation < 1 {
		return out, ErrShareConflict
	}
	if _, e := (PortService{NodeCA: s.NodeCA}).verifiedNode(ctx, in.NodeID); e != nil {
		return out, e
	}
	in.RequestedAt = in.RequestedAt.UTC().Truncate(time.Microsecond)
	raw, e := json.Marshal(in)
	if e != nil {
		return out, e
	}
	digest := sha256.Sum256(raw)
	e = (repository.AuditWriter{Pool: s.Pool}).WithTransaction(ctx, scope, policy.Mutate, &repository.Audit{Action: "share.renew", ResourceID: in.NodeID, RequestID: request, Reason: "renew enforced same-boot share without changing independent authority boundaries"}, func(q *generated.Queries) error {
		if _, e := q.LockTenant(ctx, scope.TenantID()); e != nil {
			return e
		}
		// The nonce is checked before time validity: replay returns exactly the old,
		// possibly expired token, never a newly timed authority.
		old, e := q.FindLeaseNonce(ctx, generated.FindLeaseNonceParams{TenantID: scope.TenantID(), NodeID: in.NodeID, Kind: in.Kind, Nonce: in.Nonce})
		if e == nil {
			if !bytes.Equal(old.RequestDigest, digest[:]) {
				return ErrShareConflict
			}
			out = old
			return nil
		}
		if !errors.Is(e, pgx.ErrNoRows) {
			return e
		}
		earliest, now, e := s.bounds()
		if e != nil {
			return e
		}
		if in.RequestedAt.IsZero() || in.RequestedAt.After(earliest) || now.Sub(in.RequestedAt) > 10*time.Second {
			return ErrLeaseBoundary
		}
		chain, e := q.LockShareChain(ctx, generated.LockShareChainParams{TenantID: scope.TenantID(), NodeID: in.NodeID, Kind: in.Kind})
		if e != nil {
			return e
		}
		if chain.BootID != in.BootID || chain.ActiveGeneration != in.Generation {
			return ErrShareConflict
		}
		g, e := q.GetShareGeneration(ctx, generated.GetShareGenerationParams{TenantID: scope.TenantID(), NodeID: in.NodeID, Kind: in.Kind, Generation: in.Generation})
		if e != nil {
			return e
		}
		node, e := q.GetNodeIncarnation(ctx, in.NodeID)
		if e != nil {
			return e
		}
		if node.BootID != in.BootID {
			return ErrShareConflict
		}
		fence, e := q.LockNodeFencing(ctx, generated.LockNodeFencingParams{TenantID: scope.TenantID(), NodeID: in.NodeID, BootID: in.BootID})
		if e != nil {
			return e
		}
		if fence.RenewalClosed || in.FencingGeneration < fence.FencingGeneration || g.FencingGeneration != in.FencingGeneration || g.PathGeneration != in.PathGeneration || g.SignerEpoch != in.SignerEpoch || g.SignerID != in.SignerID {
			return ErrShareConflict
		}
		a, e := q.ShareRenewalAuthority(ctx, generated.ShareRenewalAuthorityParams{TenantID: scope.TenantID(), NodeID: in.NodeID, Kind: in.Kind, Generation: in.Generation, SignerID: in.SignerID})
		if e != nil {
			return e
		}
		if a.ClosedAt.Valid || a.SignerEpoch != in.SignerEpoch {
			return ErrShareConflict
		}
		if !now.Before(g.OwnershipUntil.Time) || !now.Before(g.PolicyUntil.Time) || !now.Before(g.AutonomyUntil.Time) {
			return ErrLeaseBoundary
		}
		inPolicy := PrepareShareInput{NodeID: in.NodeID, BootID: in.BootID, Kind: in.Kind, SnapshotID: g.BillingSnapshotID, PathGeneration: g.PathGeneration, PolicyUntil: g.PolicyUntil.Time}
		if _, _, e = shareEligibility(ctx, q, scope, inPolicy, now); e != nil {
			return e
		}
		revision, e := q.LatestLeaseRevision(ctx, generated.LatestLeaseRevisionParams{TenantID: scope.TenantID(), NodeID: in.NodeID, Kind: in.Kind})
		if e != nil {
			return e
		}
		out, e = q.AppendLeaseExpectation(ctx, generated.AppendLeaseExpectationParams{TenantID: scope.TenantID(), ID: uuid.NewString(), NodeID: in.NodeID, Kind: in.Kind, ShareKind: in.Kind, Generation: g.Generation, BootID: in.BootID, Revision: revision + 1, Nonce: in.Nonce, ConnectionID: in.ConnectionID, RequestDigest: digest[:], RequestedAt: timestamp(in.RequestedAt), IssuedAt: timestamp(now), RelativeDeadline: timestamp(in.RequestedAt.Add(270 * time.Second)), OwnershipUntil: g.OwnershipUntil, PolicyUntil: g.PolicyUntil, AutonomyUntil: g.AutonomyUntil})
		if e != nil {
			return e
		}
		return domainTask(ctx, q, scope, in.NodeID, "share.renew", "resource_change", revision+1)
	})
	if e != nil {
		return generated.LeaseExpectation{}, e
	}
	return out, nil
}

type RenewalClosure struct {
	NodeID, BootID, Kind, SignerID               string
	Generation, SuccessorGeneration, SignerEpoch int64
}

// CloseOldRenewal accepts an authenticated signer statement, not a tenant's
// fabricated hash. Closing only prevents future renewals; extant child leases
// remain valid and occupancy is retained until a durable CAS receipt.
func (s ShareService) CloseOldRenewal(ctx context.Context, scope TenantScope, in RenewalClosure, request string) error {
	fingerprint, e := (PortService{NodeCA: s.SignerCA}).verifiedNode(ctx, in.SignerID)
	if e != nil {
		return e
	}
	raw, e := json.Marshal(in)
	if e != nil {
		return e
	}
	proof := sha256.Sum256(append(raw, fingerprint...))
	return (repository.AuditWriter{Pool: s.Pool}).WithTransaction(ctx, scope, policy.Mutate, &repository.Audit{Action: "share.close_renewal", ResourceID: in.NodeID, RequestID: request, Reason: "authenticated signer closed predecessor issuance; existing leases and occupation retained"}, func(q *generated.Queries) error {
		if _, e := q.LockTenant(ctx, scope.TenantID()); e != nil {
			return e
		}
		_, now, e := s.bounds()
		if e != nil {
			return e
		}
		chain, e := q.LockShareChain(ctx, generated.LockShareChainParams{TenantID: scope.TenantID(), NodeID: in.NodeID, Kind: in.Kind})
		if e != nil {
			return e
		}
		if in.Generation < 1 || in.SuccessorGeneration != in.Generation+1 || chain.BootID != in.BootID || chain.PendingGeneration != in.SuccessorGeneration || chain.ActiveGeneration != in.Generation {
			return ErrShareConflict
		}
		a, e := q.ShareRenewalAuthority(ctx, generated.ShareRenewalAuthorityParams{TenantID: scope.TenantID(), NodeID: in.NodeID, Kind: in.Kind, Generation: in.Generation, SignerID: in.SignerID})
		if e != nil {
			return e
		}
		if a.SignerEpoch != in.SignerEpoch {
			return ErrShareConflict
		}
		if a.ClosedAt.Valid {
			if !bytes.Equal(a.ClosureProof, proof[:]) {
				return ErrShareConflict
			}
			return nil
		}
		if _, e = q.CloseShareRenewalAuthority(ctx, generated.CloseShareRenewalAuthorityParams{TenantID: scope.TenantID(), NodeID: in.NodeID, Kind: in.Kind, Generation: in.Generation, SignerID: in.SignerID, ClosedAt: timestamp(now), ClosureProof: proof[:]}); e != nil {
			return e
		}
		return domainTask(ctx, q, scope, in.NodeID, "share.close_renewal", "share_handoff", in.SuccessorGeneration)
	})
}
