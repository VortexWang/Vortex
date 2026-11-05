package hub

import (
	"context"
	"fmt"
	"time"

	"vortex/agent/store"
	controlv1 "vortex/contracts/protobuf/v1"
)

// Issuer serves only backend-installed delegations. Clock must be restored
// from authenticated time after restart; opening SQLite alone does not restore
// trust. The Phase 6 backend adapter provisions the allocations. Tests use an
// explicitly identified protocol backend at that same boundary.
type Issuer struct {
	Store *store.Store
	HubID string
	Clock func() store.IssuerTime
}

func (i *Issuer) Issue(ctx context.Context, delegationID, nodeID, bootID, requestID string, rawBytes uint64, ttl time.Duration) (*controlv1.AuthorizationUpdate, error) {
	if i == nil || i.Store == nil || i.Clock == nil || i.HubID == "" {
		return nil, fmt.Errorf("hub: durable issuer or trusted clock unavailable")
	}
	result, err := i.Store.IssueHubAuthorization(ctx, store.IssueRequest{ID: requestID, HubID: i.HubID, DelegationID: delegationID, NodeID: nodeID, BootID: bootID, RawBytes: rawBytes, TTL: ttl}, i.Clock())
	if err != nil {
		return nil, err
	}
	return &controlv1.AuthorizationUpdate{Generation: result.Generation, Payload: result.Payload}, nil
}
