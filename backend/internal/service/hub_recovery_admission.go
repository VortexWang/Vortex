package service

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"sort"
	"time"

	"github.com/jackc/pgx/v5"
	"vortex/agent/store"
	"vortex/backend/db/generated"
	"vortex/backend/internal/policy"
	"vortex/backend/internal/repository"
)

// HubRecoveryAdmission returns a fresh bounded delivery of an already committed
// explicit platform admission. It cannot create approval, bind a boot, settle
// usage or infer admission from forensic retention. The ordinary funded path
// independently gates every later session and receipt.
func (s ShareService) HubRecoveryAdmission(ctx context.Context, hub, boot, backend, request string) ([]byte, bool, error) {
	if !validID(boot) || !validID(backend) {
		return nil, false, ErrShareConflict
	}
	if _, e := (PortService{NodeCA: s.SignerCA}).verifiedNode(ctx, hub); e != nil {
		return nil, false, e
	}
	boundary, e := generated.New(s.Pool).HubSignerBoundary(ctx, hub)
	if e != nil {
		return nil, false, e
	}
	scope, e := hubScope(hub, boundary.TenantID)
	if e != nil {
		return nil, false, e
	}
	var raw []byte
	available := false
	e = (repository.AuditWriter{Pool: s.Pool}).WithTransaction(ctx, scope, policy.Read, &repository.Audit{Action: "hub.deliver_same_store_admission", ResourceID: hub, RequestID: request, Reason: "fresh authenticated delivery of explicit immutable admission; finite local ACK only, no financial settlement"}, func(q *generated.Queries) error {
		if e := q.LockUsageTuple(ctx, "hub-incarnation/"+hub); e != nil {
			return e
		}
		bound, e := q.LatestHubIncarnation(ctx, hub)
		if e != nil && !errors.Is(e, pgx.ErrNoRows) {
			return e
		}
		missingBinding := errors.Is(e, pgx.ErrNoRows)
		current, e := q.HubSignerBoundary(ctx, hub)
		if e != nil {
			return e
		}
		if current.TenantID != scope.TenantID() {
			return ErrShareConflict
		}
		r, e := q.HubSameStoreRecoveryByTargetBoot(ctx, generated.HubSameStoreRecoveryByTargetBootParams{HubID: hub, ToBootID: boot})
		if errors.Is(e, pgx.ErrNoRows) {
			if !missingBinding && (bound.BootID != boot || bound.SignerEpoch != current.SignerEpoch) {
				return ErrShareUnknown
			}
			available = true // advisory only; fresh/ordinary funded gates still run
			return nil
		}
		if e != nil {
			return e
		}
		if missingBinding || bound.BootID != boot || bound.SignerEpoch != r.ToEpoch || current.SignerEpoch != r.ToEpoch {
			return ErrShareUnknown
		}
		_, c, e := sameStoreCommand(ctx, q, r.ID, hub, boot)
		if e != nil || c.BackendID != backend {
			return ErrShareConflict
		}
		admitted, e := q.GetHubSameStoreAdmission(ctx, r.ID)
		if e != nil {
			return e
		}
		proof, e := validateHubSameStoreAdmission(admitted)
		if e != nil || proof.ApprovalSHA256 != hex.EncodeToString(r.PayloadDigest) || proof.BackendID != backend {
			return ErrShareConflict
		}
		authorities, e := q.HubRecoveryAuthorities(ctx, generated.HubRecoveryAuthoritiesParams{SignerID: hub, ToEpoch: r.ToEpoch})
		if e != nil || !jsonEqual(authorities, proof.Authorities) {
			return ErrShareUnknown
		}
		manifest, e := q.GetHubSameStoreManifest(ctx, r.ID)
		if e != nil || proof.ManifestSHA256 != hex.EncodeToString(manifest.PayloadDigest) {
			return ErrShareConflict
		}
		lo, hi, e := s.bounds()
		if e != nil {
			return e
		}
		deadline := current.AutonomyUntil.Time
		allocations := make([]generated.HubFundedAllocation, 0, len(c.Allocations))
		nodes, tenants := map[string]bool{}, map[string]bool{}
		for _, link := range c.Allocations {
			a, e := q.GetHubAllocationGlobal(ctx, link.AllocationID)
			if e != nil {
				return e
			}
			allocations = append(allocations, a)
			nodes[a.NodeID], tenants[a.TenantID] = true, true
		}
		ordered := func(set map[string]bool) []string {
			keys := make([]string, 0, len(set))
			for key := range set {
				keys = append(keys, key)
			}
			sort.Strings(keys)
			return keys
		}
		for _, node := range ordered(nodes) {
			if e = q.LockNodeAuthorityRead(ctx, node); e != nil {
				return e
			}
		}
		for _, tenant := range ordered(tenants) {
			if _, e = q.LockTenant(ctx, tenant); e != nil {
				return e
			}
		}
		for _, a := range allocations {
			effective, _, e := effectiveHubAllocationTx(ctx, q, a, uint64(r.ToEpoch), boot, lo)
			if e != nil {
				return e
			}
			for _, end := range []time.Time{effective.ExpiresAt, effective.HardDeadline, effective.AutonomyDeadline} {
				if !hi.Before(end) {
					return ErrLeaseBoundary
				}
			}
			if effective.AutonomyDeadline.Before(deadline) {
				deadline = effective.AutonomyDeadline
			}
			node, e := q.GetNodeIncarnation(ctx, a.NodeID)
			if e != nil || node.BootID != a.BootID {
				return ErrShareUnknown
			}
			assigned, e := q.UsageNodeHub(ctx, a.NodeID)
			if e != nil || assigned != hub {
				return ErrShareUnknown
			}
			floor, e := q.LockNodeFencing(ctx, generated.LockNodeFencingParams{TenantID: a.TenantID, NodeID: a.NodeID, BootID: a.BootID})
			if e != nil || floor.RenewalClosed || floor.FencingGeneration > int64(effective.FencingFloor) {
				return ErrShareUnknown
			}
			ts, e := NewTenantScope(scope.ActorID(), a.TenantID, a.TenantID)
			if e != nil {
				return e
			}
			if _, _, e = shareEligibility(ctx, q, ts, PrepareShareInput{NodeID: a.NodeID, BootID: a.BootID, SnapshotID: effective.SnapshotID, PathGeneration: int64(effective.PathGeneration), PolicyUntil: effective.ExpiresAt}, hi); e != nil {
				return e
			}
			bindings, e := q.HubAllocationShareBindings(ctx, generated.HubAllocationShareBindingsParams{TenantID: a.TenantID, AllocationID: a.AllocationID})
			if e != nil || len(bindings) != 2 {
				return ErrShareUnknown
			}
			for _, b := range bindings {
				chain, e := q.LockShareChain(ctx, generated.LockShareChainParams{TenantID: a.TenantID, NodeID: a.NodeID, Kind: b.Kind})
				if e != nil || chain.BootID != a.BootID || chain.ActiveGeneration != b.Generation || chain.PendingGeneration != 0 {
					return ErrShareUnknown
				}
				barrier, e := q.ShareClosureBarrier(ctx, generated.ShareClosureBarrierParams{TenantID: a.TenantID, NodeID: a.NodeID, BootID: a.BootID, Kind: b.Kind, Generation: b.Generation, SignerID: hub})
				if e != nil || barrier {
					return ErrShareUnknown
				}
			}
		}
		if !hi.Before(deadline) {
			return ErrLeaseBoundary
		}
		// Prefix identity is independently hash-checked again by the local FULL
		// consumer. Neither this delivery nor local ACK settles retained usage.
		var manifestBody struct {
			Request store.HandoffRequest
			Result  store.HandoffResult
		}
		if json.Unmarshal(manifest.Payload, &manifestBody) != nil || manifestBody.Result.FundedInventory == nil {
			return ErrShareConflict
		}
		p := store.HubRecoveryAdmission{Version: 1, RecoveryID: r.ID, BackendID: backend, HubID: hub, BootID: boot, FromEpoch: uint64(r.FromEpoch), ToEpoch: uint64(r.ToEpoch), SealedSequence: manifest.SealedSequence, AdmissionSHA256: hex.EncodeToString(admitted.PayloadDigest), ManifestSHA256: proof.ManifestSHA256, PrefixSHA256: manifestBody.Result.FundedInventory.PrefixSHA256, ObservedAt: lo, AutonomyDeadline: deadline}
		raw, e = json.Marshal(p)
		if e != nil {
			return e
		}
		_, e = store.DecodeHubRecoveryAdmission(raw)
		return e
	})
	return raw, available, e
}
