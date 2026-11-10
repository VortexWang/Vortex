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
	"vortex/agent/store"
	"vortex/backend/db/generated"
	"vortex/backend/internal/auth"
	"vortex/backend/internal/policy"
	"vortex/backend/internal/service"
)

func TestTransferredShareColdProofKeepsOriginalClosureAndDebtUntilNodeCAS(t *testing.T) {
	x, _, a, recovered := effectiveAllocationFixture(t, true)
	ctx := context.Background()
	q := generated.New(x.f.pool)
	admin := policyScope(t, x.f.tenant, auth.PlatformAdmin, policy.Grant)
	original, e := q.ShareRenewalAuthority(ctx, generated.ShareRenewalAuthorityParams{TenantID: x.f.tenant, NodeID: a.NodeID, Kind: "rate", Generation: 1, SignerID: recovered.HubID})
	if e != nil || !original.ClosedAt.Valid {
		t.Fatal(original, e)
	}
	node, e := store.Open(t.TempDir()+"/existing-node.db", "agent")
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
	if e = x.f.nodeService.SetGrant(ctx, admin, x.f.group, 1, "active", x.now.Add(-time.Hour), x.now.Add(3*time.Hour), uuid.NewString()); e != nil {
		t.Fatal(e)
	}
	x.now = x.now.Add(2 * time.Hour)
	proof := sha256.Sum256([]byte("explicit cold recovery after admitted epoch8 original hard deadlines"))
	if _, e = x.s.AuthorizeSigner(ctx, admin, recovered.HubID, 9, x.now.Add(-time.Second), x.now.Add(time.Hour), proof[:], uuid.NewString()); e != nil {
		t.Fatal(e)
	}
	x.in.SignerEpoch = 9
	x.in.OwnershipUntil = x.now.Add(10 * time.Minute)
	x.in.PolicyUntil = x.now.Add(10 * time.Minute)
	next := prepareClosureSuccessor(t, x)
	if e = x.s.PrepareRenewalClosures(ctx, x.f.scope, next.Generation.ID, shareBackendID, uuid.NewString()); e != nil {
		t.Fatal("live epoch8 closure", e)
	}
	if e = x.s.QueueHandoff(ctx, x.f.scope, next.Generation.ID, uuid.NewString()); !errors.Is(e, service.ErrShareUnknown) {
		t.Fatal("elapsed time substituted for closure", e)
	}
	coldInput := service.HubColdRecoveryInput{ID: uuid.NewString(), HubID: recovered.HubID, FromBootID: recovered.ToBootID, ToBootID: uuid.NewString(), FromEpoch: 8, ToEpoch: 9, Reason: "fresh cold boot after complete original hard boundaries"}
	cold, e := x.s.RecoverColdHubBoot(ctx, admin, coldInput, uuid.NewString())
	if e != nil {
		t.Fatal(e)
	}
	heldTables := []string{"reservations", "ledger_projections", "ledger_events", "hub_credit_grants", "settlements", "hub_funded_allocations", "share_chains", "share_renewal_authorities", "share_generations"}
	held := authoritySnapshot(t, x.f.pool, x.f.tenant, heldTables)
	changedTables := []string{"share_renewal_authorities", "share_renewal_closure_commands", "share_renewal_closure_receipts", "tasks", "outbox", "operations", "audit_events"}
	before := authoritySnapshot(t, x.f.pool, x.f.tenant, changedTables)
	request := uuid.NewString()
	failAudit(t, x.f.pool, request)
	if e = x.s.PrepareRenewalClosures(ctx, x.f.scope, next.Generation.ID, shareBackendID, request); e == nil {
		t.Fatal("cold closure escaped failed audit")
	}
	if !bytes.Equal(before, authoritySnapshot(t, x.f.pool, x.f.tenant, changedTables)) {
		t.Fatal("partial cold closure")
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
	var fake, retired int
	if e = x.f.pool.QueryRow(ctx, `SELECT count(*) FROM share_renewal_closure_receipts WHERE tenant_id=$1`, x.f.tenant).Scan(&fake); e != nil || fake != 0 {
		t.Fatal("manufactured Hub receipt", fake, e)
	}
	if e = x.f.pool.QueryRow(ctx, `SELECT count(*) FROM share_renewal_closure_commands c JOIN tasks t ON t.tenant_id=c.tenant_id AND t.operation_id=c.operation_id JOIN operations o ON o.tenant_id=c.tenant_id AND o.id=c.operation_id JOIN outbox b ON b.tenant_id=c.tenant_id AND b.operation_id=c.operation_id WHERE c.tenant_id=$1 AND t.state='failed' AND o.state='failed' AND b.delivered_at IS NOT NULL`, x.f.tenant).Scan(&retired); e != nil || retired != 1 {
		t.Fatal("unretired old boot work", retired, e)
	}
	// Even after cold proof, the fresh Hub cannot renew the transferred old share.
	renewal := x.renewal()
	renewal.SignerEpoch = 9
	renewal.Nonce = uuid.NewString()
	if _, e = x.s.RenewShare(x.f.contexts[a.NodeID], x.f.scope, renewal, uuid.NewString()); e == nil {
		t.Fatal("cold proof minted renewal authority")
	}
	// Deliberately corrupt only this test database, including rehashed canonical
	// payloads: the consumer must validate scope/inventory/bounds, not just SHA.
	type coldProof struct {
		Version         int                          `json:"version"`
		Request         service.HubColdRecoveryInput `json:"request"`
		Earliest        time.Time                    `json:"earliest"`
		Latest          time.Time                    `json:"latest"`
		OldHardDeadline time.Time                    `json:"old_hard_deadline"`
		Authorities     []generated.SignerAuthority  `json:"authorities"`
		Allocations     []json.RawMessage            `json:"allocations"`
		Shares          []generated.ShareGeneration  `json:"shares"`
	}
	var decoded coldProof
	if e = json.Unmarshal(cold.Payload, &decoded); e != nil {
		t.Fatal(e)
	}
	canonical, _ := json.Marshal(decoded)
	if !bytes.Equal(canonical, cold.Payload) {
		t.Fatal("fixture must preserve canonical proof")
	}
	if _, e = x.f.pool.Exec(ctx, `ALTER TABLE hub_cold_recoveries DISABLE TRIGGER hub_cold_recovery_immutable`); e != nil {
		t.Fatal(e)
	}
	for _, name := range []string{"hash", "wrong_boot", "missing_share", "changed_share", "duplicate_share", "early_proof", "lower_bound"} {
		t.Run(name, func(t *testing.T) {
			var p coldProof
			if e := json.Unmarshal(cold.Payload, &p); e != nil {
				t.Fatal(e)
			}
			now := x.now
			switch name {
			case "wrong_boot":
				p.Request.FromBootID = uuid.NewString()
			case "missing_share":
				p.Shares = nil
			case "changed_share":
				for i := range p.Shares {
					if p.Shares[i].Kind == "rate" {
						p.Shares[i].Amount++
					}
				}
			case "duplicate_share":
				p.Shares = append(p.Shares, p.Shares...)
			case "early_proof":
				p.Earliest = p.OldHardDeadline.Add(-time.Nanosecond)
			case "lower_bound":
				x.now = p.OldHardDeadline.Add(-time.Nanosecond)
			}
			raw, _ := json.Marshal(p)
			hash := sha256.Sum256(raw)
			if name == "hash" {
				hash[0] ^= 1
			}
			if _, e := x.f.pool.Exec(ctx, `UPDATE hub_cold_recoveries SET payload=$1,payload_digest=$2 WHERE id=$3`, raw, hash[:], cold.ID); e != nil {
				t.Fatal(e)
			}
			defer func() {
				x.now = now
				if _, e := x.f.pool.Exec(ctx, `UPDATE hub_cold_recoveries SET payload=$1,payload_digest=$2 WHERE id=$3`, cold.Payload, cold.PayloadDigest, cold.ID); e != nil {
					t.Error(e)
				}
			}()
			unchanged := authoritySnapshot(t, x.f.pool, x.f.tenant, changedTables)
			if e := x.s.PrepareRenewalClosures(ctx, x.f.scope, next.Generation.ID, shareBackendID, uuid.NewString()); e == nil {
				t.Fatal("invalid cold proof consumed")
			}
			if e := x.s.QueueHandoff(ctx, x.f.scope, next.Generation.ID, uuid.NewString()); e == nil {
				t.Fatal("invalid cold proof permitted CAS")
			}
			if !bytes.Equal(unchanged, authoritySnapshot(t, x.f.pool, x.f.tenant, changedTables)) {
				t.Fatal("invalid proof changed authoritative state")
			}
		})
	}
	if _, e = x.f.pool.Exec(ctx, `ALTER TABLE hub_cold_recoveries ENABLE TRIGGER hub_cold_recovery_immutable`); e != nil {
		t.Fatal(e)
	}
	if e = x.s.QueueHandoff(ctx, x.f.scope, next.Generation.ID, uuid.NewString()); e != nil {
		t.Fatal("proved cold closure refused CAS", e)
	}
	if !bytes.Equal(held, authoritySnapshot(t, x.f.pool, x.f.tenant, heldTables)) {
		t.Fatal("cold proof or CAS enqueue released occupation")
	}
	// Verify the immutable closure is derived from the committed cold proof and
	// remains exact under replay, without replacing the original admission hash.
	h, e := q.GetShareHandoff(ctx, generated.GetShareHandoffParams{TenantID: x.f.tenant, ID: next.Generation.ID})
	if e != nil {
		t.Fatal(e)
	}
	if len(h.Command) == 0 {
		t.Fatal("missing CAS command")
	}
	if e = x.s.QueueHandoff(ctx, x.f.scope, next.Generation.ID, uuid.NewString()); e != nil {
		t.Fatal(e)
	}
	retained, e := q.ShareRenewalAuthority(ctx, generated.ShareRenewalAuthorityParams{TenantID: x.f.tenant, NodeID: a.NodeID, Kind: "rate", Generation: 1, SignerID: recovered.HubID})
	if e != nil {
		t.Fatal(e)
	}
	b1, _ := json.Marshal(original)
	b2, _ := json.Marshal(retained)
	if !bytes.Equal(b1, b2) {
		t.Fatal("original admission closure overwritten")
	}
	financial := authoritySnapshot(t, x.f.pool, x.f.tenant, heldTables[:6])
	observation := confirmHandoff(t, x, node, next.Generation.ID)
	if observation.ActiveGeneration != 2 || observation.MinimumGeneration != 2 || observation.State != "enforced" {
		t.Fatal(observation)
	}
	chain := shareChain(t, x)
	if chain.HeldAmount != 300000 || chain.ActiveGeneration != 2 {
		t.Fatal("Node CAS exact capacity difference", chain)
	}
	if !bytes.Equal(financial, authoritySnapshot(t, x.f.pool, x.f.tenant, heldTables[:6])) {
		t.Fatal("Node capacity CAS released unknown money")
	}
	replay, e := x.s.RecoverColdHubBoot(ctx, admin, coldInput, uuid.NewString())
	if e != nil || !bytes.Equal(replay.Payload, cold.Payload) {
		t.Fatal("cold proof replay", e)
	}
}
