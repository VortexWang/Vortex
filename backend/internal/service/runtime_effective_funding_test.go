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
	"vortex/agent"
	"vortex/agent/store"
	"vortex/backend/db/generated"
	"vortex/backend/internal/auth"
	"vortex/backend/internal/policy"
	"vortex/backend/internal/service"
	controlv1 "vortex/contracts/protobuf/v1"
	"vortex/hub"
)

func TestEffectiveRuntimeFundingAfterTransferredZeroChildFinalKeepsOriginalShare(t *testing.T) {
	testEffectiveRuntimeFunding(t, false)
}
func TestEffectiveRuntimeFundingAfterInstalledOldCreditFinalKeepsOriginalShare(t *testing.T) {
	testEffectiveRuntimeFunding(t, true)
}
func testEffectiveRuntimeFunding(t *testing.T, installed bool) {
	testEffectiveRuntimeFundingWithHook(t, installed, nil)
}
func testEffectiveRuntimeFundingWithHook(t *testing.T, installed bool, hook func(*shareFixture, *store.Store, *store.Store, hub.Allocation, service.HubSameStoreRecoveryInput, *agent.Node)) {
	t.Helper()
	ctx := context.Background()
	x, in, hs := fundedHubFixture(t)
	q := generated.New(x.f.pool)
	admin := policyScope(t, x.f.tenant, auth.PlatformAdmin, policy.Grant)
	original, e := x.s.PrepareHubAllocation(ctx, x.f.scope, in, uuid.NewString())
	if e != nil {
		t.Fatal(e)
	}
	installAllocationFixture(t, hs, original, x.now)
	row, e := q.GetHubAllocationGlobal(ctx, original.AllocationID)
	if e != nil {
		t.Fatal(e)
	}
	if e = x.s.RecordHubAllocationReceipt(x.f.contexts[in.HubID], in.HubID, in.HubBootID, original.AllocationID, row.PayloadDigest, uuid.NewString()); e != nil {
		t.Fatal(e)
	}
	provision, e := x.s.PrepareNodeSources(ctx, x.f.scope, original.AllocationID, shareBackendID, uuid.NewString())
	if e != nil {
		t.Fatal(e)
	}
	ns, e := store.Open(t.TempDir()+"/existing-node.db", "agent")
	if e != nil {
		t.Fatal(e)
	}
	defer ns.Close()
	p, e := store.DecodeSourceProvision(provision.Payload)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = ns.ApplySourceProvision(ctx, in.NodeID, in.BootID, shareBackendID, provision.Payload, store.IssuerTime{Earliest: p.IssuedAt, Latest: p.IssuedAt, Trusted: true}); e != nil {
		t.Fatal(e)
	}
	var oldCredit *creditCloseFixture
	var oldUsage *controlv1.UsageBatch
	if installed {
		issued, grant, sequence := issueAllocationFixture(t, x, hs, original, 51)
		if e = x.s.RecordHubIssue(x.f.contexts[in.HubID], in.HubID, in.HubBootID, sequence, issued.Payload, uuid.NewString()); e != nil {
			t.Fatal(e)
		}
		node, e := agent.New(in.NodeID, ns, nil)
		if e != nil {
			t.Fatal(e)
		}
		node.BootID = in.BootID
		oldCredit = &creditCloseFixture{x: x, in: in, a: original, hs: hs, st: ns, node: node, issued: issued, grant: grant}
		// Model a real Node installation: the runtime and share leases must be
		// present alongside the credit family before an epoch handoff can prove
		// those three original sources. The credit-only settlement fixture is
		// intentionally narrower than a production AuthorizationUpdate.
		facts, err := agent.InspectAuthorizationUpdate(issued.Payload, in.NodeID, in.BootID, original.PathGeneration)
		if err != nil {
			t.Fatal(err)
		}
		var installedRecords []store.AuthorizationRecord
		for _, a := range facts.Records {
			r := store.AuthorizationRecord{IssuerID: in.HubID, ShareReferenceID: a.ShareReferenceID, DelegationID: a.DelegationID, AuthorizationID: a.AuthorizationID, ParentAuthorizationID: a.ParentAuthorizationID, SourceID: a.SourceID, Kind: a.Kind, RuleID: a.RuleID, RuleRevision: a.RuleRevision, PathGeneration: a.PathGeneration, SignerEpoch: facts.SignerEpoch, BootID: in.BootID, IssuedAt: a.IssuedAt.UTC().Format(time.RFC3339Nano), NotBefore: a.NotBefore.UTC().Format(time.RFC3339Nano), ExpiresAt: a.ExpiresAt.UTC().Format(time.RFC3339Nano), HardDeadline: a.HardDeadline.UTC().Format(time.RFC3339Nano), BillingSnapshotID: a.BillingSnapshotID}
			if a.AmountBytes != nil {
				r.AmountBytes = *a.AmountBytes
			}
			if a.RateBPS != nil {
				r.RateBPS = *a.RateBPS
			}
			if a.Connections != nil {
				r.Connections = *a.Connections
			}
			installedRecords = append(installedRecords, r)
		}
		digest := sha256.Sum256(issued.Payload)
		if err = ns.PersistAuthorizationUpdate(ctx, store.AuthorizationUpdate{ShareReferences: facts.ShareReferences, IssuerID: in.HubID, BackendHeartbeat: facts.BackendHeartbeat, AutonomyDeadline: facts.AutonomyDeadline, NodeID: in.NodeID, BootID: in.BootID, UpdateID: facts.UpdateID, Payload: issued.Payload, Digest: digest[:], SignerEpoch: facts.SignerEpoch, PathGeneration: facts.PathGeneration, FencingGenerationFloor: facts.FencingFloor}, installedRecords, nil); err != nil {
			t.Fatal(err)
		}
		oldUsage = oldCredit.commitLocalUsage(t, 37)
	}
	x.now = x.now.Add(5 * time.Second)
	if _, e = x.s.AuthorizeSigner(ctx, admin, in.HubID, 8, x.now.Add(-time.Second), x.now.Add(time.Hour), make([]byte, 32), uuid.NewString()); e != nil {
		t.Fatal(e)
	}
	r := service.HubSameStoreRecoveryInput{ID: uuid.NewString(), HubID: in.HubID, BackendID: shareBackendID, FromBootID: in.HubBootID, ToBootID: uuid.NewString(), FromEpoch: 7, ToEpoch: 8, Reason: "explicit funded same-store recovery before any child issue"}
	_, raw, records := sameStoreManifest(t, x, hs, r)
	retainSameStore(t, x, r, records)
	if _, e = x.s.VerifyHubSameStorePrefix(x.f.contexts[r.HubID], r.HubID, r.ToBootID, r.ID, raw, uuid.NewString()); e != nil {
		t.Fatal(e)
	}
	advance := func(want string) {
		t.Helper()
		status, e := x.s.AdvanceHubSameStoreRecovery(ctx, admin, r.ID, shareBackendID, uuid.NewString())
		if e != nil || status.Stage != want {
			t.Fatal(status, e)
		}
	}
	advance("awaiting_node_fences")
	fence, e := q.GetNodeRecoveryCreditFenceByAllocation(ctx, generated.GetNodeRecoveryCreditFenceByAllocationParams{TenantID: x.f.tenant, RecoveryID: r.ID, AllocationID: original.AllocationID})
	if e != nil {
		t.Fatal(e)
	}
	recordRecoveryFence(t, x, fence, ns)
	advance("awaiting_node_roots")
	roots, e := q.GetNodeRecoverySourcesByAllocation(ctx, generated.GetNodeRecoverySourcesByAllocationParams{TenantID: x.f.tenant, RecoveryID: r.ID, AllocationID: original.AllocationID})
	if e != nil {
		t.Fatal(e)
	}
	receipt, e := ns.ApplySourceProvision(ctx, in.NodeID, in.BootID, shareBackendID, roots.Payload, store.IssuerTime{Earliest: x.now, Latest: x.now, Trusted: true})
	if e != nil {
		t.Fatal(e)
	}
	if e = x.s.RecordNodeRecoverySources(x.f.contexts[in.NodeID], x.f.scope, receipt, uuid.NewString()); e != nil {
		t.Fatal(e)
	}
	advance("admitted")
	admission, _, e := x.s.HubRecoveryAdmission(x.f.contexts[in.HubID], in.HubID, r.ToBootID, shareBackendID, uuid.NewString())
	if e != nil {
		t.Fatal(e)
	}
	if _, e = hs.ApplyHubRecoveryAdmission(ctx, shareBackendID, in.HubID, r.ToBootID, admission, store.IssuerTime{Earliest: x.now, Latest: x.now, Trusted: true}); e != nil {
		t.Fatal(e)
	}
	if hook != nil {
		hook(x, hs, ns, original, r, oldCredit.node)
	}
	retained := authoritySnapshot(t, x.f.pool, x.f.tenant, []string{"share_chains", "share_generations", "share_renewal_authorities", "rule_artifacts", "rule_compile_inputs"})
	financial := []string{"hub_funded_allocations", "reservations", "ledger_projections", "ledger_events"}
	before := authoritySnapshot(t, x.f.pool, x.f.tenant, financial)
	if e = x.s.FundRuntimeCandidates(ctx, x.f.scope, "phase6", uuid.NewString()); e != nil && !errors.Is(e, service.ErrShareUnknown) {
		t.Fatal(e)
	}
	if !bytes.Equal(before, authoritySnapshot(t, x.f.pool, x.f.tenant, financial)) {
		t.Fatal("unfinalized old parent refilled")
	}
	seal, e := hs.SealHubDelegation(ctx, in.HubID, r.ToBootID, original.DelegationID)
	if e != nil {
		t.Fatal(e)
	}
	sealRaw, _ := json.Marshal(seal)
	journal, e := hs.PendingLedger(ctx, 100)
	if e != nil {
		t.Fatal(e)
	}
	sequence := int64(0)
	for _, record := range journal {
		if bytes.Equal(record.Payload, sealRaw) {
			sequence = record.Sequence
		}
	}
	if e = x.s.RecordDelegationSeal(x.f.contexts[in.HubID], in.HubID, r.ToBootID, sequence, sealRaw, uuid.NewString()); e != nil {
		t.Fatal(e)
	}
	if installed {
		before = authoritySnapshot(t, x.f.pool, x.f.tenant, financial)
		if e = x.s.FundRuntimeCandidates(ctx, x.f.scope, "phase6", uuid.NewString()); e != nil && !errors.Is(e, service.ErrShareUnknown) {
			t.Fatal(e)
		}
		if !bytes.Equal(before, authoritySnapshot(t, x.f.pool, x.f.tenant, financial)) {
			t.Fatal("sealed but unfinalized installed credit refilled")
		}
		if _, e = (service.MeteringService{Pool: x.f.pool}).IngestWireUsage(ctx, in.NodeID, in.BootID, 1, oldUsage); e != nil {
			t.Fatal(e)
		}
		acceptCreditFinal(t, oldCredit)
		before = authoritySnapshot(t, x.f.pool, x.f.tenant, financial)
		if e = x.s.FundRuntimeCandidates(ctx, x.f.scope, "phase6", uuid.NewString()); e != nil && !errors.Is(e, service.ErrShareUnknown) {
			t.Fatal(e)
		}
		if !bytes.Equal(before, authoritySnapshot(t, x.f.pool, x.f.tenant, financial)) {
			t.Fatal("child final alone refilled transferred parent")
		}
		// Corrupt immutable evidence only in this isolated test database. A seal
		// accepted earlier cannot bypass independent final-time chain checks.
		for _, item := range []struct{ table, trigger, column, id string }{
			{"hub_same_store_admissions", "hub_same_store_admission_immutable", "recovery_id", r.ID},
			{"hub_same_store_manifests", "hub_same_store_manifest_immutable", "recovery_id", r.ID},
			{"node_recovery_source_receipts", "node_recovery_source_receipt_immutable", "provision_id", roots.ID},
		} {
			t.Run("final_requires_"+item.table, func(t *testing.T) {
				var saved []byte
				if e := x.f.pool.QueryRow(ctx, "SELECT payload_digest FROM "+item.table+" WHERE "+item.column+"=$1", item.id).Scan(&saved); e != nil {
					t.Fatal(e)
				}
				if _, e := x.f.pool.Exec(ctx, "ALTER TABLE "+item.table+" DISABLE TRIGGER "+item.trigger); e != nil {
					t.Fatal(e)
				}
				corrupt := append([]byte(nil), saved...)
				corrupt[0] ^= 1
				if _, e := x.f.pool.Exec(ctx, "UPDATE "+item.table+" SET payload_digest=$1 WHERE "+item.column+"=$2", corrupt, item.id); e != nil {
					t.Fatal(e)
				}
				defer func() {
					if _, e := x.f.pool.Exec(ctx, "UPDATE "+item.table+" SET payload_digest=$1 WHERE "+item.column+"=$2", saved, item.id); e != nil {
						t.Error(e)
					}
					if _, e := x.f.pool.Exec(ctx, "ALTER TABLE "+item.table+" ENABLE TRIGGER "+item.trigger); e != nil {
						t.Error(e)
					}
				}()
				unchanged := authoritySnapshot(t, x.f.pool, x.f.tenant, []string{"settlements", "ledger_events", "ledger_projections", "tasks", "operations", "outbox", "audit_events"})
				if e := x.s.FinalizeHubAllocation(ctx, x.f.scope, original.AllocationID, uuid.NewString()); e == nil {
					t.Fatal("corrupt effective witness finalized money")
				}
				if !bytes.Equal(unchanged, authoritySnapshot(t, x.f.pool, x.f.tenant, []string{"settlements", "ledger_events", "ledger_projections", "tasks", "operations", "outbox", "audit_events"})) {
					t.Fatal("failed final changed financial/task state")
				}
			})
		}
		request := uuid.NewString()
		failAudit(t, x.f.pool, request)
		before = authoritySnapshot(t, x.f.pool, x.f.tenant, append(append([]string{}, financial...), "settlements", "tasks", "operations", "outbox", "audit_events"))
		if e = x.s.FinalizeHubAllocation(ctx, x.f.scope, original.AllocationID, request); e == nil {
			t.Fatal("transferred final escaped audit")
		}
		if !bytes.Equal(before, authoritySnapshot(t, x.f.pool, x.f.tenant, append(append([]string{}, financial...), "settlements", "tasks", "operations", "outbox", "audit_events"))) {
			t.Fatal("partial transferred parent final")
		}
		if e = x.s.FinalizeHubAllocation(ctx, x.f.scope, original.AllocationID, uuid.NewString()); e != nil {
			t.Fatal("complete transferred parent final", e)
		}
		var finalWG sync.WaitGroup
		for range 8 {
			finalWG.Go(func() {
				if e := x.s.FinalizeHubAllocation(ctx, x.f.scope, original.AllocationID, uuid.NewString()); e != nil {
					t.Error(e)
				}
			})
		}
		finalWG.Wait()
		projection, e := q.GetSourceProjection(ctx, generated.GetSourceProjectionParams{TenantID: x.f.tenant, SourceID: in.SourceID})
		if e != nil || projection.Charged != 37 || projection.F != 0 {
			t.Fatal("old original-source final", projection, e)
		}
	}
	// The existing funding transaction must still roll back the new reservation,
	// immutable allocation and transport work when its final audit fails.
	if _, e = x.f.pool.Exec(ctx, "CREATE FUNCTION reject_effective_refill() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.action='hub.fund_allocation' THEN RAISE EXCEPTION 'injected effective refill audit failure'; END IF; RETURN NEW; END $$; CREATE TRIGGER reject_effective_refill BEFORE INSERT ON audit_events FOR EACH ROW EXECUTE FUNCTION reject_effective_refill()"); e != nil {
		t.Fatal(e)
	}
	atomic := append(append([]string{}, financial...), "tasks", "operations", "outbox")
	before = authoritySnapshot(t, x.f.pool, x.f.tenant, atomic)
	if e = x.s.FundRuntimeCandidates(ctx, x.f.scope, "phase6", uuid.NewString()); e == nil {
		t.Fatal("effective refill escaped audit")
	}
	if !bytes.Equal(before, authoritySnapshot(t, x.f.pool, x.f.tenant, atomic)) {
		t.Fatal("partial effective funding")
	}
	if _, e = x.f.pool.Exec(ctx, "DROP TRIGGER reject_effective_refill ON audit_events;DROP FUNCTION reject_effective_refill()"); e != nil {
		t.Fatal(e)
	}
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			if e := x.s.FundRuntimeCandidates(ctx, x.f.scope, "phase6", uuid.NewString()); e != nil && !errors.Is(e, service.ErrShareUnknown) && !errors.Is(e, service.ErrShareConflict) {
				t.Error(e)
			}
		})
	}
	wg.Wait()
	history, e := q.RuntimeFundingHistory(ctx, generated.RuntimeFundingHistoryParams{TenantID: x.f.tenant, NodeID: in.NodeID, BootID: in.BootID, SnapshotID: in.SnapshotID, PathGeneration: "1", FencingFloor: "3"})
	if e != nil || len(history) != 2 {
		t.Fatal("effective epoch successor missing", history, e)
	}
	var successor hub.Allocation
	for _, h := range history {
		if h.AllocationID != original.AllocationID {
			record, e := q.GetHubAllocationGlobal(ctx, h.AllocationID)
			if e != nil {
				t.Fatal(e)
			}
			successor, e = hub.DecodeAllocation(record.Payload)
			if e != nil {
				t.Fatal(e)
			}
			if h.Closed {
				t.Fatal("fresh parent already final")
			}
		}
	}
	if successor.SignerEpoch != 8 || successor.SourceID == original.SourceID || successor.SnapshotID != original.SnapshotID || successor.RawBytes == 0 || successor.PathGeneration != original.PathGeneration || successor.FencingFloor != original.FencingFloor {
		t.Fatal("widened/reused successor", successor)
	}
	if !bytes.Equal(retained, authoritySnapshot(t, x.f.pool, x.f.tenant, []string{"share_chains", "share_generations", "share_renewal_authorities", "rule_artifacts", "rule_compile_inputs"})) {
		t.Fatal("effective funding rewrote original share or artifact")
	}
	installAllocationFixture(t, hs, successor, x.now)
	issue, _, seq := issueAllocationFixture(t, x, hs, successor, 1)
	if e = x.s.RecordHubIssue(x.f.contexts[in.HubID], in.HubID, r.ToBootID, seq, issue.Payload, uuid.NewString()); e != nil {
		t.Fatal("fresh parent issue under effective epoch", e)
	}
	renewal := x.renewal()
	renewal.SignerEpoch = 8
	if _, e = x.s.RenewShare(x.f.contexts[in.NodeID], x.f.scope, renewal, uuid.NewString()); e != nil {
		t.Fatal("fresh current-epoch parent invalidated admitted share", e)
	}
	before = authoritySnapshot(t, x.f.pool, x.f.tenant, financial)
	for range 3 {
		if e = x.s.FundRuntimeCandidates(ctx, x.f.scope, "phase6", uuid.NewString()); e != nil && !errors.Is(e, service.ErrShareUnknown) {
			t.Fatal(e)
		}
	}
	if !bytes.Equal(before, authoritySnapshot(t, x.f.pool, x.f.tenant, financial)) {
		t.Fatal("unfinalized effective successor repeatedly refilled")
	}
}

