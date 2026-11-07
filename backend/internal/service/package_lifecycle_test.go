package service_test

import (
	"context"
	"net/url"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"vortex/backend/db/generated"
	"vortex/backend/internal/service"
	"vortex/backend/internal/testutil"
)

func newPackageLifecycleFixture(t *testing.T) *portFixture {
	return newPackageLifecycleFixtureKind(t, false)
}
func newPackageLifecycleFixtureKind(t *testing.T, platform bool) *portFixture {
	t.Helper()
	// Advancing the trusted worker clock must not advance other tests' tenants.
	admin := testutil.Postgres(t)
	schema := "package_clock_" + uuid.NewString()[:8]
	if _, e := admin.Exec(context.Background(), "CREATE SCHEMA "+schema); e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() {
		if _, e := admin.Exec(context.Background(), "DROP SCHEMA "+schema+" CASCADE"); e != nil {
			t.Error(e)
		}
	})
	u, e := url.Parse(os.Getenv("PHASE6_DATABASE_URL"))
	if e != nil {
		t.Fatal(e)
	}
	query := u.Query()
	query.Set("search_path", schema)
	u.RawQuery = query.Encode()
	t.Setenv("PHASE6_DATABASE_URL", u.String())
	return newPortFixtureKind(t, platform)
}

func TestPackageLifecycleExpiryIndependentOfNodeAuthority(t *testing.T) {
	for _, fault := range []string{"offline", "revoked_grant"} {
		t.Run(fault, func(t *testing.T) {
			f := newPackageLifecycleFixtureKind(t, fault == "revoked_grant")
			ctx := context.Background()
			sub := subscribePackage(t, f, createPackage(t, f, packageTerms()))
			r := ruleInput(f.group)
			reserveRulePort(t, f, r)
			if _, e := (service.RuleService{Pool: f.pool}).Mutate(ctx, f.scope, r, 0, uuid.NewString()); e != nil {
				t.Fatal(e)
			}
			if _, e := f.pool.Exec(ctx, "UPDATE port_reservations SET state='unknown' WHERE tenant_id=$1", f.tenant); e != nil {
				t.Fatal(e)
			}
			other := newPortFixture(t)
			subscribePackage(t, other, createPackage(t, other, packageTerms()))
			at := sub.ExpiresAt.Add(time.Hour)
			if fault == "offline" {
				if _, e := f.pool.Exec(ctx, "UPDATE nodes SET lifecycle='offline' WHERE id=$1", f.nodes[0]); e != nil {
					t.Fatal(e)
				}
			} else {
				if _, e := f.pool.Exec(ctx, "UPDATE resource_grants SET status='revoked' WHERE tenant_id=$1 AND resource_id=$2", f.tenant, f.group); e != nil {
					t.Fatal(e)
				}
			}
			s := service.PackageService{Pool: f.pool}
			for n := 0; n < 8; n++ {
				worked, e := s.ProcessPackageLifecycle(ctx, at)
				if e != nil {
					t.Errorf("unreachable node or revoked authority blocked authoritative expiry: %v", e)
					break
				}
				if !worked {
					break
				}
			}
			for _, tenant := range []string{f.tenant, other.tenant} {
				var count int
				if e := f.pool.QueryRow(ctx, "SELECT count(*) FROM current_restrictions WHERE tenant_id=$1 AND cause='package_expired' AND cleared_at IS NULL", tenant).Scan(&count); e != nil || count != 1 {
					t.Errorf("expiry missing for tenant %s: %d %v", tenant, count, e)
				}
			}
			q := generated.New(f.pool)
			if expired, e := q.SourceIsExpired(ctx, generated.SourceIsExpiredParams{TenantID: f.tenant, SourceID: sub.SourceID, AtTime: timestampTest(at)}); e != nil || !expired {
				t.Errorf("source expiry absent: %v %v", expired, e)
			}
			ports, e := q.TenantPackagePorts(ctx, f.tenant)
			if e != nil || len(ports) != 1 || ports[0].State != "retained" || !ports[0].FirstEffectiveAt.Valid || !ports[0].FirstEffectiveAt.Time.Equal(sub.ExpiresAt) || !ports[0].RetainedUntil.Time.Equal(sub.ExpiresAt.AddDate(0,0,7)) {
				t.Errorf("conservative retention missing: %v %v", ports, e)
			}
			var unknown int
			if e := f.pool.QueryRow(ctx, "SELECT count(*) FROM port_reservations WHERE tenant_id=$1 AND state='unknown'", f.tenant).Scan(&unknown); e != nil || unknown != 2 {
				t.Errorf("unknown occupation changed: %d %v", unknown, e)
			}
			var enabled bool
			if e := f.pool.QueryRow(ctx, "SELECT (convert_from(r.payload,'UTF8')::jsonb->>'enabled')::boolean FROM rule_heads h JOIN rule_revisions r ON r.tenant_id=h.tenant_id AND r.rule_id=h.id AND r.revision=h.desired_revision WHERE h.tenant_id=$1 AND h.id=$2", f.tenant, r.ID).Scan(&enabled); e != nil || enabled {
				t.Errorf("durable stop desire absent: %v %v", enabled, e)
			}
		})
	}
}

