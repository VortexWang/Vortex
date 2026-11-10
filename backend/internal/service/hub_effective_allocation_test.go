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
	"vortex/agent"
	"vortex/agent/store"
	"vortex/backend/db/generated"
	"vortex/backend/internal/auth"
	"vortex/backend/internal/policy"
	"vortex/backend/internal/service"
	"vortex/hub"
)

// This fixture explicitly completes the local ACK barrier. Production admission
// transport is a separate pending unit; no status or PG binding substitutes it.
func effectiveAllocationFixture(t *testing.T, admit bool) (*shareFixture, *store.Store, hub.Allocation, service.HubSameStoreRecoveryInput) {
	t.Helper()
	x, hs, a, r, _ := admissionFixture(t)
	ctx := context.Background()
	if admit {
		if _, e := x.s.AdmitHubSameStore(ctx, policyScope(t, x.f.tenant, auth.PlatformAdmin, policy.Grant), r.ID, uuid.NewString()); e != nil {
			t.Fatal(e)
		}
	}
	rows, e := hs.PendingLedger(ctx, 1000)
	if e != nil {
		t.Fatal(e)
	}
	for _, row := range rows {
		if _, e = hs.ConfirmLedger(ctx, row, row.Digest); e != nil {
			t.Fatal(e)
		}
	}
	if ok, e := hs.CompleteRecovery(ctx, store.IssuerTime{Earliest: x.now, Latest: x.now, Trusted: true}); e != nil || !ok {
		t.Fatal("explicit fixture ACK barrier", ok, e)
	}
	return x, hs, a, r
}

func mutateEffectiveEnvelope(t *testing.T, raw []byte, edit func(map[string]any)) []byte {
	t.Helper()
	var p map[string]any
	if e := json.Unmarshal(raw, &p); e != nil {
		t.Fatal(e)
	}
	edit(p)
	b, e := json.Marshal(p)
	if e != nil {
		t.Fatal(e)
	}
	return b
}

