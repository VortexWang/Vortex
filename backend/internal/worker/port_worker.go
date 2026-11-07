package worker

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"vortex/backend/db/generated"
	"vortex/backend/internal/service"
	controlv1 "vortex/contracts/protobuf/v1"
)

type PortReleaseAdapter interface {
	DeliverPortRelease(context.Context, service.TenantScope, string, string, *controlv1.RuleCommand, service.PortProof) error
}

// PortWorker consumes independent release tasks; the adapter persists successful
// proofs under the real node's authenticated stream context. Transport errors
// can only retain occupation and schedule the same idempotent stop again.
type PortWorker struct {
	Pool    *pgxpool.Pool
	Adapter PortReleaseAdapter
}

func (w PortWorker) Once(ctx context.Context) (bool, error) {
	if w.Adapter == nil {
		return false, errors.New("port adapter required")
	}
	tx, err := w.Pool.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer tx.Rollback(ctx)
	q := generated.New(tx)
	if err = q.ExpirePortReleaseLeases(ctx); err != nil {
		return false, err
	}
	token := pgtype.Text{String: uuid.NewString(), Valid: true}
	t, err := q.LeasePortRelease(ctx, token)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, tx.Commit(ctx)
	}
	if err != nil {
		return false, err
	}
	b, err := q.PortReleaseBinding(ctx, generated.PortReleaseBindingParams{TenantID: t.TenantID, ReleaseTaskID: pgtype.Text{String: t.ID, Valid: true}})
	if errors.Is(err, pgx.ErrNoRows) {
		// A legacy allocation without a proven rule artifact cannot authorize
		// stopping an arbitrary socket. Leave it occupied and visibly unknown.
		if err = q.ReconcileTask(ctx, generated.ReconcileTaskParams{TenantID: t.TenantID, OperationID: t.ID, State: "unknown"}); err != nil {
			return true, err
		}
		if err = q.UpdateOperationState(ctx, generated.UpdateOperationStateParams{TenantID: t.TenantID, ID: t.ID, State: "unknown"}); err != nil {
			return true, err
		}
		return true, tx.Commit(ctx)
	}
	if err != nil {
		return true, err
	}
	op, err := q.LockPortTask(ctx, generated.LockPortTaskParams{TenantID: t.TenantID, ID: t.ID})
	if err != nil {
		return true, err
	}
	c, node, boot, err := service.RuleCommand(ctx, q, t.TenantID, b.PrepareTaskID)
	if err != nil {
		return true, err
	}
	if node != b.NodeID || boot != b.BootID {
		return true, service.ErrConflict
	}
	c.Phase, c.TaskKey, c.IdempotencyKey = "release", t.ID, t.ID
	scope, err := service.NewTenantScope(b.CreatedBy, t.TenantID, t.TenantID)
	if err != nil {
		return true, err
	}
	proof := service.PortProof{AllocationID: b.AllocationID, NodeID: node, BootID: boot, TaskID: t.ID, LeaseToken: token.String, Kind: "release", Revision: op.TargetRevision, Generation: b.Generation}
	if _, err = q.MarkRunning(ctx, generated.MarkRunningParams{TenantID: t.TenantID, OperationID: t.ID, LeaseToken: token}); err != nil {
		return true, err
	}
	if err = q.UpdateOperationState(ctx, generated.UpdateOperationStateParams{TenantID: t.TenantID, ID: t.ID, State: "running"}); err != nil {
		return true, err
	}
	if err = tx.Commit(ctx); err != nil {
		return true, err
	}
	delivery, cancel := context.WithTimeout(ctx, 3*time.Second)
	err = w.Adapter.DeliverPortRelease(delivery, scope, node, boot, c, proof)
	cancel()
	if err == nil {
		return true, nil
	}
	persist, stop := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
	defer stop()
	return true, (service.PortService{Pool: w.Pool}).ReleaseUnknown(persist, scope, proof, uuid.NewString())
}

func (w PortWorker) Run(ctx context.Context) error {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		if _, err := w.Once(ctx); err != nil {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}
