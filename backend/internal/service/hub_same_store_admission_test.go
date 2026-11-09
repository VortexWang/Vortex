package service_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
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

func admissionFixture(t *testing.T) (*shareFixture, *store.Store, hub.Allocation, service.HubSameStoreRecoveryInput, generated.NodeRecoverySourceProvision) {
	return admissionFixtureEvidence(t, true)
}
func admissionFixtureEvidence(t *testing.T, recordRoot bool) (*shareFixture, *store.Store, hub.Allocation, service.HubSameStoreRecoveryInput, generated.NodeRecoverySourceProvision) {
	return admissionFixtureWithPrior(t, recordRoot, nil)
}
func admissionFixtureWithPrior(t *testing.T, recordRoot bool, prior func(*shareFixture, service.HubSameStoreRecoveryInput)) (*shareFixture, *store.Store, hub.Allocation, service.HubSameStoreRecoveryInput, generated.NodeRecoverySourceProvision) {
	t.Helper()
	x, _, hs, a, r := sameStoreFixture(t)
	if prior != nil {
		prior(x, r)
	}
	_, raw, records := sameStoreManifest(t, x, hs, r)
	retainSameStore(t, x, r, records)
	if _, e := x.s.VerifyHubSameStorePrefix(x.f.contexts[r.HubID], r.HubID, r.ToBootID, r.ID, raw, uuid.NewString()); e != nil {
		t.Fatal(e)
	}
	admin := policyScope(t, x.f.tenant, auth.PlatformAdmin, policy.Grant)
	f, e := x.s.PrepareNodeRecoveryCreditFence(context.Background(), admin, r.ID, a.AllocationID, uuid.NewString())
	if e != nil {
		t.Fatal(e)
	}
	ns, e := store.Open(t.TempDir()+"/node.db", "agent")
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { ns.Close() })
	original, e := generated.New(x.f.pool).GetNodeSourceProvision(context.Background(), generated.GetNodeSourceProvisionParams{TenantID: x.f.tenant, AllocationID: a.AllocationID})
	if e != nil {
		t.Fatal(e)
	}
	p, e := store.DecodeSourceProvision(original.Payload)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = ns.ApplySourceProvision(context.Background(), a.NodeID, a.BootID, shareBackendID, original.Payload, store.IssuerTime{Earliest: p.IssuedAt, Latest: p.IssuedAt, Trusted: true}); e != nil {
		t.Fatal(e)
	}
	recordRecoveryFence(t, x, f, ns)
	next, e := x.s.PrepareNodeRecoverySources(context.Background(), admin, r.ID, a.AllocationID, uuid.NewString())
	if e != nil {
		t.Fatal(e)
	}
	receipt, e := ns.ApplySourceProvision(context.Background(), a.NodeID, a.BootID, shareBackendID, next.Payload, store.IssuerTime{Earliest: x.now, Latest: x.now, Trusted: true})
	if e != nil {
		t.Fatal(e)
	}
	if recordRoot {
		if e = x.s.RecordNodeRecoverySources(x.f.contexts[a.NodeID], x.f.scope, receipt, uuid.NewString()); e != nil {
			t.Fatal(e)
		}
	}
	return x, hs, a, r, next
}

