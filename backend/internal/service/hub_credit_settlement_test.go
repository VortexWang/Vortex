package service_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"

	"github.com/google/uuid"
	"vortex/backend/db/generated"
	"vortex/backend/internal/service"
	"vortex/backend/internal/worker"
)

func acceptCreditFinal(t *testing.T, f *creditCloseFixture) {
	t.Helper()
	p, err := f.x.s.PrepareCreditClose(context.Background(), f.x.f.scope, f.grant, shareBackendID, uuid.NewString())
	if err != nil {
		t.Fatal(err)
	}
	r := f.localFinal(t, p)
	raw, _ := json.Marshal(r)
	if err = f.x.s.RecordCreditClose(f.x.f.contexts[f.in.NodeID], f.x.f.scope, raw, uuid.NewString()); err != nil {
		t.Fatal(err)
	}
}

func TestHubCreditFinalRequiresEveryChildAndAtomicOriginalSourceSettlement(t *testing.T) {
	f := newCreditCloseFixture(t)
	x := f.x
	ctx := context.Background()
	batch := f.commitLocalUsage(t, 37)
	issued2, grant2, seq2 := issueAllocationFixture(t, x, f.hs, f.a, 51)
	if err := x.s.RecordHubIssue(x.f.contexts[f.in.HubID], f.in.HubID, f.in.HubBootID, seq2, issued2.Payload, uuid.NewString()); err != nil {
		t.Fatal(err)
	}
	second := *f
	second.issued, second.grant = issued2, grant2
	if err := x.s.FinalizeHubAllocation(ctx, x.f.scope, f.a.AllocationID, uuid.NewString()); !errors.Is(err, service.ErrShareUnknown) {
		t.Fatal("unsealed parent finalized", err)
	}
	f.seal(t)
	if _, err := (service.MeteringService{Pool: x.f.pool}).IngestWireUsage(ctx, f.in.NodeID, f.in.BootID, 1, batch); err != nil {
		t.Fatal(err)
	}
	acceptCreditFinal(t, f)
	tables := []string{"settlements", "ledger_events", "ledger_projections", "tasks", "outbox", "operations", "audit_events"}
	before := authoritySnapshot(t, x.f.pool, x.f.tenant, tables)
	if err := x.s.FinalizeHubAllocation(ctx, x.f.scope, f.a.AllocationID, uuid.NewString()); !errors.Is(err, service.ErrShareUnknown) {
		t.Fatal("one missing child released parent", err)
	}
	if !bytes.Equal(before, authoritySnapshot(t, x.f.pool, x.f.tenant, tables)) {
		t.Fatal("incomplete closure mutated authority")
	}
	acceptCreditFinal(t, &second) // genuine never-installed zero-use witness
	request := uuid.NewString()
	failAudit(t, x.f.pool, request)
	before = authoritySnapshot(t, x.f.pool, x.f.tenant, tables)
	if err := x.s.FinalizeHubAllocation(ctx, x.f.scope, f.a.AllocationID, request); err == nil {
		t.Fatal("parent settlement escaped audit")
	}
	if !bytes.Equal(before, authoritySnapshot(t, x.f.pool, x.f.tenant, tables)) {
		t.Fatal("partial parent ledger/event")
	}
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			if err := x.s.FinalizeHubAllocation(ctx, x.f.scope, f.a.AllocationID, uuid.NewString()); err != nil {
				t.Error(err)
			}
		})
	}
	wg.Wait()
	q := generated.New(x.f.pool)
	p, err := q.GetSourceProjection(ctx, generated.GetSourceProjectionParams{TenantID: x.f.tenant, SourceID: f.in.SourceID})
	if err != nil || p.F != 0 || p.Charged != 44 || p.RemainderMilli != 400 {
		t.Fatal("wrong original-source coefficient/remainder", p, err)
	}
	a, err := q.ReservationAccounting(ctx, generated.ReservationAccountingParams{TenantID: x.f.tenant, GrantID: f.a.SourceID})
	if err != nil || !a.Terminal || a.RawCumulative != 37 || a.ReturnedRaw != 1 || a.Charged != 44 || a.ReleasedBill != 80 {
		t.Fatal("parent final lost raw/financial conservation", a, err)
	}
	// Exact replay must not append another settlement, resource task or Outbox.
	replayTables := tables[:len(tables)-1]
	before = authoritySnapshot(t, x.f.pool, x.f.tenant, replayTables)
	if err = x.s.FinalizeHubAllocation(ctx, x.f.scope, f.a.AllocationID, uuid.NewString()); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, authoritySnapshot(t, x.f.pool, x.f.tenant, replayTables)) {
		t.Fatal("final replay repeated financial/event mutation")
	}
	if worked, err := (worker.HubCreditSettlementWorker{Service: x.s}).Once(ctx); err != nil || worked {
		t.Fatal("finalized parent remained candidate", worked, err)
	}
}

