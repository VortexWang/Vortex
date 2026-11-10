package worker

import (
	"context"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"time"
	"vortex/backend/db/generated"
	"vortex/backend/internal/repository"
)

type EventRetentionWorker struct {
	Pool  *pgxpool.Pool
	Clock func() time.Time
}

func (w EventRetentionWorker) Once(ctx context.Context) error {
	now := time.Now()
	if w.Clock != nil {
		now = w.Clock()
	}
	cutoff := now.Add(-24 * time.Hour)
	tenants, e := generated.New(w.Pool).DomainEventRetentionCandidates(ctx, pgtype.Timestamptz{Time: cutoff, Valid: true})
	if e != nil {
		return e
	}
	store := repository.DomainEvents{Pool: w.Pool, Clock: w.Clock}
	for _, tenant := range tenants {
		if _, e = store.Prune(ctx, tenant, cutoff); e != nil {
			return e
		}
	}
	return nil
}
func (w EventRetentionWorker) Run(ctx context.Context) error {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		if e := w.Once(ctx); e != nil {
			return e
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}