func TestHubSameStoreAdmissionAtomicClosureBindingNoFinancialReleaseOrLocalACK(t *testing.T) {
	x, hs, a, r, _ := admissionFixture(t)
	ctx := context.Background()
	admin := policyScope(t, x.f.tenant, auth.PlatformAdmin, policy.Grant)
	debt := sha256.Sum256([]byte("unknown prior child must survive admission"))
	if e := (service.MeteringService{Pool: x.f.pool}).RecordUnknown(ctx, x.f.scope, a.SourceID, debt[:], x.now.Add(time.Hour), uuid.NewString()); e != nil {
		t.Fatal(e)
	}
	if _, e := x.s.AdmitHubSameStore(ctx, x.f.scope, r.ID, uuid.NewString()); !errors.Is(e, service.ErrScope) {
		t.Fatal(e)
	}
	tables := []string{"reservations", "ledger_projections", "ledger_events", "hub_credit_grants", "share_chains", "share_generations", "settlements", "hub_funded_allocations", "node_source_provisions", "node_recovery_source_provisions"}
	held := authoritySnapshot(t, x.f.pool, x.f.tenant, tables)
	atomic := []string{"hub_same_store_admissions", "share_renewal_authorities", "operations", "tasks", "outbox", "audit_events"}
	before := authoritySnapshot(t, x.f.pool, x.f.tenant, atomic)
	request := uuid.NewString()
	failAudit(t, x.f.pool, request)
	if _, e := x.s.AdmitHubSameStore(ctx, admin, r.ID, request); e == nil {
		t.Fatal("audit failure admitted")
	}
	if !bytes.Equal(before, authoritySnapshot(t, x.f.pool, x.f.tenant, atomic)) {
		t.Fatal("partial admission")
	}
	old, e := generated.New(x.f.pool).LatestHubIncarnation(ctx, r.HubID)
	if e != nil || old.BootID != r.FromBootID {
		t.Fatal("binding escaped audit rollback", old, e)
	}
	var wg sync.WaitGroup
	errs := make(chan error, 8)
	results := make(chan generated.HubSameStoreAdmission, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			p, e := x.s.AdmitHubSameStore(ctx, admin, r.ID, uuid.NewString())
			results <- p
			errs <- e
		}()
	}
	wg.Wait()
	close(errs)
	close(results)
	for e := range errs {
		if e != nil {
			t.Fatal(e)
		}
	}
	var first generated.HubSameStoreAdmission
	for p := range results {
		if first.RecoveryID != "" && !bytes.Equal(first.Payload, p.Payload) {
			t.Fatal("concurrent admission retimed")
		}
		first = p
	}
	if !bytes.Equal(held, authoritySnapshot(t, x.f.pool, x.f.tenant, tables)) {
		t.Fatal("admission changed financial/capacity histories")
	}
	q := generated.New(x.f.pool)
	current, e := q.LatestHubIncarnation(ctx, r.HubID)
	if e != nil || current.BootID != r.ToBootID || current.SignerEpoch != r.ToEpoch {
		t.Fatal(current, e)
	}
	shares, e := q.HubRecoveryShares(ctx, r.HubID)
	if e != nil {
		t.Fatal(e)
	}
	for _, g := range shares {
		renew, e := q.ShareRenewalAuthority(ctx, generated.ShareRenewalAuthorityParams{TenantID: g.TenantID, NodeID: g.NodeID, Kind: g.Kind, Generation: g.Generation, SignerID: r.HubID})
		if e != nil || !renew.ClosedAt.Valid || !bytes.Equal(renew.ClosureProof, first.PayloadDigest) {
			t.Fatal("old issuance still open", renew, e)
		}
	}
	if _, e = x.s.BindHubSession(x.f.contexts[r.HubID], r.HubID, r.FromBootID, uuid.NewString()); !errors.Is(e, service.ErrShareUnknown) {
		t.Fatal("old boot rebound", e)
	}
	if _, e = x.s.BindHubSession(x.f.contexts[r.HubID], r.HubID, r.ToBootID, uuid.NewString()); e != nil {
		t.Fatal("admitted target binding", e)
	}
	pending, e := hs.PendingLedger(ctx, 1000)
	if e != nil || len(pending) != 2 {
		t.Fatal("PG admission fabricated local ACK", pending, e)
	}
	if _, e = hs.IssueHubAuthorization(ctx, store.IssueRequest{ID: uuid.NewString(), HubID: r.HubID, DelegationID: a.DelegationID, NodeID: a.NodeID, BootID: a.BootID, RawBytes: 1, TTL: time.Minute}, store.IssuerTime{Earliest: x.now, Latest: x.now, Trusted: true}); !errors.Is(e, store.ErrAuthorityHandoffRequired) {
		t.Fatal("PG binding made local Hub online", e)
	}
	x.s.Clock = nil
	again, e := x.s.AdmitHubSameStore(ctx, admin, r.ID, uuid.NewString())
	if e != nil || !bytes.Equal(again.Payload, first.Payload) {
		t.Fatal("historical replay retimed", e)
	}
	if _, e = x.f.pool.Exec(ctx, "DELETE FROM hub_same_store_admissions WHERE recovery_id=$1", r.ID); e == nil {
		t.Fatal("mutable admission")
	}
}

