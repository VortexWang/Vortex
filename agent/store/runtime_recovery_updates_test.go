package store

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sync"
	"testing"
)

func TestRuntimeRecoveryUpdatesRequireFiniteBarrierAndReplayWithoutCredit(t *testing.T) {
	s, d, clock, path := renewalIssuerFixture(t)
	ctx := context.Background()
	if _, e := s.BootstrapHubAuthorizations(ctx, d.HubID, d.NodeID, d.BootID, "", clock); e != nil {
		t.Fatal(e)
	}
	h, clock := fundedHandoffRequest(t, d)
	if _, e := s.HandoffHub(ctx, h, clock); e != nil {
		t.Fatal(e)
	}
	before, e := s.AuditHubDelegation(ctx, d.ID)
	if e != nil {
		t.Fatal(e)
	}
	p, e := s.RuntimeRecoveryUpdates(ctx, d.HubID, d.NodeID, d.BootID, "", clock)
	if e != nil || len(p.Updates) != 0 {
		t.Fatal("unconfirmed finite prefix delivered runtime recovery", p, e)
	}
	completeFundedHandoff(t, s, clock)
	bootstrap, e := s.BootstrapHubAuthorizations(ctx, d.HubID, d.NodeID, d.BootID, "", clock)
	if e != nil || len(bootstrap.Updates) != 0 {
		t.Fatal("recovery bootstrapped transferred partition", bootstrap, e)
	}

	p, e = s.RuntimeRecoveryUpdates(ctx, d.HubID, d.NodeID, d.BootID, "", clock)
	if e != nil || len(p.Updates) != 1 || !p.Complete {
		t.Fatal(p, e)
	}
	var update struct {
		UpdateID       string            `json:"update_id"`
		Epoch          uint64            `json:"signer_epoch"`
		Authorizations []json.RawMessage `json:"authorizations"`
		Handoffs       []EpochHandoff    `json:"epoch_handoffs"`
		References     []ShareReference  `json:"share_references"`
	}
	if json.Unmarshal(p.Updates[0].Payload, &update) != nil || update.Epoch != 2 || len(update.Authorizations) != 0 || len(update.Handoffs) != 3 || len(update.References) != 3 {
		t.Fatal("recovery minted/widened rights", update)
	}
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			replay, e := s.RuntimeRecoveryUpdates(ctx, d.HubID, d.NodeID, d.BootID, "", clock)
			if e != nil || len(replay.Updates) != 1 || !bytes.Equal(replay.Updates[0].Payload, p.Updates[0].Payload) {
				t.Error(replay, e)
			}
		})
	}
	wg.Wait()
	if e = s.Close(); e != nil {
		t.Fatal(e)
	}
	s, e = Open(path, "hub")
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	replay, e := s.RuntimeRecoveryUpdates(ctx, d.HubID, d.NodeID, d.BootID, "", clock)
	if e != nil || len(replay.Updates) != 1 || !bytes.Equal(replay.Updates[0].Payload, p.Updates[0].Payload) {
		t.Fatal("reopen changed recovery bytes", replay, e)
	}
	after, e := s.AuditHubDelegation(ctx, d.ID)
	if e != nil || after != before {
		t.Fatal("recovery replay changed partition", before, after, e)
	}
	for _, boot := range []string{shareID(997), d.BootID} {
		cursor := ""
		if boot == d.BootID {
			cursor = p.After
		}
		page, e := s.RuntimeRecoveryUpdates(ctx, d.HubID, d.NodeID, boot, cursor, clock)
		if e != nil || len(page.Updates) != 0 {
			t.Fatal("wrong boot/cursor selected recovery", page, e)
		}
	}
	if _, e = s.RuntimeRecoveryUpdates(ctx, d.HubID, d.NodeID, d.BootID, "garbage", clock); e == nil {
		t.Fatal("invalid cursor")
	}
	if _, e = s.RuntimeRecoveryUpdates(ctx, d.HubID, d.NodeID, d.BootID, "", IssuerTime{}); e == nil {
		t.Fatal("untrusted recovery")
	}
}

