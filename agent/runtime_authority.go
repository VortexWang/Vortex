package agent

import (
	"context"
	"errors"
	"sync/atomic"

	"vortex/agent/store"
	engineaccounting "vortex/engine/accounting"
)

type RuntimeScope struct {
	RuleID                       string
	RuleRevision, PathGeneration uint64
}

type runtimeAuthorityGate struct {
	node    *Node
	scope   RuntimeScope
	clock   AuthorizationClock
	faulted atomic.Bool
}

func (n *Node) newRuntimeAuthorityGate(scope RuntimeScope, clock AuthorizationClock) engineaccounting.Gate {
	return &runtimeAuthorityGate{node: n, scope: scope, clock: clock}
}
func (g *runtimeAuthorityGate) Open(ctx context.Context, flow engineaccounting.Flow) (engineaccounting.Session, error) {
	if e, ok := g.node.Engine.(*RuntimeEngine); ok && e.activationRequired.Load() && !e.activated.Load() {
		return nil, engineaccounting.ErrDenied
	}
	if g.faulted.Load() || flow.Key == "" || flow.RuleID != "" && flow.RuleID != g.scope.RuleID {
		return nil, engineaccounting.ErrDenied
	}
	id, err := authorizationBatchID()
	if err != nil {
		g.faulted.Store(true)
		return nil, err
	}
	low, high, trusted := g.clock()
	auth, err := g.node.Store.OpenRuntimeSession(ctx, store.AuthorizationUse{NodeID: g.node.NodeID, BootID: g.node.BootID, RuleID: g.scope.RuleID, RuleRevision: g.scope.RuleRevision, PathGeneration: g.scope.PathGeneration, Earliest: low, Latest: high, TrustedTime: trusted}, id, flow.Key)
	if err != nil {
		if !errors.Is(err, store.ErrAuthorizationDenied) && !errors.Is(err, store.ErrAuthorityTimeUncertain) {
			g.faulted.Store(true)
		}
		return nil, err
	}
	use := auth.Use
	inner := &authorizationGate{node: g.node, parentFault: &g.faulted, config: AuthorizationGateConfig{SessionID: id, AuthorizationID: use.AuthorizationID, RuntimeLeaseID: use.RuntimeLeaseID, ShareLeaseID: use.ShareLeaseID, RuleID: use.RuleID, RuleRevision: use.RuleRevision, PathGeneration: use.PathGeneration, SnapshotID: use.BillingSnapshotID, Clock: g.clock}}
	return &authorizationSession{gate: inner, flowKey: flow.Key}, nil
}
