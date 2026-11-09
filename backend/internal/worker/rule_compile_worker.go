package worker

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"vortex/backend/db/generated"
	"vortex/backend/internal/service"
)

type RuleCompileWorker struct{ Pool *pgxpool.Pool }

func (w RuleCompileWorker) Once(ctx context.Context) (bool, error) {
	ready, e := generated.New(w.Pool).ReadyRuleCompiles(ctx)
	if e != nil {
		return false, e
	}
	for _, b := range ready {
		scope, e := service.NewTenantScope(b.CreatedBy, b.TenantID, b.TenantID)
		if e != nil {
			return false, e
		}
		e = (service.RuleService{Pool: w.Pool}).ConsumeCompile(ctx, scope, b.TaskID, uuid.NewString())
		if errors.Is(e, service.ErrConflict) {
			// A stale fixed resource binding is a business task failure, not a
			// reason to terminate every backend listener and unrelated worker.
			e = (service.RuleService{Pool: w.Pool}).RejectCompile(ctx, scope, b.TaskID, uuid.NewString())
		}
		if errors.Is(e, service.ErrRuleRevisionConflict) {
			// Another consumer or a newer desired revision may have won while
			// the failed compilation transaction was being rolled back.
			continue
		}
		return true, e
	}
	return false, nil
}

func (w RuleCompileWorker) Run(ctx context.Context) error {
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
