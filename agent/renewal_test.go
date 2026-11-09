package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"vortex/agent/store"
	"vortex/contracts"
	controlv1 "vortex/contracts/protobuf/v1"
	"vortex/hub"
)

func TestRenewalRejectsLegacySuccessBeforeInstallingAuthority(t *testing.T) {
	ctx := context.Background()
	s, err := store.Open(filepath.Join(t.TempDir(), "agent.db"), "agent")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	n, err := New(controlTestNodeID, s, nil)
	if err != nil {
		t.Fatal(err)
	}
	body := lifecycleAuthorizationFixture(t)
	body["boot_id"] = n.BootID
	a := body["authorizations"].([]any)[0].(map[string]any)
	r := &controlv1.RenewalRequest{RequestId: regionalSessionID, NodeId: n.NodeID, BootId: n.BootID, DelegationId: a["delegation_id"].(string), SourceId: a["source_id"].(string), RuleId: "rule-1", RuleRevision: 2, PathGeneration: 4, FencingGeneration: 8, SignerEpoch: 1, BillingSnapshotId: a["billing_snapshot_id"].(string), RequestedRawBytes: 4096, RequestedTtlSeconds: 120}
	r.RequestSha256, err = contracts.RenewalDigest(r)
	if err != nil {
		t.Fatal(err)
	}
	c := &ControlClient{Node: n}
	if err := c.PersistRenewal(ctx, regionalHubID, r); err != nil {
		t.Fatal(err)
	}
	body["schema_version"] = "1.0.0"
	delete(body, "share_references")
	delete(body, "policy_transitions")
	delete(a, "share_reference_id")
	delete(a, "delegation_id")
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	result := &controlv1.RenewalResult{RequestId: r.RequestId, RequestSha256: r.RequestSha256, Code: "ok", AuthorizationUpdate: &controlv1.AuthorizationUpdate{Generation: 4, Payload: raw}}
	if err := c.acceptRenewalResult(ctx, regionalHubID, result); err == nil {
		t.Fatal("legacy response installed uncorrelated authority")
	}
	intent, err := s.RenewalIntent(ctx, r.RequestId)
	if err != nil || intent.State != "pending" || intent.Result != nil {
		t.Fatalf("invalid response advanced journal: %+v %v", intent, err)
	}
	if _, err := s.AgentState(ctx, "authorization_signer_epoch"); err == nil {
		t.Fatal("legacy response mutated authority")
	}
}

