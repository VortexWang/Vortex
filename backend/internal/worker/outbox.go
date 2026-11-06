package worker

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"time"
	"vortex/backend/db/generated"
	"vortex/backend/internal/service"
)

type Adapter interface {
	Deliver(context.Context, generated.Operation) (service.Receipt, error)
}
type Worker struct {
	Pool            *pgxpool.Pool
	Adapter         Adapter
	DeliveryTimeout time.Duration
}

func (w *Worker) Once(ctx context.Context) (bool, error) {
	tx, err := w.Pool.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer tx.Rollback(ctx)
	q := generated.New(tx)
	expired, err := q.ExpireLeases(ctx)
	if err != nil {
		return false, err
	}
	for _, task := range expired {
		if err = q.UpdateOperationState(ctx, generated.UpdateOperationStateParams{TenantID: task.TenantID, ID: task.OperationID, State: "unknown"}); err != nil {
			return false, err
		}
	}
	token := pgtype.Text{String: uuid.NewString(), Valid: true}
	task, err := q.LeaseTask(ctx, token)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, tx.Commit(ctx)
	}
	if err != nil {
		return false, err
	}
	op, err := q.GetOperation(ctx, generated.GetOperationParams{TenantID: task.TenantID, ID: task.OperationID})
	if err != nil {
		return false, err
	}
	if err = q.UpdateOperationState(ctx, generated.UpdateOperationStateParams{TenantID: task.TenantID, ID: op.ID, State: "leased"}); err != nil {
		return false, err
	}
	if err = tx.Commit(ctx); err != nil {
		return false, err
	}
	tx, err = w.Pool.Begin(ctx)
	if err != nil {
		return true, err
	}
	defer tx.Rollback(ctx)
	q = generated.New(tx)
	count, err := q.MarkRunning(ctx, generated.MarkRunningParams{TenantID: task.TenantID, OperationID: op.ID, LeaseToken: token})
	if err != nil || count != 1 {
		return true, err
	}
	if err = q.UpdateOperationState(ctx, generated.UpdateOperationStateParams{TenantID: task.TenantID, ID: op.ID, State: "running"}); err != nil {
		return true, err
	}
	if err = tx.Commit(ctx); err != nil {
		return true, err
	}
	timeout := w.DeliveryTimeout
	if timeout <= 0 || timeout > 5*time.Second {
		timeout = 3 * time.Second
	}
	delivery, cancel := context.WithTimeout(ctx, timeout)
	receipt, deliveryErr := w.Adapter.Deliver(delivery, op)
	cancel()
	// Cancellation of external I/O must not cancel the durable unknown write.
	persist, stop := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
	defer stop()
	tx, beginErr := w.Pool.Begin(persist)
	if beginErr != nil {
		return true, beginErr
	}
	defer tx.Rollback(persist)
	q = generated.New(tx)
	locked, lockErr := q.LockTask(persist, generated.LockTaskParams{TenantID: task.TenantID, OperationID: op.ID})
	if lockErr != nil {
		return true, lockErr
	}
	if locked.LeaseToken != token || (locked.State != "running" && locked.State != "leased") {
		return true, errors.New("task lease lost; outcome requires reconciliation")
	}
	state := "unknown"
	if deliveryErr == nil {
		err = (&service.Tracer{Pool: w.Pool}).ReconcileInTransaction(persist, q, task.TenantID, op.ID, receipt)
		if err == nil {
			state = receipt.State
		} else if !errors.Is(err, service.ErrStaleReceipt) && !errors.Is(err, service.ErrConflict) {
			return true, err
		}
	}
	count, err = q.FinishTask(persist, generated.FinishTaskParams{TenantID: task.TenantID, OperationID: op.ID, LeaseToken: token, State: state})
	if err != nil {
		return true, err
	}
	if count != 1 {
		return true, errors.New("task lease lost; outcome requires reconciliation")
	}
	if err = q.UpdateOperationState(persist, generated.UpdateOperationStateParams{TenantID: task.TenantID, ID: op.ID, State: state}); err != nil {
		return true, err
	}
	if state == "succeeded" || state == "failed" {
		if err = q.MarkDelivered(persist, generated.MarkDeliveredParams{TenantID: task.TenantID, OperationID: op.ID}); err != nil {
			return true, err
		}
	}
	return true, tx.Commit(persist)
}

func (w *Worker) Run(ctx context.Context) error {
	ticker := time.NewTicker(100 * time.Millisecond)
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
