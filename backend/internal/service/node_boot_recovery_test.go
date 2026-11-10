package service_test

import (
	"bytes"
	"context"
	"crypto/sha256"
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
	"vortex/contracts"
	controlv1 "vortex/contracts/protobuf/v1"
)

func nodeColdFixture(t *testing.T) (*shareFixture, service.NodeBootRecoveryInput, *store.Store) {
	t.Helper()
	return nodeColdFixtureWithPrior(t, nil)
}
func nodeColdFixtureWithPrior(t *testing.T, prior func(*shareFixture)) (*shareFixture, service.NodeBootRecoveryInput, *store.Store) {
	t.Helper()
	x, in, hs := fundedHubFixture(t)
	ctx := context.Background()
	q := generated.New(x.f.pool)
	a, e := x.s.PrepareHubAllocation(ctx, x.f.scope, in, uuid.NewString())
	if e != nil {
		t.Fatal(e)
	}
	installAllocationFixture(t, hs, a, x.now)
	issue, _, seq := issueAllocationFixture(t, x, hs, a, 37)
	if e = x.s.RecordHubIssue(x.f.contexts[in.HubID], in.HubID, in.HubBootID, seq, issue.Payload, uuid.NewString()); e != nil {
		t.Fatal(e)
	}
	st, e := store.Open(t.TempDir()+"/retained.db", "agent")
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { st.Close() })
	for _, kind := range []string{"rate", "connections"} {
		h, e := q.ShareHandoffByGeneration(ctx, generated.ShareHandoffByGenerationParams{TenantID: x.f.tenant, NodeID: in.NodeID, Kind: kind, Generation: 1})
		if e != nil {
			t.Fatal(e)
		}
		if _, e = st.ApplyShareHandoff(ctx, in.NodeID, in.BootID, shareBackendID, "cas", h.Command, store.IssuerTime{Earliest: x.now.Add(time.Second), Latest: x.now.Add(time.Second), Trusted: true}); e != nil {
			t.Fatal(e)
		}
	}
	// Independent heads: actual Node FULL rate2 while connections stays1.
	x.in.ExpectedGeneration = 1
	x.in.Nonce = uuid.NewString()
	x.in.Amount = 500000
	x.in.BurstBytes = 62500
	next := x.prepare(t)
	closePredecessor(t, x)
	if e = x.s.QueueHandoff(ctx, x.f.scope, next.Generation.ID, uuid.NewString()); e != nil {
		t.Fatal(e)
	}
	confirmHandoff(t, x, st, next.Generation.ID)
	if _, e = x.s.RenewShare(x.f.contexts[in.NodeID], x.f.scope, service.LeaseRequest{NodeID: in.NodeID, BootID: in.BootID, Kind: "rate", Nonce: uuid.NewString(), ConnectionID: uuid.NewString(), SignerID: in.HubID, Generation: 2, SignerEpoch: 7, PathGeneration: 1, FencingGeneration: 3, RequestedAt: x.now}, uuid.NewString()); e != nil {
		t.Fatal(e)
	}
	proof := sha256.Sum256([]byte("old Node execution unknown after disk/process failure"))
	if e = (service.MeteringService{Pool: x.f.pool}).RecordUnknown(ctx, x.f.scope, a.SourceID, proof[:], x.now.Add(time.Hour), uuid.NewString()); e != nil {
		t.Fatal(e)
	}
	if prior != nil {
		prior(x)
	}
	input := service.NodeBootRecoveryInput{ID: uuid.NewString(), NodeID: in.NodeID, FromBootID: in.BootID, ToBootID: uuid.NewString(), BackendID: shareBackendID, MinimumFencing: 4, Reason: "replace failed Node after complete immutable authority boundaries"}
	if e = x.f.ports.RegisterNodeSession(x.f.contexts[in.NodeID], policyScope(t, x.f.tenant, auth.PlatformAdmin, policy.Mutate), in.NodeID, input.ToBootID, 2, uuid.NewString()); e != nil {
		t.Fatal("register new incarnation", e)
	}
	return x, input, st
}

