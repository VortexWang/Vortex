package agent

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
	"vortex/agent/store"
	controlv1 "vortex/contracts/protobuf/v1"
	ruleruntime "vortex/engine/runtime/rule"
)

func TestEnabledRulePreparesWithoutAuthorityAndCommitWaitsForLease(t *testing.T) {
	ctx := context.Background()
	node, applier, _, a := newRuleRuntimeFixture(t)
	addresses, e := net.InterfaceAddrs()
	if e != nil {
		t.Fatal(e)
	}
	local := ""
	for _, address := range addresses {
		if ip, ok := address.(*net.IPNet); ok && ip.IP.To4() != nil && !ip.IP.IsLoopback() && !ip.IP.IsLinkLocalUnicast() {
			local = ip.IP.String()
			break
		}
	}
	if local == "" {
		t.Fatal("real non-loopback target interface required")
	}
	target, e := net.Listen("tcp", net.JoinHostPort(local, "0"))
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { target.Close() })
	var reached atomic.Int64
	go func() {
		for {
			c, e := target.Accept()
			if e != nil {
				return
			}
			reached.Add(1)
			go func() { defer c.Close(); io.Copy(c, c) }()
		}
	}()
	port, e := net.Listen("tcp", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	a.Intent.Rule.Port = int32(port.Addr().(*net.TCPAddr).Port)
	port.Close()
	a.Intent.Rule.Enabled = true
	a.Intent.Rule.Targets[0].Address = target.Addr().String()
	a.RuleRevision, a.PathGeneration, a.FencingGeneration = 2, 4, 8
	raw, _ := json.Marshal(a)
	digest := sha256.Sum256(raw)
	c := &controlv1.RuleCommand{Phase: "prepare", TaskKey: "prepare-enabled", TransactionId: []byte("enabled-rule-001"), TargetRevision: 2, IdempotencyKey: "enabled-prepare", ArtifactSha256: digest[:], FencingGeneration: 8, Payload: raw}
	r := &Reconciler{Store: node.Store, Apply: applier}
	if out := r.ApplyRuleCommand(ctx, node.NodeID, c); out.State != "prepared" {
		t.Fatalf("bootstrap Prepare needs no executable rights: %+v", out)
	}
	// Preparing binds sockets but starts no reader and opens no target.
	if reached.Load() != 0 {
		t.Fatal("Prepare forwarded business traffic")
	}
	c.Phase, c.TaskKey, c.IdempotencyKey = "commit", "commit-without-lease", "commit-without-lease"
	if out := r.ApplyRuleCommand(ctx, node.NodeID, c); out.State == "active" {
		t.Fatal("unauthorized Commit activated")
	}
	var txID [16]byte
	copy(txID[:], c.TransactionId)
	if tx, _ := applier.Query(txID); string(tx.State) != "prepared" || reached.Load() != 0 {
		t.Fatal("failed authority check changed execution", tx)
	}
	// A fresh matching boot-local authority arrives after Prepare.
	var envelope map[string]any
	if e = json.Unmarshal(leasedPayload(t, node.NodeID, node.BootID), &envelope); e != nil {
		t.Fatal(e)
	}
	issued := time.Now().UTC().Add(-time.Millisecond)
	envelope["issued_at"], envelope["backend_heartbeat_at"], envelope["autonomy_deadline"] = issued, issued, issued.Add(time.Hour)
	for _, entry := range envelope["authorizations"].([]any) {
		v := entry.(map[string]any)
		v["rule_id"] = a.RuleID
		v["issued_at"], v["not_before"], v["expires_at"], v["hard_deadline"] = issued, issued, issued.Add(time.Minute), issued.Add(time.Hour)
	}
	authority, _ := json.Marshal(envelope)
	if e = (&ControlClient{Node: node}).applyAuthorizationUpdate(ctx, &controlv1.AuthorizationUpdate{Generation: 4, Payload: authority}); e != nil {
		t.Fatal(e)
	}
	if e = node.clock.observe(issued, node.bootStarted, time.Now()); e != nil {
		t.Fatal(e)
	}
	c.TaskKey, c.IdempotencyKey = "commit-with-lease", "commit-with-lease"
	if out := r.ApplyRuleCommand(ctx, node.NodeID, c); out.State != "active" {
		t.Fatalf("matching funded lease did not activate: %+v", out)
	}
	conn, e := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", a.Intent.Rule.Port), time.Second)
	if e != nil {
		t.Fatal(e)
	}
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(2 * time.Second))
	if _, e = conn.Write([]byte("live")); e != nil {
		t.Fatal(e)
	}
	var echo [4]byte
	if _, e = io.ReadFull(conn, echo[:]); e != nil || string(echo[:]) != "live" {
		t.Fatal("actual runtime relay", e, string(echo[:]))
	}
	if reached.Load() != 1 {
		t.Fatal("unexpected target connections", reached.Load())
	}
}

