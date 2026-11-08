package service_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"

	"github.com/google/uuid"
	"vortex/agent/store"
	"vortex/backend/db/generated"
	"vortex/backend/internal/service"
	"vortex/contracts"
)

func TestHubFundingIndependentKindsAndExactHistoricalClosure(t *testing.T) {
	x, in, hs := fundedHubFixture(t)
	ctx := context.Background()
	q := generated.New(x.f.pool)
	first, err := x.s.PrepareHubAllocation(ctx, x.f.scope, in, uuid.NewString())
	if err != nil {
		t.Fatal(err)
	}
	original := x.in
	x.in.Kind, x.in.ExpectedGeneration, x.in.Amount, x.in.BurstBytes = "connections", 1, 3, 0
	x.in.Nonce = uuid.NewString()
	next := x.prepare(t)
	if err = x.s.PrepareRenewalClosures(ctx, x.f.scope, next.Generation.ID, shareBackendID, uuid.NewString()); err != nil {
		t.Fatal(err)
	}
	commands, err := q.ShareClosureCommands(ctx, generated.ShareClosureCommandsParams{TenantID: x.f.tenant, HandoffID: next.Generation.ID, SignerID: in.HubID})
	if err != nil || len(commands) != 1 {
		t.Fatal(err, commands)
	}
	receipt, err := hs.ApplyShareRenewalClosure(ctx, in.HubID, in.HubBootID, shareBackendID, commands[0].Payload)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(receipt)
	if err = x.s.RecordRenewalClosureReceipt(x.f.contexts[in.HubID], in.HubID, in.HubBootID, raw, uuid.NewString()); err != nil {
		t.Fatal(err)
	}
	if err = x.s.QueueHandoff(ctx, x.f.scope, next.Generation.ID, uuid.NewString()); err != nil {
		t.Fatal(err)
	}
	node, err := store.Open(filepath.Join(t.TempDir(), "connections.db"), "agent")
	if err != nil {
		t.Fatal(err)
	}
	defer node.Close()
	bootstrap, err := q.ShareHandoffByGeneration(ctx, generated.ShareHandoffByGenerationParams{TenantID: x.f.tenant, NodeID: in.NodeID, Kind: "connections", Generation: 1})
	if err != nil {
		t.Fatal(err)
	}
	clock := store.IssuerTime{Earliest: x.now, Latest: x.now, Trusted: true}
	if _, err = node.ApplyShareHandoff(ctx, in.NodeID, in.BootID, shareBackendID, "cas", bootstrap.Command, clock); err != nil {
		t.Fatal(err)
	}
	confirmHandoff(t, x, node, next.Generation.ID)

	// Rate remains generation1 while connections advances independently to2.
	in.Nonce = uuid.NewString()
	request := uuid.NewString()
	failAudit(t, x.f.pool, request)
	tables := []string{"hub_funded_allocations", "hub_allocation_share_bindings", "reservations", "ledger_events", "ledger_projections", "operations", "tasks", "outbox", "audit_events"}
	before := authoritySnapshot(t, x.f.pool, x.f.tenant, tables)
	if _, err = x.s.PrepareHubAllocation(ctx, x.f.scope, in, request); err == nil {
		t.Fatal("independent bindings escaped funding audit rollback")
	}
	if !bytes.Equal(before, authoritySnapshot(t, x.f.pool, x.f.tenant, tables)) {
		t.Fatal("partial funding or generation dependency")
	}
	second, err := x.s.PrepareHubAllocation(ctx, x.f.scope, in, uuid.NewString())
	if err != nil {
		t.Fatal(err)
	}
	bindings, err := q.HubAllocationShareBindings(ctx, generated.HubAllocationShareBindingsParams{TenantID: x.f.tenant, AllocationID: second.AllocationID})
	if err != nil || len(bindings) != 2 || bindings[0].Kind != "connections" || bindings[0].Generation != 2 || bindings[1].Kind != "rate" || bindings[1].Generation != 1 {
		t.Fatal("funding merged independent generations", err, bindings)
	}
	for _, ref := range second.ShareReferences {
		if ref.Generation != 1 {
			t.Fatal("fresh source root inherited a Node aggregate generation", ref)
		}
	}
	installAllocationFixture(t, hs, second, x.now)
	issued, _, sequence := issueAllocationFixture(t, x, hs, second, 1)
	if err = x.s.RecordHubIssue(x.f.contexts[in.HubID], in.HubID, in.HubBootID, sequence, issued.Payload, uuid.NewString()); err != nil {
		t.Fatal("real funded issue rejected divergent kinds", err)
	}
	if _, err = x.f.pool.Exec(ctx, `UPDATE hub_allocation_share_bindings SET generation=1 WHERE tenant_id=$1 AND allocation_id=$2`, x.f.tenant, second.AllocationID); err == nil {
		t.Fatal("funding dependency history was mutable")
	}

	x.in.ExpectedGeneration, x.in.Amount, x.in.Nonce = 2, 2, uuid.NewString()
	connectionThird := x.prepare(t)
	if err = x.s.PrepareRenewalClosures(ctx, x.f.scope, connectionThird.Generation.ID, shareBackendID, uuid.NewString()); err != nil {
		t.Fatal(err)
	}
	commands, err = q.ShareClosureCommands(ctx, generated.ShareClosureCommandsParams{TenantID: x.f.tenant, HandoffID: connectionThird.Generation.ID, SignerID: in.HubID})
	if err != nil || len(commands) != 1 {
		t.Fatal(err, commands)
	}
	closed, err := contracts.DecodeShareRenewalClosure(commands[0].Payload)
	if err != nil || closed.Kind != "connections" || closed.Generation != 2 || len(closed.DelegationIDs) != 1 || closed.DelegationIDs[0] != second.DelegationID {
		t.Fatal("connection2 closure included historical connection1", err, closed)
	}
	x.in = original
	rateNext := prepareClosureSuccessor(t, x)
	if err = x.s.PrepareRenewalClosures(ctx, x.f.scope, rateNext.Generation.ID, shareBackendID, uuid.NewString()); err != nil {
		t.Fatal(err)
	}
	commands, err = q.ShareClosureCommands(ctx, generated.ShareClosureCommandsParams{TenantID: x.f.tenant, HandoffID: rateNext.Generation.ID, SignerID: in.HubID})
	if err != nil || len(commands) != 1 {
		t.Fatal(err, commands)
	}
	closed, err = contracts.DecodeShareRenewalClosure(commands[0].Payload)
	if err != nil || closed.Kind != "rate" || closed.Generation != 1 || len(closed.DelegationIDs) != 2 {
		t.Fatal("rate1 closure lost funding under connection2", err, closed)
	}
	ids := map[string]bool{first.DelegationID: true, second.DelegationID: true}
	for _, id := range closed.DelegationIDs {
		if !ids[id] {
			t.Fatal("closure substituted delegation", id)
		}
		delete(ids, id)
	}
}

