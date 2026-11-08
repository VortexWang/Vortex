package service_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"vortex/backend/db/generated"
	"vortex/backend/internal/service"
	"vortex/backend/internal/testutil"
	"vortex/backend/internal/worker"
)

func scope(t *testing.T) service.TenantScope {
	t.Helper()
	tenant := uuid.NewString()
	s, err := service.NewTenantScope(uuid.NewString(), tenant, tenant)
	if err != nil {
		t.Fatal(err)
	}
	return s
}
func mutation() service.Mutation {
	id := uuid.NewString()
	return service.Mutation{ResourceID: id, TargetKind: "node", Revision: 1, Generation: 1, IdempotencyKey: uuid.NewString(), RequestID: uuid.NewString(), Payload: []byte(`{"version":1,"node_id":"` + id + `","listeners":[],"rules":[],"revocations":[]}`)}
}

type fixtureAdapter struct{ fail bool }

func (a fixtureAdapter) Deliver(ctx context.Context, op generated.Operation) (service.Receipt, error) {
	if a.fail {
		return service.Receipt{}, context.DeadlineExceeded
	}
	return service.Receipt{Revision: op.TargetRevision, Generation: op.Generation, State: "succeeded", Payload: []byte("receipt:" + op.ID)}, nil
}

func TestTracerDesiredStateReceipt(t *testing.T) {
	pool := testutil.Postgres(t)
	ctx := context.Background()
	s := &service.Tracer{Pool: pool}
	tenant := scope(t)
	seedTracerTenant(t, pool, tenant)
	m := mutation()
	op, err := s.Mutate(ctx, tenant, m)
	if err != nil {
		t.Fatal(err)
	}
	var counts [4]int
	for i, table := range []string{"operations", "tasks", "outbox", "audit_events"} {
		if err = pool.QueryRow(ctx, "SELECT count(*) FROM "+table+" WHERE tenant_id=$1", tenant.TenantID()).Scan(&counts[i]); err != nil || counts[i] != 1 {
			t.Fatalf("%s count %d: %v", table, counts[i], err)
		}
	}
	duplicate, err := s.Mutate(ctx, tenant, m)
	if err != nil || duplicate.ID != op.ID {
		t.Fatalf("replay: %v", err)
	}
	changed := m
	changed.Payload = []byte("different")
	if _, err = s.Mutate(ctx, tenant, changed); !errors.Is(err, service.ErrConflict) {
		t.Fatalf("conflicting duplicate %v", err)
	}
	if _, err = s.Operation(ctx, scope(t), op.ID); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("cross tenant read %v", err)
	}
	if _, err = service.NewTenantScope(uuid.NewString(), tenant.TenantID(), uuid.NewString()); !errors.Is(err, service.ErrScope) {
		t.Fatal("foreign mutation scope accepted")
	}
	w := &worker.Worker{Pool: pool, Adapter: fixtureAdapter{}}
	// Workers consume all existing pending tasks; each test has isolated tenant
	// facts but the shared queue intentionally exercises production leasing.
	for i := 0; i < 100; i++ {
		if _, err = w.Once(ctx); err != nil {
			t.Fatal(err)
		}
		got, e := s.Operation(ctx, tenant, op.ID)
		if e != nil {
			t.Fatal(e)
		}
		if got.State == "succeeded" {
			break
		}
	}
	got, err := s.Operation(ctx, tenant, op.ID)
	if err != nil || got.State != "succeeded" {
		t.Fatalf("receipt state %s %v", got.State, err)
	}
	receipt := service.Receipt{Revision: 1, Generation: 1, State: "succeeded", Payload: []byte("receipt:" + op.ID)}
	if err = s.Reconcile(ctx, tenant.TenantID(), op.ID, receipt); err != nil {
		t.Fatal(err)
	}
	receipt.Revision = 2
	if err = s.Reconcile(ctx, tenant.TenantID(), op.ID, receipt); !errors.Is(err, service.ErrStaleReceipt) {
		t.Fatal("wrong revision advanced operation")
	}
	// Updating to a newer desired revision does not let a late old revision
	// mutate that current desired state.
	next := m
	next.Revision = 2
	next.ExpectedRevision = 1
	next.Generation = 2
	next.IdempotencyKey = uuid.NewString()
	nextOp, err := s.Mutate(ctx, tenant, next)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.Reconcile(ctx, tenant.TenantID(), nextOp.ID, service.Receipt{Revision: 1, Generation: 1, State: "succeeded", Payload: []byte("late")}); !errors.Is(err, service.ErrStaleReceipt) {
		t.Fatal("late receipt accepted")
	}
	w.Adapter = fixtureAdapter{fail: true}
	for i := 0; i < 100; i++ {
		if _, err = w.Once(ctx); err != nil {
			t.Fatal(err)
		}
		got, _ = s.Operation(ctx, tenant, nextOp.ID)
		if got.State == "unknown" {
			break
		}
	}
	if got.State != "unknown" || !got.OccupationHeld {
		t.Fatalf("unknown released occupation: %+v", got)
	}
	var delivered bool
	if err = pool.QueryRow(ctx, "SELECT delivered_at IS NOT NULL FROM outbox WHERE tenant_id=$1 AND operation_id=$2", tenant.TenantID(), nextOp.ID).Scan(&delivered); err != nil || delivered {
		t.Fatal("unknown marked delivered")
	}
	if err = s.Reconcile(ctx, tenant.TenantID(), nextOp.ID, service.Receipt{Revision: 2, Generation: 2, State: "succeeded", Payload: []byte("queried:" + nextOp.ID)}); err != nil {
		t.Fatal(err)
	}
	var taskState string
	if err = pool.QueryRow(ctx, "SELECT state FROM tasks WHERE tenant_id=$1 AND operation_id=$2", tenant.TenantID(), nextOp.ID).Scan(&taskState); err != nil || taskState != "succeeded" {
		t.Fatal("reconciled task diverged from operation")
	}
	got, err = s.Operation(ctx, tenant, nextOp.ID)
	if err != nil || !got.OccupationHeld {
		t.Fatal("reconciliation released occupation without proof")
	}
}

