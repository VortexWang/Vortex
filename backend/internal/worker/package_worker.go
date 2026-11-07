package worker

import (
	"context"
	"github.com/jackc/pgx/v5/pgxpool"
	"log/slog"
	"time"
	"vortex/backend/internal/service"
)

// PackageWorker consumes durable PostgreSQL change rows. Transient failures
// roll back the apply and leave the same row pending for the next bounded tick.
type PackageWorker struct {
	Pool *pgxpool.Pool
	Now  func() time.Time
}

func (w PackageWorker) Once(ctx context.Context) (bool, error) {
	at := time.Now().UTC()
	if w.Now != nil {
		at = w.Now().UTC()
	}
	return (service.PackageService{Pool: w.Pool}).ProcessDuePackageChanges(ctx, at)
}
func (w PackageWorker) Run(ctx context.Context) error {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		if _, e := w.Once(ctx); e != nil && ctx.Err() == nil {
			slog.Error("package due transaction rolled back; retrying", "error", e)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}
