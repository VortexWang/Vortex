package service_test

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"testing"
	"time"
	"vortex/backend/internal/auth"
	"vortex/backend/internal/policy"
	"vortex/backend/internal/repository"
	"vortex/backend/internal/service"
	"vortex/backend/internal/testutil"
)

func TestNodeGroupAuthoritySchema(t *testing.T) {
	pool := testutil.Postgres(t)
	for _, table := range []string{"nodes", "node_groups", "node_group_members", "platform_grants", "grant_history", "segment_permits", "encrypted_credentials"} {
		var exists bool
		if err := pool.QueryRow(context.Background(), "SELECT to_regclass($1) IS NOT NULL", table).Scan(&exists); err != nil {
			t.Fatal(err)
		}
		if !exists {
			t.Errorf("node authority table %s missing", table)
		}
	}
}

func nodeInput() service.NodeInput {
	return service.NodeInput{ID: uuid.NewString(), Name: "节点", Region: "cn", HubID: uuid.NewString(), OwnerKind: "tenant", PublicAddress: "192.0.2.1", Capabilities: []string{"entry", "relay", "exit"}, PortMin: 20000, PortMax: 20100, LinkPort: 443, ControlPort: 8443, MetricsPort: 9090, Capacity: 100}
}
func groupInput() service.GroupInput {
	return service.GroupInput{ID: uuid.NewString(), Name: "入口组", OwnerKind: "tenant", Purpose: "entry", Scheduling: "dns", HealthIntervalSeconds: 10, HealthTimeoutSeconds: 2}
}
func mustNode(t *testing.T, s service.NodeService, scope repository.TenantScope, in service.NodeInput) string {
	t.Helper()
	if _, e := s.CreateNode(context.Background(), scope, in, uuid.NewString()); e != nil {
		t.Fatal(e)
	}
	return in.ID
}
func mustGroup(t *testing.T, s service.NodeService, scope repository.TenantScope, in service.GroupInput) string {
	t.Helper()
	if _, e := s.CreateGroup(context.Background(), scope, in, uuid.NewString()); e != nil {
		t.Fatal(e)
	}
	return in.ID
}

