package service_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"sync"
	"testing"

	"github.com/google/uuid"
	"vortex/agent/store"
	"vortex/backend/db/generated"
	"vortex/backend/internal/service"
	"vortex/backend/internal/worker"
	"vortex/contracts"
)

func prepareClosureSuccessor(t *testing.T, x *shareFixture) service.PreparedShare {
	t.Helper()
	x.in.ExpectedGeneration = 1
	x.in.Nonce = uuid.NewString()
	x.in.Amount = 300000
	x.in.BurstBytes = 37500
	return x.prepare(t)
}
func TestShareRenewalClosureCompletePagesAuditRollbackAndCASOnlyRelease(t *testing.T) {
	x, in, hs := fundedHubFixture(t)
	ctx := context.Background()
	q := generated.New(x.f.pool)
	// More than one page, including undelivered delegations. Only one is locally installed.
	ids := map[string]bool{}
	for i := 0; i < 65; i++ {
		in.Nonce = uuid.NewString()
		in.RawBytes = 1
		a, e := x.s.PrepareHubAllocation(ctx, x.f.scope, in, uuid.NewString())
		if e != nil {
			t.Fatal(e)
		}
		ids[a.DelegationID] = true
		if i == 0 {
			installAllocationFixture(t, hs, a, x.now)
		}
	}
	// A genuine pre-barrier renewal can replay later, but no new renewal is minted.
	renewal := x.renewal()
	old, e := x.s.RenewShare(x.f.contexts[in.NodeID], x.f.scope, renewal, uuid.NewString())
	if e != nil {
		t.Fatal(e)
	}
	next := prepareClosureSuccessor(t, x)
	request := uuid.NewString()
	failAudit(t, x.f.pool, request)
	tables := []string{"share_renewal_closure_commands", "share_renewal_closure_receipts", "share_renewal_authorities", "operations", "tasks", "outbox", "audit_events"}
	before := authoritySnapshot(t, x.f.pool, x.f.tenant, tables)
	if e = x.s.PrepareRenewalClosures(ctx, x.f.scope, next.Generation.ID, shareBackendID, request); e == nil {
		t.Fatal("barrier escaped audit rollback")
	}
	if !bytes.Equal(before, authoritySnapshot(t, x.f.pool, x.f.tenant, tables)) {
		t.Fatal("partial barrier/task")
	}
	// Two producers race for the same immutable snapshot under the tenant lock.
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if e := x.s.PrepareRenewalClosures(ctx, x.f.scope, next.Generation.ID, shareBackendID, uuid.NewString()); e != nil {
				t.Error(e)
			}
		}()
	}
	wg.Wait()
	commands, e := q.ShareClosureCommands(ctx, generated.ShareClosureCommandsParams{TenantID: x.f.tenant, HandoffID: next.Generation.ID, SignerID: in.HubID})
	if e != nil || len(commands) != 2 {
		t.Fatal(e, len(commands))
	}
	for i, c := range commands {
		decoded, e := contracts.DecodeShareRenewalClosure(c.Payload)
		if e != nil || decoded.Kind != "rate" || decoded.Generation != 1 || decoded.SuccessorGeneration != 2 || decoded.Part != int32(i+1) || decoded.Parts != 2 {
			t.Fatal(e, decoded)
		}
		for _, id := range decoded.DelegationIDs {
			if !ids[id] {
				t.Fatal("missing/duplicate snapshot", id)
			}
			delete(ids, id)
		}
	}
	if len(ids) != 0 {
		t.Fatal("truncated closure pages")
	}
	replay, e := x.s.RenewShare(x.f.contexts[in.NodeID], x.f.scope, renewal, uuid.NewString())
	if e != nil || replay.ID != old.ID {
		t.Fatal("nonce replay changed after barrier", e)
	}
	renewal.Nonce = uuid.NewString()
	if _, e = x.s.RenewShare(x.f.contexts[in.NodeID], x.f.scope, renewal, uuid.NewString()); !errors.Is(e, service.ErrShareConflict) {
		t.Fatal("new renewal after barrier", e)
	}
	in.Nonce = uuid.NewString()
	if _, e = x.s.PrepareHubAllocation(ctx, x.f.scope, in, uuid.NewString()); e == nil {
		t.Fatal("old generation funded after snapshot")
	}
	pending, e := x.s.PendingHubAllocations(x.f.contexts[in.HubID], in.HubID, in.HubBootID)
	if e != nil || len(pending) != 0 {
		t.Fatal("closed late installs still delivered", e, len(pending))
	}
	var receipt []byte
	for i, c := range commands {
		r, e := hs.ApplyShareRenewalClosure(ctx, in.HubID, in.HubBootID, shareBackendID, c.Payload)
		if e != nil {
			t.Fatal(e)
		}
		receipt, _ = json.Marshal(r)
		if i == len(commands)-1 {
			before = authoritySnapshot(t, x.f.pool, x.f.tenant, tables)
			if e = x.s.RecordRenewalClosureReceipt(x.f.contexts[in.HubID], in.HubID, in.HubBootID, receipt, request); e == nil {
				t.Fatal("receipt escaped audit rollback")
			}
			if !bytes.Equal(before, authoritySnapshot(t, x.f.pool, x.f.tenant, tables)) {
				t.Fatal("partial receipt/authority closure")
			}
		}
		if e = x.s.RecordRenewalClosureReceipt(x.f.contexts[in.HubID], in.HubID, in.HubBootID, receipt, uuid.NewString()); e != nil {
			t.Fatal(e)
		}
		if i == 0 {
			if e = x.s.QueueHandoff(ctx, x.f.scope, next.Generation.ID, uuid.NewString()); !errors.Is(e, service.ErrShareUnknown) {
				t.Fatal("partial pages allowed CAS", e)
			}
		}
	}
	if shareChain(t, x).HeldAmount != 600000 {
		t.Fatal("issuer closure released capacity")
	}
	if e = x.s.QueueHandoff(ctx, x.f.scope, next.Generation.ID, uuid.NewString()); e != nil {
		t.Fatal(e)
	}
	// This test's Node previously confirmed both first-generation aggregates.
	node, e := store.Open(filepath.Join(t.TempDir(), "successor-node.db"), "agent")
	if e != nil {
		t.Fatal(e)
	}
	defer node.Close()
	// Reconstruct the exact first-generation command at the real Node store.
	h, e := q.ShareHandoffByGeneration(ctx, generated.ShareHandoffByGenerationParams{TenantID: x.f.tenant, NodeID: in.NodeID, Kind: "rate", Generation: 1})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = node.ApplyShareHandoff(ctx, in.NodeID, in.BootID, shareBackendID, "cas", h.Command, store.IssuerTime{Earliest: x.now, Latest: x.now, Trusted: true}); e != nil {
		t.Fatal(e)
	}
	confirmHandoff(t, x, node, next.Generation.ID)
	if shareChain(t, x).HeldAmount != 300000 {
		t.Fatal("real Node receipt did not release exact difference")
	}
	// A lost PG ACK arriving after the chain advanced replays the old receipt.
	if e = x.s.RecordRenewalClosureReceipt(x.f.contexts[in.HubID], in.HubID, in.HubBootID, receipt, uuid.NewString()); e != nil {
		t.Fatal("late exact receipt replay failed", e)
	}
	a, e := q.ShareRenewalAuthority(ctx, generated.ShareRenewalAuthorityParams{TenantID: x.f.tenant, NodeID: in.NodeID, Kind: "connections", Generation: 1, SignerID: in.HubID})
	if e != nil || a.ClosedAt.Valid {
		t.Fatal("rate closure closed independent kind", e)
	}
}

