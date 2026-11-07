package service_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"vortex/backend/db/generated"
	"vortex/backend/internal/auth"
	"vortex/backend/internal/policy"
	"vortex/backend/internal/service"
	"vortex/backend/internal/worker"
	controlv1 "vortex/contracts/protobuf/v1"
)

type downgradeAPI interface {
	PreviewDowngrade(context.Context, service.TenantScope, string, int64, []string, time.Time) (json.RawMessage, error)
	ConfirmDowngrade(context.Context, service.TenantScope, string, int64, []string, time.Time, string, string, string) (service.PackageResult, error)
	ProcessDuePackageChanges(context.Context, time.Time) (bool, error)
}

func TestRestrictionAllowsStopRevisionAndCompile(t *testing.T) {
	f := newPortFixture(t)
	ctx := context.Background()
	r := ruleInput(f.group)
	reserveRulePort(t, f, r)
	subscribePackage(t, f, createPackage(t, f, packageTerms()))
	s := service.RuleService{Pool: f.pool}
	if _, e := s.Mutate(ctx, f.scope, r, 0, uuid.NewString()); e != nil {
		t.Fatal(e)
	}
	at := time.Now().Add(-time.Minute)
	if e := (service.TenantService{Pool: f.pool}).ChangeRestriction(ctx, policyScope(t, f.tenant, auth.PlatformAdmin, policy.Restrict), service.Restriction{Cause: "package_expired", SourceID: uuid.NewString(), SourceVersion: 1, EffectiveAt: at}, uuid.NewString()); e != nil {
		t.Fatal(e)
	}
	r.Enabled = false
	stop, e := s.Mutate(ctx, f.scope, r, 1, uuid.NewString())
	if e != nil {
		t.Fatal("restriction blocked a stop revision", e)
	}
	p := service.PackageService{Pool: f.pool}
	if _, e = p.BindPackageCompile(ctx, f.scope, stop.OperationID, 2, 3, 4, uuid.NewString()); e != nil {
		t.Fatal("restriction blocked stop compilation", e)
	}
	if e = s.ConsumeCompile(ctx, f.scope, stop.OperationID, uuid.NewString()); e != nil {
		t.Fatal("restriction blocked stop deployment", e)
	}
	r.Enabled = true
	if _, e = s.Mutate(ctx, f.scope, r, 2, uuid.NewString()); e == nil {
		t.Fatal("restriction allowed activation")
	}
}

