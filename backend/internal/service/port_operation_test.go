package service_test

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"
	"vortex/backend/internal/service"
	"vortex/backend/internal/worker"
)

type portOperationProjectionAPI interface {
	ReconcilePortOperation(context.Context, string, string, string) (bool, error)
}

func portOperationID(t *testing.T, result service.PortPreparation) string {
	t.Helper()
	raw, e := json.Marshal(result)
	if e != nil {
		t.Fatal(e)
	}
	var v struct {
		Operation string `json:"operation_id"`
	}
	if e = json.Unmarshal(raw, &v); e != nil || v.Operation == "" {
		t.Fatal("exact durable aggregate port operation missing", e)
	}
	return v.Operation
}
func TestPortOperationPrepareFULLProofsUnknownReplayAndNoRelease(t *testing.T) {
	isolatePackageAuthority(t)
	f := newPortFixture(t)
	ctx := context.Background()
	result, e := f.ports.PreparePortReservation(ctx, f.scope, f.request(t, 20000), uuid.NewString())
	if e != nil {
		t.Fatal(e)
	}
	operation := portOperationID(t, result)
	api, ok := any(f.ports).(portOperationProjectionAPI)
	if !ok {
		t.Fatal("proof-bound port operation projection consumer missing")
	}
	assert := func(state string, held bool) {
		t.Helper()
		var actual string
		var occupied bool
		if e := f.pool.QueryRow(ctx, `SELECT state,occupation_held FROM operations WHERE tenant_id=$1 AND id=$2`, f.tenant, operation).Scan(&actual, &occupied); e != nil || actual != state || occupied != held {
			t.Fatal(actual, occupied, e)
		}
	}
	assert("pending", true)
	if worked, e := api.ReconcilePortOperation(ctx, f.tenant, operation, uuid.NewString()); e != nil || worked {
		t.Fatal("pending gained invented evidence", worked, e)
	}
	first := f.proof(t, result.PortAllocation, f.nodes[0], "prepare")
	first.Result = "unknown"
	if e = f.ports.RecordPrepareProof(f.contexts[f.nodes[0]], f.scope, first, uuid.NewString()); e != nil {
		t.Fatal(e)
	}
	if worked, e := api.ReconcilePortOperation(ctx, f.tenant, operation, uuid.NewString()); e != nil || !worked {
		t.Fatal(worked, e)
	}
	assert("unknown", true)
	first.Result = "prepared"
	if e = f.ports.RecordPrepareProof(f.contexts[f.nodes[0]], f.scope, first, uuid.NewString()); e != nil {
		t.Fatal(e)
	}
	if worked, e := api.ReconcilePortOperation(ctx, f.tenant, operation, uuid.NewString()); e != nil || !worked {
		t.Fatal(worked, e)
	}
	assert("pending", true)
	last := f.proof(t, result.PortAllocation, f.nodes[1], "prepare")
	if e = f.ports.RecordPrepareProof(f.contexts[f.nodes[1]], f.scope, last, uuid.NewString()); e != nil {
		t.Fatal(e)
	}
	tables := []string{"port_allocations", "port_reservations", "release_proofs", "operations", "tasks", "outbox", "audit_events", "domain_events"}
	request := uuid.NewString()
	failAudit(t, f.pool, request)
	before := authoritySnapshot(t, f.pool, f.tenant, tables)
	if worked, e := api.ReconcilePortOperation(ctx, f.tenant, operation, request); e == nil || !strings.Contains(e.Error(), "injected audit fault") || worked {
		t.Fatal("projection escaped audit rollback", worked, e)
	}
	if !bytes.Equal(before, authoritySnapshot(t, f.pool, f.tenant, tables)) {
		t.Fatal("partial aggregate projection")
	}
	errs := make(chan error, 8)
	changes := make(chan bool, 8)
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			worked, e := api.ReconcilePortOperation(ctx, f.tenant, operation, uuid.NewString())
			changes <- worked
			errs <- e
		}()
	}
	wg.Wait()
	close(changes)
	close(errs)
	n := 0
	for changed := range changes {
		if changed {
			n++
		}
	}
	for e := range errs {
		if e != nil {
			t.Fatal(e)
		}
	}
	if n != 1 {
		t.Fatal("duplicate aggregate completion", n)
	}
	assert("succeeded", true)
	var state string
	var held int
	if e = f.pool.QueryRow(ctx, `SELECT state,(SELECT count(*) FROM port_reservations WHERE tenant_id=$1 AND allocation_id=$2 AND state<>'released') FROM port_allocations WHERE tenant_id=$1 AND id=$2`, f.tenant, result.ID).Scan(&state, &held); e != nil || state != "preparing" || held != 4 {
		t.Fatal("prepare projection activated/released allocation", state, held, e)
	}
	before = authoritySnapshot(t, f.pool, f.tenant, tables)
	if worked, e := api.ReconcilePortOperation(ctx, f.tenant, operation, uuid.NewString()); e != nil || worked {
		t.Fatal(worked, e)
	}
	if !bytes.Equal(before, authoritySnapshot(t, f.pool, f.tenant, tables)) {
		t.Fatal("no-op replay appended events")
	}
}
func TestPortOperationCreationAuditRollbackAndInvalidationHold(t *testing.T) {
	isolatePackageAuthority(t)
	f := newPortFixture(t)
	ctx := context.Background()
	tables := []string{"port_allocations", "port_reservations", "port_operation_bindings", "desired_state", "operations", "tasks", "outbox", "audit_events", "domain_events"}
	input := f.request(t, 20000)
	request := uuid.NewString()
	failAudit(t, f.pool, request)
	before := authoritySnapshot(t, f.pool, f.tenant, tables)
	if result, e := f.ports.PreparePortReservation(ctx, f.scope, input, request); e == nil || result.ID != "" {
		t.Fatal("failed prepare returned accepted identity", result, e)
	}
	if !bytes.Equal(before, authoritySnapshot(t, f.pool, f.tenant, tables)) {
		t.Fatal("prepare partially persisted")
	}
	result, e := f.ports.PreparePortReservation(ctx, f.scope, f.request(t, 20000), uuid.NewString())
	if e != nil {
		t.Fatal(e)
	}
	operation := portOperationID(t, result)
	api, ok := any(f.ports).(portOperationProjectionAPI)
	if !ok {
		t.Fatal("missing projection consumer")
	}
	if _, e = f.pool.Exec(ctx, `UPDATE port_operation_bindings SET expected_reservations=1 WHERE operation_id=$1`, operation); e == nil {
		t.Fatal("mutable aggregate inventory")
	}
	if e = f.nodeService.SetMembers(ctx, f.scope, f.group, 2, []service.GroupMember{{NodeID: f.nodes[0], Weight: 1}}, uuid.NewString()); e != nil {
		t.Fatal(e)
	}
	if worked, e := api.ReconcilePortOperation(ctx, f.tenant, operation, uuid.NewString()); e != nil || !worked {
		t.Fatal(worked, e)
	}
	var state string
	var held bool
	if e = f.pool.QueryRow(ctx, `SELECT state,occupation_held FROM operations WHERE id=$1`, operation).Scan(&state, &held); e != nil || state != "failed" || !held {
		t.Fatal("stale members freed occupied port", state, held, e)
	}
}

