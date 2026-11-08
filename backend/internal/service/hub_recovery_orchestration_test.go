package service_test

import (
	"bytes"
	"context"
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
)

func TestExplicitRecoveryOrchestrationWaitsForActualNodeProofAndRetainsOccupation(t *testing.T) {
	x, _, hs, a, r := sameStoreFixture(t)
	ctx := context.Background()
	q := generated.New(x.f.pool)
	admin := policyScope(t, x.f.tenant, auth.PlatformAdmin, policy.Grant)
	if _, e := x.s.PrepareHubSameStoreRecovery(ctx, admin, r, uuid.NewString()); e != nil {
		t.Fatal(e)
	}
	financialTables := []string{"reservations", "ledger_projections", "ledger_events", "hub_credit_grants", "settlements", "share_chains", "share_generations", "hub_funded_allocations"}
	held := authoritySnapshot(t, x.f.pool, x.f.tenant, financialTables)
	advance := func(want string) {
		t.Helper()
		status, e := x.s.AdvanceHubSameStoreRecovery(ctx, admin, r.ID, shareBackendID, uuid.NewString())
		if e != nil || status.Stage != want || status.RecoveryID != r.ID || status.Allocations != 1 {
			t.Fatal("recovery stage", status, e)
		}
	}
	advance("awaiting_hub_manifest")
	for _, scope := range []service.TenantScope{x.f.scope, policyScope(t, x.f.tenant, auth.Operator, policy.Mutate), policyScope(t, x.f.tenant, auth.Auditor, policy.Read)} {
		if _, e := x.s.AdvanceHubSameStoreRecovery(ctx, scope, r.ID, shareBackendID, uuid.NewString()); !errors.Is(e, service.ErrScope) {
			t.Fatal("unauthorized orchestration", e)
		}
	}
	if _, e := x.s.AdvanceHubSameStoreRecovery(ctx, admin, r.ID, uuid.NewString(), uuid.NewString()); !errors.Is(e, service.ErrShareConflict) {
		t.Fatal("wrong backend", e)
	}
	_, raw, records := sameStoreManifest(t, x, hs, r)
	retainSameStore(t, x, r, records)
	if _, e := x.s.VerifyHubSameStorePrefix(x.f.contexts[r.HubID], r.HubID, r.ToBootID, r.ID, raw, uuid.NewString()); e != nil {
		t.Fatal(e)
	}
	tables := []string{"node_recovery_credit_fences", "node_recovery_source_provisions", "hub_same_store_admissions", "tasks", "outbox", "operations", "audit_events"}
	request := uuid.NewString()
	failAudit(t, x.f.pool, request)
	before := authoritySnapshot(t, x.f.pool, x.f.tenant, tables)
	if _, e := x.s.AdvanceHubSameStoreRecovery(ctx, admin, r.ID, shareBackendID, request); e == nil {
		t.Fatal("audit failure produced evidence commands")
	}
	if !bytes.Equal(before, authoritySnapshot(t, x.f.pool, x.f.tenant, tables)) {
		t.Fatal("inventory audit rollback")
	}
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			status, e := x.s.AdvanceHubSameStoreRecovery(ctx, admin, r.ID, shareBackendID, uuid.NewString())
			if e != nil || status.Stage != "awaiting_node_fences" {
				t.Error(status, e)
			}
		})
	}
	wg.Wait()
	fence, e := q.GetNodeRecoveryCreditFenceByAllocation(ctx, generated.GetNodeRecoveryCreditFenceByAllocationParams{TenantID: x.f.tenant, RecoveryID: r.ID, AllocationID: a.AllocationID})
	if e != nil {
		t.Fatal(e)
	}
	if fence.CreatedBy != admin.ActorID() {
		t.Fatal("stored creator was promoted instead of actual caller", fence.CreatedBy)
	}
	advance("awaiting_node_fences")
	if _, e = q.GetNodeRecoverySourcesByAllocation(ctx, generated.GetNodeRecoverySourcesByAllocationParams{TenantID: x.f.tenant, RecoveryID: r.ID, AllocationID: a.AllocationID}); e == nil {
		t.Fatal("root prepared before Node fence")
	}
	ns, e := store.Open(t.TempDir()+"/node.db", "agent")
	if e != nil {
		t.Fatal(e)
	}
	defer ns.Close()
	original, e := q.GetNodeSourceProvision(ctx, generated.GetNodeSourceProvisionParams{TenantID: x.f.tenant, AllocationID: a.AllocationID})
	if e != nil {
		t.Fatal(e)
	}
	p, e := store.DecodeSourceProvision(original.Payload)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = ns.ApplySourceProvision(ctx, a.NodeID, a.BootID, shareBackendID, original.Payload, store.IssuerTime{Earliest: p.IssuedAt, Latest: p.IssuedAt, Trusted: true}); e != nil {
		t.Fatal(e)
	}
	recordRecoveryFence(t, x, fence, ns)
	advance("awaiting_node_roots")
	roots, e := q.GetNodeRecoverySourcesByAllocation(ctx, generated.GetNodeRecoverySourcesByAllocationParams{TenantID: x.f.tenant, RecoveryID: r.ID, AllocationID: a.AllocationID})
	if e != nil || roots.CreatedBy != admin.ActorID() {
		t.Fatal(roots, e)
	}
	advance("awaiting_node_roots")
	receipt, e := ns.ApplySourceProvision(ctx, a.NodeID, a.BootID, shareBackendID, roots.Payload, store.IssuerTime{Earliest: x.now, Latest: x.now, Trusted: true})
	if e != nil {
		t.Fatal(e)
	}
	if e = x.s.RecordNodeRecoverySources(x.f.contexts[a.NodeID], x.f.scope, receipt, uuid.NewString()); e != nil {
		t.Fatal(e)
	}
	for range 8 {
		wg.Go(func() {
			status, e := x.s.AdvanceHubSameStoreRecovery(ctx, admin, r.ID, shareBackendID, uuid.NewString())
			if e != nil || status.Stage != "admitted" {
				t.Error(status, e)
			}
		})
	}
	wg.Wait()
	advance("admitted")
	if !bytes.Equal(held, authoritySnapshot(t, x.f.pool, x.f.tenant, financialTables)) {
		t.Fatal("orchestration released original money/capacity")
	}
	pending, e := hs.PendingLedger(ctx, 1000)
	if e != nil || len(pending) != 2 {
		t.Fatal("PG orchestration ACKed local Hub", pending, e)
	}
	if _, e = hs.IssueHubAuthorization(ctx, store.IssueRequest{ID: uuid.NewString(), HubID: r.HubID, DelegationID: a.DelegationID, NodeID: a.NodeID, BootID: a.BootID, RawBytes: 1, TTL: time.Minute}, store.IssuerTime{Earliest: x.now, Latest: x.now, Trusted: true}); !errors.Is(e, store.ErrAuthorityHandoffRequired) {
		t.Fatal("PG admission fabricated local readiness", e)
	}
}

