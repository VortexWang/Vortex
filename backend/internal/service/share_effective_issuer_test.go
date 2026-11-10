package service_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"github.com/google/uuid"
	"path/filepath"
	"sync"
	"testing"
	"time"
	"vortex/agent/store"
	"vortex/backend/db/generated"
	"vortex/backend/internal/auth"
	"vortex/backend/internal/policy"
	"vortex/backend/internal/service"
	"vortex/contracts"
)

func TestRecoveredShareIssuerRenewalClosureOriginalProofAndCAS(t *testing.T) {
	x, hs, a, r, _ := admissionFixture(t)
	ctx := context.Background()
	q := generated.New(x.f.pool)
	if _, e := x.s.AdmitHubSameStore(ctx, policyScope(t, x.f.tenant, auth.PlatformAdmin, policy.Grant), r.ID, uuid.NewString()); e != nil {
		t.Fatal(e)
	}
	admitted, _, e := x.s.HubRecoveryAdmission(x.f.contexts[r.HubID], r.HubID, r.ToBootID, shareBackendID, uuid.NewString())
	if e != nil {
		t.Fatal(e)
	}
	if _, e = hs.ApplyHubRecoveryAdmission(ctx, shareBackendID, r.HubID, r.ToBootID, admitted, store.IssuerTime{Earliest: x.now, Latest: x.now, Trusted: true}); e != nil {
		t.Fatal(e)
	}
	original := authoritySnapshot(t, x.f.pool, x.f.tenant, []string{"share_renewal_authorities", "share_generations", "reservations", "ledger_events", "ledger_projections", "hub_funded_allocations"})
	renewal := x.renewal()
	renewal.SignerEpoch = r.ToEpoch
	originalAuthority, e := q.ShareRenewalAuthority(ctx, generated.ShareRenewalAuthorityParams{TenantID: x.f.tenant, NodeID: a.NodeID, Kind: "rate", Generation: 1, SignerID: r.HubID})
	if e != nil {
		t.Fatal(e)
	}
	lease, e := x.s.RenewShare(x.f.contexts[a.NodeID], x.f.scope, renewal, uuid.NewString())
	if e != nil {
		t.Fatal("effective epoch renewal", e)
	}
	if lease.Generation != 1 || !lease.OwnershipUntil.Time.Equal(x.in.OwnershipUntil) {
		t.Fatal("renewal widened original generation/deadline", lease)
	}
	stale := renewal
	stale.Nonce = uuid.NewString()
	stale.SignerEpoch = r.FromEpoch
	if _, e = x.s.RenewShare(x.f.contexts[a.NodeID], x.f.scope, stale, uuid.NewString()); e == nil {
		t.Fatal("old epoch renewed")
	}
	x.in.SignerEpoch = r.ToEpoch
	next := prepareClosureSuccessor(t, x)
	if e = x.s.QueueHandoff(ctx, x.f.scope, next.Generation.ID, uuid.NewString()); !errors.Is(e, service.ErrShareUnknown) {
		t.Fatal("old epoch closure authorized CAS", e)
	}
	tables := []string{"share_renewal_closure_commands", "share_renewal_closure_receipts", "tasks", "operations", "outbox", "audit_events"}
	request := uuid.NewString()
	failAudit(t, x.f.pool, request)
	before := authoritySnapshot(t, x.f.pool, x.f.tenant, tables)
	if e = x.s.PrepareRenewalClosures(ctx, x.f.scope, next.Generation.ID, shareBackendID, request); e == nil {
		t.Fatal("closure escaped failed audit")
	}
	if !bytes.Equal(before, authoritySnapshot(t, x.f.pool, x.f.tenant, tables)) {
		t.Fatal("partial closure barrier")
	}
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			if e := x.s.PrepareRenewalClosures(ctx, x.f.scope, next.Generation.ID, shareBackendID, uuid.NewString()); e != nil {
				t.Error(e)
			}
		})
	}
	wg.Wait()
	commands, e := q.ShareClosureCommands(ctx, generated.ShareClosureCommandsParams{TenantID: x.f.tenant, HandoffID: next.Generation.ID, SignerID: r.HubID})
	if e != nil || len(commands) != 1 {
		t.Fatal(commands, e)
	}
	c, e := contracts.DecodeShareRenewalClosure(commands[0].Payload)
	if e != nil || c.SignerEpoch != r.ToEpoch || c.HubBootID != r.ToBootID || len(c.DelegationIDs) != 1 || c.DelegationIDs[0] != a.DelegationID {
		t.Fatal("wrong effective closure", c, e)
	}
	pending, e := x.s.PendingRenewalClosures(x.f.contexts[r.HubID], r.HubID, r.ToBootID)
	if e != nil || len(pending) != 1 {
		t.Fatal("closed old row suppressed successor delivery", pending, e)
	}
	renewal.Nonce = uuid.NewString()
	if _, e = x.s.RenewShare(x.f.contexts[a.NodeID], x.f.scope, renewal, uuid.NewString()); e == nil {
		t.Fatal("renewed through closure barrier")
	}
	receipt, e := hs.ApplyShareRenewalClosure(ctx, r.HubID, r.ToBootID, shareBackendID, commands[0].Payload)
	if e != nil {
		t.Fatal(e)
	}
	raw, _ := json.Marshal(receipt)
	request = uuid.NewString()
	failAudit(t, x.f.pool, request)
	before = authoritySnapshot(t, x.f.pool, x.f.tenant, tables)
	if e = x.s.RecordRenewalClosureReceipt(x.f.contexts[r.HubID], r.HubID, r.ToBootID, raw, request); e == nil {
		t.Fatal("receipt escaped audit")
	}
	if !bytes.Equal(before, authoritySnapshot(t, x.f.pool, x.f.tenant, tables)) {
		t.Fatal("partial closure receipt")
	}
	for range 8 {
		wg.Go(func() {
			if e := x.s.RecordRenewalClosureReceipt(x.f.contexts[r.HubID], r.HubID, r.ToBootID, raw, uuid.NewString()); e != nil {
				t.Error(e)
			}
		})
	}
	wg.Wait()
	// Generation2 is added, but original generation1 and closed admission proof remain immutable.
	nowOriginal := authoritySnapshot(t, x.f.pool, x.f.tenant, []string{"share_renewal_authorities", "share_generations", "reservations", "ledger_events", "ledger_projections", "hub_funded_allocations"})
	if bytes.Equal(original, nowOriginal) {
		t.Fatal("successor fixture missing")
	}
	financial := authoritySnapshot(t, x.f.pool, x.f.tenant, []string{"share_renewal_authorities", "share_generations", "reservations", "ledger_events", "ledger_projections", "hub_funded_allocations", "share_chains"})
	if e = x.s.QueueHandoff(ctx, x.f.scope, next.Generation.ID, uuid.NewString()); e != nil {
		t.Fatal("complete new epoch closure did not permit CAS", e)
	}
	if !bytes.Equal(financial, authoritySnapshot(t, x.f.pool, x.f.tenant, []string{"share_renewal_authorities", "share_generations", "reservations", "ledger_events", "ledger_projections", "hub_funded_allocations", "share_chains"})) {
		t.Fatal("queue CAS released funds/capacity")
	}
	retainedAuthority, e := q.ShareRenewalAuthority(ctx, generated.ShareRenewalAuthorityParams{TenantID: x.f.tenant, NodeID: a.NodeID, Kind: "rate", Generation: 1, SignerID: r.HubID})
	if e != nil {
		t.Fatal(e)
	}
	oldBytes, _ := json.Marshal(originalAuthority)
	retainedBytes, _ := json.Marshal(retainedAuthority)
	if !bytes.Equal(oldBytes, retainedBytes) {
		t.Fatal("original admitted old epoch proof rewritten")
	}
	node, e := store.Open(filepath.Join(t.TempDir(), "cas.db"), "agent")
	if e != nil {
		t.Fatal(e)
	}
	defer node.Close()
	bootstrap, e := q.ShareHandoffByGeneration(ctx, generated.ShareHandoffByGenerationParams{TenantID: x.f.tenant, NodeID: a.NodeID, Kind: "rate", Generation: 1})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = node.ApplyShareHandoff(ctx, a.NodeID, a.BootID, shareBackendID, "cas", bootstrap.Command, store.IssuerTime{Earliest: x.now, Latest: x.now, Trusted: true}); e != nil {
		t.Fatal(e)
	}
	confirmHandoff(t, x, node, next.Generation.ID)
	if chain := shareChain(t, x); chain.HeldAmount != 300000 || chain.ActiveGeneration != 2 {
		t.Fatal("exact Node CAS capacity difference", chain)
	}
	replay, e := x.s.RenewShare(x.f.contexts[a.NodeID], x.f.scope, func() service.LeaseRequest { v := renewal; v.Nonce = lease.Nonce; return v }(), uuid.NewString())
	if e != nil || replay.ID != lease.ID {
		t.Fatal("original lease exact replay", e)
	}
}

