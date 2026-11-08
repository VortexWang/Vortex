package service_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"github.com/google/uuid"
	"sync"
	"testing"
	"time"
	"vortex/backend/db/generated"
	"vortex/backend/internal/auth"
	"vortex/backend/internal/policy"
	"vortex/backend/internal/repository"
	"vortex/backend/internal/service"
	"vortex/backend/internal/testutil"
)

func TestShareLeaseAuthoritySchema(t *testing.T) {
	p := testutil.Postgres(t)
	for _, table := range []string{"share_chains", "share_generations", "lease_expectations", "node_fencing", "share_renewal_authorities", "signer_authorities"} {
		var present bool
		if e := p.QueryRow(context.Background(), "SELECT to_regclass($1) IS NOT NULL", table).Scan(&present); e != nil {
			t.Fatal(e)
		}
		if !present {
			t.Errorf("durable share/lease authority table %s missing", table)
		}
	}
}

type shareFixture struct {
	f   *portFixture
	s   service.ShareService
	in  service.PrepareShareInput
	now time.Time
}

func newShareFixture(t *testing.T) *shareFixture {
	t.Helper()
	f, _, snapshot := ledgerFixture(t)
	now := time.Now().UTC().Truncate(time.Microsecond)
	x := &shareFixture{f: f, now: now}
	x.s = service.ShareService{Pool: f.pool, NodeCA: f.ports.NodeCA, SignerCA: f.ports.NodeCA, Clock: func() (time.Time, time.Time, bool) { return x.now, x.now, true }}
	q := generated.New(f.pool)
	node, e := q.GetNodeIncarnation(context.Background(), f.nodes[0])
	if e != nil {
		t.Fatal(e)
	}
	x.in = service.PrepareShareInput{NodeID: f.nodes[0], BootID: node.BootID, Kind: "rate", SignerID: f.nodes[1], SnapshotID: snapshot, Nonce: uuid.NewString(), SignerEpoch: 7, PathGeneration: 1, FencingGeneration: 3, Amount: 600000, BurstBytes: 75000, OwnershipUntil: now.Add(30 * time.Minute), PolicyUntil: now.Add(20 * time.Minute)}
	proof := sha256.Sum256([]byte("explicit platform signer approval"))
	if _, e = x.s.AuthorizeSigner(context.Background(), policyScope(t, f.tenant, auth.PlatformAdmin, policy.Grant), x.in.SignerID, 7, now.Add(-time.Second), now.Add(time.Hour), proof[:], uuid.NewString()); e != nil {
		t.Fatal(e)
	}
	return x
}
func (x *shareFixture) prepare(t *testing.T) service.PreparedShare {
	t.Helper()
	p, e := x.s.PrepareShare(context.Background(), x.f.scope, x.in, uuid.NewString())
	if e != nil {
		t.Fatal(e)
	}
	return p
}

