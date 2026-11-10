package store

import (
	"bytes"
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"
)

func TestRecoveredShareClosureFollowsTwoEpochsFULLRollbackConcurrentReplay(t *testing.T) {
	s, d, clock, path := renewalIssuerFixture(t)
	ctx := context.Background()
	original, _ := json.Marshal(d)
	oldRequest := issuerRequest(d, 981)
	old, e := s.IssueHubAuthorization(ctx, oldRequest, clock)
	if e != nil {
		t.Fatal(e)
	}
	h, clock := fundedHandoffRequest(t, d)
	if _, e = s.HandoffHub(ctx, h, clock); e != nil {
		t.Fatal(e)
	}
	completeFundedHandoff(t, s, clock)
	if _, e = s.IssueHubAuthorization(ctx, issuerRequest(d, 982), clock); e != nil {
		t.Fatal(e)
	}
	h.ID = shareID(983)
	h.FromEpoch++
	h.ToEpoch++
	h.Heartbeat = h.Heartbeat.Add(5 * time.Second)
	for i := range h.FundedSuccessors[0].ShareReferences {
		h.FundedSuccessors[0].ShareReferences[i].SignerEpoch++
		h.FundedSuccessors[0].ShareReferences[i].Revision++
	}
	clock.Earliest, clock.Latest = h.Heartbeat, h.Heartbeat.Add(time.Second)
	if _, e = s.HandoffHub(ctx, h, clock); e != nil {
		t.Fatal(e)
	}
	// Closing is permitted before finite recovery ACK, but grants remain denied.
	c := closureFixture(d, []string{d.ID})
	c.SignerEpoch = 3
	raw, _ := json.Marshal(c)
	before, e := s.AuditHubDelegation(ctx, d.ID)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.db.Exec(`CREATE TRIGGER fail_recovered_closure BEFORE INSERT ON share_renewal_closures BEGIN SELECT RAISE(ABORT,'FULL failure'); END`); e != nil {
		t.Fatal(e)
	}
	if _, e = s.ApplyShareRenewalClosure(ctx, c.HubID, c.HubBootID, c.BackendID, raw); e == nil {
		t.Fatal("FULL failure returned closure")
	}
	var state string
	if e = s.db.QueryRow(`SELECT state FROM issuer_delegations WHERE delegation_id=?`, d.ID).Scan(&state); e != nil || state != "open" {
		t.Fatal(state, e)
	}
	if _, e = s.db.Exec(`DROP TRIGGER fail_recovered_closure`); e != nil {
		t.Fatal(e)
	}
	other, e := Open(path, "hub")
	if e != nil {
		t.Fatal(e)
	}
	defer other.Close()
	var wg sync.WaitGroup
	for i := range 8 {
		db := []*Store{s, other}[i%2]
		wg.Go(func() {
			r, e := db.ApplyShareRenewalClosure(ctx, c.HubID, c.HubBootID, c.BackendID, raw)
			if e != nil || r.CommandDigest == "" {
				t.Error(r, e)
			}
		})
	}
	wg.Wait()
	if _, e = other.IssueHubAuthorization(ctx, issuerRequest(d, 984), clock); e == nil {
		t.Fatal("closed recovered issuer spent")
	}
	if _, e = other.RefreshHubDelegationLeases(ctx, d.HubID, d.NodeID, d.BootID, d.ID, clock); e == nil {
		t.Fatal("closed recovered issuer refreshed")
	}
	replay, e := other.IssueHubAuthorization(ctx, oldRequest, clock)
	if e != nil || !bytes.Equal(replay.Payload, old.Payload) {
		t.Fatal("original issue replay changed", e)
	}
	after, e := other.AuditHubDelegation(ctx, d.ID)
	if e != nil || before != after {
		t.Fatal("closure changed funds", before, after, e)
	}
	var retained []byte
	if e = other.db.QueryRow(`SELECT payload FROM issuer_delegations WHERE delegation_id=?`, d.ID).Scan(&retained); e != nil || !bytes.Equal(original, retained) {
		t.Fatal("original delegation changed", e)
	}
	seal, e := other.SealHubDelegation(ctx, d.HubID, c.HubBootID, d.ID)
	if e != nil || seal.SignerEpoch != 3 || seal.Issued != 512 || seal.Unissued != 512 {
		t.Fatal("closure lost original partition", seal, e)
	}
}

func TestRecoveredShareClosureRejectsUnprovenEpochAndCorruptOriginal(t *testing.T) {
	for _, name := range []string{"epoch_column_only", "corrupt_original", "missing_handoff", "corrupt_handoff", "stale_command"} {
		t.Run(name, func(t *testing.T) {
			s, d, _, _ := renewalIssuerFixture(t)
			ctx := context.Background()
			h, clock := fundedHandoffRequest(t, d)
			if name != "epoch_column_only" {
				if _, e := s.HandoffHub(ctx, h, clock); e != nil {
					t.Fatal(e)
				}
			}
			switch name {
			case "epoch_column_only":
				_, e := s.db.Exec(`UPDATE issuer_delegations SET signer_epoch='2' WHERE delegation_id=?`, d.ID)
				if e != nil {
					t.Fatal(e)
				}
			case "corrupt_original":
				_, e := s.db.Exec(`UPDATE issuer_delegations SET payload=? WHERE delegation_id=?`, []byte("corrupt"), d.ID)
				if e != nil {
					t.Fatal(e)
				}
			case "missing_handoff":
				_, e := s.db.Exec(`DROP TRIGGER immutable_hub_handoff_update; UPDATE hub_handoffs SET from_epoch='0'`)
				if e != nil {
					t.Fatal(e)
				}
			case "corrupt_handoff":
				_, e := s.db.Exec(`DROP TRIGGER immutable_hub_handoff_journal_update; UPDATE hub_ledger SET payload='corrupt' WHERE kind='handoff'`)
				if e != nil {
					t.Fatal(e)
				}
			}
			c := closureFixture(d, []string{d.ID})
			c.SignerEpoch = 2
			if name == "stale_command" {
				c.SignerEpoch = 1
			}
			raw, _ := json.Marshal(c)
			if _, e := s.ApplyShareRenewalClosure(ctx, c.HubID, c.HubBootID, c.BackendID, raw); e == nil {
				t.Fatal("unproven epoch closure accepted")
			}
			var state string
			var count int
			if e := s.db.QueryRow(`SELECT state FROM issuer_delegations WHERE delegation_id=?`, d.ID).Scan(&state); e != nil || state != "open" {
				t.Fatal(state, e)
			}
			if e := s.db.QueryRow(`SELECT count(*) FROM closed_share_delegations`).Scan(&count); e != nil || count != 0 {
				t.Fatal("tombstone escaped failed verification", count, e)
			}
		})
	}
}
