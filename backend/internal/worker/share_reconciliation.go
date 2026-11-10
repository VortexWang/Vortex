package worker

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"log/slog"
	"time"
	"vortex/backend/db/generated"
	"vortex/backend/internal/service"
	controlv1 "vortex/contracts/protobuf/v1"
)

type ShareAdapter interface {
	DeliverShare(context.Context, service.TenantScope, string, string, string, string, *controlv1.ShareCommand) error
}
type ShareReconciliationWorker struct {
	Service         service.ShareService
	Adapter         ShareAdapter
	DeliveryTimeout time.Duration
}

func (w ShareReconciliationWorker) Recover(ctx context.Context) error {
	jobs, e := generated.New(w.Service.Pool).ExpiredShareDeliveries(ctx)
	if e != nil {
		return e
	}
	for _, j := range jobs {
		scope, e := service.NewTenantScope(j.CreatedBy, j.TenantID, j.TenantID)
		if e != nil {
			return e
		}
		if e = w.Service.ShareDeliveryUnknown(ctx, scope, j.ID, j.LeaseToken.String, uuid.NewString()); e != nil {
			return e
		}
	}
	return nil
}
func (w ShareReconciliationWorker) Once(ctx context.Context) (bool, error) {
	if w.Service.Pool == nil || w.Adapter == nil {
		return false, errors.New("share service and transport required")
	}
	if e := w.Recover(ctx); e != nil {
		return false, e
	}
	jobs, e := generated.New(w.Service.Pool).ShareDeliveryCandidates(ctx)
	if e != nil {
		return false, e
	}
	for _, j := range jobs {
		scope, e := service.NewTenantScope(j.CreatedBy, j.TenantID, j.TenantID)
		if e != nil {
			return false, e
		}
		d, e := w.Service.LeaseShareDelivery(ctx, scope, j.ID, uuid.NewString())
		if errors.Is(e, pgx.ErrNoRows) || errors.Is(e, service.ErrShareConflict) {
			continue
		}
		if e != nil {
			return false, e
		}
		timeout := w.DeliveryTimeout
		if timeout <= 0 || timeout > 5*time.Second {
			timeout = 3 * time.Second
		}
		send, cancel := context.WithTimeout(ctx, timeout)
		e = w.Adapter.DeliverShare(send, scope, j.ID, d.Token, d.Binding.NodeID, d.Binding.BootID, &controlv1.ShareCommand{Phase: d.Binding.Phase, Payload: d.Binding.Command, PayloadSha256: d.Binding.CommandDigest})
		cancel()
		if e != nil {
			persist, stop := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
			defer stop()
			return true, w.Service.ShareDeliveryUnknown(persist, scope, j.ID, d.Token, uuid.NewString())
		}
		return true, nil
	}
	return false, nil
}
func (w ShareReconciliationWorker) Run(ctx context.Context) error {
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		if _, e := w.Once(ctx); e != nil && ctx.Err() == nil {
			slog.Error("share reconciliation rolled back; capacity retained", "error", e)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}