func failAuditUntilDropped(t *testing.T, pool *pgxpool.Pool, request string) func() {
	t.Helper()
	name := "lifecycle_fault_" + uuid.NewString()[:8]
	if _, e := pool.Exec(context.Background(), "CREATE FUNCTION "+name+"() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.request_id='"+request+"' THEN RAISE EXCEPTION 'lifecycle audit fault'; END IF; RETURN NEW; END $$; CREATE TRIGGER "+name+" BEFORE INSERT ON audit_events FOR EACH ROW EXECUTE FUNCTION "+name+"()"); e != nil {
		t.Fatal(e)
	}
	drop := func() {
		if _, e := pool.Exec(context.Background(), "DROP TRIGGER IF EXISTS "+name+" ON audit_events; DROP FUNCTION IF EXISTS "+name+"()"); e != nil {
			t.Error(e)
		}
	}
	t.Cleanup(drop)
	return drop
}

func lifecycle(t *testing.T, f *portFixture, at time.Time) {
	t.Helper()
	api, ok := any(service.PackageService{Pool: f.pool}).(interface {
		ProcessPackageLifecycle(context.Context, time.Time) (bool, error)
	})
	if !ok {
		t.Fatal("production package expiry/monthly lifecycle missing")
	}
	for n := 0; n < 2048; n++ {
		worked, e := api.ProcessPackageLifecycle(context.Background(), at)
		if e != nil {
			t.Fatal(e)
		}
		if !worked {
			return
		}
	}
	t.Fatal("lifecycle failed to reach an idempotent fixed point")
}

func TestPackageLifecycleMonthlyExpiryAndReplay(t *testing.T) {
	f := newPackageLifecycleFixture(t)
	ctx := context.Background()
	terms := packageTerms()
	terms.DurationDays = 365
	terms.TrafficReset = "monthly"
	sub := subscribePackage(t, f, createPackage(t, f, terms))
	_, edge := service.MonthlyPeriod(sub.AnchorAt, sub.AnchorAt)
	lifecycle(t, f, edge.Add(-time.Microsecond))
	q := generated.New(f.pool)
	sources, e := q.ListTrafficSources(ctx, f.tenant)
	if e != nil || len(sources) != 1 {
		t.Fatal("premature issue", sources, e)
	}
	// Downtime crosses two periods: issue only the current period, not missed
	// historical or future credits, and never mutate the old source.
	start, end := service.MonthlyPeriod(sub.AnchorAt, edge.AddDate(0, 2, 1))
	at := start.Add(time.Second)
	lifecycle(t, f, at)
	lifecycle(t, f, at)
	sources, e = q.ListTrafficSources(ctx, f.tenant)
	if e != nil || len(sources) != 2 {
		t.Fatal("period replay", sources, e)
	}
	var current string
	for _, source := range sources {
		if source.IssuedAt.Time.Equal(start) {
			current = source.ID
			if !source.ExpiresAt.Time.Equal(end) || source.InitialAmount != terms.TrafficBytes || source.SnapshotID != sub.SnapshotID {
				t.Fatal(source)
			}
		}
	}
	if current == "" {
		t.Fatal("current immutable period absent")
	}
	if expired, e := q.SourceIsExpired(ctx, generated.SourceIsExpiredParams{TenantID: f.tenant, SourceID: sub.SourceID, AtTime: pgtype.Timestamptz{Time: at, Valid: true}}); e != nil || !expired {
		t.Fatal("old source still usable", expired, e)
	}
	lifecycle(t, f, sub.ExpiresAt)
	lifecycle(t, f, sub.ExpiresAt)
	var events int
	if e = f.pool.QueryRow(ctx, "SELECT count(*) FROM restriction_events WHERE tenant_id=$1 AND cause='package_expired' AND event_kind='applied'", f.tenant).Scan(&events); e != nil || events != 1 {
		t.Fatal("expiry replay", events, e)
	}
	if e = f.pool.QueryRow(ctx, "SELECT count(*) FROM traffic_sources WHERE tenant_id=$1", f.tenant).Scan(&events); e != nil || events != 2 {
		t.Fatal("expired subscription issued rights", events, e)
	}
}