func TestHubEffectiveEpochIssueLeaseSealPreserveOriginalParentAndOldReplay(t *testing.T) {
	x, hs, a, r := effectiveAllocationFixture(t, true)
	ctx := context.Background()
	hubctx := x.f.contexts[r.HubID]
	q := generated.New(x.f.pool)
	original, e := q.GetHubAllocationGlobal(ctx, a.AllocationID)
	if e != nil {
		t.Fatal(e)
	}
	oldRows, e := hs.LedgerPrefix(ctx, 0, 1, 10)
	if e != nil || len(oldRows) != 1 {
		t.Fatal(e)
	}
	financial := []string{"reservations", "ledger_events", "ledger_projections", "share_chains", "share_generations", "hub_funded_allocations", "node_source_provisions"}
	before := authoritySnapshot(t, x.f.pool, x.f.tenant, financial)
	issued, _, sequence := issueAllocationFixture(t, x, hs, a, 10)
	facts, e := agent.InspectAuthorizationUpdate(issued.Payload, a.NodeID, a.BootID, a.PathGeneration)
	if e != nil || facts.SignerEpoch != uint64(r.ToEpoch) || len(facts.Handoffs) != 3 || facts.Handoffs[0].Remaining != 66 || facts.ShareReferences[0].Revision != a.ShareReferences[0].Revision+1 {
		t.Fatal("actual Hub did not carry original R and next roots", facts, e)
	}
	atomicTables := []string{"hub_issue_receipts", "hub_credit_grants", "operations", "tasks", "outbox", "audit_events"}
	atomic := authoritySnapshot(t, x.f.pool, x.f.tenant, atomicTables)
	request := uuid.NewString()
	failAudit(t, x.f.pool, request)
	if e = x.s.RecordHubIssue(hubctx, r.HubID, r.ToBootID, sequence, issued.Payload, request); e == nil {
		t.Fatal("new-epoch issue escaped failed audit")
	}
	if !bytes.Equal(atomic, authoritySnapshot(t, x.f.pool, x.f.tenant, atomicTables)) {
		t.Fatal("partial new-epoch issue")
	}
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			if e := x.s.RecordHubIssue(hubctx, r.HubID, r.ToBootID, sequence, issued.Payload, uuid.NewString()); e != nil {
				t.Error(e)
			}
		})
	}
	wg.Wait()
	total, e := q.HubAllocationIssued(ctx, generated.HubAllocationIssuedParams{TenantID: x.f.tenant, AllocationID: a.AllocationID})
	if e != nil || total != 47 {
		t.Fatal("epochs did not share one raw cap", total, e)
	}
	if !bytes.Equal(before, authoritySnapshot(t, x.f.pool, x.f.tenant, financial)) {
		t.Fatal("epoch issue reserved another parent or rewrote original capacity")
	}
	when := x.now.Add(62 * time.Second)
	for _, seconds := range []int{20, 40, 60, 62} {
		if e = hs.RecordBackendHeartbeat(ctx, x.now.Add(time.Duration(seconds)*time.Second), a.AutonomyDeadline); e != nil {
			t.Fatal(e)
		}
	}
	for _, refresh := range []func(context.Context, string, string, string, string, store.IssuerTime) (store.IssuedAuthorization, error){hs.RefreshHubRuntimeHeartbeat, hs.RefreshHubDelegationLeases} {
		lease, e := refresh(ctx, r.HubID, a.NodeID, a.BootID, a.DelegationID, store.IssuerTime{Earliest: when, Latest: when, Trusted: true})
		if e != nil || len(lease.Payload) == 0 {
			t.Fatal("new-epoch runtime refresh", e)
		}
	}
	rows, e := hs.PendingLedger(ctx, 1000)
	if e != nil || len(rows) != 3 {
		t.Fatal(rows, e)
	}
	for _, row := range rows {
		if row.Kind != "runtime-lease" {
			continue
		}
		for _, negative := range []string{"missing_proofs", "remaining", "old_boot"} {
			boot := r.ToBootID
			changed := mutateEffectiveEnvelope(t, row.Payload, func(p map[string]any) {
				switch negative {
				case "missing_proofs":
					delete(p, "epoch_handoffs")
				case "remaining":
					p["epoch_handoffs"].([]any)[1].(map[string]any)["remaining_bytes"] = 1
				case "old_boot":
					boot = r.FromBootID
				}
			})
			if e = x.s.RecordHubRuntimeLease(hubctx, r.HubID, boot, row.Sequence, changed, uuid.NewString()); e == nil {
				t.Fatal("invalid lease handoff", negative)
			}
		}
		before := authoritySnapshot(t, x.f.pool, x.f.tenant, []string{"hub_runtime_lease_receipts", "operations", "tasks", "outbox", "audit_events"})
		request := uuid.NewString()
		failAudit(t, x.f.pool, request)
		if e = x.s.RecordHubRuntimeLease(hubctx, r.HubID, r.ToBootID, row.Sequence, row.Payload, request); e == nil {
			t.Fatal("new-epoch lease escaped failed audit")
		}
		if !bytes.Equal(before, authoritySnapshot(t, x.f.pool, x.f.tenant, []string{"hub_runtime_lease_receipts", "operations", "tasks", "outbox", "audit_events"})) {
			t.Fatal("partial new-epoch lease")
		}
		if e = x.s.RecordHubRuntimeLease(hubctx, r.HubID, r.ToBootID, row.Sequence, row.Payload, uuid.NewString()); e != nil {
			t.Fatal("new-epoch lease rejected", e)
		}
	}
	if !bytes.Equal(before, authoritySnapshot(t, x.f.pool, x.f.tenant, financial)) {
		t.Fatal("new-epoch refresh changed original funds or capacity")
	}
	seal, e := hs.SealHubDelegation(ctx, r.HubID, r.ToBootID, a.DelegationID)
	if e != nil || seal.SignerEpoch != r.ToEpoch || seal.Issued != 47 || seal.Unissued != 56 {
		t.Fatal(seal, e)
	}
	raw, _ := json.Marshal(seal)
	stale := seal
	stale.SignerEpoch, stale.HubBootID = r.FromEpoch, r.FromBootID
	staleRaw, _ := json.Marshal(stale)
	if e = x.s.RecordDelegationSeal(hubctx, r.HubID, r.FromBootID, rows[len(rows)-1].Sequence+1, staleRaw, uuid.NewString()); e == nil {
		t.Fatal("old boot sealed new-epoch inventory")
	}

	sealSequence := rows[len(rows)-1].Sequence + 1
	sealTables := []string{"hub_delegation_seals", "settlements", "ledger_events", "ledger_projections", "operations", "tasks", "outbox", "audit_events"}
	prior := authoritySnapshot(t, x.f.pool, x.f.tenant, sealTables)
	request = uuid.NewString()
	failAudit(t, x.f.pool, request)
	if e = x.s.RecordDelegationSeal(hubctx, r.HubID, r.ToBootID, sealSequence, raw, request); e == nil {
		t.Fatal("new-epoch seal escaped failed audit")
	}
	if !bytes.Equal(prior, authoritySnapshot(t, x.f.pool, x.f.tenant, sealTables)) {
		t.Fatal("partial new-epoch return")
	}
	for range 8 {
		wg.Go(func() {
			if e := x.s.RecordDelegationSeal(hubctx, r.HubID, r.ToBootID, sealSequence, raw, uuid.NewString()); e != nil {
				t.Error(e)
			}
		})
	}
	wg.Wait()
	accounting, e := q.ReservationAccounting(ctx, generated.ReservationAccountingParams{TenantID: x.f.tenant, GrantID: a.SourceID})
	if e != nil || accounting.Terminal || accounting.ReturnedRaw != 56 {
		t.Fatal("seal released unknown old/new children", accounting, e)
	}
	reservation, e := q.GetUsageReservation(ctx, generated.GetUsageReservationParams{TenantID: x.f.tenant, ID: a.SourceID})
	if e != nil {
		t.Fatal(e)
	}
	projection, e := q.GetSourceProjection(ctx, generated.GetSourceProjectionParams{TenantID: x.f.tenant, SourceID: reservation.SourceID})
	if e != nil || projection.F != 47 {
		t.Fatal("cross-epoch original financial hold", projection, e)
	}
	unchanged, e := q.GetHubAllocationGlobal(ctx, a.AllocationID)
	if e != nil || !bytes.Equal(original.Payload, unchanged.Payload) {
		t.Fatal("rewrote original allocation", e)
	}
	for _, row := range rows[1:] {
		fresh := mutateEffectiveEnvelope(t, row.Payload, func(p map[string]any) { p["update_id"] = uuid.NewString() })
		if e = x.s.RecordHubRuntimeLease(hubctx, r.HubID, r.ToBootID, sealSequence+1, fresh, uuid.NewString()); e == nil {
			t.Fatal("sealed allocation accepted fresh runtime rights")
		}
	}
	x.s.Clock = nil
	if e = x.s.RecordHubIssue(hubctx, r.HubID, r.FromBootID, oldRows[0].Sequence, oldRows[0].Payload, uuid.NewString()); e != nil {
		t.Fatal("exact historical old-boot replay", e)
	}
	if e = x.s.RecordHubIssue(hubctx, r.HubID, r.ToBootID, sequence, issued.Payload, uuid.NewString()); e != nil {
		t.Fatal("exact new-epoch replay after seal", e)
	}
	for _, row := range rows[1:] {
		if e = x.s.RecordHubRuntimeLease(hubctx, r.HubID, r.ToBootID, row.Sequence, row.Payload, uuid.NewString()); e != nil {
			t.Fatal("exact runtime replay after seal", e)
		}
	}
	if e = x.s.RecordDelegationSeal(hubctx, r.HubID, r.ToBootID, sealSequence, raw, uuid.NewString()); e != nil {
		t.Fatal("exact seal replay", e)
	}
}

