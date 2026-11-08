package service_test

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"testing"
	"time"
	"vortex/backend/db/generated"
	"vortex/backend/internal/auth"
	"vortex/backend/internal/compiler"
	"vortex/backend/internal/policy"
	"vortex/backend/internal/service"
)

func prepareAllocationRule(t *testing.T, x *shareFixture, rule string, snapshot string) {
	t.Helper()
	prepareAllocationRuleEpoch(t, x, rule, snapshot, 7)
}
func prepareAllocationRuleEpoch(t *testing.T, x *shareFixture, rule, snapshot string, epoch int64) {
	t.Helper()
	ctx := context.Background()
	q := generated.New(x.f.pool)
	b, e := q.GetBillingSnapshot(ctx, generated.GetBillingSnapshotParams{TenantID: x.f.tenant, ID: snapshot})
	if e != nil {
		t.Fatal(e)
	}
	bill := compiler.BillingSnapshot{ID: b.ID, Version: b.Version, MultiplierMilli: b.MultiplierMilli, RuleRevision: b.RuleRevision, PathGeneration: b.PathGeneration}
	r, e := (service.RuleService{Pool: x.f.pool}).CompileRevision(ctx, x.f.scope, rule, b.RuleRevision, bill, epoch, b.PathGeneration, 3, uuid.NewString())
	if e != nil {
		t.Fatalf("compile allocation rule epoch%d revision%d: %v", epoch, b.RuleRevision, e)
	}
	o := service.RuleOrchestrator{Pool: x.f.pool}
	d, e := o.Begin(ctx, x.f.scope, rule, b.RuleRevision, r.InputDigest, uuid.NewString())
	if e != nil {
		t.Fatalf("begin allocation rule epoch%d revision%d: %v diagnostics=%v", epoch, b.RuleRevision, e, r.Diagnostics)
	}
	rows, e := x.f.pool.Query(ctx, "SELECT task_id FROM rule_delivery_bindings WHERE tenant_id=$1 AND deployment_id=$2 AND phase='prepare' ORDER BY node_id", x.f.tenant, d.ID)
	if e != nil {
		t.Fatal(e)
	}
	var ids []string
	for rows.Next() {
		var id string
		if e = rows.Scan(&id); e != nil {
			t.Fatal(e)
		}
		ids = append(ids, id)
	}
	rows.Close()
	if e = rows.Err(); e != nil {
		t.Fatal(e)
	}
	for _, id := range ids {
		c, node, _, e := service.RuleCommand(ctx, q, x.f.tenant, id)
		if e != nil {
			t.Fatal(e)
		}
		lease := uuid.NewString()
		if _, e = x.f.pool.Exec(ctx, "UPDATE tasks SET state='running',lease_token=$3 WHERE tenant_id=$1 AND id=$2", x.f.tenant, id, lease); e != nil {
			t.Fatal(e)
		}
		if e = o.ReceiptWithLease(x.f.contexts[node], x.f.scope, id, lease, receipt(c, "prepared"), uuid.NewString()); e != nil {
			t.Fatalf("allocation Prepare receipt: %v", e)
		}
	}
}
func TestAllocationWindowPreparedCandidatesReuseAndDemandAuditRollback(t *testing.T) {
	x := newShareFixture(t)
	ctx := context.Background()
	q := generated.New(x.f.pool)
	b, e := q.GetBillingSnapshot(ctx, generated.GetBillingSnapshotParams{TenantID: x.f.tenant, ID: x.in.SnapshotID})
	if e != nil {
		t.Fatal(e)
	}
	empty, e := x.s.AllocateWindow(ctx, x.f.scope, uuid.NewString())
	if e != nil || len(empty.Nodes) != 0 {
		t.Fatal("unprepared candidate", empty, e)
	}
	prepareAllocationRule(t, x, b.RuleID, b.ID)
	// The next trusted minute sees Prepare; the existing window is immutable.
	same, e := x.s.AllocateWindow(ctx, x.f.scope, uuid.NewString())
	if e != nil || len(same.Nodes) != 0 || same.OperationID != empty.OperationID {
		t.Fatal("same minute was rewritten", same, e)
	}
	x.now = x.now.Add(time.Minute)
	sample := service.DemandSample{SampleID: uuid.NewString(), NodeID: x.in.NodeID, BootID: x.in.BootID, WindowAt: x.now.Truncate(time.Minute).Add(-time.Minute), RateDemand: 100, RateExhaustionPPM: 1000000}
	request := uuid.NewString()
	failAudit(t, x.f.pool, request)
	tables := []string{"share_demand_samples", "share_allocation_windows", "share_allocation_queue", "operations", "tasks", "outbox", "audit_events"}
	before := authoritySnapshot(t, x.f.pool, x.f.tenant, tables)
	if e = x.s.RecordDemand(x.f.contexts[x.in.NodeID], x.f.scope, sample, request); e == nil {
		t.Fatal("sample escaped audit")
	}
	if !bytes.Equal(before, authoritySnapshot(t, x.f.pool, x.f.tenant, tables)) {
		t.Fatal("partial sample")
	}
	if e = x.s.RecordDemand(x.f.contexts[x.in.NodeID], x.f.scope, sample, uuid.NewString()); e != nil {
		t.Fatal(e)
	}
	if e = x.s.RecordDemand(x.f.contexts[x.in.NodeID], x.f.scope, sample, uuid.NewString()); e != nil {
		t.Fatal(e)
	}
	sample.RateDemand++
	if e = x.s.RecordDemand(x.f.contexts[x.in.NodeID], x.f.scope, sample, uuid.NewString()); e == nil {
		t.Fatal("sample overwrite")
	}
	before = authoritySnapshot(t, x.f.pool, x.f.tenant, tables)
	if _, e = x.s.AllocateWindow(ctx, x.f.scope, request); e == nil {
		t.Fatal("allocation escaped audit")
	}
	if !bytes.Equal(before, authoritySnapshot(t, x.f.pool, x.f.tenant, tables)) {
		t.Fatal("partial window or queue")
	}
	out, e := x.s.AllocateWindow(ctx, x.f.scope, uuid.NewString())
	if e != nil || len(out.Nodes) != 2 {
		t.Fatal(out, e)
	}
	var total int64
	for _, n := range out.Nodes {
		total += n.RateBPS
		if n.NodeID == x.in.NodeID && n.RateBPS != 750000 {
			t.Fatal("demand weighting missing", n)
		}
	}
	if total != 1000000 {
		t.Fatal("global target conservation", out)
	}
	// Actual execution projection cannot exclude prepared candidates.
	if _, e = x.f.pool.Exec(ctx, "UPDATE rule_heads SET active_revision=0 WHERE tenant_id=$1", x.f.tenant); e != nil {
		t.Fatal(e)
	}
	// A second prepared rule on the same nodes adds references, not owners.
	in := ruleInput(x.f.group)
	in.Port = 20002
	reserveRulePort(t, x.f, in)
	if _, e = (service.RuleService{Pool: x.f.pool}).Mutate(ctx, x.f.scope, in, 0, uuid.NewString()); e != nil {
		t.Fatal(e)
	}
	second := durableBilling(t, x.f, in.ID, 1, 1)
	prepareAllocationRule(t, x, in.ID, second.ID)
	x.now = x.now.Add(time.Minute)
	reused, e := x.s.AllocateWindow(ctx, x.f.scope, uuid.NewString())
	if e != nil || len(reused.Nodes) != 2 {
		t.Fatal("rule duplicated node allocation", reused, e)
	}
	for _, n := range reused.Nodes {
		if len(n.References) != 2 {
			t.Fatal("reference lost", n)
		}
	}
	encoded, _ := json.Marshal(reused)
	again, e := x.s.AllocateWindow(ctx, x.f.scope, uuid.NewString())
	raw, _ := json.Marshal(again)
	if e != nil || !bytes.Equal(encoded, raw) {
		t.Fatal("window replay changed", e)
	}
	var held int64
	if e = x.f.pool.QueryRow(ctx, "SELECT COALESCE(sum(held_amount),0) FROM share_chains WHERE tenant_id=$1", x.f.tenant).Scan(&held); e != nil || held != 0 {
		t.Fatal("target itself minted share", held, e)
	}
}