func TestNodeBootRecoveryProofFullReceiptPreservesFloorsAndDebtUntilRealCAS(t *testing.T) {
	x, in, st := nodeColdFixture(t)
	ctx := context.Background()
	q := generated.New(x.f.pool)
	admin := policyScope(t, x.f.tenant, auth.PlatformAdmin, policy.Grant)
	financial := []string{"reservations", "ledger_projections", "ledger_events", "hub_credit_grants", "settlements"}
	held := authoritySnapshot(t, x.f.pool, x.f.tenant, financial)
	if _, e := x.s.PrepareNodeBootRecovery(ctx, x.f.scope, in, uuid.NewString()); !errors.Is(e, service.ErrScope) {
		t.Fatal("tenant approval", e)
	}
	if _, e := x.s.PrepareNodeBootRecovery(ctx, admin, in, uuid.NewString()); !errors.Is(e, service.ErrShareUnknown) {
		t.Fatal("live authority", e)
	}
	base := x.now
	x.now = base.Add(time.Hour - time.Microsecond)
	if _, e := x.s.PrepareNodeBootRecovery(ctx, admin, in, uuid.NewString()); !errors.Is(e, service.ErrShareUnknown) {
		t.Fatal("early lower", e)
	}
	x.now = base.Add(time.Hour)
	request := uuid.NewString()
	failAudit(t, x.f.pool, request)
	tables := []string{"share_chains", "node_fencing", "node_boot_recoveries", "node_boot_recovery_receipts", "tasks", "operations", "outbox", "audit_events"}
	before := authoritySnapshot(t, x.f.pool, x.f.tenant, tables)
	if _, e := x.s.PrepareNodeBootRecovery(ctx, admin, in, request); e == nil {
		t.Fatal("approval escaped audit")
	}
	if !bytes.Equal(before, authoritySnapshot(t, x.f.pool, x.f.tenant, tables)) {
		t.Fatal("partial approval")
	}
	p, e := x.s.PrepareNodeBootRecovery(ctx, admin, in, uuid.NewString())
	if e != nil {
		t.Fatal(e)
	}
	c, e := contracts.DecodeNodeBootRecovery(p.Payload)
	if e != nil || c.Previous[0].Generation != 2 || c.Previous[1].Generation != 1 || !c.SafeAfter.Equal(x.now) {
		t.Fatal(c, e)
	}
	var proof struct {
		Shares         []any
		Allocations    []any
		Leases         []any
		Configurations []any
	}
	if json.Unmarshal(p.Proof, &proof) != nil || len(proof.Shares) != 3 || len(proof.Allocations) != 1 || len(proof.Leases) != 1 || len(proof.Configurations) == 0 {
		t.Fatal("incomplete inventory", string(p.Proof))
	}
	for _, kind := range []string{"rate", "connections"} {
		chain, e := q.ReadShareChain(ctx, generated.ReadShareChainParams{TenantID: x.f.tenant, NodeID: in.NodeID, Kind: kind})
		if e != nil || chain.BootID != in.FromBootID || chain.ActiveGeneration == 0 {
			t.Fatal("prepare migrated boot", chain, e)
		}
	}
	r, e := st.RetireNodeBootShares(ctx, in.NodeID, in.ToBootID, in.BackendID, p.Payload, store.IssuerTime{Earliest: x.now, Latest: x.now, Trusted: true})
	if e != nil {
		t.Fatal(e)
	}
	raw, _ := json.Marshal(r)
	if e = x.s.RecordNodeBootRecovery(ctx, x.f.scope, raw, uuid.NewString()); !errors.Is(e, service.ErrScope) {
		t.Fatal("unsigned receipt", e)
	}
	bad := r
	bad.MinimumFencing++
	wrong, _ := json.Marshal(bad)
	if e = x.s.RecordNodeBootRecovery(x.f.contexts[in.NodeID], x.f.scope, wrong, uuid.NewString()); !errors.Is(e, service.ErrStaleReceipt) {
		t.Fatal("changed receipt", e)
	}
	request = uuid.NewString()
	failAudit(t, x.f.pool, request)
	before = authoritySnapshot(t, x.f.pool, x.f.tenant, tables)
	if e = x.s.RecordNodeBootRecovery(x.f.contexts[in.NodeID], x.f.scope, raw, request); e == nil {
		t.Fatal("receipt escaped audit")
	}
	if !bytes.Equal(before, authoritySnapshot(t, x.f.pool, x.f.tenant, tables)) {
		t.Fatal("partial receipt")
	}
	var wg sync.WaitGroup
	results := make(chan error, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results <- x.s.RecordNodeBootRecovery(x.f.contexts[in.NodeID], x.f.scope, raw, uuid.NewString())
		}()
	}
	wg.Wait()
	close(results)
	for e := range results {
		if e != nil {
			t.Fatal(e)
		}
	}
	for i, g := range c.Previous {
		chain, e := q.ReadShareChain(ctx, generated.ReadShareChainParams{TenantID: x.f.tenant, NodeID: in.NodeID, Kind: g.Kind})
		if e != nil || chain.BootID != in.ToBootID || chain.HeadGeneration != g.Generation || chain.RetiredGeneration != g.Generation || chain.ActiveGeneration != 0 || chain.PendingGeneration != 0 || chain.HeldAmount != g.Amount {
			t.Fatal("retirement changed occupation/floor", chain, e, i)
		}
	}
	if !bytes.Equal(held, authoritySnapshot(t, x.f.pool, x.f.tenant, financial)) {
		t.Fatal("retirement released debt")
	}
	oldFence, e := q.LockNodeFencing(ctx, generated.LockNodeFencingParams{TenantID: x.f.tenant, NodeID: in.NodeID, BootID: in.FromBootID})
	if e != nil || !oldFence.RenewalClosed {
		t.Fatal(oldFence, e)
	}
	// New policy and fresh signer authority do not become active on retirement.
	if e = x.f.nodeService.SetGrant(ctx, admin, x.f.group, 1, "active", base.Add(-time.Hour), base.Add(3*time.Hour), uuid.NewString()); e != nil {
		t.Fatal(e)
	}
	approve := sha256.Sum256([]byte("independent new signer approval"))
	if _, e = x.s.AuthorizeSigner(ctx, admin, x.in.SignerID, 8, x.now, x.now.Add(time.Hour), approve[:], uuid.NewString()); e != nil {
		t.Fatal(e)
	}
	x.in.BootID = in.ToBootID
	x.in.SignerEpoch = 8
	x.in.FencingGeneration = 4
	x.in.OwnershipUntil = x.now.Add(10 * time.Minute)
	x.in.PolicyUntil = x.now.Add(10 * time.Minute)
	for _, g := range c.Previous {
		x.in.Kind = g.Kind
		x.in.ExpectedGeneration = g.Generation
		x.in.Nonce = uuid.NewString()
		x.in.Amount = 250000
		x.in.BurstBytes = 31250
		if g.Kind == "connections" {
			x.in.Amount = 2
			x.in.BurstBytes = 0
		}
		next := x.prepare(t)
		if e = x.s.QueueHandoff(ctx, x.f.scope, next.Generation.ID, uuid.NewString()); e != nil {
			t.Fatal("retired predecessor did not permit next CAS", e)
		}
		confirmHandoff(t, x, st, next.Generation.ID)
		chain, e := q.ReadShareChain(ctx, generated.ReadShareChainParams{TenantID: x.f.tenant, NodeID: in.NodeID, Kind: g.Kind})
		if e != nil || chain.HeadGeneration != g.Generation+1 || chain.ActiveGeneration != g.Generation+1 || chain.RetiredGeneration != 0 || chain.HeldAmount != x.in.Amount {
			t.Fatal("actual CAS failed exact capacity release", chain, e)
		}
	}
	// Lost receipt replay after both next CAS operations cannot lower either head.
	x.now = x.now.Add(10 * time.Minute)
	replay, e := x.s.PrepareNodeBootRecovery(ctx, admin, in, uuid.NewString())
	if e != nil || !bytes.Equal(replay.Payload, p.Payload) {
		t.Fatal("expired approval replay", e)
	}
	if e = x.s.RecordNodeBootRecovery(x.f.contexts[in.NodeID], x.f.scope, raw, uuid.NewString()); e != nil {
		t.Fatal(e)
	}
	for _, g := range c.Previous {
		chain, e := q.ReadShareChain(ctx, generated.ReadShareChainParams{TenantID: x.f.tenant, NodeID: in.NodeID, Kind: g.Kind})
		if e != nil || chain.ActiveGeneration != g.Generation+1 {
			t.Fatal("replay lowered head", chain, e)
		}
	}
	if !bytes.Equal(held, authoritySnapshot(t, x.f.pool, x.f.tenant, financial)) {
		t.Fatal("new CAS released financial debt")
	}
	// Historical authenticated-origin usage stays on its original parent even
	// after current boot ownership has moved; replay cannot charge twice.
	var grant, source string
	if e = x.f.pool.QueryRow(ctx, `SELECT g.grant_id,a.reservation_id FROM hub_credit_grants g JOIN hub_funded_allocations a ON a.tenant_id=g.tenant_id AND a.allocation_id=g.allocation_id WHERE g.tenant_id=$1`, x.f.tenant).Scan(&grant, &source); e != nil {
		t.Fatal(e)
	}
	batch := &controlv1.UsageBatch{BatchId: uuid.NewString(), BillingSnapshotId: c.Previous[0].SnapshotID, Deltas: []*controlv1.UsageDelta{{RuleId: c.Previous[0].RuleID, GrantId: grant, Role: "source", Direction: "ingress", Bytes: 7}}}
	for i := 0; i < 2; i++ {
		if _, e = (service.MeteringService{Pool: x.f.pool}).IngestWireUsage(ctx, in.NodeID, in.FromBootID, 1, batch); e != nil {
			t.Fatal("late original boot usage", e)
		}
	}
	var charged int64
	if e = x.f.pool.QueryRow(ctx, `SELECT charged FROM ledger_projections WHERE tenant_id=$1 AND source_id=(SELECT source_id FROM reservations WHERE tenant_id=$1 AND id=$2)`, x.f.tenant, source).Scan(&charged); e != nil || charged != 7 {
		t.Fatal("late original usage lost/doubled", charged, e)
	}
	if _, e = x.f.pool.Exec(ctx, `UPDATE node_boot_recoveries SET to_boot_id=$1`, uuid.NewString()); e == nil {
		t.Fatal("mutable approval")
	}
	if _, e = x.f.pool.Exec(ctx, `DELETE FROM node_boot_recovery_receipts`); e == nil {
		t.Fatal("mutable receipt")
	}
}

