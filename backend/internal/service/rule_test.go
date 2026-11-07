package service_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"testing"
	"vortex/backend/db/generated"
	"vortex/backend/internal/auth"
	"vortex/backend/internal/compiler"
	"vortex/backend/internal/control"
	"vortex/backend/internal/policy"
	"vortex/backend/internal/repository"
	"vortex/backend/internal/service"
	"vortex/backend/internal/testutil"
)

func TestRuleRevisionSchema(t *testing.T) {
	pool := testutil.Postgres(t)
	for _, table := range []string{"rule_heads", "rule_revisions", "rule_targets", "rule_segments", "rule_dependencies"} {
		var exists bool
		if err := pool.QueryRow(context.Background(), "SELECT to_regclass($1) IS NOT NULL", table).Scan(&exists); err != nil {
			t.Fatal(err)
		}
		if !exists {
			t.Errorf("immutable rule authority table %s missing", table)
		}
	}
}

func ruleInput(group string) service.RuleInput {
	return service.RuleInput{ID: uuid.NewString(), Name: "规则", Protocol: "tcp", Mode: "direct", Path: []string{group}, Targets: []service.RuleTarget{{Address: "192.0.2.80:443", Weight: 1}}, Scheduling: "weighted_round_robin", ProxyProtocol: "off", Enabled: true}
}

func TestRuleImmutableRevisionConflictAndTenantScope(t *testing.T) {
	f := newPortFixture(t)
	ctx := context.Background()
	s := service.RuleService{Pool: f.pool}
	in := ruleInput(f.group)
	a, e := s.Mutate(ctx, f.scope, in, 0, uuid.NewString())
	if e != nil {
		t.Fatal(e)
	}
	in.Name = "第二版"
	b, e := s.Mutate(ctx, f.scope, in, 1, uuid.NewString())
	if e != nil || b.Revision.Revision != 2 || b.ChangeClass != "hot" {
		t.Fatal("append revision", e, b)
	}
	if _, e = s.Mutate(ctx, f.scope, in, 1, uuid.NewString()); !errors.Is(e, service.ErrRuleRevisionConflict) {
		t.Fatal("stale revision changed desired state", e)
	}
	repo := repository.RuleRepository{Pool: f.pool}
	old, e := repo.Revision(ctx, f.scope, in.ID, 1, uuid.NewString())
	if e != nil || !bytes.Equal(old.Payload, a.Revision.Payload) {
		t.Fatal("old revision overwritten", e)
	}
	for _, table := range []string{"rule_revisions", "rule_targets", "rule_segments", "rule_dependencies"} {
		if _, e = f.pool.Exec(ctx, "UPDATE "+table+" SET revision=revision WHERE tenant_id=$1", f.tenant); e == nil && table != "rule_segments" {
			t.Fatal("mutable immutable fact", table)
		}
	}
	foreign := policyScope(t, seedTenant(t, f.pool), auth.Owner, policy.Mutate)
	if _, e = repo.Revision(ctx, foreign, in.ID, 1, uuid.NewString()); !errors.Is(e, pgx.ErrNoRows) {
		t.Fatal("foreign revision leak", e)
	}
	if _, e = s.Mutate(ctx, foreign, in, 0, uuid.NewString()); e == nil {
		t.Fatal("foreign entry group accepted")
	}
	var current int64
	if e = f.pool.QueryRow(ctx, "SELECT desired_revision FROM rule_heads WHERE tenant_id=$1 AND id=$2", f.tenant, in.ID).Scan(&current); e != nil || current != 2 {
		t.Fatal("stale mutation moved head", e)
	}
}

