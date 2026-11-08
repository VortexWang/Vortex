package service_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"github.com/google/uuid"
	"sync"
	"testing"
	"time"
	"vortex/backend/db/generated"
	"vortex/backend/internal/service"
	"vortex/backend/internal/testutil"
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

func TestUnknownRiskBudgetAuthoritySchema(t *testing.T){
 p:=testutil.Postgres(t);var exists bool
 if e:=p.QueryRow(context.Background(),"SELECT to_regclass('risk_budget_funding') IS NOT NULL").Scan(&exists);e!=nil{t.Fatal(e)}
 if !exists{t.Fatal("approved funded risk budget authority missing; 72h cannot release unknown")}
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
	input.SnapshotID = uuid.NewString()
	if _, e := s.ReserveUsage(ctx, f.scope, input, uuid.NewString()); e == nil {
		t.Fatal("same nonce reserves across versions")
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