func TestAllocationWindowRevokedGrantKeepsUnknownOccupation(t *testing.T) {
	x := newShareFixture(t)
	ctx := context.Background()
	b, e := generated.New(x.f.pool).GetBillingSnapshot(ctx, generated.GetBillingSnapshotParams{TenantID: x.f.tenant, ID: x.in.SnapshotID})
	if e != nil {
		t.Fatal(e)
	}
	prepareAllocationRule(t, x, b.RuleID, b.ID)
	p := x.prepare(t)
	if e = x.s.QueueHandoff(ctx, x.f.scope, p.Generation.ID, uuid.NewString()); e != nil {
		t.Fatal(e)
	}
	leaseShareTask(t, x, p.Generation.ID)
	out, e := x.s.AllocateWindow(ctx, x.f.scope, uuid.NewString())
	if e != nil || len(out.Nodes) != 2 {
		t.Fatal(out, e)
	}
	if e = x.f.nodeService.SetGrant(ctx, policyScope(t, x.f.tenant, auth.PlatformAdmin, policy.Grant), x.f.group, 1, "revoked", time.Now().Add(-time.Hour), time.Now().Add(time.Hour), uuid.NewString()); e != nil {
		t.Fatal(e)
	}
	x.now = x.now.Add(time.Minute)
	out, e = x.s.AllocateWindow(ctx, x.f.scope, uuid.NewString())
	if e != nil || len(out.Nodes) != 0 {
		t.Fatal("revoked candidate", out, e)
	}
	if c := shareChain(t, x); c.HeldAmount != 600000 || c.ActiveGeneration != 0 {
		t.Fatal("excluded candidate freed unknown", c)
	}
}

