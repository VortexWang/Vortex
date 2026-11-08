package service_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"vortex/agent/store"
	"vortex/backend/db/generated"
	"vortex/backend/internal/auth"
	"vortex/backend/internal/policy"
	"vortex/backend/internal/service"
	"vortex/backend/internal/worker"
)

func schedulingFixture(t *testing.T, terms service.PackageTerms) *shareFixture {
	t.Helper()
	x := newShareFixtureTerms(t, terms)
	ctx := context.Background()
	q := generated.New(x.f.pool)
	seen := map[string]bool{}
	for _, node := range x.f.nodes {
		hub, e := q.UsageNodeHub(ctx, node)
		if e != nil {
			t.Fatal(e)
		}
		if seen[hub] {
			continue
		}
		seen[hub] = true
		proof := sha256.Sum256([]byte("explicit scheduler fixture signer approval"))
		if _, e = x.s.AuthorizeSigner(ctx, policyScope(t, x.f.tenant, auth.PlatformAdmin, policy.Grant), hub, 7, x.now.Add(-time.Second), x.now.Add(time.Hour), proof[:], uuid.NewString()); e != nil {
			t.Fatal(e)
		}
	}
	b, e := q.GetBillingSnapshot(ctx, generated.GetBillingSnapshotParams{TenantID: x.f.tenant, ID: x.in.SnapshotID})
	if e != nil {
		t.Fatal(e)
	}
	prepareAllocationRule(t, x, b.RuleID, b.ID)
	return x
}

func TestShareSchedulingConcurrentWorkersDurableBootstrapAndUnchangedTarget(t *testing.T) {
	x := schedulingFixture(t, packageTerms())
	ctx := context.Background()
	w := worker.ShareSchedulingWorker{Service: x.s}
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
	if worked, e := w.Once(ctx); e != nil || !worked {
		t.Fatal(worked, e)
	}
	var generations, deliveries int
	if e := x.f.pool.QueryRow(ctx, "SELECT count(*) FROM share_generations WHERE tenant_id=$1", x.f.tenant).Scan(&generations); e != nil || generations != 4 {
		t.Fatal("concurrent scheduler minted generations", generations, e)
	}
	if e := x.f.pool.QueryRow(ctx, "SELECT count(*) FROM share_delivery_bindings WHERE tenant_id=$1", x.f.tenant).Scan(&deliveries); e != nil || deliveries != 4 {
		t.Fatal("concurrent scheduler duplicated CAS delivery", deliveries, e)
	}
	// Neither the schedule nor a queued CAS is an execution receipt. Obtain the
	// actual FULL Node observations before accepting active ownership in PG.
	q := generated.New(x.f.pool)
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
			c, e := q.ReadShareChain(ctx, generated.ReadShareChainParams{TenantID: x.f.tenant, NodeID: id, Kind: kind})
			if e != nil || c.ActiveGeneration != 0 || c.PendingGeneration != 1 {
				t.Fatal("schedule invented activation", c, e)
			}
			g, e := q.GetShareGeneration(ctx, generated.GetShareGenerationParams{TenantID: x.f.tenant, NodeID: id, Kind: kind, Generation: 1})
			if e != nil {
				t.Fatal(e)
			}
			confirmHandoff(t, &local, st, g.ID)
		}
	}
	x.now = x.now.Add(time.Minute)
	if _, e := w.Once(ctx); e != nil {
		t.Fatal(e)
	}
	var after int
	if e := x.f.pool.QueryRow(ctx, "SELECT count(*) FROM share_generations WHERE tenant_id=$1", x.f.tenant).Scan(&after); e != nil || after != generations {
		t.Fatal("unchanged next minute refilled generations", after, e)
	}
	var heldRate, heldConnections int64
	if e := x.f.pool.QueryRow(ctx, "SELECT sum(held_amount) FILTER (WHERE kind='rate'),sum(held_amount) FILTER (WHERE kind='connections') FROM share_chains WHERE tenant_id=$1", x.f.tenant).Scan(&heldRate, &heldConnections); e != nil || heldRate != packageTerms().RateLimit || heldConnections != packageTerms().ConnectionLimit {
		t.Fatal("global capacity conservation", heldRate, heldConnections, e)
	}
	var funding int
	if e := x.f.pool.QueryRow(ctx, "SELECT count(*) FROM hub_funded_allocations WHERE tenant_id=$1", x.f.tenant).Scan(&funding); e != nil || funding != 0 {
		t.Fatal("share scheduling invented traffic funds", funding, e)
	}
}

