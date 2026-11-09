package worker

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"vortex/backend/db/generated"
	"vortex/backend/internal/service"
)

type RuntimeFundingWorker struct {
	Service service.ShareService
	Region  string
}

func (w RuntimeFundingWorker) Once(ctx context.Context) (bool, error) {
	if w.Service.Pool == nil || w.Region == "" {
		return false, errors.New("runtime funding dependencies unavailable")
	}
	rows, e := generated.New(w.Service.Pool).ShareSchedulingTenants(ctx)
	if e != nil {
		return false, e
	}
	var failures []error
	for _, row := range rows {
		if ctx.Err() != nil {
			return len(rows) > 0, ctx.Err()
		}
		scope, e := service.NewTenantScope(row.CreatedBy, row.TenantID, row.TenantID)
		if e == nil {
			e = w.Service.FundRuntimeCandidates(ctx, scope, w.Region, uuid.NewString())
		}
		if e != nil {
			failures = append(failures, fmt.Errorf("tenant %s: %w", row.TenantID, e))
		}
	}
	return len(rows) > 0, errors.Join(failures...)
}

func (w RuntimeFundingWorker) Run(ctx context.Context) error {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		if _, e := w.Once(ctx); e != nil && ctx.Err() == nil {
			slog.Error("runtime funding pending; existing funds retained", "error", e)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}
