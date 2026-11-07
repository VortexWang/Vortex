package service_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
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

func TestCompileWorkerConflictRetainsAuthority(t *testing.T) {
	f := newPortFixture(t)
	ctx := context.Background()
	s := service.RuleService{Pool: f.pool}
	in := ruleInput(f.group)
	in.Port = 0
	in.Protocol = "tcp_udp"
	m, err := s.Mutate(ctx, f.scope, in, 0, uuid.NewString())
	if err != nil {
		t.Fatal(err)
	}
	req := f.request(t, 0)
	req.RuleID = in.ID
	if _, err = f.ports.PreparePortReservation(ctx, f.scope, req, uuid.NewString()); err != nil {
		t.Fatal(err)
	}
	if _, err = f.pool.Exec(ctx, `UPDATE tasks SET state='unknown' WHERE tenant_id=$1 AND task_kind='port_prepare'`, f.tenant); err != nil {
		t.Fatal(err)
	}
	b := compiler.BillingSnapshot{ID: uuid.NewString(), Version: 1, RuleRevision: 1, PathGeneration: 3}
	if _, err = s.BindCompileInput(ctx, f.scope, m.OperationID, b, 2, 3, 4, uuid.NewString()); err != nil {
		t.Fatal(err)
	}
	tables := []string{"rule_heads", "rule_artifacts", "rule_deployments", "port_allocations", "port_reservations", "tasks", "operations", "outbox", "audit_events"}
	request := uuid.NewString()
	failAudit(t, f.pool, request)
	before := authoritySnapshot(t, f.pool, f.tenant, tables)
	if err = s.RejectCompile(ctx, f.scope, m.OperationID, request); err == nil {
		t.Fatal("rejection bypassed audit")
	}
	if !bytes.Equal(before, authoritySnapshot(t, f.pool, f.tenant, tables)) {
		t.Fatal("partial rejection transaction")
	}
	// A database error while recording the rejection must escape the worker;
	// neither artifacts nor a terminal task state may escape that rollback.
	fault := "compile_reject_fault_" + uuid.NewString()[:8]
	if _, err = f.pool.Exec(ctx, "CREATE FUNCTION "+fault+"() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.action='rule.compile.rejected' AND NEW.tenant_id='"+f.tenant+"' THEN RAISE EXCEPTION 'injected compile rejection fault'; END IF; RETURN NEW; END $$; CREATE TRIGGER "+fault+" BEFORE INSERT ON audit_events FOR EACH ROW EXECUTE FUNCTION "+fault+"()"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := f.pool.Exec(ctx, "DROP TRIGGER IF EXISTS "+fault+" ON audit_events; DROP FUNCTION IF EXISTS "+fault+"()"); err != nil {
			t.Error(err)
		}
	})
	if _, err = (worker.RuleCompileWorker{Pool: f.pool}).Once(ctx); err == nil {
		t.Fatal("worker swallowed rejection persistence failure")
	}
	if !bytes.Equal(before, authoritySnapshot(t, f.pool, f.tenant, tables)) {
		t.Fatal("worker persisted partial conflict rejection")
	}
	if _, err = f.pool.Exec(ctx, "DROP TRIGGER "+fault+" ON audit_events; DROP FUNCTION "+fault+"()"); err != nil {
		t.Fatal(err)
	}
	holds := authoritySnapshot(t, f.pool, f.tenant, []string{"port_allocations", "port_reservations"})
	state := "pending"
	for i := 0; i < 100 && state == "pending"; i++ {
		if _, err = (worker.RuleCompileWorker{Pool: f.pool}).Once(ctx); err != nil {
			t.Fatal("one business conflict terminated worker", err)
		}
		if err = f.pool.QueryRow(ctx, `SELECT state FROM tasks WHERE tenant_id=$1 AND id=$2`, f.tenant, m.OperationID).Scan(&state); err != nil {
			t.Fatal(err)
		}
	}
	var op, sync, port string
	var held, delivered bool
	var active, artifacts int
	err = f.pool.QueryRow(ctx, `SELECT o.state,o.occupation_held,b.delivered_at IS NOT NULL,h.sync_status,h.active_revision,(SELECT count(*) FROM rule_artifacts WHERE tenant_id=$1),(SELECT state FROM tasks WHERE tenant_id=$1 AND task_kind='port_prepare' LIMIT 1) FROM operations o JOIN outbox b ON b.tenant_id=o.tenant_id AND b.operation_id=o.id JOIN rule_compile_requests r ON r.tenant_id=o.tenant_id AND r.task_id=o.id JOIN rule_heads h ON h.tenant_id=r.tenant_id AND h.id=r.rule_id WHERE o.tenant_id=$1 AND o.id=$2`, f.tenant, m.OperationID).Scan(&op, &held, &delivered, &sync, &active, &artifacts, &port)
	if err != nil || state != "failed" || op != "failed" || !held || !delivered || sync != "failed" || active != 0 || artifacts != 0 || port != "unknown" {
		t.Fatal("unsafe rejection", err, state, op, held, delivered, sync, active, artifacts, port)
	}
	if !bytes.Equal(holds, authoritySnapshot(t, f.pool, f.tenant, []string{"port_allocations", "port_reservations"})) {
		t.Fatal("rejection released uncertain socket occupation")
	}
	if _, err = (worker.RuleCompileWorker{Pool: f.pool}).Once(ctx); err != nil {
		t.Fatal("worker could not continue", err)
	}
}

