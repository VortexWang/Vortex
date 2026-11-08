package service_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"sync"
	"testing"
	"time"
	"vortex/backend/db/generated"
	"vortex/backend/internal/auth"
	"vortex/backend/internal/policy"
	"vortex/backend/internal/service"
	"vortex/backend/internal/testutil"
	"vortex/backend/internal/worker"
	controlv1 "vortex/contracts/protobuf/v1"
)

func TestUsageBatchIdempotencyAuthoritySchema(t *testing.T) {
	p := testutil.Postgres(t)
	var found bool
	if e := p.QueryRow(context.Background(), "SELECT to_regclass('usage_batches') IS NOT NULL").Scan(&found); e != nil {
		t.Fatal(e)
	}
	if !found {
		t.Fatal("immutable usage tuple/digest authority missing")
	}
}

func TestUnknownRiskBudgetAuthoritySchema(t *testing.T) {
	p := testutil.Postgres(t)
	var exists bool
	if e := p.QueryRow(context.Background(), "SELECT to_regclass('risk_budget_funding') IS NOT NULL").Scan(&exists); e != nil {
		t.Fatal(e)
	}
	if !exists {
		t.Fatal("approved funded risk budget authority missing; 72h cannot release unknown")
	}
}

func reserveUsage(t *testing.T, f *portFixture, source, snapshot string, n int64) generated.Reservation {
	t.Helper()
	now := time.Now().UTC().Truncate(time.Microsecond)
	r, e := (service.MeteringService{Pool: f.pool}).ReserveUsage(context.Background(), f.scope, service.ReserveInput{SourceID: source, SnapshotID: snapshot, NodeID: f.nodes[0], BootID: uuid.NewString(), Nonce: uuid.NewString(), RawBytes: n, Earliest: now, Latest: now, Deadline: now.Add(time.Hour)}, uuid.NewString())
	if e != nil {
		t.Fatal(e)
	}
	return r
}
func usageInput(r generated.Reservation, seq, n int64) service.UsageInput {
	payload := []byte(r.ID + "/" + uuid.NewString())
	d := sha256.Sum256(payload)
	return service.UsageInput{NodeID: r.NodeID, BootID: r.BootID, BatchID: uuid.NewString(), SnapshotID: r.BillingSnapshotID, Sequence: seq, Payload: payload, Digest: d[:], Facts: []service.UsageFact{{GrantID: r.ID, RawCumulative: n, Role: "execution"}}}
}

func TestUsageIdempotentTupleDigestDisputeAndFinal(t *testing.T) {
	f, source, snapshot := ledgerFixture(t)
	ctx := context.Background()
	s := service.MeteringService{Pool: f.pool}
	r := reserveUsage(t, f, source, snapshot, 1000)
	in := usageInput(r, 1, 100)
	ack, e := s.IngestUsageBatch(ctx, f.scope, in, uuid.NewString())
	if e != nil {
		t.Fatal(e)
	}
	for i := 0; i < 3; i++ {
		got, e := s.IngestUsageBatch(ctx, f.scope, in, uuid.NewString())
		if e != nil || !bytes.Equal(got.Digest, ack.Digest) {
			t.Fatal("non-idempotent usage", e)
		}
	}
	before, e := generated.New(f.pool).GetSourceProjection(ctx, generated.GetSourceProjectionParams{TenantID: f.tenant, SourceID: source})
	if e != nil {
		t.Fatal(e)
	}
	conflict := in
	conflict.Payload = []byte("digest conflict")
	d := sha256.Sum256(conflict.Payload)
	conflict.Digest = d[:]
	if _, e = s.IngestUsageBatch(ctx, f.scope, conflict, uuid.NewString()); !errors.Is(e, service.ErrUsageDisputed) {
		t.Fatal("no durable dispute", e)
	}
	after, e := generated.New(f.pool).GetSourceProjection(ctx, generated.GetSourceProjectionParams{TenantID: f.tenant, SourceID: source})
	if e != nil || before.B != after.B || before.F != after.F {
		t.Fatal("conflict released reservation", e)
	}
	var count int
	if e = f.pool.QueryRow(ctx, "SELECT count(*) FROM disputes WHERE tenant_id=$1 AND grant_id=$2", f.tenant, r.ID).Scan(&count); e != nil || count != 1 {
		t.Fatal(e, count)
	}
	proof := sha256.Sum256([]byte("authenticated final exact ledger"))
	if e = s.RecordFinalReceipt(ctx, f.scope, r.ID, snapshot, "final", 100, proof[:], uuid.NewString()); e != nil {
		t.Fatal(e)
	}
	if e = s.RecordFinalReceipt(ctx, f.scope, r.ID, snapshot, "final", 100, proof[:], uuid.NewString()); e != nil {
		t.Fatal("final replay", e)
	}
	after, e = generated.New(f.pool).GetSourceProjection(ctx, generated.GetSourceProjectionParams{TenantID: f.tenant, SourceID: source})
	if e != nil || after.F != 0 || after.B != 9900 || after.Charged != 100 {
		t.Fatal(e, after)
	}
	if e = s.CommitActualUsage(ctx, f.scope, r.ID, snapshot, "after-final", 101, proof[:], uuid.NewString()); e == nil {
		t.Fatal("terminal grant reused")
	}
}