func TestRecoveredShareRenewalRequiresCurrentScopeAndKeepsOriginalDeadlines(t *testing.T) {
	for _, name := range []string{"no_admission", "unsigned", "new_epoch", "wrong_epoch", "node_boot", "fencing", "restriction", "expiry", "audit"} {
		t.Run(name, func(t *testing.T) {
			x, _, a, r, _ := admissionFixture(t)
			ctx := context.Background()
			admin := policyScope(t, x.f.tenant, auth.PlatformAdmin, policy.Grant)
			if name != "no_admission" {
				if _, e := x.s.AdmitHubSameStore(ctx, admin, r.ID, uuid.NewString()); e != nil {
					t.Fatal(e)
				}
			}
			in := x.renewal()
			in.SignerEpoch = r.ToEpoch
			peer := x.f.contexts[a.NodeID]
			request := uuid.NewString()
			switch name {
			case "unsigned":
				peer = ctx
			case "new_epoch":
				if _, e := x.s.AuthorizeSigner(ctx, admin, r.HubID, 9, x.now.Add(-time.Second), x.now.Add(time.Hour), make([]byte, 32), uuid.NewString()); e != nil {
					t.Fatal(e)
				}
			case "wrong_epoch":
				in.SignerEpoch++
			case "node_boot":
				if _, e := x.f.pool.Exec(ctx, "UPDATE nodes SET boot_id=$1 WHERE id=$2", uuid.NewString(), a.NodeID); e != nil {
					t.Fatal(e)
				}
			case "fencing":
				if e := x.s.RecordNodeFencing(x.f.contexts[a.NodeID], x.f.scope, a.NodeID, a.BootID, in.FencingGeneration+1, uuid.NewString()); e != nil {
					t.Fatal(e)
				}
			case "restriction":
				if _, e := x.f.pool.Exec(ctx, "UPDATE tenants SET account_state='suspended' WHERE id=$1", x.f.tenant); e != nil {
					t.Fatal(e)
				}
			case "expiry":
				x.now = x.now.Add(time.Hour)
				in.RequestedAt = x.now.Add(-time.Second)
			case "audit":
				failAudit(t, x.f.pool, request)
			}
			tables := []string{"lease_expectations", "share_chains", "share_generations", "share_renewal_authorities", "tasks", "operations", "outbox", "audit_events"}
			before := authoritySnapshot(t, x.f.pool, x.f.tenant, tables)
			if _, e := x.s.RenewShare(peer, x.f.scope, in, request); e == nil {
				t.Fatal("stale/unapproved renewal", name)
			}
			if !bytes.Equal(before, authoritySnapshot(t, x.f.pool, x.f.tenant, tables)) {
				t.Fatal("failed effective renewal changed authoritative state")
			}
		})
	}
}

