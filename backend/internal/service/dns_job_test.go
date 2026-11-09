package service_test

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"
	"vortex/backend/db/generated"
	"vortex/backend/internal/policy"
	"vortex/backend/internal/repository"
	"vortex/backend/internal/service"
	"vortex/backend/internal/worker"
)

func TestDNSJobActorBoundConcurrentConsumerUnknownParkAndReplay(t *testing.T) {
	isolatePackageAuthority(t)
	f, store, access, provider := dnsFixture(t)
	ctx := context.Background()
	a := f.prepare(t, 20000)
	f.commit(t, a)
	svc := service.ServingService{Pool: f.pool}
	op, e := svc.RecomputeDNS(ctx, f.scope, f.group, access, f.group+".example.test", uuid.NewString(), uuid.NewString())
	if e != nil {
		t.Fatal(e)
	}
	w := worker.DNSWorker{Pool: f.pool, Store: store}
	var wg sync.WaitGroup
	errs := make(chan error, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); _, e := w.Once(ctx); errs <- e }()
	}
	wg.Wait()
	close(errs)
	for e := range errs {
		if e != nil {
			t.Fatal(e)
		}
	}
	if provider.calls != 1 {
		t.Fatal("concurrent worker repeated provider effects", provider.calls)
	}
	saved, e := generated.New(f.pool).GetOperation(ctx, generated.GetOperationParams{TenantID: f.tenant, ID: op.ID})
	if e != nil || saved.State != "succeeded" || !saved.OccupationHeld {
		t.Fatal(saved, e)
	}
	tables := []string{"operations", "tasks", "outbox", "audit_events", "domain_events", "port_allocations", "port_reservations"}
	before := authoritySnapshot(t, f.pool, f.tenant, tables)
	if worked, e := svc.ProcessDNSJob(ctx, f.tenant, op.ID, uuid.NewString(), store); e != nil || worked {
		t.Fatal(worked, e)
	}
	if !bytes.Equal(before, authoritySnapshot(t, f.pool, f.tenant, tables)) {
		t.Fatal("succeeded replay changed authority")
	}
	provider.fail = true
	op, e = svc.RecomputeDNS(ctx, f.scope, f.group, access, f.group+".example.test", uuid.NewString(), uuid.NewString())
	if e != nil {
		t.Fatal(e)
	}
	if worked, e := w.Once(ctx); e != nil || !worked {
		t.Fatal(worked, e)
	}
	saved, e = generated.New(f.pool).GetOperation(ctx, generated.GetOperationParams{TenantID: f.tenant, ID: op.ID})
	if e != nil || saved.State != "unknown" || !saved.OccupationHeld {
		t.Fatal(saved, e)
	}
	if worked, e := w.Once(ctx); e != nil || worked || provider.calls != 2 {
		t.Fatal("unknown job was hot-retried", worked, e, provider.calls)
	}
	var held int
	if e = f.pool.QueryRow(ctx, `SELECT count(*) FROM port_reservations WHERE tenant_id=$1 AND state<>'released'`, f.tenant).Scan(&held); e != nil || held != 4 {
		t.Fatal(held, e)
	}
}
func TestDNSJobRevokedActorLegacyIsolationAndAuditLeaseRollback(t *testing.T) {
	isolatePackageAuthority(t)
	f, store, access, provider := dnsFixture(t)
	ctx := context.Background()
	svc := service.ServingService{Pool: f.pool}
	op, e := svc.RecomputeDNS(ctx, f.scope, f.group, access, f.group+".example.test", uuid.NewString(), uuid.NewString())
	if e != nil {
		t.Fatal(e)
	}
	// A legacy actorless payload is a historical hint, never worker authority.
	e = (repository.AuditWriter{Pool: f.pool}).WithTransaction(ctx, f.scope, policy.Mutate, &repository.Audit{Action: "dns.legacy.fixture", ResourceID: f.group, RequestID: uuid.NewString(), Reason: "actorless compatibility negative"}, func(q *generated.Queries) error {
		_, e := repository.EnqueueDNS(ctx, q, f.scope, uuid.NewString(), service.DNSDesired{Action: "dns.reconcile", GroupID: f.group, AccessID: access, Name: f.group + ".example.test"})
		return e
	})
	if e != nil {
		t.Fatal(e)
	}
	request := uuid.NewString()
	failAudit(t, f.pool, request)
	before := authoritySnapshot(t, f.pool, f.tenant, []string{"tasks", "operations", "outbox", "audit_events", "domain_events"})
	if worked, e := svc.ProcessDNSJob(ctx, f.tenant, op.ID, request, store); e == nil || !strings.Contains(e.Error(), "injected audit fault") || worked {
		t.Fatal(worked, e)
	}
	if !bytes.Equal(before, authoritySnapshot(t, f.pool, f.tenant, []string{"tasks", "operations", "outbox", "audit_events", "domain_events"})) {
		t.Fatal("lease escaped audit")
	}
	if provider.calls != 0 {
		t.Fatal("external call before lease commit")
	}
	if _, e = f.pool.Exec(ctx, `UPDATE users SET state='suspended' WHERE id=$1`, f.scope.ActorID()); e != nil {
		t.Fatal(e)
	}
	if worked, e := (worker.DNSWorker{Pool: f.pool, Store: store}).Once(ctx); e != nil || worked || provider.calls != 0 {
		t.Fatal(worked, e, provider.calls)
	}
	if _, e = f.pool.Exec(ctx, `UPDATE users SET state='active' WHERE id=$1`, f.scope.ActorID()); e != nil {
		t.Fatal(e)
	}
	if worked, e := (worker.DNSWorker{Pool: f.pool, Store: store}).Once(ctx); e != nil || !worked || provider.calls != 1 {
		t.Fatal(worked, e, provider.calls)
	}
	if worked, e := (worker.DNSWorker{Pool: f.pool, Store: store}).Once(ctx); e != nil || worked || provider.calls != 1 {
		t.Fatal("legacy job gained actor authority", worked, e, provider.calls)
	}
	// Payload validation is exact before any stored actor can be reconstructed.
	var desired service.DNSDesired
	if e = json.Unmarshal(op.Payload, &desired); e != nil || desired.Schema != 1 || desired.ActorID != f.scope.ActorID() {
		t.Fatal(desired, e)
	}
}
func TestDNSJobServingChangeParksWithoutExternalEffects(t *testing.T) {
	isolatePackageAuthority(t)
	f, store, access, provider := dnsFixture(t)
	ctx := context.Background()
	a := f.prepare(t, 20000)
	f.commit(t, a)
	svc := service.ServingService{Pool: f.pool}
	op, e := svc.RecomputeDNS(ctx, f.scope, f.group, access, f.group+".example.test", uuid.NewString(), uuid.NewString())
	if e != nil {
		t.Fatal(e)
	}
	if _, e = f.pool.Exec(ctx, `UPDATE nodes SET health='unhealthy' WHERE id=ANY($1::text[])`, f.nodes); e != nil {
		t.Fatal(e)
	}
	if worked, e := (worker.DNSWorker{Pool: f.pool, Store: store}).Once(ctx); e != nil || !worked || provider.calls != 0 {
		t.Fatal(worked, e, provider.calls)
	}
	saved, e := generated.New(f.pool).GetOperation(ctx, generated.GetOperationParams{TenantID: f.tenant, ID: op.ID})
	if e != nil || saved.State != "unknown" {
		t.Fatal(saved, e)
	}
}