func TestNodeBootRecoveryConcurrentDecisionsAndCurrentBootPin(t *testing.T) {
	x, in, _ := nodeColdFixture(t)
	ctx := context.Background()
	admin := policyScope(t, x.f.tenant, auth.PlatformAdmin, policy.Grant)
	x.now = x.now.Add(time.Hour)
	bad := in
	bad.ToBootID = uuid.NewString()
	if _, e := x.s.PrepareNodeBootRecovery(ctx, admin, bad, uuid.NewString()); !errors.Is(e, service.ErrShareUnknown) {
		t.Fatal("unobserved boot", e)
	}
	bad = in
	bad.MinimumFencing = 3
	if _, e := x.s.PrepareNodeBootRecovery(ctx, admin, bad, uuid.NewString()); !errors.Is(e, service.ErrShareConflict) {
		t.Fatal("floor not raised", e)
	}
	var wg sync.WaitGroup
	results := make(chan error, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			c := in
			c.ID = uuid.NewString()
			_, e := x.s.PrepareNodeBootRecovery(ctx, admin, c, uuid.NewString())
			results <- e
		}()
	}
	wg.Wait()
	close(results)
	yes, no := 0, 0
	for e := range results {
		if e == nil {
			yes++
		} else if errors.Is(e, service.ErrShareConflict) {
			no++
		} else {
			t.Fatal(e)
		}
	}
	if yes != 1 || no != 7 {
		t.Fatal(yes, no)
	}
	// Direct old-boot fresh authority is barred despite all old immutable facts.
	old := x.in
	old.Nonce = uuid.NewString()
	old.ExpectedGeneration = 2
	old.OwnershipUntil = x.now.Add(time.Minute)
	old.PolicyUntil = old.OwnershipUntil
	if _, e := x.s.PrepareShare(ctx, x.f.scope, old, uuid.NewString()); !errors.Is(e, service.ErrShareUnknown) {
		t.Fatal("old boot new share", e)
	}
}

