package service

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"

	"github.com/jackc/pgx/v5"
	"vortex/agent/store"
	"vortex/backend/db/generated"
	"vortex/backend/internal/policy"
	"vortex/backend/internal/repository"
	"vortex/hub"
)

func (s ShareService) PrepareNodeSources(ctx context.Context, scope TenantScope, allocation, backend, request string) (generated.NodeSourceProvision, error) {
	var out generated.NodeSourceProvision
	if !validID(backend) {
		return out, ErrScope
	}
	e := (repository.AuditWriter{Pool: s.Pool}).WithTransaction(ctx, scope, policy.Mutate, &repository.Audit{Action: "node.prepare_sources", ResourceID: allocation, RequestID: request, Reason: "finite funded source roots persisted before direct backend transport; no runtime rights"}, func(q *generated.Queries) error {
		if _, e := q.LockTenant(ctx, scope.TenantID()); e != nil {
			return e
		}
		old, e := q.GetNodeSourceProvision(ctx, generated.GetNodeSourceProvisionParams{TenantID: scope.TenantID(), AllocationID: allocation})
		if e == nil {
			if old.BackendID != backend {
				return ErrShareConflict
			}
			out = old
			return nil
		}
		if !errors.Is(e, pgx.ErrNoRows) {
			return e
		}
		a, e := q.GetHubAllocation(ctx, generated.GetHubAllocationParams{TenantID: scope.TenantID(), AllocationID: allocation})
		if e != nil {
			return e
		}
		decoded, e := hub.DecodeAllocation(a.Payload)
		if e != nil {
			return e
		}
		node, e := q.GetNodeIncarnation(ctx, a.NodeID)
		if e != nil {
			return e
		}
		if node.BootID != a.BootID {
			return ErrShareUnknown
		}
		lo, hi, e := s.bounds()
		if e != nil {
			return e
		}
		if !hi.Before(decoded.ExpiresAt) {
			return ErrLeaseBoundary
		}
		p := store.SourceProvision{Version: 1, ID: allocation, BackendID: backend, NodeID: a.NodeID, BootID: a.BootID, IssuedAt: lo, Roots: decoded.ShareReferences}
		raw, e := json.Marshal(p)
		if e != nil {
			return e
		}
		if _, e = store.DecodeSourceProvision(raw); e != nil {
			return e
		}
		sum := sha256.Sum256(raw)
		op, e := newDomainTask(ctx, q, scope, a.NodeID, "node.provision_sources", "resource_change", int64(decoded.PathGeneration))
		if e != nil {
			return e
		}
		if e = q.AppendNodeSourceProvision(ctx, generated.AppendNodeSourceProvisionParams{TenantID: a.TenantID, AllocationID: allocation, NodeID: a.NodeID, BootID: a.BootID, BackendID: backend, OperationID: op.ID, Payload: raw, PayloadDigest: sum[:]}); e != nil {
			return e
		}
		out, e = q.GetNodeSourceProvision(ctx, generated.GetNodeSourceProvisionParams{TenantID: a.TenantID, AllocationID: allocation})
		return e
	})
	if e != nil {
		return generated.NodeSourceProvision{}, e
	}
	return out, nil
}

func (s ShareService) RecordNodeSources(ctx context.Context, scope TenantScope, r store.SourceProvisionReceipt, request string) error {
	if _, e := (PortService{NodeCA: s.NodeCA}).verifiedNode(ctx, r.NodeID); e != nil {
		return e
	}
	raw, e := json.Marshal(r)
	if e != nil {
		return e
	}
	sum := sha256.Sum256(raw)
	return (repository.AuditWriter{Pool: s.Pool}).WithTransaction(ctx, scope, policy.Mutate, &repository.Audit{Action: "node.sources_installed", ResourceID: r.ID, RequestID: request, Reason: "Node FULL source-root receipt only; preserve all runtime/financial unknown occupation"}, func(q *generated.Queries) error {
		if _, e := q.LockTenant(ctx, scope.TenantID()); e != nil {
			return e
		}
		p, e := q.GetNodeSourceProvision(ctx, generated.GetNodeSourceProvisionParams{TenantID: scope.TenantID(), AllocationID: r.ID})
		if e != nil {
			return e
		}
		if r.Version != 1 || r.NodeID != p.NodeID || r.BootID != p.BootID || r.BackendID != p.BackendID || r.Digest != hex.EncodeToString(p.PayloadDigest) {
			return ErrStaleReceipt
		}
		node, e := q.GetNodeIncarnation(ctx, p.NodeID)
		if e != nil {
			return e
		}
		if node.BootID != p.BootID {
			return ErrShareUnknown
		}
		old, e := q.GetNodeSourceReceipt(ctx, generated.GetNodeSourceReceiptParams{TenantID: scope.TenantID(), AllocationID: r.ID})
		if e == nil {
			if !bytes.Equal(old.Payload, raw) {
				return ErrShareConflict
			}
			return nil
		}
		if !errors.Is(e, pgx.ErrNoRows) {
			return e
		}
		if e = q.AppendNodeSourceReceipt(ctx, generated.AppendNodeSourceReceiptParams{TenantID: scope.TenantID(), AllocationID: r.ID, Payload: raw, PayloadDigest: sum[:]}); e != nil {
			return e
		}
		if e = q.ReconcileTask(ctx, generated.ReconcileTaskParams{TenantID: scope.TenantID(), OperationID: p.OperationID, State: "succeeded"}); e != nil {
			return e
		}
		if e = q.UpdateOperationState(ctx, generated.UpdateOperationStateParams{TenantID: scope.TenantID(), ID: p.OperationID, State: "succeeded"}); e != nil {
			return e
		}
		if e = q.MarkDelivered(ctx, generated.MarkDeliveredParams{TenantID: scope.TenantID(), OperationID: p.OperationID}); e != nil {
			return e
		}
		return domainTask(ctx, q, scope, r.ID, "node.sources_installed", "resource_change", 1)
	})
}
