package service_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"net/url"
	"os"
	"testing"
	"time"
	"vortex/backend/db"
	"vortex/backend/db/generated"
	"vortex/backend/internal/auth"
	"vortex/backend/internal/policy"
	"vortex/backend/internal/repository"
	"vortex/backend/internal/service"
	"vortex/backend/internal/testutil"
)

func timestampTest(t time.Time) pgtype.Timestamptz {
	return pgtype.Timestamptz{Time: t.UTC().Truncate(time.Microsecond), Valid: true}
}

func TestTenantRestrictionSchema(t *testing.T) {
	pool := testutil.Postgres(t)
	for _, table := range []string{"tenants", "users", "platform_principals", "resource_grants", "restriction_events", "current_restrictions"} {
		var exists bool
		if err := pool.QueryRow(context.Background(), "SELECT to_regclass($1) IS NOT NULL", table).Scan(&exists); err != nil {
			t.Fatal(err)
		}
		if !exists {
			t.Errorf("tenant policy authority table %s is missing", table)
		}
	}
}

func policyScope(t *testing.T, tenant string, role auth.Role, action policy.Action) repository.TenantScope {
	t.Helper()
	home := tenant
	if role == auth.PlatformAdmin || role == auth.Operator || role == auth.Auditor {
		home = ""
	}
	p, err := auth.NewPrincipal(uuid.NewString(), home, role, "active")
	if err != nil {
		t.Fatal(err)
	}
	s, err := repository.NewTenantScope(p, tenant, action)
	if err != nil {
		t.Fatal(err)
	}
	return s
}
func seedTenant(t *testing.T, pool *pgxpool.Pool) string {
	t.Helper()
	id := uuid.NewString()
	if _, err := generated.New(pool).InsertTenant(context.Background(), id); err != nil {
		t.Fatal(err)
	}
	return id
}

func TestTenantRepositoryScopeAndPlatformAudit(t *testing.T) {
	pool := testutil.Postgres(t)
	ctx := context.Background()
	tenant, foreign := seedTenant(t, pool), seedTenant(t, pool)
	own := policyScope(t, tenant, auth.Owner, policy.Mutate)
	other := policyScope(t, foreign, auth.Owner, policy.Mutate)
	tracer := service.Tracer{Pool: pool}
	op, err := tracer.Mutate(ctx, own, mutation())
	if err != nil {
		t.Fatal(err)
	}
	repo := repository.TenantRepository{Pool: pool}
	if _, err = repo.Operation(ctx, other, op.ID, "foreign-read"); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("foreign read exposed resource: %v", err)
	}
	if _, err = repo.Operation(ctx, repository.TenantScope{}, op.ID, "zero"); !errors.Is(err, policy.ErrForbidden) {
		t.Fatalf("zero scope: %v", err)
	}
	member, err := auth.NewPrincipal(uuid.NewString(), tenant, auth.Member, "active")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = repository.NewTenantScope(member, foreign, policy.Mutate); !errors.Is(err, policy.ErrForbidden) {
		t.Fatal("foreign mutation capability minted")
	}
	auditor := policyScope(t, tenant, auth.Auditor, policy.Read)
	if _, err = tracer.Mutate(ctx, auditor, mutation()); !errors.Is(err, policy.ErrForbidden) {
		t.Fatalf("read capability wrote: %v", err)
	}
	for _, role := range []auth.Role{auth.PlatformAdmin, auth.Operator, auth.Auditor} {
		scope := policyScope(t, tenant, role, policy.Read)
		request := uuid.NewString()
		got, e := repo.Operation(ctx, scope, op.ID, request)
		if e != nil || got.ID != op.ID {
			t.Fatalf("platform read: %v", e)
		}
		var count int
		if e = pool.QueryRow(ctx, "SELECT count(*) FROM audit_events WHERE tenant_id=$1 AND actor_id=$2 AND request_id=$3 AND action='operation.read'", tenant, scope.ActorID(), request).Scan(&count); e != nil || count != 1 {
			t.Fatal("platform read missing audit", e)
		}
	}
	// An audit failure must suppress the returned sensitive result as well as commit.
	_, err = repo.Operation(ctx, auditor, op.ID, "")
	if err == nil {
		t.Fatal("platform read without audit context succeeded")
	}
	admin := policyScope(t, tenant, auth.PlatformAdmin, policy.ManageAccess)
	tenants := service.TenantService{Pool: pool}
	if err = tenants.SetAccountState(ctx, admin, "suspended", 1, uuid.NewString()); err != nil {
		t.Fatal(err)
	}
	if err = tenants.SetAccountState(ctx, admin, "expired", 2, uuid.NewString()); !errors.Is(err, service.ErrConflict) {
		t.Fatal("restriction became account status")
	}
	if err = tenants.SetAccountState(ctx, own, "active", 2, uuid.NewString()); !errors.Is(err, service.ErrScope) {
		t.Fatal("tenant self-restored platform account")
	}
	if err = tenants.SetAccountState(ctx, admin, "active", 1, uuid.NewString()); !errors.Is(err, service.ErrConflict) {
		t.Fatal("stale account CAS succeeded")
	}
}