func TestCompileWorkerConflictPreservesPriorActiveArtifact(t *testing.T) {
	f := newPortFixture(t)
	ctx := context.Background()
	s := service.RuleService{Pool: f.pool}
	in := ruleInput(f.group)
	reserveRulePort(t, f, in)
	if _, err := s.Mutate(ctx, f.scope, in, 0, uuid.NewString()); err != nil {
		t.Fatal(err)
	}
	r, err := s.CompileRevision(ctx, f.scope, in.ID, 1, compiler.BillingSnapshot{ID: uuid.NewString(), Version: 1, RuleRevision: 1, PathGeneration: 3}, 2, 3, 4, uuid.NewString())
	if err != nil {
		t.Fatal(err)
	}
	if _, err = (service.RuleOrchestrator{Pool: f.pool}).Begin(ctx, f.scope, in.ID, 1, r.InputDigest, uuid.NewString()); err != nil {
		t.Fatal(err)
	}
	if _, err = f.pool.Exec(ctx, `UPDATE rule_heads SET active_revision=1,sync_status='running' WHERE tenant_id=$1 AND id=$2`, f.tenant, in.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = f.pool.Exec(ctx, `UPDATE tasks SET state='unknown' WHERE tenant_id=$1 AND task_kind='rule_prepare'`, f.tenant); err != nil {
		t.Fatal(err)
	}
	if _, err = f.pool.Exec(ctx, `UPDATE operations SET state='unknown' WHERE tenant_id=$1 AND id IN (SELECT task_id FROM rule_delivery_bindings WHERE tenant_id=$1)`, f.tenant); err != nil {
		t.Fatal(err)
	}
	in.Name = "next desired revision"
	m, err := s.Mutate(ctx, f.scope, in, 1, uuid.NewString())
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.BindCompileInput(ctx, f.scope, m.OperationID, compiler.BillingSnapshot{ID: uuid.NewString(), Version: 1, RuleRevision: 2, PathGeneration: 3}, 2, 3, 5, uuid.NewString()); err != nil {
		t.Fatal(err)
	}
	tables := []string{"rule_artifacts", "rule_artifact_commits", "resource_grants", "port_allocations", "port_reservations", "rule_deployments", "rule_deployment_members", "rule_delivery_bindings"}
	before := authoritySnapshot(t, f.pool, f.tenant, tables)
	if worked, err := (worker.RuleCompileWorker{Pool: f.pool}).Once(ctx); err != nil || !worked {
		t.Fatal("conflict terminated worker", worked, err)
	}
	var active int
	var state string
	if err = f.pool.QueryRow(ctx, `SELECT h.active_revision,t.state FROM rule_heads h JOIN rule_compile_requests r ON r.tenant_id=h.tenant_id AND r.rule_id=h.id JOIN tasks t ON t.tenant_id=r.tenant_id AND t.id=r.task_id WHERE h.tenant_id=$1 AND t.id=$2`, f.tenant, m.OperationID).Scan(&active, &state); err != nil || active != 1 || state != "failed" {
		t.Fatal("prior active revision lost", err, active, state)
	}
	if !bytes.Equal(before, authoritySnapshot(t, f.pool, f.tenant, tables)) {
		t.Fatal("conflict altered prior artifacts, grants, deployment, or socket occupation")
	}
	// A rejected stale task cannot overwrite a new desired revision's status.
	in.Name = "newer desired revision"
	if _, err = s.Mutate(ctx, f.scope, in, 2, uuid.NewString()); err != nil {
		t.Fatal(err)
	}
	before = authoritySnapshot(t, f.pool, f.tenant, []string{"rule_heads", "tasks", "operations", "outbox", "audit_events"})
	if err = s.RejectCompile(ctx, f.scope, m.OperationID, uuid.NewString()); !errors.Is(err, service.ErrRuleRevisionConflict) {
		t.Fatal("stale rejection accepted", err)
	}
	if !bytes.Equal(before, authoritySnapshot(t, f.pool, f.tenant, []string{"rule_heads", "tasks", "operations", "outbox", "audit_events"})) {
		t.Fatal("stale rejection modified newer desired revision")
	}
}
