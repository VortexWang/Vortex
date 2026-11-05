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
	if estimate.RequestBytes == 0 {
		return store.IssueRequest{}, estimate, nil
	}
	request, err := n.Store.PrepareDemand(ctx, c.Scope, estimate.RequestBytes, c.TTL)
	return request, estimate, err
}