func TestReserveConcurrentNonceDualVersionAndReturnUnused(t *testing.T) {
	f, source, snapshot := ledgerFixture(t)
	ctx := context.Background()
	s := service.MeteringService{Pool: f.pool}
	now := time.Now().UTC().Truncate(time.Microsecond)
	input := service.ReserveInput{SourceID: source, SnapshotID: snapshot, NodeID: f.nodes[0], BootID: uuid.NewString(), Nonce: uuid.NewString(), RawBytes: 1000, Earliest: now, Latest: now, Deadline: now.Add(time.Hour)}
	var wg sync.WaitGroup
	rows := make(chan generated.Reservation, 2)
	errs := make(chan error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r, e := s.ReserveUsage(ctx, f.scope, input, uuid.NewString())
			rows <- r
			errs <- e
		}()
	}
	wg.Wait()
	close(rows)
	close(errs)
	for e := range errs {
		if e != nil {
			t.Fatal(e)
		}
	}
	var r generated.Reservation
	for row := range rows {
		if r.ID != "" && r.ID != row.ID {
			t.Fatal("nonce double reserve")
		}
		r = row
	}
	explanation, e := (service.LedgerService{Pool: f.pool}).BillingExplanation(ctx, f.scope, snapshot, uuid.NewString())
	if e != nil {
		t.Fatal(e)
	}
	groups := explanation.Groups
	groups[0].Version++
	groups[0].Multiplier++
	next, e := (service.LedgerService{Pool: f.pool}).ScheduleRateVersion(ctx, policyScope(t, f.tenant, auth.PlatformAdmin, policy.Mutate), snapshot, groups, now.Add(25*time.Hour), now.Add(48*time.Hour), now, now, "issuer synchronization evidence", uuid.NewString())
	if e != nil {
		t.Fatal(e)
	}
	input.SnapshotID = next
	input.Preauthorize = true
	input.Deadline = now.Add(26 * time.Hour)
	if _, e := s.ReserveUsage(ctx, f.scope, input, uuid.NewString()); e == nil {
		t.Fatal("same nonce reserves across versions")
	}
	// A distinct future hold is valid only with explicit preauthorization; the
	// uncertainty interval straddling the activation boundary grants nothing.
	input.Nonce = uuid.NewString()
	input.Preauthorize = false
	if _, e := s.ReserveUsage(ctx, f.scope, input, uuid.NewString()); e == nil {
		t.Fatal("future version without preauthorization")
	}
	input.Preauthorize = true
	future, e := s.ReserveUsage(ctx, f.scope, input, uuid.NewString())
	if e != nil {
		t.Fatal("future preauthorization", e)
	}
	input.Nonce = uuid.NewString()
	input.Earliest, input.Latest = now.Add(25*time.Hour-time.Second), now.Add(25*time.Hour+time.Second)
	if _, e := s.ReserveUsage(ctx, f.scope, input, uuid.NewString()); e == nil {
		t.Fatal("authority granted in protection band")
	}
	futureProof := sha256.Sum256([]byte("cancel unused future preauthorization"))
	if e := s.RecordFinalReceipt(ctx, f.scope, future.ID, next, "unused-future-final", 0, futureProof[:], uuid.NewString()); e != nil {
		t.Fatal(e)
	}
	proof := sha256.Sum256([]byte("durable unused proof"))
	for i := 0; i < 2; i++ {
		if e := s.ReturnUnused(ctx, f.scope, r.ID, snapshot, "return", 0, 400, proof[:], uuid.NewString()); e != nil {
			t.Fatal(e)
		}
	}
	if e := s.CommitActualUsage(ctx, f.scope, r.ID, snapshot, "actual", 601, proof[:], uuid.NewString()); e == nil {
		t.Fatal("returned bytes reused")
	}
	if e := s.RecordFinalReceipt(ctx, f.scope, r.ID, snapshot, "final", 500, proof[:], uuid.NewString()); e != nil {
		t.Fatal(e)
	}
	p, e := generated.New(f.pool).GetSourceProjection(ctx, generated.GetSourceProjectionParams{TenantID: f.tenant, SourceID: source})
	if e != nil || p.F != 0 || p.B != 9500 {
		t.Fatal(e, p)
	}
}

