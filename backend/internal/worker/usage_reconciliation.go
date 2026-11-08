package worker

import (
	"context"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"log/slog"
	"time"
	"vortex/backend/db/generated"
	"vortex/backend/internal/service"
)

type UsageReconciliationWorker struct {
	Pool *pgxpool.Pool
	Now  func() time.Time
}

func (w UsageReconciliationWorker) Once(ctx context.Context) (bool, error) {
	at := time.Now().UTC()
	if w.Now != nil {
		at = w.Now().UTC()
	}
	s := service.MeteringService{Pool: w.Pool}
	if worked, e := s.DetectExpiredUsage(ctx, at); e != nil || worked {
		return worked, e
	}
	if worked, e := (service.LedgerService{Pool: w.Pool}).ProcessSourceLedgerLifecycle(ctx, at); e != nil || worked {
		return worked, e
	}
	jobs, e := generated.New(w.Pool).DueUsageReconciliation(ctx, pgtype.Timestamptz{Time: at, Valid: true})
	if e != nil || len(jobs) == 0 {
		return false, e
	}
	j := jobs[0]
	e = s.ReconcilePending(ctx, j.TenantID, j.GrantID, at)
	return e == nil, e
}
func (w UsageReconciliationWorker) Run(ctx context.Context) error {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		if _, e := w.Once(ctx); e != nil && ctx.Err() == nil {
			slog.Error("usage reconciliation rolled back; liability retained", "error", e)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}
