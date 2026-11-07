package service_test

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"vortex/backend/db/generated"
	"vortex/backend/internal/compiler"
	"vortex/backend/internal/service"
	"vortex/backend/internal/worker"
)

func TestCompileWorkerMissingSnapshotThenReadyRestart(t *testing.T) {
	f := newPortFixture(t)
	ctx := context.Background()
	s := service.RuleService{Pool: f.pool}
	in := ruleInput(f.group)
	reserveRulePort(t, f, in)
	m, e := s.Mutate(ctx, f.scope, in, 0, uuid.NewString())
	if e != nil {
		t.Fatal(e)
	}
	if _, e = f.pool.Exec(ctx, `UPDATE rule_heads SET active_revision=1,sync_status='running' WHERE tenant_id=$1 AND id=$2`, f.tenant, in.ID); e != nil {
		t.Fatal(e)
	}
	checkPending := func() {
		t.Helper()
		var state string
		var artifacts, active int
		if e := f.pool.QueryRow(ctx, `SELECT t.state,h.active_revision,(SELECT count(*) FROM rule_artifacts a WHERE a.tenant_id=h.tenant_id) FROM tasks t JOIN rule_compile_requests r ON r.tenant_id=t.tenant_id AND r.task_id=t.id JOIN rule_heads h ON h.tenant_id=r.tenant_id AND h.id=r.rule_id WHERE t.tenant_id=$1 AND t.id=$2`, f.tenant, m.OperationID).Scan(&state, &active, &artifacts); e != nil || state != "pending" || active != 1 || artifacts != 0 {
			t.Fatal("missing snapshot changed authority", e, state, active, artifacts)
		}
	}
	if worked, e := (worker.RuleCompileWorker{Pool: f.pool}).Once(ctx); e != nil || worked {
		t.Fatal(worked, e)
	}
	checkPending()
	// Explicitly reject missing billing rather than inventing a snapshot.
	if _, e = s.BindCompileInput(ctx, f.scope, m.OperationID, compiler.BillingSnapshot{}, 2, 3, 4, uuid.NewString()); e == nil {
		t.Fatal("empty snapshot accepted")
	}
	checkPending()
	billing := compiler.BillingSnapshot{ID: uuid.NewString(), Version: 1, MultiplierMilli: 1000, RuleRevision: 1, PathGeneration: 3}
	digest, e := s.BindCompileInput(ctx, f.scope, m.OperationID, billing, 2, 3, 4, uuid.NewString())
	if e != nil {
		t.Fatal(e)
	}
	p, e := generated.New(f.pool).GetRuleCompileInput(ctx, generated.GetRuleCompileInputParams{TenantID: f.tenant, RuleID: in.ID, RuleRevision: 1, InputDigest: digest})
	if e != nil {
		t.Fatal(e)
	}
	var fixed compiler.CompileInput
	if e = json.Unmarshal(p.Canonical, &fixed); e != nil {
		t.Fatal(e)
	}
	want := compiler.Compile(ctx, fixed)
	// A newer catalog version cannot change the already bound compiler input.
	if _, e = f.pool.Exec(ctx, `UPDATE node_groups SET version=version+1 WHERE id=$1`, f.group); e != nil {
		t.Fatal(e)
	}
	// Even opaque bytes in an unrelated operation must not be parsed as rule JSON.
	opaque := uuid.NewString()
	_, e = f.pool.Exec(ctx, `WITH desired AS (INSERT INTO desired_state(tenant_id,resource_id,target_kind,revision,payload,payload_sha256) VALUES($2,$1,'node',1,decode('ff00','hex'),decode(repeat('00',32),'hex')) RETURNING tenant_id,resource_id), op AS (INSERT INTO operations(id,tenant_id,resource_id,idempotency_key,request_sha256,target_kind,target_revision,expected_revision,generation,payload) SELECT resource_id,tenant_id,resource_id,resource_id,decode(repeat('00',32),'hex'),'node',1,0,1,decode('ff00','hex') FROM desired RETURNING id,tenant_id), task AS (INSERT INTO tasks(id,tenant_id,operation_id) SELECT id,tenant_id,id FROM op) INSERT INTO outbox(tenant_id,operation_id) SELECT tenant_id,id FROM op`, opaque, f.tenant)
	if e != nil {
		t.Fatal(e)
	}
	if worked, e := (worker.RuleCompileWorker{Pool: f.pool}).Once(ctx); e != nil || !worked {
		t.Fatal(worked, e)
	}
	rows, e := generated.New(f.pool).ListCommittedRuleArtifacts(ctx, generated.ListCommittedRuleArtifactsParams{TenantID: f.tenant, RuleID: in.ID, RuleRevision: 1, InputDigest: digest})
	if e != nil || len(rows) != len(want.Artifacts) {
		t.Fatal(e, len(rows))
	}
	for _, a := range rows {
		found := false
		for _, w := range want.Artifacts {
			if a.NodeID == w.NodeID {
				found = true
				if a.Digest != w.Digest || !bytes.Equal(a.Canonical, w.Canonical) {
					t.Fatal("consumer reread mutable catalog")
				}
			}
		}
		if !found {
			t.Fatal("unexpected node")
		}
	}
	var state string
	var delivered, held bool
	var prepares int
	if e = f.pool.QueryRow(ctx, `SELECT t.state,o.occupation_held,b.delivered_at IS NOT NULL,(SELECT count(*) FROM rule_delivery_bindings WHERE tenant_id=$1 AND phase='prepare') FROM tasks t JOIN operations o ON o.tenant_id=t.tenant_id AND o.id=t.operation_id JOIN outbox b ON b.tenant_id=t.tenant_id AND b.operation_id=t.operation_id WHERE t.tenant_id=$1 AND t.id=$2`, f.tenant, m.OperationID).Scan(&state, &held, &delivered, &prepares); e != nil || state != "succeeded" || !held || !delivered || prepares != len(rows) {
		t.Fatal("compile completion not atomic", e, state, held, delivered, prepares)
	}
	before := authoritySnapshot(t, f.pool, f.tenant, []string{"rule_artifacts", "rule_deployments", "rule_delivery_bindings", "tasks", "operations", "outbox"})
	if worked, e := (worker.RuleCompileWorker{Pool: f.pool}).Once(ctx); e != nil || worked {
		t.Fatal("restart duplicated work", worked, e)
	}
	if !bytes.Equal(before, authoritySnapshot(t, f.pool, f.tenant, []string{"rule_artifacts", "rule_deployments", "rule_delivery_bindings", "tasks", "operations", "outbox"})) {
		t.Fatal("restart changed authority")
	}
	if got, e := s.BindCompileInput(ctx, f.scope, m.OperationID, billing, 2, 3, 4, uuid.NewString()); e != nil || got != digest {
		t.Fatal("binding replay changed identity", got, e)
	}
	billing.ID = uuid.NewString()
	if _, e = s.BindCompileInput(ctx, f.scope, m.OperationID, billing, 2, 3, 4, uuid.NewString()); e == nil {
		t.Fatal("task rebound to different snapshot")
	}
	if _, e = f.pool.Exec(ctx, `UPDATE rule_compile_requests SET input_digest=input_digest WHERE tenant_id=$1 AND task_id=$2`, f.tenant, m.OperationID); e == nil {
		t.Fatal("binding mutable")
	}
}

