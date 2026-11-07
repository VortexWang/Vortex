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

type RuleAdapter interface {
	DeliverRule(context.Context, string, string, *controlv1.RuleCommand) (*controlv1.RuleReceipt, error)
}
type RuleWorker struct {
	Pool            *pgxpool.Pool
	Adapter         RuleAdapter
	DeliveryTimeout time.Duration
}

func (w RuleWorker) Once(ctx context.Context) (bool, error) {
	tx, e := w.Pool.Begin(ctx)
	if e != nil {
		return false, e
	}
	defer tx.Rollback(ctx)
	q := generated.New(tx)
	token := pgtype.Text{String: uuid.NewString(), Valid: true}
	task, e := q.LeaseRuleDelivery(ctx, token)
	if errors.Is(e, pgx.ErrNoRows) {
		return false, tx.Commit(ctx)
	}
	if e != nil {
		return false, e
	}
	b, e := q.RuleDelivery(ctx, generated.RuleDeliveryParams{TenantID: task.TenantID, TaskID: task.ID})
	if e != nil {
		return true, e
	}
	d, e := q.LockRuleDeployment(ctx, generated.LockRuleDeploymentParams{TenantID: task.TenantID, ID: b.DeploymentID})
	if e != nil {
		return true, e
	}
	c, node, boot, e := service.RuleCommand(ctx, q, task.TenantID, task.ID)
	if e != nil {
		return true, e
	}
	if e = q.UpdateOperationState(ctx, generated.UpdateOperationStateParams{TenantID: task.TenantID, ID: task.OperationID, State: "running"}); e != nil {
		return true, e
	}
	if _, e = q.MarkRunning(ctx, generated.MarkRunningParams{TenantID: task.TenantID, OperationID: task.OperationID, LeaseToken: token}); e != nil {
		return true, e
	}
	// Lease/running and all authority/outbox facts are durable before transport.
	if e = tx.Commit(ctx); e != nil {
		return true, e
	}
	timeout := w.DeliveryTimeout
	if timeout <= 0 || timeout > 5*time.Second {
		timeout = 3 * time.Second
	}
	delivery, cancel := context.WithTimeout(ctx, timeout)
	receipt, sendErr := w.Adapter.DeliverRule(delivery, node, boot, c)
	cancel()
	if sendErr != nil {
		receipt = &controlv1.RuleReceipt{Phase: c.Phase, TransactionId: c.TransactionId, TaskKey: c.TaskKey, TargetRevision: c.TargetRevision, IdempotencyKey: c.IdempotencyKey, ArtifactSha256: c.ArtifactSha256, FencingGeneration: c.FencingGeneration, State: "unknown", ErrorCode: "transport_unknown"}
	}
	persist, stop := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
	defer stop()
	scope, e := service.NewTenantScope(d.CreatedBy, task.TenantID, task.TenantID)
	if e != nil {
		return true, e
	}
	return true, (service.RuleOrchestrator{Pool: w.Pool}).Receipt(persist, scope, task.ID, receipt, uuid.NewString())
}

func (w RuleWorker) Run(ctx context.Context) error {
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		if _, e := w.Once(ctx); e != nil {
			return e
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}
