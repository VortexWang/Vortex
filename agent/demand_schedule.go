package agent

import (
	"context"
	"fmt"
	"time"
	"vortex/agent/store"
)

// AdaptiveDemandConfig is installed by trusted local provisioning. It cannot
// change delegation scope, and is not itself spending authority. Parameters
// remain explicitly supplied experimental values until qualification locks them.
type AdaptiveDemandConfig struct {
	Scope  store.DemandScope
	Policy DemandPolicyConfig
	TTL    time.Duration
}

func (n *Node) PrepareAdaptiveDemand(ctx context.Context, c AdaptiveDemandConfig) (store.IssueRequest, DemandEstimate, error) {
	if n == nil || n.Store == nil || c.Scope.NodeID != n.NodeID || c.Scope.BootID != n.BootID || c.TTL < time.Second || c.TTL > 300*time.Second || c.TTL%time.Second != 0 {
		return store.IssueRequest{}, DemandEstimate{}, fmt.Errorf("agent: invalid adaptive demand scope")
	}
	policy, err := NewDemandPolicy(c.Policy)
	if err != nil {
		return store.IssueRequest{}, DemandEstimate{}, err
	}
	early, late, trusted := n.TrustedTime()
	ledger, err := n.Store.ReadDemandLedger(ctx, c.Scope, store.IssuerTime{Earliest: early, Latest: late, Trusted: trusted}, time.Now())
	if err != nil {
		return store.IssueRequest{}, DemandEstimate{}, err
	}
	estimate := policy.estimateTotal(ledger.MinuteBytes, ledger.Available)
	// A datagram can exceed the renewal waterline while valid credit is below
	// its indivisible write size. Refill toward the existing target for a live
	// matching waiter; do not enlarge the target or count held/unknown credit.
	n.demandMu.Lock()
	for write := range n.demandWrites {
		if write.scope.RuleID == c.Scope.RuleID && write.scope.RuleRevision == c.Scope.RuleRevision && write.scope.PathGeneration == c.Scope.PathGeneration && ledger.Available < write.bytes && ledger.Available < estimate.Target {
			estimate.RequestBytes = estimate.Target - ledger.Available
			break
		}
	}
	n.demandMu.Unlock()
	if estimate.RequestBytes == 0 {
		return store.IssueRequest{}, estimate, nil
	}
	request, err := n.Store.PrepareAutomaticDemand(ctx, c.Scope, estimate.RequestBytes, c.TTL)
	return request, estimate, err
}
