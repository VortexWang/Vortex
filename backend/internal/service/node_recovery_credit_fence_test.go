package service_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"github.com/google/uuid"
	"sync"
	"testing"
	"vortex/agent/store"
	"vortex/backend/db/generated"
	"vortex/backend/internal/auth"
	"vortex/backend/internal/policy"
	"vortex/backend/internal/service"
)

func preparedCreditFence(t *testing.T) (*shareFixture, generated.NodeRecoveryCreditFence, *store.Store) {
	t.Helper()
	x, _, hs, a, r := sameStoreFixture(t)
	_, raw, records := sameStoreManifest(t, x, hs, r)
	retainSameStore(t, x, r, records)
	if _, e := x.s.VerifyHubSameStorePrefix(x.f.contexts[r.HubID], r.HubID, r.ToBootID, r.ID, raw, uuid.NewString()); e != nil {
		t.Fatal(e)
	}
	p, e := x.s.PrepareNodeRecoveryCreditFence(context.Background(), policyScope(t, x.f.tenant, auth.PlatformAdmin, policy.Grant), r.ID, a.AllocationID, uuid.NewString())
	if e != nil {
		t.Fatal(e)
	}
	st, e := store.Open(t.TempDir()+"/node.db", "agent")
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { st.Close() })
	prov, e := generated.New(x.f.pool).GetNodeSourceProvision(context.Background(), generated.GetNodeSourceProvisionParams{TenantID: x.f.tenant, AllocationID: a.AllocationID})
	if e != nil {
		t.Fatal(e)
	}
	source, e := store.DecodeSourceProvision(prov.Payload)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = st.ApplySourceProvision(context.Background(), p.NodeID, p.BootID, p.BackendID, prov.Payload, store.IssuerTime{Earliest: source.IssuedAt, Latest: source.IssuedAt, Trusted: true}); e != nil {
		t.Fatal(e)
	}
	return x, p, st
}

func TestNodeRecoveryCreditFencePGAtomicReplayAndFinancialIsolation(t *testing.T) {
	x, p, st := preparedCreditFence(t)
	ctx := context.Background()
	c, e := store.DecodeRecoveryCreditFence(p.Payload)
	if e != nil {
		t.Fatal(e)
	}
	if len(c.Credits) != 1 || c.Issued != 37 || c.Remaining != c.OriginalRaw-37 {
		t.Fatal(c)
	}
	if _, e = x.s.PrepareNodeRecoveryCreditFence(ctx, x.f.scope, p.RecoveryID, p.AllocationID, uuid.NewString()); !errors.Is(e, service.ErrScope) {
		t.Fatal(e)
	}
	heldTables := []string{"reservations", "ledger_projections", "ledger_events", "hub_credit_grants", "share_chains", "share_generations", "settlements", "hub_signer_incarnations"}
	held := authoritySnapshot(t, x.f.pool, x.f.tenant, heldTables[:len(heldTables)-1])
	// No Hub issue has reached this Node. PG's expected child remains occupied;
	// its absence at Node cannot become fresh R.
	receipt, e := st.ApplyRecoveryCreditFence(ctx, p.NodeID, p.BootID, p.BackendID, p.Payload, store.IssuerTime{Earliest: x.now, Latest: x.now, Trusted: true})
	if e != nil {
		t.Fatal(e)
	}
	if len(receipt.Installed) != 0 {
		t.Fatal(receipt)
	}
	raw, _ := json.Marshal(receipt)
	bad := receipt
	bad.Installed = []store.RecoveryCreditEntry{{GrantID: uuid.NewString(), Amount: 1}}
	forged, _ := json.Marshal(bad)
	if e = x.s.RecordNodeRecoveryCreditFence(x.f.contexts[p.NodeID], x.f.scope, forged, uuid.NewString()); !errors.Is(e, service.ErrStaleReceipt) {
		t.Fatal("extra child", e)
	}
	if e = x.s.RecordNodeRecoveryCreditFence(ctx, x.f.scope, raw, uuid.NewString()); e == nil {
		t.Fatal("unauthenticated receipt")
	}
	all := []string{"node_recovery_credit_fence_receipts", "operations", "tasks", "outbox", "audit_events"}
	before := authoritySnapshot(t, x.f.pool, x.f.tenant, all)
	request := uuid.NewString()
	failAudit(t, x.f.pool, request)
	if e = x.s.RecordNodeRecoveryCreditFence(x.f.contexts[p.NodeID], x.f.scope, raw, request); e == nil {
		t.Fatal("audit failure escaped")
	}
	if !bytes.Equal(before, authoritySnapshot(t, x.f.pool, x.f.tenant, all)) {
		t.Fatal("partial receipt")
	}
	errs := make(chan error, 8)
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs <- x.s.RecordNodeRecoveryCreditFence(x.f.contexts[p.NodeID], x.f.scope, raw, uuid.NewString())
		}()
	}
	wg.Wait()
	close(errs)
	for e := range errs {
		if e != nil {
			t.Fatal(e)
		}
	}
	if !bytes.Equal(held, authoritySnapshot(t, x.f.pool, x.f.tenant, heldTables[:len(heldTables)-1])) {
		t.Fatal("fence changed funds or capacity")
	}
	replay, e := x.s.PrepareNodeRecoveryCreditFence(ctx, policyScope(t, x.f.tenant, auth.PlatformAdmin, policy.Grant), p.RecoveryID, p.AllocationID, uuid.NewString())
	if e != nil || !bytes.Equal(p.Payload, replay.Payload) {
		t.Fatal("command replay", e)
	}
	q := generated.New(x.f.pool)
	bound, e := q.LatestHubIncarnation(ctx, c.HubID)
	if e != nil || bound.SignerEpoch != int64(c.FromEpoch) {
		t.Fatal("fence admitted successor Hub", bound, e)
	}
	approval, e := q.HubSameStoreRecoveryByID(ctx, c.RecoveryID)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = x.s.BindHubSession(x.f.contexts[c.HubID], c.HubID, approval.ToBootID, uuid.NewString()); !errors.Is(e, service.ErrShareUnknown) {
		t.Fatal("fence became online admission", e)
	}
	saved, e := q.GetNodeRecoveryCreditFenceReceipt(ctx, generated.GetNodeRecoveryCreditFenceReceiptParams{TenantID: x.f.tenant, FenceID: p.ID})
	h := sha256.Sum256(raw)
	if e != nil || !bytes.Equal(saved.PayloadDigest, h[:]) || receipt.Digest != hex.EncodeToString(p.PayloadDigest) {
		t.Fatal(saved, e)
	}
	op, e := q.GetOperation(ctx, generated.GetOperationParams{TenantID: x.f.tenant, ID: p.OperationID})
	if e != nil || op.State != "succeeded" {
		t.Fatal(op, e)
	}
}