func TestNodeGroupOwnershipAndStableMembers(t *testing.T) {
	pool := testutil.Postgres(t)
	ctx := context.Background()
	tenant := seedTenant(t, pool)
	other := seedTenant(t, pool)
	scope := policyScope(t, tenant, auth.Owner, policy.Mutate)
	foreign := policyScope(t, other, auth.Owner, policy.Mutate)
	admin := policyScope(t, tenant, auth.PlatformAdmin, policy.Mutate)
	s := service.NodeService{Pool: pool}
	n := mustNode(t, s, scope, nodeInput())
	g := mustGroup(t, s, scope, groupInput())
	if e := s.SetMembers(ctx, scope, g, 1, []service.GroupMember{{NodeID: n, Weight: 1}}, uuid.NewString()); e != nil {
		t.Fatal(e)
	}
	a, e := s.Snapshot(ctx, scope, g, uuid.NewString())
	if e != nil {
		t.Fatal(e)
	}
	b, e := s.Snapshot(ctx, scope, g, uuid.NewString())
	if e != nil || a.MemberSetVersion != b.MemberSetVersion || len(a.Members) != 1 {
		t.Fatal("unstable member snapshot", e)
	}
	if _, e = (repository.NodeRepository{Pool: pool}).Node(ctx, foreign, n, uuid.NewString()); !errors.Is(e, pgx.ErrNoRows) {
		t.Fatal("foreign lookup leaked", e)
	}
	if e = s.Transition(ctx, foreign, n, "online", 1, 0, uuid.NewString()); !errors.Is(e, pgx.ErrNoRows) {
		t.Fatal("foreign node mutation", e)
	}
	bad := nodeInput()
	bad.PublicAddress = ""
	private := mustNode(t, s, scope, bad)
	if e = s.SetMembers(ctx, scope, g, 2, []service.GroupMember{{NodeID: private, Weight: 1}}, uuid.NewString()); e == nil {
		t.Fatal("private entry accepted")
	}
	after, e := s.Snapshot(ctx, scope, g, uuid.NewString())
	if e != nil || after.MemberSetVersion != a.MemberSetVersion {
		t.Fatal("failed replacement changed set", e)
	}
	p := nodeInput()
	p.OwnerKind = "platform"
	platform := mustNode(t, s, admin, p)
	if e = s.SetMembers(ctx, scope, g, 2, []service.GroupMember{{NodeID: platform, Weight: 1}}, uuid.NewString()); e == nil {
		t.Fatal("mixed ownership accepted")
	}
	bill := groupInput()
	bill.BillingMultiplierMilli = 1
	if _, e = s.CreateGroup(ctx, scope, bill, uuid.NewString()); e == nil {
		t.Fatal("tenant multiplier accepted")
	}
	for _, field := range []string{"purpose", "scheduling"} {
		bad := groupInput()
		if field == "purpose" {
			bad.Purpose = "unknown"
		} else {
			bad.Scheduling = "primary_backup"
		}
		if _, e = s.CreateGroup(ctx, scope, bad, uuid.NewString()); e == nil {
			t.Fatal("invalid group", field)
		}
	}
	var count int
	if e = pool.QueryRow(ctx, "SELECT count(*) FROM tasks WHERE tenant_id=$1 AND task_kind='resource_change'", tenant).Scan(&count); e != nil || count != 1 {
		t.Fatal("atomic membership revalidation task missing", e, count)
	}
	if _, e = pool.Exec(ctx, "UPDATE node_groups SET billing_multiplier_milli=1 WHERE id=$1", g); e == nil {
		t.Fatal("mutable multiplier bypass")
	}
}

func TestNodeGrantValidityHistoryAndAudit(t *testing.T) {
	pool := testutil.Postgres(t)
	ctx := context.Background()
	tenant := seedTenant(t, pool)
	s := service.NodeService{Pool: pool}
	admin := policyScope(t, tenant, auth.PlatformAdmin, policy.Mutate)
	in := nodeInput()
	in.OwnerKind = "platform"
	node := mustNode(t, s, admin, in)
	own := policyScope(t, tenant, auth.Owner, policy.Read)
	repo := repository.NodeRepository{Pool: pool}
	if _, e := repo.Node(ctx, own, node, uuid.NewString()); !errors.Is(e, pgx.ErrNoRows) {
		t.Fatal("ungranted platform read", e)
	}
	now := time.Now().UTC()
	grant := policyScope(t, tenant, auth.PlatformAdmin, policy.Grant)
	if e := s.SetGrant(ctx, grant, node, 0, "active", now.Add(time.Hour), now.Add(2*time.Hour), uuid.NewString()); e != nil {
		t.Fatal(e)
	}
	if _, e := repo.Node(ctx, own, node, uuid.NewString()); !errors.Is(e, pgx.ErrNoRows) {
		t.Fatal("future grant accepted", e)
	}
	if e := s.SetGrant(ctx, grant, node, 1, "active", now.Add(-time.Hour), now.Add(time.Hour), uuid.NewString()); e != nil {
		t.Fatal(e)
	}
	if _, e := repo.Node(ctx, own, node, uuid.NewString()); e != nil {
		t.Fatal(e)
	}
	if e := s.Transition(ctx, policyScope(t, tenant, auth.Owner, policy.Mutate), node, "online", 1, 0, uuid.NewString()); !errors.Is(e, service.ErrScope) {
		t.Fatal("grant transferred ownership", e)
	}
	for _, role := range []auth.Role{auth.PlatformAdmin, auth.Operator, auth.Auditor} {
		request := uuid.NewString()
		sco := policyScope(t, tenant, role, policy.Read)
		if _, e := repo.Node(ctx, sco, node, request); e != nil {
			t.Fatal(e)
		}
		var count int
		if e := pool.QueryRow(ctx, "SELECT count(*) FROM audit_events WHERE request_id=$1 AND tenant_id=$2", request, tenant).Scan(&count); e != nil || count != 1 {
			t.Fatal("missing read audit", e)
		}
	}
	if _, e := repo.Node(ctx, policyScope(t, tenant, auth.Auditor, policy.Read), node, ""); e == nil {
		t.Fatal("sensitive result escaped failed audit")
	}
	if e := s.SetGrant(ctx, grant, node, 1, "revoked", now.Add(-time.Hour), now.Add(time.Hour), uuid.NewString()); !errors.Is(e, service.ErrConflict) {
		t.Fatal("stale grant CAS", e)
	}
	if e := s.SetGrant(ctx, grant, node, 2, "revoked", now.Add(-time.Hour), now.Add(time.Hour), uuid.NewString()); e != nil {
		t.Fatal(e)
	}
	if _, e := repo.Node(ctx, own, node, uuid.NewString()); !errors.Is(e, pgx.ErrNoRows) {
		t.Fatal("revoked grant readable", e)
	}
	var count int
	if e := pool.QueryRow(ctx, "SELECT count(*) FROM grant_history WHERE tenant_id=$1 AND resource_id=$2", tenant, node).Scan(&count); e != nil || count != 5 {
		t.Fatal("history absent", e, count)
	}
	if _, e := pool.Exec(ctx, "DELETE FROM grant_history WHERE tenant_id=$1", tenant); e == nil {
		t.Fatal("history mutable")
	}
}

