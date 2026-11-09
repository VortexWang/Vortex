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
	var count int
	// The worker consumes a shared durable queue. Older ready tasks may belong
	// to other tenants or previous test runs; one tick is not our completion.
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if e = f.pool.QueryRow(ctx, "SELECT count(*) FROM rule_artifacts WHERE tenant_id=$1 AND billing_snapshot_id=$2", f.tenant, row.ID).Scan(&count); e != nil {
			t.Fatal(e)
		}
		if count == len(f.nodes) {
			break
		}
		if worked, e := (worker.RuleCompileWorker{Pool: f.pool}).Once(ctx); e != nil || !worked {
			t.Fatal("compile queue stopped before this snapshot completed", worked, e)
		}
	}
	if count != len(f.nodes) {
		t.Fatal(count, e)
	}
	forged := in.Billing
	forged.MultiplierMilli = 1000
	if _, e = (service.RuleService{Pool: f.pool}).BindCompileInput(ctx, f.scope, m.OperationID, forged, 2, 3, 4, uuid.NewString()); e == nil {
		t.Fatal("forged billing accepted")
	}
}

func TestEntitlementAtomicRedemptionContract(t *testing.T) {
	f := newPortFixture(t)
	pkg := createPackage(t, f, packageTerms())
	s := service.PackageService{Pool: f.pool}
	type issuer interface {
		CreateEntitlement(context.Context, service.TenantScope, string, string, string, int64, int32, int64, int64, time.Time, string, string) error
		RedeemEntitlement(context.Context, service.TenantScope, string, string, string) (service.PackageResult, error)
	}
	api, ok := any(s).(issuer)
	if !ok {
		t.Fatal("atomic entitlement issuance/redemption service missing")
	}
	code, key := uuid.NewString(), uuid.NewString()
	ctx := context.Background()
	if e := api.CreateEntitlement(ctx, policyScope(t, f.tenant, auth.PlatformAdmin, policy.Grant), code, "package", pkg, 1, 90, 10000, 1, time.Now().Add(time.Hour), f.tenant, uuid.NewString()); e != nil {
		t.Fatal(e)
	}
	first, e := api.RedeemEntitlement(ctx, f.scope, code, key, uuid.NewString())
	if e != nil || first.SubscriptionID == "" || first.SourceID == "" {
		t.Fatal(first, e)
	}
	replay, e := api.RedeemEntitlement(ctx, f.scope, code, key, uuid.NewString())
	if e != nil || first != replay {
		t.Fatal("redemption replay changed result", first, replay, e)
	}
	if _, e = api.RedeemEntitlement(ctx, f.scope, uuid.NewString(), key, uuid.NewString()); e == nil {
		t.Fatal("payload conflict accepted")
	}
	var uses, redemptions, sources int
	if e = f.pool.QueryRow(ctx, "SELECT remaining_uses,(SELECT count(*) FROM entitlement_redemptions WHERE tenant_id=$1),(SELECT count(*) FROM traffic_sources WHERE tenant_id=$1) FROM entitlement_codes WHERE applicable_tenant=$1", f.tenant).Scan(&uses, &redemptions, &sources); e != nil || uses != 0 || redemptions != 1 || sources != 1 {
		t.Fatal(uses, redemptions, sources, e)
	}
}