func TestPortOperationNeverReleasesUntilEveryAuthenticatedReleaseProof(t *testing.T) {
	isolatePackageAuthority(t)
	f := newPortFixture(t)
	ctx := context.Background()
	result, e := f.ports.PreparePortReservation(ctx, f.scope, f.request(t, 20000), uuid.NewString())
	if e != nil {
		t.Fatal(e)
	}
	operation := portOperationID(t, result)
	w := worker.PortOperationWorker{Pool: f.pool}
	f.commit(t, result.PortAllocation)
	if worked, e := w.Once(ctx); e != nil || !worked {
		t.Fatal(worked, e)
	}
	if e = f.ports.RequestRelease(ctx, f.scope, result.ID, "explicit", uuid.NewString()); e != nil {
		t.Fatal(e)
	}
	first := f.proof(t, result.PortAllocation, f.nodes[0], "release")
	first.Result = "unknown"
	if e = f.ports.RecordReleaseProof(f.contexts[f.nodes[0]], f.scope, first, uuid.NewString()); e != nil {
		t.Fatal(e)
	}
	// A completed prepare outcome is historical. Unknown release must neither
	// rewrite it nor drop the aggregate occupation flag.
	if worked, e := w.Once(ctx); e != nil || worked {
		t.Fatal("release uncertainty changed proven prepare", worked, e)
	}
	assertHeld := func(want bool) {
		t.Helper()
		var held bool
		var state string
		if e := f.pool.QueryRow(ctx, `SELECT state,occupation_held FROM operations WHERE id=$1`, operation).Scan(&state, &held); e != nil || state != "succeeded" || held != want {
			t.Fatal(state, held, e)
		}
	}
	assertHeld(true)
	first.Result = "released"
	if e = f.ports.RecordReleaseProof(f.contexts[f.nodes[0]], f.scope, first, uuid.NewString()); e != nil {
		t.Fatal(e)
	}
	if worked, e := w.Once(ctx); e != nil || worked {
		t.Fatal("partial child release freed parent", worked, e)
	}
	assertHeld(true)
	last := f.proof(t, result.PortAllocation, f.nodes[1], "release")
	if e = f.ports.RecordReleaseProof(f.contexts[f.nodes[1]], f.scope, last, uuid.NewString()); e != nil {
		t.Fatal(e)
	}
	if worked, e := w.Once(ctx); e != nil || !worked {
		t.Fatal("complete release evidence missing", worked, e)
	}
	assertHeld(false)
	if worked, e := w.Once(ctx); e != nil || worked {
		t.Fatal("projection replay", worked, e)
	}
}
func TestPortOperationRejectsUnprovenFlagsAndIncompleteInventory(t *testing.T) {
	isolatePackageAuthority(t)
	f := newPortFixture(t)
	ctx := context.Background()
	result, e := f.ports.PreparePortReservation(ctx, f.scope, f.request(t, 20000), uuid.NewString())
	if e != nil {
		t.Fatal(e)
	}
	operation := portOperationID(t, result)
	api := any(f.ports).(portOperationProjectionAPI)
	// Existing mutable flags, an expiry or absence are not authenticated FULL
	// receipts. The independent immutable proof rows remain mandatory.
	if _, e = f.pool.Exec(ctx, `UPDATE port_reservations SET config_confirmed=true,state='confirmed' WHERE tenant_id=$1 AND allocation_id=$2`, f.tenant, result.ID); e != nil {
		t.Fatal(e)
	}
	if worked, e := api.ReconcilePortOperation(ctx, f.tenant, operation, uuid.NewString()); e != nil || worked {
		t.Fatal("flags stood in for proof", worked, e)
	}
	if _, e = f.pool.Exec(ctx, `DELETE FROM port_reservations WHERE tenant_id=$1 AND allocation_id=$2 AND node_id=$3 AND protocol='tcp'`, f.tenant, result.ID, f.nodes[0]); e != nil {
		t.Fatal(e)
	}
	if worked, e := api.ReconcilePortOperation(ctx, f.tenant, operation, uuid.NewString()); e != nil || !worked {
		t.Fatal("incomplete original inventory not unknown", worked, e)
	}
	var state string
	var held bool
	if e = f.pool.QueryRow(ctx, `SELECT state,occupation_held FROM operations WHERE id=$1`, operation).Scan(&state, &held); e != nil || state != "unknown" || !held {
		t.Fatal(state, held, e)
	}
}

