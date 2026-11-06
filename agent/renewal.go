package agent

import (
	"context"
	"errors"
	"fmt"
	"time"

	"vortex/agent/store"
	"vortex/contracts"
	controlv1 "vortex/contracts/protobuf/v1"
)

func (c *ControlClient) queueDueRenewals(ctx context.Context, issuer string, config RenewalConfig) error {
	e, ok := c.Node.Engine.(*RuntimeEngine)
	if !ok || e == nil {
		return nil
	}
	if e.activationRequired.Load() && !e.activated.Load() {
		return nil
	}
	low, high, trusted := c.Node.TrustedTime()
	if !trusted {
		return nil
	} // the data plane independently enforces the cutoff
	due, err := c.Node.Store.DueRenewals(ctx, store.AuthorizationUse{NodeID: c.Node.NodeID, BootID: c.Node.BootID, RuleID: e.scope.RuleID, RuleRevision: e.scope.RuleRevision, PathGeneration: e.scope.PathGeneration, FencingGeneration: e.activeFence.Load(), Earliest: low, Latest: high, TrustedTime: true}, issuer, config.Percent)
	if err != nil {
		return err
	}
	for _, candidate := range due {
		r := candidate.Request
		r.RequestId, err = newControlID()
		if err != nil {
			return err
		}
		r.RequestSha256, err = contracts.RenewalDigest(r)
		if err != nil {
			return err
		}
		if err := c.Node.Store.PersistTriggeredRenewal(ctx, issuer, r, candidate.GrantID); err != nil && !errors.Is(err, store.ErrRenewalConflict) {
			return err
		}
	}
	return nil
}

type RenewalConfig struct {
	Percent int
	Retry   time.Duration
}

func (v RenewalConfig) validated() (RenewalConfig, error) {
	if v.Percent == 0 {
		v.Percent = 30
	}
	if v.Retry == 0 {
		v.Retry = time.Second
	}
	if v.Percent < 1 || v.Percent > 100 || v.Retry < time.Second || v.Retry > time.Minute {
		return v, fmt.Errorf("agent: invalid renewal cadence")
	}
	return v, nil
}

func validRenewalCapabilities(caps []string, enabled bool) bool {
	if len(caps) == 1 && caps[0] == "status-v1" {
		return !enabled
	}
	return enabled && len(caps) == 2 && caps[0] == "status-v1" && caps[1] == "renewal-v1"
}

// PersistRenewal queues an immutable intent for the serialized current-session
// sender. It does not select a new issuer on failover or create funding.
func (c *ControlClient) PersistRenewal(ctx context.Context, issuer string, request *controlv1.RenewalRequest) error {
	if c == nil || c.Node == nil || request == nil || request.NodeId != c.Node.NodeID || request.BootId != c.Node.BootID {
		return fmt.Errorf("agent: renewal boot identity mismatch")
	}
	return c.Node.Store.PersistRenewalIntent(ctx, issuer, request)
}

func (c *ControlClient) acceptRenewalResult(ctx context.Context, issuer string, result *controlv1.RenewalResult) error {
	if result == nil {
		return fmt.Errorf("agent: missing renewal result")
	}
	intent, err := c.Node.Store.RenewalIntent(ctx, result.RequestId)
	if err != nil {
		return err
	}
	if intent.IssuerID != issuer || intent.Request.NodeId != c.Node.NodeID || intent.Request.BootId != c.Node.BootID {
		return fmt.Errorf("agent: stale renewal response")
	}
	if _, err := contracts.EncodeRenewalResult(intent.Request, result); err != nil {
		return err
	}
	if result.Code == "ok" {
		payload, err := validateAuthorizationUpdate(result.AuthorizationUpdate.Payload, c.Node.NodeID, c.Node.BootID, result.AuthorizationUpdate.Generation)
		if err != nil {
			return err
		}
		if payload.SchemaVersion != "1.2.0" || payload.RenewalRequestID != result.RequestId || payload.RenewalRequestSHA256 != fmt.Sprintf("%x", intent.Request.RequestSha256) {
			return fmt.Errorf("agent: renewal success lacks approved full correlation")
		}
	}
	if err := c.Node.Store.PersistRenewalResult(ctx, issuer, result); err != nil {
		return err
	}
	if result.Code != "ok" {
		return nil
	}
	if err := c.applyAuthorizationUpdateFrom(ctx, result.AuthorizationUpdate, issuer); err != nil {
		return err
	}
	payload, err := validateAuthorizationUpdate(result.AuthorizationUpdate.Payload, c.Node.NodeID, c.Node.BootID, result.AuthorizationUpdate.Generation)
	if err != nil {
		return err
	}
	if c.Node.clock == nil {
		return fmt.Errorf("agent: authority clock missing")
	}
	if err := c.Node.clock.observe(payload.IssuedAt, c.Node.bootStarted, time.Now()); err != nil {
		return err
	}
	return c.Node.Store.MarkRenewalInstalled(ctx, issuer, c.Node.NodeID, c.Node.BootID, result.RequestId, payload.UpdateID)
}

// Each connection replays the original bounded journal page. The sender calls
// this no more often than the configured retry interval; transport loss never
// changes request IDs, moves ownership, or clears unresolved liabilities.
func (c *ControlClient) sendPendingRenewals(ctx context.Context, stream controlv1.NodeControl_ConnectClient, issuer string, lastSequence *uint64) error {
	intents, err := c.Node.Store.PendingRenewalIntents(ctx, issuer, c.Node.NodeID, c.Node.BootID, "")
	if err != nil {
		return err
	}
	for _, intent := range intents {
		if intent.State == "received" {
			if err := c.acceptRenewalResult(ctx, issuer, intent.Result); err != nil {
				return err
			}
			continue
		}
		seq, err := c.Node.Store.NextControlSequence(ctx)
		if err != nil {
			return err
		}
		if err := stream.Send(&controlv1.NodeMessage{NodeId: c.Node.NodeID, BootId: c.Node.BootID, Sequence: seq, Body: &controlv1.NodeMessage_RenewalRequest{RenewalRequest: intent.Request}}); err != nil {
			return err
		}
		*lastSequence = seq
	}
	return nil
}
