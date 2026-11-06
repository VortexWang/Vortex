package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"vortex/agent/store"
	"vortex/contracts"
	controlv1 "vortex/contracts/protobuf/v1"
	accounting "vortex/engine/accounting"
	"vortex/hub"
)

// Explicit finite provisioning at the authenticated adapter boundary. No source
// is created by a remote NodeControl message or a production backend transport.
type renewalSafetyFixture struct {
	paths      map[string]string
	ctx        context.Context
	node       *Node
	client     *ControlClient
	agent, hub *store.Store
	delegation store.HubDelegation
	request    *controlv1.RenewalRequest
	initial    authorizationPayload
	result     *controlv1.RenewalResult
}

func newRenewalSafetyFixture(t *testing.T) *renewalSafetyFixture {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	t.Cleanup(cancel)
	paths := map[string]string{}
	open := func(owner string) *store.Store {
		path := filepath.Join(t.TempDir(), owner+".db")
		paths[owner] = path
		s, err := store.Open(path, owner)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { s.Close() })
		return s
	}
	ns, hs := open("agent"), open("hub")
	n, err := New(controlTestNodeID, ns, nil)
	if err != nil {
		t.Fatal(err)
	}
	id := func(i int) string { return fmt.Sprintf("550e8400-e29b-41d4-a716-%012d", 1100+i) }
	now := time.Now().UTC()
	d := store.HubDelegation{ID: id(1), HubID: regionalHubID, NodeID: n.NodeID, BootID: n.BootID, SourceID: id(2), RuntimeSourceID: id(3), ShareSourceID: id(4), BillingSnapshotID: id(5), RuleID: "rule", RuleRevision: 1, PathGeneration: 1, FencingFloor: 1, SignerEpoch: 1, RawBytes: 16384, RateBPS: 1000000, Connections: 4, ValidFrom: now, ExpiresAt: now.Add(time.Hour), HardDeadline: now.Add(time.Hour), BackendHeartbeat: now, AutonomyDeadline: now.Add(time.Hour)}
	for i, source := range []string{d.SourceID, d.RuntimeSourceID, d.ShareSourceID} {
		root := store.ShareReference{ID: id(10 + i), SourceID: source, IssuerID: d.HubID, NodeID: d.NodeID, SignerEpoch: 1, Generation: 1, Revision: 1, RuleID: d.RuleID, RuleRevision: 1, PathGeneration: 1, RateBPS: d.RateBPS, Connections: d.Connections, NotBefore: now, ExpiresAt: d.ExpiresAt, HardDeadline: d.HardDeadline}
		for _, s := range []*store.Store{ns, hs} {
			if err := s.InstallShareSourceAnchor(ctx, n.NodeID, id(20), root); err != nil {
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
	initial, err := issuer.Issue(ctx, d.ID, n.NodeID, n.BootID, id(30), 4096, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	c := &ControlClient{Node: n}
	if err = c.applyAuthorizationUpdateFrom(ctx, initial, d.HubID); err != nil {
		t.Fatal(err)
	}
	initialPayload, err := validateAuthorizationUpdate(initial.Payload, n.NodeID, n.BootID, 1)
	if err != nil {
		t.Fatal(err)
	}
	if err = n.clock.observe(initialPayload.IssuedAt, n.bootStarted, time.Now()); err != nil {
		t.Fatal(err)
	}
	r := &controlv1.RenewalRequest{RequestId: id(31), NodeId: n.NodeID, BootId: n.BootID, DelegationId: d.ID, SourceId: d.SourceID, RuleId: d.RuleID, RuleRevision: 1, PathGeneration: 1, FencingGeneration: 1, SignerEpoch: 1, BillingSnapshotId: d.BillingSnapshotID, RequestedRawBytes: 4096, RequestedTtlSeconds: 60}
	r.RequestSha256, err = contracts.RenewalDigest(r)
	if err != nil {
		t.Fatal(err)
	}
	if err = c.PersistRenewal(ctx, d.HubID, r); err != nil {
		t.Fatal(err)
	}
	result, err := issuer.Renew(ctx, n.NodeID, n.BootID, r)
	if err != nil || result.Code != "ok" {
		t.Fatalf("issue=%+v err=%v", result, err)
	}
	return &renewalSafetyFixture{paths: paths, ctx: ctx, node: n, client: c, agent: ns, hub: hs, delegation: d, request: r, initial: initialPayload, result: result}
}
func (f *renewalSafetyFixture) gate(clock AuthorizationClock) accounting.Gate {
	return f.node.newRuntimeAuthorityGate(RuntimeScope{RuleID: f.delegation.RuleID, RuleRevision: 1, PathGeneration: 1}, clock)
}
func (f *renewalSafetyFixture) consumeAndHold(t *testing.T) accounting.Session {
	t.Helper()
	session, err := f.gate(f.node.TrustedTime).Open(f.ctx, accounting.Flow{Key: "existing", RuleID: "rule"})
	if err != nil {
		t.Fatal(err)
	}
	batch, err := session.Reserve(f.ctx, accounting.LeftToRight, 128)
	if err != nil {
		t.Fatal(err)
	}
	if err = batch.MarkUnknown(f.ctx); err != nil {
		t.Fatal(err)
	}
	batch, err = session.Reserve(f.ctx, accounting.LeftToRight, 64)
	if err != nil {
		t.Fatal(err)
	}
	if err = batch.Commit(f.ctx, 64); err != nil {
		t.Fatal(err)
	}
	return session
}
func (f *renewalSafetyFixture) assertBalance(t *testing.T, initial uint64) {
	t.Helper()
	b, err := f.agent.AuditAuthorizationSource(f.ctx, f.node.NodeID, f.delegation.SourceID)
	if err != nil || b.Initial != initial || b.Committed != 64 || b.Unknown != 128 || b.Reserved != 0 || b.Initial != b.Remaining+b.Committed+b.Unknown+b.Reserved {
		t.Fatalf("Agent conservation=%+v err=%v", b, err)
	}
	h, err := f.hub.AuditHubDelegation(f.ctx, f.delegation.ID)
	if err != nil || h.Issued != 8192 || h.Initial != h.Issued+h.Remaining {
		t.Fatalf("Hub conservation=%+v err=%v", h, err)
	}
}

func TestRenewalSafetyRevocationDominatesDelayedAndConcurrentSuccess(t *testing.T) {
	for _, order := range []string{"before-result", "after-result", "concurrent"} {
		t.Run(order, func(t *testing.T) {
			f := newRenewalSafetyFixture(t)
			session := f.consumeAndHold(t)
			defer session.Close()
			p, err := validateAuthorizationUpdate(f.result.AuthorizationUpdate.Payload, f.node.NodeID, f.node.BootID, 1)
			if err != nil {
				t.Fatal(err)
			}
			rev := p
			rev.UpdateID = "550e8400-e29b-41d4-a716-000000001199"
			rev.RenewalRequestID = ""
			rev.RenewalRequestSHA256 = ""
			rev.Authorizations = []authorizationRecord{}
			for _, a := range append(f.initial.Authorizations, p.Authorizations...) {
				rev.Revocations = append(rev.Revocations, a.AuthorizationID)
			}
			var object map[string]any
			if err := json.Unmarshal(f.result.AuthorizationUpdate.Payload, &object); err != nil {
				t.Fatal(err)
			}
			object["update_id"], object["authorizations"], object["revocations"] = rev.UpdateID, []any{}, rev.Revocations
			delete(object, "renewal_request_id")
			delete(object, "renewal_request_sha256")
			raw, err := json.Marshal(object)
			if err != nil {
				t.Fatal(err)
			}
			revoke := func() error {
				return f.client.applyAuthorizationUpdateFrom(f.ctx, &controlv1.AuthorizationUpdate{Generation: 1, Payload: raw}, f.delegation.HubID)
			}
			accept := func() error {
				err := f.client.acceptRenewalResult(f.ctx, f.delegation.HubID, f.result)
				if errors.Is(err, store.ErrAuthorizationDenied) {
					return nil
				}
				return err
			}
			if order == "concurrent" {
				done := make(chan error, 2)
				go func() { done <- revoke() }()
				go func() { done <- accept() }()
				for range 2 {
					if err := <-done; err != nil {
						t.Fatal(err)
					}
				}
			} else {
				first, second := revoke, accept
				if order == "after-result" {
					first, second = accept, revoke
				}
				if err = first(); err != nil {
					t.Fatal(err)
				}
				if err = second(); err != nil {
					t.Fatal(err)
				}
			}
			if _, err = session.Reserve(f.ctx, accounting.LeftToRight, 1); err == nil {
				t.Fatal("revoked existing flow still reserved bytes")
			}
			if opened, err := f.gate(f.node.TrustedTime).Open(f.ctx, accounting.Flow{Key: "new", RuleID: "rule"}); err == nil {
				opened.Close()
				t.Fatal("revoked renewal admitted new flow")
			}
			if err = accept(); err != nil {
				t.Fatal(err)
			} // exact durable replay must not resurrect records
			intent, err := f.agent.RenewalIntent(f.ctx, f.request.RequestId)
			if err != nil || intent.Result == nil {
				t.Fatalf("lost durable success: %+v %v", intent, err)
			}
			want := uint64(4096)
			if intent.State == "installed" {
				want = 8192
			} else if intent.State != "received" {
				t.Fatalf("unexpected result state: %+v", intent)
			}
			if order == "before-result" && want != 4096 || order == "after-result" && want != 8192 {
				t.Fatalf("wrong ordered outcome: %s %+v", order, intent)
			}
			f.assertBalance(t, want)
			if err := session.Close(); err != nil {
				t.Fatal(err)
			}
			f.save(t, "renewal-safety-revoke-"+order)
		})
	}
}

func TestRenewalSafetyTimeBoundaryCannotReleaseOrConsume(t *testing.T) {
	for _, fault := range []string{"exact-expiry", "stale-anchor"} {
		t.Run(fault, func(t *testing.T) {
			f := newRenewalSafetyFixture(t)
			session := f.consumeAndHold(t)
			defer session.Close()
			if err := f.client.acceptRenewalResult(f.ctx, f.delegation.HubID, f.result); err != nil {
				t.Fatal(err)
			}
			clock := AuthorizationClock(f.node.TrustedTime)
			if fault == "exact-expiry" {
				p, err := validateAuthorizationUpdate(f.result.AuthorizationUpdate.Payload, f.node.NodeID, f.node.BootID, 1)
				if err != nil {
					t.Fatal(err)
				}
				expires := p.Authorizations[0].ExpiresAt
				// A deterministic trusted adapter proves the exact exclusive boundary;
				// the shipped binary process regression separately exercises real clocks.
				clock = func() (time.Time, time.Time, bool) { return expires, expires, true }
				session.(*authorizationSession).gate.config.Clock = clock
			} else {
				f.node.clock.mu.Lock()
				received := f.node.clock.received
				f.node.clock.now = func() time.Time { return received.Add(authorityClockMaxAge + time.Nanosecond) }
				f.node.clock.mu.Unlock()
				if _, _, trusted := f.node.TrustedTime(); trusted {
					t.Fatal("stale anchor remained trusted")
				}
			}
			if _, err := session.Reserve(f.ctx, accounting.LeftToRight, 1); err == nil {
				t.Fatal("unsafe time permitted existing flow")
			}
			if opened, err := f.gate(clock).Open(f.ctx, accounting.Flow{Key: "new", RuleID: "rule"}); err == nil {
				opened.Close()
				t.Fatal("unsafe time admitted a new flow")
			}
			pending, err := f.agent.PendingUnknownUsage(f.ctx, f.node.NodeID)
			if err != nil || len(pending) != 1 || pending[0].Upper != 128 {
				t.Fatalf("lost unknown=%+v err=%v", pending, err)
			}
			f.assertBalance(t, 8192)
			if err := session.Close(); err != nil {
				t.Fatal(err)
			}
			f.save(t, "renewal-safety-time-"+fault)
		})
	}
}

// The request and result retain their original hashes across transport races.
func sameRenewalIntent(a store.RenewalIntent, r *controlv1.RenewalRequest) bool {
	return a.State == "pending" && a.Result == nil && a.Request.RequestId == r.RequestId && bytes.Equal(a.Request.RequestSha256, r.RequestSha256)
}

func (f *renewalSafetyFixture) save(t *testing.T, label string) {
	t.Helper()
	if os.Getenv("PHASE5_AUTHORIZATION_EVIDENCE_DIR") == "" {
		return
	}
	if err := f.agent.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.hub.Close(); err != nil {
		t.Fatal(err)
	}
	saveNamedLedger(t, label, f.paths["agent"], f.paths["hub"])
}