func TestHubFundingMissingKindBindingCannotProveClosure(t *testing.T) {
	x, in, _ := fundedHubFixture(t)
	ctx := context.Background()
	if _, err := x.s.PrepareHubAllocation(ctx, x.f.scope, in, uuid.NewString()); err != nil {
		t.Fatal(err)
	}
	// Model a historical candidate DB without a complete dependency proof. This
	// fixture alone removes one immutable row; production has no such mutation.
	if _, err := x.f.pool.Exec(ctx, `ALTER TABLE hub_allocation_share_bindings DISABLE TRIGGER hub_allocation_share_binding_immutable;
DELETE FROM hub_allocation_share_bindings WHERE kind='connections';
ALTER TABLE hub_allocation_share_bindings ENABLE TRIGGER hub_allocation_share_binding_immutable;`); err != nil {
		t.Fatal(err)
	}
	next := prepareClosureSuccessor(t, x)
	tables := []string{"share_renewal_closure_commands", "share_renewal_authorities", "ledger_projections", "operations", "tasks", "outbox", "audit_events"}
	before := authoritySnapshot(t, x.f.pool, x.f.tenant, tables)
	if err := x.s.PrepareRenewalClosures(ctx, x.f.scope, next.Generation.ID, shareBackendID, uuid.NewString()); !errors.Is(err, service.ErrShareUnknown) {
		t.Fatal("missing independent kind treated as zero delegations", err)
	}
	if !bytes.Equal(before, authoritySnapshot(t, x.f.pool, x.f.tenant, tables)) {
		t.Fatal("missing binding produced closure or released funds")
	}
}