func TestCompileWorkerAuditRollback(t *testing.T) {
	f := newPortFixture(t)
	ctx := context.Background()
	s := service.RuleService{Pool: f.pool}
	in := ruleInput(f.group)
	reserveRulePort(t, f, in)
	m, e := s.Mutate(ctx, f.scope, in, 0, uuid.NewString())
	if e != nil {
		t.Fatal(e)
	}
	b := compiler.BillingSnapshot{ID: uuid.NewString(), Version: 1, RuleRevision: 1, PathGeneration: 3}
	tables := []string{"rule_compile_requests", "rule_compile_inputs", "rule_artifacts", "rule_artifact_commits", "rule_deployments", "rule_deployment_members", "rule_delivery_bindings", "rule_heads", "tasks", "operations", "outbox", "audit_events"}
	request := uuid.NewString()
	failAudit(t, f.pool, request)
	before := authoritySnapshot(t, f.pool, f.tenant, tables)
	if _, e = s.BindCompileInput(ctx, f.scope, m.OperationID, b, 2, 3, 4, request); e == nil {
		t.Fatal("bind audit bypass")
	}
	if !bytes.Equal(before, authoritySnapshot(t, f.pool, f.tenant, tables)) {
		t.Fatal("partial bind transaction")
	}
	if _, e = s.BindCompileInput(ctx, f.scope, m.OperationID, b, 2, 3, 4, uuid.NewString()); e != nil {
		t.Fatal(e)
	}
	request = uuid.NewString()
	failAudit(t, f.pool, request)
	before = authoritySnapshot(t, f.pool, f.tenant, tables)
	if e = s.ConsumeCompile(ctx, f.scope, m.OperationID, request); e == nil {
		t.Fatal("consume audit bypass")
	}
	if !bytes.Equal(before, authoritySnapshot(t, f.pool, f.tenant, tables)) {
		t.Fatal("partial consumer transaction")
	}
	if worked, e := (worker.RuleCompileWorker{Pool: f.pool}).Once(ctx); e != nil || !worked {
		t.Fatal("rolled back task did not retry", worked, e)
	}
}

