package service_test

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"testing"
	"time"
	"vortex/backend/internal/auth"
	"vortex/backend/internal/policy"
	"vortex/backend/internal/service"
	"vortex/backend/internal/testutil"
)

func TestValkeyLossRebuildAllProducedAuthority(t *testing.T) {
	pool := testutil.Postgres(t)
	ctx := context.Background()
	address := testutil.Valkey(t)
	tenant := seedTenant(t, pool)
	own := policyScope(t, tenant, auth.Owner, policy.Mutate)
	s := service.Tracer{Pool: pool}
	tenants := service.TenantService{Pool: pool}
	user, private, platform := uuid.NewString(), uuid.NewString(), uuid.NewString()
	if _, err := pool.Exec(ctx, "INSERT INTO users(id,tenant_id,role) VALUES($1,$2,'member');", user, tenant); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, "INSERT INTO resource_ownership(resource_id,resource_kind,owner_kind,owner_tenant_id) VALUES($1,'node','tenant',$2),($3,'node','platform',NULL)", private, tenant, platform); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	grantScope := policyScope(t, tenant, auth.PlatformAdmin, policy.Grant)
	if err := tenants.GrantResource(ctx, grantScope, platform, 1, now.Add(-time.Hour), now.Add(time.Hour), uuid.NewString()); err != nil {
		t.Fatal(err)
	}
	restriction := service.Restriction{Cause: "quota_exhausted", SourceID: uuid.NewString(), SourceVersion: 1, EffectiveAt: now.Add(-time.Minute)}
	if err := tenants.ChangeRestriction(ctx, policyScope(t, tenant, auth.Operator, policy.Restrict), restriction, uuid.NewString()); err != nil {
		t.Fatal(err)
	}
	op, err := s.Mutate(ctx, own, mutation())
	if err != nil {
		t.Fatal(err)
	}
	if err = s.Reconcile(ctx, tenant, op.ID, service.Receipt{Revision: 1, Generation: 1, State: "succeeded", Payload: []byte("confirmed receipt")}); err != nil {
		t.Fatal(err)
	}
	unknown, err := s.Mutate(ctx, own, mutation())
	if err != nil {
		t.Fatal(err)
	}
	if err = s.Reconcile(ctx, tenant, unknown.ID, service.Receipt{Revision: 1, Generation: 1, State: "unknown", Payload: []byte("connection lost")}); err != nil {
		t.Fatal(err)
	}
	tables := []string{"desired_state", "operations", "tasks", "outbox", "audit_events", "control_receipts", "users", "resource_grants", "restriction_events", "current_restrictions"}
	facts := authoritySnapshot(t, pool, tenant, tables)
	ownership := func() string {
		var raw string
		if e := pool.QueryRow(ctx, "SELECT jsonb_agg(to_jsonb(r) ORDER BY resource_id)::text FROM resource_ownership r WHERE resource_id IN ($1,$2)", private, platform).Scan(&raw); e != nil {
			t.Fatal(e)
		}
		return raw
	}
	owners := ownership()
	before, err := s.RebuildCache(ctx, own, address)
	if err != nil {
		t.Fatal(err)
	}
	var projection service.AuthorityProjection
	if err = json.Unmarshal(before, &projection); err != nil {
		t.Fatal(err)
	}
	if len(projection.Operations) != 2 || len(projection.Tasks) != 2 || len(projection.Resources) != 2 || len(projection.Grants) != 1 || len(projection.Restrictions) != 1 || len(projection.Users) != 1 {
		t.Fatalf("nonempty producers missing in projection: %+v", projection)
	}
	if _, err = service.CacheCommand(ctx, address, "FLUSHALL"); err != nil {
		t.Fatal(err)
	}
	if _, err = service.CacheCommand(ctx, address, "GET", "vortex:authority:"+tenant); err == nil {
		t.Fatal("flush did not remove projection")
	}
	if _, err = s.RebuildCache(ctx, own, testutil.DisconnectedValkey(t)); err == nil {
		t.Fatal("established cache disconnect not surfaced")
	}
	if !bytes.Equal(facts, authoritySnapshot(t, pool, tenant, tables)) || owners != ownership() {
		t.Fatal("cache loss changed PostgreSQL authority")
	}
	after, err := s.RebuildCache(ctx, own, address)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("PostgreSQL rebuild changed projection", err)
	}
	cached, err := service.CacheCommand(ctx, address, "GET", "vortex:authority:"+tenant)
	if err != nil || cached != string(after) {
		t.Fatal("rebuild absent from real Valkey", err)
	}
	if !bytes.Equal(facts, authoritySnapshot(t, pool, tenant, tables)) || owners != ownership() {
		t.Fatal("rebuild wrote authoritative rows")
	}
	if _, err = s.RebuildCache(ctx, policyScope(t, tenant, auth.Auditor, policy.Read), address); err == nil {
		t.Fatal("platform cache maintenance bypassed read audit")
	}
	// Source ledger/balance producers do not exist until 06-05. The existing
	// WINDOWS entry remains open; an empty balance set is not evidence.
}
