package store

import (
	"bytes"
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"
)

func TestRuntimeLeaseRefreshCadenceFullRollbackReplayAndNoCredit(t *testing.T) {
	s, d, clock, path := renewalIssuerFixture(t)
	ctx := context.Background()
	if _, e := s.RefreshHubDelegationLeases(ctx, d.HubID, d.NodeID, d.BootID, d.ID, clock); e == nil {
		t.Fatal("lease refresh bootstrapped unfunded initial issue")
	}
	initial, e := s.BootstrapHubAuthorizations(ctx, d.HubID, d.NodeID, d.BootID, "", clock)
	if e != nil || len(initial.Updates) != 1 {
		t.Fatal(initial, e)
	}
	before, e := s.AuditHubDelegation(ctx, d.ID)
	if e != nil {
		t.Fatal(e)
	}
	clock.Earliest = clock.Latest.Add(59 * time.Second)
	clock.Latest = clock.Earliest.Add(time.Second)
	if p, e := s.RefreshHubDelegationLeases(ctx, d.HubID, d.NodeID, d.BootID, d.ID, clock); e != nil || len(p.Payload) > 0 {
		t.Fatal("upper bound cannot prove60s", e)
	}
	clock.Earliest = clock.Earliest.Add(time.Second)
	clock.Latest = clock.Earliest.Add(time.Second)
	if _, e = s.db.Exec(`CREATE TRIGGER fail_lease_journal BEFORE INSERT ON hub_ledger WHEN NEW.kind='runtime-lease' BEGIN SELECT RAISE(ABORT,'injected FULL failure'); END`); e != nil {
		t.Fatal(e)
	}
	if _, e = s.RefreshHubDelegationLeases(ctx, d.HubID, d.NodeID, d.BootID, d.ID, clock); e == nil {
		t.Fatal("failed FULL journal returned authority")
	}
	var count int
	if e = s.db.QueryRow(`SELECT count(*) FROM issuer_lease_refreshes`).Scan(&count); e != nil || count != 0 {
		t.Fatal("partial lease after journal failure", count, e)
	}
	if _, e = s.db.Exec(`DROP TRIGGER fail_lease_journal`); e != nil {
		t.Fatal(e)
	}
	var wg sync.WaitGroup
	updates := make(chan IssuedAuthorization, 8)
	for range 8 {
		wg.Go(func() {
			p, e := s.RefreshHubDelegationLeases(ctx, d.HubID, d.NodeID, d.BootID, d.ID, clock)
			if e != nil {
				t.Error(e)
			}
			updates <- p
		})
	}
	wg.Wait()
	close(updates)
	var first IssuedAuthorization
	for p := range updates {
		if len(p.Payload) == 0 {
			t.Fatal("missing refresh")
		}
		if first.Payload == nil {
			first = p
		}
		if !bytes.Equal(p.Payload, first.Payload) {
			t.Fatal("concurrent refresh changed immutable bytes")
		}
	}
	var envelope struct {
		UpdateID       string    `json:"update_id"`
		IssuedAt       time.Time `json:"issued_at"`
		Authorizations []struct {
			Kind     string    `json:"kind"`
			SourceID string    `json:"source_id"`
			ParentID string    `json:"parent_authorization_id"`
			Amount   *uint64   `json:"amount_bytes"`
			IssuedAt time.Time `json:"issued_at"`
			Expires  time.Time `json:"expires_at"`
			Hard     time.Time `json:"hard_deadline"`
		} `json:"authorizations"`
	}
	if e = json.Unmarshal(first.Payload, &envelope); e != nil || len(envelope.Authorizations) != 2 {
		t.Fatal(e)
	}
	seen := map[string]bool{}
	for _, a := range envelope.Authorizations {
		if seen[a.Kind] || (a.Kind != "runtime_lease" && a.Kind != "share_lease") || a.ParentID != d.ID || a.Amount != nil || a.Expires.Sub(a.IssuedAt) != 300*time.Second || a.Hard.After(d.HardDeadline) {
			t.Fatal("refresh minted credit or extended limits", a)
		}
		seen[a.Kind] = true
	}
	journal, e := s.PendingLedger(ctx, 1000)
	if e != nil || len(journal) != 2 || journal[0].Kind != "issue" || journal[1].Kind != "runtime-lease" {
		t.Fatal(journal, e)
	}
	if e = s.Close(); e != nil {
		t.Fatal(e)
	}
	s, e = Open(path, "hub")
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	replay, e := s.RefreshHubDelegationLeases(ctx, d.HubID, d.NodeID, d.BootID, d.ID, clock)
	if e != nil || !bytes.Equal(first.Payload, replay.Payload) {
		t.Fatal("reopen changed refresh", e)
	}
	after, e := s.AuditHubDelegation(ctx, d.ID)
	if e != nil || after != before {
		t.Fatal("lease refresh changed credit partition", before, after, e)
	}
	if e = s.db.QueryRow(`SELECT count(*) FROM issuer_issues`).Scan(&count); e != nil || count != 1 {
		t.Fatal("refresh entered credit seal inventory", count, e)
	}
	clock.Earliest = clock.Latest.Add(time.Minute)
	clock.Latest = clock.Earliest.Add(time.Second)
	next, e := s.RefreshHubDelegationLeases(ctx, d.HubID, d.NodeID, d.BootID, d.ID, clock)
	if e != nil || bytes.Equal(first.Payload, next.Payload) {
		t.Fatal("next60s did not refresh", e)
	}
	if e = s.db.QueryRow(`SELECT count(*) FROM issuer_lease_refreshes`).Scan(&count); e != nil || count != 2 {
		t.Fatal(count, e)
	}
}