func TestExplicitRecoveryOrchestrationCommandBudgetResumesWithoutStarvation(t *testing.T) {
	x, in, hs := fundedHubFixture(t)
	ctx := context.Background()
	q := generated.New(x.f.pool)
	admin := policyScope(t, x.f.tenant, auth.PlatformAdmin, policy.Grant)
	for range 33 {
		in.Nonce = uuid.NewString()
		in.RawBytes = 1
		a, e := x.s.PrepareHubAllocation(ctx, x.f.scope, in, uuid.NewString())
		if e != nil {
			t.Fatal(e)
		}
		installAllocationFixture(t, hs, a, x.now)
		row, e := q.GetHubAllocationGlobal(ctx, a.AllocationID)
		if e != nil {
			t.Fatal(e)
		}
		if e = x.s.RecordHubAllocationReceipt(x.f.contexts[in.HubID], in.HubID, in.HubBootID, a.AllocationID, row.PayloadDigest, uuid.NewString()); e != nil {
			t.Fatal(e)
		}
		if _, e = x.s.PrepareNodeSources(ctx, x.f.scope, a.AllocationID, shareBackendID, uuid.NewString()); e != nil {
			t.Fatal(e)
		}
	}
	x.now = x.now.Add(5 * time.Second)
	if _, e := x.s.AuthorizeSigner(ctx, admin, in.HubID, 8, x.now.Add(-time.Second), x.now.Add(time.Hour), make([]byte, 32), uuid.NewString()); e != nil {
		t.Fatal(e)
	}
	r := service.HubSameStoreRecoveryInput{ID: uuid.NewString(), HubID: in.HubID, BackendID: shareBackendID, FromBootID: in.HubBootID, ToBootID: uuid.NewString(), FromEpoch: 7, ToEpoch: 8, Reason: "explicit finite batch progression"}
	_, raw, records := sameStoreManifest(t, x, hs, r)
	retainSameStore(t, x, r, records)
	if _, e := x.s.VerifyHubSameStorePrefix(x.f.contexts[r.HubID], r.HubID, r.ToBootID, r.ID, raw, uuid.NewString()); e != nil {
		t.Fatal(e)
	}
	for i, want := range []int{32, 33, 33} {
		status, e := x.s.AdvanceHubSameStoreRecovery(ctx, admin, r.ID, shareBackendID, uuid.NewString())
		if e != nil || status.Stage != "awaiting_node_fences" || status.Allocations != 33 || status.FencesConfirmed != 0 || status.RootsConfirmed != 0 {
			t.Fatal(i, status, e)
		}
		var n, roots int
		if e = x.f.pool.QueryRow(ctx, `SELECT count(*) FROM node_recovery_credit_fences WHERE recovery_id=$1`, r.ID).Scan(&n); e != nil || n != want {
			t.Fatal("budget or starvation", i, n, e)
		}
		if e = x.f.pool.QueryRow(ctx, `SELECT count(*) FROM node_recovery_source_provisions WHERE recovery_id=$1`, r.ID).Scan(&roots); e != nil || roots != 0 {
			t.Fatal("unproved root", roots, e)
		}
	}
}

