package service

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/jackc/pgx/v5"
	"vortex/agent/store"
	"vortex/backend/db/generated"
	"vortex/backend/internal/policy"
	"vortex/backend/internal/repository"
	"vortex/hub"
)

type hubSameStoreAdmissionProof struct {
	Version                                            int
	RecoveryID, BackendID, HubID, FromBootID, ToBootID string
	FromEpoch, ToEpoch                                 int64
	Earliest, Latest                                   time.Time
	ApprovalSHA256, ManifestSHA256                     string
	Authorities                                        []generated.SignerAuthority
	Shares                                             []generated.ShareGeneration
	Nodes                                              []hubAdmissionNodeWitness
}
type hubAdmissionNodeWitness struct {
	TenantID, AllocationID, NodeID, BootID, FenceID, ProvisionID             string
	FenceSHA256, FenceReceiptSHA256, ProvisionSHA256, ProvisionReceiptSHA256 string
}

// AdmitHubSameStore commits only after then-current complete PG inventory and
// all Node FULL fences/next-root receipts agree. The immutable admission binds
// one target incarnation and closes old renewal issuance. It does not ACK the
// Hub ledger, settle usage, release money/capacity or make the local Hub online.
func (s ShareService) AdmitHubSameStore(ctx context.Context, scope TenantScope, recovery, request string) (generated.HubSameStoreAdmission, error) {
	var out generated.HubSameStoreAdmission
	if !scope.Platform() || !scope.Allows(policy.Grant) {
		return out, ErrScope
	}
	q := generated.New(s.Pool)
	approval, e := q.HubSameStoreRecoveryByID(ctx, recovery)
	if e != nil {
		return out, e
	}
	if approval.TenantID != scope.TenantID() {
		return out, ErrScope
	}
	e = (repository.AuditWriter{Pool: s.Pool}).WithTransaction(ctx, scope, policy.Grant, &repository.Audit{Action: "hub.admit_same_store", ResourceID: recovery, RequestID: request, Reason: "current all-tenant inventory and every Node FULL fence/root receipt; close old issuance, bind target only; no local ACK or financial release"}, func(q *generated.Queries) error {
		if e := q.LockUsageTuple(ctx, "hub-incarnation/"+approval.HubID); e != nil {
			return e
		}
		prior, e := q.GetHubSameStoreAdmission(ctx, recovery)
		if e == nil {
			if _, e = validateHubSameStoreAdmission(prior); e != nil {
				return e
			}
			if prior.TenantID != scope.TenantID() || prior.CreatedBy != scope.ActorID() {
				return ErrScope
			}
			out = prior
			return nil
		}
		if !errors.Is(e, pgx.ErrNoRows) {
			return e
		}
		approval, c, e := sameStoreCommand(ctx, q, recovery, approval.HubID, approval.ToBootID)
		if e != nil {
			return e
		}
		var original hubSameStoreRecoveryProof
		if json.Unmarshal(approval.Proof, &original) != nil {
			return ErrShareConflict
		}
		// Global Hub lock prevents new funding/authorities. Node locks precede all
		// sorted tenant locks, matching Node recovery/funding lock order.
		allocations, e := q.HubRecoveryAllocations(ctx, c.HubID)
		if e != nil {
			return e
		}
		shares, e := q.HubRecoveryShares(ctx, c.HubID)
		if e != nil {
			return e
		}
		nodes, tenants := map[string]bool{}, map[string]bool{scope.TenantID(): true}
		for _, a := range allocations {
			nodes[a.NodeID] = true
			tenants[a.TenantID] = true
		}
		for _, g := range shares {
			nodes[g.NodeID] = true
			tenants[g.TenantID] = true
		}
		for _, a := range original.Authorities {
			tenants[a.TenantID] = true
		}
		ordered := func(set map[string]bool) []string {
			out := make([]string, 0, len(set))
			for k := range set {
				out = append(out, k)
			}
			sort.Strings(out)
			return out
		}
		for _, node := range ordered(nodes) {
			if e = q.LockNodeAuthorityWrite(ctx, node); e != nil {
				return e
			}
		}
		for _, tenant := range ordered(tenants) {
			if _, e = q.LockTenant(ctx, tenant); e != nil {
				return e
			}
		}
		manifest, e := q.GetHubSameStoreManifest(ctx, recovery)
		if e != nil {
			return e
		}
		mh := sha256.Sum256(manifest.Payload)
		if !bytes.Equal(mh[:], manifest.PayloadDigest) {
			return ErrShareConflict
		}
		m, e := hub.DecodeSameStoreManifest(manifest.Payload, c)
		if e != nil {
			return e
		}
		if e = s.recheckSameStorePrefixTx(ctx, q, original, c, m, manifest.Payload); e != nil {
			return e
		}
		lo, hi, e := s.bounds()
		if e != nil {
			return e
		}
		proof := hubSameStoreAdmissionProof{Version: 1, RecoveryID: recovery, BackendID: c.BackendID, HubID: c.HubID, FromBootID: c.FromBootID, ToBootID: c.ToBootID, FromEpoch: int64(c.Handoff.FromEpoch), ToEpoch: int64(c.Handoff.ToEpoch), Earliest: lo, Latest: hi, ApprovalSHA256: hex.EncodeToString(approval.PayloadDigest), ManifestSHA256: hex.EncodeToString(mh[:]), Authorities: original.Authorities, Shares: shares, Nodes: []hubAdmissionNodeWitness{}}
		links := map[string]int{}
		for i, link := range c.Allocations {
			links[link.AllocationID] = i
		}
		// Do not adopt a new/hidden allocation or silently omit a now-sealed one.
		unsealed := 0
		covered := map[string]bool{}
		for _, a := range allocations {
			_, e := q.GetHubDelegationSeal(ctx, generated.GetHubDelegationSealParams{TenantID: a.TenantID, AllocationID: a.AllocationID})
			if e == nil {
				if _, ok := links[a.AllocationID]; ok {
					return ErrShareUnknown
				}
				continue
			}
			if !errors.Is(e, pgx.ErrNoRows) {
				return e
			}
			index, ok := links[a.AllocationID]
			if !ok || a.HubBootID != c.FromBootID {
				return ErrShareUnknown
			}
			unsealed++
			decoded, e := hub.DecodeAllocation(a.Payload)
			if e != nil {
				return e
			}
			if !hi.Before(decoded.ExpiresAt) || !hi.Before(decoded.HardDeadline) || !hi.Before(decoded.AutonomyDeadline) {
				return ErrLeaseBoundary
			}
			node, e := q.LockNodeRecoveryIncarnation(ctx, a.NodeID)
			if e != nil {
				return e
			}
			if node.BootID != a.BootID {
				return ErrShareUnknown
			}
			assigned, e := q.UsageNodeHub(ctx, a.NodeID)
			if e != nil {
				return e
			}
			if assigned != c.HubID {
				return ErrShareUnknown
			}
			ts, e := NewTenantScope(scope.ActorID(), a.TenantID, a.TenantID)
			if e != nil {
				return e
			}
			if _, _, e = shareEligibility(ctx, q, ts, PrepareShareInput{NodeID: a.NodeID, BootID: a.BootID, SnapshotID: decoded.SnapshotID, PathGeneration: int64(decoded.PathGeneration), PolicyUntil: decoded.ExpiresAt}, hi); e != nil {
				return e
			}
			floor, e := q.LockNodeFencing(ctx, generated.LockNodeFencingParams{TenantID: a.TenantID, NodeID: a.NodeID, BootID: a.BootID})
			if e != nil {
				return e
			}
			if floor.RenewalClosed || floor.FencingGeneration > int64(decoded.FencingFloor) {
				return ErrShareUnknown
			}
			reservation, e := q.GetUsageReservation(ctx, generated.GetUsageReservationParams{TenantID: a.TenantID, ID: a.ReservationID})
			if e != nil {
				return e
			}
			accounting, e := q.ReservationAccounting(ctx, generated.ReservationAccountingParams{TenantID: a.TenantID, GrantID: a.ReservationID})
			if e != nil {
				return e
			}
			if accounting.Terminal || accounting.ReturnedRaw != 0 || reservation.RawGranted != int64(decoded.RawBytes) {
				return ErrShareUnknown
			}
			bindings, e := q.HubAllocationShareBindings(ctx, generated.HubAllocationShareBindingsParams{TenantID: a.TenantID, AllocationID: a.AllocationID})
			if e != nil {
				return e
			}
			if len(bindings) != 2 {
				return ErrShareUnknown
			}
			for _, b := range bindings {
				chain, e := q.LockShareChain(ctx, generated.LockShareChainParams{TenantID: a.TenantID, NodeID: b.NodeID, Kind: b.Kind})
				if e != nil {
					return e
				}
				if chain.BootID != a.BootID || chain.ActiveGeneration != b.Generation || chain.PendingGeneration != 0 {
					return ErrShareUnknown
				}
				g, e := q.GetShareGeneration(ctx, generated.GetShareGenerationParams{TenantID: a.TenantID, NodeID: b.NodeID, Kind: b.Kind, Generation: b.Generation})
				if e != nil {
					return e
				}
				if g.SignerID != c.HubID || g.SignerEpoch != int64(c.Handoff.FromEpoch) || g.BootID != a.BootID || g.FencingGeneration > int64(decoded.FencingFloor) || !hi.Before(g.OwnershipUntil.Time) || !hi.Before(g.PolicyUntil.Time) || !hi.Before(g.AutonomyUntil.Time) {
					return ErrShareUnknown
				}
				if chain.HeldAmount < g.Amount || (b.Kind == "rate" && uint64(g.Amount) < decoded.RateBPS) || (b.Kind == "connections" && uint64(g.Amount) < decoded.Connections) {
					return ErrShareCapacity
				}
				authority, e := q.ShareRenewalAuthority(ctx, generated.ShareRenewalAuthorityParams{TenantID: a.TenantID, NodeID: b.NodeID, Kind: b.Kind, Generation: b.Generation, SignerID: c.HubID})
				if e != nil {
					return e
				}
				barrier, e := q.ShareClosureBarrier(ctx, generated.ShareClosureBarrierParams{TenantID: a.TenantID, NodeID: b.NodeID, BootID: a.BootID, Kind: b.Kind, Generation: b.Generation, SignerID: c.HubID})
				if e != nil {
					return e
				}
				if barrier || authority.ClosedAt.Valid || authority.SignerEpoch != int64(c.Handoff.FromEpoch) {
					return ErrShareUnknown
				}
				covered[fmt.Sprintf("%s/%s/%s/%d", a.TenantID, b.NodeID, b.Kind, b.Generation)] = true
			}
			w, e := recheckRecoveryNodeWitness(ctx, q, a, c, m, index)
			if e != nil {
				return e
			}
			proof.Nodes = append(proof.Nodes, w)
		}
		if unsealed != len(c.Allocations) {
			return ErrShareUnknown
		}
		// Every current share must participate; pending/unknown configurations cannot
		// be turned into a confirmed epoch transfer through unrelated allocations.
		for _, g := range shares {
			if g.SignerEpoch > int64(c.Handoff.FromEpoch) {
				return ErrShareUnknown
			}
			chain, e := q.LockShareChain(ctx, generated.LockShareChainParams{TenantID: g.TenantID, NodeID: g.NodeID, Kind: g.Kind})
			if e != nil {
				return e
			}
			if chain.PendingGeneration == g.Generation {
				return ErrShareUnknown
			}
			if chain.ActiveGeneration == g.Generation && !covered[fmt.Sprintf("%s/%s/%s/%d", g.TenantID, g.NodeID, g.Kind, g.Generation)] {
				return ErrShareUnknown
			}
		}
		raw, e := json.Marshal(proof)
		if e != nil {
			return e
		}
		if len(raw) > 4<<20 {
			return ErrShareUnknown
		}
		hash := sha256.Sum256(raw)
		for _, g := range shares {
			if _, e = q.CloseShareRenewalAuthority(ctx, generated.CloseShareRenewalAuthorityParams{TenantID: g.TenantID, NodeID: g.NodeID, Kind: g.Kind, Generation: g.Generation, SignerID: c.HubID, ClosedAt: timestamp(lo), ClosureProof: hash[:]}); e != nil {
				return e
			}
		}
		if e = q.BindHubIncarnation(ctx, generated.BindHubIncarnationParams{HubID: c.HubID, SignerEpoch: int64(c.Handoff.ToEpoch), BootID: c.ToBootID}); e != nil {
			return e
		}
		op, e := newDomainTask(ctx, q, scope, recovery, "hub.same_store_admitted", "resource_change", int64(c.Handoff.ToEpoch))
		if e != nil {
			return e
		}
		if e = q.AppendHubSameStoreAdmission(ctx, generated.AppendHubSameStoreAdmissionParams{TenantID: scope.TenantID(), RecoveryID: recovery, HubID: c.HubID, FromEpoch: int64(c.Handoff.FromEpoch), ToEpoch: int64(c.Handoff.ToEpoch), FromBootID: c.FromBootID, ToBootID: c.ToBootID, BackendID: c.BackendID, OperationID: op.ID, CreatedBy: scope.ActorID(), Payload: raw, PayloadDigest: hash[:]}); e != nil {
			return e
		}
		out, e = q.GetHubSameStoreAdmission(ctx, recovery)
		return e
	})
	if e != nil {
		return generated.HubSameStoreAdmission{}, e
	}
	return out, nil
}

