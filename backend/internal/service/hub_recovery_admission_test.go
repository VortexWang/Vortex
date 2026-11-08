package service_test

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"vortex/agent/store"
	"vortex/backend/internal/auth"
	"vortex/backend/internal/policy"
	"vortex/backend/internal/service"
)

func TestHubRecoveryAdmissionRequiresCommittedProofAndCurrentScope(t *testing.T) {
	for _, name := range []string{"valid", "no_admission", "unsigned", "boot", "backend", "expiry", "new_authority", "node_boot", "node_disabled", "fencing", "restriction", "audit"} {
		t.Run(name, func(t *testing.T) {
			x, hs, a, r, _ := admissionFixture(t)
			ctx := context.Background()
			admin := policyScope(t, x.f.tenant, auth.PlatformAdmin, policy.Grant)
			if name != "no_admission" {
				if _, e := x.s.AdmitHubSameStore(ctx, admin, r.ID, uuid.NewString()); e != nil {
					t.Fatal(e)
				}
			}
			peer, boot, backend, request := x.f.contexts[r.HubID], r.ToBootID, shareBackendID, uuid.NewString()
			switch name {
			case "unsigned":
				peer = ctx
			case "boot":
				boot = uuid.NewString()
			case "backend":
				backend = uuid.NewString()
			case "expiry":
				x.now = x.now.Add(time.Hour)
			case "new_authority":
				if _, e := x.s.AuthorizeSigner(ctx, admin, r.HubID, 9, x.now.Add(-time.Second), x.now.Add(time.Hour), make([]byte, 32), uuid.NewString()); e != nil {
					t.Fatal(e)
				}
			case "node_boot":
				if _, e := x.f.pool.Exec(ctx, "UPDATE nodes SET boot_id=$1 WHERE id=$2", uuid.NewString(), a.NodeID); e != nil {
					t.Fatal(e)
				}
			case "node_disabled":
				if _, e := x.f.pool.Exec(ctx, "UPDATE nodes SET lifecycle='disabled' WHERE id=$1", a.NodeID); e != nil {
					t.Fatal(e)
				}
			case "fencing":
				if e := x.s.RecordNodeFencing(x.f.contexts[a.NodeID], x.f.scope, a.NodeID, a.BootID, int64(a.FencingFloor)+1, uuid.NewString()); e != nil {
					t.Fatal(e)
				}
			case "restriction":
				if _, e := x.f.pool.Exec(ctx, "UPDATE tenants SET account_state='suspended' WHERE id=$1", x.f.tenant); e != nil {
					t.Fatal(e)
				}
			case "audit":
				failAudit(t, x.f.pool, request)
			}
			tables := []string{"hub_same_store_admissions", "hub_funded_allocations", "share_chains", "share_generations", "share_renewal_authorities", "reservations", "ledger_events", "ledger_projections", "settlements", "operations", "tasks", "outbox"}
			before := authoritySnapshot(t, x.f.pool, x.f.tenant, tables)
			raw, available, e := x.s.HubRecoveryAdmission(peer, r.HubID, boot, backend, request)
			if name == "valid" {
				p, decode := store.DecodeHubRecoveryAdmission(raw)
				if e != nil || decode != nil || available || p.RecoveryID != r.ID || p.SealedSequence != 1 {
					t.Fatal(p, available, e, decode)
				}
			} else if e == nil {
				t.Fatal("stale/unapproved admission delivered", name, available)
			}
			if !bytes.Equal(before, authoritySnapshot(t, x.f.pool, x.f.tenant, tables)) {
				t.Fatal("admission delivery changed business/financial state")
			}
			rows, e := hs.PendingLedger(ctx, 1000)
			if e != nil || len(rows) != 2 {
				t.Fatal("server delivery alone ACKed Hub", len(rows), e)
			}
			if _, e = hs.IssueHubAuthorization(ctx, store.IssueRequest{ID: uuid.NewString(), HubID: r.HubID, DelegationID: a.DelegationID, NodeID: a.NodeID, BootID: a.BootID, RawBytes: 1, TTL: time.Minute}, store.IssuerTime{Earliest: x.now, Latest: x.now, Trusted: true}); e == nil || name == "valid" && !errors.Is(e, store.ErrAuthorityHandoffRequired) {
				t.Fatal("server delivery alone created local admission", e)
			}
		})
	}
}

func TestHubRecoveryAdmissionOldEpochClosureCannotQueueGenerationSuccessor(t *testing.T) {
	x, _, a, r, _ := admissionFixture(t)
	ctx := context.Background()
	if _, e := x.s.AdmitHubSameStore(ctx, policyScope(t, x.f.tenant, auth.PlatformAdmin, policy.Grant), r.ID, uuid.NewString()); e != nil {
		t.Fatal(e)
	}
	// Preparing a future generation is still reversible desired state, but its
	// predecessor's S/R closure does not prove the new epoch stopped issuing.
	x.in.ExpectedGeneration = 1
	x.in.SignerEpoch = r.ToEpoch
	x.in.Nonce = uuid.NewString()
	x.in.Amount = 300000
	x.in.BurstBytes = 37500
	p := x.prepare(t)
	tables := []string{"share_chains", "share_generations", "share_handoffs", "share_renewal_authorities", "tasks", "operations", "outbox", "audit_events"}
	before := authoritySnapshot(t, x.f.pool, x.f.tenant, tables)
	if e := x.s.QueueHandoff(ctx, x.f.scope, p.Generation.ID, uuid.NewString()); !errors.Is(e, service.ErrShareUnknown) {
		t.Fatal("old-epoch admission closure authorized generation CAS", a.AllocationID, e)
	}
	if !bytes.Equal(before, authoritySnapshot(t, x.f.pool, x.f.tenant, tables)) {
		t.Fatal("unclosed admitted epoch released capacity or queued CAS")
	}
}