func TestRuleValidationPathsTargetsAndDirections(t *testing.T) {
	f := newPortFixture(t)
	ctx := context.Background()
	s := service.RuleService{Pool: f.pool}
	for _, mutate := range []func(*service.RuleInput){func(r *service.RuleInput) { r.Protocol = "quic" }, func(r *service.RuleInput) { r.Path = append(r.Path, r.Path[0]); r.Mode = "tunnel" }, func(r *service.RuleInput) { r.Targets[0].Weight = 0 }, func(r *service.RuleInput) { r.Targets[0].Address = "127.0.0.1:80" }, func(r *service.RuleInput) { r.Protocol = "udp"; r.ProxyProtocol = "v1" }, func(r *service.RuleInput) { r.Directions = []string{"sideways"} }} {
		in := ruleInput(f.group)
		mutate(&in)
		if _, e := s.Mutate(ctx, f.scope, in, 0, uuid.NewString()); e == nil {
			t.Fatal("invalid rule accepted", in)
		}
	}
	// A private downstream selects reverse automatically; a private upstream
	// cannot establish either direction and cannot be skipped.
	n := nodeInput()
	n.PublicAddress = ""
	n.Capabilities = []string{"exit"}
	id := mustNode(t, f.nodeService, f.scope, n)
	if _, e := f.pool.Exec(ctx, "UPDATE nodes SET lifecycle='online',boot_id=$2 WHERE id=$1", id, uuid.NewString()); e != nil {
		t.Fatal(e)
	}
	g := groupInput()
	g.Purpose = "exit"
	g.Scheduling = "weighted_round_robin"
	exit := mustGroup(t, f.nodeService, f.scope, g)
	if e := f.nodeService.SetMembers(ctx, f.scope, exit, 1, []service.GroupMember{{NodeID: id, Weight: 1}}, uuid.NewString()); e != nil {
		t.Fatal(e)
	}
	in := ruleInput(f.group)
	in.Mode = "tunnel"
	in.Path = append(in.Path, exit)
	a, e := s.Mutate(ctx, f.scope, in, 0, uuid.NewString())
	if e != nil {
		t.Fatal("auto reverse requires no paid concealment", e)
	}
	var direction string
	if e = f.pool.QueryRow(ctx, "SELECT direction FROM rule_segments WHERE tenant_id=$1 AND rule_id=$2 AND revision=$3", f.tenant, in.ID, a.Revision.Revision).Scan(&direction); e != nil || direction != "reverse" {
		t.Fatal("wrong segment", e, direction)
	}
	if _, e = f.pool.Exec(ctx, "UPDATE nodes SET public_address='192.0.2.3' WHERE id=$1", id); e != nil {
		t.Fatal(e)
	}
	in.Directions = []string{"reverse"}
	if _, e = s.Mutate(ctx, f.scope, in, 1, uuid.NewString()); !errors.Is(e, compiler.Unauthorized) {
		t.Fatal("manual concealment granted without entitlement", e)
	}
	in.Directions = []string{"forward"}
	if _, e = s.Mutate(ctx, f.scope, in, 1, uuid.NewString()); e != nil {
		t.Fatal(e)
	}
}

func TestRuleDependencyRecompileKeepsActiveRevision(t *testing.T) {
	f := newPortFixture(t)
	ctx := context.Background()
	s := service.RuleService{Pool: f.pool}
	in := ruleInput(f.group)
	if _, e := s.Mutate(ctx, f.scope, in, 0, uuid.NewString()); e != nil {
		t.Fatal(e)
	}
	if _, e := f.pool.Exec(ctx, "UPDATE rule_heads SET active_revision=1,sync_status='running' WHERE tenant_id=$1 AND id=$2", f.tenant, in.ID); e != nil {
		t.Fatal(e)
	}
	var version int64
	if e := f.pool.QueryRow(ctx, "SELECT version FROM node_groups WHERE id=$1", f.group).Scan(&version); e != nil {
		t.Fatal(e)
	}
	members := []service.GroupMember{}
	for _, id := range f.nodes {
		members = append(members, service.GroupMember{NodeID: id, Weight: 1})
	}
	if e := f.nodeService.SetMembers(ctx, f.scope, f.group, version, members, uuid.NewString()); e != nil {
		t.Fatal(e)
	}
	var active, desired int64
	var pending, obsolete int
	if e := f.pool.QueryRow(ctx, "SELECT active_revision,desired_revision FROM rule_heads WHERE tenant_id=$1 AND id=$2", f.tenant, in.ID).Scan(&active, &desired); e != nil || active != 1 || desired != 1 {
		t.Fatal("invalidation revoked active configuration", e)
	}
	if e := f.pool.QueryRow(ctx, "SELECT count(*) FILTER(WHERE state='pending'),count(*) FILTER(WHERE state='superseded') FROM tasks WHERE tenant_id=$1 AND task_kind='rule_compile'", f.tenant).Scan(&pending, &obsolete); e != nil || pending != 1 || obsolete != 1 {
		t.Fatal("recompile identity", e, pending, obsolete)
	}
}

func TestRuleAuditRollback(t *testing.T) {
	f := newPortFixture(t)
	ctx := context.Background()
	s := service.RuleService{Pool: f.pool}
	in := ruleInput(f.group)
	request := uuid.NewString()
	failAudit(t, f.pool, request)
	tables := []string{"rule_heads", "rule_revisions", "rule_targets", "rule_segments", "rule_dependencies", "desired_state", "operations", "tasks", "outbox", "audit_events"}
	before := authoritySnapshot(t, f.pool, f.tenant, tables)
	if _, e := s.Mutate(ctx, f.scope, in, 0, request); e == nil {
		t.Fatal("audit failure committed rule")
	}
	if !bytes.Equal(before, authoritySnapshot(t, f.pool, f.tenant, tables)) {
		t.Fatal("partial rule/revision/task/outbox commit")
	}
}