func TestRuleCommandPrepareCommitQueryRestartRealRuntime(t *testing.T) {
	ctx := context.Background()
	node, applier, path, a := newRuleRuntimeFixture(t)
	s := node.Store
	payload, _ := json.Marshal(a)
	digest := sha256.Sum256(payload)
	command := &controlv1.RuleCommand{Phase: "prepare", TaskKey: "prepare-one", TransactionId: []byte("transaction-0001"), TargetRevision: 1, IdempotencyKey: "idem-prepare", ArtifactSha256: digest[:], FencingGeneration: 1, Payload: payload}
	r := &Reconciler{Store: s, Apply: applier}
	out := r.ApplyRuleCommand(ctx, node.NodeID, command)
	if out.State != "prepared" {
		t.Fatalf("Prepare: %+v", out)
	}
	phased, e := s.IsPhasedConfig(ctx, fmt.Sprintf("%x", command.TransactionId))
	if e != nil || !phased {
		t.Fatal("resume could commit Prepare", e)
	}
	command.Phase = "query"
	command.TaskKey = "query-one"
	command.IdempotencyKey = "idem-query"
	if out = r.ApplyRuleCommand(ctx, node.NodeID, command); out.State != "prepared" {
		t.Fatalf("Query auto-committed: %+v", out)
	}
	command.Phase = "commit"
	command.TaskKey = "commit-one"
	command.IdempotencyKey = "idem-commit"
	if out = r.ApplyRuleCommand(ctx, node.NodeID, command); out.State != "active" {
		t.Fatalf("Commit: %+v", out)
	}
	// A different executor cannot infer that an absent transaction stopped.
	freshEngine, e := NewRuntimeEngine(path, controlTestNodeID)
	if e != nil {
		t.Fatal(e)
	}
	node.Engine = freshEngine
	if e = freshEngine.SetAccountingGate(node.newRuntimeAuthorityGate(freshEngine.scope, node.TrustedTime)); e != nil {
		t.Fatal(e)
	}
	fresh, e := node.RuntimeApplier()
	if e != nil {
		t.Fatal(e)
	}
	command.Phase = "query"
	command.TaskKey = "restart-query"
	command.IdempotencyKey = "idem-restart"
	if out = (&Reconciler{Store: s, Apply: fresh}).ApplyRuleCommand(ctx, node.NodeID, command); out.State != "unknown" {
		t.Fatalf("new executor fabricated absence proof: %+v", out)
	}
	// A task key cannot be rebound to a different phase/correlation.
	command.Phase = "abort"
	if out = r.ApplyRuleCommand(ctx, node.NodeID, command); out.State != "failed" {
		t.Fatalf("task correlation rewritten: %+v", out)
	}
	// Keep the test tied to a real RuntimeEngine listener, rather than a model.
	if e = freshEngine.Start(ctx); e != nil {
		t.Fatal(e)
	}
	c, e := net.DialTimeout("tcp", freshEngine.runtime.Addr().String(), time.Second)
	if e != nil {
		t.Fatal(e)
	}
	c.Close()
	fresh.Stop(ctx)
}

func newRuleRuntimeFixture(t *testing.T) (*Node, *RuntimeApplier, string, ruleruntime.Artifact) {
	t.Helper()
	dir := t.TempDir()
	cert, key, ca := writeRuntimeIdentity(t, dir, controlTestNodeID)
	path := filepath.Join(dir, "engine.json")
	legacy := fmt.Sprintf(`{"node_id":%q,"role":"destination","certificate":%q,"private_key":%q,"trust_bundle":%q,"listen":"127.0.0.1:0","version":1,"rule_id":"legacy","rule_revision":1,"path_generation":1,"target":"127.0.0.1:9","execution_point":true}`, controlTestNodeID, cert, key, ca)
	if e := os.WriteFile(path, []byte(legacy), 0600); e != nil {
		t.Fatal(e)
	}
	s, e := store.Open(filepath.Join(dir, "agent.db"), "agent")
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { s.Close() })
	engine, e := NewRuntimeEngine(path, controlTestNodeID)
	if e != nil {
		t.Fatal(e)
	}
	node, e := New(controlTestNodeID, s, engine)
	if e != nil {
		t.Fatal(e)
	}
	applier, e := node.RuntimeApplier()
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if e := applier.Stop(ctx); e != nil {
			t.Error(e)
		}
	})
	// Disabled intent needs no spending authority, yet still exercises the real
	// process-local Engine applier, WAL correlation and restart unknown boundary.
	a := ruleruntime.Artifact{TenantID: "550e8400-e29b-41d4-a716-446655440001", RuleID: "550e8400-e29b-41d4-a716-446655440077", NodeID: node.NodeID, BootID: node.BootID, RuleRevision: 1, SignerEpoch: 1, PathGeneration: 1, FencingGeneration: 1, BillingSnapshotID: "550e8400-e29b-41d4-a716-446655440024", InputDigest: fmt.Sprintf("%064d", 1), InputVersions: map[string]string{}, Intent: ruleruntime.Intent{Role: "entry", SessionStable: true, AllowedUpstream: []string{}, UpstreamMembers: []ruleruntime.Member{}, NextMembers: []ruleruntime.Member{}, Segments: []ruleruntime.Segment{}, Rule: ruleruntime.Model{ID: "550e8400-e29b-41d4-a716-446655440077", Name: "stop", Mode: "direct", Protocol: "tcp", Port: 20001, Path: []string{"group"}, Directions: []string{}, Targets: []ruleruntime.Target{{Address: "192.0.2.1:80", Weight: 1}}, Scheduling: "weighted_round_robin", ProxyProtocol: "off", IdleTimeoutSeconds: 60}}}
	return node, applier, path, a
}