func TestPackageLifecycleAuditRollbackAndUnknownRetention(t *testing.T) {
	f := newPackageLifecycleFixture(t)
	ctx := context.Background()
	sub := subscribePackage(t, f, createPackage(t, f, packageTerms()))
	r := ruleInput(f.group)
	reserveRulePort(t, f, r)
	allocations, e := generated.New(f.pool).RuleCompilePortAllocations(ctx, generated.RuleCompilePortAllocationsParams{TenantID: f.tenant, RuleID: r.ID, GroupID: f.group})
	if e != nil || len(allocations) != 1 {
		t.Fatal(allocations, e)
	}
	allocation := allocations[0]
	if _, e := (service.RuleService{Pool: f.pool}).Mutate(ctx, f.scope, r, 0, uuid.NewString()); e != nil {
		t.Fatal(e)
	}
	if _, e := f.pool.Exec(ctx, "UPDATE port_reservations SET state='unknown' WHERE tenant_id=$1 AND allocation_id=$2", f.tenant, allocation.ID); e != nil {
		t.Fatal(e)
	}
	// The deterministic worker request is injectable through the shared audit
	// trigger. No production code receives a test-only fault switch.
	request := "package-lifecycle/" + sub.SubscriptionID + "/expiry/1"
	drop := failAuditUntilDropped(t, f.pool, request)
	api, ok := any(service.PackageService{Pool: f.pool}).(interface {
		ProcessPackageLifecycle(context.Context, time.Time) (bool, error)
	})
	if !ok {
		t.Fatal("production lifecycle missing")
	}
	// Other tenants may precede this one in the shared queue.
	failed := false
	for n := 0; n < 2048; n++ {
		worked, e := api.ProcessPackageLifecycle(ctx, sub.ExpiresAt)
		if e != nil {
			failed = true
			break
		}
		if !worked {
			break
		}
	}
	if !failed {
		t.Fatal("audit failure did not abort expiry")
	}
	var events int
	if e := f.pool.QueryRow(ctx, "SELECT count(*) FROM restriction_events WHERE tenant_id=$1 AND cause='package_expired'", f.tenant).Scan(&events); e != nil || events != 0 {
		t.Fatal("partial expiry", events, e)
	}
	drop()
	lifecycle(t, f, sub.ExpiresAt)
	var enabled bool
	if e := f.pool.QueryRow(ctx, "SELECT (convert_from(r.payload,'UTF8')::jsonb->>'enabled')::boolean FROM rule_heads h JOIN rule_revisions r ON r.tenant_id=h.tenant_id AND r.rule_id=h.id AND r.revision=h.desired_revision WHERE h.tenant_id=$1 AND h.id=$2", f.tenant, r.ID).Scan(&enabled); e != nil || enabled {
		t.Fatal("expiry did not append stop revision", enabled, e)
	}
	if e := f.pool.QueryRow(ctx, "SELECT count(*) FROM port_reservations WHERE tenant_id=$1 AND allocation_id=$2 AND state='unknown'", f.tenant, allocation.ID).Scan(&events); e != nil || events != len(f.nodes)*len(allocation.Protocols) {
		t.Fatal("unknown released or relabelled", events, e)
	}
	// A delayed old clock cannot clear another independent restriction.
	if _, e := f.pool.Exec(ctx, "UPDATE subscriptions SET expires_at=$2,version=version+1 WHERE tenant_id=$1", f.tenant, sub.ExpiresAt.AddDate(0, 0, 30)); e != nil {
		t.Fatal(e)
	}
	manual := uuid.NewString()
	if _, e := f.pool.Exec(ctx, "WITH e AS (INSERT INTO restriction_events(id,tenant_id,cause,source_id,source_version,event_kind,effective_at) VALUES($1,$2,'manual',$1,1,'applied',now()) RETURNING *) INSERT INTO current_restrictions(tenant_id,cause,source_id,source_version,event_id,effective_at) SELECT tenant_id,cause,source_id,source_version,id,effective_at FROM e", manual, f.tenant); e != nil {
		t.Fatal(e)
	}
	lifecycle(t, f, sub.ExpiresAt.Add(time.Second))
	if e := f.pool.QueryRow(ctx, "SELECT count(*) FROM current_restrictions WHERE tenant_id=$1 AND cause='manual' AND cleared_at IS NULL", f.tenant).Scan(&events); e != nil || events != 1 {
		t.Fatal("independent restriction cleared", events, e)
	}
}
