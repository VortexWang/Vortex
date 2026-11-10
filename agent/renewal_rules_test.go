package agent

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"vortex/agent/store"
	controlv1 "vortex/contracts/protobuf/v1"
	ruleruntime "vortex/engine/runtime/rule"
	"vortex/hub"
)

// Keep each candidate socket reserved through authority/configuration writes.
// Release it only at the real Engine Prepare call, where that Engine takes
// ownership. Probing and closing before setup lets another socket claim it.
type renewalPortApplier struct {
	*RuntimeApplier
	ports map[[16]byte]net.Listener
}

func (a *renewalPortApplier) PrepareRuleArtifact(id [16]byte, generation uint64, raw []byte) error {
	if listener := a.ports[id]; listener != nil {
		delete(a.ports, id)
		if err := listener.Close(); err != nil {
			return err
		}
	}
	return a.RuntimeApplier.PrepareRuleArtifact(id, generation, raw)
}

func TestAutomaticRenewalObservesCommittedRulesOutsideLegacyScope(t *testing.T) {
	ctx := context.Background()
	node, applier, _, base := newRuleRuntimeFixture(t)
	hs, err := store.Open(t.TempDir()+"/hub.db", "hub")
	if err != nil {
		t.Fatal(err)
	}
	defer hs.Close()
	id := func(n int) string { return fmt.Sprintf("550e8400-e29b-41d4-a716-%012d", 7000+n) }
	clock := func() store.IssuerTime {
		now := time.Now().UTC()
		return store.IssuerTime{Earliest: now, Latest: now, Trusted: true}
	}
	issuer := &hub.Issuer{Store: hs, HubID: regionalHubID, Clock: clock}
	client := &ControlClient{Node: node}
	preparer := &renewalPortApplier{RuntimeApplier: applier, ports: make(map[[16]byte]net.Listener)}
	reconcile := &Reconciler{Store: node.Store, Apply: preparer}
	commands := []*controlv1.RuleCommand{}
	originalGrants := map[string]string{}
	for i := 0; i < 37; i++ {
		a := base
		a.RuleID = id(i * 100)
		a.Intent.Rule.ID = a.RuleID
		a.Intent.Rule.Enabled = i != 35
		a.Intent.Rule.Port = 0
		a.BillingSnapshotID = id(i*100 + 1)
		l, err := net.Listen("tcp", ":0")
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { l.Close() })
		a.Intent.Rule.Port = int32(l.Addr().(*net.TCPAddr).Port)
		now := time.Now().UTC()
		d := store.HubDelegation{ID: id(i*100 + 2), HubID: regionalHubID, NodeID: node.NodeID, BootID: node.BootID, SourceID: id(i*100 + 3), RuntimeSourceID: id(i*100 + 4), ShareSourceID: id(i*100 + 5), BillingSnapshotID: a.BillingSnapshotID, RuleID: a.RuleID, RuleRevision: 1, PathGeneration: 1, FencingFloor: 1, SignerEpoch: 1, RawBytes: 1024, RateBPS: 1000000, Connections: 4, ValidFrom: now, ExpiresAt: now.Add(time.Hour), HardDeadline: now.Add(time.Hour), BackendHeartbeat: now, AutonomyDeadline: now.Add(time.Hour)}
		for j, source := range []string{d.SourceID, d.RuntimeSourceID, d.ShareSourceID} {
			root := store.ShareReference{ID: id(i*100 + 10 + j), SourceID: source, IssuerID: d.HubID, NodeID: d.NodeID, SignerEpoch: 1, Generation: 1, Revision: 1, RuleID: d.RuleID, RuleRevision: 1, PathGeneration: 1, RateBPS: d.RateBPS, Connections: d.Connections, NotBefore: now, ExpiresAt: d.ExpiresAt, HardDeadline: d.HardDeadline}
			for _, s := range []*store.Store{hs, node.Store} {
				if err = s.InstallShareSourceAnchor(ctx, node.NodeID, id(99), root); err != nil {
					t.Fatal(err)
				}
			}
			d.ShareReferences = append(d.ShareReferences, root)
		}
		// This explicit finite protocol fixture starts with a partial issue.
		// Use the supported root/child topology, distinct from the production
		// flat-three bootstrap that intentionally consumes its whole partition.
		leaf := d.ShareReferences[0]
		leaf.ID = id(i*100 + 14)
		leaf.ParentID = d.ShareReferences[0].ID
		leaf.ParentRevision = 1
		d.ShareReferences = append(d.ShareReferences, leaf)
		if err = hs.InstallHubDelegation(ctx, d, clock()); err != nil {
			t.Fatal(err)
		}
		update, err := issuer.Issue(ctx, d.ID, node.NodeID, node.BootID, id(i*100+20), 256, time.Minute)
		if err != nil {
			t.Fatal(err)
		}
		if err = client.applyAuthorizationUpdateFrom(ctx, update, regionalHubID); err != nil {
			t.Fatal(err)
		}
		facts, err := validateAuthorizationUpdate(update.Payload, node.NodeID, node.BootID, 1)
		if err != nil {
			t.Fatal(err)
		}
		if err = node.clock.observe(facts.IssuedAt, node.bootStarted, time.Now()); err != nil {
			t.Fatal(err)
		}
		var grant string
		for _, a := range facts.Authorizations {
			if a.Kind == "credit_grant" {
				grant = a.AuthorizationID
			}
		}
		originalGrants[a.RuleID] = grant
		use := store.AuthorizationUse{NodeID: node.NodeID, BootID: node.BootID, AuthorizationID: grant, Kind: "credit_grant", RuleID: a.RuleID, RuleRevision: 1, PathGeneration: 1, BillingSnapshotID: a.BillingSnapshotID, Earliest: clock().Earliest, Latest: clock().Latest, TrustedTime: true, FencingGeneration: 1}
		if _, err = node.Store.ReserveAuthorizedUsage(ctx, store.UsageReservation{NodeID: node.NodeID, BootID: node.BootID, AuthorizationID: grant, BatchID: id(i*100 + 21), SnapshotID: a.BillingSnapshotID, RuleID: a.RuleID, Direction: "egress", FlowKey: "held", Reserved: 200}, use); err != nil {
			t.Fatal(err)
		}
		raw, _ := json.Marshal(a)
		digest := sha256.Sum256(raw)
		command := &controlv1.RuleCommand{Phase: "prepare", TaskKey: fmt.Sprint("prepare-rule-", i), TransactionId: []byte(fmt.Sprintf("renewal-rule-%03d", i)), TargetRevision: 1, IdempotencyKey: fmt.Sprint("prepare-rule-", i), ArtifactSha256: digest[:], FencingGeneration: 1, Payload: raw}
		var transactionID [16]byte
		copy(transactionID[:], command.TransactionId)
		preparer.ports[transactionID] = l
		if out := reconcile.ApplyRuleCommand(ctx, node.NodeID, command); out.State != "prepared" {
			record, recordErr := node.Store.GetConfigResult(ctx, fmt.Sprintf("%x", command.TransactionId))
			t.Fatal("real Prepare", out, "durable reason", record.Reason, recordErr)
		}
		commands = append(commands, command)
	}
	if err = client.queueDueRenewals(ctx, regionalHubID, RenewalConfig{Percent: 30}); err != nil {
		t.Fatal(err)
	}
	pending, err := node.Store.PendingRenewalIntents(ctx, regionalHubID, node.NodeID, node.BootID, "")
	if err != nil || len(pending) != 0 {
		t.Fatal("Prepare-only scope scheduled", pending, err)
	}
	for i, c := range commands {
		if i == 34 {
			continue
		} // Prepare-only
		c.Phase = "commit"
		c.TaskKey = fmt.Sprint("commit-rule-", i)
		c.IdempotencyKey = c.TaskKey
		if out := reconcile.ApplyRuleCommand(ctx, node.NodeID, c); out.State != "active" {
			t.Fatal("real Commit", out)
		}
	}
	var released [16]byte
	copy(released[:], commands[36].TransactionId)
	if err = applier.ReleaseRule(ctx, released); err != nil {
		t.Fatal("real release", err)
	}
	// An active disabled successor must suppress its still-retained old
	// transaction, rather than letting an active-history row revive it.
	var replacement ruleruntime.Artifact
	if err = json.Unmarshal(commands[0].Payload, &replacement); err != nil {
		t.Fatal(err)
	}
	replacement.RuleRevision = 2
	replacement.FencingGeneration = 2
	replacement.Intent.Rule.Enabled = false
	raw, _ := json.Marshal(replacement)
	digest := sha256.Sum256(raw)
	stop := &controlv1.RuleCommand{Phase: "prepare", TaskKey: "prepare-disable-rule", TransactionId: []byte("renewal-stop-000"), TargetRevision: 2, IdempotencyKey: "prepare-disable-rule", ArtifactSha256: digest[:], FencingGeneration: 2, Payload: raw}
	if out := reconcile.ApplyRuleCommand(ctx, node.NodeID, stop); out.State != "prepared" {
		t.Fatal(out)
	}
	stop.Phase = "commit"
	stop.TaskKey = "commit-disable-rule"
	stop.IdempotencyKey = stop.TaskKey
	if out := reconcile.ApplyRuleCommand(ctx, node.NodeID, stop); out.State != "active" {
		t.Fatal(out)
	}
	if err = client.queueDueRenewals(ctx, regionalHubID, RenewalConfig{Percent: 30}); err != nil {
		t.Fatal(err)
	}
	first, err := node.Store.PendingRenewalIntents(ctx, regionalHubID, node.NodeID, node.BootID, "")
	if err != nil || len(first) != 32 {
		t.Fatal("first bounded rule page", len(first), err)
	}
	if err = client.queueDueRenewals(ctx, regionalHubID, RenewalConfig{Percent: 30}); err != nil {
		t.Fatal(err)
	}
	pending, err = node.Store.PendingRenewalIntents(ctx, regionalHubID, node.NodeID, node.BootID, "")
	if err != nil || len(pending) != 33 {
		t.Fatal("committed rules hidden by legacy Engine scope", pending, err)
	}
	for _, p := range pending {
		if p.Request.RuleId == "legacy" || p.Request.RuleId == id(0) || p.Request.RuleId == id(3400) || p.Request.RuleId == id(3500) || p.Request.RuleId == id(3600) || p.Request.RequestedRawBytes != 256 || p.Request.SignerEpoch != 1 {
			t.Fatal("changed funding scope", p)
		}
	}
	// The production sender/Hub issuer/consumer must finish each durable
	// request over actual mTLS without new scope or losing old pending writes.
	serverTLS, clientTLS, _ := controlTLSWithHub(t, node.NodeID, regionalHubID)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	server := grpc.NewServer(grpc.Creds(credentials.NewTLS(serverTLS)))
	defer server.Stop()
	controlv1.RegisterNodeControlServer(server, &hub.RuntimeServer{HubID: regionalHubID, Region: "east", Issuer: issuer, Usage: &hub.UsageRelay{Store: hs, Clock: clock}})
	go server.Serve(listener)
	run, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	client.TLS = clientTLS
	done := make(chan error, 1)
	go func() {
		done <- client.RunRegion(run, RegionalConfig{Region: "east", Watchdog: 3 * time.Second, Renewal: &RenewalConfig{Percent: 30, Retry: time.Second}, Endpoints: []HubEndpoint{{Region: "east", HubID: regionalHubID, ServerName: regionalHubID, Address: listener.Addr().String()}}})
	}()
	tick := time.NewTicker(10 * time.Millisecond)
	defer tick.Stop()
	for {
		installed := 0
		for _, p := range pending {
			intent, err := node.Store.RenewalIntent(ctx, p.Request.RequestId)
			if err != nil {
				t.Fatal(err)
			}
			if intent.State == "installed" {
				installed++
			}
		}
		if installed == 33 {
			break
		}
		select {
		case err := <-done:
			t.Fatal("automatic stream stopped", err)
		case <-run.Done():
			t.Fatal("automatic renewal did not converge", installed, client.RegionalCounters(), run.Err())
		case <-tick.C:
		}
	}
	if client.RegionalCounters().Attempts != 1 {
		t.Fatal("hidden automatic renewal reconnect", client.RegionalCounters())
	}
	firstSession := client.CurrentHubStatus().GetSessionId()
	cancel()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("automatic renewal did not join")
	}
	replay, stopReplay := context.WithTimeout(ctx, 5*time.Second)
	defer stopReplay()
	go func() {
		done <- client.RunRegion(replay, RegionalConfig{Region: "east", Watchdog: 3 * time.Second, Renewal: &RenewalConfig{Percent: 30, Retry: time.Second}, Endpoints: []HubEndpoint{{Region: "east", HubID: regionalHubID, ServerName: regionalHubID, Address: listener.Addr().String()}}})
	}()
	for {
		status := client.CurrentHubStatus()
		if status != nil && status.SessionId != firstSession {
			break
		}
		select {
		case err := <-done:
			t.Fatal("renewal reconnect stopped", err)
		case <-replay.Done():
			t.Fatal(replay.Err())
		case <-tick.C:
		}
	}
	select {
	case err := <-done:
		t.Fatal("renewal reconnect rejected", err)
	case <-time.After(150 * time.Millisecond):
	}
	if client.RegionalCounters().Attempts != 2 {
		t.Fatal("renewal reconnect retried", client.RegionalCounters())
	}
	stopReplay()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("renewal reconnect did not join")
	}
	for _, p := range pending {
		balance, err := node.Store.AuditAuthorizationBalance(ctx, node.NodeID, originalGrants[p.Request.RuleId])
		if err != nil || balance.Initial != 256 || balance.Reserved != 200 || balance.Remaining != 56 {
			t.Fatal("renewal changed original pending debt", balance, err)
		}
		h, err := hs.AuditHubDelegation(ctx, p.Request.DelegationId)
		if err != nil || h.Initial != 1024 || h.Issued != 512 || h.Remaining != 512 {
			t.Fatal("automatic renewal expanded parent", h, err)
		}
	}

	for _, i := range []int{0, 34, 35, 36} {
		h, err := hs.AuditHubDelegation(ctx, id(i*100+2))
		if err != nil || h.Issued != 256 || h.Remaining != 768 {
			t.Fatal("inactive rule renewed", i, h, err)
		}
	}

}
