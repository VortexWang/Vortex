package service_test

import (
	"bytes"
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"vortex/agent/store"
	"vortex/backend/db/generated"
	"vortex/backend/internal/service"
	"vortex/backend/internal/worker"
)

func TestRuntimeFundingSchedulerRequiresCASAndKeepsOneFiniteParentAcrossRetries(t *testing.T) {
	terms := packageTerms()
	terms.TrafficBytes = 4 << 20
	x := schedulingFixture(t, terms)
	ctx := context.Background()
	q := generated.New(x.f.pool)
	boots := map[string]string{}
	for _, id := range x.f.nodes {
		hub, e := q.UsageNodeHub(ctx, id)
		if e != nil {
			t.Fatal(e)
		}
		if boots[hub] == "" {
			boots[hub] = uuid.NewString()
			if _, e = x.s.BindHubSession(portNodeContext(t, x.s.SignerCA, x.f.caKey, hub), hub, boots[hub], uuid.NewString()); e != nil {
				t.Fatal(e)
			}
		}
	}
	if _, e := (worker.ShareSchedulingWorker{Service: x.s}).Once(ctx); e != nil {
		t.Fatal(e)
	}
	w := worker.RuntimeFundingWorker{Service: x.s, Region: "phase6"}
	if _, e := w.Once(ctx); !errors.Is(e, service.ErrShareUnknown) {
		t.Fatal("queued CAS unexpectedly funded runtime", e)
	}
	for _, id := range x.f.nodes {
		node, e := q.GetNodeIncarnation(ctx, id)
		if e != nil {
			t.Fatal(e)
		}
		st, e := store.Open(t.TempDir()+"/node.db", "agent")
		if e != nil {
			t.Fatal(e)
		}
		t.Cleanup(func() { st.Close() })
		local := *x
		local.in.NodeID, local.in.BootID = id, node.BootID
		for _, kind := range []string{"rate", "connections"} {
			g, e := q.GetShareGeneration(ctx, generated.GetShareGenerationParams{TenantID: x.f.tenant, NodeID: id, Kind: kind, Generation: 1})
			if e != nil {
				t.Fatal(e)
			}
			confirmHandoff(t, &local, st, g.ID)
		}
	}
	// Fail the final audit write in the real funding producer. Earlier window
	// reads can append audit entries, but no funds/tasks/Outbox may escape it.
	if _, e := x.f.pool.Exec(ctx, "CREATE FUNCTION reject_scheduler_funding() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.action='hub.fund_allocation' THEN RAISE EXCEPTION 'injected scheduler funding audit failure'; END IF; RETURN NEW; END $$; CREATE TRIGGER reject_scheduler_funding BEFORE INSERT ON audit_events FOR EACH ROW EXECUTE FUNCTION reject_scheduler_funding()"); e != nil {
		t.Fatal(e)
	}
	tables := []string{"hub_funded_allocations", "reservations", "ledger_projections", "ledger_events", "tasks", "operations", "outbox"}
	before := authoritySnapshot(t, x.f.pool, x.f.tenant, tables)
	if _, e := w.Once(ctx); e == nil {
		t.Fatal("funding bypassed same-transaction audit")
	}
	if !bytes.Equal(before, authoritySnapshot(t, x.f.pool, x.f.tenant, tables)) {
		t.Fatal("funding scheduler leaked a partial financial mutation")
	}
	if _, e := x.f.pool.Exec(ctx, "DROP TRIGGER reject_scheduler_funding ON audit_events; DROP FUNCTION reject_scheduler_funding()"); e != nil {
		t.Fatal(e)
	}
	var wg sync.WaitGroup
	results := make(chan error, 4)
	for range 4 {
		wg.Go(func() {
			_, e := w.Once(ctx)
			results <- e
		})
	}
	wg.Wait()
	close(results)
	for e := range results {
		if e != nil && !errors.Is(e, service.ErrShareConflict) {
			t.Fatal(e)
		}
	}
	if _, e := w.Once(ctx); e != nil {
		t.Fatal(e)
	}
	var allocations int
	if e := x.f.pool.QueryRow(ctx, "SELECT count(*) FROM hub_funded_allocations WHERE tenant_id=$1", x.f.tenant).Scan(&allocations); e != nil || allocations != 2 {
		t.Fatal("duplicate or missing funded parents", allocations, e)
	}
	var held int64
	if e := x.f.pool.QueryRow(ctx, "SELECT sum(f) FROM ledger_projections WHERE tenant_id=$1", x.f.tenant).Scan(&held); e != nil || held != 2<<20 {
		t.Fatal("initial raw budget did not reserve exactly once", held, e)
	}
	financial := []string{"hub_funded_allocations", "reservations", "ledger_projections", "ledger_events"}
	stable := authoritySnapshot(t, x.f.pool, x.f.tenant, financial)
	x.now = x.now.Add(time.Minute)
	// A new allocation window has its own durable operation, so compare the
	// financial authority independently from those scheduling tasks.
	for range 3 {
		if _, e := w.Once(ctx); e != nil {
			t.Fatal(e)
		}
	}
	if !bytes.Equal(stable, authoritySnapshot(t, x.f.pool, x.f.tenant, financial)) {
		t.Fatal("idle retries refilled or returned unknown funding")
	}
	x.now = x.now.Add(2 * time.Hour)
	if _, e := w.Once(ctx); e != nil {
		t.Fatal(e)
	}
	if !bytes.Equal(stable, authoritySnapshot(t, x.f.pool, x.f.tenant, financial)) {
		t.Fatal("expired autonomy returned or replaced unclosed parent")
	}
}

func TestRuntimeFundingSchedulerRecognizesExplicitAllocationWithoutRefill(t *testing.T) {
	x, in, _ := fundedHubFixture(t)
	ctx := context.Background()
	if _, e := x.s.PrepareHubAllocation(ctx, x.f.scope, in, uuid.NewString()); e != nil {
		t.Fatal(e)
	}
	tables := []string{"hub_funded_allocations", "reservations", "ledger_projections", "ledger_events"}
	before := authoritySnapshot(t, x.f.pool, x.f.tenant, tables)
	if _, e := (worker.RuntimeFundingWorker{Service: x.s, Region: "phase6"}).Once(ctx); !errors.Is(e, service.ErrShareUnknown) {
		t.Fatal("unconfirmed second Node should remain unfunded", e)
	}
	if !bytes.Equal(before, authoritySnapshot(t, x.f.pool, x.f.tenant, tables)) {
		t.Fatal("explicit allocation was funded twice by automatic producer")
	}
}