func TestUsageAuditRollback(t *testing.T) {
	f, source, snapshot := ledgerFixture(t)
	ctx := context.Background()
	s := service.MeteringService{Pool: f.pool}
	r := reserveUsage(t, f, source, snapshot, 1000)
	in := usageInput(r, 1, 100)
	request := uuid.NewString()
	failAudit(t, f.pool, request)
	tables := []string{"usage_batches", "settlements", "ledger_events", "ledger_projections", "audit_events"}
	before := authoritySnapshot(t, f.pool, f.tenant, tables)
	if _, e := s.IngestUsageBatch(ctx, f.scope, in, request); e == nil {
		t.Fatal("audit bypass")
	}
	if !bytes.Equal(before, authoritySnapshot(t, f.pool, f.tenant, tables)) {
		t.Fatal("usage survives failed audit")
	}
	if _, e := s.IngestUsageBatch(ctx, f.scope, in, uuid.NewString()); e != nil {
		t.Fatal(e)
	}
}

func TestUnknownRiskBudget72HourFreezeLateExactAndAuditRollback(t *testing.T) {
	f, source, snapshot := ledgerFixture(t)
	ctx := context.Background()
	s := service.MeteringService{Pool: f.pool}
	r := reserveUsage(t, f, source, snapshot, 1000)
	q := generated.New(f.pool)
	proof := sha256.Sum256([]byte("persisted final proof"))
	at := time.Now().UTC().Truncate(time.Microsecond)
	if e := s.CommitActualUsage(ctx, f.scope, r.ID, snapshot, "known", 100, proof[:], uuid.NewString()); e != nil {
		t.Fatal(e)
	}
	if e := s.RecordUnknown(ctx, f.scope, r.ID, proof[:], at, uuid.NewString()); e != nil {
		t.Fatal(e)
	}
	u, e := q.GetUnknownLiability(ctx, generated.GetUnknownLiabilityParams{TenantID: f.tenant, GrantID: r.ID})
	if e != nil || u.ConfirmedRaw != 100 || u.UnknownRaw != 900 || u.UnknownBill != 900 {
		t.Fatal(e, u)
	}
	for _, hours := range []int{24, 72, 73} {
		if e = s.ReconcilePending(ctx, f.tenant, r.ID, at.Add(time.Duration(hours)*time.Hour)); e != nil {
			t.Fatal(e)
		}
	}
	p, e := q.GetSourceProjection(ctx, generated.GetSourceProjectionParams{TenantID: f.tenant, SourceID: source})
	if e != nil || p.F != 900 || p.B != 9900 {
		t.Fatal("timeout released unknown", e, p)
	}
	admin := policyScope(t, f.tenant, auth.PlatformAdmin, policy.Grant)
	if e = s.ConservativeClose(ctx, admin, r.ID, uuid.NewString(), at.Add(73*time.Hour), uuid.NewString()); e == nil {
		t.Fatal("unfunded close")
	}
	budget := uuid.NewString()
	if e = s.FundRiskBudget(ctx, admin, service.RiskFunding{ID: budget, BackingReference: uuid.NewString(), Approval: "explicit platform treasury allocation", Amount: 899, ValidUntil: at.Add(100 * time.Hour)}, uuid.NewString()); e != nil {
		t.Fatal(e)
	}
	if e = s.ConservativeClose(ctx, admin, r.ID, budget, at.Add(73*time.Hour), uuid.NewString()); e == nil {
		t.Fatal("insufficient budget closed debt")
	}
	budget = uuid.NewString()
	if e = s.FundRiskBudget(ctx, admin, service.RiskFunding{ID: budget, BackingReference: uuid.NewString(), Approval: "approved funded treasury allocation", Amount: 900, ValidUntil: at.Add(100 * time.Hour)}, uuid.NewString()); e != nil {
		t.Fatal(e)
	}
	if e = s.ConservativeClose(ctx, admin, r.ID, budget, at.Add(24*time.Hour), uuid.NewString()); e == nil {
		t.Fatal("early closure")
	}
	request := uuid.NewString()
	failAudit(t, f.pool, request)
	tables := []string{"traffic_sources", "source_ledgers", "ledger_events", "ledger_projections", "settlements", "risk_budget_debits", "audit_events"}
	before := authoritySnapshot(t, f.pool, f.tenant, tables)
	if e = s.ConservativeClose(ctx, admin, r.ID, budget, at.Add(73*time.Hour), request); e == nil {
		t.Fatal("closure audit bypass")
	}
	if !bytes.Equal(before, authoritySnapshot(t, f.pool, f.tenant, tables)) {
		t.Fatal("partial risk compensation")
	}
	if e = s.ConservativeClose(ctx, admin, r.ID, budget, at.Add(73*time.Hour), uuid.NewString()); e != nil {
		t.Fatal(e)
	}
	debit, e := q.GrantRiskDebit(ctx, generated.GrantRiskDebitParams{TenantID: f.tenant, GrantID: r.ID})
	if e != nil || debit.Amount != 900 {
		t.Fatal(e, debit)
	}
	original, e := q.GetSourceProjection(ctx, generated.GetSourceProjectionParams{TenantID: f.tenant, SourceID: source})
	if e != nil || original.B != 9000 || original.F != 0 {
		t.Fatal(e, original)
	}
	comp, e := q.GetSourceProjection(ctx, generated.GetSourceProjectionParams{TenantID: f.tenant, SourceID: debit.CompensationSourceID})
	if e != nil || comp.B != 900 || comp.F != 0 {
		t.Fatal(e, comp)
	}
	lateRequest := uuid.NewString()
	failAudit(t, f.pool, lateRequest)
	lateTables := []string{"ledger_events", "ledger_projections", "settlements", "risk_budget_recoveries", "audit_events"}
	lateBefore := authoritySnapshot(t, f.pool, f.tenant, lateTables)
	if e = s.ReconcileExact(ctx, f.scope, r.ID, 250, proof[:], lateRequest); e == nil {
		t.Fatal("late exact bypassed audit")
	}
	if !bytes.Equal(lateBefore, authoritySnapshot(t, f.pool, f.tenant, lateTables)) {
		t.Fatal("late exact survived failed audit")
	}
	for i := 0; i < 2; i++ {
		if e = s.ReconcileExact(ctx, f.scope, r.ID, 250, proof[:], uuid.NewString()); e != nil {
			t.Fatal("late exact replay", e)
		}
	}
	after, e := q.GetSourceProjection(ctx, generated.GetSourceProjectionParams{TenantID: f.tenant, SourceID: source})
	if e != nil || after.B != original.B || after.F != 0 || after.Charged != original.Charged {
		t.Fatal("late exact duplicated user debit/credit", e, after)
	}
	var recovered int64
	if e = f.pool.QueryRow(ctx, "SELECT amount FROM risk_budget_recoveries WHERE tenant_id=$1 AND grant_id=$2", f.tenant, r.ID).Scan(&recovered); e != nil || recovered != 750 {
		t.Fatal(e, recovered)
	}
	if e = s.ReconcilePending(ctx, f.tenant, r.ID, at.Add(74*time.Hour)); e != nil {
		t.Fatal(e)
	}
}

