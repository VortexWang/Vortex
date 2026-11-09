package service_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"testing"

	"github.com/google/uuid"
	"vortex/agent/store"
	"vortex/backend/db/generated"
	"vortex/backend/internal/service"
	"vortex/backend/internal/worker"
	"vortex/hub"
)

// The inventory is independent of live signing eligibility: every immutable
// parent in the same runtime context must remain visible across epochs, even
// while new signing/funding is correctly blocked by separate authority guards.
func TestRuntimeFundingHistoryDoesNotHideUnknownParentAcrossSignerEpochs(t *testing.T) {
	x, in, hs := fundedHubFixture(t)
	ctx := context.Background()
	q := generated.New(x.f.pool)
	original, e := x.s.PrepareHubAllocation(ctx, x.f.scope, in, uuid.NewString())
	if e != nil {
		t.Fatal(e)
	}
	record, e := q.GetHubAllocationGlobal(ctx, original.AllocationID)
	if e != nil {
		t.Fatal(e)
	}
	// An independently source-funded immutable earlier-epoch row, constructed only
	// in this PG query fixture. No Hub boot/admission or live authority is inferred.
	held, e := (service.MeteringService{Pool: x.f.pool}).ReserveUsage(ctx, x.f.scope, service.ReserveInput{SourceID: in.SourceID, SnapshotID: original.SnapshotID, NodeID: original.NodeID, BootID: original.BootID, Nonce: uuid.NewString(), RawBytes: 1, Earliest: x.now, Latest: x.now, Deadline: original.ExpiresAt}, uuid.NewString())
	if e != nil {
		t.Fatal(e)
	}
	prior := original
	prior.AllocationID = uuid.NewString()
	prior.DelegationID = uuid.NewString()
	prior.SourceID = held.ID
	prior.RawBytes = 1
	prior.SignerEpoch = 6
	prior.RuntimeSourceID = uuid.NewString()
	prior.ShareSourceID = uuid.NewString()
	prior.ShareReferences = append([]store.ShareReference(nil), original.ShareReferences...)
	for i := range prior.ShareReferences {
		prior.ShareReferences[i].SignerEpoch = 6
		if prior.ShareReferences[i].SourceID == original.SourceID {
			prior.ShareReferences[i].SourceID = held.ID
		}
		if prior.ShareReferences[i].SourceID == original.RuntimeSourceID {
			prior.ShareReferences[i].SourceID = prior.RuntimeSourceID
		}
		if prior.ShareReferences[i].SourceID == original.ShareSourceID {
			prior.ShareReferences[i].SourceID = prior.ShareSourceID
		}
		prior.ShareReferences[i].ID = uuid.NewString()
	}
	raw, _ := json.Marshal(prior)
	hash := sha256.Sum256(raw)
	if _, e = hub.DecodeAllocation(raw); e != nil {
		t.Fatal(e)
	}
	if e = q.AppendHubAllocation(ctx, generated.AppendHubAllocationParams{TenantID: x.f.tenant, AllocationID: prior.AllocationID, DelegationID: prior.DelegationID, ReservationID: held.ID, HubID: in.HubID, HubBootID: uuid.NewString(), NodeID: in.NodeID, BootID: in.BootID, RequestKey: uuid.NewString(), RequestDigest: hash[:], Payload: raw, PayloadDigest: hash[:], OperationID: record.OperationID, CreatedBy: record.CreatedBy}); e != nil {
		t.Fatal(e)
	}
	tables := []string{"reservations", "ledger_projections", "ledger_events", "settlements", "hub_funded_allocations", "tasks", "operations", "outbox", "audit_events"}
	before := authoritySnapshot(t, x.f.pool, x.f.tenant, tables)
	input := generated.RuntimeFundingHistoryParams{TenantID: x.f.tenant, NodeID: in.NodeID, BootID: in.BootID, SnapshotID: in.SnapshotID, PathGeneration: "1", FencingFloor: "3"}
	rows, e := q.RuntimeFundingHistory(ctx, input)
	if e != nil || len(rows) != 2 {
		t.Fatal("unknown original parent hidden by epoch", rows, e)
	}
	seen := map[string]bool{}
	for _, r := range rows {
		if r.Closed {
			t.Fatal("new epoch or expiry fabricated financial closure", r)
		}
		seen[r.AllocationID] = true
	}
	if !seen[original.AllocationID] || !seen[prior.AllocationID] {
		t.Fatal("incomplete parent set", seen)
	}
	for _, mutate := range []func(*generated.RuntimeFundingHistoryParams){func(v *generated.RuntimeFundingHistoryParams) { v.BootID = uuid.NewString() }, func(v *generated.RuntimeFundingHistoryParams) { v.NodeID = uuid.NewString() }, func(v *generated.RuntimeFundingHistoryParams) { v.TenantID = uuid.NewString() }, func(v *generated.RuntimeFundingHistoryParams) { v.SnapshotID = uuid.NewString() }, func(v *generated.RuntimeFundingHistoryParams) { v.PathGeneration = "2" }, func(v *generated.RuntimeFundingHistoryParams) { v.FencingFloor = "4" }} {
		other := input
		mutate(&other)
		rows, e = q.RuntimeFundingHistory(ctx, other)
		if e != nil || len(rows) != 0 {
			t.Fatal("inventory crossed independent runtime context", rows, e)
		}
	}
	if !bytes.Equal(before, authoritySnapshot(t, x.f.pool, x.f.tenant, tables)) {
		t.Fatal("inventory read altered financial/authority state")
	}
	// Fully finalize the current epoch's zero-child parent through the actual
	// Hub FULL seal and PG consumer. The earlier unknown parent must still stop
	// the real scheduler from creating a fresh financial reservation.
	installAllocationFixture(t, hs, original, x.now)
	seal, e := hs.SealHubDelegation(ctx, in.HubID, in.HubBootID, original.DelegationID)
	if e != nil {
		t.Fatal(e)
	}
	sealRaw, _ := json.Marshal(seal)
	pending, e := hs.PendingLedger(ctx, 100)
	if e != nil {
		t.Fatal(e)
	}
	sequence := int64(0)
	for _, r := range pending {
		if bytes.Equal(r.Payload, sealRaw) {
			sequence = r.Sequence
		}
	}
	if sequence == 0 {
		t.Fatal("missing FULL seal")
	}
	if e = x.s.RecordDelegationSeal(x.f.contexts[in.HubID], in.HubID, in.HubBootID, sequence, sealRaw, uuid.NewString()); e != nil {
		t.Fatal(e)
	}
	rows, e = q.RuntimeFundingHistory(ctx, input)
	if e != nil || len(rows) != 2 {
		t.Fatal(rows, e)
	}
	for _, r := range rows {
		if r.Closed != (r.AllocationID == original.AllocationID) {
			t.Fatal("wrong parent final classification", r)
		}
	}
	financial := []string{"hub_funded_allocations", "reservations", "ledger_events", "ledger_projections", "settlements"}
	before = authoritySnapshot(t, x.f.pool, x.f.tenant, financial)
	for range 3 {
		if _, e = (worker.RuntimeFundingWorker{Service: x.s, Region: "phase6"}).Once(ctx); e != nil && !errors.Is(e, service.ErrShareUnknown) {
			t.Fatal(e)
		}
	}
	if !bytes.Equal(before, authoritySnapshot(t, x.f.pool, x.f.tenant, financial)) {
		t.Fatal("earlier unknown parent bypassed refill barrier")
	}

}
