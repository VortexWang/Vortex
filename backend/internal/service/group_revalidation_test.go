package service_test

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"vortex/backend/internal/auth"
	"vortex/backend/internal/policy"
	"vortex/backend/internal/service"
	"vortex/backend/internal/worker"
)

type groupRevalidationAPI interface {
	ProcessGroupRevalidation(context.Context, string, string, string) (bool, error)
}

func TestGroupRevalidationAllTenantsAtomicReplayAndOriginalAuthority(t *testing.T) {
	isolatePackageAuthority(t)
	f := newPortFixtureKind(t, true)
	ctx := context.Background()
	api, ok := any(f.nodeService).(groupRevalidationAPI)
	if !ok {
		t.Fatal("durable global group revalidation consumer missing")
	}
	admin := policyScope(t, f.tenant, auth.PlatformAdmin, policy.Mutate)
	if _, e := f.pool.Exec(ctx, `INSERT INTO platform_principals(id,role) VALUES($1,'platform_admin')`, admin.ActorID()); e != nil {
		t.Fatal(e)
	}
	other := seedTenant(t, f.pool)
	owner := policyScope(t, other, auth.Owner, policy.Mutate)
	if e := f.nodeService.SetGrant(ctx, policyScope(t, other, auth.PlatformAdmin, policy.Grant), f.group, 0, "active", time.Now().Add(-time.Hour), time.Now().Add(time.Hour), uuid.NewString()); e != nil {
		t.Fatal(e)
	}
	rules := service.RuleService{Pool: f.pool}
	ruleIDs := map[string]string{}
	for _, scope := range []service.TenantScope{f.scope, owner} {
		in := ruleInput(f.group)
		ruleIDs[scope.TenantID()] = in.ID
		if _, e := rules.Mutate(ctx, scope, in, 0, uuid.NewString()); e != nil {
			t.Fatal(e)
		}
		if _, e := f.pool.Exec(ctx, `UPDATE rule_heads SET active_revision=1,sync_status='running' WHERE tenant_id=$1 AND id=$2`, scope.TenantID(), in.ID); e != nil {
			t.Fatal(e)
		}
	}
	result, e := f.nodeService.SetMembersWithOperation(ctx, admin, f.group, 2, []service.GroupMember{{NodeID: f.nodes[0], Weight: 2}}, uuid.NewString())
	if e != nil {
		t.Fatal(e)
	}
	tables := []string{"rule_heads", "rule_revisions", "rule_dependencies", "rule_compile_requests", "operations", "tasks", "outbox", "audit_events", "domain_events", "port_allocations", "traffic_sources", "share_generations"}
	before := map[string][]byte{}
	for _, tenant := range []string{f.tenant, other} {
		before[tenant] = authoritySnapshot(t, f.pool, tenant, tables)
	}
	request := uuid.NewString()
	name := "group_global_audit_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if _, e := f.pool.Exec(ctx, `CREATE FUNCTION `+name+`() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.tenant_id='`+other+`' AND NEW.action='group.revalidate.rules' AND NEW.request_id='`+request+`' THEN RAISE EXCEPTION 'injected audit fault'; END IF; RETURN NEW; END $$; CREATE TRIGGER `+name+` BEFORE INSERT ON audit_events FOR EACH ROW EXECUTE FUNCTION `+name+`() `); e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() {
		if _, e := f.pool.Exec(ctx, `DROP TRIGGER `+name+` ON audit_events; DROP FUNCTION `+name+`() `); e != nil {
			t.Error(e)
		}
	})
	if worked, e := api.ProcessGroupRevalidation(ctx, f.tenant, result.OperationID, request); e == nil || !strings.Contains(e.Error(), "injected audit fault") || worked {
		t.Fatal("global audit failure escaped", worked, e)
	}
	for _, tenant := range []string{f.tenant, other} {
		if !bytes.Equal(before[tenant], authoritySnapshot(t, f.pool, tenant, tables)) {
			t.Fatal("partial cross-tenant revalidation", tenant)
		}
	}
	financial := map[string][]byte{}
	financialTables := []string{"traffic_sources", "share_generations", "port_allocations", "rule_revisions", "rule_dependencies"}
	for _, tenant := range []string{f.tenant, other} {
		financial[tenant] = authoritySnapshot(t, f.pool, tenant, financialTables)
	}
	successes := make(chan bool, 8)
	errs := make(chan error, 8)
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			worked, e := api.ProcessGroupRevalidation(ctx, f.tenant, result.OperationID, uuid.NewString())
			successes <- worked
			errs <- e
		}()
	}
	wg.Wait()
	close(successes)
	close(errs)
	n := 0
	for worked := range successes {
		if worked {
			n++
		}
	}
	for e := range errs {
		if e != nil {
			t.Fatal(e)
		}
	}
	if n != 1 {
		t.Fatal("duplicate global consumption", n)
	}
	for _, tenant := range []string{f.tenant, other} {
		if !bytes.Equal(financial[tenant], authoritySnapshot(t, f.pool, tenant, financialTables)) {
			t.Fatal("revalidation rewrote original financial/dependency authority", tenant)
		}
	}
	for tenant, id := range ruleIDs {
		var active, desired int64
		var pending, superseded, audits int
		if e := f.pool.QueryRow(ctx, `SELECT active_revision,desired_revision FROM rule_heads WHERE tenant_id=$1 AND id=$2`, tenant, id).Scan(&active, &desired); e != nil || active != 1 || desired != 1 {
			t.Fatal("revalidation changed original revision", active, desired, e)
		}
		if e := f.pool.QueryRow(ctx, `SELECT count(*) FILTER(WHERE state='pending'),count(*) FILTER(WHERE state='superseded') FROM tasks WHERE tenant_id=$1 AND task_kind='rule_compile'`, tenant).Scan(&pending, &superseded); e != nil || pending != 1 || superseded != 1 {
			t.Fatal("missing per-tenant compile", tenant, pending, superseded, e)
		}
		if e := f.pool.QueryRow(ctx, `SELECT count(*) FROM audit_events WHERE tenant_id=$1 AND action='group.revalidate.rules' AND actor_id=$2`, tenant, admin.ActorID()).Scan(&audits); e != nil || audits != 1 {
			t.Fatal("missing all-tenant audit", tenant, audits, e)
		}
	}
	var state string
	var delivered bool
	if e := f.pool.QueryRow(ctx, `SELECT o.state,b.delivered_at IS NOT NULL FROM operations o JOIN outbox b ON b.operation_id=o.id WHERE o.id=$1`, result.OperationID).Scan(&state, &delivered); e != nil || state != "succeeded" || !delivered {
		t.Fatal(state, delivered, e)
	}
	// Replays do not produce another audit, compile job or event.
	after := authoritySnapshot(t, f.pool, f.tenant, tables)
	if worked, e := api.ProcessGroupRevalidation(ctx, f.tenant, result.OperationID, uuid.NewString()); e != nil || worked {
		t.Fatal(worked, e)
	}
	if !bytes.Equal(after, authoritySnapshot(t, f.pool, f.tenant, tables)) {
		t.Fatal("replay rewrote authority")
	}
	// A later accepted job retains its actor and must not become executable from
	// a now-revoked role, nor accept a tenant or arbitrary control operation.
	result, e = f.nodeService.SetMembersWithOperation(ctx, admin, f.group, 3, []service.GroupMember{{NodeID: f.nodes[1], Weight: 1}}, uuid.NewString())
	if e != nil {
		t.Fatal(e)
	}
	if _, e = f.pool.Exec(ctx, `UPDATE platform_principals SET state='suspended' WHERE id=$1`, admin.ActorID()); e != nil {
		t.Fatal(e)
	}
	if worked, e := api.ProcessGroupRevalidation(ctx, f.tenant, result.OperationID, uuid.NewString()); !errors.Is(e, service.ErrScope) || worked {
		t.Fatal("revoked actor executed", worked, e)
	}
	if worked, e := api.ProcessGroupRevalidation(ctx, other, result.OperationID, uuid.NewString()); e == nil || worked {
		t.Fatal("relabeled tenant consumed", worked, e)
	}
}

