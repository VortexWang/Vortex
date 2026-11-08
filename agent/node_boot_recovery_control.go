package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"time"

	"vortex/agent/store"
	"vortex/contracts"
	controlv1 "vortex/contracts/protobuf/v1"
)

func (c *ControlClient) recoverNodeBootFromBackend(ctx context.Context, issuer string, command *controlv1.NodeBootRecoveryCommand) (*controlv1.NodeBootRecoveryReceipt, error) {
	if c == nil || c.Node == nil || !canonicalUUID(c.SourceBackendID) || issuer != c.SourceBackendID || command == nil || len(command.Payload) > 64<<10 || len(command.ProtoReflect().GetUnknown()) != 0 || !bytes.Equal(command.PayloadSha256, sha256BytesControl(command.Payload)) {
		return nil, contracts.ErrNodeBootRecovery
	}
	p, e := contracts.DecodeNodeBootRecovery(command.Payload)
	if e != nil || p.BackendID != issuer || p.NodeID != c.Node.NodeID || p.ToBootID != c.Node.BootID || c.Node.clock == nil {
		return nil, contracts.ErrNodeBootRecovery
	}
	// An already FULL immutable command replays even after clock/command expiry.
	// A first installation still needs trusted bounds. An untrusted clock may
	// observe the authenticated response under the existing continuity rules.
	lo, hi, trusted := c.Node.TrustedTime()
	r, e := c.Node.Store.RetireNodeBootShares(ctx, c.Node.NodeID, c.Node.BootID, issuer, command.Payload, store.IssuerTime{Earliest: lo, Latest: hi, Trusted: trusted})
	if errors.Is(e, store.ErrAuthorityTimeUncertain) && !trusted {
		if e = c.Node.clock.observe(p.IssuedAt, c.Node.bootStarted, time.Now()); e != nil {
			return nil, e
		}
		lo, hi, trusted = c.Node.TrustedTime()
		r, e = c.Node.Store.RetireNodeBootShares(ctx, c.Node.NodeID, c.Node.BootID, issuer, command.Payload, store.IssuerTime{Earliest: lo, Latest: hi, Trusted: trusted})
	}
	if e != nil {
		return nil, e
	}
	raw, e := json.Marshal(r)
	if e != nil {
		return nil, e
	}
	return &controlv1.NodeBootRecoveryReceipt{Payload: raw, PayloadSha256: sha256BytesControl(raw)}, nil
}

// fenceRecoveryCreditFromBackend authenticates an independently advertised,
// direct-only backend command. Exact FULL replay precedes clock bootstrap.
func (c *ControlClient) fenceRecoveryCreditFromBackend(ctx context.Context, issuer string, command *controlv1.RecoveryCreditFenceCommand) (*controlv1.RecoveryCreditFenceReceipt, error) {
	if c == nil || c.Node == nil || !canonicalUUID(c.SourceBackendID) || issuer != c.SourceBackendID || command == nil || len(command.Payload) > 1<<20 || len(command.ProtoReflect().GetUnknown()) != 0 || !bytes.Equal(command.PayloadSha256, sha256BytesControl(command.Payload)) {
		return nil, store.ErrSourceProvision
	}
	p, e := store.DecodeRecoveryCreditFence(command.Payload)
	if e != nil || p.BackendID != issuer || p.NodeID != c.Node.NodeID || p.BootID != c.Node.BootID || c.Node.clock == nil {
		return nil, store.ErrSourceProvision
	}
	lo, hi, trusted := c.Node.TrustedTime()
	r, e := c.Node.Store.ApplyRecoveryCreditFence(ctx, c.Node.NodeID, c.Node.BootID, issuer, command.Payload, store.IssuerTime{Earliest: lo, Latest: hi, Trusted: trusted})
	if errors.Is(e, store.ErrAuthorityTimeUncertain) && !trusted {
		if e = c.Node.clock.observe(p.IssuedAt, c.Node.bootStarted, time.Now()); e != nil {
			return nil, e
		}
		lo, hi, trusted = c.Node.TrustedTime()
		r, e = c.Node.Store.ApplyRecoveryCreditFence(ctx, c.Node.NodeID, c.Node.BootID, issuer, command.Payload, store.IssuerTime{Earliest: lo, Latest: hi, Trusted: trusted})
	}
	if e != nil {
		return nil, e
	}
	raw, e := json.Marshal(r)
	if e != nil {
		return nil, e
	}
	return &controlv1.RecoveryCreditFenceReceipt{Payload: raw, PayloadSha256: sha256BytesControl(raw)}, nil
}
