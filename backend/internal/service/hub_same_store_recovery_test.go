package service_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
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
	"vortex/hub"
)

func sameStoreFixture(t *testing.T) (*shareFixture, service.HubFundingInput, *store.Store, hub.Allocation, service.HubSameStoreRecoveryInput) {
	t.Helper()
	return sameStoreFixtureEvidence(t, true, true)
}

func sameStoreFixtureEvidence(t *testing.T, installed, provisioned bool) (*shareFixture, service.HubFundingInput, *store.Store, hub.Allocation, service.HubSameStoreRecoveryInput) {
	t.Helper()
	x, in, hs := fundedHubFixture(t)
	a, e := x.s.PrepareHubAllocation(context.Background(), x.f.scope, in, uuid.NewString())
	if e != nil {
		t.Fatal(e)
	}
	installAllocationFixture(t, hs, a, x.now)
	issued, _, seq := issueAllocationFixture(t, x, hs, a, 37)
	if e = x.s.RecordHubIssue(x.f.contexts[in.HubID], in.HubID, in.HubBootID, seq, issued.Payload, uuid.NewString()); e != nil {
		t.Fatal(e)
	}
	raw, e := generated.New(x.f.pool).GetHubAllocationGlobal(context.Background(), a.AllocationID)
	if e != nil {
		t.Fatal(e)
	}
	if installed {
		if e = x.s.RecordHubAllocationReceipt(x.f.contexts[in.HubID], in.HubID, in.HubBootID, a.AllocationID, raw.PayloadDigest, uuid.NewString()); e != nil {
			t.Fatal(e)
		}
	}
	if provisioned {
		if _, e = x.s.PrepareNodeSources(context.Background(), x.f.scope, a.AllocationID, shareBackendID, uuid.NewString()); e != nil {
			t.Fatal(e)
		}
	}
	x.now = x.now.Add(5 * time.Second)
	proof := sha256.Sum256([]byte("explicit next epoch approval does not prove replay or execution"))
	if _, e = x.s.AuthorizeSigner(context.Background(), policyScope(t, x.f.tenant, auth.PlatformAdmin, policy.Grant), in.HubID, 8, x.now.Add(-time.Second), x.now.Add(time.Hour), proof[:], uuid.NewString()); e != nil {
		t.Fatal(e)
	}
	r := service.HubSameStoreRecoveryInput{ID: uuid.NewString(), HubID: in.HubID, BackendID: shareBackendID, FromBootID: in.HubBootID, ToBootID: uuid.NewString(), FromEpoch: 7, ToEpoch: 8, Reason: "explicit same-store FULL handoff; complete replay still required"}
	return x, in, hs, a, r
}

