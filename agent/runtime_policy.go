package agent

import (
	"context"
	"vortex/agent/store"
)

// openRuntimePolicy is the per-OPEN Engine adapter. SQL selection, effective
// boundary and share/credit admission are a single Agent-owned transaction;
// the Engine receives a session whose usage sink pins this immutable value.
// It never mutates Runtime's shared topology snapshot at a billing cutover.
func (n *Node) openRuntimePolicy(ctx context.Context, scope store.AuthorizationUse, id, flow string) (store.RuntimeAuthorization, error) {
	return n.Store.OpenRuntimeSession(ctx, scope, id, flow)
}