func TestRuntimeRecoveryUpdatesPageDistinctPathsAndFULLRollback(t *testing.T) {
	s, d, clock, _ := renewalIssuerFixture(t)
	ctx := context.Background()
	h, later := fundedHandoffRequest(t, d)
	h.FundedSuccessors = nil
	var delegations []HubDelegation
	for i := 0; i < 34; i++ {
		next := d
		if i > 0 {
			next.ID, next.SourceID, next.RuntimeSourceID, next.ShareSourceID = shareID(2000+i*5), shareID(2001+i*5), shareID(2002+i*5), shareID(2003+i*5)
			next.PathGeneration = d.PathGeneration + uint64(i)
			next.ShareReferences = append([]ShareReference(nil), d.ShareReferences...)
			for j, source := range []string{next.SourceID, next.RuntimeSourceID, next.ShareSourceID} {
				r := &next.ShareReferences[j]
				r.ID = shareID(3000 + i*3 + j)
				r.SourceID = source
				r.PathGeneration = next.PathGeneration
				if e := s.InstallShareSourceAnchor(ctx, next.NodeID, shareID(799), *r); e != nil {
					t.Fatal(e)
				}
			}
			if e := s.InstallHubDelegation(ctx, next, clock); e != nil {
				t.Fatal(e)
			}
		}
		// Last path has no issue: it must not send a handoff for an unknown family.
		if i < 33 {
			if _, e := s.IssueHubAuthorization(ctx, issuerRequest(next, 4000+i), clock); e != nil {
				t.Fatal(e)
			}
		}
		raw, _ := json.Marshal(next)
		hash := sha256.Sum256(raw)
		roots := append([]ShareReference(nil), next.ShareReferences...)
		for j := range roots {
			roots[j].Revision++
			roots[j].SignerEpoch++
		}
		h.FundedSuccessors = append(h.FundedSuccessors, FundedHandoffSuccessor{DelegationID: next.ID, AllocationSHA256: hex.EncodeToString(hash[:]), ShareReferences: roots})
		delegations = append(delegations, next)
	}
	if _, e := s.HandoffHub(ctx, h, later); e != nil {
		t.Fatal(e)
	}
	completeFundedHandoff(t, s, later)
	if _, e := s.db.Exec(`CREATE TRIGGER fail_runtime_recovery BEFORE INSERT ON hub_recovery_updates BEGIN SELECT RAISE(ABORT,'FULL failure'); END`); e != nil {
		t.Fatal(e)
	}
	if p, e := s.RuntimeRecoveryUpdates(ctx, d.HubID, d.NodeID, d.BootID, "", later); e == nil || len(p.Updates) != 0 {
		t.Fatal("write failure exposed authority", p, e)
	}
	var count int
	if e := s.db.QueryRow(`SELECT COUNT(*) FROM hub_recovery_updates`).Scan(&count); e != nil || count != 0 {
		t.Fatal("partial FULL outbox", count, e)
	}
	if _, e := s.db.Exec(`DROP TRIGGER fail_runtime_recovery`); e != nil {
		t.Fatal(e)
	}
	p, e := s.RuntimeRecoveryUpdates(ctx, d.HubID, d.NodeID, d.BootID, "", later)
	if e != nil || len(p.Updates) != 32 || p.Complete {
		t.Fatal("first bounded page", p, e)
	}
	q, e := s.RuntimeRecoveryUpdates(ctx, d.HubID, d.NodeID, d.BootID, p.After, later)
	if e != nil || len(q.Updates) != 1 || !q.Complete {
		t.Fatal("tail/never-issued path", q, e)
	}
	ids := map[string]bool{}
	for _, u := range append(p.Updates, q.Updates...) {
		var v struct {
			ID     string         `json:"update_id"`
			Path   uint64         `json:"path_generation"`
			Proofs []EpochHandoff `json:"epoch_handoffs"`
		}
		if json.Unmarshal(u.Payload, &v) != nil || ids[v.ID] || v.ID == h.ID || len(v.Proofs) != 3 {
			t.Fatal("colliding recovery identity", v)
		}
		ids[v.ID] = true
	}
	for _, next := range delegations {
		b, e := s.AuditHubDelegation(ctx, next.ID)
		if e != nil || b.Issued+b.Remaining != next.RawBytes {
			t.Fatal("paging changed conservation", b, e)
		}
	}
}

func TestRuntimeRecoveryUpdatesRetainLegacyFULLPayloadIdentity(t *testing.T) {
	s, d, clock, _ := renewalIssuerFixture(t)
	ctx := context.Background()
	if _, e := s.IssueHubAuthorization(ctx, issuerRequest(d, 5100), clock); e != nil {
		t.Fatal(e)
	}
	h, clock := fundedHandoffRequest(t, d)
	if _, e := s.HandoffHub(ctx, h, clock); e != nil {
		t.Fatal(e)
	}
	completeFundedHandoff(t, s, clock)
	first, e := s.RuntimeRecoveryUpdates(ctx, d.HubID, d.NodeID, d.BootID, "", clock)
	if e != nil || len(first.Updates) != 1 {
		t.Fatal(first, e)
	}
	var legacy map[string]any
	if e = json.Unmarshal(first.Updates[0].Payload, &legacy); e != nil {
		t.Fatal(e)
	}
	legacy["update_id"] = h.ID
	raw, e := json.Marshal(legacy)
	if e != nil {
		t.Fatal(e)
	}
	digest := sha256.Sum256(raw)
	// Simulate a database written by the original RecoveryAuthorization producer.
	// Its pre-existing outbox is not regenerated by the new path-scoped producer.
	if _, e = s.db.Exec(`UPDATE hub_recovery_updates SET payload=?,payload_sha256=? WHERE handoff_id=?`, raw, digest[:], h.ID); e != nil {
		t.Fatal(e)
	}
	replay, e := s.RuntimeRecoveryUpdates(ctx, d.HubID, d.NodeID, d.BootID, "", clock)
	if e != nil || len(replay.Updates) != 1 || !bytes.Equal(raw, replay.Updates[0].Payload) {
		t.Fatal("legacy FULL identity rewritten", replay, e)
	}
}