func TestHubSameStoreApprovalAuditReplayOriginalRootsAndNoAdmission(t *testing.T) {
	x, in, hs, a, r := sameStoreFixture(t)
	ctx := context.Background()
	admin := policyScope(t, x.f.tenant, auth.PlatformAdmin, policy.Grant)
	proof := sha256.Sum256([]byte("unknown original child liability"))
	if e := (service.MeteringService{Pool: x.f.pool}).RecordUnknown(ctx, x.f.scope, a.SourceID, proof[:], x.now.Add(time.Hour), uuid.NewString()); e != nil {
		t.Fatal(e)
	}
	tables := []string{"reservations", "ledger_projections", "ledger_events", "hub_credit_grants", "share_chains", "share_generations", "share_renewal_authorities", "settlements"}
	held := authoritySnapshot(t, x.f.pool, x.f.tenant, tables)
	for _, scope := range []service.TenantScope{x.f.scope, policyScope(t, x.f.tenant, auth.Operator, policy.Mutate)} {
		if _, e := x.s.PrepareHubSameStoreRecovery(ctx, scope, r, uuid.NewString()); !errors.Is(e, service.ErrScope) {
			t.Fatal("unauthorized recovery", e)
		}
	}
	request := uuid.NewString()
	failAudit(t, x.f.pool, request)
	all := append(append([]string{}, tables...), "hub_same_store_recoveries", "operations", "tasks", "outbox", "audit_events")
	before := authoritySnapshot(t, x.f.pool, x.f.tenant, all)
	if _, e := x.s.PrepareHubSameStoreRecovery(ctx, admin, r, request); e == nil {
		t.Fatal("audit failure accepted approval")
	}
	if !bytes.Equal(before, authoritySnapshot(t, x.f.pool, x.f.tenant, all)) {
		t.Fatal("partial approval escaped audit rollback")
	}
	result, e := x.s.PrepareHubSameStoreRecovery(ctx, admin, r, uuid.NewString())
	if e != nil {
		t.Fatal(e)
	}
	c, e := hub.DecodeSameStoreRecovery(result.Payload)
	if e != nil {
		t.Fatal(e)
	}
	original, _ := json.Marshal(a.Delegation())
	dh := sha256.Sum256(original)
	wire, _ := json.Marshal(a)
	wh := sha256.Sum256(wire)
	if len(c.Allocations) != 1 || c.Allocations[0].PayloadSHA256 != hex.EncodeToString(wh[:]) || c.Allocations[0].DelegationSHA256 != hex.EncodeToString(dh[:]) {
		t.Fatal("original bytes not linked", c.Allocations)
	}
	for i, root := range c.Handoff.FundedSuccessors[0].ShareReferences {
		old := a.ShareReferences[i]
		old.Revision++
		old.SignerEpoch++
		if root != old {
			t.Fatal("root changed source, limit or deadline", root, old)
		}
	}
	if !bytes.Equal(held, authoritySnapshot(t, x.f.pool, x.f.tenant, tables)) {
		t.Fatal("approval changed original liabilities")
	}
	q := generated.New(x.f.pool)
	bound, e := q.LatestHubIncarnation(ctx, in.HubID)
	if e != nil || bound.SignerEpoch != 7 || bound.BootID != in.HubBootID {
		t.Fatal("approval bound new incarnation", bound, e)
	}
	if _, e = x.s.BindHubSession(x.f.contexts[in.HubID], in.HubID, r.ToBootID, uuid.NewString()); !errors.Is(e, service.ErrShareUnknown) {
		t.Fatal("approval bypassed complete-prefix admission", e)
	}
	op, e := q.GetOperation(ctx, generated.GetOperationParams{TenantID: x.f.tenant, ID: result.OperationID})
	if e != nil || op.State == "succeeded" {
		t.Fatal("approval claimed completion", op, e)
	}
	// Execute only the local FULL primitive. No fixture ACK or admission is supplied.
	h, e := hs.HandoffHub(ctx, c.Handoff, store.IssuerTime{Earliest: x.now, Latest: x.now, Trusted: true})
	if e != nil || h.FundedInventory == nil || h.Remaining[a.DelegationID] != 66 || h.FundedInventory.Delegations[0].Issued != 37 {
		t.Fatal("command cannot apply exact local partition", h, e)
	}
	if _, e = hs.IssueHubAuthorization(ctx, store.IssueRequest{ID: uuid.NewString(), HubID: in.HubID, DelegationID: a.DelegationID, NodeID: a.NodeID, BootID: a.BootID, RawBytes: 1, TTL: time.Minute}, store.IssuerTime{Earliest: x.now, Latest: x.now, Trusted: true}); e == nil {
		t.Fatal("local prefix unconfirmed but fresh credit minted")
	}
	// Replay is immutable even after all clocks expire; it grants no new authority.
	x.s.Clock = nil
	replay, e := x.s.PrepareHubSameStoreRecovery(ctx, admin, r, uuid.NewString())
	if e != nil || !bytes.Equal(replay.Payload, result.Payload) || !bytes.Equal(replay.Proof, result.Proof) || replay.OperationID != result.OperationID {
		t.Fatal("replay retimed approval", e)
	}
	r.ToBootID = uuid.NewString()
	if _, e = x.s.PrepareHubSameStoreRecovery(ctx, admin, r, uuid.NewString()); !errors.Is(e, service.ErrShareConflict) {
		t.Fatal("replay changed target", e)
	}
	if _, e = x.f.pool.Exec(ctx, `UPDATE hub_same_store_recoveries SET to_boot_id=$1 WHERE id=$2`, uuid.NewString(), result.ID); e == nil {
		t.Fatal("mutable approval")
	}
	if _, e = x.f.pool.Exec(ctx, `DELETE FROM hub_same_store_recoveries WHERE id=$1`, result.ID); e == nil {
		t.Fatal("deletable approval")
	}
}