func TestRestrictionBlocksDispatchButPreservesLateObservation(t *testing.T) {
	f := newPortFixture(t)
	ctx := context.Background()
	r := ruleInput(f.group)
	reserveRulePort(t, f, r)
	s := service.RuleService{Pool: f.pool}
	if _, e := s.Mutate(ctx, f.scope, r, 0, uuid.NewString()); e != nil {
		t.Fatal(e)
	}
	compiled, e := s.CompileRevision(ctx, f.scope, r.ID, 1, durableBilling(t, f, r.ID, 1, 3), 2, 3, 5, uuid.NewString())
	if e != nil {
		t.Fatal(e)
	}
	o := service.RuleOrchestrator{Pool: f.pool}
	d, e := o.Begin(ctx, f.scope, r.ID, 1, compiled.InputDigest, uuid.NewString())
	if e != nil {
		t.Fatal(e)
	}
	rows, e := f.pool.Query(ctx, "SELECT task_id FROM rule_delivery_bindings WHERE tenant_id=$1 AND deployment_id=$2 AND phase='prepare' ORDER BY node_id", f.tenant, d.ID)
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
	q := generated.New(f.pool)
	command, _, _, e := service.RuleCommand(ctx, q, f.tenant, ids[0])
	if e != nil {
		t.Fatal(e)
	}
	second, _, _, e := service.RuleCommand(ctx, q, f.tenant, ids[1])
	if e != nil {
		t.Fatal(e)
	}
	lease := uuid.NewString()
	if _, e = f.pool.Exec(ctx, "UPDATE tasks SET state='running',lease_token=$3 WHERE tenant_id=$1 AND id=$2", f.tenant, ids[0], lease); e != nil {
		t.Fatal(e)
	}
	if e = q.ApplyPackageRuleRestriction(ctx, generated.ApplyPackageRuleRestrictionParams{ID: uuid.NewString(), TenantID: f.tenant, SourceID: uuid.NewString(), SourceVersion: 1, EffectiveAt: pgtype.Timestamptz{Time: time.Now().Add(-time.Second), Valid: true}, ResourceID: pgtype.Text{String: r.ID, Valid: true}}); e != nil {
		t.Fatal(e)
	}
	if _, _, _, e = service.RuleCommand(ctx, q, f.tenant, ids[1]); !errors.Is(e, service.ErrConflict) {
		t.Fatal("restricted prepare dispatched", e)
	}
	// The first send occurred before restriction. Its uncertain result remains
	// evidence, even though dispatching the same activation is no longer allowed.
	if e = o.ReceiptWithLease(ctx, f.scope, ids[0], lease, receipt(command, "unknown"), uuid.NewString()); e != nil {
		t.Fatal("restriction discarded in-flight observation", e)
	}
	var held bool
	var state string
	if e = f.pool.QueryRow(ctx, "SELECT state,occupation_held FROM operations WHERE tenant_id=$1 AND id=$2", f.tenant, ids[0]).Scan(&state, &held); e != nil || state != "unknown" || !held {
		t.Fatal("uncertainty lost", state, held, e)
	}
	var query string
	if e = f.pool.QueryRow(ctx, "SELECT task_id FROM rule_delivery_bindings WHERE tenant_id=$1 AND deployment_id=$2 AND phase='query'", f.tenant, d.ID).Scan(&query); e != nil {
		t.Fatal(e)
	}
	if _, _, _, e = service.RuleCommand(ctx, q, f.tenant, query); e != nil {
		t.Fatal("restriction blocked safe query", e)
	}
	// Late prepared observations from both members may only schedule aborts,
	// even when the current desired revision still equals the deployment.
	for _, c := range []*controlv1.RuleCommand{second, mustRuleCommand(t, f, query)} {
		l := uuid.NewString()
		if _, e = f.pool.Exec(ctx, "UPDATE tasks SET state='running',lease_token=$3 WHERE tenant_id=$1 AND id=$2", f.tenant, c.TaskKey, l); e != nil {
			t.Fatal(e)
		}
		if e = o.ReceiptWithLease(ctx, f.scope, c.TaskKey, l, receipt(c, "prepared"), uuid.NewString()); e != nil {
			t.Fatal("prepared observation lost", e)
		}
	}
	var commits, aborts int
	if e = f.pool.QueryRow(ctx, "SELECT count(*) FILTER(WHERE phase='commit'),count(*) FILTER(WHERE phase='abort') FROM rule_delivery_bindings WHERE tenant_id=$1 AND deployment_id=$2", f.tenant, d.ID).Scan(&commits, &aborts); e != nil || commits != 0 || aborts != 2 {
		t.Fatal("restriction scheduled activation or lost abort", commits, aborts, e)
	}
	other := ruleInput(f.group)
	other.Port = 20001
	reserveRulePort(t, f, other)
	if _, e = s.Mutate(ctx, f.scope, other, 0, uuid.NewString()); e != nil {
		t.Fatal("local restriction blocked another rule", e)
	}
	billing := durableBilling(t, f, other.ID, 1, 3)
	if _, e = s.CompileRevision(ctx, f.scope, other.ID, 1, billing, 2, 3, 5, uuid.NewString()); e != nil {
		t.Fatal("unaffected rule cannot compile", e)
	}
	if _, e = s.Mutate(ctx, f.scope, r, 1, uuid.NewString()); !errors.Is(e, service.ErrConflict) {
		t.Fatal("restricted rule re-enabled", e)
	}
}

func mustRuleCommand(t *testing.T, f *portFixture, task string) *controlv1.RuleCommand {
	t.Helper()
	c, _, _, e := service.RuleCommand(context.Background(), generated.New(f.pool), f.tenant, task)
	if e != nil {
		t.Fatal(e)
	}
	return c
}