func TestRuntimeHeartbeatFullJournalAndLeaseCadenceStayIndependent(t *testing.T) {
	s, d, clock, _ := renewalIssuerFixture(t)
	ctx := context.Background()
	if _, e := s.BootstrapHubAuthorizations(ctx, d.HubID, d.NodeID, d.BootID, "", clock); e != nil {
		t.Fatal(e)
	}
	before, e := s.AuditHubDelegation(ctx, d.ID)
	if e != nil {
		t.Fatal(e)
	}
	clock.Earliest = clock.Latest.Add(10 * time.Second)
	clock.Latest = clock.Earliest
	if _, e = s.db.Exec(`CREATE TRIGGER fail_runtime_heartbeat BEFORE INSERT ON hub_ledger WHEN NEW.kind='runtime-lease' BEGIN SELECT RAISE(ABORT,'FULL heartbeat journal failure'); END`); e != nil {
		t.Fatal(e)
	}
	if _, e = s.RefreshHubRuntimeHeartbeat(ctx, d.HubID, d.NodeID, d.BootID, d.ID, clock); e == nil {
		t.Fatal("heartbeat escaped failed journal")
	}
	var count int
	if e = s.db.QueryRow(`SELECT count(*) FROM issuer_runtime_heartbeats`).Scan(&count); e != nil || count != 0 {
		t.Fatal("partial heartbeat", count, e)
	}
	if _, e = s.db.Exec(`DROP TRIGGER fail_runtime_heartbeat`); e != nil {
		t.Fatal(e)
	}
	for i := 0; i < 6; i++ {
		p, e := s.RefreshHubLeases(ctx, d.HubID, d.NodeID, d.BootID, "", clock)
		if e != nil {
			t.Fatal(e)
		}
		if len(p.Updates) != 1 && i < 5 {
			t.Fatal("heartbeat minted premature lease", p)
		}
		var heartbeat struct {
			Authorizations []json.RawMessage `json:"authorizations"`
		}
		if e = json.Unmarshal(p.Updates[0].Payload, &heartbeat); e != nil || len(heartbeat.Authorizations) != 0 {
			t.Fatal("heartbeat created authority", e)
		}
		if i == 5 && len(p.Updates) != 2 {
			t.Fatal("heartbeats postponed60s lease", len(p.Updates))
		}
		clock.Earliest = clock.Earliest.Add(10 * time.Second)
		clock.Latest = clock.Earliest
	}
	after, e := s.AuditHubDelegation(ctx, d.ID)
	if e != nil || after != before {
		t.Fatal("heartbeat/lease changed credit", after, e)
	}
	seal, e := s.SealHubDelegation(ctx, d.HubID, shareID(995), d.ID)
	if e != nil || seal.IssueCount != 1 {
		t.Fatal("liveness entered credit seal inventory", seal, e)
	}
}

func TestRuntimeLeaseRefreshScopeClosureAndDeadline(t *testing.T) {
	for _, negative := range []string{"boot", "hub", "clock", "closed", "sealed", "expired", "source"} {
		t.Run(negative, func(t *testing.T) {
			s, d, clock, _ := renewalIssuerFixture(t)
			ctx := context.Background()
			if _, e := s.BootstrapHubAuthorizations(ctx, d.HubID, d.NodeID, d.BootID, "", clock); e != nil {
				t.Fatal(e)
			}
			before, e := s.AuditHubDelegation(ctx, d.ID)
			if e != nil {
				t.Fatal(e)
			}
			clock.Earliest = clock.Latest.Add(time.Minute)
			clock.Latest = clock.Earliest.Add(time.Second)
			hub, node, boot := d.HubID, d.NodeID, d.BootID
			switch negative {
			case "boot":
				boot = shareID(992)
			case "hub":
				hub = shareID(993)
			case "clock":
				clock.Trusted = false
			case "closed":
				if e = s.CloseHubSigner(ctx, hub, d.SignerEpoch); e != nil {
					t.Fatal(e)
				}
			case "sealed":
				if _, e = s.SealHubDelegation(ctx, hub, shareID(994), d.ID); e != nil {
					t.Fatal(e)
				}
			case "expired":
				clock.Earliest = d.ExpiresAt
				clock.Latest = d.ExpiresAt
			case "source":
				r := d.ShareReferences[0]
				r.Revision++
				if e = s.InstallShareSourceAnchor(ctx, node, shareID(799), r); e != nil {
					t.Fatal(e)
				}
			}
			if _, e = s.RefreshHubDelegationLeases(ctx, hub, node, boot, d.ID, clock); e == nil {
				t.Fatal("scope/boundary bypass", negative)
			}
			after, e := s.AuditHubDelegation(ctx, d.ID)
			if e != nil || before.Issued != after.Issued || before.Remaining != after.Remaining {
				t.Fatal("rejected refresh changed funds", after, e)
			}
		})
	}
}
