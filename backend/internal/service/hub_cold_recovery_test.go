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
	"vortex/backend/db/generated"
	"vortex/backend/internal/auth"
	"vortex/backend/internal/policy"
	"vortex/backend/internal/service"
	controlv1 "vortex/contracts/protobuf/v1"
)

func coldRecoveryInput(t *testing.T, x *shareFixture, hub, boot string) service.HubColdRecoveryInput {
	t.Helper()
	proof := sha256.Sum256([]byte("explicit cold epoch; no authority transfer"))
	// The successor is approved before expiry, with its own fixed autonomy clock.
	if _, e := x.s.AuthorizeSigner(context.Background(), policyScope(t, x.f.tenant, auth.PlatformAdmin, policy.Grant), hub, 8, x.now.Add(-time.Second), x.now.Add(2*time.Hour), proof[:], uuid.NewString()); e != nil {
		t.Fatal(e)
	}
	return service.HubColdRecoveryInput{ID: uuid.NewString(), HubID: hub, FromBootID: boot, ToBootID: uuid.NewString(), FromEpoch: 7, ToEpoch: 8, Reason: "replace failed Hub disk after every original hard boundary"}
}

func TestHubColdRecoveryExpiryProofAuditRollbackAndUnknownFundsRetained(t *testing.T) {
	x, in, hs := fundedHubFixture(t)
	ctx := context.Background()
	a, e := x.s.PrepareHubAllocation(ctx, x.f.scope, in, uuid.NewString())
	if e != nil {
		t.Fatal(e)
	}
	installAllocationFixture(t, hs, a, x.now)
	issued, grant, seq := issueAllocationFixture(t, x, hs, a, 37)
	if e = x.s.RecordHubIssue(x.f.contexts[in.HubID], in.HubID, in.HubBootID, seq, issued.Payload, uuid.NewString()); e != nil {
		t.Fatal(e)
	}
	unknownProof := sha256.Sum256([]byte("unconfirmed child execution remains frozen after cold Hub recovery"))
	if e = (service.MeteringService{Pool: x.f.pool}).RecordUnknown(ctx, x.f.scope, a.SourceID, unknownProof[:], x.now.Add(time.Hour), uuid.NewString()); e != nil {
		t.Fatal(e)
	}
	r := coldRecoveryInput(t, x, in.HubID, in.HubBootID)
	admin := policyScope(t, x.f.tenant, auth.PlatformAdmin, policy.Grant)
	tables := []string{"reservations", "ledger_projections", "ledger_events", "hub_credit_grants", "share_chains", "share_generations", "share_renewal_authorities", "settlements"}
	held := authoritySnapshot(t, x.f.pool, x.f.tenant, tables)
	if _, e = x.s.RecoverColdHubBoot(ctx, x.f.scope, r, uuid.NewString()); !errors.Is(e, service.ErrScope) {
		t.Fatal("tenant recovered Hub", e)
	}
	if _, e = x.s.RecoverColdHubBoot(ctx, policyScope(t, x.f.tenant, auth.Operator, policy.Mutate), r, uuid.NewString()); !errors.Is(e, service.ErrScope) {
		t.Fatal("operator recovered Hub", e)
	}
	if _, e = x.s.RecoverColdHubBoot(ctx, admin, r, uuid.NewString()); !errors.Is(e, service.ErrShareUnknown) {
		t.Fatal("live old authority bypassed", e)
	}
	oldDeadline := x.now.Add(time.Hour)
	x.s.Clock = func() (time.Time, time.Time, bool) {
		return oldDeadline.Add(-time.Microsecond), oldDeadline.Add(time.Microsecond), true
	}
	if _, e = x.s.RecoverColdHubBoot(ctx, admin, r, uuid.NewString()); !errors.Is(e, service.ErrShareUnknown) {
		t.Fatal("upper bound proved expiry", e)
	}
	x.s.Clock = func() (time.Time, time.Time, bool) { return oldDeadline, oldDeadline, true }
	request := uuid.NewString()
	failAudit(t, x.f.pool, request)
	before := authoritySnapshot(t, x.f.pool, x.f.tenant, append(tables, "hub_cold_recoveries", "tasks", "outbox", "operations", "audit_events"))
	if _, e = x.s.RecoverColdHubBoot(ctx, admin, r, request); e == nil {
		t.Fatal("audit failure accepted recovery")
	}
	if !bytes.Equal(before, authoritySnapshot(t, x.f.pool, x.f.tenant, append(tables, "hub_cold_recoveries", "tasks", "outbox", "operations", "audit_events"))) {
		t.Fatal("recovery escaped audit rollback")
	}
	q := generated.New(x.f.pool)
	latest, e := q.LatestHubIncarnation(ctx, in.HubID)
	if e != nil || latest.SignerEpoch != 7 {
		t.Fatal("partial boot binding", latest, e)
	}
	result, e := x.s.RecoverColdHubBoot(ctx, admin, r, uuid.NewString())
	if e != nil {
		t.Fatal(e)
	}
	if !bytes.Equal(held, authoritySnapshot(t, x.f.pool, x.f.tenant, tables)) {
		t.Fatal("cold recovery changed original liability/capacity")
	}
	var p struct {
		OldHardDeadline time.Time `json:"old_hard_deadline"`
		Allocations     []any     `json:"allocations"`
		Shares          []any     `json:"shares"`
	}
	if json.Unmarshal(result.Payload, &p) != nil || !p.OldHardDeadline.Equal(oldDeadline) || len(p.Allocations) != 1 || len(p.Shares) != 2 {
		t.Fatal("incomplete immutable inventory", string(result.Payload))
	}
	again, e := x.s.RecoverColdHubBoot(ctx, admin, r, uuid.NewString())
	if e != nil || !bytes.Equal(again.Payload, result.Payload) {
		t.Fatal("replay changed proof", e)
	}
	changed := r
	changed.ToBootID = uuid.NewString()
	if _, e = x.s.RecoverColdHubBoot(ctx, admin, changed, uuid.NewString()); !errors.Is(e, service.ErrShareConflict) {
		t.Fatal("recovery rebound target", e)
	}
	if _, e = x.s.BindHubSession(x.f.contexts[in.HubID], in.HubID, in.HubBootID, uuid.NewString()); !errors.Is(e, service.ErrShareUnknown) {
		t.Fatal("old boot revived", e)
	}
	bound, e := x.s.BindHubSession(x.f.contexts[in.HubID], in.HubID, r.ToBootID, uuid.NewString())
	if e != nil || bound.Epoch != 8 {
		t.Fatal(bound, e)
	}
	// A late authenticated-origin fact belongs to the original financial parent;
	// recovery never rewrites its source or rejects history merely for being old.
	batch := &controlv1.UsageBatch{BatchId: uuid.NewString(), BillingSnapshotId: a.SnapshotID, Deltas: []*controlv1.UsageDelta{{RuleId: a.RuleID, GrantId: grant, Role: "source", Direction: "ingress", Bytes: 7}}}
	for i := 0; i < 2; i++ {
		if _, e = (service.MeteringService{Pool: x.f.pool}).IngestWireUsage(ctx, a.NodeID, a.BootID, 1, batch); e != nil {
			t.Fatal("late old-boot usage", e)
		}
	}
	projection, e := q.GetSourceProjection(ctx, generated.GetSourceProjectionParams{TenantID: x.f.tenant, SourceID: in.SourceID})
	if e != nil || projection.Charged != 7 || projection.F != 96 {
		t.Fatal("late usage lost original source hold", projection, e)
	}
	if _, e = x.f.pool.Exec(ctx, `UPDATE hub_cold_recoveries SET to_boot_id=$1`, uuid.NewString()); e == nil {
		t.Fatal("mutable recovery proof")
	}
}

