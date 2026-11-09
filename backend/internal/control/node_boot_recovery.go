package control

import (
	"context"
	"errors"
	"vortex/backend/internal/service"
	controlv1 "vortex/contracts/protobuf/v1"
)

type nodeBootRecoveryRequest struct {
	command *controlv1.NodeBootRecoveryCommand
	boot    string
	scope   service.TenantScope
	result  chan error
	ctx     context.Context
}

// DeliverNodeBootRecovery completes only after PG accepts the authenticated FULL
// receipt, never merely after sending a command or receiving transport bytes.
func (a *Adapter) DeliverNodeBootRecovery(ctx context.Context, scope service.TenantScope, node, boot string, c *controlv1.NodeBootRecoveryCommand) error {
	a.mu.Lock()
	s := a.nodes[node]
	a.mu.Unlock()
	if s == nil {
		return errors.New("boot recovery target disconnected")
	}
	r := nodeBootRecoveryRequest{c, boot, scope, make(chan error, 1), ctx}
	select {
	case s.bootRecoveries <- r:
	case <-s.done:
		return errors.New("boot recovery target disconnected")
	case <-ctx.Done():
		return ctx.Err()
	}
	select {
	case e := <-r.result:
		return e
	case <-s.done:
		return errors.New("boot recovery target disconnected")
	case <-ctx.Done():
		return ctx.Err()
	}
}
