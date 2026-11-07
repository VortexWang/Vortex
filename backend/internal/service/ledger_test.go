package service_test

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"math/rand/v2"
	"os"
	"testing"
	"time"
	"vortex/backend/db/generated"
	"vortex/backend/internal/auth"
	"vortex/backend/internal/policy"
	"vortex/backend/internal/repository"
	"vortex/backend/internal/service"
	"vortex/backend/internal/testutil"
)

func text(s string) pgtype.Text { return pgtype.Text{String: s, Valid: true} }

func TestLedgerSourceAuthoritySchema(t *testing.T) {
	p := testutil.Postgres(t)
	for _, table := range []string{"source_ledgers", "ledger_events", "ledger_projections", "reservations", "settlements", "disputes", "unknown_liabilities"} {
		var found bool
		if e := p.QueryRow(context.Background(), "SELECT to_regclass($1) IS NOT NULL", table).Scan(&found); e != nil {
			t.Fatal(e)
		}
		if !found {
			t.Errorf("source-local immutable ledger table %s missing", table)
		}
	}
}

func ledgerFixture(t *testing.T) (*portFixture, string, string) {
	t.Helper()
	f := newPortFixtureKind(t, true)
	subscribePackage(t, f, createPackage(t, f, packageTerms()))
	in := ruleInput(f.group)
	reserveRulePort(t, f, in)
	if _, e := (service.RuleService{Pool: f.pool}).Mutate(context.Background(), f.scope, in, 0, uuid.NewString()); e != nil {
		t.Fatal(e)
	}
	b := durableBilling(t, f, in.ID, 1, 1)
	sources, e := generated.New(f.pool).ListTrafficSources(context.Background(), f.tenant)
	if e != nil || len(sources) != 1 {
		t.Fatal(e, sources)
	}
	source := sources[0].ID
	if e = (service.LedgerService{Pool: f.pool}).InitializeSource(context.Background(), f.scope, source, uuid.NewString()); e != nil {
		t.Fatal(e)
	}
	return f, source, b.ID
}

func TestLedgerConservationRebuildSourceIsolationAndAuditRollback(t *testing.T) {
	f, source, snapshot := ledgerFixture(t)
	ctx := context.Background()
	s := service.LedgerService{Pool: f.pool}
	p, e := (repository.LedgerRepository{Pool: f.pool}).Projection(ctx, f.scope, source, uuid.NewString())
	if e != nil || p.B != 10000 || p.F != 0 {
		t.Fatal(e, p)
	}
	if e = s.InitializeSource(ctx, f.scope, source, uuid.NewString()); e != nil {
		t.Fatal(e)
	}
	q := generated.New(f.pool)
	// Random bounded obligations, all in one source, not an aggregate model.
	for i := 0; i < 50; i++ {
		amount := int64(rand.IntN(100) + 1)
		_, e = q.AppendLedgerEvent(ctx, generated.AppendLedgerEventParams{TenantID: f.tenant, SourceID: source, EventKey: uuid.NewString(), Kind: "test_reserve", BillingSnapshotID: text(snapshot), DeltaF: amount, Evidence: []byte(`{"proof":"property"}`)})
		if e != nil {
			t.Fatal(e)
		}
	}
	live, e := q.GetSourceProjection(ctx, generated.GetSourceProjectionParams{TenantID: f.tenant, SourceID: source})
	if e != nil {
		t.Fatal(e)
	}
	rebuilt, e := s.Rebuild(ctx, f.scope, source, uuid.NewString())
	if e != nil || live.B != rebuilt.B || live.F != rebuilt.F || live.Sequence != rebuilt.Sequence || !bytes.Equal(live.Digest, rebuilt.Digest) {
		t.Fatal(e, live, rebuilt)
	}
	if _, e = q.AppendLedgerEvent(ctx, generated.AppendLedgerEventParams{TenantID: f.tenant, SourceID: source, EventKey: "overspend", Kind: "reserve", BillingSnapshotID: text(snapshot), DeltaF: 10001, Evidence: []byte(`{}`)}); e == nil {
		t.Fatal("source-local constraint allowed F>B+L")
	}
	for _, table := range []string{"source_ledgers", "ledger_events"} {
		if _, e = f.pool.Exec(ctx, "UPDATE "+table+" SET source_id=source_id WHERE tenant_id=$1", f.tenant); e == nil {
			t.Fatal("mutable", table)
		}
	}
	if _, e = (repository.LedgerRepository{Pool: f.pool}).Projection(ctx, policyScope(t, seedTenant(t, f.pool), auth.Owner, policy.Read), source, uuid.NewString()); e == nil {
		t.Fatal("source crossed tenant")
	}
	request := uuid.NewString()
	failAudit(t, f.pool, request)
	before := authoritySnapshot(t, f.pool, f.tenant, []string{"ledger_events", "ledger_projections", "audit_events"})
	if _, e = s.Rebuild(ctx, f.scope, source, request); e == nil {
		t.Fatal("audit rollback bypass")
	}
	if !bytes.Equal(before, authoritySnapshot(t, f.pool, f.tenant, []string{"ledger_events", "ledger_projections", "audit_events"})) {
		t.Fatal("partial ledger audit transaction")
	}
	// Valkey is explicitly disposable and cannot mint or erase source funds.
	address := os.Getenv("PHASE6_VALKEY_ADDR")
	if address == "" {
		t.Fatal("PHASE6_VALKEY_ADDR required")
	}
	read := policyScope(t, f.tenant, auth.Owner, policy.Read)
	raw, e := s.RebuildCache(ctx, read, address)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = service.CacheCommand(ctx, address, "FLUSHALL"); e != nil {
		t.Fatal(e)
	}
	again, e := s.RebuildCache(ctx, read, address)
	if e != nil || !bytes.Equal(raw, again) {
		t.Fatal("PG source rebuild after cache loss", e)
	}
}