func failAudit(t *testing.T, pool *pgxpool.Pool, request string) {
	t.Helper()
	ctx := context.Background()
	name := "audit_fault_" + uuid.New().String()[:8]
	_, e := pool.Exec(ctx, "CREATE FUNCTION "+name+"() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.request_id='"+request+"' THEN RAISE EXCEPTION 'injected audit fault'; END IF; RETURN NEW; END $$; CREATE TRIGGER "+name+" BEFORE INSERT ON audit_events FOR EACH ROW EXECUTE FUNCTION "+name+"()")
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() {
		_, e := pool.Exec(ctx, "DROP TRIGGER "+name+" ON audit_events; DROP FUNCTION "+name+"()")
		if e != nil {
			t.Error(e)
		}
	})
}
func TestNodeLifecycleDrainDeleteAndAuditRollback(t *testing.T) {
	pool := testutil.Postgres(t)
	ctx := context.Background()
	tenant := seedTenant(t, pool)
	scope := policyScope(t, tenant, auth.Owner, policy.Mutate)
	s := service.NodeService{Pool: pool}
	node := mustNode(t, s, scope, nodeInput())
	request := uuid.NewString()
	failAudit(t, pool, request)
	tables := []string{"operations", "tasks", "outbox", "audit_events"}
	before := authoritySnapshot(t, pool, tenant, tables)
	if e := s.Transition(ctx, scope, node, "online", 1, 0, request); e == nil {
		t.Fatal("audit fault committed")
	}
	if string(before) != string(authoritySnapshot(t, pool, tenant, tables)) {
		t.Fatal("audit/task/outbox partial commit")
	}
	n, e := (repository.NodeRepository{Pool: pool}).Node(ctx, scope, node, uuid.NewString())
	if e != nil || n.Lifecycle != "pending" || n.Version != 1 {
		t.Fatal("node change escaped rollback", e)
	}
	if e = s.Transition(ctx, scope, node, "online", 1, 0, uuid.NewString()); e != nil {
		t.Fatal(e)
	}
	if e = s.Transition(ctx, scope, node, "disabled", 2, 0, uuid.NewString()); !errors.Is(e, service.ErrConflict) {
		t.Fatal("online skipped drain", e)
	}
	if e = s.Transition(ctx, scope, node, "draining", 2, 0, uuid.NewString()); e != nil {
		t.Fatal(e)
	}
	n, e = (repository.NodeRepository{Pool: pool}).Node(ctx, scope, node, uuid.NewString())
	if e != nil || time.Until(n.DrainDeadline.Time) < 295*time.Second {
		t.Fatal("default drain deadline", e)
	}
	if _, e = pool.Exec(ctx, "INSERT INTO node_dependencies VALUES($1,$2,$3,'session',2)", tenant, node, uuid.NewString()); e != nil {
		t.Fatal(e)
	}
	if e = s.Transition(ctx, scope, node, "disabled", 3, 0, uuid.NewString()); !errors.Is(e, service.ErrConflict) {
		t.Fatal("live sessions disabled", e)
	}
	if _, e = pool.Exec(ctx, "DELETE FROM node_dependencies WHERE node_id=$1", node); e != nil {
		t.Fatal(e)
	}
	if e = s.Transition(ctx, scope, node, "disabled", 3, 0, uuid.NewString()); e != nil {
		t.Fatal(e)
	}
	if _, e = pool.Exec(ctx, "INSERT INTO node_dependencies VALUES($1,$2,$3,'settlement',1)", tenant, node, uuid.NewString()); e != nil {
		t.Fatal(e)
	}
	if e = s.DeleteNode(ctx, scope, node, 4, uuid.NewString()); !errors.Is(e, service.ErrConflict) {
		t.Fatal("unsettled delete", e)
	}
	if _, e = pool.Exec(ctx, "DELETE FROM node_dependencies WHERE node_id=$1", node); e != nil {
		t.Fatal(e)
	}
	if e = s.DeleteNode(ctx, scope, node, 4, uuid.NewString()); e != nil {
		t.Fatal(e)
	}
	if _, e = (repository.NodeRepository{Pool: pool}).Node(ctx, scope, node, uuid.NewString()); !errors.Is(e, pgx.ErrNoRows) {
		t.Fatal("soft deleted visible", e)
	}
}