func TestTracerTransactionRollback(t *testing.T) {
	pool := testutil.Postgres(t)
	ctx := context.Background()
	s := &service.Tracer{Pool: pool}
	tenant := scope(t)
	seedTracerTenant(t, pool, tenant)
	m := mutation()
	// A real PostgreSQL trigger causes the final audit insert to fail, after
	// desired/operation/task/outbox writes, proving the shared rollback boundary.
	_, err := pool.Exec(ctx, `CREATE OR REPLACE FUNCTION phase6_audit_fault() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.request_id='phase6-audit-fault' THEN RAISE EXCEPTION 'injected audit failure'; END IF; RETURN NEW; END $$; CREATE TRIGGER phase6_audit_fault BEFORE INSERT ON audit_events FOR EACH ROW EXECUTE FUNCTION phase6_audit_fault()`)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Exec(ctx, "DROP TRIGGER phase6_audit_fault ON audit_events; DROP FUNCTION phase6_audit_fault()")
	m.RequestID = "phase6-audit-fault"
	if _, err = s.Mutate(ctx, tenant, m); err == nil || !strings.Contains(err.Error(), "injected audit failure") {
		t.Fatal("fault did not abort transaction")
	}
	for _, table := range []string{"desired_state", "operations", "tasks", "outbox", "audit_events"} {
		var count int
		if err = pool.QueryRow(ctx, "SELECT count(*) FROM "+table+" WHERE tenant_id=$1", tenant.TenantID()).Scan(&count); err != nil || count != 0 {
			t.Fatalf("%s survived rollback", table)
		}
	}
}

func TestTracerConcurrentMutation(t *testing.T) {
	pool := testutil.Postgres(t)
	ctx := context.Background()
	s := &service.Tracer{Pool: pool}
	tenant := scope(t)
	seedTracerTenant(t, pool, tenant)
	m := mutation()
	var wg sync.WaitGroup
	results := make(chan string, 8)
	errs := make(chan error, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); op, err := s.Mutate(ctx, tenant, m); results <- op.ID; errs <- err }()
	}
	wg.Wait()
	close(results)
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	var id string
	for got := range results {
		if id != "" && got != id {
			t.Fatal("duplicate effect")
		}
		id = got
	}
}

func TestValkeyLossRebuild(t *testing.T) {
	pool := testutil.Postgres(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	address := os.Getenv("PHASE6_VALKEY_ADDR")
	if address == "" {
		t.Fatal("PHASE6_VALKEY_ADDR required")
	}
	tenant := scope(t)
	seedTracerTenant(t, pool, tenant)
	s := &service.Tracer{Pool: pool}
	m := mutation()
	op, err := s.Mutate(ctx, tenant, m)
	if err != nil {
		t.Fatal(err)
	}
	w := &worker.Worker{Pool: pool, Adapter: fixtureAdapter{}}
	for i := 0; i < 100; i++ {
		if _, err = w.Once(ctx); err != nil {
			t.Fatal(err)
		}
		got, _ := s.Operation(ctx, tenant, op.ID)
		if got.State == "succeeded" {
			break
		}
	}
	before, err := s.RebuildCache(ctx, tenant, address)
	if err != nil {
		t.Fatal(err)
	}
	// All currently implemented authority columns, not only the cached fields,
	// remain identical while the optional cache disappears. Source balances are
	// introduced by 06-05 and must extend this test there.
	snapshot := func() []byte {
		var raw []byte
		err := pool.QueryRow(ctx, "SELECT to_jsonb(o)::text FROM operations o WHERE tenant_id=$1 AND id=$2", tenant.TenantID(), op.ID).Scan(&raw)
		if err != nil {
			t.Fatal(err)
		}
		return raw
	}
	facts := snapshot()
	if _, err = service.CacheCommand(ctx, address, "FLUSHALL"); err != nil {
		t.Fatal(err)
	}
	if _, err = service.CacheCommand(ctx, address, "GET", "vortex:authority:"+tenant.TenantID()); err == nil {
		t.Fatal("cache did not disappear")
	}
	if _, err = s.RebuildCache(ctx, tenant, "127.0.0.1:1"); err == nil {
		t.Fatal("connection loss not detected")
	}
	if !bytes.Equal(facts, snapshot()) {
		t.Fatal("cache loss changed authority")
	}
	after, err := s.RebuildCache(ctx, tenant, address)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatalf("rebuild %v", err)
	}
	cached, err := service.CacheCommand(ctx, address, "GET", "vortex:authority:"+tenant.TenantID())
	if err != nil || cached != string(before) {
		t.Fatal("projection not rebuilt")
	}
	var parsed any
	if json.Unmarshal(after, &parsed) != nil {
		t.Fatal("invalid projection")
	}
	_ = sha256.Sum256(after)
}

func seedTracerTenant(t *testing.T, pool *pgxpool.Pool, s service.TenantScope) {
	t.Helper()
	ctx := context.Background()
	if _, e := pool.Exec(ctx, `INSERT INTO tenants(id) VALUES($1)`, s.TenantID()); e != nil {
		t.Fatal(e)
	}
	if _, e := pool.Exec(ctx, `INSERT INTO users(id,tenant_id,role) VALUES($1,$2,'owner')`, s.ActorID(), s.TenantID()); e != nil {
		t.Fatal(e)
	}
}
