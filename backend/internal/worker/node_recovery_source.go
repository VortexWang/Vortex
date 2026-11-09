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

type NodeRecoverySourceAdapter interface {
	DeliverRecoverySources(context.Context, service.TenantScope, string, string, *controlv1.SourceProvisionCommand) error
}
type NodeRecoverySourceWorker struct {
	Service   service.ShareService
	BackendID string
	Adapter   NodeRecoverySourceAdapter
}

// This worker only delivers existing immutable explicit platform approvals. It
// never approves recovery automatically or rewrites an expired command.
func (w NodeRecoverySourceWorker) Once(ctx context.Context) (bool, error) {
	if w.Service.Pool == nil || w.Adapter == nil {
		return false, errors.New("recovery source provisioning dependencies unavailable")
	}
	rows, e := generated.New(w.Service.Pool).PendingNodeRecoverySources(ctx)
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
			e = w.Adapter.DeliverRecoverySources(send, scope, p.NodeID, p.BootID, &controlv1.SourceProvisionCommand{Payload: p.Payload, PayloadSha256: p.PayloadDigest})
			cancel()
		}
		if e != nil {
			failures = append(failures, e)
		}
	}
	return len(rows) > 0, errors.Join(failures...)
}
func (w NodeRecoverySourceWorker) Run(ctx context.Context) error {
	tick := time.NewTicker(time.Second)
	defer tick.Stop()
	for {
		if _, e := w.Once(ctx); e != nil && ctx.Err() == nil {
			slog.Error("Node recovery source provisioning pending; original occupation retained", "error", e)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-tick.C:
		}
	}
}