func TestNodeSegmentPermitIndependentOfGrants(t *testing.T) {
	pool := testutil.Postgres(t)
	ctx := context.Background()
	tenant := seedTenant(t, pool)
	scope := policyScope(t, tenant, auth.Owner, policy.Mutate)
	admin := policyScope(t, tenant, auth.PlatformAdmin, policy.Mutate)
	grant := policyScope(t, tenant, auth.PlatformAdmin, policy.Grant)
	s := service.NodeService{Pool: pool}
	a := mustGroup(t, s, scope, groupInput())
	b := mustGroup(t, s, scope, groupInput())
	if e := s.CheckSegment(ctx, scope, a, b, uuid.NewString()); e != nil {
		t.Fatal("self hosted default denied", e)
	}
	in := groupInput()
	in.OwnerKind = "platform"
	in.BillingMultiplierMilli = 1000
	c := mustGroup(t, s, admin, in)
	now := time.Now()
	if e := s.SetGrant(ctx, grant, c, 0, "active", now.Add(-time.Hour), now.Add(time.Hour), uuid.NewString()); e != nil {
		t.Fatal(e)
	}
	if e := s.CheckSegment(ctx, scope, a, c, uuid.NewString()); !errors.Is(e, service.ErrScope) {
		t.Fatal("grant implied segment", e)
	}
	if e := s.SetPermit(ctx, grant, a, c, 0, true, uuid.NewString()); e != nil {
		t.Fatal(e)
	}
	if e := s.CheckSegment(ctx, scope, a, c, uuid.NewString()); e != nil {
		t.Fatal(e)
	}
	if e := s.SetPermit(ctx, grant, a, c, 1, false, uuid.NewString()); e != nil {
		t.Fatal(e)
	}
	if e := s.CheckSegment(ctx, scope, a, c, uuid.NewString()); !errors.Is(e, service.ErrScope) {
		t.Fatal("revoked permit", e)
	}
}
