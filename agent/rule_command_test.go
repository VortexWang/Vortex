package agent

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"
	"vortex/agent/store"
	controlv1 "vortex/contracts/protobuf/v1"
	ruleruntime "vortex/engine/runtime/rule"
)

func TestRuleCommandPrepareCommitQueryRestartRealRuntime(t *testing.T) {
	ctx := context.Background()
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
	defer s.Close()
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