func TestNodeBootRecoveryIncludesOtherTenantOriginalHardBoundary(t *testing.T) {
	var other string
	x, in, _ := nodeColdFixtureWithPrior(t, func(x *shareFixture) {
		ctx := context.Background()
		f, _, _ := ledgerFixture(t)
		other = f.tenant
		admin := policyScope(t, f.tenant, auth.PlatformAdmin, policy.Mutate)
		// Use the actual shared platform Node in a second tenant's new fixed group.
		if e := f.nodeService.SetMembers(ctx, admin, f.group, 2, []service.GroupMember{{NodeID: x.in.NodeID, Weight: 1}}, uuid.NewString()); e != nil {
			t.Fatal(e)
		}
		if e := f.nodeService.SetGrant(ctx, policyScope(t, f.tenant, auth.PlatformAdmin, policy.Grant), f.group, 1, "active", x.now.Add(-time.Hour), x.now.Add(3*time.Hour), uuid.NewString()); e != nil {
			t.Fatal(e)
		}
		rule := ruleInput(f.group)
		rule.Port = 20100
		reserveRulePort(t, f, rule)
		if _, e := (service.RuleService{Pool: f.pool}).Mutate(ctx, f.scope, rule, 0, uuid.NewString()); e != nil {
			t.Fatal(e)
		}
		b := durableBilling(t, f, rule.ID, 1, 1)
		otherNow := time.Now().UTC().Truncate(time.Microsecond)
		otherService := x.s
		otherService.Clock = func() (time.Time, time.Time, bool) { return otherNow, otherNow, true }
		approve := sha256.Sum256([]byte("other tenant longer original Node authority"))
		if _, e := otherService.AuthorizeSigner(ctx, policyScope(t, f.tenant, auth.PlatformAdmin, policy.Grant), x.in.SignerID, 7, otherNow, x.now.Add(90*time.Minute), approve[:], uuid.NewString()); e != nil {
			t.Fatal(e)
		}
		share := x.in
		share.SnapshotID = b.ID
		share.ExpectedGeneration = 0
		share.Nonce = uuid.NewString()
		share.Amount = 1000
		share.BurstBytes = 125
		share.OwnershipUntil = x.now.Add(80 * time.Minute)
		share.PolicyUntil = x.now.Add(80 * time.Minute)
		if _, e := otherService.PrepareShare(ctx, f.scope, share, uuid.NewString()); e != nil {
			t.Fatal("other tenant original share", e)
		}
	})
	ctx := context.Background()
	admin := policyScope(t, x.f.tenant, auth.PlatformAdmin, policy.Grant)
	base := x.now
	x.now = base.Add(time.Hour)
	if _, e := x.s.PrepareNodeBootRecovery(ctx, admin, in, uuid.NewString()); !errors.Is(e, service.ErrShareUnknown) {
		t.Fatal("omitted other tenant original deadline", e)
	}
	x.now = base.Add(90 * time.Minute)
	p, e := x.s.PrepareNodeBootRecovery(ctx, admin, in, uuid.NewString())
	if e != nil {
		t.Fatal(e)
	}
	var proof struct {
		Shares    []generated.ShareGeneration
		SafeAfter time.Time `json:"safe_after"`
	}
	if e = json.Unmarshal(p.Proof, &proof); e != nil {
		t.Fatal(e)
	}
	found := false
	for _, g := range proof.Shares {
		if g.TenantID == other {
			found = true
		}
	}
	if !found || !proof.SafeAfter.Equal(x.now) {
		t.Fatal("not a complete Node authority inventory", string(p.Proof))
	}
	// The target tenant's command cannot mutate the other tenant's reserved head.
	q := generated.New(x.f.pool)
	chain, e := q.ReadShareChain(ctx, generated.ReadShareChainParams{TenantID: other, NodeID: in.NodeID, Kind: "rate"})
	if e != nil || chain.BootID != in.FromBootID || chain.PendingGeneration != 1 || chain.HeldAmount != 1000 {
		t.Fatal(chain, e)
	}
}

