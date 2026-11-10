package store

import (
	"bytes"
	"context"
	"encoding/json"
	"path/filepath"
	"testing"
	"vortex/contracts"
)

func closureFixture(d HubDelegation, ids []string) contracts.ShareRenewalClosure {
	return contracts.ShareRenewalClosure{Version: 1, ID: "550e8400-e29b-41d4-a716-446655440800", BackendID: "550e8400-e29b-41d4-a716-446655440801", TenantID: "550e8400-e29b-41d4-a716-446655440802", HandoffID: "550e8400-e29b-41d4-a716-446655440803", HubID: d.HubID, HubBootID: "550e8400-e29b-41d4-a716-446655440804", NodeID: d.NodeID, BootID: d.BootID, Kind: "connections", Generation: 3, SuccessorGeneration: 4, SignerEpoch: int64(d.SignerEpoch), Part: 1, Parts: 1, DelegationIDs: ids}
}
func TestShareRenewalClosureFULLRollbackReopenLateInstallAndExistingIssueReplay(t *testing.T) {
	s, d, clock, path := issuerFixture(t)
	ctx := context.Background()
	request := issuerRequest(d, 200)
	issued, e := s.IssueHubAuthorization(ctx, request, clock)
	if e != nil {
		t.Fatal(e)
	}
	late := d
	late.ID = "550e8400-e29b-41d4-a716-446655440900"
	c := closureFixture(d, []string{d.ID, late.ID})
	raw, _ := json.Marshal(c)
	// Reject before COMMIT; updates and both tombstones must roll back together.
	if _, e = s.db.Exec(`CREATE TRIGGER fail_closure BEFORE INSERT ON share_renewal_closures BEGIN SELECT RAISE(ABORT,'disk write failure'); END;`); e != nil {
		t.Fatal(e)
	}
	if _, e = s.ApplyShareRenewalClosure(ctx, c.HubID, c.HubBootID, c.BackendID, raw); e == nil {
		t.Fatal("failed FULL write produced receipt")
	}
	var state string
	var count int
	if e = s.db.QueryRow(`SELECT state FROM issuer_delegations WHERE delegation_id=?`, d.ID).Scan(&state); e != nil || state != "open" {
		t.Fatal(e, state)
	}
	if e = s.db.QueryRow(`SELECT count(*) FROM closed_share_delegations`).Scan(&count); e != nil || count != 0 {
		t.Fatal(e, count)
	}
	if _, e = s.db.Exec(`DROP TRIGGER fail_closure`); e != nil {
		t.Fatal(e)
	}
	first, e := s.ApplyShareRenewalClosure(ctx, c.HubID, c.HubBootID, c.BackendID, raw)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.IssueHubAuthorization(ctx, issuerRequest(d, 201), clock); e == nil {
		t.Fatal("new issue after closure")
	}
	replay, e := s.IssueHubAuthorization(ctx, request, clock)
	if e != nil || !bytes.Equal(replay.Payload, issued.Payload) {
		t.Fatal("old bytes replay changed", e)
	}
	reopened, e := Open(path, "hub")
	if e != nil {
		t.Fatal(e)
	}
	defer reopened.Close()
	second, e := reopened.ApplyShareRenewalClosure(ctx, c.HubID, c.HubBootID, c.BackendID, raw)
	if e != nil || first != second {
		t.Fatal("lost receipt replay", e)
	}
	if e = reopened.InstallHubDelegation(ctx, late, clock); e == nil {
		t.Fatal("late install resurrected rights")
	}
	if e = reopened.InstallHubDelegation(ctx, d, clock); e == nil {
		t.Fatal("closed delegation reinstalled")
	}
	b, e := reopened.AuditHubDelegation(ctx, d.ID)
	if e != nil || b.Remaining != 768 || b.Issued != 256 {
		t.Fatal(e, b)
	}
	changed := c
	changed.Kind = "rate"
	changedRaw, _ := json.Marshal(changed)
	if _, e = reopened.ApplyShareRenewalClosure(ctx, c.HubID, c.HubBootID, c.BackendID, changedRaw); e == nil {
		t.Fatal("command rebound")
	}
}
func TestShareRenewalClosureRejectsWrongScopeWithoutPartialClosure(t *testing.T) {
	s, d, _, _ := issuerFixture(t)
	ctx := context.Background()
	c := closureFixture(d, []string{d.ID})
	for _, change := range []func(*contracts.ShareRenewalClosure){func(c *contracts.ShareRenewalClosure) { c.BackendID = c.ID }, func(c *contracts.ShareRenewalClosure) { c.HubBootID = c.ID }, func(c *contracts.ShareRenewalClosure) { c.NodeID = c.ID }, func(c *contracts.ShareRenewalClosure) { c.BootID = c.ID }, func(c *contracts.ShareRenewalClosure) { c.SignerEpoch++ }, func(c *contracts.ShareRenewalClosure) { c.HubID = c.ID }} {
		bad := c
		change(&bad)
		raw, _ := json.Marshal(bad)
		if _, e := s.ApplyShareRenewalClosure(ctx, c.HubID, c.HubBootID, c.BackendID, raw); e == nil {
			t.Fatal("wrong scope closed delegation")
		}
	}
	agent, e := Open(filepath.Join(t.TempDir(), "agent.db"), "agent")
	if e != nil {
		t.Fatal(e)
	}
	defer agent.Close()
	raw, _ := json.Marshal(c)
	if _, e = agent.ApplyShareRenewalClosure(ctx, c.HubID, c.HubBootID, c.BackendID, raw); e == nil {
		t.Fatal("agent accepted hub closure")
	}
	var state string
	if e = s.db.QueryRow(`SELECT state FROM issuer_delegations WHERE delegation_id=?`, d.ID).Scan(&state); e != nil || state != "open" {
		t.Fatal(e, state)
	}
}