func TestRuleCompileCommittedArtifactAndAuditRollback(t *testing.T) {
	f := newPortFixture(t)
	ctx := context.Background()
	s := service.RuleService{Pool: f.pool}
	in := ruleInput(f.group)
	if _, e := s.Mutate(ctx, f.scope, in, 0, uuid.NewString()); e != nil {
		t.Fatal(e)
	}
	billing := compiler.BillingSnapshot{ID: uuid.NewString(), Version: 1, RuleRevision: 1, PathGeneration: 3}
	request := uuid.NewString()
	failAudit(t, f.pool, request)
	if _, e := s.CompileRevision(ctx, f.scope, in.ID, 1, billing, 2, 3, 5, request); e == nil {
		t.Fatal("compiler artifact escaped failed audit")
	}
	var count int
	if e := f.pool.QueryRow(ctx, "SELECT count(*) FROM rule_artifacts WHERE tenant_id=$1", f.tenant).Scan(&count); e != nil || count != 0 {
		t.Fatal("partial compiler commit", e, count)
	}
	result, e := s.CompileRevision(ctx, f.scope, in.ID, 1, billing, 2, 3, 5, uuid.NewString())
	if e != nil || len(result.Artifacts) != 2 {
		t.Fatal(e, result.Diagnostics)
	}
	replayed, e := s.CompileRevision(ctx, f.scope, in.ID, 1, billing, 2, 3, 5, uuid.NewString())
	if e != nil || replayed.InputDigest != result.InputDigest || replayed.Artifacts[0].Digest != result.Artifacts[0].Digest {
		t.Fatal("database replay changed fixed-input digest", e)
	}
	binding := repository.ArtifactBinding{RuleID: in.ID, NodeID: result.Artifacts[0].NodeID, Digest: result.Artifacts[0].Digest, Revision: 1, FencingGeneration: 5}
	if _, e = control.LoadCommittedArtifact(ctx, f.pool, f.tenant, binding); e != nil {
		t.Fatal(e)
	}
	for _, alter := range []func(*repository.ArtifactBinding){func(b *repository.ArtifactBinding) { b.Revision++ }, func(b *repository.ArtifactBinding) { b.FencingGeneration++ }, func(b *repository.ArtifactBinding) { b.NodeID = uuid.NewString() }, func(b *repository.ArtifactBinding) { b.Digest = "bad" }} {
		bad := binding
		alter(&bad)
		if _, e = control.LoadCommittedArtifact(ctx, f.pool, f.tenant, bad); e == nil {
			t.Fatal("mismatched artifact passed runtime gate")
		}
	}
	if _, e = control.LoadCommittedArtifact(ctx, f.pool, seedTenant(t, f.pool), binding); e == nil {
		t.Fatal("foreign artifact passed runtime gate")
	}
	// Inject a valid but unpublished artifact through SQL: the transport gate
	// must require the separate committed marker, not just canonical digest.
	var pinned compiler.CompileInput
	if e = json.Unmarshal(result.InputCanonical, &pinned); e != nil {
		t.Fatal(e)
	}
	pinned.SignerEpoch++
	uncommitted := compiler.Compile(ctx, pinned)
	a := uncommitted.Artifacts[0]
	q := generated.New(f.pool)
	if e = q.InsertRuleCompileInput(ctx, generated.InsertRuleCompileInputParams{TenantID: f.tenant, RuleID: in.ID, RuleRevision: 1, InputDigest: uncommitted.InputDigest, Canonical: uncommitted.InputCanonical, BillingSnapshotID: billing.ID, SignerEpoch: pinned.SignerEpoch, PathGeneration: 3, FencingGeneration: 5}); e != nil {
		t.Fatal(e)
	}
	if e = q.InsertRuleArtifact(ctx, generated.InsertRuleArtifactParams{TenantID: f.tenant, RuleID: in.ID, RuleRevision: 1, NodeID: a.NodeID, InputDigest: uncommitted.InputDigest, Digest: a.Digest, Canonical: a.Canonical, BillingSnapshotID: billing.ID, SignerEpoch: pinned.SignerEpoch, PathGeneration: 3, FencingGeneration: 5}); e != nil {
		t.Fatal(e)
	}
	binding.Digest = a.Digest
	if _, e = control.LoadCommittedArtifact(ctx, f.pool, f.tenant, binding); e == nil {
		t.Fatal("uncommitted artifact passed runtime gate")
	}
	if _, e = f.pool.Exec(ctx, "UPDATE rule_artifacts SET canonical=canonical WHERE tenant_id=$1", f.tenant); e == nil {
		t.Fatal("artifact mutable")
	}
}
