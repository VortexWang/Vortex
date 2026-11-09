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
	if w.Adapter == nil {
		return false, errors.New("rule adapter required")
	}
	if e := w.Recover(ctx); e != nil {
		return false, e
	}
	return w.deliverOnce(ctx)
}

func (w RuleWorker) deliverOnce(ctx context.Context) (bool, error) {
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
	// Operation event triggers reference the tenant too, including query/abort.
	// Never retain a task lock while waiting for that tenant row.
	if e = tx.Commit(ctx); e != nil {
		return true, e
	}
	tx, e = w.Pool.Begin(ctx)
	if e != nil {
		return true, e
	}
	defer tx.Rollback(ctx)
	q = generated.New(tx)
	if _, e = q.LockTenant(ctx, task.TenantID); e != nil {
		return true, e
	}
	locked, err := q.LockTask(ctx, generated.LockTaskParams{TenantID: task.TenantID, OperationID: task.OperationID})
	if err != nil {
		return true, err
	}
	if locked.LeaseToken != token || locked.State != "leased" || !locked.LeaseUntil.Valid || !locked.LeaseUntil.Time.After(time.Now()) {
		return false, tx.Commit(ctx)
	}

	// Only immutable identity is needed here. Holding a deployment lock after
	// leasing a task would invert the head -> deployment -> task receipt order.
	d, e := q.GetRuleDeployment(ctx, generated.GetRuleDeploymentParams{TenantID: task.TenantID, ID: b.DeploymentID})
	if e != nil {
		return true, e
	}
	c, node, boot, e := service.RuleCommand(ctx, q, task.TenantID, task.ID)
	if e != nil {
		// A restriction can commit between lease selection and this current
		// authority check. Nothing has been sent: leave the task pending instead
		// of terminating the backend or inventing an execution failure.
		if (errors.Is(e, service.ErrConflict) || errors.Is(e, service.ErrScope) || errors.Is(e, service.ErrShareUnknown)) && (b.Phase == "prepare" || b.Phase == "commit") {
			if e = q.ReconcileTask(ctx, generated.ReconcileTaskParams{TenantID: task.TenantID, OperationID: task.OperationID, State: "pending"}); e != nil {
				return false, e
			}
			return false, tx.Commit(ctx)
		}
		return true, e
	}
	if e = q.UpdateOperationState(ctx, generated.UpdateOperationStateParams{TenantID: task.TenantID, ID: task.OperationID, State: "running"}); e != nil {
		return true, e
	}
	count, e := q.MarkRunning(ctx, generated.MarkRunningParams{TenantID: task.TenantID, OperationID: task.OperationID, LeaseToken: token})
	if e != nil {
		return true, e
	}
	if count != 1 {
		return false, tx.Rollback(ctx)
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
	return true, (service.RuleOrchestrator{Pool: w.Pool}).ReceiptWithLease(persist, scope, task.ID, token.String, receipt, uuid.NewString())
}

// Recover closes the crash window between durable running and receipt commit.
// It synthesizes uncertainty, never success; the next task queries the same
// transaction and immutable artifact. Timed out revisions use a new rollback.
func (w RuleWorker) Recover(ctx context.Context) error {
	q := generated.New(w.Pool)
	if e := q.ExpireRuleLeases(ctx); e != nil {
		return e
	}
	unknowns, e := q.UnobservedRuleUnknowns(ctx)
	if e != nil {
		return e
	}
	o := service.RuleOrchestrator{Pool: w.Pool}
	for _, task := range unknowns {
		b, e := q.RuleDelivery(ctx, generated.RuleDeliveryParams{TenantID: task.TenantID, TaskID: task.ID})
		if e != nil {
			return e
		}
		d, e := q.RuleDeploymentByInput(ctx, generated.RuleDeploymentByInputParams{TenantID: task.TenantID, RuleID: b.RuleID, RuleRevision: b.RuleRevision, InputDigest: b.InputDigest})
		if e != nil {
			return e
		}
		scope, e := service.NewTenantScope(d.CreatedBy, task.TenantID, task.TenantID)
		if e != nil {
			return e
		}
		c, _, _, e := service.RuleObservationCommand(ctx, q, task.TenantID, task.ID)
		if e != nil {
			return e
		}
		r := &controlv1.RuleReceipt{Phase: c.Phase, TransactionId: c.TransactionId, TaskKey: c.TaskKey, TargetRevision: c.TargetRevision, IdempotencyKey: c.IdempotencyKey, ArtifactSha256: c.ArtifactSha256, FencingGeneration: c.FencingGeneration, State: "unknown", ErrorCode: "worker_restart_unknown"}
		if e = o.ReceiptWithLease(ctx, scope, task.ID, task.LeaseToken.String, r, uuid.NewString()); e != nil && !errors.Is(e, service.ErrConflict) && !errors.Is(e, service.ErrStaleReceipt) {
			return e
		}
	}
	expired, e := q.ExpiredRuleDeployments(ctx)
	if e != nil {
		return e
	}
	for _, d := range expired {
		scope, e := service.NewTenantScope(d.CreatedBy, d.TenantID, d.TenantID)
		if e != nil {
			return e
		}
		if _, e = o.Rollback(ctx, scope, d.ID, uuid.NewString()); e != nil {
			if e = o.FailRollback(ctx, scope, d.ID, uuid.NewString()); e != nil {
				return e
			}
		}
	}
	return nil
}

func (w RuleWorker) Run(ctx context.Context) error {
	if w.Adapter == nil {
		return errors.New("rule adapter required")
	}
	// Recovery scans are global and bounded, not per delivered member. Run on
	// startup and at most once per second while retaining Once's explicit scan
	// semantics for one-shot callers. Expiry still creates uncertainty, never a
	// success or release, and every delivery independently rechecks authority.
	var nextRecovery time.Time
	return runRuleReconciliation(ctx, func(ctx context.Context) (bool, error) {
		if !time.Now().Before(nextRecovery) {
			if err := w.Recover(ctx); err != nil {
				return false, err
			}
			nextRecovery = time.Now().Add(time.Second)
		}
		return w.deliverOnce(ctx)
	})
}