func TestHubSameStoreApprovalRejectsMissingEvidenceAndStaleTargets(t *testing.T) {
	for _, name := range []string{"backend", "old-boot", "epoch-gap", "expired", "clock", "missing-install", "missing-roots"} {
		t.Run(name, func(t *testing.T) {
			x, _, _, a, r := sameStoreFixtureEvidence(t, name != "missing-install", name != "missing-roots")
			ctx := context.Background()
			switch name {
			case "backend":
				r.BackendID = uuid.NewString()
			case "old-boot":
				r.FromBootID = uuid.NewString()
			case "epoch-gap":
				r.ToEpoch++
			case "expired":
				x.now = a.ExpiresAt
			case "clock":
				x.s.Clock = nil

			}
			before := authoritySnapshot(t, x.f.pool, x.f.tenant, []string{"hub_same_store_recoveries", "operations", "tasks", "outbox", "audit_events"})
			if _, e := x.s.PrepareHubSameStoreRecovery(ctx, policyScope(t, x.f.tenant, auth.PlatformAdmin, policy.Grant), r, uuid.NewString()); e == nil {
				t.Fatal("unsafe approval", name)
			}
			if !bytes.Equal(before, authoritySnapshot(t, x.f.pool, x.f.tenant, []string{"hub_same_store_recoveries", "operations", "tasks", "outbox", "audit_events"})) {
				t.Fatal("rejected approval mutated state")
			}
		})
	}
}

func TestHubSameStoreApprovalConcurrentReplayAndAllTenantConsent(t *testing.T) {
	x, _, _, _, r := sameStoreFixture(t)
	ctx := context.Background()
	admin := policyScope(t, x.f.tenant, auth.PlatformAdmin, policy.Grant)
	other := uuid.NewString()
	if _, e := x.f.pool.Exec(ctx, `INSERT INTO tenants(id,account_state,version) VALUES($1,'active',1)`, other); e != nil {
		t.Fatal(e)
	}
	proof := sha256.Sum256([]byte("another tenant sharing original Hub"))
	otherAdmin := policyScope(t, other, auth.PlatformAdmin, policy.Grant)
	if _, e := x.s.AuthorizeSigner(ctx, otherAdmin, r.HubID, 7, x.now.Add(-time.Second), x.now.Add(time.Hour), proof[:], uuid.NewString()); e != nil {
		t.Fatal(e)
	}
	if _, e := x.s.PrepareHubSameStoreRecovery(ctx, admin, r, uuid.NewString()); !errors.Is(e, service.ErrShareUnknown) {
		t.Fatal("missing other tenant approval ignored", e)
	}
	if _, e := x.s.AuthorizeSigner(ctx, otherAdmin, r.HubID, 8, x.now.Add(-time.Second), x.now.Add(time.Hour), proof[:], uuid.NewString()); e != nil {
		t.Fatal(e)
	}
	var wg sync.WaitGroup
	start := make(chan struct{})
	results := make(chan generated.HubSameStoreRecovery, 8)
	errs := make(chan error, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			v, e := x.s.PrepareHubSameStoreRecovery(ctx, admin, r, uuid.NewString())
			results <- v
			errs <- e
		}()
	}
	close(start)
	wg.Wait()
	close(results)
	close(errs)
	for e := range errs {
		if e != nil {
			t.Fatal(e)
		}
	}
	var first []byte
	for v := range results {
		if first == nil {
			first = v.Payload
		}
		if !bytes.Equal(first, v.Payload) {
			t.Fatal("concurrent replay split command")
		}
	}
	var count int
	if e := x.f.pool.QueryRow(ctx, `SELECT count(*) FROM hub_same_store_recoveries WHERE hub_id=$1`, r.HubID).Scan(&count); e != nil || count != 1 {
		t.Fatal(count, e)
	}
	var p struct{ Authorities []generated.SignerAuthority }
	v, e := generated.New(x.f.pool).GetHubSameStoreRecovery(ctx, generated.GetHubSameStoreRecoveryParams{HubID: r.HubID, ToEpoch: r.ToEpoch})
	if e != nil || json.Unmarshal(v.Proof, &p) != nil || len(p.Authorities) != 4 {
		t.Fatal("incomplete all-tenant proof", len(p.Authorities), e)
	}
}

func TestHubSameStoreApprovalConcurrentTargetsChooseOne(t *testing.T) {
	x, _, _, _, r := sameStoreFixture(t)
	ctx := context.Background()
	admin := policyScope(t, x.f.tenant, auth.PlatformAdmin, policy.Grant)
	var wg sync.WaitGroup
	start := make(chan struct{})
	errs := make(chan error, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			c := r
			c.ID = uuid.NewString()
			c.ToBootID = uuid.NewString()
			_, e := x.s.PrepareHubSameStoreRecovery(ctx, admin, c, uuid.NewString())
			errs <- e
		}()
	}
	close(start)
	wg.Wait()
	close(errs)
	ok, conflict := 0, 0
	for e := range errs {
		if e == nil {
			ok++
		} else if errors.Is(e, service.ErrShareConflict) {
			conflict++
		} else {
			t.Fatal(e)
		}
	}
	if ok != 1 || conflict != 7 {
		t.Fatal("split approval", ok, conflict)
	}
}
