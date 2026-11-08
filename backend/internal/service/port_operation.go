package service

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"

	"github.com/jackc/pgx/v5"
	"vortex/backend/db/generated"
)

// ReconcilePortOperation is an internal evidence projection, not an authority
// mutation or a public receipt endpoint. The immutable binding retains original
// actor attribution; replay never promotes that actor into a current role. It
// changes only aggregate operation/task/Outbox status and its audit/event rows.
func (s PortService) ReconcilePortOperation(ctx context.Context, tenant, operation, request string) (bool, error) {
	if !validID(tenant) || !validID(operation) || request == "" || len(request) > 128 {
		return false, ErrConflict
	}
	worked := false
	e := pgx.BeginTxFunc(ctx, s.Pool, pgx.TxOptions{}, func(tx pgx.Tx) error {
		q := generated.New(tx)
		b, e := q.GetPortOperationBinding(ctx, generated.GetPortOperationBindingParams{TenantID: tenant, OperationID: operation})
		if e != nil {
			return e
		}
		a, e := q.GetPortAllocation(ctx, generated.GetPortAllocationParams{TenantID: tenant, ID: b.AllocationID})
		if e != nil {
			return e
		}
		// Receipt writers use group-before-allocation locking. Holding the same
		// allocation stabilizes every child state/proof included in the projection.
		if _, e = q.LockPortGroup(ctx, a.GroupID); e != nil {
			return e
		}
		if _, e = q.LockPortAllocation(ctx, generated.LockPortAllocationParams{TenantID: tenant, ID: b.AllocationID}); e != nil {
			return e
		}
		task, e := q.LockPortOperationTask(ctx, generated.LockPortOperationTaskParams{TenantID: tenant, OperationID: operation})
		if e != nil {
			return e
		}
		if task.TaskKind != "resource_change" || b.Action != "prepare" {
			return ErrConflict
		}
		op, e := q.GetOperation(ctx, generated.GetOperationParams{TenantID: tenant, ID: operation})
		if e != nil {
			return e
		}
		var envelope struct {
			Action   string `json:"action"`
			Resource string `json:"resource_id"`
			Version  int64  `json:"version"`
		}
		digest := sha256.Sum256(op.Payload)
		if json.Unmarshal(op.Payload, &envelope) != nil || envelope.Action != "port.prepare.requested" || envelope.Resource != b.AllocationID || envelope.Version != b.AllocationVersion || op.Generation != b.AllocationVersion || !bytes.Equal(digest[:], op.RequestSha256) {
			return ErrConflict
		}
		projection, e := q.GetPortOperationProjection(ctx, generated.GetPortOperationProjectionParams{TenantID: tenant, OperationID: operation})
		if e != nil {
			return e
		}
		if !projection.DesiredHeld.Valid {
			return ErrConflict
		}
		if projection.ActualState == projection.DesiredState && projection.ActualHeld == projection.DesiredHeld.Bool {
			return nil
		}
		if e = q.SetPortOperationProjection(ctx, generated.SetPortOperationProjectionParams{TenantID: tenant, ID: operation, State: projection.DesiredState, OccupationHeld: projection.DesiredHeld.Bool}); e != nil {
			return e
		}
		if e = q.ReconcileTask(ctx, generated.ReconcileTaskParams{TenantID: tenant, OperationID: operation, State: projection.DesiredState}); e != nil {
			return e
		}
		if projection.DesiredState == "succeeded" || projection.DesiredState == "failed" {
			if e = q.MarkDelivered(ctx, generated.MarkDeliveredParams{TenantID: tenant, OperationID: operation}); e != nil {
				return e
			}
		}
		if e = q.InsertAudit(ctx, generated.InsertAuditParams{TenantID: tenant, ActorID: b.ActorID, Action: "port.operation.project", ResourceID: b.AllocationID, RequestID: request, Reason: "aggregate only authenticated persisted child proofs; no authority or financial change", BeforeSummary: auditJSON(struct {
			State string `json:"state"`
			Held  bool   `json:"occupation_held"`
		}{projection.ActualState, projection.ActualHeld}), AfterSummary: auditJSON(struct {
			Operation string `json:"operation_id"`
			State     string `json:"state"`
			Held      bool   `json:"occupation_held"`
		}{operation, projection.DesiredState, projection.DesiredHeld.Bool})}); e != nil {
			return e
		}
		worked = true
		return nil
	})
	if e != nil {
		return false, e
	}
	return worked, nil
}