func TestRecoveredShareMissingDependencyCannotReopenOriginalClosedRow(t *testing.T) {
	x, _, a, r, _ := admissionFixture(t)
	ctx := context.Background()
	if _, e := x.s.AdmitHubSameStore(ctx, policyScope(t, x.f.tenant, auth.PlatformAdmin, policy.Grant), r.ID, uuid.NewString()); e != nil {
		t.Fatal(e)
	}
	// Corrupt historical candidate fixture only. Detection must not depend on
	// joining the very binding whose absence the consumer must reject.
	if _, e := x.f.pool.Exec(ctx, `ALTER TABLE hub_allocation_share_bindings DISABLE TRIGGER hub_allocation_share_binding_immutable`); e != nil {
		t.Fatal(e)
	}
	if _, e := x.f.pool.Exec(ctx, `DELETE FROM hub_allocation_share_bindings WHERE tenant_id=$1 AND allocation_id=$2 AND kind='rate'`, x.f.tenant, a.AllocationID); e != nil {
		t.Fatal(e)
	}
	in := x.renewal()
	in.SignerEpoch = r.ToEpoch
	if _, e := x.s.RenewShare(x.f.contexts[a.NodeID], x.f.scope, in, uuid.NewString()); e == nil {
		t.Fatal("missing immutable dependency renewed")
	}
	x.in.SignerEpoch = r.ToEpoch
	next := prepareClosureSuccessor(t, x)
	tables := []string{"share_renewal_closure_commands", "tasks", "operations", "outbox", "share_chains", "share_renewal_authorities"}
	before := authoritySnapshot(t, x.f.pool, x.f.tenant, tables)
	if e := x.s.PrepareRenewalClosures(ctx, x.f.scope, next.Generation.ID, shareBackendID, uuid.NewString()); e == nil {
		t.Fatal("missing binding bypassed effective closure")
	}
	if e := x.s.QueueHandoff(ctx, x.f.scope, next.Generation.ID, uuid.NewString()); e == nil {
		t.Fatal("missing binding reused old closure")
	}
	if !bytes.Equal(before, authoritySnapshot(t, x.f.pool, x.f.tenant, tables)) {
		t.Fatal("missing binding released/queued authority")
	}
}