func TestHubEffectiveEpochRequiresAdmissionAndExactOriginalHandoff(t *testing.T) {
	for _, admitted := range []bool{false, true} {
		t.Run(map[bool]string{false: "approval_only", true: "admitted"}[admitted], func(t *testing.T) {
			x, hs, a, r := effectiveAllocationFixture(t, admitted)
			issued, _, sequence := issueAllocationFixture(t, x, hs, a, 1)
			ctx := x.f.contexts[r.HubID]
			if !admitted {
				if e := x.s.RecordHubIssue(ctx, r.HubID, r.ToBootID, sequence, issued.Payload, uuid.NewString()); e == nil {
					t.Fatal("retention/approval alone granted new epoch")
				}
				return
			}
			for _, name := range []string{"missing_proofs", "credit_R", "runtime_R", "manifest", "transfer", "revision", "source", "deadline", "over_original_cap", "old_boot"} {
				t.Run(name, func(t *testing.T) {
					boot := r.ToBootID
					raw := mutateEffectiveEnvelope(t, issued.Payload, func(p map[string]any) {
						h := p["epoch_handoffs"].([]any)
						switch name {
						case "missing_proofs":
							delete(p, "epoch_handoffs")
						case "credit_R":
							h[0].(map[string]any)["remaining_bytes"] = 67
						case "runtime_R":
							h[1].(map[string]any)["remaining_bytes"] = 1
						case "manifest":
							h[0].(map[string]any)["record_sha256"] = string(bytes.Repeat([]byte("0"), 64))
						case "transfer":
							h[0].(map[string]any)["transfer_id"] = uuid.NewString()
						case "revision":
							p["share_references"].([]any)[0].(map[string]any)["revision"] = 99
						case "source":
							p["authorizations"].([]any)[0].(map[string]any)["source_id"] = uuid.NewString()
						case "deadline":
							p["share_references"].([]any)[0].(map[string]any)["expires_at"] = a.ExpiresAt.Add(time.Minute).Format(time.RFC3339Nano)
						case "over_original_cap":
							for _, record := range p["authorizations"].([]any) {
								v := record.(map[string]any)
								if v["kind"] == "credit_grant" {
									v["amount_bytes"] = 67
								}
							}
						case "old_boot":
							boot = r.FromBootID
						}
					})
					tables := []string{"hub_issue_receipts", "hub_credit_grants", "reservations", "ledger_events", "ledger_projections", "operations", "tasks", "outbox", "audit_events"}
					before := authoritySnapshot(t, x.f.pool, x.f.tenant, tables)
					e := x.s.RecordHubIssue(ctx, r.HubID, boot, sequence, raw, uuid.NewString())
					if e == nil || name == "over_original_cap" && !errors.Is(e, service.ErrShareCapacity) {
						t.Fatal("invalid new-epoch issue accepted", name, e)
					}
					if !bytes.Equal(before, authoritySnapshot(t, x.f.pool, x.f.tenant, tables)) {
						t.Fatal("invalid issue mutated authority", name)
					}
				})
			}
			old, e := hs.LedgerPrefix(context.Background(), 0, 1, 10)
			if e != nil {
				t.Fatal(e)
			}
			freshOld := mutateEffectiveEnvelope(t, old[0].Payload, func(p map[string]any) { p["update_id"] = uuid.NewString() })
			if e = x.s.RecordHubIssue(ctx, r.HubID, r.FromBootID, 100, freshOld, uuid.NewString()); e == nil {
				t.Fatal("old boot appended fresh issue after admission")
			}
		})
	}
}