func TestShareRenewalClosureWorkerEmptyPageAndWrongIdentity(t *testing.T) {
	x, in, hs := fundedHubFixture(t)
	ctx := context.Background()
	next := prepareClosureSuccessor(t, x)
	w := worker.ShareRenewalClosureWorker{Service: x.s, BackendID: shareBackendID}
	if did, e := w.Once(ctx); e != nil || !did {
		t.Fatal(e, did)
	}
	rows, e := x.s.PendingRenewalClosures(x.f.contexts[in.HubID], in.HubID, in.HubBootID)
	if e != nil || len(rows) != 1 {
		t.Fatal(e, len(rows))
	}
	c, e := contracts.DecodeShareRenewalClosure(rows[0].Payload)
	if e != nil || len(c.DelegationIDs) != 0 {
		t.Fatal("missing explicit empty statement", e)
	}
	r, e := hs.ApplyShareRenewalClosure(ctx, in.HubID, in.HubBootID, shareBackendID, rows[0].Payload)
	if e != nil {
		t.Fatal(e)
	}
	raw, _ := json.Marshal(r)
	if e = x.s.RecordRenewalClosureReceipt(ctx, in.HubID, in.HubBootID, raw, uuid.NewString()); !errors.Is(e, service.ErrScope) {
		t.Fatal("unsigned receipt", e)
	}
	if e = x.s.RecordRenewalClosureReceipt(x.f.contexts[in.HubID], in.HubID, uuid.NewString(), raw, uuid.NewString()); !errors.Is(e, service.ErrShareConflict) {
		t.Fatal("wrong boot receipt", e)
	}
	r.CommandDigest = contracts.SharePayloadDigest([]byte("wrong command"))
	bad, _ := json.Marshal(r)
	if e = x.s.RecordRenewalClosureReceipt(x.f.contexts[in.HubID], in.HubID, in.HubBootID, bad, uuid.NewString()); !errors.Is(e, service.ErrShareConflict) {
		t.Fatal("wrong command digest", e)
	}
	if e = x.s.RecordRenewalClosureReceipt(x.f.contexts[in.HubID], in.HubID, in.HubBootID, raw, uuid.NewString()); e != nil {
		t.Fatal(e)
	}
	if e = x.s.QueueHandoff(ctx, x.f.scope, next.Generation.ID, uuid.NewString()); e != nil {
		t.Fatal(e)
	}
}
