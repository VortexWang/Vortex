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

type ShareRenewalClosureWorker struct {
	Service   service.ShareService
	BackendID string
}

func (w ShareRenewalClosureWorker) Once(ctx context.Context) (bool, error) {
	if w.Service.Pool == nil || w.BackendID == "" {
		return false, errors.New("closure dependencies unavailable")
	}
	rows, e := generated.New(w.Service.Pool).ShareClosureCandidates(ctx)
	if e != nil {
		return false, e
	}
	var failures []error
	for _, r := range rows {
		if ctx.Err() != nil {
			return len(rows) > 0, ctx.Err()
		}
		scope, e := service.NewTenantScope(r.CreatedBy, r.TenantID, r.TenantID)
		if e == nil {
			e = w.Service.PrepareRenewalClosures(ctx, scope, r.ID, w.BackendID, uuid.NewString())
		}
		if e != nil {
			failures = append(failures, fmt.Errorf("handoff %s: %w", r.ID, e))
		}
	}
	return len(rows) > 0, errors.Join(failures...)
}
func (w ShareRenewalClosureWorker) Run(ctx context.Context) error {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		if _, e := w.Once(ctx); e != nil && ctx.Err() == nil {
			slog.Error("old issuer closure pending; occupation retained", "error", e)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}
