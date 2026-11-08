package control

import (
	"context"
	"errors"
	"vortex/backend/internal/service"
	controlv1 "vortex/contracts/protobuf/v1"
)

func (a *Adapter) DeliverSources(ctx context.Context, scope service.TenantScope, node, boot string, c *controlv1.SourceProvisionCommand) error {
	a.mu.Lock()
	s := a.nodes[node]
	a.mu.Unlock()
	if s == nil {
		return errors.New("source target disconnected")
	}
	r := sourceRequest{command: c, boot: boot, scope: scope, result: make(chan error, 1), ctx: ctx}
	select {
	case s.sources <- r:
	case <-s.done:
		return errors.New("source target disconnected")
	case <-ctx.Done():
		return ctx.Err()
	}
	select {
	case e := <-r.result:
		return e
	case <-s.done:
		return errors.New("source target disconnected")
	case <-ctx.Done():
		return ctx.Err()
	}
}
