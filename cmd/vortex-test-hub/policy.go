package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"time"
	"vortex/agent/store"
	controlv1 "vortex/contracts/protobuf/v1"
	"vortex/hub"
)

// Offline allocations here are explicitly funded protocol-test provisions.
// Production backend policy/delegation provisioning belongs to Phase6.
func testPolicyDelivery(ctx context.Context, s *store.Store, issuer *hub.Issuer, path, node, boot string, old *controlv1.AuthorizationUpdate) (*controlv1.AuthorizationUpdate, error) {
	var fixture struct {
		Delegation store.HubDelegation
		Transition store.PolicyTransition
	}
	raw, err := os.ReadFile(path)
	if err != nil || len(raw) > 1<<20 {
		return nil, fmt.Errorf("invalid policy fixture")
	}
	if err = json.Unmarshal(raw, &fixture); err != nil {
		return nil, err
	}
	d := fixture.Delegation
	d.BootID = boot
	if d.NodeID != node || d.HubID != issuer.HubID {
		return nil, store.ErrAuthorizationDenied
	}
	digest := sha256.Sum256([]byte(d.ID + "/" + boot))
	id := hex.EncodeToString(digest[:16])
	d.ID = id[:8] + "-" + id[8:12] + "-" + id[12:16] + "-" + id[16:20] + "-" + id[20:]
	if err = s.InstallHubDelegation(ctx, d, issuer.Clock()); err != nil {
		return nil, err
	}
	next, err := issuer.Issue(ctx, d.ID, node, boot, d.ID, 8192, time.Minute)
	if err != nil {
		return nil, err
	}
	var before, after map[string]json.RawMessage
	if json.Unmarshal(old.Payload, &before) != nil || json.Unmarshal(next.Payload, &after) != nil {
		return nil, fmt.Errorf("invalid issued policy bundle")
	}
	var entries, more []map[string]any
	var refs, other []store.ShareReference
	if json.Unmarshal(before["authorizations"], &entries) != nil || json.Unmarshal(after["authorizations"], &more) != nil || json.Unmarshal(before["share_references"], &refs) != nil || json.Unmarshal(after["share_references"], &other) != nil {
		return nil, fmt.Errorf("invalid issued policy references")
	}
	p := fixture.Transition
	for _, r := range more {
		id, _ := r["authorization_id"].(string)
		switch r["kind"] {
		case "credit_grant":
			p.FutureCreditID = id
		case "runtime_lease":
			p.FutureRuntimeID = id
		case "share_lease":
			p.FutureShareID = id
		}
	}
	if err = store.ValidatePolicyTransition(p); err != nil {
		return nil, err
	}
	before["authorizations"], _ = json.Marshal(append(entries, more...))
	before["share_references"], _ = json.Marshal(append(refs, other...))
	before["policy_transitions"], _ = json.Marshal([]store.PolicyTransition{p})
	before["update_id"] = after["update_id"]
	raw, err = json.Marshal(before)
	if err != nil {
		return nil, err
	}
	return &controlv1.AuthorizationUpdate{Generation: old.Generation, Payload: raw}, nil
}
