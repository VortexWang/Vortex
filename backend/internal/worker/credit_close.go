package worker

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"vortex/backend/db/generated"
	"vortex/backend/internal/service"
	controlv1 "vortex/contracts/protobuf/v1"
)

type CreditCloseAdapter interface {
	DeliverCreditClose(context.Context, service.TenantScope, string, string, *controlv1.CreditCloseCommand) error
}

type CreditCloseWorker struct {
	Service   service.ShareService
	BackendID string
	Adapter   CreditCloseAdapter
}

// Concurrent producers replay one immutable PG command. A timeout or unsettled
// receipt retains the task and all original financial occupation.
func (w CreditCloseWorker) Once(ctx context.Context) (bool, error) {
	if w.Service.Pool == nil || w.Adapter == nil {
		return false, errors.New("credit close dependencies unavailable")
	}
	rows, err := generated.New(w.Service.Pool).CreditCloseCandidates(ctx)
	if err != nil {
		return false, err
	}
	var failures []error
	for _, row := range rows {
		if ctx.Err() != nil {
			return len(rows) > 0, ctx.Err()
		}
		scope, err := service.NewTenantScope(row.CreatedBy, row.TenantID, row.TenantID)
		if err == nil {
			var p generated.NodeCreditCloseCommand
			p, err = w.Service.PrepareCreditClose(ctx, scope, row.GrantID, w.BackendID, uuid.NewString())
			if err == nil {
				send, cancel := context.WithTimeout(ctx, 3*time.Second)
				err = w.Adapter.DeliverCreditClose(send, scope, p.NodeID, p.BootID, &controlv1.CreditCloseCommand{Payload: p.Payload, PayloadSha256: p.PayloadDigest})
				cancel()
			}
		}
		if err != nil {
			failures = append(failures, err)
		}
	}
	return len(rows) > 0, errors.Join(failures...)
}

func (w CreditCloseWorker) Run(ctx context.Context) error {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		if _, err := w.Once(ctx); err != nil && ctx.Err() == nil {
			slog.Error("child credit closure pending; original funds retained", "error", err)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}
