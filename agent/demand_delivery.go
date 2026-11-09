package agent

import (
	"context"
	"errors"
	"fmt"
	"time"
	"vortex/agent/store"
	controlv1 "vortex/contracts/protobuf/v1"
)

func (c *ControlClient) sendPendingDemands(ctx context.Context, stream controlv1.NodeControl_ConnectClient, issuer string, cursor *string, sequence *uint64) error {
	if len(c.AdaptiveDemands) > 64 {
		return fmt.Errorf("agent: adaptive demand scope limit exceeded")
	}
	for _, config := range c.AdaptiveDemands {
		if config.Scope.HubID != issuer {
			continue
		}
		if _, _, err := c.Node.PrepareAdaptiveDemand(ctx, config); err != nil {
			// Boot trust must first be established by authenticated time. Do
			// not issue cold authority based solely on the process wall clock.
			if errors.Is(err, store.ErrAuthorityTimeUncertain) || errors.Is(err, store.ErrDemandRetryDeferred) {
				continue
			}
			return err
		}
	}
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
