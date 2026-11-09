package hub

import (
	"context"
	"fmt"
	"time"

	"google.golang.org/protobuf/proto"
	"vortex/agent/store"
	"vortex/contracts"
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

// Renew binds the typed request to an already authenticated stream identity.
// Neither session metadata nor a caller-supplied delegation creates authority.
func (i *Issuer) Renew(ctx context.Context, nodeID, bootID string, request *controlv1.RenewalRequest) (*controlv1.RenewalResult, error) {
	if i == nil || i.Store == nil || i.Clock == nil || i.HubID == "" {
		return nil, fmt.Errorf("hub: durable issuer or trusted clock unavailable")
	}
	if _, err := contracts.EncodeRenewalRequest(request); err != nil {
		return nil, err
	}
	if request.NodeId != nodeID || request.BootId != bootID {
		return nil, fmt.Errorf("hub: renewal session identity mismatch")
	}
	// Take an immutable copy before handing it to the durable issuer.
	return i.Store.RenewHubAuthorization(ctx, i.HubID, proto.Clone(request).(*controlv1.RenewalRequest), i.Clock())
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

// ResolveDemand is called only after the control server binds nodeID/bootID to
// its authenticated stream. Exactly one response is returned after Store has
// committed an immutable success or definitive non-issuance outcome.
func (i *Issuer) ResolveDemand(ctx context.Context, nodeID, bootID string, demand *controlv1.CreditDemand) (*controlv1.AuthorizationUpdate, *controlv1.CreditDemandDenied, error) {
	if i == nil || i.Store == nil || i.Clock == nil || i.HubID == "" || demand == nil {
		return nil, nil, fmt.Errorf("hub: durable demand issuer unavailable")
	}
	result, err := i.Store.ResolveHubDemand(ctx, store.IssueRequest{ID: demand.GetRequestId(), HubID: i.HubID, DelegationID: demand.GetDelegationId(), NodeID: nodeID, BootID: bootID, RawBytes: demand.GetRawBytes(), TTL: time.Duration(demand.GetTtlSeconds()) * time.Second}, i.Clock())
	if err != nil {
		return nil, nil, err
	}
	if result.Denial == "" {
		return &controlv1.AuthorizationUpdate{Generation: result.Authorization.Generation, Payload: result.Authorization.Payload}, nil, nil
	}
	var reason controlv1.CreditDemandDenial
	switch result.Denial {
	case "invalid_scope":
		reason = controlv1.CreditDemandDenial_CREDIT_DEMAND_DENIAL_INVALID_SCOPE
	case "insufficient_funds":
		reason = controlv1.CreditDemandDenial_CREDIT_DEMAND_DENIAL_INSUFFICIENT_FUNDS
	case "expired":
		reason = controlv1.CreditDemandDenial_CREDIT_DEMAND_DENIAL_EXPIRED
	default:
		return nil, nil, fmt.Errorf("hub: invalid persisted demand denial")
	}
	return nil, &controlv1.CreditDemandDenied{RequestId: demand.GetRequestId(), Reason: reason}, nil
}
