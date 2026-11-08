package control

import (
	"context"
	"errors"
	"vortex/backend/internal/service"
	controlv1 "vortex/contracts/protobuf/v1"
)

type nodeRecoveryCreditFenceRequest struct {
	command *controlv1.RecoveryCreditFenceCommand
	boot    string
	scope   service.TenantScope
	result  chan error
	ctx     context.Context
}

// DeliverNodeRecoveryCreditFence completes only after PG accepts the authenticated FULL
// receipt, never merely after sending a command or receiving transport bytes.
func (a *Adapter) DeliverNodeRecoveryCreditFence(ctx context.Context, scope service.TenantScope, node, boot string, c *controlv1.RecoveryCreditFenceCommand) error {
	a.mu.Lock()
	s := a.nodes[node]
	a.mu.Unlock()
	if s == nil {
		return errors.New("recovery credit fence target disconnected")
	}
	r := nodeRecoveryCreditFenceRequest{c, boot, scope, make(chan error, 1), ctx}
	select {
	case s.recoveryCreditFences <- r:
	case <-s.done:
		return errors.New("recovery credit fence target disconnected")
	case <-ctx.Done():
		return ctx.Err()
	}
	select {
	case e := <-r.result:
		return e
	case <-s.done:
		return errors.New("recovery credit fence target disconnected")
	case <-ctx.Done():
		return ctx.Err()
	}
}
