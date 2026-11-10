package service

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"vortex/backend/db/generated"
	"vortex/backend/internal/policy"
	"vortex/backend/internal/repository"
	"vortex/hub"
)

// RecoveryProgress describes committed PG evidence, never local Hub readiness.
// The caller can resume this explicit platform action after delivery/reconnect.
// Each step commits independently with its own audit/task/Outbox transaction.
// At most 32 missing commands are produced per call; existing delivery workers
// transmit them only after commit. No network I/O occurs in this coordinator.
type RecoveryProgress struct {
	RecoveryID      string `json:"recovery_id"`
	Stage           string `json:"stage"`
	Allocations     int    `json:"allocations"`
	FencesConfirmed int    `json:"fences_confirmed"`
	RootsConfirmed  int    `json:"roots_confirmed"`
}

// AdvanceHubSameStoreRecovery uses the current authenticated platform grant
// scope. Approval.CreatedBy is attribution, not a source of platform privilege.
// It consumes an existing explicit approval; it neither approves signer epochs
// nor substitutes absence/timeout/status for a finite manifest or FULL receipt.
func (s ShareService) AdvanceHubSameStoreRecovery(ctx context.Context, scope TenantScope, recovery, backend, request string) (RecoveryProgress, error) {
	out := RecoveryProgress{RecoveryID: recovery}
	if !scope.Platform() || !scope.Allows(policy.Grant) {
		return RecoveryProgress{}, ErrScope
	}
	if !validID(recovery) || !validID(backend) {
		return RecoveryProgress{}, ErrShareConflict
	}
	var command hub.SameStoreRecoveryCommand
	allocations := []generated.HubFundedAllocation{}
	manifestReady, admitted := false, false
	event := repository.Audit{Action: "hub.advance_same_store", ResourceID: recovery, RequestID: request, Reason: "explicit authenticated platform orchestration read; only existing approval and committed evidence; no local readiness or financial release"}
	e := (repository.AuditWriter{Pool: s.Pool}).WithTransaction(ctx, scope, policy.Grant, &event, func(q *generated.Queries) error {
		r, e := q.HubSameStoreRecoveryByID(ctx, recovery)
		if e != nil {
			return e
		}
		if r.TenantID != scope.TenantID() {
			return ErrScope
		}
		if r.BackendID != backend {
			return ErrShareConflict
		}
		if e = q.LockUsageTuple(ctx, "hub-incarnation/"+r.HubID); e != nil {
			return e
		}
		_, command, e = sameStoreCommand(ctx, q, recovery, r.HubID, r.ToBootID)
		if e != nil {
			return e
		}
		out.Allocations = len(command.Allocations)
		for _, link := range command.Allocations {
			a, e := q.GetHubAllocationGlobal(ctx, link.AllocationID)
			if e != nil {
				return e
			}
			hash := sha256.Sum256(a.Payload)
			if !bytes.Equal(hash[:], a.PayloadDigest) || hex.EncodeToString(hash[:]) != link.PayloadSHA256 || a.HubID != command.HubID || a.DelegationID != link.DelegationID {
				return ErrShareConflict
			}
			allocations = append(allocations, a)
		}
		admission, e := q.GetHubSameStoreAdmission(ctx, recovery)
		if e == nil {
			if _, e = validateHubSameStoreAdmission(admission); e != nil {
				return e
			}
			admitted = true
		} else if !errors.Is(e, pgx.ErrNoRows) {
			return e
		}
		if !admitted {
			if e = s.pendingSameStore(ctx, q, command); e != nil {
				return e
			}
		}
		manifest, e := q.GetHubSameStoreManifest(ctx, recovery)
		if e == nil {
			hash := sha256.Sum256(manifest.Payload)
			if !bytes.Equal(hash[:], manifest.PayloadDigest) || len(manifest.PeerFingerprint) != 32 {
				return ErrShareConflict
			}
			if _, e = hub.DecodeSameStoreManifest(manifest.Payload, command); e != nil {
				return e
			}
			manifestReady = true
		} else if !errors.Is(e, pgx.ErrNoRows) {
			return e
		}
		if admitted && !manifestReady {
			return ErrShareUnknown
		}
		event.After, _ = json.Marshal(struct {
			Allocations int  `json:"allocations"`
			Manifest    bool `json:"manifest_verified"`
			Admitted    bool `json:"pg_admitted"`
		}{out.Allocations, manifestReady, admitted})
		return nil
	})
	if e != nil {
		return RecoveryProgress{}, e
	}
	if admitted {
		out.Stage = "admitted"
		out.FencesConfirmed = out.Allocations
		out.RootsConfirmed = out.Allocations
		return out, nil
	}
	if !manifestReady {
		out.Stage = "awaiting_hub_manifest"
		return out, nil
	}
	q := generated.New(s.Pool)
	budget := 32
	stepRequest := func(step, id string) string {
		return uuid.NewSHA1(uuid.NameSpaceOID, []byte("hub.advance/"+request+"/"+recovery+"/"+step+"/"+id)).String()
	}
	for _, a := range allocations {
		if e = ctx.Err(); e != nil {
			return RecoveryProgress{}, e
		}
		target, e := scope.ForTenant(a.TenantID, policy.Grant)
		if e != nil {
			return RecoveryProgress{}, e
		}
		fence, e := q.GetNodeRecoveryCreditFenceByAllocation(ctx, generated.GetNodeRecoveryCreditFenceByAllocationParams{TenantID: a.TenantID, RecoveryID: recovery, AllocationID: a.AllocationID})
		if errors.Is(e, pgx.ErrNoRows) {
			if budget == 0 {
				continue
			}
			fence, e = s.PrepareNodeRecoveryCreditFence(ctx, target, recovery, a.AllocationID, stepRequest("fence", a.AllocationID))
			budget--
		}
		if e != nil {
			return RecoveryProgress{}, e
		}
		if _, e = validateNodeRecoveryCreditFence(fence); e != nil {
			return RecoveryProgress{}, e
		}
		if _, e = q.GetNodeRecoveryCreditFenceReceipt(ctx, generated.GetNodeRecoveryCreditFenceReceiptParams{TenantID: a.TenantID, FenceID: fence.ID}); errors.Is(e, pgx.ErrNoRows) {
			continue
		}
		if e != nil {
			return RecoveryProgress{}, e
		}
		out.FencesConfirmed++
		roots, e := q.GetNodeRecoverySourcesByAllocation(ctx, generated.GetNodeRecoverySourcesByAllocationParams{TenantID: a.TenantID, RecoveryID: recovery, AllocationID: a.AllocationID})
		if errors.Is(e, pgx.ErrNoRows) {
			if budget == 0 {
				continue
			}
			roots, e = s.PrepareNodeRecoverySources(ctx, target, recovery, a.AllocationID, stepRequest("roots", a.AllocationID))
			budget--
		}
		if e != nil {
			return RecoveryProgress{}, e
		}
		if _, e = validateNodeRecoverySources(roots); e != nil {
			return RecoveryProgress{}, e
		}
		if _, e = q.GetNodeRecoverySourceReceipt(ctx, generated.GetNodeRecoverySourceReceiptParams{TenantID: a.TenantID, ProvisionID: roots.ID}); errors.Is(e, pgx.ErrNoRows) {
			continue
		}
		if e != nil {
			return RecoveryProgress{}, e
		}
		out.RootsConfirmed++
	}
	if out.FencesConfirmed < out.Allocations {
		out.Stage = "awaiting_node_fences"
		return out, nil
	}
	if out.RootsConfirmed < out.Allocations {
		out.Stage = "awaiting_node_roots"
		return out, nil
	}
	if _, e = s.AdmitHubSameStore(ctx, scope, recovery, stepRequest("admit", recovery)); e != nil {
		return RecoveryProgress{}, e
	}
	out.Stage = "admitted"
	return out, nil
}
