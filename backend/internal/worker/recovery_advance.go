package worker

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"time"
	"vortex/backend/db/generated"
	"vortex/backend/internal/service"
)

type RecoveryAdvanceWorker struct{ Service service.ShareService }

func (w RecoveryAdvanceWorker) Once(ctx context.Context) (bool, error) {
	if w.Service.Pool == nil {
		return false, service.ErrConflict
	}
	rows, e := generated.New(w.Service.Pool).PendingRecoveryAdvanceJobs(ctx)
	if e != nil {
		return false, e
	}
	for _, row := range rows {
		worked, e := w.Service.ProcessRecoveryAdvance(ctx, row.TenantID, row.ID, uuid.NewString())
		if errors.Is(e, service.ErrScope) || errors.Is(e, service.ErrConflict) || errors.Is(e, pgx.ErrNoRows) {
			continue
		}
		if e != nil || worked {
			return worked, e
		}
	}
	return false, nil
}
func (w RecoveryAdvanceWorker) Run(ctx context.Context) error {
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