func TestHubSameStoreAdmissionRechecksStaleWitnessAndNodeReceipt(t *testing.T) {
	for _, name := range []string{"expiry", "node boot", "fence raised", "authority", "account restricted", "missing root receipt", "extra old issue", "extra unsealed allocation", "seal"} {
		t.Run(name, func(t *testing.T) {
			x, _, a, r, _ := admissionFixtureEvidence(t, name != "missing root receipt")
			ctx := context.Background()
			admin := policyScope(t, x.f.tenant, auth.PlatformAdmin, policy.Grant)
			q := generated.New(x.f.pool)
			switch name {
			case "expiry":
				x.now = x.now.Add(time.Hour)
			case "node boot":
				if _, e := x.f.pool.Exec(ctx, "UPDATE nodes SET boot_id=$1 WHERE id=$2", uuid.NewString(), a.NodeID); e != nil {
					t.Fatal(e)
				}
			case "fence raised":
				if e := x.s.RecordNodeFencing(x.f.contexts[a.NodeID], x.f.scope, a.NodeID, a.BootID, int64(a.FencingFloor)+1, uuid.NewString()); e != nil {
					t.Fatal(e)
				}
			case "authority":
				if _, e := x.s.AuthorizeSigner(ctx, admin, r.HubID, 9, x.now.Add(-time.Second), x.now.Add(time.Hour), make([]byte, 32), uuid.NewString()); e != nil {
					t.Fatal(e)
				}
			case "account restricted":
				if _, e := x.f.pool.Exec(ctx, "UPDATE tenants SET account_state='suspended' WHERE id=$1", x.f.tenant); e != nil {
					t.Fatal(e)
				}
			case "missing root receipt":
				// The fixture installed roots locally but intentionally never returned the
				// authenticated FULL receipt to PG.
			case "extra old issue":
				// Produce a legitimate old-epoch FULL journal from its original immutable
				// allocation in an independent test store, then register it after witness.
				other, e := store.Open(t.TempDir()+"/old.db", "hub")
				if e != nil {
					t.Fatal(e)
				}
				defer other.Close()
				installAllocationFixture(t, other, a, a.ValidFrom)
				issue, _, _ := issueAllocationFixture(t, x, other, a, 1)
				if e = x.s.RecordHubIssue(x.f.contexts[r.HubID], r.HubID, r.FromBootID, 100, issue.Payload, uuid.NewString()); e != nil {
					t.Fatal(e)
				}
			case "extra unsealed allocation":
				// The admission must enumerate all current allocations, even one with no
				// issued children and absent from the historical manifest.
				original, e := q.GetHubAllocationGlobal(ctx, a.AllocationID)
				if e != nil {
					t.Fatal(e)
				}
				oldReservation, e := q.GetUsageReservation(ctx, generated.GetUsageReservationParams{TenantID: x.f.tenant, ID: original.ReservationID})
				if e != nil {
					t.Fatal(e)
				}
				held, e := (service.MeteringService{Pool: x.f.pool}).ReserveUsage(ctx, x.f.scope, service.ReserveInput{SourceID: oldReservation.SourceID, SnapshotID: a.SnapshotID, NodeID: a.NodeID, BootID: a.BootID, Nonce: uuid.NewString(), RawBytes: 1, Earliest: x.now, Latest: x.now, Deadline: a.ExpiresAt}, uuid.NewString())
				if e != nil {
					t.Fatal(e)
				}
				extra := a
				extra.AllocationID = uuid.NewString()
				extra.DelegationID = uuid.NewString()
				extra.SourceID = held.ID
				extra.RawBytes = 1
				extra.ShareReferences = append([]store.ShareReference(nil), a.ShareReferences...)
				extra.ShareReferences[0].SourceID = held.ID
				extra.ShareReferences[0].ID = uuid.NewString()
				raw, _ := json.Marshal(extra)
				hash := sha256.Sum256(raw)
				if _, e = hub.DecodeAllocation(raw); e != nil {
					t.Fatal(e)
				}
				if e = q.AppendHubAllocation(ctx, generated.AppendHubAllocationParams{TenantID: x.f.tenant, AllocationID: extra.AllocationID, DelegationID: extra.DelegationID, ReservationID: held.ID, HubID: r.HubID, HubBootID: r.FromBootID, NodeID: a.NodeID, BootID: a.BootID, RequestKey: uuid.NewString(), RequestDigest: hash[:], Payload: raw, PayloadDigest: hash[:], OperationID: original.OperationID, CreatedBy: original.CreatedBy}); e != nil {
					t.Fatal(e)
				}

			case "seal":
				original, e := q.GetHubAllocationGlobal(ctx, a.AllocationID)
				if e != nil {
					t.Fatal(e)
				}
				if e = q.AppendHubDelegationSeal(ctx, generated.AppendHubDelegationSealParams{TenantID: x.f.tenant, AllocationID: a.AllocationID, SealID: uuid.NewString(), HubID: r.HubID, HubBootID: r.FromBootID, LedgerSequence: 100, Payload: []byte("intervening immutable seal"), PayloadDigest: make([]byte, 32), PeerFingerprint: make([]byte, 32)}); e != nil {
					t.Fatal(e)
				}
				_ = original
			}
			if _, e := x.s.AdmitHubSameStore(ctx, admin, r.ID, uuid.NewString()); e == nil {
				t.Fatal("stale proof admitted")
			}
			current, e := q.LatestHubIncarnation(ctx, r.HubID)
			if e != nil || current.BootID != r.FromBootID {
				t.Fatal("failed admission changed binding", current, e)
			}
		})
	}
}

