package worker

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"vortex/backend/db/generated"
	"vortex/backend/internal/service"
	controlv1 "vortex/contracts/protobuf/v1"
)

type NodeBootRecoveryAdapter interface {
	DeliverNodeBootRecovery(context.Context, service.TenantScope, string, string, *controlv1.NodeBootRecoveryCommand) error
}
type NodeBootRecoveryWorker struct {
	Service   service.ShareService
	BackendID string
	Adapter   NodeBootRecoveryAdapter
}

// This worker only delivers existing immutable explicit platform approvals. It
// never approves recovery automatically or rewrites an expired command.
func (w NodeBootRecoveryWorker) Once(ctx context.Context) (bool, error) {
	if w.Service.Pool == nil || w.Adapter == nil {
		return false, errors.New("boot recovery dependencies unavailable")
	}
	rows, e := generated.New(w.Service.Pool).PendingNodeBootRecoveries(ctx)
	if e != nil {
		return false, e
	}
	var failures []error
	for _, p := range rows {
		if ctx.Err() != nil {
			return len(rows) > 0, ctx.Err()
		}
		if p.BackendID != w.BackendID {
			continue
		}
		scope, e := service.NewTenantScope(p.CreatedBy, p.TenantID, p.TenantID)
		if e == nil {
			send, cancel := context.WithTimeout(ctx, 3*time.Second)
			e = w.Adapter.DeliverNodeBootRecovery(send, scope, p.NodeID, p.ToBootID, &controlv1.NodeBootRecoveryCommand{Payload: p.Payload, PayloadSha256: p.PayloadDigest})
			cancel()
		}
		if e != nil {
			failures = append(failures, e)
		}
	}
	return len(rows) > 0, errors.Join(failures...)
}
func (w NodeBootRecoveryWorker) Run(ctx context.Context) error {
	tick := time.NewTicker(time.Second)
	defer tick.Stop()
	for {
		if _, e := w.Once(ctx); e != nil && ctx.Err() == nil {
			slog.Error("Node boot retirement pending; original occupation retained", "error", e)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-tick.C:
		}
	}
}
