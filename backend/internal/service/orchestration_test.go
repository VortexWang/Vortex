package service_test

import (
	"bytes"
	"context"
	"errors"
	"github.com/google/uuid"
	"testing"
	"vortex/backend/db/generated"
	"vortex/backend/internal/compiler"
	"vortex/backend/internal/service"
	"vortex/backend/internal/worker"
	controlv1 "vortex/contracts/protobuf/v1"
)

func receipt(c *controlv1.RuleCommand, state string) *controlv1.RuleReceipt {
	return &controlv1.RuleReceipt{Phase: c.Phase, TransactionId: c.TransactionId, TaskKey: c.TaskKey, TargetRevision: c.TargetRevision, IdempotencyKey: c.IdempotencyKey, ArtifactSha256: c.ArtifactSha256, FencingGeneration: c.FencingGeneration, State: state}
}

func TestRollbackAuditAtomicSupersedeAndWorkerCrashRecovery(t *testing.T) {
	f := newPortFixture(t)
	ctx := context.Background()
	s := service.RuleService{Pool: f.pool}
	o := service.RuleOrchestrator{Pool: f.pool}
	in := ruleInput(f.group)
	reserveRulePort(t, f, in)
	if _, e := s.Mutate(ctx, f.scope, in, 0, uuid.NewString()); e != nil {
		t.Fatal(e)
	}
	if _, e := f.pool.Exec(ctx, `UPDATE rule_heads SET active_revision=1,sync_status='running' WHERE tenant_id=$1 AND id=$2`, f.tenant, in.ID); e != nil {
		t.Fatal(e)
	}
	in.Name = "bad revision"
	if _, e := s.Mutate(ctx, f.scope, in, 1, uuid.NewString()); e != nil {
		t.Fatal(e)
	}
	r, e := s.CompileRevision(ctx, f.scope, in.ID, 2, compiler.BillingSnapshot{ID: uuid.NewString(), Version: 1, RuleRevision: 2, PathGeneration: 4}, 2, 4, 6, uuid.NewString())
	if e != nil {
		t.Fatal(e)
	}
	d, e := o.Begin(ctx, f.scope, in.ID, 2, r.InputDigest, uuid.NewString())
	if e != nil {
		t.Fatal(e)
	}
	request := uuid.NewString()
	failAudit(t, f.pool, request)
	tables := []string{"rule_heads", "rule_revisions", "rule_deployments", "rule_delivery_bindings", "tasks", "operations", "outbox", "audit_events"}
	before := authoritySnapshot(t, f.pool, f.tenant, tables)
	if _, e = o.Rollback(ctx, f.scope, d.ID, request); e == nil {
		t.Fatal("rollback escaped failed audit")
	}
	if !bytes.Equal(before, authoritySnapshot(t, f.pool, f.tenant, tables)) {
		t.Fatal("partial rollback transaction")
	}
	// Persist one running task, then lose the worker before transport/receipt.
	var task string
	if e = f.pool.QueryRow(ctx, `SELECT task_id FROM rule_delivery_bindings WHERE tenant_id=$1 AND deployment_id=$2 ORDER BY node_id LIMIT 1`, f.tenant, d.ID).Scan(&task); e != nil {
		t.Fatal(e)
	}
	if _, e = f.pool.Exec(ctx, `UPDATE tasks SET state='running',lease_token='crashed-worker',lease_until=now()-interval '1 second' WHERE tenant_id=$1 AND id=$2`, f.tenant, task); e != nil {
		t.Fatal(e)
	}
	w := worker.RuleWorker{Pool: f.pool}
	if e = w.Recover(ctx); e != nil {
		t.Fatal(e)
	}
	var unknown, queries int
	if e = f.pool.QueryRow(ctx, `SELECT count(*) FROM rule_deployment_members WHERE tenant_id=$1 AND deployment_id=$2 AND state='unknown'`, f.tenant, d.ID).Scan(&unknown); e != nil || unknown != 1 {
		t.Fatal("crash erased uncertainty", e, unknown)
	}
	if e = f.pool.QueryRow(ctx, `SELECT count(*) FROM rule_delivery_bindings WHERE tenant_id=$1 AND deployment_id=$2 AND phase='query'`, f.tenant, d.ID).Scan(&queries); e != nil || queries != 1 {
		t.Fatal("crash lacks query", e, queries)
	}
	if _, e = f.pool.Exec(ctx, `UPDATE rule_deployments SET deadline=now()-interval '1 second' WHERE tenant_id=$1 AND id=$2`, f.tenant, d.ID); e != nil {
		t.Fatal(e)
	}
	if e = w.Recover(ctx); e != nil {
		t.Fatal(e)
	}
	var desired, active, rollback int64
	if e = f.pool.QueryRow(ctx, `SELECT h.desired_revision,h.active_revision,d.rollback_revision FROM rule_heads h JOIN rule_deployments d ON d.tenant_id=h.tenant_id AND d.rule_id=h.id WHERE d.tenant_id=$1 AND d.id=$2`, f.tenant, d.ID).Scan(&desired, &active, &rollback); e != nil || desired != 3 || active != 1 || rollback != 3 {
		t.Fatal("timeout failed to append monotonic rollback", e, desired, active, rollback)
	}
	var held bool
	var state string
	if e = f.pool.QueryRow(ctx, `SELECT occupation_held,state FROM operations WHERE tenant_id=$1 AND id=$2`, f.tenant, task).Scan(&held, &state); e != nil || !held || state != "unknown" {
		t.Fatal("rollback freed in-flight unknown", e, held, state)
	}
	var pending, superseded int
	if e = f.pool.QueryRow(ctx, `SELECT count(*) FILTER(WHERE t.state='pending'),count(*) FILTER(WHERE t.state='superseded') FROM tasks t JOIN rule_delivery_bindings b ON b.tenant_id=t.tenant_id AND b.task_id=t.id WHERE b.tenant_id=$1 AND b.deployment_id=$2 AND b.phase='prepare'`, f.tenant, d.ID).Scan(&pending, &superseded); e != nil || pending != 0 || superseded != 1 {
		t.Fatal("only pending obsolete work should supersede", e, pending, superseded)
	}
}
func TestApplyPrepareCommitUnknownAndLateReceipt(t *testing.T) {
	f := newPortFixture(t)
	ctx := context.Background()
	s := service.RuleService{Pool: f.pool}
	o := service.RuleOrchestrator{Pool: f.pool}
	in := ruleInput(f.group)
	reserveRulePort(t, f, in)
	if _, e := s.Mutate(ctx, f.scope, in, 0, uuid.NewString()); e != nil {
		t.Fatal(e)
	}
	r, e := s.CompileRevision(ctx, f.scope, in.ID, 1, compiler.BillingSnapshot{ID: uuid.NewString(), Version: 1, RuleRevision: 1, PathGeneration: 3}, 2, 3, 5, uuid.NewString())
	if e != nil {
		t.Fatal(e)
	}
	request := uuid.NewString()
	failAudit(t, f.pool, request)
	tables := []string{"rule_deployments", "rule_deployment_members", "rule_delivery_bindings", "tasks", "outbox", "operations", "audit_events"}
	before := authoritySnapshot(t, f.pool, f.tenant, tables)
	if _, e = o.Begin(ctx, f.scope, in.ID, 1, r.InputDigest, request); e == nil {
		t.Fatal("prepare escaped failed audit")
	}
	if !bytes.Equal(before, authoritySnapshot(t, f.pool, f.tenant, tables)) {
		t.Fatal("partial prepare transaction")
	}
	d, e := o.Begin(ctx, f.scope, in.ID, 1, r.InputDigest, uuid.NewString())
	if e != nil {
		t.Fatal(e)
	}
	commands := func(phase string) []*controlv1.RuleCommand {
		t.Helper()
		rows, e := f.pool.Query(ctx, `SELECT task_id FROM rule_delivery_bindings WHERE tenant_id=$1 AND deployment_id=$2 AND phase=$3 ORDER BY node_id,task_id`, f.tenant, d.ID, phase)
		if e != nil {
			t.Fatal(e)
		}
		var ids []string
		for rows.Next() {
			var id string
			if e = rows.Scan(&id); e != nil {
				t.Fatal(e)
			}
			ids = append(ids, id)
		}
		rows.Close()
		if e = rows.Err(); e != nil {
			t.Fatal(e)
		}
		var out []*controlv1.RuleCommand
		for _, id := range ids {
			c, _, _, e := service.RuleCommand(ctx, generated.New(f.pool), f.tenant, id)
			if e != nil {
				t.Fatal(e)
			}
			out = append(out, c)
		}
		return out
	}
	prepare := commands("prepare")
	if len(prepare) != 2 {
		t.Fatal("fixed member set", len(prepare))
	}
	accept := func(c *controlv1.RuleCommand, state string) {
		t.Helper()
		lease := uuid.NewString()
		if _, e = f.pool.Exec(ctx, `UPDATE tasks SET state='running',lease_token=$3 WHERE tenant_id=$1 AND id=$2`, f.tenant, c.TaskKey, lease); e != nil {
			t.Fatal(e)
		}
		if e = o.ReceiptWithLease(ctx, f.scope, c.TaskKey, lease, receipt(c, state), uuid.NewString()); e != nil {
			t.Fatal(e)
		}
	}
	accept(prepare[0], "prepared")
	if len(commands("commit")) != 0 {
		t.Fatal("committed before all prepared")
	}
	bad := receipt(prepare[1], "prepared")
	bad.FencingGeneration++
	if e = o.Receipt(ctx, f.scope, prepare[1].TaskKey, bad, uuid.NewString()); !errors.Is(e, service.ErrStaleReceipt) {
		t.Fatal("foreign generation receipt", e)
	}
	accept(prepare[1], "prepared")
	commit := commands("commit")
	if len(commit) != 2 {
		t.Fatal("missing durable commits", len(commit))
	}
	accept(commit[0], "active")
	accept(commit[1], "unknown")
	var state string
	var active int64
	if e = f.pool.QueryRow(ctx, `SELECT d.state,h.active_revision FROM rule_deployments d JOIN rule_heads h ON h.tenant_id=d.tenant_id AND h.id=d.rule_id WHERE d.tenant_id=$1 AND d.id=$2`, f.tenant, d.ID).Scan(&state, &active); e != nil || state != "partial" || active != 0 {
		t.Fatal("unknown falsely activated", e, state, active)
	}
	query := commands("query")
	if len(query) != 1 {
		t.Fatal("unknown lacks reconciliation", len(query))
	}
	accept(query[0], "active")
	if e = f.pool.QueryRow(ctx, `SELECT active_revision FROM rule_heads WHERE tenant_id=$1 AND id=$2`, f.tenant, in.ID).Scan(&active); e != nil || active != 1 {
		t.Fatal("proof did not activate", e, active)
	}
	// A newer desired revision cannot be advanced by a duplicate old observation.
	in.Name = "new"
	if _, e = s.Mutate(ctx, f.scope, in, 1, uuid.NewString()); e != nil {
		t.Fatal(e)
	}
	accept(commit[0], "active")
	if e = f.pool.QueryRow(ctx, `SELECT active_revision FROM rule_heads WHERE tenant_id=$1 AND id=$2`, f.tenant, in.ID).Scan(&active); e != nil || active != 1 {
		t.Fatal("late receipt changed new revision", e)
	}
}