// Task 1 lease tests install a known enforced fixture directly. Task 2 tests
// must obtain the same state exclusively through the durable Node handoff.
func (x *shareFixture) enforcedFixture(t *testing.T) {
	t.Helper()
	if _, e := x.f.pool.Exec(context.Background(), "UPDATE share_chains SET active_generation=head_generation,pending_generation=0 WHERE tenant_id=$1", x.f.tenant); e != nil {
		t.Fatal(e)
	}
}
func (x *shareFixture) renewal() service.LeaseRequest {
	return service.LeaseRequest{NodeID: x.in.NodeID, BootID: x.in.BootID, Kind: x.in.Kind, Nonce: uuid.NewString(), ConnectionID: uuid.NewString(), SignerID: x.in.SignerID, Generation: 1, SignerEpoch: 7, PathGeneration: 1, FencingGeneration: 3, RequestedAt: x.now.Add(-time.Second)}
}
func TestShareConcurrentLinearChainCapacityKindsAndAuditRollback(t *testing.T) {
	x := newShareFixture(t)
	ctx := context.Background()
	f := x.f
	var wg sync.WaitGroup
	results := make(chan error, 12)
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			in := x.in
			in.Nonce = uuid.NewString()
			_, e := x.s.PrepareShare(ctx, f.scope, in, uuid.NewString())
			results <- e
		}()
	}
	wg.Wait()
	close(results)
	success := 0
	for e := range results {
		if e == nil {
			success++
		} else if !errors.Is(e, service.ErrShareConflict) {
			t.Fatal(e)
		}
	}
	if success != 1 {
		t.Fatal("concurrent successor owners", success)
	}
	c, e := (repository.ShareRepository{Pool: f.pool}).Chain(ctx, f.scope, x.in.NodeID, "rate", uuid.NewString())
	if e != nil || c.HeadGeneration != 1 || c.PendingGeneration != 1 || c.ActiveGeneration != 0 || c.HeldAmount != 600000 {
		t.Fatal(e, c)
	}
	if _, e = x.s.RenewShare(f.contexts[x.in.NodeID], f.scope, x.renewal(), uuid.NewString()); !errors.Is(e, service.ErrShareConflict) {
		t.Fatal("prepared became renewable", e)
	}
	in := x.in
	in.Kind = "connections"
	in.Nonce = uuid.NewString()
	in.Amount = 600
	in.BurstBytes = 0
	p, e := x.s.PrepareShare(ctx, f.scope, in, uuid.NewString())
	if e != nil || p.Generation.Generation != 1 || p.Generation.SignerEpoch != 7 || p.Generation.PathGeneration != 1 || p.Generation.FencingGeneration != 3 {
		t.Fatal("independent kind/epochs", e, p)
	}
	// Capacity counts the entire tenant, across both target nodes.
	node, e := generated.New(f.pool).GetNodeIncarnation(ctx, f.nodes[1])
	if e != nil {
		t.Fatal(e)
	}
	in = x.in
	in.NodeID = f.nodes[1]
	in.BootID = node.BootID
	in.Nonce = uuid.NewString()
	in.Amount = 500000
	in.BurstBytes = 62500
	if _, e = x.s.PrepareShare(ctx, f.scope, in, uuid.NewString()); !errors.Is(e, service.ErrShareCapacity) {
		t.Fatal("global over-allocation", e)
	}
	request := uuid.NewString()
	failAudit(t, f.pool, request)
	tables := []string{"share_chains", "share_generations", "share_renewal_authorities", "share_handoffs", "node_fencing", "desired_state", "operations", "tasks", "outbox", "audit_events"}
	before := authoritySnapshot(t, f.pool, f.tenant, tables)
	in.Amount = 100000
	in.BurstBytes = 12500
	in.Nonce = uuid.NewString()
	if _, e = x.s.PrepareShare(ctx, f.scope, in, request); e == nil {
		t.Fatal("audit failure committed share")
	}
	if !bytes.Equal(before, authoritySnapshot(t, f.pool, f.tenant, tables)) {
		t.Fatal("partial authority/task/outbox/audit commit")
	}
	other := policyScope(t, seedTenant(t, f.pool), auth.Owner, policy.Read)
	if _, e = (repository.ShareRepository{Pool: f.pool}).Chain(ctx, other, x.in.NodeID, "rate", uuid.NewString()); e == nil {
		t.Fatal("cross tenant share read")
	}
	if _, e = (repository.ShareRepository{Pool: f.pool}).Chain(ctx, policyScope(t, f.tenant, auth.PlatformAdmin, policy.Read), x.in.NodeID, "rate", request); e == nil {
		t.Fatal("unaudited platform read")
	}
}
func TestShareLeaseDeadlineNonceBootFloorAndRenewalClosure(t *testing.T) {
	x := newShareFixture(t)
	f := x.f
	ctx := f.contexts[x.in.NodeID]
	x.in.OwnershipUntil = x.now.Add(2 * time.Minute)
	p := x.prepare(t)
	replay, e := x.s.PrepareShare(context.Background(), f.scope, x.in, uuid.NewString())
	if e != nil || replay.Generation.ID != p.Generation.ID {
		t.Fatal("prepare replay changed", e)
	}
	x.enforcedFixture(t)
	r := x.renewal()
	l, e := x.s.RenewShare(ctx, f.scope, r, uuid.NewString())
	if e != nil {
		t.Fatal(e)
	}
	if l.TtlSeconds != 300 || l.RenewalSeconds != 60 || !l.RelativeDeadline.Time.Equal(r.RequestedAt.Add(270*time.Second)) || !service.EffectiveLeaseDeadline(l).Equal(x.in.OwnershipUntil) {
		t.Fatal("TTL reduced twice or independent deadline changed", l)
	}
	x.now = x.now.Add(time.Minute)
	again, e := x.s.RenewShare(ctx, f.scope, r, uuid.NewString())
	if e != nil || again.ID != l.ID || !again.IssuedAt.Time.Equal(l.IssuedAt.Time) {
		t.Fatal("nonce replay extended", e)
	}
	bad := r
	bad.ConnectionID = uuid.NewString()
	if _, e = x.s.RenewShare(ctx, f.scope, bad, uuid.NewString()); !errors.Is(e, service.ErrShareConflict) {
		t.Fatal("changed nonce payload", e)
	}
	for _, change := range []func(*service.LeaseRequest){func(i *service.LeaseRequest) { i.BootID = uuid.NewString() }, func(i *service.LeaseRequest) { i.Generation = 2 }, func(i *service.LeaseRequest) { i.FencingGeneration = 2 }, func(i *service.LeaseRequest) { i.SignerEpoch = 6 }, func(i *service.LeaseRequest) { i.PathGeneration = 2 }, func(i *service.LeaseRequest) { i.RequestedAt = x.now.Add(-11 * time.Second) }} {
		bad = x.renewal()
		change(&bad)
		if _, e = x.s.RenewShare(ctx, f.scope, bad, uuid.NewString()); e == nil {
			t.Fatal("stale renewal accepted", bad)
		}
	}
	// Preparing a lower successor reserves max(old,new); it does not revoke the
	// old child lease and cannot release capacity before authenticated closure/CAS.
	x.in.ExpectedGeneration = 1
	x.in.Nonce = uuid.NewString()
	x.in.Amount = 300000
	x.in.BurstBytes = 37500
	x.prepare(t)
	close := service.RenewalClosure{NodeID: x.in.NodeID, BootID: x.in.BootID, Kind: "rate", SignerID: x.in.SignerID, Generation: 1, SuccessorGeneration: 2, SignerEpoch: 7}
	if e = x.s.CloseOldRenewal(context.Background(), f.scope, close, uuid.NewString()); !errors.Is(e, service.ErrScope) {
		t.Fatal("tenant forged signer closure", e)
	}
	request := uuid.NewString()
	failAudit(t, f.pool, request)
	tables := []string{"share_chains", "share_renewal_authorities", "operations", "tasks", "outbox", "audit_events"}
	before := authoritySnapshot(t, f.pool, f.tenant, tables)
	if e = x.s.CloseOldRenewal(f.contexts[x.in.SignerID], f.scope, close, request); e == nil {
		t.Fatal("closure audit rollback")
	}
	if !bytes.Equal(before, authoritySnapshot(t, f.pool, f.tenant, tables)) {
		t.Fatal("partial closure")
	}
	for i := 0; i < 2; i++ {
		if e = x.s.CloseOldRenewal(f.contexts[x.in.SignerID], f.scope, close, uuid.NewString()); e != nil {
			t.Fatal(e)
		}
	}
	if _, e = x.s.RenewShare(ctx, f.scope, x.renewal(), uuid.NewString()); !errors.Is(e, service.ErrShareConflict) {
		t.Fatal("closed signer renewed", e)
	}
	c, e := (repository.ShareRepository{Pool: f.pool}).Chain(context.Background(), f.scope, x.in.NodeID, "rate", uuid.NewString())
	if e != nil || c.HeldAmount != 600000 || c.ActiveGeneration != 1 {
		t.Fatal("closure released occupation", e, c)
	}
	// A fence raises independently from signer/path/share generation.
	if e = x.s.RecordNodeFencing(ctx, f.scope, x.in.NodeID, x.in.BootID, 4, uuid.NewString()); e != nil {
		t.Fatal(e)
	}
	if e = x.s.RecordNodeFencing(ctx, f.scope, x.in.NodeID, x.in.BootID, 3, uuid.NewString()); !errors.Is(e, service.ErrShareConflict) {
		t.Fatal("floor regressed", e)
	}
}
func TestShareBurstSignerTimeAndRenewAuditRollback(t *testing.T) {
	x := newShareFixture(t)
	f := x.f
	in := x.in
	in.BurstBytes = in.Amount/8 + 1
	if _, e := x.s.PrepareShare(context.Background(), f.scope, in, uuid.NewString()); !errors.Is(e, service.ErrShareCapacity) {
		t.Fatal("burst > one second", e)
	}
	noClock := x.s
	noClock.Clock = nil
	if _, e := noClock.PrepareShare(context.Background(), f.scope, x.in, uuid.NewString()); !errors.Is(e, service.ErrLeaseBoundary) {
		t.Fatal("untrusted time", e)
	}
	x.prepare(t)
	x.enforcedFixture(t)
	request := uuid.NewString()
	failAudit(t, f.pool, request)
	tables := []string{"lease_expectations", "operations", "tasks", "outbox", "audit_events"}
	before := authoritySnapshot(t, f.pool, f.tenant, tables)
	if _, e := x.s.RenewShare(f.contexts[x.in.NodeID], f.scope, x.renewal(), request); e == nil {
		t.Fatal("renew audit rollback")
	}
	if !bytes.Equal(before, authoritySnapshot(t, f.pool, f.tenant, tables)) {
		t.Fatal("partial lease/task/outbox")
	}
	r := x.renewal()
	l, e := x.s.RenewShare(f.contexts[x.in.NodeID], f.scope, r, uuid.NewString())
	if e != nil {
		t.Fatal(e)
	}
	r = x.renewal()
	l2, e := x.s.RenewShare(f.contexts[x.in.NodeID], f.scope, r, uuid.NewString())
	if e != nil || l2.Revision != l.Revision+1 {
		t.Fatal("lease revision", e)
	}
	// Reboot registration cannot renew an old, still held incarnation.
	if e = (f.ports).RegisterNodeSession(f.contexts[x.in.NodeID], policyScope(t, f.tenant, auth.PlatformAdmin, policy.Mutate), x.in.NodeID, uuid.NewString(), 2, uuid.NewString()); e != nil {
		t.Fatal(e)
	}
	if _, e = x.s.RenewShare(f.contexts[x.in.NodeID], f.scope, x.renewal(), uuid.NewString()); !errors.Is(e, service.ErrShareConflict) {
		t.Fatal("old boot renewed after restart", e)
	}
}