func TestDowngradeAuditRollbackWorkerRetryAndStaleConfirmation(t *testing.T) {
	f := newPortFixture(t)
	ctx := context.Background()
	if _, e := f.pool.Exec(ctx, "INSERT INTO users(id,tenant_id,role,state) VALUES($1,$2,'owner','active')", f.scope.ActorID(), f.tenant); e != nil {
		t.Fatal(e)
	}
	old := subscribePackage(t, f, createPackage(t, f, packageTerms()))
	p := packageTerms()
	p.RuleLimit = 0
	target := createPackage(t, f, p)
	s := service.PackageService{Pool: f.pool}
	at := time.Now().UTC().Add(time.Hour).Truncate(time.Microsecond)
	raw, e := s.PreviewDowngrade(ctx, f.scope, target, 1, nil, at)
	if e != nil {
		t.Fatal(e)
	}
	var preview struct {
		Digest string `json:"digest"`
	}
	if e = json.Unmarshal(raw, &preview); e != nil {
		t.Fatal(e)
	}
	tables := []string{"subscriptions", "package_snapshots", "traffic_sources", "source_expirations", "package_changes", "restriction_events", "current_restrictions", "tasks", "operations", "outbox", "audit_events", "package_requests"}
	request := uuid.NewString()
	failAudit(t, f.pool, request)
	before := authoritySnapshot(t, f.pool, f.tenant, tables)
	if _, e = s.ConfirmDowngrade(ctx, f.scope, target, 1, nil, at, preview.Digest, uuid.NewString(), request); e == nil {
		t.Fatal("confirmation escaped audit failure")
	}
	if !bytes.Equal(before, authoritySnapshot(t, f.pool, f.tenant, tables)) {
		t.Fatal("partial confirmation")
	}
	if _, e = f.pool.Exec(ctx, "UPDATE users SET state='suspended' WHERE id=$1", f.scope.ActorID()); e != nil {
		t.Fatal(e)
	}
	if _, e = s.ConfirmDowngrade(ctx, f.scope, target, 1, nil, at, preview.Digest, uuid.NewString(), uuid.NewString()); !errors.Is(e, service.ErrScope) {
		t.Fatal("revoked actor confirmed", e)
	}
	if _, e = f.pool.Exec(ctx, "UPDATE users SET state='active' WHERE id=$1", f.scope.ActorID()); e != nil {
		t.Fatal(e)
	}
	result, e := s.ConfirmDowngrade(ctx, f.scope, target, 1, nil, at, preview.Digest, uuid.NewString(), uuid.NewString())
	if e != nil {
		t.Fatal(e)
	}
	var change string
	if e = f.pool.QueryRow(ctx, "SELECT id FROM package_changes WHERE tenant_id=$1 AND task_id=$2", f.tenant, result.OperationID).Scan(&change); e != nil {
		t.Fatal(e)
	}
	// A real PostgreSQL audit fault must roll back every effect of the worker;
	// removal of the fault makes the same durable pending change retryable.
	name := "due_fault_" + uuid.NewString()[:8]
	if _, e = f.pool.Exec(ctx, "CREATE FUNCTION "+name+"() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.request_id='"+change+"' THEN RAISE EXCEPTION 'due audit fault'; END IF; RETURN NEW; END $$; CREATE TRIGGER "+name+" BEFORE INSERT ON audit_events FOR EACH ROW EXECUTE FUNCTION "+name+"()"); e != nil {
		t.Fatal(e)
	}
	drop := func() {
		if _, e := f.pool.Exec(ctx, "DROP TRIGGER IF EXISTS "+name+" ON audit_events; DROP FUNCTION IF EXISTS "+name+"()"); e != nil {
			t.Error(e)
		}
	}
	t.Cleanup(drop)
	before = authoritySnapshot(t, f.pool, f.tenant, tables)
	w := worker.PackageWorker{Pool: f.pool, Now: func() time.Time { return at }}
	if _, e = w.Once(ctx); e == nil {
		t.Fatal("due escaped audit failure")
	}
	if !bytes.Equal(before, authoritySnapshot(t, f.pool, f.tenant, tables)) {
		t.Fatal("partial due apply")
	}
	drop()
	if worked, e := w.Once(ctx); e != nil || !worked {
		t.Fatal("pending change did not retry", worked, e)
	}
	if worked, e := w.Once(ctx); e != nil || worked {
		t.Fatal("duplicate due apply", worked, e)
	}
	sub, e := generated.New(f.pool).GetSubscription(ctx, f.tenant)
	if e != nil || sub.Version != old.Version+1 || sub.SnapshotID != result.SnapshotID {
		t.Fatal("retry applied wrong authority", sub, e)
	}
	// A preview is bound to the subscription authority, not merely the target.
	if _, e = s.ConfirmDowngrade(ctx, f.scope, target, 1, nil, at, preview.Digest, uuid.NewString(), uuid.NewString()); !errors.Is(e, service.ErrConflict) {
		t.Fatal("stale preview accepted", e)
	}
}

