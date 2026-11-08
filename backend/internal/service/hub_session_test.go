package service_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"vortex/backend/db/generated"
	"vortex/backend/internal/auth"
	"vortex/backend/internal/policy"
	"vortex/backend/internal/service"
)

func TestHubBootBindingApprovedEpochAuditRollbackAndNoImplicitRecovery(t *testing.T) {
	x := newShareFixture(t)
	id, boot := x.in.SignerID, uuid.NewString()
	ctx := x.f.contexts[id]
	request := uuid.NewString()
	failAudit(t, x.f.pool, request)
	if _, e := x.s.BindHubSession(ctx, id, boot, request); e == nil {
		t.Fatal("boot binding escaped audit rollback")
	}
	q := generated.New(x.f.pool)
	if _, e := q.GetHubIncarnation(ctx, generated.GetHubIncarnationParams{HubID: id, SignerEpoch: 7}); e == nil {
		t.Fatal("partial boot binding")
	}
	if _, e := x.s.BindHubSession(context.Background(), id, boot, uuid.NewString()); !errors.Is(e, service.ErrScope) {
		t.Fatal("unsigned boot", e)
	}
	for i := 0; i < 2; i++ {
		b, e := x.s.BindHubSession(ctx, id, boot, uuid.NewString())
		if e != nil || b.Epoch != 7 || !b.Deadline.Equal(x.now.Add(time.Hour)) {
			t.Fatal("reconnect moved authority", e, b)
		}
	}
	if _, e := x.s.BindHubSession(ctx, id, uuid.NewString(), uuid.NewString()); !errors.Is(e, service.ErrShareUnknown) {
		t.Fatal("new boot reopened old epoch", e)
	}
	proof := sha256.Sum256([]byte("explicit next epoch does not prove previous stop"))
	if _, e := x.s.AuthorizeSigner(context.Background(), policyScope(t, x.f.tenant, auth.PlatformAdmin, policy.Grant), id, 8, x.now.Add(-time.Second), x.now.Add(time.Hour), proof[:], uuid.NewString()); e != nil {
		t.Fatal(e)
	}
	if _, e := x.s.BindHubSession(ctx, id, uuid.NewString(), uuid.NewString()); !errors.Is(e, service.ErrShareUnknown) {
		t.Fatal("new epoch bypassed boot recovery", e)
	}
}

func TestHubAllocationReceiptAuditRollbackReplayAndBootBinding(t *testing.T) {
	x, in, _ := fundedHubFixture(t)
	a, e := x.s.PrepareHubAllocation(context.Background(), x.f.scope, in, uuid.NewString())
	if e != nil {
		t.Fatal(e)
	}
	ctx := x.f.contexts[in.HubID]
	q := generated.New(x.f.pool)
	r, e := q.GetHubAllocationGlobal(ctx, a.AllocationID)
	if e != nil {
		t.Fatal(e)
	}
	request := uuid.NewString()
	failAudit(t, x.f.pool, request)
	tables := []string{"hub_allocation_receipts", "reservations", "ledger_projections", "ledger_events", "tasks", "outbox", "operations", "audit_events"}
	before := authoritySnapshot(t, x.f.pool, x.f.tenant, tables)
	if e = x.s.RecordHubAllocationReceipt(ctx, in.HubID, in.HubBootID, a.AllocationID, r.PayloadDigest, request); e == nil {
		t.Fatal("receipt escaped audit")
	}
	if !bytes.Equal(before, authoritySnapshot(t, x.f.pool, x.f.tenant, tables)) {
		t.Fatal("partial receipt effects")
	}
	if e = x.s.RecordHubAllocationReceipt(ctx, in.HubID, uuid.NewString(), a.AllocationID, r.PayloadDigest, uuid.NewString()); !errors.Is(e, service.ErrShareConflict) {
		t.Fatal("other boot receipt", e)
	}
	for i := 0; i < 2; i++ {
		if e = x.s.RecordHubAllocationReceipt(ctx, in.HubID, in.HubBootID, a.AllocationID, r.PayloadDigest, uuid.NewString()); e != nil {
			t.Fatal(e)
		}
	}
	op, e := q.GetOperation(ctx, generated.GetOperationParams{TenantID: x.f.tenant, ID: r.OperationID})
	if e != nil || op.State != "succeeded" || !op.OccupationHeld {
		t.Fatal("installation operation did not preserve financial occupation", e, op)
	}
	rows, e := x.s.PendingHubAllocations(ctx, in.HubID, in.HubBootID)
	if e != nil || len(rows) != 0 {
		t.Fatal("receipt not persisted", e, rows)
	}
	bad := in
	bad.Nonce = uuid.NewString()
	bad.HubBootID = uuid.NewString()
	if _, e = x.s.PrepareHubAllocation(ctx, x.f.scope, bad, uuid.NewString()); !errors.Is(e, service.ErrShareUnknown) {
		t.Fatal("other boot allocation funded", e)
	}
}

func TestHubBootConcurrentFirstSessionsBindOneIncarnation(t *testing.T) {
	x := newShareFixture(t)
	id := x.in.SignerID
	start := make(chan struct{})
	results := make(chan error, 2)
	for i := 0; i < 2; i++ {
		go func(boot string) {
			<-start
			_, e := x.s.BindHubSession(x.f.contexts[id], id, boot, uuid.NewString())
			results <- e
		}(uuid.NewString())
	}
	close(start)
	accepted, blocked := 0, 0
	for i := 0; i < 2; i++ {
		e := <-results
		if e == nil {
			accepted++
		} else if errors.Is(e, service.ErrShareUnknown) {
			blocked++
		} else {
			t.Fatal(e)
		}
	}
	if accepted != 1 || blocked != 1 {
		t.Fatal("two first boots acquired authority", accepted, blocked)
	}
}
