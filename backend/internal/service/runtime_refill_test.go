package service_test

import (
	"bytes"
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/google/uuid"
	"vortex/backend/db/generated"
	"vortex/backend/internal/service"
	"vortex/backend/internal/worker"
	"vortex/hub"
)

func TestRuntimeRefillRequiresCompleteParentFinalAndOneAuditedSuccessor(t *testing.T) {
	f := newCreditCloseFixture(t)
	x := f.x
	ctx := context.Background()
	batch := f.commitLocalUsage(t, 37)
	f.seal(t)
	if _, err := (service.MeteringService{Pool: x.f.pool}).IngestWireUsage(ctx, f.in.NodeID, f.in.BootID, 1, batch); err != nil {
		t.Fatal(err)
	}
	acceptCreditFinal(t, f)
	w := worker.RuntimeFundingWorker{Service: x.s, Region: "phase6"}
	financial := []string{"hub_funded_allocations", "reservations", "ledger_projections", "ledger_events"}
	before := authoritySnapshot(t, x.f.pool, x.f.tenant, financial)
	if _, err := w.Once(ctx); err != nil && !errors.Is(err, service.ErrShareUnknown) {
		t.Fatal(err)
	}
	if !bytes.Equal(before, authoritySnapshot(t, x.f.pool, x.f.tenant, financial)) {
		t.Fatal("child final alone allowed refill")
	}
	if err := x.s.FinalizeHubAllocation(ctx, x.f.scope, f.a.AllocationID, uuid.NewString()); err != nil {
		t.Fatal(err)
	}
	// Failed final audit cannot create a successor reservation or transport work.
	if _, err := x.f.pool.Exec(ctx, "CREATE FUNCTION reject_refill() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.action='hub.fund_allocation' THEN RAISE EXCEPTION 'injected refill audit failure'; END IF; RETURN NEW; END $$; CREATE TRIGGER reject_refill BEFORE INSERT ON audit_events FOR EACH ROW EXECUTE FUNCTION reject_refill()"); err != nil {
		t.Fatal(err)
	}
	atomicFunding := append(append([]string{}, financial...), "tasks", "operations", "outbox")
	before = authoritySnapshot(t, x.f.pool, x.f.tenant, atomicFunding)
	if _, err := w.Once(ctx); err == nil {
		t.Fatal("refill escaped audit")
	}
	if !bytes.Equal(before, authoritySnapshot(t, x.f.pool, x.f.tenant, atomicFunding)) {
		t.Fatal("partial refill")
	}
	if _, err := x.f.pool.Exec(ctx, "DROP TRIGGER reject_refill ON audit_events;DROP FUNCTION reject_refill()"); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			_, err := w.Once(ctx)
			if err != nil && !errors.Is(err, service.ErrShareUnknown) && !errors.Is(err, service.ErrShareConflict) {
				t.Error(err)
			}
		})
	}
	wg.Wait()
	var count int
	if err := x.f.pool.QueryRow(ctx, "SELECT count(*) FROM hub_funded_allocations WHERE tenant_id=$1 AND node_id=$2", x.f.tenant, f.in.NodeID).Scan(&count); err != nil || count != 2 {
		t.Fatal("refill duplicated/missing", count, err)
	}
	q := generated.New(x.f.pool)
	history, err := q.RuntimeFundingHistory(ctx, generated.RuntimeFundingHistoryParams{TenantID: x.f.tenant, NodeID: f.in.NodeID, BootID: f.in.BootID, SnapshotID: f.a.SnapshotID, SignerEpoch: "7", PathGeneration: "1", FencingFloor: "3"})
	if err != nil || len(history) != 2 {
		t.Fatal(history, err)
	}
	var successor hub.Allocation
	for _, row := range history {
		if row.AllocationID == f.a.AllocationID {
			if !row.Closed {
				t.Fatal("complete original not recognized")
			}
			continue
		}
		if row.Closed {
			t.Fatal("new parent fabricated final")
		}
		a, err := q.GetHubAllocation(ctx, generated.GetHubAllocationParams{TenantID: x.f.tenant, AllocationID: row.AllocationID})
		if err != nil {
			t.Fatal(err)
		}
		successor, err = hub.DecodeAllocation(a.Payload)
		if err != nil {
			t.Fatal(err)
		}
	}
	if successor.SourceID == f.a.SourceID || successor.RawBytes < 1 {
		t.Fatal("reused original financial parent", successor)
	}
	projection, err := q.GetSourceProjection(ctx, generated.GetSourceProjectionParams{TenantID: x.f.tenant, SourceID: f.in.SourceID})
	if err != nil || projection.Charged != 44 || projection.RemainderMilli != 400 || projection.F > projection.B {
		t.Fatal("refill unfunded or reset source", projection, err)
	}
	// The next finite delegation reaches the real FULL Hub issuer under a new
	// parent source; it does not reopen old grant IDs or reserve a second time.
	installAllocationFixture(t, f.hs, successor, x.now)
	issue, _, seq := issueAllocationFixture(t, x, f.hs, successor, min(successor.RawBytes, 51))
	before = authoritySnapshot(t, x.f.pool, x.f.tenant, []string{"reservations", "ledger_events", "ledger_projections"})
	if err = x.s.RecordHubIssue(x.f.contexts[f.in.HubID], f.in.HubID, f.in.HubBootID, seq, issue.Payload, uuid.NewString()); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, authoritySnapshot(t, x.f.pool, x.f.tenant, []string{"reservations", "ledger_events", "ledger_projections"})) {
		t.Fatal("successor Hub child double reserved funds")
	}
	before = authoritySnapshot(t, x.f.pool, x.f.tenant, financial)
	for range 3 {
		if _, err = w.Once(ctx); err != nil && !errors.Is(err, service.ErrShareUnknown) {
			t.Fatal(err)
		}
	}
	if !bytes.Equal(before, authoritySnapshot(t, x.f.pool, x.f.tenant, financial)) {
		t.Fatal("unclosed successor repeatedly refilled")
	}
}