func TestRenewalCurrentMutualTLSStreamReplaysLostResult(t *testing.T) {
	for _, drop := range []bool{false, true} {
		t.Run(fmt.Sprintf("lost-result-%t", drop), func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 6*time.Second)
			defer cancel()
			ns, err := store.Open(filepath.Join(t.TempDir(), "agent.db"), "agent")
			if err != nil {
				t.Fatal(err)
			}
			defer ns.Close()
			hs, err := store.Open(filepath.Join(t.TempDir(), "hub.db"), "hub")
			if err != nil {
				t.Fatal(err)
			}
			defer hs.Close()
			n, err := New(controlTestNodeID, ns, nil)
			if err != nil {
				t.Fatal(err)
			}
			now := time.Now().UTC()
			id := func(i int) string { return fmt.Sprintf("550e8400-e29b-41d4-a716-%012d", i) }
			d := store.HubDelegation{ID: id(920), HubID: regionalHubID, NodeID: n.NodeID, BootID: n.BootID, SourceID: id(921), RuntimeSourceID: id(922), ShareSourceID: id(923), BillingSnapshotID: id(924), RuleID: "rule", RuleRevision: 1, PathGeneration: 1, FencingFloor: 1, SignerEpoch: 1, RawBytes: 1024, RateBPS: 1000000, Connections: 1, ValidFrom: now, ExpiresAt: now.Add(time.Hour), HardDeadline: now.Add(time.Hour), BackendHeartbeat: now, AutonomyDeadline: now.Add(time.Hour)}
			// Explicit finite provisioning fixture at the internal adapter, not
			// a backend provisioning protocol or a source asserted by NodeControl.
			for i, source := range []string{d.SourceID, d.RuntimeSourceID, d.ShareSourceID} {
				root := store.ShareReference{ID: id(930 + i), SourceID: source, IssuerID: d.HubID, NodeID: d.NodeID, SignerEpoch: 1, Generation: 1, Revision: 1, RuleID: d.RuleID, RuleRevision: 1, PathGeneration: 1, RateBPS: d.RateBPS, Connections: 1, NotBefore: now, ExpiresAt: d.ExpiresAt, HardDeadline: d.HardDeadline}
				for _, s := range []*store.Store{ns, hs} {
					if err := s.InstallShareSourceAnchor(ctx, n.NodeID, id(939), root); err != nil {
						t.Fatal(err)
					}
				}
				d.ShareReferences = append(d.ShareReferences, root)
			}
			clock := func() store.IssuerTime {
				v := time.Now().UTC()
				return store.IssuerTime{Earliest: v, Latest: v, Trusted: true}
			}
			if err := hs.InstallHubDelegation(ctx, d, clock()); err != nil {
				t.Fatal(err)
			}
			issuer := &hub.Issuer{Store: hs, HubID: d.HubID, Clock: clock}
			server := &hub.TestServer{Store: hs, HubID: d.HubID, Region: "east", AcceptUsage: true, RenewalIssuer: issuer, DropFirstRenewalReply: drop}
			server.AuthorizationUpdate = func(node, boot string) (*controlv1.AuthorizationUpdate, error) {
				return issuer.Issue(ctx, d.ID, node, boot, id(940), 256, time.Minute)
			}
			serverTLS, clientTLS, _ := controlTLSWithHub(t, n.NodeID, d.HubID)
			listener := startTestControlHub(t, ctx, server, serverTLS)
			c := &ControlClient{Node: n, TLS: clientTLS}
			r := &controlv1.RenewalRequest{RequestId: id(941), NodeId: n.NodeID, BootId: n.BootID, DelegationId: d.ID, SourceId: d.SourceID, RuleId: d.RuleID, RuleRevision: 1, PathGeneration: 1, FencingGeneration: 1, SignerEpoch: 1, BillingSnapshotId: d.BillingSnapshotID, RequestedRawBytes: 256, RequestedTtlSeconds: 60}
			r.RequestSha256, err = contracts.RenewalDigest(r)
			if err != nil {
				t.Fatal(err)
			}
			if err := c.PersistRenewal(ctx, d.HubID, r); err != nil {
				t.Fatal(err)
			}
			config := RegionalConfig{Region: "east", Endpoints: []HubEndpoint{{Region: "east", HubID: d.HubID, ServerName: d.HubID, Address: listener.Addr().String()}}, DialTimeout: time.Second, Watchdog: 2 * time.Second, FailoverBudget: 2 * time.Second, Renewal: &RenewalConfig{Retry: time.Second}}
			done := make(chan error, 1)
			go func() { done <- c.RunRegion(ctx, config) }()
			defer func() {
				cancel()
				select {
				case <-done:
				case <-time.After(time.Second):
					t.Error("renewal sender did not join")
				}
			}()
			for {
				intent, err := ns.RenewalIntent(ctx, r.RequestId)
				if err != nil {
					t.Fatal(err)
				}
				if intent.State == "installed" {
					break
				}
				select {
				case err := <-done:
					t.Fatalf("control stopped before installation: %v", err)
				case <-ctx.Done():
					t.Fatal("renewal was not installed over negotiated stream")
				case <-time.After(10 * time.Millisecond):
				}
			}
			balance, err := hs.AuditHubDelegation(ctx, d.ID)
			if err != nil || balance.Issued != 512 || balance.Remaining != 512 {
				t.Fatalf("lost result repeated debit: %+v %v", balance, err)
			}
			if _, _, trusted := n.TrustedTime(); !trusted {
				t.Fatal("validated authorization did not establish trusted time")
			}
		})
	}
}