func TestTenantRestrictionsIndependentAndImmutable(t *testing.T) {
	pool := testutil.Postgres(t)
	ctx := context.Background()
	tenant := seedTenant(t, pool)
	admin := policyScope(t, tenant, auth.PlatformAdmin, policy.Restrict)
	own := policyScope(t, tenant, auth.Owner, policy.Read)
	s := service.TenantService{Pool: pool}
	now := time.Now().UTC().Truncate(time.Microsecond)
	a := service.Restriction{Cause: "package_expired", SourceID: uuid.NewString(), SourceVersion: 1, EffectiveAt: now.Add(-time.Minute)}
	b := a
	b.Cause = "quota_exhausted"
	b.SourceID = uuid.NewString()
	for _, r := range []service.Restriction{a, b, a} {
		if err := s.ChangeRestriction(ctx, admin, r, uuid.NewString()); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.ChangeRestriction(ctx, own, a, uuid.NewString()); !errors.Is(err, service.ErrScope) {
		t.Fatal("tenant altered restriction")
	}
	rows, err := s.Restrictions(ctx, own, now, "")
	if err != nil || len(rows) != 2 {
		t.Fatalf("active restrictions: %d %v", len(rows), err)
	}
	a.ClearedAt = now
	if err = s.ChangeRestriction(ctx, admin, a, uuid.NewString()); err != nil {
		t.Fatal(err)
	}
	if err = s.ChangeRestriction(ctx, admin, a, uuid.NewString()); err != nil {
		t.Fatal("clear replay", err)
	}
	rows, err = s.Restrictions(ctx, own, now, "")
	if err != nil || len(rows) != 1 || rows[0].Cause != b.Cause {
		t.Fatal("clearing one restriction restored all business", err)
	}
	tenantRow, err := s.Tenant(ctx, own, "")
	if err != nil || tenantRow.AccountState != "active" {
		t.Fatal("business restriction changed account status", err)
	}
	q := generated.New(pool)
	history, err := q.ListRestrictionHistory(ctx, tenant)
	if err != nil || len(history) != 3 {
		t.Fatal("immutable apply/clear history", err)
	}
	if _, err = pool.Exec(ctx, "UPDATE restriction_events SET source_id='rewritten' WHERE tenant_id=$1", tenant); err == nil {
		t.Fatal("restriction history mutable")
	}
	if _, err = pool.Exec(ctx, "DELETE FROM restriction_events WHERE tenant_id=$1", tenant); err == nil {
		t.Fatal("restriction history deletable")
	}
	b.ClearedAt = now
	if err = s.ChangeRestriction(ctx, admin, b, uuid.NewString()); err != nil {
		t.Fatal(err)
	}
	rows, err = s.Restrictions(ctx, own, now, "")
	if err != nil || len(rows) != 0 {
		t.Fatal("all-cleared still restricted", err)
	}
	a.SourceVersion = 2
	a.ClearedAt = time.Time{}
	if err = s.ChangeRestriction(ctx, admin, a, uuid.NewString()); err != nil {
		t.Fatal(err)
	}
	a.SourceVersion = 1
	a.ClearedAt = now
	if err = s.ChangeRestriction(ctx, admin, a, uuid.NewString()); err != nil {
		t.Fatal("historical clear replay", err)
	}
	rows, err = s.Restrictions(ctx, own, now, "")
	if err != nil || len(rows) != 1 || rows[0].SourceVersion != 2 {
		t.Fatal("stale clear removed newer restriction", err)
	}
}

func TestTenantPlatformOwnershipGrant(t *testing.T) {
	pool := testutil.Postgres(t)
	ctx := context.Background()
	tenant, foreign := seedTenant(t, pool), seedTenant(t, pool)
	platform, private := uuid.NewString(), uuid.NewString()
	if _, err := pool.Exec(ctx, "INSERT INTO resource_ownership(resource_id,resource_kind,owner_kind,owner_tenant_id) VALUES($1,'node','platform',NULL),($2,'node','tenant',$3)", platform, private, tenant); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	s := service.TenantService{Pool: pool}
	admin := policyScope(t, foreign, auth.PlatformAdmin, policy.Grant)
	if err := s.GrantResource(ctx, admin, private, 1, now.Add(-time.Minute), now.Add(time.Hour), uuid.NewString()); !errors.Is(err, service.ErrScope) {
		t.Fatal("admin reassigned self-hosted node", err)
	}
	if err := s.GrantResource(ctx, admin, platform, 1, now.Add(-time.Minute), now.Add(time.Hour), uuid.NewString()); err != nil {
		t.Fatal(err)
	}
	foreignScope := policyScope(t, foreign, auth.Member, policy.Read)
	resources, err := s.Resources(ctx, foreignScope, now, "")
	if err != nil || len(resources) != 1 || resources[0].ResourceID != platform || resources[0].OwnerKind != "platform" || resources[0].OwnerTenantID.Valid {
		t.Fatal("grant changed ownership or exposed private resource", err)
	}
	own := policyScope(t, tenant, auth.Owner, policy.Read)
	resources, err = s.Resources(ctx, own, now, "")
	if err != nil || len(resources) != 1 || resources[0].ResourceID != private {
		t.Fatal("tenant resources predicate", err)
	}
	resources, err = s.Resources(ctx, foreignScope, now.Add(2*time.Hour), "")
	if err != nil || len(resources) != 0 {
		t.Fatal("expired grant still usable", err)
	}
	if _, err = pool.Exec(ctx, "INSERT INTO resource_grants(id,tenant_id,resource_id,version,status,valid_from,valid_until) VALUES($1,$2,$3,1,'active',now(),now()+interval '1 hour')", uuid.NewString(), foreign, private); err == nil {
		t.Fatal("database allowed self-hosted cross-tenant grant")
	}
}

func authoritySnapshot(t *testing.T, pool *pgxpool.Pool, tenant string, tables []string) []byte {
	t.Helper()
	values := map[string]json.RawMessage{}
	for _, table := range tables {
		var raw []byte
		if err := pool.QueryRow(context.Background(), "SELECT COALESCE(jsonb_agg(to_jsonb(r) ORDER BY to_jsonb(r)::text),'[]'::jsonb)::text FROM "+table+" r WHERE tenant_id=$1", tenant).Scan(&raw); err != nil {
			t.Fatal(err)
		}
		values[table] = raw
	}
	raw, err := json.Marshal(values)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func TestTenantAuditRollbackSharedAuthority(t *testing.T) {
	pool := testutil.Postgres(t)
	ctx := context.Background()
	tenant := seedTenant(t, pool)
	scope := policyScope(t, tenant, auth.PlatformAdmin, policy.Mutate)
	tracer := service.Tracer{Pool: pool}
	m := mutation()
	op, err := tracer.Mutate(ctx, scope, m)
	if err != nil {
		t.Fatal(err)
	}
	if err = tracer.Reconcile(ctx, tenant, op.ID, service.Receipt{Revision: 1, Generation: 1, State: "succeeded", Payload: []byte("seed receipt")}); err != nil {
		t.Fatal(err)
	}
	tables := []string{"desired_state", "operations", "tasks", "outbox", "audit_events", "control_receipts", "restriction_events", "current_restrictions"}
	before := authoritySnapshot(t, pool, tenant, tables)
	err = (repository.AuditWriter{Pool: pool}).WithTransaction(ctx, scope, policy.Mutate, &repository.Audit{Action: "test.rollback", ResourceID: m.ResourceID, RequestID: uuid.NewString(), Reason: "audit insert failure", After: []byte("invalid JSON")}, func(q *generated.Queries) error {
		if _, e := q.PutDesiredState(ctx, generated.PutDesiredStateParams{TenantID: tenant, ResourceID: m.ResourceID, TargetKind: "node", Revision: 2, ExpectedRevision: 1, Payload: m.Payload, PayloadSha256: op.RequestSha256}); e != nil {
			return e
		}
		id := uuid.NewString()
		if _, e := q.InsertOperation(ctx, generated.InsertOperationParams{ID: id, TenantID: tenant, ResourceID: m.ResourceID, IdempotencyKey: id, RequestSha256: op.RequestSha256, TargetKind: "node", TargetRevision: 2, ExpectedRevision: 1, Generation: 2, Payload: m.Payload}); e != nil {
			return e
		}
		if e := q.InsertTask(ctx, generated.InsertTaskParams{ID: id, TenantID: tenant, OperationID: id}); e != nil {
			return e
		}
		if e := q.InsertOutbox(ctx, generated.InsertOutboxParams{TenantID: tenant, OperationID: id}); e != nil {
			return e
		}
		eventID := uuid.NewString()
		at := timestampTest(time.Now())
		if _, e := q.AppendRestrictionEvent(ctx, generated.AppendRestrictionEventParams{ID: eventID, TenantID: tenant, Cause: "manual", SourceID: id, SourceVersion: 1, EventKind: "applied", EffectiveAt: at}); e != nil {
			return e
		}
		_, e := q.ApplyCurrentRestriction(ctx, generated.ApplyCurrentRestrictionParams{TenantID: tenant, Cause: "manual", SourceID: id, SourceVersion: 1, EventID: eventID, EffectiveAt: at})
		return e
	})
	if err == nil {
		t.Fatal("audit fault did not fail")
	}
	if !bytes.Equal(before, authoritySnapshot(t, pool, tenant, tables)) {
		t.Fatal("authority/audit/task/outbox did not roll back together")
	}
}

func TestMigrationUpgradeTenantAuthority(t *testing.T) {
	admin := testutil.Postgres(t)
	ctx := context.Background()
	for _, seeded := range []bool{true, false} {
		t.Run(map[bool]string{true: "seeded00001", false: "empty"}[seeded], func(t *testing.T) {
			schema := "tenant_upgrade_" + uuid.New().String()[:8]
			if _, err := admin.Exec(ctx, "CREATE SCHEMA "+schema); err != nil {
				t.Fatal(err)
			}
			defer admin.Exec(ctx, "DROP SCHEMA "+schema+" CASCADE")
			u, err := url.Parse(os.Getenv("PHASE6_DATABASE_URL"))
			if err != nil {
				t.Fatal(err)
			}
			query := u.Query()
			query.Set("search_path", schema)
			u.RawQuery = query.Encode()
			if err = db.MigrateTo(ctx, u.String(), 1); err != nil {
				t.Fatal(err)
			}
			pool, err := pgxpool.New(ctx, u.String())
			if err != nil {
				t.Fatal(err)
			}
			defer pool.Close()
			own := scope(t)
			tables := []string{"desired_state", "operations", "tasks", "outbox", "audit_events", "control_receipts"}
			if seeded {
				tracer := service.Tracer{Pool: pool}
				op, e := tracer.Mutate(ctx, own, mutation())
				if e != nil {
					t.Fatal(e)
				}
				if e = tracer.Reconcile(ctx, own.TenantID(), op.ID, service.Receipt{Revision: 1, Generation: 1, State: "succeeded", Payload: []byte("upgrade receipt")}); e != nil {
					t.Fatal(e)
				}
			}
			before := authoritySnapshot(t, pool, own.TenantID(), tables)
			if err = db.Migrate(ctx, u.String()); err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(before, authoritySnapshot(t, pool, own.TenantID(), tables)) {
				t.Fatal("upgrade changed existing authoritative rows")
			}
			var present bool
			if err = pool.QueryRow(ctx, "SELECT to_regclass('restriction_events') IS NOT NULL AND to_regclass('current_restrictions') IS NOT NULL").Scan(&present); err != nil || !present {
				t.Fatal("new migration absent", err)
			}
		})
	}
}