func validateHubSameStoreAdmission(p generated.HubSameStoreAdmission) (hubSameStoreAdmissionProof, error) {
	var c hubSameStoreAdmissionProof
	h := sha256.Sum256(p.Payload)
	if json.Unmarshal(p.Payload, &c) != nil || !bytes.Equal(h[:], p.PayloadDigest) || c.Version != 1 || c.RecoveryID != p.RecoveryID || c.HubID != p.HubID || c.BackendID != p.BackendID || c.FromBootID != p.FromBootID || c.ToBootID != p.ToBootID || c.FromEpoch != p.FromEpoch || c.ToEpoch != p.ToEpoch {
		return c, ErrShareConflict
	}
	canonical, _ := json.Marshal(c)
	if !bytes.Equal(canonical, p.Payload) {
		return c, ErrShareConflict
	}
	return c, nil
}

func recheckRecoveryNodeWitness(ctx context.Context, q *generated.Queries, a generated.HubFundedAllocation, c hub.SameStoreRecoveryCommand, m hub.SameStoreRecoveryManifest, index int) (hubAdmissionNodeWitness, error) {
	var out hubAdmissionNodeWitness
	f, e := q.GetNodeRecoveryCreditFenceByAllocation(ctx, generated.GetNodeRecoveryCreditFenceByAllocationParams{TenantID: a.TenantID, RecoveryID: c.ID, AllocationID: a.AllocationID})
	if e != nil {
		return out, e
	}
	fence, e := validateNodeRecoveryCreditFence(f)
	if e != nil {
		return out, e
	}
	manifest, _ := json.Marshal(m)
	mh := sha256.Sum256(manifest)
	if fence.ManifestSHA256 != hex.EncodeToString(mh[:]) || fence.SealedSequence != m.Result.SealedSequence || fence.FromEpoch != c.Handoff.FromEpoch || fence.ToEpoch != c.Handoff.ToEpoch || fence.BackendID != c.BackendID || fence.NodeID != a.NodeID || fence.BootID != a.BootID || fence.ParentID != a.DelegationID {
		return out, ErrShareConflict
	}
	receipt, e := q.GetNodeRecoveryCreditFenceReceipt(ctx, generated.GetNodeRecoveryCreditFenceReceiptParams{TenantID: a.TenantID, FenceID: f.ID})
	if e != nil {
		return out, e
	}
	var r store.RecoveryCreditFenceReceipt
	rh := sha256.Sum256(receipt.Payload)
	if json.Unmarshal(receipt.Payload, &r) != nil || !bytes.Equal(rh[:], receipt.PayloadDigest) || len(receipt.PeerFingerprint) != 32 || r.ID != f.ID || r.Version != 1 || r.RecoveryID != c.ID || r.NodeID != a.NodeID || r.BootID != a.BootID || r.BackendID != c.BackendID || r.Digest != hex.EncodeToString(f.PayloadDigest) || r.Installed == nil {
		return out, ErrShareConflict
	}
	canonical, _ := json.Marshal(r)
	if !bytes.Equal(canonical, receipt.Payload) {
		return out, ErrShareConflict
	}
	allowed := map[string]uint64{}
	for _, g := range fence.Credits {
		allowed[g.GrantID] = g.Amount
	}
	last := ""
	for _, g := range r.Installed {
		if g.GrantID <= last || allowed[g.GrantID] != g.Amount || g.Amount == 0 {
			return out, ErrShareConflict
		}
		last = g.GrantID
	}
	partition := m.Result.FundedInventory.Delegations[index]
	if partition.Raw != int64(fence.OriginalRaw) || partition.Issued != int64(fence.Issued) || partition.Raw-partition.Issued != int64(fence.Remaining) {
		return out, ErrShareConflict
	}
	p, e := q.GetNodeRecoverySourcesByAllocation(ctx, generated.GetNodeRecoverySourcesByAllocationParams{TenantID: a.TenantID, RecoveryID: c.ID, AllocationID: a.AllocationID})
	if e != nil {
		return out, e
	}
	roots, e := validateNodeRecoverySources(p)
	if e != nil {
		return out, e
	}
	if p.FenceID != f.ID || p.NodeID != a.NodeID || p.BootID != a.BootID || p.BackendID != c.BackendID || !jsonEqual(roots.Roots, c.Handoff.FundedSuccessors[index].ShareReferences) {
		return out, ErrShareConflict
	}
	installed, e := q.GetNodeRecoverySourceReceipt(ctx, generated.GetNodeRecoverySourceReceiptParams{TenantID: a.TenantID, ProvisionID: p.ID})
	if e != nil {
		return out, e
	}
	var sr store.SourceProvisionReceipt
	sh := sha256.Sum256(installed.Payload)
	if json.Unmarshal(installed.Payload, &sr) != nil || !bytes.Equal(sh[:], installed.PayloadDigest) || len(installed.PeerFingerprint) != 32 || sr.Version != 1 || sr.ID != p.ID || sr.BackendID != c.BackendID || sr.NodeID != a.NodeID || sr.BootID != a.BootID || sr.Digest != hex.EncodeToString(p.PayloadDigest) {
		return out, ErrShareConflict
	}
	canonical, _ = json.Marshal(sr)
	if !bytes.Equal(canonical, installed.Payload) {
		return out, ErrShareConflict
	}
	return hubAdmissionNodeWitness{TenantID: a.TenantID, AllocationID: a.AllocationID, NodeID: a.NodeID, BootID: a.BootID, FenceID: f.ID, ProvisionID: p.ID, FenceSHA256: hex.EncodeToString(f.PayloadDigest), FenceReceiptSHA256: hex.EncodeToString(rh[:]), ProvisionSHA256: hex.EncodeToString(p.PayloadDigest), ProvisionReceiptSHA256: hex.EncodeToString(sh[:])}, nil
}
