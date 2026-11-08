package agent

import (
	"encoding/json"
	"errors"
	"testing"
	"time"

	"vortex/agent/store"
	controlv1 "vortex/contracts/protobuf/v1"
	"vortex/engine/accounting"
)

func issuedClosureGrant(t *testing.T, f *renewalSafetyFixture) string {
	t.Helper()
	p, err := validateAuthorizationUpdate(f.result.AuthorizationUpdate.Payload, f.node.NodeID, f.node.BootID, f.result.AuthorizationUpdate.Generation)
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range p.Authorizations {
		if a.Kind == "credit_grant" {
			return a.AuthorizationID
		}
	}
	t.Fatal("fixture omitted credit grant")
	return ""
}

func TestIssuedCreditClosureValidatesCurrentBootAndOriginalHub(t *testing.T) {
	f := newRenewalSafetyFixture(t)
	grant := issuedClosureGrant(t, f)
	if _, err := f.node.CloseIssuedCreditGrant(f.ctx, "550e8400-e29b-41d4-a716-000000000999", f.result.AuthorizationUpdate, grant); !errors.Is(err, store.ErrAuthorizationDenied) {
		t.Fatalf("another Hub manufactured closure: %v", err)
	}
	var body map[string]any
	if err := json.Unmarshal(f.result.AuthorizationUpdate.Payload, &body); err != nil {
		t.Fatal(err)
	}
	body["boot_id"] = "550e8400-e29b-41d4-a716-000000000998"
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.node.CloseIssuedCreditGrant(f.ctx, f.delegation.HubID, &controlv1.AuthorizationUpdate{Generation: 1, Payload: raw}, grant); err == nil {
		t.Fatal("current Node attested another boot never installed credit")
	}
	final, err := f.node.CloseIssuedCreditGrant(f.ctx, f.delegation.HubID, f.result.AuthorizationUpdate, grant)
	if err != nil || !final.NeverInstalled || final.Used != 0 || final.Unused != 4096 || final.Grant.SourceID != f.delegation.SourceID || final.Grant.DelegationID != f.delegation.ID {
		t.Fatalf("original issue final=%+v %v", final, err)
	}
	// A late authentic renewal result may be retained as revoked history or be
	// denied before installation; neither can undo the durable zero-use fence.
	err = f.client.acceptRenewalResult(f.ctx, f.delegation.HubID, f.result)
	if err != nil && !errors.Is(err, store.ErrAuthorizationDenied) {
		t.Fatal(err)
	}
	replay, err := f.node.CloseIssuedCreditGrant(f.ctx, f.delegation.HubID, f.result.AuthorizationUpdate, grant)
	if err != nil || replay != final {
		t.Fatalf("late result changed closure: %+v %v", replay, err)
	}
	low, high, trusted := f.node.TrustedTime()
	if _, err := f.agent.CheckStoredAuthorization(f.ctx, store.AuthorizationUse{NodeID: f.node.NodeID, BootID: f.node.BootID, AuthorizationID: grant, Kind: "credit_grant", RuleID: f.delegation.RuleID, RuleRevision: 1, PathGeneration: 1, BillingSnapshotID: f.delegation.BillingSnapshotID, Earliest: low, Latest: high, TrustedTime: trusted}); !errors.Is(err, store.ErrAuthorizationDenied) {
		t.Fatalf("late result made closed grant executable: %v", err)
	}
}

func TestIssuedCreditClosureCannotReclassifyInstalledGrant(t *testing.T) {
	f := newRenewalSafetyFixture(t)
	grant := issuedClosureGrant(t, f)
	if err := f.client.acceptRenewalResult(f.ctx, f.delegation.HubID, f.result); err != nil {
		t.Fatal(err)
	}
	// The consumer must use the installed grant path, not generate an absence
	// witness merely because its original command/result transport was retried.
	final, err := f.node.CloseIssuedCreditGrant(f.ctx, f.delegation.HubID, f.result.AuthorizationUpdate, grant)
	if err != nil || final.NeverInstalled || final.Used != 0 || final.Unused != 4096 {
		t.Fatalf("installed grant misclassified as absent: %+v %v", final, err)
	}
}

func TestIssuedCreditClosureTypedUnknownAckCannotProduceFinal(t *testing.T) {
	f := newRenewalSafetyFixture(t)
	grant := issuedClosureGrant(t, f)
	if err := f.client.acceptRenewalResult(f.ctx, f.delegation.HubID, f.result); err != nil {
		t.Fatal(err)
	}
	p, err := validateAuthorizationUpdate(f.result.AuthorizationUpdate.Payload, f.node.NodeID, f.node.BootID, 1)
	if err != nil {
		t.Fatal(err)
	}
	instant := p.IssuedAt.Add(time.Millisecond)
	gate, err := f.node.NewAuthorizationGate(AuthorizationGateConfig{AuthorizationID: grant, RuleID: f.delegation.RuleID, RuleRevision: 1, PathGeneration: 1, SnapshotID: f.delegation.BillingSnapshotID, Clock: func() (time.Time, time.Time, bool) { return instant, instant, true }})
	if err != nil {
		t.Fatal(err)
	}
	session, err := gate.Open(f.ctx, accounting.Flow{Key: "uncertain-write", RuleID: f.delegation.RuleID})
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	batch, err := session.Reserve(f.ctx, accounting.LeftToRight, 64)
	if err != nil {
		t.Fatal(err)
	}
	if err := batch.MarkUnknown(f.ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := f.node.CloseIssuedCreditGrant(f.ctx, f.delegation.HubID, f.result.AuthorizationUpdate, grant); !errors.Is(err, store.ErrCreditGrantUnsettled) {
		t.Fatalf("unknown grant produced final: %v", err)
	}
	unknown, err := f.agent.PendingUnknownUsage(f.ctx, f.node.NodeID)
	if err != nil || len(unknown) != 1 {
		t.Fatalf("unknown=%+v %v", unknown, err)
	}
	if err := f.agent.AcknowledgeUnknownUsage(f.ctx, f.node.NodeID, unknown[0].BatchID, unknown[0].Digest); err != nil {
		t.Fatal(err)
	}
	if _, err := f.node.CloseIssuedCreditGrant(f.ctx, f.delegation.HubID, f.result.AuthorizationUpdate, grant); !errors.Is(err, store.ErrCreditGrantUnsettled) {
		t.Fatalf("transport ACK finalized unknown: %v", err)
	}
	balance, err := f.agent.AuditAuthorizationBalance(f.ctx, f.node.NodeID, grant)
	if err != nil || balance.Unknown != 64 || balance.Remaining != 4032 || balance.Reserved != 0 {
		t.Fatalf("unknown liability changed: %+v %v", balance, err)
	}
}