func TestRateVersionSnapshotLeadAndBillingExplanation(t *testing.T) {
	f, _, snapshot := ledgerFixture(t)
	ctx := context.Background()
	s := service.LedgerService{Pool: f.pool}
	explanation, e := s.BillingExplanation(ctx, f.scope, snapshot, uuid.NewString())
	if e != nil || len(explanation.Groups) != 1 || explanation.Formula == "" {
		t.Fatal(e, explanation)
	}
	old := explanation.CoefficientMilli
	groups := explanation.Groups
	groups[0].Version++
	groups[0].Multiplier++
	now := time.Now().UTC().Truncate(time.Second)
	scope := policyScope(t, f.tenant, auth.PlatformAdmin, policy.Mutate)
	if _, e = s.ScheduleRateVersion(ctx, scope, snapshot, groups, now.Add(time.Hour), now.Add(48*time.Hour), now, now, "sync-proof", uuid.NewString()); e == nil {
		t.Fatal("less than 24 hour lead accepted")
	}
	next, e := s.ScheduleRateVersion(ctx, scope, snapshot, groups, now.Add(25*time.Hour), now.Add(48*time.Hour), now, now, "explicit synchronized issuer evidence", uuid.NewString())
	if e != nil {
		t.Fatal(e)
	}
	nextExplanation, e := s.BillingExplanation(ctx, f.scope, next, uuid.NewString())
	if e != nil || nextExplanation.CoefficientMilli != old+1 {
		t.Fatal(e, nextExplanation)
	}
	original, e := s.BillingExplanation(ctx, f.scope, snapshot, uuid.NewString())
	if e != nil || original.CoefficientMilli != old {
		t.Fatal("old snapshot recalculated", e)
	}
	raw, _ := json.Marshal(original)
	if !bytes.Contains(raw, []byte("path_coefficient_milli")) {
		t.Fatal("phase7 missing explanation API field")
	}
	if _, e = f.pool.Exec(ctx, "UPDATE billing_snapshots SET multiplier_milli=0 WHERE id=$1", snapshot); e == nil {
		t.Fatal("mutable snapshot")
	}
}