func TestNodeBootRecoveryUnknownPredecessorAndStaleTargetStayHeld(t *testing.T) {
	t.Run("pending CAS", func(t *testing.T) {
		x, in, _ := nodeColdFixtureWithPrior(t, func(x *shareFixture) { x.in.ExpectedGeneration = 2; x.in.Nonce = uuid.NewString(); x.prepare(t) })
		x.now = x.now.Add(time.Hour)
		if _, e := x.s.PrepareNodeBootRecovery(context.Background(), policyScope(t, x.f.tenant, auth.PlatformAdmin, policy.Grant), in, uuid.NewString()); !errors.Is(e, service.ErrShareUnknown) {
			t.Fatal("unknown head reset", e)
		}
	})
	t.Run("target replaced after FULL", func(t *testing.T) {
		x, in, st := nodeColdFixture(t)
		ctx := context.Background()
		x.now = x.now.Add(time.Hour)
		p, e := x.s.PrepareNodeBootRecovery(ctx, policyScope(t, x.f.tenant, auth.PlatformAdmin, policy.Grant), in, uuid.NewString())
		if e != nil {
			t.Fatal(e)
		}
		r, e := st.RetireNodeBootShares(ctx, in.NodeID, in.ToBootID, in.BackendID, p.Payload, store.IssuerTime{Earliest: x.now, Latest: x.now, Trusted: true})
		if e != nil {
			t.Fatal(e)
		}
		raw, _ := json.Marshal(r)
		if e = x.f.ports.RegisterNodeSession(x.f.contexts[in.NodeID], policyScope(t, x.f.tenant, auth.PlatformAdmin, policy.Mutate), in.NodeID, uuid.NewString(), 3, uuid.NewString()); e != nil {
			t.Fatal(e)
		}
		tables := []string{"share_chains", "node_fencing", "node_boot_recovery_receipts", "reservations", "ledger_projections"}
		before := authoritySnapshot(t, x.f.pool, x.f.tenant, tables)
		if e = x.s.RecordNodeBootRecovery(x.f.contexts[in.NodeID], x.f.scope, raw, uuid.NewString()); !errors.Is(e, service.ErrStaleReceipt) {
			t.Fatal("stale target accepted", e)
		}
		if !bytes.Equal(before, authoritySnapshot(t, x.f.pool, x.f.tenant, tables)) {
			t.Fatal("stale target changed occupation")
		}
	})
}