func TestUnknownExactFinalReconciliationAndProtectionBand(t *testing.T) {
	f, source, snapshot := ledgerFixture(t)
	ctx := context.Background()
	s := service.MeteringService{Pool: f.pool}
	r := reserveUsage(t, f, source, snapshot, 1000)
	proof := sha256.Sum256([]byte("exact receipt"))
	at := time.Now().UTC()
	request := uuid.NewString()
	failAudit(t, f.pool, request)
	tables := []string{"unknown_liabilities", "usage_reconciliation_jobs", "operations", "tasks", "outbox", "ledger_events", "ledger_projections", "audit_events"}
	before := authoritySnapshot(t, f.pool, f.tenant, tables)
	if e := s.RecordUnknown(ctx, f.scope, r.ID, proof[:], at, request); e == nil {
		t.Fatal("unknown bypassed audit")
	}
	if !bytes.Equal(before, authoritySnapshot(t, f.pool, f.tenant, tables)) {
		t.Fatal("unknown/task survived failed audit")
	}
	if e := s.RecordUnknown(ctx, f.scope, r.ID, proof[:], at, uuid.NewString()); e != nil {
		t.Fatal(e)
	}
	exactRequest := uuid.NewString()
	failAudit(t, f.pool, exactRequest)
	exactTables := []string{"settlements", "ledger_events", "ledger_projections", "audit_events"}
	exactBefore := authoritySnapshot(t, f.pool, f.tenant, exactTables)
	if e := s.ReconcileExact(ctx, f.scope, r.ID, 123, proof[:], exactRequest); e == nil {
		t.Fatal("exact closure bypassed audit")
	}
	if !bytes.Equal(exactBefore, authoritySnapshot(t, f.pool, f.tenant, exactTables)) {
		t.Fatal("exact closure survived failed audit")
	}
	for i := 0; i < 2; i++ {
		if e := s.ReconcileExact(ctx, f.scope, r.ID, 123, proof[:], uuid.NewString()); e != nil {
			t.Fatal(e)
		}
	}
	p, e := generated.New(f.pool).GetSourceProjection(ctx, generated.GetSourceProjectionParams{TenantID: f.tenant, SourceID: source})
	if e != nil || p.B != 9877 || p.F != 0 {
		t.Fatal(e, p)
	}
	T := at.Add(time.Hour)
	if got := service.ProtectionState(T.Add(-5*time.Second), T.Add(5*time.Second), T, T.Add(-6*time.Second), T.Add(time.Hour), true); got != "pause" {
		t.Fatal(got)
	}
	if got := service.ProtectionState(T.Add(-5*time.Second), T.Add(8*time.Second), T, T.Add(-6*time.Second), T.Add(time.Hour), true); got != "close" {
		t.Fatal(got)
	}
	if got := service.ProtectionState(T, T, T, T, T.Add(time.Hour), true); got != "new" {
		t.Fatal(got)
	}
	if got := service.ProtectionState(T.Add(-time.Second), T, T, T.Add(-time.Second), T.Add(time.Hour), false); got != "close" {
		t.Fatal(got)
	}
}