func TestShareSignerAndFenceAuditRollbackAndSelfHostedExemption(t *testing.T) {
	x := newShareFixture(t)
	f := x.f
	ctx := context.Background()
	request := uuid.NewString()
	failAudit(t, f.pool, request)
	tables := []string{"signer_authorities", "node_fencing", "operations", "tasks", "outbox", "audit_events"}
	before := authoritySnapshot(t, f.pool, f.tenant, tables)
	proof := sha256.Sum256([]byte("epoch 8 approval"))
	grant := policyScope(t, f.tenant, auth.PlatformAdmin, policy.Grant)
	if _, e := x.s.AuthorizeSigner(ctx, grant, x.in.SignerID, 8, x.now.Add(-time.Second), x.now.Add(time.Hour), proof[:], request); e == nil {
		t.Fatal("signer audit rollback")
	}
	if !bytes.Equal(before, authoritySnapshot(t, f.pool, f.tenant, tables)) {
		t.Fatal("partial signer authority")
	}
	if _, e := x.s.AuthorizeSigner(ctx, f.scope, x.in.SignerID, 8, x.now.Add(-time.Second), x.now.Add(time.Hour), proof[:], uuid.NewString()); !errors.Is(e, service.ErrScope) {
		t.Fatal("tenant authorized signer", e)
	}
	if _, e := x.s.AuthorizeSigner(ctx, grant, x.in.SignerID, 7, x.now.Add(-time.Second), x.now.Add(time.Hour), proof[:], uuid.NewString()); !errors.Is(e, service.ErrShareConflict) {
		t.Fatal("same epoch different approval", e)
	}
	x.prepare(t)
	before = authoritySnapshot(t, f.pool, f.tenant, tables)
	if e := x.s.RecordNodeFencing(f.contexts[x.in.NodeID], f.scope, x.in.NodeID, x.in.BootID, 9, request); e == nil {
		t.Fatal("fence audit rollback")
	}
	if !bytes.Equal(before, authoritySnapshot(t, f.pool, f.tenant, tables)) {
		t.Fatal("partial fence authority")
	}
	if e := x.s.RecordNodeFencing(ctx, f.scope, x.in.NodeID, x.in.BootID, 9, uuid.NewString()); !errors.Is(e, service.ErrScope) {
		t.Fatal("unauthenticated floor proof", e)
	}
	f2 := newPortFixture(t)
	in := ruleInput(f2.group)
	reserveRulePort(t, f2, in)
	if _, e := (service.RuleService{Pool: f2.pool}).Mutate(ctx, f2.scope, in, 0, uuid.NewString()); e != nil {
		t.Fatal(e)
	}
	b := durableBilling(t, f2, in.ID, 1, 1)
	node, e := generated.New(f2.pool).GetNodeIncarnation(ctx, f2.nodes[0])
	if e != nil {
		t.Fatal(e)
	}
	input := x.in
	input.NodeID = f2.nodes[0]
	input.BootID = node.BootID
	input.SnapshotID = b.ID
	input.Nonce = uuid.NewString()
	x.now = time.Now().UTC().Truncate(time.Microsecond)
	s := x.s
	s.Pool = f2.pool
	if _, e = s.PrepareShare(ctx, f2.scope, input, uuid.NewString()); !errors.Is(e, service.ErrSelfHostedExempt) {
		t.Fatal("self hosted path charged platform shares", e)
	}
}
