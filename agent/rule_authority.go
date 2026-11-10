package agent

import (
	"context"
	"sync/atomic"
	"vortex/agent/store"
	"vortex/engine/accounting"
)

// Each immutable rule generation owns its own gate. Existing sessions keep
// their old generation; a listener change cannot substitute billing authority.
type ruleAuthorityGate struct {
	node     *Node
	scope    RuntimeScope
	fence    uint64
	snapshot string
	faulted  atomic.Bool
}

func (g *ruleAuthorityGate) Open(ctx context.Context, flow accounting.Flow) (accounting.Session, error) {
	if g.faulted.Load() || flow.Key == "" || flow.RuleID != "" && flow.RuleID != g.scope.RuleID {
		return nil, accounting.ErrDenied
	}
	id, e := authorizationBatchID()
	if e != nil {
		return nil, e
	}
	low, high, trusted := g.node.TrustedTime()
	auth, e := g.node.openRuntimePolicy(ctx, store.AuthorizationUse{NodeID: g.node.NodeID, BootID: g.node.BootID, RuleID: g.scope.RuleID, RuleRevision: g.scope.RuleRevision, PathGeneration: g.scope.PathGeneration, FencingGeneration: g.fence, BillingSnapshotID: g.snapshot, Earliest: low, Latest: high, TrustedTime: trusted}, id, flow.Key)
	if e != nil {
		return nil, e
	}
	use := auth.Use
	inner := &authorizationGate{node: g.node, parentFault: &g.faulted, config: AuthorizationGateConfig{SessionID: id, AuthorizationID: use.AuthorizationID, RuntimeLeaseID: use.RuntimeLeaseID, ShareLeaseID: use.ShareLeaseID, RuleID: use.RuleID, RuleRevision: use.RuleRevision, PathGeneration: use.PathGeneration, SnapshotID: use.BillingSnapshotID, Clock: g.node.TrustedTime}}
	return &authorizationSession{gate: inner, flowKey: flow.Key}, nil
}
