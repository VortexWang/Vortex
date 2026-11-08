package service_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"vortex/agent/store"
	"vortex/backend/db/generated"
	"vortex/backend/internal/service"
)

func TestNodeSourceProvisionAuditRollbackReceiptsAndFundedHold(t *testing.T) {
	x, in, _ := fundedHubFixture(t)
	ctx := context.Background()
	a, e := x.s.PrepareHubAllocation(ctx, x.f.scope, in, uuid.NewString())
	if e != nil {
		t.Fatal(e)
	}
	request := uuid.NewString()
	failAudit(t, x.f.pool, request)
	tables := []string{"node_source_provisions", "node_source_receipts", "tasks", "operations", "outbox", "audit_events", "reservations", "ledger_projections"}
	before := authoritySnapshot(t, x.f.pool, x.f.tenant, tables)
	if _, e = x.s.PrepareNodeSources(ctx, x.f.scope, a.AllocationID, shareBackendID, request); e == nil {
		t.Fatal("provision escaped audit")
	}
	if !bytes.Equal(before, authoritySnapshot(t, x.f.pool, x.f.tenant, tables)) {
		t.Fatal("partial provision")
	}
	p, e := x.s.PrepareNodeSources(ctx, x.f.scope, a.AllocationID, shareBackendID, uuid.NewString())
	if e != nil {
		t.Fatal(e)
	}
	replay, e := x.s.PrepareNodeSources(ctx, x.f.scope, a.AllocationID, shareBackendID, uuid.NewString())
	if e != nil || !bytes.Equal(p.Payload, replay.Payload) {
		t.Fatal("provision replay changed", e)
	}
	st, e := store.Open(t.TempDir()+"/agent.db", "agent")
	if e != nil {
		t.Fatal(e)
	}
	defer st.Close()
	r, e := st.ApplySourceProvision(ctx, in.NodeID, in.BootID, shareBackendID, p.Payload, store.IssuerTime{Earliest: x.now, Latest: x.now.Add(time.Second), Trusted: true})
	if e != nil {
		t.Fatal(e)
	}
	if e = x.s.RecordNodeSources(ctx, x.f.scope, r, uuid.NewString()); !errors.Is(e, service.ErrScope) {
		t.Fatal("unsigned receipt", e)
	}
	request = uuid.NewString()
	failAudit(t, x.f.pool, request)
	before = authoritySnapshot(t, x.f.pool, x.f.tenant, tables)
	if e = x.s.RecordNodeSources(x.f.contexts[in.NodeID], x.f.scope, r, request); e == nil {
		t.Fatal("receipt escaped audit")
	}
	if !bytes.Equal(before, authoritySnapshot(t, x.f.pool, x.f.tenant, tables)) {
		t.Fatal("partial receipt")
	}
	for i := 0; i < 2; i++ {
		if e = x.s.RecordNodeSources(x.f.contexts[in.NodeID], x.f.scope, r, uuid.NewString()); e != nil {
			t.Fatal(e)
		}
	}
	bad := r
	bad.BootID = uuid.NewString()
	if e = x.s.RecordNodeSources(x.f.contexts[in.NodeID], x.f.scope, bad, uuid.NewString()); !errors.Is(e, service.ErrStaleReceipt) {
		t.Fatal("other boot receipt", e)
	}
	q := generated.New(x.f.pool)
	op, e := q.GetOperation(ctx, generated.GetOperationParams{TenantID: x.f.tenant, ID: p.OperationID})
	if e != nil || op.State != "succeeded" || !op.OccupationHeld {
		t.Fatal("provision released occupation", e, op)
	}
	projection, e := q.GetSourceProjection(ctx, generated.GetSourceProjectionParams{TenantID: x.f.tenant, SourceID: in.SourceID})
	if e != nil || projection.F != in.RawBytes || projection.Charged != 0 {
		t.Fatal("roots changed funds", e, projection)
	}
	raw, _ := json.Marshal(r)
	digest := sha256.Sum256(raw)
	saved, e := q.GetNodeSourceReceipt(ctx, generated.GetNodeSourceReceiptParams{TenantID: x.f.tenant, AllocationID: a.AllocationID})
	if e != nil || !bytes.Equal(saved.PayloadDigest, digest[:]) {
		t.Fatal("receipt not durable", e)
	}
}
