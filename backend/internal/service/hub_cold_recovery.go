package service

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"vortex/backend/db/generated"
	"vortex/backend/internal/policy"
	"vortex/backend/internal/repository"
	"vortex/hub"
)

// HubColdRecoveryInput is an explicit platform decision for one new process.
// It is not an S/R transfer: every earlier hard boundary must already be past,
// and all financial/capacity occupation survives unchanged.
type HubColdRecoveryInput struct {
	ID         string `json:"recovery_id"`
	HubID      string `json:"hub_id"`
	FromBootID string `json:"from_boot_id"`
	ToBootID   string `json:"to_boot_id"`
	FromEpoch  int64  `json:"from_epoch"`
	ToEpoch    int64  `json:"to_epoch"`
	Reason     string `json:"reason"`
}

type hubColdRecoveryProof struct {
	Version         int                         `json:"version"`
	Request         HubColdRecoveryInput        `json:"request"`
	Earliest        time.Time                   `json:"earliest"`
	Latest          time.Time                   `json:"latest"`
	OldHardDeadline time.Time                   `json:"old_hard_deadline"`
	Authorities     []generated.SignerAuthority `json:"authorities"`
	Allocations     []hubColdAllocationProof    `json:"allocations"`
	Shares          []generated.ShareGeneration `json:"shares"`
}
type hubColdAllocationProof struct {
	TenantID      string `json:"tenant_id"`
	AllocationID  string `json:"allocation_id"`
	ReservationID string `json:"reservation_id"`
	PayloadSHA256 string `json:"payload_sha256"`
}