func TestCompileWorkerMissingSnapshotPreservesPriorAuthority(t *testing.T) {
	f := newPortFixture(t)
	ctx := context.Background()
	s := service.RuleService{Pool: f.pool}
	in := ruleInput(f.group)
	reserveRulePort(t, f, in)
	if _, e := s.Mutate(ctx, f.scope, in, 0, uuid.NewString()); e != nil {
		t.Fatal(e)
	}
	r, e := s.CompileRevision(ctx, f.scope, in.ID, 1, compiler.BillingSnapshot{ID: uuid.NewString(), Version: 1, RuleRevision: 1, PathGeneration: 3}, 2, 3, 4, uuid.NewString())
	if e != nil {
		t.Fatal(e)
	}
	if _, e = (service.RuleOrchestrator{Pool: f.pool}).Begin(ctx, f.scope, in.ID, 1, r.InputDigest, uuid.NewString()); e != nil {
		t.Fatal(e)
	}
	if _, e = f.pool.Exec(ctx, `UPDATE rule_heads SET active_revision=1,sync_status='running' WHERE tenant_id=$1 AND id=$2`, f.tenant, in.ID); e != nil {
		t.Fatal(e)
	}
	if _, e = f.pool.Exec(ctx, `UPDATE tasks SET state='unknown' WHERE tenant_id=$1 AND task_kind='rule_prepare'`, f.tenant); e != nil {
		t.Fatal(e)
	}
	if _, e = f.pool.Exec(ctx, `UPDATE operations SET state='unknown' WHERE tenant_id=$1 AND id IN (SELECT task_id FROM rule_delivery_bindings WHERE tenant_id=$1)`, f.tenant); e != nil {
		t.Fatal(e)
	}
	in.Name = "next desired revision"
	m, e := s.Mutate(ctx, f.scope, in, 1, uuid.NewString())
	if e != nil {
		t.Fatal(e)
	}
	tables := []string{"rule_heads", "rule_compile_requests", "rule_compile_inputs", "rule_artifacts", "rule_artifact_commits", "resource_grants", "rule_deployments", "rule_deployment_members", "tasks", "operations", "outbox", "audit_events"}
	before := authoritySnapshot(t, f.pool, f.tenant, tables)
	if worked, e := (worker.RuleCompileWorker{Pool: f.pool}).Once(ctx); e != nil || worked {
		t.Fatal(worked, e)
	}
	if _, e = s.BindCompileInput(ctx, f.scope, m.OperationID, compiler.BillingSnapshot{}, 2, 3, 5, uuid.NewString()); e == nil {
		t.Fatal("missing snapshot accepted")
	}
	if !bytes.Equal(before, authoritySnapshot(t, f.pool, f.tenant, tables)) {
		t.Fatal("missing snapshot altered active artifact, grant, or unknown occupation")
	}
}
