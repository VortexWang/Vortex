package service_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"sync"
	"testing"
	"time"
	"vortex/backend/db/generated"
	"vortex/backend/internal/auth"
	"vortex/backend/internal/compiler"
	"vortex/backend/internal/policy"
	"vortex/backend/internal/repository"
	"vortex/backend/internal/service"
	"vortex/backend/internal/testutil"
	"vortex/backend/internal/worker"
)

func TestPackageSnapshotAuthoritySchema(t *testing.T) {
	p := testutil.Postgres(t)
	for _, name := range []string{"package_catalog", "package_versions", "package_snapshots", "subscriptions", "traffic_sources", "billing_snapshots"} {
		var found bool
		if e := p.QueryRow(context.Background(), "SELECT to_regclass($1) IS NOT NULL", name).Scan(&found); e != nil {
			t.Fatal(e)
		}
		if !found {
			t.Errorf("immutable package authority table %s missing", name)
		}
	}
}

func packageTerms() service.PackageTerms {
	return service.PackageTerms{DurationDays: 90, TrafficBytes: 10000, PriceMinor: 500, Capabilities: []string{"tcp", "udp", "direct", "tunnel", "multi_hop"}, RuleLimit: 100, RateLimit: 1000000, ConnectionLimit: 1000}
}
func createPackage(t *testing.T, f *portFixture, p service.PackageTerms) string {
	t.Helper()
	id := uuid.NewString()
	if _, e := (service.PackageService{Pool: f.pool}).PutCatalog(context.Background(), policyScope(t, f.tenant, auth.PlatformAdmin, policy.Mutate), id, "测试套餐", 0, p, uuid.NewString()); e != nil {
		t.Fatal(e)
	}
	return id
}
func subscribePackage(t *testing.T, f *portFixture, pkg string) service.PackageResult {
	t.Helper()
	r, e := (service.PackageService{Pool: f.pool}).Subscribe(context.Background(), policyScope(t, f.tenant, auth.PlatformAdmin, policy.Grant), pkg, 1, uuid.NewString(), uuid.NewString())
	if e != nil {
		t.Fatal(e)
	}
	return r
}
func durableBilling(t *testing.T, f *portFixture, rule string, revision, path int64) compiler.BillingSnapshot {
	t.Helper()
	q := generated.New(f.pool)
	if _, e := q.GetSubscription(context.Background(), f.tenant); errors.Is(e, pgx.ErrNoRows) {
		subscribePackage(t, f, createPackage(t, f, packageTerms()))
	} else if e != nil {
		t.Fatal(e)
	}
	b, e := (service.PackageService{Pool: f.pool}).ProduceBillingSnapshot(context.Background(), f.scope, rule, revision, path, uuid.NewString())
	if e != nil {
		t.Fatal(e)
	}
	return b
}
func TestPackageCatalogSnapshotAndSubscriptionImmutable(t *testing.T) {
	f := newPortFixture(t)
	ctx := context.Background()
	s := service.PackageService{Pool: f.pool}
	p := packageTerms()
	id := createPackage(t, f, p)
	first := subscribePackage(t, f, id)
	p.TrafficBytes = 999
	p.PriceMinor = 999
	p.RuleLimit = 1
	if _, e := s.PutCatalog(ctx, policyScope(t, f.tenant, auth.PlatformAdmin, policy.Mutate), id, "修改后", 1, p, uuid.NewString()); e != nil {
		t.Fatal(e)
	}
	sub, terms, e := s.Subscription(ctx, f.scope, uuid.NewString())
	if e != nil || sub.SnapshotID != first.SnapshotID || terms.TrafficBytes != 10000 || terms.PriceMinor != 500 || terms.RuleLimit != 100 {
		t.Fatal("catalog rewrote immutable subscription", e, sub, terms)
	}
	if _, e = s.Subscribe(ctx, policyScope(t, f.tenant, auth.PlatformAdmin, policy.Grant), id, 2, uuid.NewString(), uuid.NewString()); e == nil {
		t.Fatal("second primary accepted")
	}
	for _, table := range []string{"package_versions", "package_snapshots", "traffic_sources"} {
		if _, e = f.pool.Exec(ctx, "UPDATE "+table+" SET "+map[string]string{"package_versions": "terms=terms", "package_snapshots": "terms=terms", "traffic_sources": "initial_amount=initial_amount"}[table]); e == nil {
			t.Fatal("mutable", table)
		}
	}
	other := seedTenant(t, f.pool)
	if _, e = (repository.PackageRepository{Pool: f.pool}).Snapshot(ctx, policyScope(t, other, auth.Owner, policy.Read), first.SnapshotID, uuid.NewString()); !errors.Is(e, pgx.ErrNoRows) {
		t.Fatal("foreign snapshot exposed", e)
	}
}
func TestPackageMonthlyAnchorAndCapabilityIntersection(t *testing.T) {
	a := time.Date(2024, 1, 31, 17, 8, 9, 0, time.UTC)
	feb, mar := service.MonthlyPeriod(a, time.Date(2024, 2, 29, 18, 0, 0, 0, time.UTC))
	if feb.Day() != 29 || mar.Day() != 31 || mar.Month() != 3 || mar.Hour() != 17 {
		t.Fatal(feb, mar)
	}
	mar, apr := service.MonthlyPeriod(a, mar)
	if mar.Day() != 31 || apr.Day() != 30 {
		t.Fatal(mar, apr)
	}
	p := packageTerms()
	q := p
	q.Capabilities = []string{"tcp", "direct"}
	q.RuleLimit = 1
	q.RateLimit = 0
	r := service.EffectiveEntitlements(p, q)
	if len(r.Capabilities) != 2 || r.RuleLimit != 1 || r.RateLimit != 0 || r.ConnectionLimit != p.ConnectionLimit {
		t.Fatal(r)
	}
}
func TestPackageSubscriptionConcurrentPrimaryAndAuditRollback(t *testing.T) {
	f := newPortFixture(t)
	ctx := context.Background()
	s := service.PackageService{Pool: f.pool}
	id := createPackage(t, f, packageTerms())
	scope := policyScope(t, f.tenant, auth.PlatformAdmin, policy.Grant)
	request := uuid.NewString()
	failAudit(t, f.pool, request)
	tables := []string{"subscriptions", "package_snapshots", "traffic_sources", "package_requests", "audit_events", "tasks", "operations", "outbox"}
	before := authoritySnapshot(t, f.pool, f.tenant, tables)
	if _, e := s.Subscribe(ctx, scope, id, 1, uuid.NewString(), request); e == nil {
		t.Fatal("audit bypass")
	}
	if !bytes.Equal(before, authoritySnapshot(t, f.pool, f.tenant, tables)) {
		t.Fatal("partial package transaction")
	}
	results := make(chan error, 2)
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, e := s.Subscribe(ctx, scope, id, 1, uuid.NewString(), uuid.NewString())
			results <- e
		}()
	}
	wg.Wait()
	close(results)
	success := 0
	for e := range results {
		if e == nil {
			success++
		}
	}
	if success != 1 {
		t.Fatal("primary race", success)
	}
}
func TestPackageDurableSnapshotProductionCompileAndHistory(t *testing.T) {
	f := newPortFixture(t)
	ctx := context.Background()
	p := service.PackageService{Pool: f.pool}
	pkg := createPackage(t, f, packageTerms())
	sub := subscribePackage(t, f, pkg)
	r := ruleInput(f.group)
	reserveRulePort(t, f, r)
	m, e := (service.RuleService{Pool: f.pool}).Mutate(ctx, f.scope, r, 0, uuid.NewString())
	if e != nil {
		t.Fatal(e)
	}
	digest, e := p.BindPackageCompile(ctx, f.scope, m.OperationID, 2, 3, 4, uuid.NewString())
	if e != nil {
		t.Fatal(e)
	}
	pinned, e := generated.New(f.pool).GetRuleCompileInput(ctx, generated.GetRuleCompileInputParams{TenantID: f.tenant, RuleID: r.ID, RuleRevision: 1, InputDigest: digest})
	if e != nil {
		t.Fatal(e)
	}
	var in compiler.CompileInput
	if e = json.Unmarshal(pinned.Canonical, &in); e != nil {
		t.Fatal(e)
	}
	row, e := generated.New(f.pool).GetBillingSnapshot(ctx, generated.GetBillingSnapshotParams{TenantID: f.tenant, ID: in.Billing.ID})
	if e != nil || row.PackageSnapshotID != sub.SnapshotID || row.MultiplierMilli != 0 {
		t.Fatal("not durable billing binding", e, row)
	}
	if _, e = f.pool.Exec(ctx, "UPDATE billing_snapshots SET multiplier_milli=1000 WHERE id=$1", row.ID); e == nil {
		t.Fatal("snapshot coefficient mutable")
	}
	if worked, e := (worker.RuleCompileWorker{Pool: f.pool}).Once(ctx); e != nil || !worked {
		t.Fatal(worked, e)
	}
	var count int
	if e = f.pool.QueryRow(ctx, "SELECT count(*) FROM rule_artifacts WHERE tenant_id=$1 AND billing_snapshot_id=$2", f.tenant, row.ID).Scan(&count); e != nil || count != len(f.nodes) {
		t.Fatal(count, e)
	}
	forged := in.Billing
	forged.MultiplierMilli = 1000
	if _, e = (service.RuleService{Pool: f.pool}).BindCompileInput(ctx, f.scope, m.OperationID, forged, 2, 3, 4, uuid.NewString()); e == nil {
		t.Fatal("forged billing accepted")
	}
}

