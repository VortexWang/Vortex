package control

import (
	"context"
	"errors"

	"vortex/backend/internal/service"
	controlv1 "vortex/contracts/protobuf/v1"
)

type creditCloseRequest struct {
	command *controlv1.CreditCloseCommand
	boot    string
	scope   service.TenantScope
	result  chan error
	ctx     context.Context
}

// DeliverCreditClose succeeds only after the authenticated stream handler has
// independently verified and durably accepted the child final in PostgreSQL.
func (a *Adapter) DeliverCreditClose(ctx context.Context, scope service.TenantScope, node, boot string, c *controlv1.CreditCloseCommand) error {
	a.mu.Lock()
	s := a.nodes[node]
	a.mu.Unlock()
	if s == nil {
		return errors.New("credit close target disconnected")
	}
	r := creditCloseRequest{command: c, boot: boot, scope: scope, result: make(chan error, 1), ctx: ctx}
	select {
	case s.creditCloses <- r:
	case <-s.done:
		return errors.New("credit close target disconnected")
	case <-ctx.Done():
		return ctx.Err()
	}
	select {
	case err := <-r.result:
		return err
	case <-s.done:
		return errors.New("credit close target disconnected")
	case <-ctx.Done():
		return ctx.Err()
	}
}
