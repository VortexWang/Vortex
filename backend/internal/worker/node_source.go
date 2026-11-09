package worker

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"vortex/backend/db/generated"
	"vortex/backend/internal/service"
	controlv1 "vortex/contracts/protobuf/v1"
)

type SourceAdapter interface {
	DeliverSources(context.Context, service.TenantScope, string, string, *controlv1.SourceProvisionCommand) error
}
type NodeSourceWorker struct {
	Service   service.ShareService
	BackendID string
	Adapter   SourceAdapter
}

// Candidate rows and immutable bytes are in PG. Concurrent producers may send
// the same finite roots; exact FULL and PG receipts make that harmless. Failed
// transport leaves the task pending and funds held, without a timeout return.
func (w NodeSourceWorker) Once(ctx context.Context) (bool, error) {
	if w.Service.Pool == nil || w.Adapter == nil {
		return false, errors.New("source provisioning dependencies unavailable")
	}
	rows, e := generated.New(w.Service.Pool).NodeSourceCandidates(ctx)
	if e != nil {
		return false, e
	}
	var failures []error
	for _, a := range rows {
		if ctx.Err() != nil {
			return len(rows) > 0, ctx.Err()
		}
		scope, e := service.NewTenantScope(a.CreatedBy, a.TenantID, a.TenantID)
		if e != nil {
			failures = append(failures, e)
			continue
		}
		p, e := w.Service.PrepareNodeSources(ctx, scope, a.AllocationID, w.BackendID, uuid.NewString())
		if e != nil {
			failures = append(failures, e)
			continue
		}
		send, cancel := context.WithTimeout(ctx, 3*time.Second)
		e = w.Adapter.DeliverSources(send, scope, p.NodeID, p.BootID, &controlv1.SourceProvisionCommand{Payload: p.Payload, PayloadSha256: p.PayloadDigest})
		cancel()
		if e != nil {
			failures = append(failures, e)
			continue
		}
	}
	return len(rows) > 0, errors.Join(failures...)
}

func (w NodeSourceWorker) Run(ctx context.Context) error {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		if _, e := w.Once(ctx); e != nil && ctx.Err() == nil {
			slog.Error("Node source provisioning pending; original funds retained", "error", e)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}