func TestEntitlementAtomicRedemptionContract(t *testing.T){
 f:=newPortFixture(t);pkg:=createPackage(t,f,packageTerms());s:=service.PackageService{Pool:f.pool}
 type issuer interface {CreateEntitlement(context.Context,service.TenantScope,string,string,string,int64,int32,int64,int64,time.Time,string,string)error;RedeemEntitlement(context.Context,service.TenantScope,string,string,string)(service.PackageResult,error)}
 api,ok:=any(s).(issuer);if !ok{t.Fatal("atomic entitlement issuance/redemption service missing")}
 code,key:=uuid.NewString(),uuid.NewString();ctx:=context.Background()
 if e:=api.CreateEntitlement(ctx,policyScope(t,f.tenant,auth.PlatformAdmin,policy.Grant),code,"package",pkg,1,90,10000,1,time.Now().Add(time.Hour),f.tenant,uuid.NewString());e!=nil{t.Fatal(e)}
 first,e:=api.RedeemEntitlement(ctx,f.scope,code,key,uuid.NewString());if e!=nil||first.SubscriptionID==""||first.SourceID==""{t.Fatal(first,e)}
 replay,e:=api.RedeemEntitlement(ctx,f.scope,code,key,uuid.NewString());if e!=nil||first!=replay{t.Fatal("redemption replay changed result",first,replay,e)}
 if _,e=api.RedeemEntitlement(ctx,f.scope,uuid.NewString(),key,uuid.NewString());e==nil{t.Fatal("payload conflict accepted")}
 var uses,redemptions,sources int;if e=f.pool.QueryRow(ctx,"SELECT remaining_uses,(SELECT count(*) FROM entitlement_redemptions WHERE tenant_id=$1),(SELECT count(*) FROM traffic_sources WHERE tenant_id=$1) FROM entitlement_codes WHERE applicable_tenant=$1",f.tenant).Scan(&uses,&redemptions,&sources);e!=nil||uses!=0||redemptions!=1||sources!=1{t.Fatal(uses,redemptions,sources,e)}
}