func TestUsageReconciliationWorkerExpiredReceiptFreeze(t *testing.T) {
	f, source, snapshot := ledgerFixture(t)
	ctx := context.Background()
	r := reserveUsage(t, f, source, snapshot, 1000)
	w := worker.UsageReconciliationWorker{Pool: f.pool, Now: func() time.Time { return r.ValidUntil.Time.Add(time.Hour) }}
	for i := 0; i < 256; i++ {
		if _, e := w.Once(ctx); e != nil {
			t.Fatal(e)
		}
		if _, e := generated.New(f.pool).GetUnknownLiability(ctx, generated.GetUnknownLiabilityParams{TenantID: f.tenant, GrantID: r.ID}); e == nil {
			break
		}
	}
	if _, e := generated.New(f.pool).GetUnknownLiability(ctx, generated.GetUnknownLiabilityParams{TenantID: f.tenant, GrantID: r.ID}); e != nil {
		t.Fatal("worker did not consume actual expired reservation", e)
	}
	p, e := generated.New(f.pool).GetSourceProjection(ctx, generated.GetSourceProjectionParams{TenantID: f.tenant, SourceID: source})
	if e != nil || p.F != 1000 {
		t.Fatal("worker timeout released authority", e, p)
	}
}

func TestSourceLocalUnknownDoesNotNetOrFreezeNewSource(t *testing.T) {
	f, source, snapshot := ledgerFixture(t)
	ctx := context.Background()
	s := service.MeteringService{Pool: f.pool}
	q := generated.New(f.pool)
	r := reserveUsage(t, f, source, snapshot, 900)
	at := time.Now().UTC().Truncate(time.Microsecond)
	proof := sha256.Sum256([]byte("old source evidence"))
	if e := s.RecordUnknown(ctx, f.scope, r.ID, proof[:], at, uuid.NewString()); e != nil {
		t.Fatal(e)
	}
	if e := q.ExpireSource(ctx, generated.ExpireSourceParams{TenantID: f.tenant, SourceID: source, EffectiveAt: timestampTest(at), Reason: "replacement"}); e != nil {
		t.Fatal(e)
	}
	l := service.LedgerService{Pool: f.pool}
	if e := l.ExpireSource(ctx, f.scope, source, uuid.NewString(), at); e != nil {
		t.Fatal(e)
	}
	old, e := q.GetSourceProjection(ctx, generated.GetSourceProjectionParams{TenantID: f.tenant, SourceID: source})
	if e != nil || old.B != 0 || old.F != 900 || old.L != 900 {
		t.Fatal(e, old)
	}
	b, e := q.GetBillingSnapshot(ctx, generated.GetBillingSnapshotParams{TenantID: f.tenant, ID: snapshot})
	if e != nil {
		t.Fatal(e)
	}
	fresh, e := q.AppendTrafficSource(ctx, generated.AppendTrafficSourceParams{ID: uuid.NewString(), TenantID: f.tenant, SnapshotID: b.PackageSnapshotID, Kind: "traffic", InitialAmount: 10, IssuedAt: timestampTest(at.Add(-time.Second)), ExpiresAt: timestampTest(at.Add(time.Hour)), PeriodKey: uuid.NewString()})
	if e != nil {
		t.Fatal(e)
	}
	if e = l.InitializeSource(ctx, f.scope, fresh.ID, uuid.NewString()); e != nil {
		t.Fatal(e)
	}
	reserveUsage(t, f, fresh.ID, snapshot, 10)
	p, e := q.GetSourceProjection(ctx, generated.GetSourceProjectionParams{TenantID: f.tenant, SourceID: fresh.ID})
	if e != nil || p.B != 10 || p.F != 10 || p.L != 0 {
		t.Fatal("old debt froze or netted new source", e, p)
	}
	if e = s.ReconcileExact(ctx, f.scope, r.ID, 100, proof[:], uuid.NewString()); e != nil {
		t.Fatal(e)
	}
	old, e = q.GetSourceProjection(ctx, generated.GetSourceProjectionParams{TenantID: f.tenant, SourceID: source})
	if e != nil || old.B != 0 || old.F != 0 || old.L != 0 {
		t.Fatal("old actual borrowed new source", e, old)
	}
}