func TestHubColdRecoveryConcurrentTargetsAndAllTenantHardBoundaries(t *testing.T) {
	x := newShareFixture(t)
	ctx := context.Background()
	hub := x.in.SignerID
	boot := uuid.NewString()
	if _, e := x.s.BindHubSession(x.f.contexts[hub], hub, boot, uuid.NewString()); e != nil {
		t.Fatal(e)
	}
	// An unrelated tenant may have a later original deadline under the same Hub.
	other := uuid.NewString()
	if _, e := x.f.pool.Exec(ctx, `INSERT INTO tenants(id,account_state,version) VALUES($1,'active',1)`, other); e != nil {
		t.Fatal(e)
	}
	evidence := sha256.Sum256([]byte("other tenant prior authority"))
	if _, e := x.s.AuthorizeSigner(ctx, policyScope(t, other, auth.PlatformAdmin, policy.Grant), hub, 7, x.now.Add(-time.Second), x.now.Add(90*time.Minute), evidence[:], uuid.NewString()); e != nil {
		t.Fatal(e)
	}
	r := coldRecoveryInput(t, x, hub, boot)
	admin := policyScope(t, x.f.tenant, auth.PlatformAdmin, policy.Grant)
	base := x.now
	x.now = base.Add(time.Hour)
	if _, e := x.s.RecoverColdHubBoot(ctx, admin, r, uuid.NewString()); !errors.Is(e, service.ErrShareUnknown) {
		t.Fatal("missing other tenant approval", e)
	}
	if _, e := x.s.AuthorizeSigner(ctx, policyScope(t, other, auth.PlatformAdmin, policy.Grant), hub, 8, x.now.Add(-time.Second), base.Add(3*time.Hour), evidence[:], uuid.NewString()); e != nil {
		t.Fatal(e)
	}
	if _, e := x.s.RecoverColdHubBoot(ctx, admin, r, uuid.NewString()); !errors.Is(e, service.ErrShareUnknown) {
		t.Fatal("ignored other tenant hard deadline", e)
	}
	x.now = base.Add(90 * time.Minute)
	var wg sync.WaitGroup
	start := make(chan struct{})
	results := make(chan error, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			c := r
			c.ID = uuid.NewString()
			c.ToBootID = uuid.NewString()
			_, e := x.s.RecoverColdHubBoot(ctx, admin, c, uuid.NewString())
			results <- e
		}()
	}
	close(start)
	wg.Wait()
	close(results)
	accepted, blocked := 0, 0
	for e := range results {
		if e == nil {
			accepted++
		} else if errors.Is(e, service.ErrShareConflict) {
			blocked++
		} else {
			t.Fatal(e)
		}
	}
	if accepted != 1 || blocked != 7 {
		t.Fatal("concurrent recovery split ownership", accepted, blocked)
	}
	// A new tenant cannot approve a retired epoch after cold recovery.
	third := uuid.NewString()
	if _, e := x.f.pool.Exec(ctx, `INSERT INTO tenants(id,account_state,version) VALUES($1,'active',1)`, third); e != nil {
		t.Fatal(e)
	}
	if _, e := x.s.AuthorizeSigner(ctx, policyScope(t, third, auth.PlatformAdmin, policy.Grant), hub, 7, x.now.Add(-time.Second), x.now.Add(time.Hour), evidence[:], uuid.NewString()); !errors.Is(e, service.ErrShareConflict) {
		t.Fatal("retired epoch gained new authority", e)
	}
}