func TestExplicitRecoveryOrchestrationRechecksCurrentAdmissionGuards(t *testing.T) {
	for _, name := range []string{"expiry", "clock", "node_boot", "account", "new_epoch", "wrong_tenant", "admit_audit"} {
		t.Run(name, func(t *testing.T) {
			x, _, a, r, _ := admissionFixture(t)
			ctx := context.Background()
			admin := policyScope(t, x.f.tenant, auth.PlatformAdmin, policy.Grant)
			request := uuid.NewString()
			switch name {
			case "expiry":
				x.now = x.now.Add(time.Hour)
			case "clock":
				x.s.Clock = nil
			case "node_boot":
				if _, e := x.f.pool.Exec(ctx, `UPDATE nodes SET boot_id=$1 WHERE id=$2`, uuid.NewString(), a.NodeID); e != nil {
					t.Fatal(e)
				}
			case "account":
				if _, e := x.f.pool.Exec(ctx, `UPDATE tenants SET account_state='suspended' WHERE id=$1`, x.f.tenant); e != nil {
					t.Fatal(e)
				}
			case "new_epoch":
				if _, e := x.s.AuthorizeSigner(ctx, admin, r.HubID, 9, x.now.Add(-time.Second), x.now.Add(time.Hour), make([]byte, 32), uuid.NewString()); e != nil {
					t.Fatal(e)
				}
			case "wrong_tenant":
				admin = policyScope(t, uuid.NewString(), auth.PlatformAdmin, policy.Grant)
			case "admit_audit":
				failAudit(t, x.f.pool, uuid.NewSHA1(uuid.NameSpaceOID, []byte("hub.advance/"+request+"/"+r.ID+"/admit/"+r.ID)).String())
			}
			tables := []string{"hub_same_store_admissions", "share_renewal_authorities", "share_chains", "reservations", "ledger_projections", "ledger_events", "hub_credit_grants", "tasks", "operations", "outbox"}
			before := authoritySnapshot(t, x.f.pool, x.f.tenant, tables)
			if status, e := x.s.AdvanceHubSameStoreRecovery(ctx, admin, r.ID, shareBackendID, request); e == nil {
				t.Fatal("stale proof admitted", status)
			}
			if !bytes.Equal(before, authoritySnapshot(t, x.f.pool, x.f.tenant, tables)) {
				t.Fatal("failed orchestration changed authority")
			}
			inc, e := generated.New(x.f.pool).LatestHubIncarnation(ctx, r.HubID)
			if e != nil || inc.BootID != r.FromBootID || inc.SignerEpoch != r.FromEpoch {
				t.Fatal("failed orchestration rebound boot", inc, e)
			}
		})
	}
}