func TestPortOperationReleasedFlagsWithoutFULLProofStillHold(t *testing.T) {
	isolatePackageAuthority(t)
	f := newPortFixture(t)
	ctx := context.Background()
	result, e := f.ports.PreparePortReservation(ctx, f.scope, f.request(t, 20000), uuid.NewString())
	if e != nil {
		t.Fatal(e)
	}
	operation := portOperationID(t, result)
	if _, e = f.pool.Exec(ctx, `UPDATE port_reservations SET state='released' WHERE tenant_id=$1 AND allocation_id=$2;`, f.tenant, result.ID); e != nil {
		t.Fatal(e)
	}
	if _, e = f.pool.Exec(ctx, `UPDATE port_allocations SET state='released' WHERE tenant_id=$1 AND id=$2`, f.tenant, result.ID); e != nil {
		t.Fatal(e)
	}
	if worked, e := f.ports.ReconcilePortOperation(ctx, f.tenant, operation, uuid.NewString()); e != nil || !worked {
		t.Fatal(worked, e)
	}
	var state string
	var held bool
	if e = f.pool.QueryRow(ctx, `SELECT state,occupation_held FROM operations WHERE id=$1`, operation).Scan(&state, &held); e != nil || state != "failed" || !held {
		t.Fatal("released flags substituted for authenticated proof", state, held, e)
	}
}
