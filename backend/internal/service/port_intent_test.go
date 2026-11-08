package service_test

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"
	"vortex/backend/internal/service"
)

func TestPortIntentCommitCASAuditAndFullReleaseProjection(t *testing.T) {
	isolatePackageAuthority(t)
	f := newPortFixture(t)
	ctx := context.Background()
	a, e := f.ports.PreparePortReservation(ctx, f.scope, f.request(t, 20000), uuid.NewString())
	if e != nil {
		t.Fatal(e)
	}
	for _, n := range f.nodes {
		p := f.proof(t, a.PortAllocation, n, "prepare")
		if e = f.ports.RecordPrepareProof(f.contexts[n], f.scope, p, uuid.NewString()); e != nil {
			t.Fatal(e)
		}
	}
	tables := []string{"port_allocations", "port_reservations", "port_operation_bindings", "desired_state", "operations", "tasks", "outbox", "audit_events", "domain_events"}
	request := uuid.NewString()
	failAudit(t, f.pool, request)
	before := authoritySnapshot(t, f.pool, f.tenant, tables)
	if result, e := f.ports.CommitPortWithOperation(ctx, f.scope, a.ID, 1, request); e == nil || !strings.Contains(e.Error(), "injected audit fault") || result.ID != "" || result.OperationID != "" {
		t.Fatal(result, e)
	}
	if !bytes.Equal(before, authoritySnapshot(t, f.pool, f.tenant, tables)) {
		t.Fatal("partial commit")
	}
	results := make(chan service.PortMutationResult, 8)
	errs := make(chan error, 8)
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r, e := f.ports.CommitPortWithOperation(ctx, f.scope, a.ID, 1, uuid.NewString())
			results <- r
			errs <- e
		}()
	}
	wg.Wait()
	close(results)
	close(errs)
	accepted, stale := 0, 0
	var commit service.PortMutationResult
	for e := range errs {
		if e == nil {
			accepted++
		} else if errors.Is(e, service.ErrPrecondition) {
			stale++
		} else {
			t.Fatal(e)
		}
	}
	for r := range results {
		if r.OperationID != "" {
			commit = r
		}
	}
	if accepted != 1 || stale != 7 || commit.Version != 2 || commit.State != "committed" || commit.OperationState != "succeeded" {
		t.Fatal(accepted, stale, commit)
	}
	assert := func(op, state string, held bool) {
		t.Helper()
		var s string
		var h bool
		if e := f.pool.QueryRow(ctx, `SELECT state,occupation_held FROM operations WHERE tenant_id=$1 AND id=$2`, f.tenant, op).Scan(&s, &h); e != nil || s != state || h != held {
			t.Fatal(s, h, e)
		}
	}
	assert(commit.OperationID, "succeeded", true)
	// Commit is the persisted allocation stage, not a proof of Engine activation.
	request = uuid.NewString()
	failAudit(t, f.pool, request)
	before = authoritySnapshot(t, f.pool, f.tenant, tables)
	if r, e := f.ports.ReleasePortWithOperation(ctx, f.scope, a.ID, "release", 2, request); e == nil || !strings.Contains(e.Error(), "injected audit fault") || r.ID != "" {
		t.Fatal(r, e)
	}
	if !bytes.Equal(before, authoritySnapshot(t, f.pool, f.tenant, tables)) {
		t.Fatal("partial release intent")
	}
	release, e := f.ports.ReleasePortWithOperation(ctx, f.scope, a.ID, "release", 2, uuid.NewString())
	if e != nil || release.Version != 3 || release.OperationState != "pending" {
		t.Fatal(release, e)
	}
	assert(release.OperationID, "pending", true)
	if _, e = f.ports.ReleasePortWithOperation(ctx, f.scope, a.ID, "release", 2, uuid.NewString()); !errors.Is(e, service.ErrPrecondition) {
		t.Fatal(e)
	}
	p := f.proof(t, a.PortAllocation, f.nodes[0], "release")
	p.Result = "unknown"
	if e = f.ports.ReleaseUnknown(ctx, f.scope, p, uuid.NewString()); e != nil {
		t.Fatal(e)
	}
	if _, e = f.ports.ReconcilePortOperation(ctx, f.tenant, release.OperationID, uuid.NewString()); e != nil {
		t.Fatal(e)
	}
	assert(release.OperationID, "unknown", true)
	p.Result = "released"
	if e = f.ports.RecordReleaseProof(f.contexts[f.nodes[0]], f.scope, p, uuid.NewString()); e != nil {
		t.Fatal(e)
	}
	if _, e = f.ports.ReconcilePortOperation(ctx, f.tenant, release.OperationID, uuid.NewString()); e != nil {
		t.Fatal(e)
	}
	assert(release.OperationID, "pending", true)
	p = f.proof(t, a.PortAllocation, f.nodes[1], "release")
	if e = f.ports.RecordReleaseProof(f.contexts[f.nodes[1]], f.scope, p, uuid.NewString()); e != nil {
		t.Fatal(e)
	}
	request = uuid.NewString()
	failAudit(t, f.pool, request)
	before = authoritySnapshot(t, f.pool, f.tenant, tables)
	if worked, e := f.ports.ReconcilePortOperation(ctx, f.tenant, release.OperationID, request); e == nil || !strings.Contains(e.Error(), "injected audit fault") || worked {
		t.Fatal("release completion escaped audit", worked, e)
	}
	if !bytes.Equal(before, authoritySnapshot(t, f.pool, f.tenant, tables)) {
		t.Fatal("release completion partially committed")
	}
	for _, op := range []string{commit.OperationID, release.OperationID} {
		if _, e = f.ports.ReconcilePortOperation(ctx, f.tenant, op, uuid.NewString()); e != nil {
			t.Fatal(e)
		}
	}
	assert(commit.OperationID, "succeeded", false)
	assert(release.OperationID, "succeeded", false)
	before = authoritySnapshot(t, f.pool, f.tenant, tables)
	if worked, e := f.ports.ReconcilePortOperation(ctx, f.tenant, release.OperationID, uuid.NewString()); e != nil || worked {
		t.Fatal(worked, e)
	}
	if !bytes.Equal(before, authoritySnapshot(t, f.pool, f.tenant, tables)) {
		t.Fatal("replay appended evidence")
	}
}
func TestPortIntentRejectsMutableConfirmationAndHistoricalUnknown(t *testing.T) {
	f := newPortFixture(t)
	ctx := context.Background()
	a := f.prepare(t, 20000)
	if _, e := f.pool.Exec(ctx, `UPDATE port_reservations SET state='confirmed',config_confirmed=true WHERE tenant_id=$1 AND allocation_id=$2`, f.tenant, a.ID); e != nil {
		t.Fatal(e)
	}
	if r, e := f.ports.CommitPortWithOperation(ctx, f.scope, a.ID, 1, uuid.NewString()); !errors.Is(e, service.ErrConflict) || r.OperationID != "" {
		t.Fatal("mutable flags minted commit", r, e)
	}
	// Historical unknown allocations have no immutable original inventory. They
	// remain occupied, rather than declaring a partial count to be complete.
	old := uuid.NewString()
	if _, e := f.pool.Exec(ctx, `INSERT INTO port_allocations(id,tenant_id,rule_id,group_id,member_set_version,port,listen_address,protocols,purpose,state) VALUES($1,$2,$3,$4,repeat('0',64),20500,'*',ARRAY['tcp'],'service','unknown')`, old, f.tenant, uuid.NewString(), f.group); e != nil {
		t.Fatal(e)
	}
	if r, e := f.ports.ReleasePortWithOperation(ctx, f.scope, old, "release", 1, uuid.NewString()); !errors.Is(e, service.ErrConflict) || r.ID != "" {
		t.Fatal(r, e)
	}
	var state string
	if e := f.pool.QueryRow(ctx, `SELECT state FROM port_allocations WHERE tenant_id=$1 AND id=$2`, f.tenant, old).Scan(&state); e != nil || state != "unknown" {
		t.Fatal(state, e)
	}
}
func TestPortIntentAbortDoesNotReleaseWithoutAllNodeProofs(t *testing.T) {
	f := newPortFixture(t)
	ctx := context.Background()
	a := f.prepare(t, 20000)
	r, e := f.ports.ReleasePortWithOperation(ctx, f.scope, a.ID, "abort", 1, uuid.NewString())
	if e != nil || r.State != "aborting" {
		t.Fatal(r, e)
	}
	if _, e = f.ports.ReconcilePortOperation(ctx, f.tenant, r.OperationID, uuid.NewString()); e != nil {
		t.Fatal(e)
	}
	var held bool
	var state string
	if e = f.pool.QueryRow(ctx, `SELECT state,occupation_held FROM operations WHERE tenant_id=$1 AND id=$2`, f.tenant, r.OperationID).Scan(&state, &held); e != nil || state != "pending" || !held {
		t.Fatal(state, held, e)
	}
	for _, n := range f.nodes {
		p := f.proof(t, a, n, "release")
		if e = f.ports.RecordReleaseProof(f.contexts[n], f.scope, p, uuid.NewString()); e != nil {
			t.Fatal(e)
		}
	}
	if _, e = f.ports.ReconcilePortOperation(ctx, f.tenant, r.OperationID, uuid.NewString()); e != nil {
		t.Fatal(e)
	}
	if e = f.pool.QueryRow(ctx, `SELECT state,occupation_held FROM operations WHERE tenant_id=$1 AND id=$2`, f.tenant, r.OperationID).Scan(&state, &held); e != nil || state != "succeeded" || held {
		t.Fatal(state, held, e)
	}
}
