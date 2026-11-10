package service

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"vortex/backend/db/generated"
)

// OperationState describes only this port management stage, never Engine
// activation or financial release. Public adapters explicitly project fields.
type PortMutationResult struct {
	generated.PortAllocation
	OperationID    string
	OperationState string
}

func confirmedPortProofs(ctx context.Context, q *generated.Queries, scope TenantScope, a generated.PortAllocation, rows []generated.PortReservation) error {
	for _, r := range rows {
		proof, e := q.GetPortProof(ctx, generated.GetPortProofParams{TenantID: scope.TenantID(), AllocationID: a.ID, NodeID: r.NodeID, TaskID: r.PrepareTaskID})
		if errors.Is(e, pgx.ErrNoRows) {
			return ErrConflict
		}
		if e != nil {
			return e
		}
		task, e := q.LockPortTask(ctx, generated.LockPortTaskParams{TenantID: scope.TenantID(), ID: r.PrepareTaskID})
		if e != nil {
			return e
		}
		if proof.ProofKind != "prepare" || proof.BootID != r.BootID || proof.Generation != r.Generation || proof.Revision != task.TargetRevision || task.Generation != r.Generation || task.TaskKind != "port_prepare" {
			return ErrConflict
		}
	}
	return nil
}

// New public actions require the original frozen inventory. Historical unknown
// allocations without a binding remain held and cannot invent a smaller count.
func portIntentInventory(ctx context.Context, q *generated.Queries, scope TenantScope, a generated.PortAllocation, rows []generated.PortReservation) (int32, error) {
	count, e := q.PortOriginalInventory(ctx, generated.PortOriginalInventoryParams{TenantID: scope.TenantID(), AllocationID: a.ID})
	if errors.Is(e, pgx.ErrNoRows) {
		return 0, ErrConflict
	}
	if e != nil {
		return 0, e
	}
	if count < 1 || int(count) != len(rows) {
		return 0, ErrConflict
	}
	return count, nil
}
func appendPortIntent(ctx context.Context, q *generated.Queries, scope TenantScope, a generated.PortAllocation, action string, count int32, complete bool) (string, error) {
	op, e := newDomainTask(ctx, q, scope, a.ID, "port."+action+".requested", "resource_change", a.Version)
	if e != nil {
		return "", e
	}
	if e = q.InsertPortOperationBinding(ctx, generated.InsertPortOperationBindingParams{TenantID: scope.TenantID(), AllocationID: a.ID, OperationID: op.ID, Action: action, AllocationVersion: a.Version, ActorID: scope.ActorID(), ExpectedReservations: count}); e != nil {
		return "", e
	}
	if complete {
		if e = q.SetPortOperationProjection(ctx, generated.SetPortOperationProjectionParams{TenantID: scope.TenantID(), ID: op.ID, State: "succeeded", OccupationHeld: true}); e != nil {
			return "", e
		}
		if e = q.ReconcileTask(ctx, generated.ReconcileTaskParams{TenantID: scope.TenantID(), OperationID: op.ID, State: "succeeded"}); e != nil {
			return "", e
		}
		if e = q.MarkDelivered(ctx, generated.MarkDeliveredParams{TenantID: scope.TenantID(), OperationID: op.ID}); e != nil {
			return "", e
		}
	}
	return op.ID, nil
}
