package service_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"github.com/google/uuid"
	"sync"
	"testing"
	"time"
	"vortex/agent/store"
	"vortex/backend/db/generated"
	"vortex/backend/internal/auth"
	"vortex/backend/internal/policy"
	"vortex/backend/internal/service"
)

func recordRecoveryFence(t *testing.T, x *shareFixture, f generated.NodeRecoveryCreditFence, st *store.Store) {
	t.Helper()
	r, e := st.ApplyRecoveryCreditFence(context.Background(), f.NodeID, f.BootID, f.BackendID, f.Payload, store.IssuerTime{Earliest: x.now, Latest: x.now, Trusted: true})
	if e != nil {
		t.Fatal(e)
	}
	raw, _ := json.Marshal(r)
	if e = x.s.RecordNodeRecoveryCreditFence(x.f.contexts[f.NodeID], x.f.scope, raw, uuid.NewString()); e != nil {
		t.Fatal(e)
	}
}
func TestNodeRecoverySourcesRequireFULLFenceAndAtomicExactRoots(t *testing.T) {
	x, f, st := preparedCreditFence(t)
	ctx := context.Background()
	admin := policyScope(t, x.f.tenant, auth.PlatformAdmin, policy.Grant)
	if _, e := x.s.PrepareNodeRecoverySources(ctx, admin, f.RecoveryID, f.AllocationID, uuid.NewString()); e == nil {
		t.Fatal("unconfirmed fence created roots")
	}
	recordRecoveryFence(t, x, f, st)
	if _, e := x.s.PrepareNodeRecoverySources(ctx, x.f.scope, f.RecoveryID, f.AllocationID, uuid.NewString()); !errors.Is(e, service.ErrScope) {
		t.Fatal(e)
	}
	heldTables := []string{"reservations", "ledger_projections", "ledger_events", "hub_credit_grants", "share_chains", "share_generations", "share_renewal_authorities", "settlements", "hub_funded_allocations", "node_source_provisions", "node_source_receipts"}
	held := authoritySnapshot(t, x.f.pool, x.f.tenant, heldTables)
	atomic := []string{"node_recovery_source_provisions", "operations", "tasks", "outbox", "audit_events"}
	before := authoritySnapshot(t, x.f.pool, x.f.tenant, atomic)
	request := uuid.NewString()
	failAudit(t, x.f.pool, request)
	if _, e := x.s.PrepareNodeRecoverySources(ctx, admin, f.RecoveryID, f.AllocationID, request); e == nil {
		t.Fatal("audit failure committed roots")
	}
	if !bytes.Equal(before, authoritySnapshot(t, x.f.pool, x.f.tenant, atomic)) {
		t.Fatal("partial command")
	}
	var wg sync.WaitGroup
	results := make(chan generated.NodeRecoverySourceProvision, 8)
	errs := make(chan error, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			p, e := x.s.PrepareNodeRecoverySources(ctx, admin, f.RecoveryID, f.AllocationID, uuid.NewString())
			results <- p
			errs <- e
		}()
	}
	wg.Wait()
	close(results)
	close(errs)
	for e := range errs {
		if e != nil {
			t.Fatal(e)
		}
	}
	var p generated.NodeRecoverySourceProvision
	for item := range results {
		if p.ID != "" && !bytes.Equal(p.Payload, item.Payload) {
			t.Fatal("concurrent roots diverged")
		}
		p = item
	}
	q := generated.New(x.f.pool)
	original, e := q.GetNodeSourceProvision(ctx, generated.GetNodeSourceProvisionParams{TenantID: x.f.tenant, AllocationID: f.AllocationID})
	if e != nil {
		t.Fatal(e)
	}
	old, e := store.DecodeSourceProvision(original.Payload)
	if e != nil {
		t.Fatal(e)
	}
	next, e := store.DecodeSourceProvision(p.Payload)
	if e != nil {
		t.Fatal(e)
	}
	if next.ID == old.ID || next.BackendID != old.BackendID || next.NodeID != old.NodeID || next.BootID != old.BootID {
		t.Fatal("rebound original provision", next)
	}
	for i, root := range old.Roots {
		root.SignerEpoch++
		root.Revision++
		if root != next.Roots[i] {
			t.Fatal("changed original root limits/source/deadline", root, next.Roots[i])
		}
	}
	receipt, e := st.ApplySourceProvision(ctx, p.NodeID, p.BootID, p.BackendID, p.Payload, store.IssuerTime{Earliest: x.now, Latest: x.now, Trusted: true})
	if e != nil {
		t.Fatal(e)
	}
	if e = x.s.RecordNodeRecoverySources(ctx, x.f.scope, receipt, uuid.NewString()); e == nil {
		t.Fatal("unauthenticated receipt accepted")
	}
	bad := receipt
	bad.Digest = "bad"
	if e = x.s.RecordNodeRecoverySources(x.f.contexts[p.NodeID], x.f.scope, bad, uuid.NewString()); !errors.Is(e, service.ErrStaleReceipt) {
		t.Fatal(e)
	}
	atomic = []string{"node_recovery_source_receipts", "operations", "tasks", "outbox", "audit_events"}
	before = authoritySnapshot(t, x.f.pool, x.f.tenant, atomic)
	request = uuid.NewString()
	failAudit(t, x.f.pool, request)
	if e = x.s.RecordNodeRecoverySources(x.f.contexts[p.NodeID], x.f.scope, receipt, request); e == nil {
		t.Fatal("audit failure committed receipt")
	}
	if !bytes.Equal(before, authoritySnapshot(t, x.f.pool, x.f.tenant, atomic)) {
		t.Fatal("partial receipt")
	}
	errs = make(chan error, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs <- x.s.RecordNodeRecoverySources(x.f.contexts[p.NodeID], x.f.scope, receipt, uuid.NewString())
		}()
	}
	wg.Wait()
	close(errs)
	for e := range errs {
		if e != nil {
			t.Fatal(e)
		}
	}
	if !bytes.Equal(held, authoritySnapshot(t, x.f.pool, x.f.tenant, heldTables)) {
		t.Fatal("root installation changed original funds/capacity/history")
	}
	approval, e := q.HubSameStoreRecoveryByID(ctx, f.RecoveryID)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = x.s.BindHubSession(x.f.contexts[approval.HubID], approval.HubID, approval.ToBootID, uuid.NewString()); !errors.Is(e, service.ErrShareUnknown) {
		t.Fatal("roots admitted Hub", e)
	}
	x.now = x.now.Add(time.Hour)
	replay, e := x.s.PrepareNodeRecoverySources(ctx, admin, f.RecoveryID, f.AllocationID, uuid.NewString())
	if e != nil || !bytes.Equal(replay.Payload, p.Payload) {
		t.Fatal("command retimed", e)
	}
	again, e := st.ApplySourceProvision(ctx, p.NodeID, p.BootID, p.BackendID, p.Payload, store.IssuerTime{})
	if e != nil || again != receipt {
		t.Fatal("FULL replay changed receipt", e)
	}
	if e = x.s.RecordNodeRecoverySources(x.f.contexts[p.NodeID], x.f.scope, again, uuid.NewString()); e != nil {
		t.Fatal("expired exact evidence replay", e)
	}
}
func TestNodeRecoverySourcesRefuseChangedCurrentAuthorityAndExpiry(t *testing.T) {
	for _, name := range []string{"expiry", "node boot", "authority"} {
		t.Run(name, func(t *testing.T) {
			x, f, st := preparedCreditFence(t)
			recordRecoveryFence(t, x, f, st)
			ctx := context.Background()
			admin := policyScope(t, x.f.tenant, auth.PlatformAdmin, policy.Grant)
			switch name {
			case "expiry":
				x.now = x.now.Add(time.Hour)
			case "node boot":
				if _, e := x.f.pool.Exec(ctx, "UPDATE nodes SET boot_id=$1 WHERE id=$2", uuid.NewString(), f.NodeID); e != nil {
					t.Fatal(e)
				}
			case "authority":
				if _, e := x.s.AuthorizeSigner(ctx, admin, x.in.SignerID, 9, x.now.Add(-time.Second), x.now.Add(time.Hour), make([]byte, 32), uuid.NewString()); e != nil {
					t.Fatal(e)
				}
			}
			if _, e := x.s.PrepareNodeRecoverySources(ctx, admin, f.RecoveryID, f.AllocationID, uuid.NewString()); e == nil {
				t.Fatal("changed state accepted")
			}
		})
	}
}