func TestShareSchedulingQueueMissingApprovalClockAndAuditFailClosed(t *testing.T) {
	terms := packageTerms()
	terms.ConnectionLimit = 1
	x := schedulingFixture(t, terms)
	ctx := context.Background()
	request := uuid.NewString()
	failAudit(t, x.f.pool, request)
	tables := []string{"share_allocation_windows", "share_allocation_queue", "share_generations", "share_chains", "tasks", "outbox", "operations", "audit_events"}
	before := authoritySnapshot(t, x.f.pool, x.f.tenant, tables)
	if _, e := x.s.ScheduleShares(ctx, x.f.scope, request); e == nil {
		t.Fatal("scheduler escaped window audit failure")
	}
	if !bytes.Equal(before, authoritySnapshot(t, x.f.pool, x.f.tenant, tables)) {
		t.Fatal("partial schedule after audit rollback")
	}
	noClock := x.s
	noClock.Clock = nil
	if _, e := (worker.ShareSchedulingWorker{Service: noClock}).Once(ctx); !errors.Is(e, service.ErrLeaseBoundary) {
		t.Fatal("scheduler accepted uncertain clock", e)
	}
	w, e := x.s.ScheduleShares(ctx, x.f.scope, uuid.NewString())
	if e != nil {
		t.Fatal(e)
	}
	q := generated.New(x.f.pool)
	for _, n := range w.Nodes {
		c, e := q.ReadShareChain(ctx, generated.ReadShareChainParams{TenantID: x.f.tenant, NodeID: n.NodeID, Kind: "connections"})
		if n.Queued {
			if e == nil {
				t.Fatal("FIFO queue minted executable capacity", c)
			}
		} else if e != nil || c.HeldAmount != 1 {
			t.Fatal("admitted candidate not scheduled", c, e)
		}
	}

	// Prepared candidates alone cannot authorize a signer epoch.
	y := newShareFixture(t)
	b, e := generated.New(y.f.pool).GetBillingSnapshot(ctx, generated.GetBillingSnapshotParams{TenantID: y.f.tenant, ID: y.in.SnapshotID})
	if e != nil {
		t.Fatal(e)
	}
	prepareAllocationRule(t, y, b.RuleID, b.ID)
	if _, e = (worker.ShareSchedulingWorker{Service: y.s}).Once(ctx); e == nil {
		t.Fatal("scheduler invented missing Hub approval")
	}
	var minted int
	if e = y.f.pool.QueryRow(ctx, "SELECT count(*) FROM share_generations WHERE tenant_id=$1", y.f.tenant).Scan(&minted); e != nil || minted != 0 {
		t.Fatal("unapproved Hub minted rights", minted, e)
	}
}

func TestShareSchedulingResumesPreparedSuccessorWithoutForgingClosure(t *testing.T) {
	x := schedulingFixture(t, packageTerms())
	ctx := context.Background()
	// Crash after reservation and before QueueHandoff: production scheduler
	// discovers the durable pending generation and sends this same ID.
	hub, e := generated.New(x.f.pool).UsageNodeHub(ctx, x.in.NodeID)
	if e != nil {
		t.Fatal(e)
	}
	x.in.SignerID = hub
	x.in.Amount, x.in.BurstBytes = 500000, 62500
	p := x.prepare(t)
	if _, e = (worker.ShareSchedulingWorker{Service: x.s}).Once(ctx); e != nil {
		t.Fatal("reservation crash did not resume", e)
	}
	st, e := store.Open(t.TempDir()+"/node.db", "agent")
	if e != nil {
		t.Fatal(e)
	}
	defer st.Close()
	confirmHandoff(t, x, st, p.Generation.ID)
	x.in.ExpectedGeneration, x.in.Amount, x.in.BurstBytes = 1, 250000, 31250
	x.in.Nonce = uuid.NewString()
	successor := x.prepare(t)
	if _, e = (worker.ShareSchedulingWorker{Service: x.s}).Once(ctx); !errors.Is(e, service.ErrShareUnknown) {
		t.Fatal("scheduler bypassed predecessor closure", e)
	}
	c := shareChain(t, x)
	if c.ActiveGeneration != 1 || c.PendingGeneration != 2 || c.HeldAmount != 500000 {
		t.Fatal("unclosed successor released or activated", c)
	}
	h, e := generated.New(x.f.pool).GetShareHandoff(ctx, generated.GetShareHandoffParams{TenantID: x.f.tenant, ID: successor.Generation.ID})
	if e != nil || len(h.Command) != 0 {
		t.Fatal("unclosed successor became deliverable", h, e)
	}
}
