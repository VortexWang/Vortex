package worker

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"vortex/backend/db/generated"
	"vortex/backend/internal/service"
)

type DNSWorker struct {
	Pool  *pgxpool.Pool
	Store service.DNSCredentialAccess
}

func (w DNSWorker) Once(ctx context.Context) (bool, error) {
	if w.Pool == nil || w.Store == nil {
		return false, service.ErrConflict
	}
	rows, e := generated.New(w.Pool).PendingDNSJobs(ctx)
	if e != nil {
		return false, e
	}
	for _, row := range rows {
		worked, e := (service.ServingService{Pool: w.Pool}).ProcessDNSJob(ctx, row.TenantID, row.ID, uuid.NewString(), w.Store)
		if errors.Is(e, service.ErrScope) || errors.Is(e, service.ErrConflict) || errors.Is(e, pgx.ErrNoRows) {
			continue
		}
		if e != nil || worked {
			return worked, e
		}
	}
	return false, nil
}
func (w DNSWorker) Run(ctx context.Context) error {
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