func TestHubSameStoreAdmissionRetainsAllTenantConsentAndRejectsLaterAuthority(t *testing.T) {
	for _, changed := range []bool{false, true} {
		t.Run(fmt.Sprint(changed), func(t *testing.T) {
			other := uuid.NewString()
			x, _, _, r, _ := admissionFixtureWithPrior(t, true, func(x *shareFixture, r service.HubSameStoreRecoveryInput) {
				if _, e := x.f.pool.Exec(context.Background(), "INSERT INTO tenants(id,account_state,version) VALUES($1,'active',1)", other); e != nil {
					t.Fatal(e)
				}
				admin := policyScope(t, other, auth.PlatformAdmin, policy.Grant)
				for _, epoch := range []int64{7, 8} {
					if _, e := x.s.AuthorizeSigner(context.Background(), admin, r.HubID, epoch, x.now.Add(-time.Second), x.now.Add(10*time.Minute), make([]byte, 32), uuid.NewString()); e != nil {
						t.Fatal(e)
					}
				}
			})
			admin := policyScope(t, x.f.tenant, auth.PlatformAdmin, policy.Grant)
			if changed {
				if _, e := x.s.AuthorizeSigner(context.Background(), policyScope(t, other, auth.PlatformAdmin, policy.Grant), r.HubID, 9, x.now.Add(-time.Second), x.now.Add(10*time.Minute), make([]byte, 32), uuid.NewString()); e != nil {
					t.Fatal(e)
				}
			}
			held := authoritySnapshot(t, x.f.pool, other, []string{"signer_authorities", "share_chains", "ledger_events", "ledger_projections", "reservations"})
			p, e := x.s.AdmitHubSameStore(context.Background(), admin, r.ID, uuid.NewString())
			if changed {
				if e == nil {
					t.Fatal("other tenant advanced epoch ignored")
				}
			} else {
				if e != nil {
					t.Fatal(e)
				}
				var proof struct{ Authorities []generated.SignerAuthority }
				if json.Unmarshal(p.Payload, &proof) != nil || len(proof.Authorities) != 4 {
					t.Fatal("other tenant consent omitted", len(proof.Authorities))
				}
			}
			if !bytes.Equal(held, authoritySnapshot(t, x.f.pool, other, []string{"signer_authorities", "share_chains", "ledger_events", "ledger_projections", "reservations"})) {
				t.Fatal("admission mutated other tenant holdings")
			}
		})
	}
}
