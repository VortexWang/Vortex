package service_test

import (
	"bytes"
	"context"
	"github.com/google/uuid"
	"testing"
	"vortex/backend/db/generated"
)

func TestShareBootstrapCommitWaitsForBothKindsAtEveryPreparedNode(t *testing.T) {
	x := newShareFixture(t)
	ctx := context.Background()
	q := generated.New(x.f.pool)
	b, e := q.GetBillingSnapshot(ctx, generated.GetBillingSnapshotParams{TenantID: x.f.tenant, ID: x.in.SnapshotID})
	if e != nil {
		t.Fatal(e)
	}
	prepareAllocationRule(t, x, b.RuleID, b.ID)
	count := func() int {
		t.Helper()
		var n int
		if e = x.f.pool.QueryRow(ctx, "SELECT count(*) FROM rule_delivery_bindings WHERE tenant_id=$1 AND phase='commit'", x.f.tenant).Scan(&n); e != nil {
			t.Fatal(e)
		}
		return n
	}
	if count() != 0 {
		t.Fatal("Prepare alone issued Commit")
	}
	first := x.in
	for _, kind := range []string{"rate", "connections"} {
		x.in.Kind = kind
		x.in.Nonce = uuid.NewString()
		x.in.Amount = 500000
		x.in.BurstBytes = 62500
		if kind == "connections" {
			x.in.Amount = 1
			x.in.BurstBytes = 0
		}
		x.prepare(t)
	}
	node, e := q.GetNodeIncarnation(ctx, x.f.nodes[1])
	if e != nil {
		t.Fatal(e)
	}
	x.in = first
	x.in.NodeID = x.f.nodes[1]
	x.in.BootID = node.BootID
	x.in.Nonce = uuid.NewString()
	x.in.Amount = 500000
	x.in.BurstBytes = 62500
	x.prepare(t)
	if count() != 0 {
		t.Fatal("partial node/kind bootstrap issued Commit")
	}
	x.in.Kind = "connections"
	x.in.Nonce = uuid.NewString()
	x.in.Amount = 1
	x.in.BurstBytes = 0
	request := uuid.NewString()
	failAudit(t, x.f.pool, request)
	tables := []string{"share_chains", "share_generations", "share_renewal_authorities", "share_handoffs", "rule_deployments", "rule_delivery_bindings", "rule_heads", "operations", "tasks", "outbox", "audit_events"}
	before := authoritySnapshot(t, x.f.pool, x.f.tenant, tables)
	if _, e = x.s.PrepareShare(ctx, x.f.scope, x.in, request); e == nil {
		t.Fatal("bootstrap escaped audit")
	}
	if !bytes.Equal(before, authoritySnapshot(t, x.f.pool, x.f.tenant, tables)) {
		t.Fatal("partial bootstrap/Commit")
	}
	x.prepare(t)
	if count() != 2 {
		t.Fatal("fully reserved bootstrap did not resume both members")
	}
	var active int
	if e = x.f.pool.QueryRow(ctx, "SELECT count(*) FROM share_chains WHERE tenant_id=$1 AND active_generation>0", x.f.tenant).Scan(&active); e != nil || active != 0 {
		t.Fatal("Commit permit falsely confirmed capacity", active, e)
	}
}
