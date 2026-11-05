package agent

import (
	"context"
	"fmt"
	"time"
	controlv1 "vortex/contracts/protobuf/v1"
)

func (c *ControlClient) sendPendingDemands(ctx context.Context, stream controlv1.NodeControl_ConnectClient, issuer string, cursor *string, sequence *uint64) error {
	requests, err := c.Node.Store.PendingDemands(ctx, c.Node.NodeID, c.Node.BootID, issuer, *cursor)
	if err != nil {
		return err
	}
	if len(requests) == 0 {
		*cursor = ""
		return nil
	}
	for _, r := range requests {
		if r.TTL < time.Second || r.TTL > 300*time.Second || r.TTL%time.Second != 0 {
			return fmt.Errorf("agent: invalid persisted demand TTL")
		}
		next, err := c.Node.Store.NextControlSequence(ctx)
		if err != nil {
			return err
		}
		if err := stream.Send(&controlv1.NodeMessage{NodeId: c.Node.NodeID, BootId: c.Node.BootID, Sequence: next, Body: &controlv1.NodeMessage_CreditDemand{CreditDemand: &controlv1.CreditDemand{RequestId: r.ID, DelegationId: r.DelegationID, RawBytes: r.RawBytes, TtlSeconds: uint32(r.TTL / time.Second)}}}); err != nil {
			return err
		}
		*sequence = next
		*cursor = r.ID
	}
	return nil
}

func (c *ControlClient) applyDemandDenial(ctx context.Context, issuer string, denied *controlv1.CreditDemandDenied) error {
	if !c.EnableCreditDemand || denied == nil {
		return fmt.Errorf("agent: unexpected demand denial")
	}
	var reason string
	switch denied.GetReason() {
	case controlv1.CreditDemandDenial_CREDIT_DEMAND_DENIAL_INVALID_SCOPE:
		reason = "invalid_scope"
	case controlv1.CreditDemandDenial_CREDIT_DEMAND_DENIAL_INSUFFICIENT_FUNDS:
		reason = "insufficient_funds"
	case controlv1.CreditDemandDenial_CREDIT_DEMAND_DENIAL_EXPIRED:
		reason = "expired"
	default:
		return fmt.Errorf("agent: unspecified or unknown demand denial")
	}
	return c.Node.Store.DenyDemand(ctx, c.Node.NodeID, c.Node.BootID, issuer, denied.GetRequestId(), reason)
}