func TestHubCreditFinalAggregatesTinyChildrenWithoutPerChildCeil(t *testing.T) {
	f := newCreditCloseFixtureMultiplier(t, 1, 1)
	x := f.x
	ctx := context.Background()
	firstBatch := f.commitLocalUsage(t, 1)
	issued, grant, seq := issueAllocationFixture(t, x, f.hs, f.a, 1)
	if err := x.s.RecordHubIssue(x.f.contexts[f.in.HubID], f.in.HubID, f.in.HubBootID, seq, issued.Payload, uuid.NewString()); err != nil {
		t.Fatal(err)
	}
	second := *f
	second.issued, second.grant = issued, grant
	secondBatch := second.commitLocalUsage(t, 1)
	f.seal(t)
	meter := service.MeteringService{Pool: x.f.pool}
	if _, err := meter.IngestWireUsage(ctx, f.in.NodeID, f.in.BootID, 1, firstBatch); err != nil {
		t.Fatal(err)
	}
	if _, err := meter.IngestWireUsage(ctx, f.in.NodeID, f.in.BootID, 2, secondBatch); err != nil {
		t.Fatal(err)
	}
	acceptCreditFinal(t, f)
	acceptCreditFinal(t, &second)
	w := worker.HubCreditSettlementWorker{Service: x.s}
	if worked, err := w.Once(ctx); err != nil || !worked {
		t.Fatal("complete proof not settled", worked, err)
	}
	q := generated.New(x.f.pool)
	p, err := q.GetSourceProjection(ctx, generated.GetSourceProjectionParams{TenantID: x.f.tenant, SourceID: f.in.SourceID})
	if err != nil || p.F != 0 || p.Charged != 0 || p.RemainderMilli != 2 {
		t.Fatal("per-child ceil or remainder reset", p, err)
	}
}

func TestHubCreditFinalRejectsUnknownCreatedAfterAcceptedChild(t *testing.T) {
	f := newCreditCloseFixture(t)
	ctx := context.Background()
	f.seal(t)
	acceptCreditFinal(t, f)
	proof := make([]byte, 32)
	proof[0] = 1
	if err := (service.MeteringService{Pool: f.x.f.pool}).RecordUnknown(ctx, f.x.f.scope, f.a.SourceID, proof, f.x.now, uuid.NewString()); err != nil {
		t.Fatal(err)
	}
	before := authoritySnapshot(t, f.x.f.pool, f.x.f.tenant, []string{"ledger_events", "ledger_projections", "settlements", "audit_events"})
	if err := f.x.s.FinalizeHubAllocation(ctx, f.x.f.scope, f.a.AllocationID, uuid.NewString()); !errors.Is(err, service.ErrShareUnknown) {
		t.Fatal("accepted child overrode later unknown", err)
	}
	if !bytes.Equal(before, authoritySnapshot(t, f.x.f.pool, f.x.f.tenant, []string{"ledger_events", "ledger_projections", "settlements", "audit_events"})) {
		t.Fatal("unknown occupation released")
	}
}