func TestAllocationWindowConnectionShortagePersistsRealQueue(t *testing.T) {
	terms := packageTerms()
	terms.ConnectionLimit = 1
	x := newShareFixtureTerms(t, terms)
	ctx := context.Background()
	b, e := generated.New(x.f.pool).GetBillingSnapshot(ctx, generated.GetBillingSnapshotParams{TenantID: x.f.tenant, ID: x.in.SnapshotID})
	if e != nil {
		t.Fatal(e)
	}
	prepareAllocationRule(t, x, b.RuleID, b.ID)
	out, e := x.s.AllocateWindow(ctx, x.f.scope, uuid.NewString())
	if e != nil || len(out.Nodes) != 2 {
		t.Fatal(out, e)
	}
	admitted, queued := 0, 0
	for _, n := range out.Nodes {
		if n.Queued {
			queued++
			if n.Connections != 0 {
				t.Fatal("queued node got slot")
			}
		} else {
			admitted++
			if n.Connections != 1 {
				t.Fatal("capacity repeated")
			}
		}
	}
	if admitted != 1 || queued != 1 {
		t.Fatal(out)
	}
	rows, e := generated.New(x.f.pool).GetShareAllocationQueue(ctx, x.f.tenant)
	if e != nil {
		t.Fatal(e)
	}
	recorded := 0
	for _, q := range rows {
		if q.Kind == "connections" && q.State == "queued" && q.Target == 0 {
			recorded++
		}
	}
	if recorded != 1 {
		t.Fatal("queue is only a UI flag", rows)
	}
	// A queue target creates no share generation, runtime lease or extra slot.
	var minted int
	if e = x.f.pool.QueryRow(ctx, "SELECT count(*) FROM share_generations WHERE tenant_id=$1", x.f.tenant).Scan(&minted); e != nil || minted != 0 {
		t.Fatal(minted, e)
	}
}
