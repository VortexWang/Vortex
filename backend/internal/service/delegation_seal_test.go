package service_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"testing"
	"vortex/contracts"

	"github.com/google/uuid"
	"vortex/backend/db/generated"
	"vortex/backend/internal/service"
)

func TestDelegationSealInventoryGateAuditRollbackReturnsOnlyNeverIssued(t *testing.T) {
	x, in, hs := fundedHubFixtureMultiplier(t, 1200)
	ctx := context.Background()
	in.RawBytes = 103
	a, e := x.s.PrepareHubAllocation(ctx, x.f.scope, in, uuid.NewString())
	if e != nil {
		t.Fatal(e)
	}
	installAllocationFixture(t, hs, a, x.now)
	one, grant, seq := issueAllocationFixture(t, x, hs, a, 51)
	seal, e := hs.SealHubDelegation(ctx, in.HubID, in.HubBootID, a.DelegationID)
	if e != nil {
		t.Fatal(e)
	}
	raw, _ := json.Marshal(seal)
	records, e := hs.PendingLedger(ctx, 100)
	if e != nil || len(records) != 2 {
		t.Fatal(e, len(records))
	}
	sealSeq := records[1].Sequence
	if e = x.s.RecordDelegationSeal(x.f.contexts[in.HubID], in.HubID, in.HubBootID, sealSeq, raw, uuid.NewString()); !errors.Is(e, service.ErrShareUnknown) {
		t.Fatal("seal accepted before issue mapping", e)
	}
	if e = x.s.RecordHubIssue(x.f.contexts[in.HubID], in.HubID, in.HubBootID, seq, one.Payload, uuid.NewString()); e != nil {
		t.Fatal(e)
	}
	q := generated.New(x.f.pool)
	p, e := q.GetSourceProjection(ctx, generated.GetSourceProjectionParams{TenantID: x.f.tenant, SourceID: in.SourceID})
	if e != nil {
		t.Fatal(e)
	}
	request := uuid.NewString()
	failAudit(t, x.f.pool, request)
	tables := []string{"hub_delegation_seals", "settlements", "ledger_events", "ledger_projections", "operations", "tasks", "outbox", "audit_events"}
	before := authoritySnapshot(t, x.f.pool, x.f.tenant, tables)
	if e = x.s.RecordDelegationSeal(x.f.contexts[in.HubID], in.HubID, in.HubBootID, sealSeq, raw, request); e == nil {
		t.Fatal("failed audit returned money")
	}
	if !bytes.Equal(before, authoritySnapshot(t, x.f.pool, x.f.tenant, tables)) {
		t.Fatal("partial seal/return")
	}
	for i := 0; i < 2; i++ {
		if e = x.s.RecordDelegationSeal(x.f.contexts[in.HubID], in.HubID, in.HubBootID, sealSeq, raw, uuid.NewString()); e != nil {
			t.Fatal(e)
		}
	}
	after, e := q.GetSourceProjection(ctx, generated.GetSourceProjectionParams{TenantID: x.f.tenant, SourceID: in.SourceID})
	if e != nil {
		t.Fatal(e)
	}
	// ceil(103*1.2)=124; ceil(51*1.2)=62. Only unissued52 can return.
	if p.F-after.F != 62 || after.F != 62 || after.B != p.B || after.Charged != 0 {
		t.Fatal("returned issued/rounded debt", p, after)
	}
	accounting, e := q.ReservationAccounting(ctx, generated.ReservationAccountingParams{TenantID: x.f.tenant, GrantID: a.SourceID})
	if e != nil || accounting.Terminal || accounting.ReturnedRaw != 52 {
		t.Fatal(e, accounting)
	}
	// Expiry/seal is not a child final receipt and cannot return those51 bytes.
	meter := service.MeteringService{Pool: x.f.pool}
	if e = meter.RecordFinalReceipt(ctx, x.f.scope, a.SourceID, a.SnapshotID, "fabricated-child-stop", 0, make([]byte, 32), uuid.NewString()); !errors.Is(e, service.ErrShareUnknown) {
		t.Fatal("parent finalized without child closure", e)
	}
	// The old issued payload can still reach PG after a lost ACK, without a new partition.
	if e = x.s.RecordHubIssue(x.f.contexts[in.HubID], in.HubID, in.HubBootID, seq, one.Payload, uuid.NewString()); e != nil {
		t.Fatal("old issue replay denied", e)
	}
	t.Log("sealed parent retains issued child", grant, "and original fractional accounting")
}
func TestDelegationSealZeroIssueProofSafelyReturnsWholeParent(t *testing.T) {
	x, in, hs := fundedHubFixture(t)
	ctx := context.Background()
	a, e := x.s.PrepareHubAllocation(ctx, x.f.scope, in, uuid.NewString())
	if e != nil {
		t.Fatal(e)
	}
	installAllocationFixture(t, hs, a, x.now)
	seal, e := hs.SealHubDelegation(ctx, in.HubID, in.HubBootID, a.DelegationID)
	if e != nil {
		t.Fatal(e)
	}
	raw, _ := json.Marshal(seal)
	if e = x.s.RecordDelegationSeal(x.f.contexts[in.HubID], in.HubID, in.HubBootID, 1, raw, uuid.NewString()); e != nil {
		t.Fatal(e)
	}
	q := generated.New(x.f.pool)
	accounting, e := q.ReservationAccounting(ctx, generated.ReservationAccountingParams{TenantID: x.f.tenant, GrantID: a.SourceID})
	if e != nil || !accounting.Terminal {
		t.Fatal(e, accounting)
	}
	p, e := q.GetSourceProjection(ctx, generated.GetSourceProjectionParams{TenantID: x.f.tenant, SourceID: in.SourceID})
	if e != nil || p.F != 0 || p.Charged != 0 {
		t.Fatal(e, p)
	}
	// The seal itself is durable issuer evidence even if the earlier installation
	// ACK was lost. An empty Hub DB must not receive and refill that delegation.
	pending, e := x.s.PendingHubAllocations(x.f.contexts[in.HubID], in.HubID, in.HubBootID)
	if e != nil || len(pending) != 0 {
		t.Fatal("sealed allocation remained deliverable without installation ACK", e, len(pending))
	}
	evidence, e := q.HubBootHasIssuerEvidence(ctx, generated.HubBootHasIssuerEvidenceParams{HubID: in.HubID, HubBootID: in.HubBootID})
	if e != nil || !evidence {
		t.Fatal("seal failed to fence empty-state boot reuse", e, evidence)
	}
}