// RecoverColdHubBoot records the complete immutable authority inventory and
// binds a preapproved epoch to a specified fresh boot in one audit transaction.
// It never claims Node stop/known usage, settles debt or transfers unissued R.
func (s ShareService) RecoverColdHubBoot(ctx context.Context, scope TenantScope, in HubColdRecoveryInput, request string) (generated.HubColdRecovery, error) {
	var out generated.HubColdRecovery
	if !scope.Platform() || !scope.Allows(policy.Grant) {
		return out, ErrScope
	}
	for _, id := range []string{in.ID, in.HubID, in.FromBootID, in.ToBootID} {
		if !validID(id) {
			return out, ErrShareConflict
		}
	}
	if in.FromBootID == in.ToBootID || in.FromEpoch < 1 || in.ToEpoch <= in.FromEpoch || strings.TrimSpace(in.Reason) == "" || len(in.Reason) > 256 {
		return out, ErrShareConflict
	}
	event := repository.Audit{Action: "hub.cold_recover", ResourceID: in.HubID, RequestID: request, Reason: in.Reason}
	err := (repository.AuditWriter{Pool: s.Pool}).WithTransaction(ctx, scope, policy.Grant, &event, func(q *generated.Queries) error {
		if e := q.LockUsageTuple(ctx, "hub-incarnation/"+in.HubID); e != nil {
			return e
		}
		if _, e := q.LockTenant(ctx, scope.TenantID()); e != nil {
			return e
		}
		prior, e := q.GetHubColdRecovery(ctx, generated.GetHubColdRecoveryParams{HubID: in.HubID, ToEpoch: in.ToEpoch})
		if e == nil {
			var p hubColdRecoveryProof
			hash := sha256.Sum256(prior.Payload)
			if json.Unmarshal(prior.Payload, &p) != nil || !bytes.Equal(hash[:], prior.PayloadDigest) || prior.TenantID != scope.TenantID() || p.Request != in {
				return ErrShareConflict
			}
			out = prior
			event.After = []byte(`{"proof_sha256":"` + hex.EncodeToString(prior.PayloadDigest) + `","replayed":true}`)
			return nil
		}
		if !errors.Is(e, pgx.ErrNoRows) {
			return e
		}
		boundary, e := q.HubSignerBoundary(ctx, in.HubID)
		if e != nil {
			return e
		}
		if boundary.SignerEpoch != in.ToEpoch {
			return ErrShareUnknown
		}
		old, e := q.LatestHubIncarnation(ctx, in.HubID)
		if e != nil {
			return e
		}
		if old.SignerEpoch != in.FromEpoch || old.BootID != in.FromBootID {
			return ErrShareConflict
		}
		lo, hi, e := s.bounds()
		if e != nil {
			return e
		}
		if !hi.Before(boundary.AutonomyUntil.Time) {
			return ErrLeaseBoundary
		}
		authorities, e := q.HubRecoveryAuthorities(ctx, generated.HubRecoveryAuthoritiesParams{SignerID: in.HubID, ToEpoch: in.ToEpoch})
		if e != nil {
			return e
		}
		proof := hubColdRecoveryProof{Version: 1, Request: in, Earliest: lo, Latest: hi, Authorities: authorities, Allocations: []hubColdAllocationProof{}}
		oldTenants, newTenants := map[string]bool{}, map[string]bool{}
		for _, a := range authorities {
			if a.SignerEpoch == in.ToEpoch {
				if !hi.Before(a.AutonomyUntil.Time) {
					return ErrLeaseBoundary
				}
				newTenants[a.TenantID] = true
			} else {
				oldTenants[a.TenantID] = true
				if a.AutonomyUntil.Time.After(proof.OldHardDeadline) {
					proof.OldHardDeadline = a.AutonomyUntil.Time
				}
			}
		}
		if !newTenants[scope.TenantID()] || len(oldTenants) == 0 {
			return ErrScope
		}
		for tenant := range oldTenants {
			if !newTenants[tenant] {
				return ErrShareUnknown
			}
		}
		allocations, e := q.HubRecoveryAllocations(ctx, in.HubID)
		if e != nil {
			return e
		}
		for _, a := range allocations {
			hash := sha256.Sum256(a.Payload)
			original, e := hub.DecodeAllocation(a.Payload)
			if e != nil || !bytes.Equal(hash[:], a.PayloadDigest) || original.HubID != in.HubID || original.AllocationID != a.AllocationID || original.SignerEpoch >= uint64(in.ToEpoch) {
				return ErrShareConflict
			}
			if original.AutonomyDeadline.After(proof.OldHardDeadline) {
				proof.OldHardDeadline = original.AutonomyDeadline
			}
			proof.Allocations = append(proof.Allocations, hubColdAllocationProof{a.TenantID, a.AllocationID, a.ReservationID, hex.EncodeToString(hash[:])})
		}
		shares, e := q.HubRecoveryShares(ctx, in.HubID)
		if e != nil {
			return e
		}
		proof.Shares = []generated.ShareGeneration{}
		for _, g := range shares {
			if g.SignerEpoch == in.ToEpoch {
				continue
			} // prepared successor rights are not retired
			if g.SignerEpoch > in.ToEpoch {
				return ErrShareConflict
			}
			proof.Shares = append(proof.Shares, g)
			for _, end := range []time.Time{g.OwnershipUntil.Time, g.PolicyUntil.Time, g.AutonomyUntil.Time} {
				if end.After(proof.OldHardDeadline) {
					proof.OldHardDeadline = end
				}
			}
		}
		if proof.OldHardDeadline.IsZero() || lo.Before(proof.OldHardDeadline) {
			return ErrShareUnknown
		}
		payload, e := json.Marshal(proof)
		if e != nil {
			return e
		}
		digest := sha256.Sum256(payload)
		if e = q.BindHubIncarnation(ctx, generated.BindHubIncarnationParams{HubID: in.HubID, SignerEpoch: in.ToEpoch, BootID: in.ToBootID}); e != nil {
			return e
		}
		if e = q.AppendHubColdRecovery(ctx, generated.AppendHubColdRecoveryParams{TenantID: scope.TenantID(), ID: in.ID, HubID: in.HubID, FromEpoch: in.FromEpoch, ToEpoch: in.ToEpoch, FromBootID: in.FromBootID, ToBootID: in.ToBootID, Payload: payload, PayloadDigest: digest[:], CreatedBy: scope.ActorID()}); e != nil {
			return e
		}
		if e = domainTask(ctx, q, scope, in.HubID, "hub.cold_recovered", "resource_change", in.ToEpoch); e != nil {
			return e
		}
		event.After = []byte(`{"proof_sha256":"` + hex.EncodeToString(digest[:]) + `","held_funds_unchanged":true}`)
		out, e = q.GetHubColdRecovery(ctx, generated.GetHubColdRecoveryParams{HubID: in.HubID, ToEpoch: in.ToEpoch})
		return e
	})
	if err != nil {
		return generated.HubColdRecovery{}, err
	}
	return out, nil
}
