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

type PortOperationWorker struct{ Pool *pgxpool.Pool }

func (w PortOperationWorker) Once(ctx context.Context) (bool, error) {
	rows, e := generated.New(w.Pool).ReadyPortOperationProjections(ctx)
	if e != nil {
		return false, e
	}
	for _, row := range rows {
		changed, e := (service.PortService{Pool: w.Pool}).ReconcilePortOperation(ctx, row.TenantID, row.OperationID, uuid.NewString())
		if errors.Is(e, pgx.ErrNoRows) {
			continue
		}
		if e != nil || changed {
			return changed, e
		}
	}
	return false, nil
}
func (w PortOperationWorker) Run(ctx context.Context) error {
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