func TestGroupRevalidationWorkerSkipsLegacyAndOpaqueControl(t *testing.T) {
	isolatePackageAuthority(t)
	f := newPortFixtureKind(t, true)
	ctx := context.Background()
	admin := policyScope(t, f.tenant, auth.PlatformAdmin, policy.Mutate)
	if _, e := f.pool.Exec(ctx, `INSERT INTO platform_principals(id,role) VALUES($1,'platform_admin')`, admin.ActorID()); e != nil {
		t.Fatal(e)
	}
	for _, payload := range [][]byte{{255, 0}, []byte(`{"action":"group.revalidate_all_rules","resource_id":"` + f.group + `","version":2}`)} {
		op, e := (&service.Tracer{Pool: f.pool}).Mutate(ctx, f.scope, service.Mutation{ResourceID: uuid.NewString(), TargetKind: "node", Revision: 1, Generation: 1, IdempotencyKey: uuid.NewString(), RequestID: uuid.NewString(), Payload: payload})
		if e != nil {
			t.Fatal(e)
		}
		if len(payload) > 2 {
			if _, e = f.pool.Exec(ctx, `UPDATE tasks SET task_kind='resource_change' WHERE operation_id=$1`, op.ID); e != nil {
				t.Fatal(e)
			}
		}
		if worked, e := f.nodeService.ProcessGroupRevalidation(ctx, f.tenant, op.ID, uuid.NewString()); !errors.Is(e, service.ErrConflict) || worked {
			t.Fatal("legacy/control admitted", worked, e)
		}
	}
	result, e := f.nodeService.SetMembersWithOperation(ctx, admin, f.group, 2, []service.GroupMember{{NodeID: f.nodes[0], Weight: 1}}, uuid.NewString())
	if e != nil {
		t.Fatal(e)
	}
	w := worker.GroupRevalidationWorker{Pool: f.pool}
	if worked, e := w.Once(ctx); e != nil || !worked {
		t.Fatal("production worker", worked, e)
	}
	if worked, e := w.Once(ctx); e != nil || worked {
		t.Fatal("worker duplicated or authorized legacy", worked, e)
	}
	var state string
	if e = f.pool.QueryRow(ctx, `SELECT state FROM operations WHERE id=$1`, result.OperationID).Scan(&state); e != nil || state != "succeeded" {
		t.Fatal(state, e)
	}
}
