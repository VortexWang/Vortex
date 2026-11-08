package service

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"vortex/agent/store"
	"vortex/backend/db/generated"
	"vortex/backend/internal/policy"
	"vortex/backend/internal/repository"
	"vortex/hub"
)

type HubSameStoreRecoveryInput struct {
	ID, HubID, BackendID, FromBootID, ToBootID string
	FromEpoch, ToEpoch                         int64
	Reason                                     string
}

type hubSameStoreRecoveryProof struct {
	Version          int
	Input            HubSameStoreRecoveryInput
	Earliest, Latest time.Time
	Authorities      []generated.SignerAuthority
	Allocations      []hub.SameStoreAllocationLink
}

// PrepareHubSameStoreRecovery commits approval and exact original allocation
// links only. It cannot bind an incarnation, acknowledge a journal, release
// funds/capacity or declare a Hub online. A complete authenticated S/R consumer
// is required separately, including independently checking the entire prefix.
func (s ShareService) PrepareHubSameStoreRecovery(ctx context.Context, scope TenantScope, in HubSameStoreRecoveryInput, request string) (generated.HubSameStoreRecovery, error) {
	var out generated.HubSameStoreRecovery
	if !scope.Platform() || !scope.Allows(policy.Grant) {
		return out, ErrScope
	}
	for _, id := range []string{in.ID, in.HubID, in.BackendID, in.FromBootID, in.ToBootID} {
		if !validID(id) {
			return out, ErrShareConflict
		}
	}
	if in.FromEpoch < 1 || in.FromEpoch == math.MaxInt64 || in.ToEpoch != in.FromEpoch+1 || strings.TrimSpace(in.Reason) == "" || len(in.Reason) > 256 {
		return out, ErrShareConflict
	}
	event := repository.Audit{Action: "hub.prepare_same_store_recovery", ResourceID: in.HubID, RequestID: request, Reason: in.Reason}
	err := (repository.AuditWriter{Pool: s.Pool}).WithTransaction(ctx, scope, policy.Grant, &event, func(q *generated.Queries) error {
		if e := q.LockUsageTuple(ctx, "hub-incarnation/"+in.HubID); e != nil {
			return e
		}
		if _, e := q.LockTenant(ctx, scope.TenantID()); e != nil {
			return e
		}
		prior, e := q.GetHubSameStoreRecovery(ctx, generated.GetHubSameStoreRecoveryParams{HubID: in.HubID, ToEpoch: in.ToEpoch})
		if e == nil {
			var p hubSameStoreRecoveryProof
			ph, ch := sha256.Sum256(prior.Proof), sha256.Sum256(prior.Payload)
			if prior.TenantID != scope.TenantID() || json.Unmarshal(prior.Proof, &p) != nil || p.Input != in || !bytes.Equal(ph[:], prior.ProofDigest) || !bytes.Equal(ch[:], prior.PayloadDigest) {
				return ErrShareConflict
			}
			if _, e := hub.DecodeSameStoreRecovery(prior.Payload); e != nil {
				return e
			}
			out = prior
			return nil
		}
		if !errors.Is(e, pgx.ErrNoRows) {
			return e
		}
		old, e := q.LatestHubIncarnation(ctx, in.HubID)
		if e != nil {
			return e
		}
		if old.SignerEpoch != in.FromEpoch || old.BootID != in.FromBootID {
			return ErrShareUnknown
		}
		boundary, e := q.HubSignerBoundary(ctx, in.HubID)
		if e != nil {
			return e
		}
		if boundary.SignerEpoch != in.ToEpoch {
			return ErrShareUnknown
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
		approved := map[string]generated.SignerAuthority{}
		needed := map[string]bool{}
		for _, a := range authorities {
			if a.SignerEpoch == in.ToEpoch {
				approved[a.TenantID] = a
			} else {
				needed[a.TenantID] = true
			}
		}
		for tenant := range needed {
			a, ok := approved[tenant]
			if !ok || !hi.Before(a.AutonomyUntil.Time) {
				return ErrShareUnknown
			}
		}
		allocations, e := q.HubRecoveryAllocations(ctx, in.HubID)
		if e != nil {
			return e
		}
		sort.Slice(allocations, func(i, j int) bool { return allocations[i].AllocationID < allocations[j].AllocationID })
		command := hub.SameStoreRecoveryCommand{Version: 1, ID: in.ID, BackendID: in.BackendID, HubID: in.HubID, FromBootID: in.FromBootID, ToBootID: in.ToBootID, IssuedAt: lo, ExpiresAt: hi.Add(4 * time.Minute), Allocations: []hub.SameStoreAllocationLink{}, Handoff: store.HandoffRequest{ID: in.ID, HubID: in.HubID, BackendID: in.BackendID, FromEpoch: uint64(in.FromEpoch), ToEpoch: uint64(in.ToEpoch), Heartbeat: lo, RevokeDelegations: []string{}, FundedSuccessors: []store.FundedHandoffSuccessor{}}}
		for _, record := range allocations {
			if _, e := q.GetHubDelegationSeal(ctx, generated.GetHubDelegationSealParams{TenantID: record.TenantID, AllocationID: record.AllocationID}); e == nil {
				continue
			} else if !errors.Is(e, pgx.ErrNoRows) {
				return e
			}
			// A separate cold/older boot's unknown financial parent cannot be
			// silently adopted as unissued R by this same-store command.
			installed, e := q.GetHubAllocationReceipt(ctx, generated.GetHubAllocationReceiptParams{TenantID: record.TenantID, AllocationID: record.AllocationID})
			if e != nil {
				if errors.Is(e, pgx.ErrNoRows) {
					return ErrShareUnknown
				}
				return e
			}
			wireHash := sha256.Sum256(record.Payload)
			if !bytes.Equal(wireHash[:], record.PayloadDigest) || installed.HubID != in.HubID || installed.HubBootID != record.HubBootID || !bytes.Equal(installed.PayloadDigest, record.PayloadDigest) {
				return ErrShareConflict
			}
			originalAllocation, e := hub.DecodeAllocation(record.Payload)
			if e != nil {
				return e
			}
			a, _, e := effectiveHubAllocationTx(ctx, q, record, uint64(in.FromEpoch), in.FromBootID, lo)
			if e != nil {
				return e
			}
			if a.SignerEpoch != uint64(in.FromEpoch) || a.AllocationID != record.AllocationID || a.DelegationID != record.DelegationID || a.SourceID != record.ReservationID || a.NodeID != record.NodeID || a.BootID != record.BootID || a.HubID != in.HubID {
				return ErrShareUnknown
			}
			provision, e := q.GetNodeSourceProvision(ctx, generated.GetNodeSourceProvisionParams{TenantID: record.TenantID, AllocationID: record.AllocationID})
			if e != nil {
				if errors.Is(e, pgx.ErrNoRows) {
					return ErrShareUnknown
				}
				return e
			}
			if provision.BackendID != in.BackendID {
				return ErrShareConflict
			}
			approval, ok := approved[record.TenantID]
			if !ok || !hi.Before(a.ExpiresAt) || !hi.Before(a.HardDeadline) || !hi.Before(a.AutonomyDeadline) || !hi.Before(approval.AutonomyUntil.Time) {
				return ErrLeaseBoundary
			}
			original, e := json.Marshal(originalAllocation.Delegation())
			if e != nil {
				return e
			}
			delegationHash := sha256.Sum256(original)
			refs := append([]store.ShareReference(nil), a.ShareReferences...)
			for i := range refs {
				if refs[i].Revision >= math.MaxInt64 {
					return ErrShareUnknown
				}
				refs[i].Revision++
				refs[i].SignerEpoch = uint64(in.ToEpoch)
			}
			command.Allocations = append(command.Allocations, hub.SameStoreAllocationLink{AllocationID: record.AllocationID, PayloadSHA256: hex.EncodeToString(wireHash[:]), DelegationID: a.DelegationID, DelegationSHA256: hex.EncodeToString(delegationHash[:])})
			command.Handoff.FundedSuccessors = append(command.Handoff.FundedSuccessors, store.FundedHandoffSuccessor{DelegationID: a.DelegationID, AllocationSHA256: hex.EncodeToString(delegationHash[:]), ShareReferences: refs})
		}
		if len(command.Allocations) == 0 {
			return ErrShareUnknown
		}
		raw, e := json.Marshal(command)
		if e != nil {
			return e
		}
		if _, e = hub.DecodeSameStoreRecovery(raw); e != nil {
			return e
		}
		proof, e := json.Marshal(hubSameStoreRecoveryProof{Version: 1, Input: in, Earliest: lo, Latest: hi, Authorities: authorities, Allocations: command.Allocations})
		if e != nil {
			return e
		}
		if len(proof) > 4<<20 {
			return ErrShareUnknown
		}
		hash, proofHash := sha256.Sum256(raw), sha256.Sum256(proof)
		op, e := newDomainTask(ctx, q, scope, in.HubID, "hub.recover_same_store", "resource_change", in.ToEpoch)
		if e != nil {
			return e
		}
		if e = q.AppendHubSameStoreRecovery(ctx, generated.AppendHubSameStoreRecoveryParams{TenantID: scope.TenantID(), ID: in.ID, HubID: in.HubID, FromEpoch: in.FromEpoch, ToEpoch: in.ToEpoch, FromBootID: in.FromBootID, ToBootID: in.ToBootID, BackendID: in.BackendID, OperationID: op.ID, CreatedBy: scope.ActorID(), Payload: raw, PayloadDigest: hash[:], Proof: proof, ProofDigest: proofHash[:]}); e != nil {
			return e
		}
		out, e = q.GetHubSameStoreRecovery(ctx, generated.GetHubSameStoreRecoveryParams{HubID: in.HubID, ToEpoch: in.ToEpoch})
		return e
	})
	if err != nil {
		return generated.HubSameStoreRecovery{}, err
	}
	return out, nil
}