func TestNodeRecoveryCreditFenceRequiresVerifiedWitnessAndAtomicCommand(t *testing.T) {
	x, _, hs, a, r := sameStoreFixture(t)
	ctx := context.Background()
	admin := policyScope(t, x.f.tenant, auth.PlatformAdmin, policy.Grant)
	_, raw, records := sameStoreManifest(t, x, hs, r)
	if _, e := x.s.PrepareNodeRecoveryCreditFence(ctx, admin, r.ID, a.AllocationID, uuid.NewString()); e == nil {
		t.Fatal("unverified manifest became command")
	}
	retainSameStore(t, x, r, records)
	if _, e := x.s.VerifyHubSameStorePrefix(x.f.contexts[r.HubID], r.HubID, r.ToBootID, r.ID, raw, uuid.NewString()); e != nil {
		t.Fatal(e)
	}
	tables := []string{"node_recovery_credit_fences", "operations", "tasks", "outbox", "audit_events"}
	before := authoritySnapshot(t, x.f.pool, x.f.tenant, tables)
	request := uuid.NewString()
	failAudit(t, x.f.pool, request)
	if _, e := x.s.PrepareNodeRecoveryCreditFence(ctx, admin, r.ID, a.AllocationID, request); e == nil {
		t.Fatal("audit failed command committed")
	}
	if !bytes.Equal(before, authoritySnapshot(t, x.f.pool, x.f.tenant, tables)) {
		t.Fatal("partial command")
	}
	results := make(chan generated.NodeRecoveryCreditFence, 8)
	errs := make(chan error, 8)
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			p, e := x.s.PrepareNodeRecoveryCreditFence(ctx, admin, r.ID, a.AllocationID, uuid.NewString())
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
	var prior []byte
	for p := range results {
		if prior != nil && !bytes.Equal(prior, p.Payload) {
			t.Fatal("concurrent commands diverged")
		}
		prior = p.Payload
	}
}