func TestRenewalCadenceAndCapabilityBounds(t *testing.T) {
	for _, v := range []RenewalConfig{{Percent: -1}, {Percent: 101}, {Retry: time.Millisecond}, {Retry: 61 * time.Second}} {
		if _, err := v.validated(); err == nil {
			t.Fatalf("invalid cadence %+v", v)
		}
	}
	defaulted, err := (RenewalConfig{}).validated()
	if err != nil || defaulted.Percent != 30 || defaulted.Retry != time.Second {
		t.Fatalf("defaults %+v %v", defaulted, err)
	}
	for _, caps := range [][]string{nil, {"renewal-v1"}, {"status-v1", "renewal-v1", "renewal-v1"}, {"status-v1", "future"}, {"renewal-v1", "status-v1"}} {
		if validRenewalCapabilities(caps, true) {
			t.Fatalf("unsupported capabilities %v", caps)
		}
	}
	if validRenewalCapabilities([]string{"status-v1"}, true) || validRenewalCapabilities([]string{"status-v1", "renewal-v1"}, false) {
		t.Fatal("silent renewal downgrade/upgrade")
	}
}

// With no replies, retries must visit the complete immutable journal rather
// than repeatedly sending its first page or monopolizing one control-loop turn.
func TestRenewalSenderYieldsAndRotatesUnansweredJournal(t *testing.T) {
	ctx := context.Background()
	s, err := store.Open(filepath.Join(t.TempDir(), "agent.db"), "agent")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	n, err := New(controlTestNodeID, s, nil)
	if err != nil {
		t.Fatal(err)
	}
	c := &ControlClient{Node: n}
	for i := 0; i < 9; i++ {
		id := func(n int) string { return fmt.Sprintf("550e8400-e29b-41d4-a716-%012d", 9500+n) }
		r := &controlv1.RenewalRequest{RequestId: id(i), NodeId: n.NodeID, BootId: n.BootID, DelegationId: id(20 + i), SourceId: id(40 + i), RuleId: fmt.Sprint("rule-", i), RuleRevision: 1, PathGeneration: 1, FencingGeneration: 1, SignerEpoch: 1, BillingSnapshotId: id(60 + i), RequestedRawBytes: 256, RequestedTtlSeconds: 60}
		r.RequestSha256, err = contracts.RenewalDigest(r)
		if err != nil {
			t.Fatal(err)
		}
		if err = c.PersistRenewal(ctx, regionalHubID, r); err != nil {
			t.Fatal(err)
		}
	}
	var seq uint64
	after := ""
	stream := &capturedUsageStream{}
	for turn, want := range []int{4, 4, 1, 4} {
		before := len(stream.sent)
		if err = c.sendPendingRenewals(ctx, stream, regionalHubID, &seq, &after); err != nil {
			t.Fatal(err)
		}
		if len(stream.sent)-before != want {
			t.Fatal("control turn did not yield", turn, len(stream.sent)-before, want)
		}
	}
	seen := map[string]bool{}
	for i, m := range stream.sent[:9] {
		r := m.GetRenewalRequest()
		if r == nil || seen[r.RequestId] || m.Sequence != uint64(i+1) {
			t.Fatal("journal skipped/duplicated before wrapping", m)
		}
		seen[r.RequestId] = true
	}
	if stream.sent[9].GetRenewalRequest().RequestId != stream.sent[0].GetRenewalRequest().RequestId {
		t.Fatal("unanswered intent not retried")
	}
	pending, err := s.PendingRenewalIntents(ctx, regionalHubID, n.NodeID, n.BootID, "")
	if err != nil || len(pending) != 9 {
		t.Fatal("sending changed durable intent", len(pending), err)
	}
	for _, intent := range pending {
		if intent.State != "pending" || intent.Result != nil {
			t.Fatal("send fabricated authority", intent)
		}
	}
}
