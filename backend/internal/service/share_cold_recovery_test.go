package service_test

import (
	"bytes"
	"context"
	"encoding/json"
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

func TestColdHubProofClosesOnlyRenewalAndRealNodeCASReleasesCapacity(t *testing.T) {
	x, in, hs := fundedHubFixture(t)
	ctx := context.Background()
	q := generated.New(x.f.pool)
	a, e := x.s.PrepareHubAllocation(ctx, x.f.scope, in, uuid.NewString())
	if e != nil {
		t.Fatal(e)
	}
	installAllocationFixture(t, hs, a, x.now)
	issued, _, seq := issueAllocationFixture(t, x, hs, a, 37)
	if e = x.s.RecordHubIssue(x.f.contexts[in.HubID], in.HubID, in.HubBootID, seq, issued.Payload, uuid.NewString()); e != nil {
		t.Fatal(e)
	}
	node, e := store.Open(t.TempDir()+"/existing-node.db", "agent")
	if e != nil {
		t.Fatal(e)
	}
	defer node.Close()
	for _, kind := range []string{"rate", "connections"} {
		h, e := q.ShareHandoffByGeneration(ctx, generated.ShareHandoffByGenerationParams{TenantID: x.f.tenant, NodeID: in.NodeID, Kind: kind, Generation: 1})
		if e != nil {
			t.Fatal(e)
		}
		if _, e = node.ApplyShareHandoff(ctx, in.NodeID, in.BootID, shareBackendID, "cas", h.Command, store.IssuerTime{Earliest: x.now.Add(time.Second), Latest: x.now.Add(time.Second), Trusted: true}); e != nil {
			t.Fatal(e)
		}
	}
	// Current policy remains independent of expired signer authority. Extend
	// the test grant via the real versioned platform service before recovery.
	if e = x.f.nodeService.SetGrant(ctx, policyScope(t, x.f.tenant, auth.PlatformAdmin, policy.Grant), x.f.group, 1, "active", x.now.Add(-time.Hour), x.now.Add(3*time.Hour), uuid.NewString()); e != nil {
		t.Fatal(e)
	}
	r := coldRecoveryInput(t, x, in.HubID, in.HubBootID)
	x.now = x.now.Add(time.Hour)
	// Successors can be prepared, but approval alone does not close the old Hub.
	pending := []service.PreparedShare{}
	for _, kind := range []string{"rate", "connections"} {
		x.in.Kind = kind
		x.in.ExpectedGeneration = 1
		x.in.SignerEpoch = 8
		x.in.Nonce = uuid.NewString()
		x.in.OwnershipUntil = x.now.Add(10 * time.Minute)
		x.in.PolicyUntil = x.now.Add(10 * time.Minute)
		x.in.Amount = 300000
		x.in.BurstBytes = 37500
		if kind == "connections" {
			x.in.Amount = 2
			x.in.BurstBytes = 0
		}
		pending = append(pending, x.prepare(t))
	}
	if _, e = (worker.ShareRenewalClosureWorker{Service: x.s, BackendID: shareBackendID}).Once(ctx); e != nil {
		t.Fatal(e)
	}
	for _, p := range pending {
		if e = x.s.QueueHandoff(ctx, x.f.scope, p.Generation.ID, uuid.NewString()); !errors.Is(e, service.ErrShareUnknown) {
			t.Fatal("unproved cold Hub closed", e)
		}
	}
	// The complete platform proof must include the original generation1 rights.
	if _, e = x.s.RecoverColdHubBoot(ctx, policyScope(t, x.f.tenant, auth.PlatformAdmin, policy.Grant), r, uuid.NewString()); e != nil {
		t.Fatal(e)
	}
	financial := []string{"reservations", "ledger_projections", "ledger_events", "hub_credit_grants", "settlements"}
	held := authoritySnapshot(t, x.f.pool, x.f.tenant, financial)
	request := uuid.NewString()
	failAudit(t, x.f.pool, request)
	before := authoritySnapshot(t, x.f.pool, x.f.tenant, []string{"share_renewal_authorities", "share_chains", "tasks", "outbox", "operations", "audit_events"})
	if e = x.s.PrepareRenewalClosures(ctx, x.f.scope, pending[0].Generation.ID, shareBackendID, request); e == nil {
		t.Fatal("cold closure escaped audit")
	}
	if !bytes.Equal(before, authoritySnapshot(t, x.f.pool, x.f.tenant, []string{"share_renewal_authorities", "share_chains", "tasks", "outbox", "operations", "audit_events"})) {
		t.Fatal("partial cold closure")
	}
	var wg sync.WaitGroup
	errs := make(chan error, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, e := (worker.ShareRenewalClosureWorker{Service: x.s, BackendID: shareBackendID}).Once(ctx)
			errs <- e
		}()
	}
	wg.Wait()
	close(errs)
	for e := range errs {
		if e != nil {
			t.Fatal(e)
		}
	}
	var fakeReceipts, retired int
	if e = x.f.pool.QueryRow(ctx, `SELECT count(*) FROM share_renewal_closure_receipts`).Scan(&fakeReceipts); e != nil || fakeReceipts != 0 {
		t.Fatal("cold proof fabricated Hub receipts", fakeReceipts, e)
	}
	if e = x.f.pool.QueryRow(ctx, `SELECT count(*) FROM share_renewal_closure_commands c JOIN tasks t ON t.tenant_id=c.tenant_id AND t.operation_id=c.operation_id JOIN operations o ON o.tenant_id=c.tenant_id AND o.id=c.operation_id JOIN outbox b ON b.tenant_id=c.tenant_id AND b.operation_id=c.operation_id WHERE c.tenant_id=$1 AND t.state='failed' AND o.state='failed' AND b.delivered_at IS NOT NULL`, x.f.tenant).Scan(&retired); e != nil || retired != 2 {
		t.Fatal("orphaned live Hub closure work", retired, e)
	}
	for _, p := range pending {
		chain, e := q.ReadShareChain(ctx, generated.ReadShareChainParams{TenantID: x.f.tenant, NodeID: in.NodeID, Kind: p.Generation.Kind})
		if e != nil {
			t.Fatal(e)
		}
		expected := int64(600000)
		if p.Generation.Kind == "connections" {
			expected = 4
		}
		if chain.HeldAmount != expected || chain.ActiveGeneration != 1 {
			t.Fatal("closure released capacity before Node CAS", chain)
		}
		if e = x.s.QueueHandoff(ctx, x.f.scope, p.Generation.ID, uuid.NewString()); e != nil {
			t.Fatal(e)
		}
		x.in.Kind = p.Generation.Kind
		observation := confirmHandoff(t, x, node, p.Generation.ID)
		if observation.MinimumGeneration != 2 || observation.ActiveGeneration != 2 || observation.State != "enforced" {
			t.Fatal(observation)
		}
		chain, e = q.ReadShareChain(ctx, generated.ReadShareChainParams{TenantID: x.f.tenant, NodeID: in.NodeID, Kind: p.Generation.Kind})
		if e != nil || chain.HeldAmount != p.Generation.Amount || chain.HeadGeneration != 2 || chain.ActiveGeneration != 2 {
			t.Fatal("Node CAS did not release exact difference", chain, e)
		}
	}
	if !bytes.Equal(held, authoritySnapshot(t, x.f.pool, x.f.tenant, financial)) {
		t.Fatal("capacity recovery released child financial liability")
	}
	// Independently prepare a fresh rule under the approved epoch; a pending
	// original deployment keeps its own immutable port/configuration ownership.
	oldRule, e := q.GetRuleRevision(ctx, generated.GetRuleRevisionParams{TenantID: x.f.tenant, RuleID: a.RuleID, Revision: int64(a.RuleRevision)})
	if e != nil {
		t.Fatal(e)
	}
	var desired service.RuleInput
	if e = json.Unmarshal(oldRule.Payload, &desired); e != nil {
		t.Fatal(e)
	}
	desired.ID = uuid.NewString()
	desired.Port++
	reserveRulePort(t, x.f, desired)
	if _, e = (service.RuleService{Pool: x.f.pool}).Mutate(ctx, x.f.scope, desired, 0, uuid.NewString()); e != nil {
		t.Fatal(e)
	}
	newBilling := durableBilling(t, x.f, desired.ID, 1, int64(a.PathGeneration))
	prepareAllocationRuleEpoch(t, x, desired.ID, newBilling.ID, 8)
	next := in
	next.SnapshotID = newBilling.ID
	next.SignerEpoch = 8
	next.HubBootID = r.ToBootID
	next.Nonce = uuid.NewString()
	next.ExpiresAt = x.now.Add(5 * time.Minute)
	funded, e := x.s.PrepareHubAllocation(ctx, x.f.scope, next, uuid.NewString())
	if e != nil {
		t.Fatal("fresh epoch funding after real CAS", e)
	}
	if funded.SourceID == a.SourceID {
		t.Fatal("cold recovery reused old financial parent")
	}
	fresh, e := store.Open(t.TempDir()+"/recovered-hub.db", "hub")
	if e != nil {
		t.Fatal(e)
	}
	defer fresh.Close()
	installAllocationFixture(t, fresh, funded, x.now)
	newIssue, _, newSeq := issueAllocationFixture(t, x, fresh, funded, 37)
	if newSeq != 1 || seq != 1 {
		t.Fatal("fixture must restart local journal numbering", seq, newSeq)
	}
	if e = x.s.RecordHubIssue(x.f.contexts[in.HubID], in.HubID, r.ToBootID, newSeq, newIssue.Payload, uuid.NewString()); e != nil {
		t.Fatal("fresh boot sequence collided with original", e)
	}
	if e = x.s.RecordHubIssue(x.f.contexts[in.HubID], in.HubID, in.HubBootID, seq, issued.Payload, uuid.NewString()); e != nil {
		t.Fatal("old immutable issue replay lost", e)
	}
	if e = x.s.RecordHubIssue(x.f.contexts[in.HubID], in.HubID, r.ToBootID, seq, issued.Payload, uuid.NewString()); !errors.Is(e, service.ErrScope) {
		t.Fatal("fresh boot relabeled original issue", e)
	}
	projection, e := q.GetSourceProjection(ctx, generated.GetSourceProjectionParams{TenantID: x.f.tenant, SourceID: in.SourceID})
	if e != nil || projection.F != 206 || projection.Charged != 0 {
		t.Fatal("fresh source hold changed original debt", projection, e)
	}
}