func TestDelegationSealPreservesAlreadyChargedAndUnknownChildHold(t *testing.T) {
	x, in, hs := fundedHubFixtureMultiplier(t, 1200)
	ctx := context.Background()
	in.RawBytes = 103
	a, e := x.s.PrepareHubAllocation(ctx, x.f.scope, in, uuid.NewString())
	if e != nil {
		t.Fatal(e)
	}
	installAllocationFixture(t, hs, a, x.now)
	issued, _, sequence := issueAllocationFixture(t, x, hs, a, 51)
	if e = x.s.RecordHubIssue(x.f.contexts[in.HubID], in.HubID, in.HubBootID, sequence, issued.Payload, uuid.NewString()); e != nil {
		t.Fatal(e)
	}
	meter := service.MeteringService{Pool: x.f.pool}
	proof := sha256.Sum256([]byte("actual9 bytes authenticated fixture"))
	if e = meter.CommitActualUsage(ctx, x.f.scope, a.SourceID, a.SnapshotID, "actual9", 9, proof[:], uuid.NewString()); e != nil {
		t.Fatal(e)
	}
	seal, e := hs.SealHubDelegation(ctx, in.HubID, in.HubBootID, a.DelegationID)
	if e != nil {
		t.Fatal(e)
	}
	raw, _ := json.Marshal(seal)
	if e = x.s.RecordDelegationSeal(x.f.contexts[in.HubID], in.HubID, in.HubBootID, sequence+1, raw, uuid.NewString()); e != nil {
		t.Fatal(e)
	}
	p, e := generated.New(x.f.pool).GetSourceProjection(ctx, generated.GetSourceProjectionParams{TenantID: x.f.tenant, SourceID: in.SourceID})
	if e != nil || p.F != 52 || p.Charged != 10 || p.RemainderMilli != 800 {
		t.Fatal("seal reset charged usage or remainder", e, p)
	}
	bad := seal
	bad.IssueDigest = contracts.SharePayloadDigest([]byte("same-total replacement"))
	forged, _ := json.Marshal(bad)
	if e = x.s.RecordDelegationSeal(x.f.contexts[in.HubID], in.HubID, in.HubBootID, sequence+1, forged, uuid.NewString()); !errors.Is(e, service.ErrShareConflict) {
		t.Fatal("sealed inventory rebound", e)
	}
	if e = x.s.RecordDelegationSeal(ctx, in.HubID, in.HubBootID, sequence+1, raw, uuid.NewString()); !errors.Is(e, service.ErrScope) {
		t.Fatal("unsigned seal", e)
	}
	if e = x.s.RecordDelegationSeal(x.f.contexts[in.HubID], in.HubID, uuid.NewString(), sequence+1, raw, uuid.NewString()); !errors.Is(e, service.ErrShareConflict) {
		t.Fatal("wrong boot seal", e)
	}
	// Delayed actual usage is still chargeable against the retained issued hold.
	if e = meter.CommitActualUsage(ctx, x.f.scope, a.SourceID, a.SnapshotID, "delayed51", 51, proof[:], uuid.NewString()); e != nil {
		t.Fatal("seal blocked delayed actual usage", e)
	}
	p, e = generated.New(x.f.pool).GetSourceProjection(ctx, generated.GetSourceProjectionParams{TenantID: x.f.tenant, SourceID: in.SourceID})
	if e != nil || p.F != 1 || p.Charged != 61 || p.RemainderMilli != 200 {
		t.Fatal("delayed usage changed parent rounding or released unknown tail", e, p)
	}
}

func TestHubIssueWithoutInstallReceiptStillFencesEmptyBootEvidence(t *testing.T) {
	x, in, hs := fundedHubFixture(t)
	ctx := context.Background()
	a, err := x.s.PrepareHubAllocation(ctx, x.f.scope, in, uuid.NewString())
	if err != nil {
		t.Fatal(err)
	}
	installAllocationFixture(t, hs, a, x.now)
	issued, _, sequence := issueAllocationFixture(t, x, hs, a, 51)
	if err = x.s.RecordHubIssue(x.f.contexts[in.HubID], in.HubID, in.HubBootID, sequence, issued.Payload, uuid.NewString()); err != nil {
		t.Fatal(err)
	}
	evidence, err := generated.New(x.f.pool).HubBootHasIssuerEvidence(ctx, generated.HubBootHasIssuerEvidenceParams{HubID: in.HubID, HubBootID: in.HubBootID})
	if err != nil || !evidence {
		t.Fatal("accepted issue was not issuer evidence", err, evidence)
	}
}