func TestEffectiveRuntimeFundingRefusesMissingTransferAndCurrentClosureGuards(t *testing.T) {
	for _, name := range []string{"unadmitted", "missing_binding", "future_epoch", "wrong_boot", "closure_barrier", "audit"} {
		t.Run(name, func(t *testing.T) {
			x, _, a, r := effectiveAllocationFixture(t, name != "unadmitted")
			ctx := context.Background()
			q := generated.New(x.f.pool)
			original, e := q.GetHubAllocationGlobal(ctx, a.AllocationID)
			if e != nil {
				t.Fatal(e)
			}
			parent, e := q.GetUsageReservation(ctx, generated.GetUsageReservationParams{TenantID: x.f.tenant, ID: original.ReservationID})
			if e != nil {
				t.Fatal(e)
			}
			in := service.HubFundingInput{NodeID: a.NodeID, BootID: a.BootID, HubID: r.HubID, HubBootID: r.ToBootID, Region: "phase6", SourceID: parent.SourceID, SnapshotID: a.SnapshotID, Nonce: uuid.NewString(), SignerEpoch: r.ToEpoch, PathGeneration: int64(a.PathGeneration), FencingFloor: int64(a.FencingFloor), RawBytes: 1, ExpiresAt: a.ExpiresAt}
			request := uuid.NewString()
			switch name {
			case "missing_binding":
				if _, e = x.f.pool.Exec(ctx, `ALTER TABLE hub_allocation_share_bindings DISABLE TRIGGER hub_allocation_share_binding_immutable`); e != nil {
					t.Fatal(e)
				}
				if _, e = x.f.pool.Exec(ctx, `DELETE FROM hub_allocation_share_bindings WHERE tenant_id=$1 AND allocation_id=$2 AND kind='rate'`, x.f.tenant, a.AllocationID); e != nil {
					t.Fatal(e)
				}
			case "future_epoch":
				if _, e = x.s.AuthorizeSigner(ctx, policyScope(t, x.f.tenant, auth.PlatformAdmin, policy.Grant), r.HubID, 9, x.now.Add(-time.Second), x.now.Add(time.Hour), make([]byte, 32), uuid.NewString()); e != nil {
					t.Fatal(e)
				}
			case "wrong_boot":
				in.HubBootID = uuid.NewString()
			case "closure_barrier":
				x.in.SignerEpoch = 8
				next := prepareClosureSuccessor(t, x)
				if e = x.s.PrepareRenewalClosures(ctx, x.f.scope, next.Generation.ID, shareBackendID, uuid.NewString()); e != nil {
					t.Fatal(e)
				}
			case "audit":
				failAudit(t, x.f.pool, request)
			}
			tables := []string{"hub_funded_allocations", "hub_allocation_share_bindings", "reservations", "ledger_projections", "ledger_events", "share_generations", "share_chains", "share_renewal_authorities", "tasks", "operations", "outbox", "audit_events"}
			before := authoritySnapshot(t, x.f.pool, x.f.tenant, tables)
			if _, e = x.s.PrepareHubAllocation(ctx, x.f.scope, in, request); e == nil {
				t.Fatal("missing/stale transfer funded")
			}
			if !bytes.Equal(before, authoritySnapshot(t, x.f.pool, x.f.tenant, tables)) {
				t.Fatal("failed funding changed financial/authority state")
			}
		})
	}
}
