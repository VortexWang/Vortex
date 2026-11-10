package worker

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"vortex/backend/db/generated"
	"vortex/backend/internal/service"
)

type HubCreditSettlementWorker struct{ Service service.ShareService }

func (w HubCreditSettlementWorker) Once(ctx context.Context) (bool, error) {
	if w.Service.Pool == nil {
		return false, errors.New("credit settlement authority unavailable")
	}
	rows, err := generated.New(w.Service.Pool).HubCreditSettlementCandidates(ctx)
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
			err = w.Service.FinalizeHubAllocation(ctx, scope, row.AllocationID, uuid.NewString())
		}
		if err != nil {
			failures = append(failures, err)
		}
	}
	return len(rows) > 0, errors.Join(failures...)
}

func (w HubCreditSettlementWorker) Run(ctx context.Context) error {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		if _, err := w.Once(ctx); err != nil && ctx.Err() == nil {
			slog.Error("parent credit settlement pending; original funds retained", "error", err)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}