func TestUsageSourceFixedPointRemainderAcrossGrants(t *testing.T) {
	f, source, snapshot := ledgerFixture(t)
	ctx := context.Background()
	q := generated.New(f.pool)
	b, e := q.GetBillingSnapshot(ctx, generated.GetBillingSnapshotParams{TenantID: f.tenant, ID: snapshot})
	if e != nil {
		t.Fatal(e)
	}
	vector, _ := json.Marshal([]service.BillingGroup{{Group: f.group, Version: 1, Multiplier: 1}})
	small, e := q.ScheduleBillingSnapshot(ctx, generated.ScheduleBillingSnapshotParams{ID: uuid.NewString(), TenantID: f.tenant, PackageSnapshotID: b.PackageSnapshotID, RuleID: b.RuleID, RuleRevision: b.RuleRevision, PathGeneration: b.PathGeneration, Version: 2, MultiplierMilli: 1, PathVersions: vector, ValidFrom: pgtype.Timestamptz{InfinityModifier: pgtype.NegativeInfinity, Valid: true}, ValidUntil: pgtype.Timestamptz{InfinityModifier: pgtype.Infinity, Valid: true}})
	if e != nil {
		t.Fatal(e)
	}
	proof := sha256.Sum256([]byte("fractional exact final"))
	s := service.MeteringService{Pool: f.pool}
	for i := 0; i < 20; i++ {
		r := reserveUsage(t, f, source, small.ID, 51)
		if r.ReservedBill != 1 {
			t.Fatal("safe ceiling lost", r)
		}
		if e = s.RecordFinalReceipt(ctx, f.scope, r.ID, small.ID, "final", 51, proof[:], uuid.NewString()); e != nil {
			t.Fatal(e)
		}
	}
	p, e := q.GetSourceProjection(ctx, generated.GetSourceProjectionParams{TenantID: f.tenant, SourceID: source})
	if e != nil || p.B != 9999 || p.Charged != 1 || p.F != 0 || p.RemainderMilli != 20 {
		t.Fatal("per-packet ceil overcharged user", e, p)
	}
}