func TestDowngradePreviewConfirmDueAuthority(t *testing.T) {
	f := newPortFixture(t)
	ctx := context.Background()
	if _, e := f.pool.Exec(ctx, "INSERT INTO users(id,tenant_id,role,state) VALUES($1,$2,'owner','active')", f.scope.ActorID(), f.tenant); e != nil {
		t.Fatal(e)
	}
	old := subscribePackage(t, f, createPackage(t, f, packageTerms()))
	p := packageTerms()
	p.Capabilities = []string{"direct"}
	p.RuleLimit = 0
	target := createPackage(t, f, p)
	r := ruleInput(f.group)
	reserveRulePort(t, f, r)
	if _, e := (service.RuleService{Pool: f.pool}).Mutate(ctx, f.scope, r, 0, uuid.NewString()); e != nil {
		t.Fatal(e)
	}
	api, ok := any(service.PackageService{Pool: f.pool}).(downgradeAPI)
	if !ok {
		t.Fatal("read-only downgrade preview and atomic future confirmation service missing")
	}
	at := time.Now().UTC().Add(time.Hour).Truncate(time.Microsecond)
	tables := []string{"subscriptions", "package_snapshots", "traffic_sources", "source_expirations", "package_changes", "restriction_events", "current_restrictions", "tasks", "operations", "outbox", "audit_events"}
	before := authoritySnapshot(t, f.pool, f.tenant, tables)
	raw, e := api.PreviewDowngrade(ctx, f.scope, target, 1, nil, at)
	if e != nil {
		t.Fatal(e)
	}
	var preview struct {
		Digest           string   `json:"digest"`
		InvalidRules     []string `json:"invalid_rules"`
		LostCapabilities []string `json:"lost_capabilities"`
		Discarded        []any    `json:"discarded_amounts"`
		Resources        []string `json:"affected_resources"`
	}
	if e = json.Unmarshal(raw, &preview); e != nil {
		t.Fatal(e)
	}
	if len(preview.InvalidRules) != 1 || preview.InvalidRules[0] != r.ID || len(preview.LostCapabilities) == 0 || len(preview.Discarded) != 1 || len(preview.Resources) == 0 || preview.Digest == "" {
		t.Fatal(string(raw))
	}
	if !bytes.Equal(before, authoritySnapshot(t, f.pool, f.tenant, tables)) {
		t.Fatal("preview mutated authority")
	}
	key := uuid.NewString()
	result, e := api.ConfirmDowngrade(ctx, f.scope, target, 1, nil, at, preview.Digest, key, uuid.NewString())
	if e != nil {
		t.Fatal(e)
	}
	replay, e := api.ConfirmDowngrade(ctx, f.scope, target, 1, nil, at, preview.Digest, key, uuid.NewString())
	if e != nil || replay != result {
		t.Fatal("confirmation replay", e)
	}
	if _, e = api.ConfirmDowngrade(ctx, f.scope, target, 1, nil, at.Add(time.Second), preview.Digest, key, uuid.NewString()); !errors.Is(e, service.ErrConflict) {
		t.Fatal("payload conflict", e)
	}
	sub, e := generated.New(f.pool).GetSubscription(ctx, f.tenant)
	if e != nil || sub.SnapshotID != old.SnapshotID {
		t.Fatal("future change active early", e)
	}
	var activeNow, activeAt bool
	if e = f.pool.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM current_restrictions WHERE tenant_id=$1 AND resource_id=$2 AND cleared_at IS NULL AND effective_at<=now())", f.tenant, r.ID).Scan(&activeNow); e != nil || activeNow {
		t.Fatal("future rule restriction became active early", activeNow, e)
	}
	if worked, e := api.ProcessDuePackageChanges(ctx, at.Add(-time.Microsecond)); e != nil || worked {
		t.Fatal("early due execution", worked, e)
	}
	if worked, e := api.ProcessDuePackageChanges(ctx, at); e != nil || !worked {
		t.Fatal("due execution", worked, e)
	}
	if e = f.pool.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM current_restrictions WHERE tenant_id=$1 AND resource_id=$2 AND cleared_at IS NULL AND effective_at<=$3)", f.tenant, r.ID, at).Scan(&activeAt); e != nil || !activeAt {
		t.Fatal("rule restriction projection missing at effective time", activeAt, e)
	}
	sub, e = generated.New(f.pool).GetSubscription(ctx, f.tenant)
	if e != nil || sub.SnapshotID != result.SnapshotID || sub.Version != old.Version+1 {
		t.Fatal("due snapshot not activated", sub, e)
	}
	if worked, e := api.ProcessDuePackageChanges(ctx, at); e != nil || worked {
		t.Fatal("duplicate due execution", worked, e)
	}
	var count int
	if e = f.pool.QueryRow(ctx, "SELECT count(*) FROM port_allocations WHERE tenant_id=$1 AND state='retained' AND retained_until=$2", f.tenant, at.Add(7*24*time.Hour)).Scan(&count); e != nil || count != 1 {
		t.Fatal("seven-day retention", count, e)
	}
}
