package service_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"vortex/backend/db/generated"
	"vortex/backend/internal/service"
)

func failAuditUntilDropped(t *testing.T, pool *pgxpool.Pool, request string) func() {
	t.Helper()
	name := "lifecycle_fault_" + uuid.NewString()[:8]
	if _, e := pool.Exec(context.Background(), "CREATE FUNCTION "+name+"() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.request_id='"+request+"' THEN RAISE EXCEPTION 'lifecycle audit fault'; END IF; RETURN NEW; END $$; CREATE TRIGGER "+name+" BEFORE INSERT ON audit_events FOR EACH ROW EXECUTE FUNCTION "+name+"()"); e != nil { t.Fatal(e) }
	drop := func() {
		if _, e := pool.Exec(context.Background(), "DROP TRIGGER IF EXISTS "+name+" ON audit_events; DROP FUNCTION IF EXISTS "+name+"()"); e != nil { t.Error(e) }
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
		if e != nil { t.Fatal(e) }
		if !worked { return }
	}
	t.Fatal("lifecycle failed to reach an idempotent fixed point")
}

func TestPackageLifecycleMonthlyExpiryAndReplay(t *testing.T) {
	f := newPortFixture(t)
	ctx := context.Background()
	terms := packageTerms()
	terms.DurationDays = 365
	terms.TrafficReset = "monthly"
	sub := subscribePackage(t, f, createPackage(t, f, terms))
	_, edge := service.MonthlyPeriod(sub.AnchorAt, sub.AnchorAt)
	lifecycle(t, f, edge.Add(-time.Microsecond))
	q := generated.New(f.pool)
	sources, e := q.ListTrafficSources(ctx, f.tenant)
	if e != nil || len(sources) != 1 { t.Fatal("premature issue", sources, e) }
	// Downtime crosses two periods: issue only the current period, not missed
	// historical or future credits, and never mutate the old source.
	start, end := service.MonthlyPeriod(sub.AnchorAt, edge.AddDate(0, 2, 1))
	at := start.Add(time.Second)
	lifecycle(t, f, at)
	lifecycle(t, f, at)
	sources, e = q.ListTrafficSources(ctx, f.tenant)
	if e != nil || len(sources) != 2 { t.Fatal("period replay", sources, e) }
	var current string
	for _, source := range sources {
		if source.IssuedAt.Time.Equal(start) {
			current = source.ID
			if !source.ExpiresAt.Time.Equal(end) || source.InitialAmount != terms.TrafficBytes || source.SnapshotID != sub.SnapshotID { t.Fatal(source) }
		}
	}
	if current == "" { t.Fatal("current immutable period absent") }
	if expired, e := q.SourceIsExpired(ctx, generated.SourceIsExpiredParams{TenantID: f.tenant, SourceID: sub.SourceID, AtTime: pgtype.Timestamptz{Time: at, Valid: true}}); e != nil || !expired { t.Fatal("old source still usable", expired, e) }
	lifecycle(t, f, sub.ExpiresAt)
	lifecycle(t, f, sub.ExpiresAt)
	var events int
	if e = f.pool.QueryRow(ctx, "SELECT count(*) FROM restriction_events WHERE tenant_id=$1 AND cause='package_expired' AND event_kind='applied'", f.tenant).Scan(&events); e != nil || events != 1 { t.Fatal("expiry replay", events, e) }
	if e = f.pool.QueryRow(ctx, "SELECT count(*) FROM traffic_sources WHERE tenant_id=$1", f.tenant).Scan(&events); e != nil || events != 2 { t.Fatal("expired subscription issued rights", events, e) }
}

func TestPackageLifecycleAuditRollbackAndUnknownRetention(t *testing.T) {
	f := newPortFixture(t)
	ctx := context.Background()
	sub := subscribePackage(t, f, createPackage(t, f, packageTerms()))
	allocation := f.prepare(t, 20000)
	if _, e := f.pool.Exec(ctx, "UPDATE port_reservations SET state='unknown' WHERE tenant_id=$1 AND allocation_id=$2", f.tenant, allocation.ID); e != nil { t.Fatal(e) }
	// The deterministic worker request is injectable through the shared audit
	// trigger. No production code receives a test-only fault switch.
	request := "package-lifecycle/" + sub.SubscriptionID + "/expiry/1"
	drop := failAuditUntilDropped(t, f.pool, request)
	api, ok := any(service.PackageService{Pool: f.pool}).(interface { ProcessPackageLifecycle(context.Context, time.Time) (bool, error) })
	if !ok { t.Fatal("production lifecycle missing") }
	// Other tenants may precede this one in the shared queue.
	failed := false
	for n := 0; n < 2048; n++ {
		worked, e := api.ProcessPackageLifecycle(ctx, sub.ExpiresAt)
		if e != nil { failed = true; break }
		if !worked { break }
	}
	if !failed { t.Fatal("audit failure did not abort expiry") }
	var events int
	if e := f.pool.QueryRow(ctx, "SELECT count(*) FROM restriction_events WHERE tenant_id=$1 AND cause='package_expired'", f.tenant).Scan(&events); e != nil || events != 0 { t.Fatal("partial expiry", events, e) }
	drop()
	lifecycle(t, f, sub.ExpiresAt)
	if e := f.pool.QueryRow(ctx, "SELECT count(*) FROM port_reservations WHERE tenant_id=$1 AND allocation_id=$2 AND state='unknown'", f.tenant, allocation.ID).Scan(&events); e != nil || events != len(f.nodes)*len(allocation.Protocols) { t.Fatal("unknown released or relabelled", events, e) }
	// A delayed old clock cannot clear another independent restriction.
	if _, e := f.pool.Exec(ctx, "UPDATE subscriptions SET expires_at=$2,version=version+1 WHERE tenant_id=$1", f.tenant, sub.ExpiresAt.AddDate(0, 0, 30)); e != nil { t.Fatal(e) }
	manual := uuid.NewString()
	if _, e := f.pool.Exec(ctx, "WITH e AS (INSERT INTO restriction_events(id,tenant_id,cause,source_id,source_version,event_kind,effective_at) VALUES($1,$2,'manual',$1,1,'applied',now()) RETURNING *) INSERT INTO current_restrictions(tenant_id,cause,source_id,source_version,event_id,effective_at) SELECT tenant_id,cause,source_id,source_version,id,effective_at FROM e", manual, f.tenant); e != nil { t.Fatal(e) }
	lifecycle(t, f, sub.ExpiresAt.Add(time.Second))
	if e := f.pool.QueryRow(ctx, "SELECT count(*) FROM current_restrictions WHERE tenant_id=$1 AND cause='manual' AND cleared_at IS NULL", f.tenant).Scan(&events); e != nil || events != 1 { t.Fatal("independent restriction cleared", events, e) }
}