func TestUsageRelayAndSelfHostedNoCharge(t *testing.T) {
	ctx := context.Background()
	for _, selfHosted := range []bool{false, true} {
		var f *portFixture
		var source, snapshot string
		if !selfHosted {
			f, source, snapshot = ledgerFixture(t)
		} else {
			f = newPortFixture(t)
			terms := packageTerms()
			terms.TrafficReset = "none"
			subscribePackage(t, f, createPackage(t, f, terms))
			in := ruleInput(f.group)
			reserveRulePort(t, f, in)
			if _, e := (service.RuleService{Pool: f.pool}).Mutate(ctx, f.scope, in, 0, uuid.NewString()); e != nil {
				t.Fatal(e)
			}
			snapshot = durableBilling(t, f, in.ID, 1, 1).ID
		}
		b, e := generated.New(f.pool).GetBillingSnapshot(ctx, generated.GetBillingSnapshotParams{TenantID: f.tenant, ID: snapshot})
		if e != nil {
			t.Fatal(e)
		}
		role := "relay"
		if selfHosted {
			role = "source"
		}
		batch := &controlv1.UsageBatch{BatchId: uuid.NewString(), BillingSnapshotId: snapshot, Deltas: []*controlv1.UsageDelta{{RuleId: b.RuleID, Role: role, Direction: "egress", Bytes: 999}}}
		s := service.MeteringService{Pool: f.pool}
		boot := uuid.NewString()
		for i := uint64(1); i < 3; i++ {
			if _, e = s.IngestWireUsage(ctx, f.nodes[0], boot, i, batch); e != nil {
				t.Fatal("observation charged or required execution grant", e)
			}
		}
		var reservations int
		if e = f.pool.QueryRow(ctx, "SELECT count(*) FROM reservations WHERE tenant_id=$1", f.tenant).Scan(&reservations); e != nil || reservations != 0 {
			t.Fatal("self-hosted/relay minted billing grants", e, reservations)
		}
		if !selfHosted {
			p, e := generated.New(f.pool).GetSourceProjection(ctx, generated.GetSourceProjectionParams{TenantID: f.tenant, SourceID: source})
			if e != nil || p.B != 10000 || p.F != 0 || p.Charged != 0 {
				t.Fatal(e, p)
			}
		}
	}
}