func issueCode(t *testing.T, f *portFixture, pkg, kind string, days int32, amount, uses int64) string {
	t.Helper()
	code := uuid.NewString()
	if e := (service.PackageService{Pool: f.pool}).CreateEntitlement(context.Background(), policyScope(t, f.tenant, auth.PlatformAdmin, policy.Grant), code, kind, pkg, 1, days, amount, uses, time.Now().Add(time.Hour), f.tenant, uuid.NewString()); e != nil {
		t.Fatal(e)
	}
	return code
}
func redeem(t *testing.T, f *portFixture, code string) service.PackageResult {
	t.Helper()
	r, e := (service.PackageService{Pool: f.pool}).RedeemEntitlement(context.Background(), f.scope, code, uuid.NewString(), uuid.NewString())
	if e != nil {
		t.Fatal(e)
	}
	return r
}
func TestEntitlementRedeemConcurrentUsesAndRequestConflict(t *testing.T) {
	f := newPortFixture(t)
	s := service.PackageService{Pool: f.pool}
	pkg := createPackage(t, f, packageTerms())
	code := issueCode(t, f, pkg, "traffic", 7, 123, 1)
	ctx := context.Background()
	key := uuid.NewString()
	out := make(chan service.PackageResult, 2)
	errch := make(chan error, 2)
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r, e := s.RedeemEntitlement(ctx, f.scope, code, key, uuid.NewString())
			out <- r
			errch <- e
		}()
	}
	wg.Wait()
	close(out)
	close(errch)
	for e := range errch {
		if e != nil {
			t.Fatal(e)
		}
	}
	var first service.PackageResult
	for r := range out {
		if first.SourceID != "" && r != first {
			t.Fatal("concurrent duplicate issuance")
		}
		first = r
	}
	other := seedTenant(t, f.pool)
	if _, e := s.RedeemEntitlement(ctx, policyScope(t, other, auth.Owner, policy.Mutate), code, key, uuid.NewString()); !errors.Is(e, service.ErrConflict) {
		t.Fatal("cross tenant request replay", e)
	}
	if _, e := s.RedeemEntitlement(ctx, f.scope, code, uuid.NewString(), uuid.NewString()); !errors.Is(e, service.ErrEntitlementInvalid) {
		t.Fatal("exhausted accepted", e)
	}
	code = issueCode(t, f, pkg, "traffic", 7, 123, 1)
	errch = make(chan error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, e := s.RedeemEntitlement(ctx, f.scope, code, uuid.NewString(), uuid.NewString())
			errch <- e
		}()
	}
	wg.Wait()
	close(errch)
	success := 0
	for e := range errch {
		if e == nil {
			success++
		}
	}
	if success != 1 {
		t.Fatal("code overspend", success)
	}
}
func TestEntitlementAuditRollbackExpiredRevokedApplicability(t *testing.T) {
	f := newPortFixture(t)
	ctx := context.Background()
	s := service.PackageService{Pool: f.pool}
	pkg := createPackage(t, f, packageTerms())
	code := issueCode(t, f, pkg, "package", 90, 10000, 2)
	tables := []string{"subscriptions", "package_snapshots", "traffic_sources", "source_extensions", "package_requests", "entitlement_redemptions", "audit_events", "tasks", "operations", "outbox"}
	request := uuid.NewString()
	failAudit(t, f.pool, request)
	before := authoritySnapshot(t, f.pool, f.tenant, tables)
	if _, e := s.RedeemEntitlement(ctx, f.scope, code, uuid.NewString(), request); e == nil {
		t.Fatal("audit bypass")
	}
	if !bytes.Equal(before, authoritySnapshot(t, f.pool, f.tenant, tables)) {
		t.Fatal("partial redeem")
	}
	var uses int
	if e := f.pool.QueryRow(ctx, "SELECT remaining_uses FROM entitlement_codes WHERE applicable_tenant=$1", f.tenant).Scan(&uses); e != nil || uses != 2 {
		t.Fatal("partial counter", uses, e)
	}
	other := seedTenant(t, f.pool)
	if _, e := s.RedeemEntitlement(ctx, policyScope(t, other, auth.Owner, policy.Mutate), code, uuid.NewString(), uuid.NewString()); !errors.Is(e, service.ErrEntitlementInvalid) {
		t.Fatal(e)
	}
	if e := s.RevokeEntitlement(ctx, policyScope(t, f.tenant, auth.PlatformAdmin, policy.Grant), code, uuid.NewString()); e != nil {
		t.Fatal(e)
	}
	if _, e := s.RedeemEntitlement(ctx, f.scope, code, uuid.NewString(), uuid.NewString()); !errors.Is(e, service.ErrEntitlementInvalid) {
		t.Fatal("revoked accepted", e)
	}
	code = issueCode(t, f, pkg, "traffic", 7, 123, 1)
	if _, e := f.pool.Exec(ctx, "UPDATE entitlement_codes SET valid_until=now()-interval '1 second' WHERE NOT revoked AND applicable_tenant=$1", f.tenant); e != nil {
		t.Fatal(e)
	}
	if _, e := s.RedeemEntitlement(ctx, f.scope, code, uuid.NewString(), uuid.NewString()); !errors.Is(e, service.ErrEntitlementInvalid) {
		t.Fatal("expired accepted", e)
	}
}
func TestEntitlementRenewalMonthlyNoneDurationAndIndependentSources(t *testing.T) {
	for _, reset := range []string{"monthly", "none"} {
		t.Run(reset, func(t *testing.T) {
			f := newPortFixture(t)
			ctx := context.Background()
			s := service.PackageService{Pool: f.pool}
			p := packageTerms()
			p.TrafficReset = reset
			pkg := createPackage(t, f, p)
			initial := subscribePackage(t, f, pkg)
			next := redeem(t, f, issueCode(t, f, pkg, "package", 30, 77, 1))
			if next.AnchorAt != initial.AnchorAt || !next.ExpiresAt.Equal(initial.ExpiresAt.AddDate(0, 0, 30)) {
				t.Fatal("anchor/expiry changed", initial, next)
			}
			rows, e := generated.New(f.pool).ListTrafficSources(ctx, f.tenant)
			if e != nil {
				t.Fatal(e)
			}
			want := 1
			if reset == "none" {
				want = 2
			}
			if len(rows) != want {
				t.Fatal("future monthly funds issued", len(rows))
			}
			duration := redeem(t, f, issueCode(t, f, pkg, "duration", 10, 999, 1))
			if duration.AnchorAt != initial.AnchorAt {
				t.Fatal("duration changed anchor")
			}
			rows, e = generated.New(f.pool).ListTrafficSources(ctx, f.tenant)
			if e != nil || len(rows) != want {
				t.Fatal("duration added funds", e, len(rows))
			}
			traffic1 := redeem(t, f, issueCode(t, f, pkg, "traffic", 9, 101, 1))
			traffic2 := redeem(t, f, issueCode(t, f, pkg, "traffic", 3, 202, 1))
			if traffic1.SourceID == traffic2.SourceID || traffic1.SnapshotID == traffic2.SnapshotID {
				t.Fatal("independent sources merged")
			}
			available, e := s.ConsumableSources(ctx, f.scope, time.Now())
			if e != nil || len(available) != want+2 || available[want].ID != traffic2.SourceID || available[want+1].ID != traffic1.SourceID {
				t.Fatal("source ordering", e, available)
			}
		})
	}
}
func TestEntitlementTrafficWithoutPrimaryAndExpiredNoResurrection(t *testing.T) {
	f := newPortFixture(t)
	ctx := context.Background()
	s := service.PackageService{Pool: f.pool}
	pkg := createPackage(t, f, packageTerms())
	traffic := redeem(t, f, issueCode(t, f, pkg, "traffic", 3, 500, 1))
	if traffic.SourceID == "" {
		t.Fatal("traffic issue missing")
	}
	if _, e := s.RedeemEntitlement(ctx, f.scope, issueCode(t, f, pkg, "duration", 3, 0, 1), uuid.NewString(), uuid.NewString()); !errors.Is(e, service.ErrEntitlementInvalid) {
		t.Fatal("duration without primary accepted", e)
	}
	initial := subscribePackage(t, f, pkg)
	old := initial.AnchorAt.AddDate(0, 0, -100)
	if _, e := f.pool.Exec(ctx, "UPDATE subscriptions SET anchor_at=$2,expires_at=now()-interval '1 second' WHERE tenant_id=$1", f.tenant, old); e != nil {
		t.Fatal(e)
	}
	if _, e := s.RedeemEntitlement(ctx, f.scope, issueCode(t, f, pkg, "duration", 3, 0, 1), uuid.NewString(), uuid.NewString()); !errors.Is(e, service.ErrEntitlementInvalid) {
		t.Fatal("expired duration accepted", e)
	}
	fresh := redeem(t, f, issueCode(t, f, pkg, "package", 90, 10000, 1))
	if fresh.SnapshotID == initial.SnapshotID || !fresh.AnchorAt.After(initial.AnchorAt) {
		t.Fatal("expired subscription resurrected")
	}
	other := createPackage(t, f, packageTerms())
	if _, e := s.RedeemEntitlement(ctx, f.scope, issueCode(t, f, other, "package", 90, 10000, 1), uuid.NewString(), uuid.NewString()); !errors.Is(e, service.ErrPackageConfirmationRequired) {
		t.Fatal("different package skipped confirmation", e)
	}
}
