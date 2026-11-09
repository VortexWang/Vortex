package store

import (
	"context"
	"errors"
	"sync"
	"testing"
)

func TestStoppedCreditRecoveryHandoffRequiresBackendFenceAndMintsNoRights(t *testing.T) {
	for _, mode := range []string{"valid", "pending-ticket", "missing-fence", "wrong-hash", "new-grant", "policy-transition"} {
		t.Run(mode, func(t *testing.T) {
			s, p, u, grant, path := creditFenceFixture(t)
			ctx := context.Background()
			if mode != "missing-fence" {
				if _, e := applyFence(t, s, p); e != nil {
					t.Fatal(e)
				}
			}
			if mode == "pending-ticket" {
				use := AuthorizationUse{NodeID: u.NodeID, BootID: u.BootID, AuthorizationID: grant.AuthorizationID, Kind: "credit_grant", RuleID: grant.RuleID, RuleRevision: grant.RuleRevision, PathGeneration: grant.PathGeneration, BillingSnapshotID: grant.BillingSnapshotID, Earliest: p.IssuedAt, Latest: p.IssuedAt, TrustedTime: true}
				r := UsageReservation{NodeID: u.NodeID, BootID: u.BootID, AuthorizationID: grant.AuthorizationID, BatchID: shareID(6204), SnapshotID: grant.BillingSnapshotID, FlowKey: "pending-before-recovery", RuleID: grant.RuleID, Direction: "egress", Reserved: 64}
				if _, e := s.ReserveAuthorizedUsage(ctx, r, use); e != nil {
					t.Fatal(e)
				}
			}
			if e := s.StopCreditGrant(ctx, u.NodeID, u.BootID, u.IssuerID, grant.AuthorizationID); e != nil {
				t.Fatal(e)
			}
			before, e := s.AuditAuthorizationBalance(ctx, u.NodeID, grant.AuthorizationID)
			if e != nil {
				t.Fatal(e)
			}
			next := p.Root
			next.Revision++
			next.SignerEpoch++
			if e = s.InstallShareSourceAnchor(ctx, p.NodeID, p.BackendID, next); e != nil {
				t.Fatal(e)
			}
			update := shareUpdate(u, 6200, next)
			update.SignerEpoch = 2
			h := EpochHandoff{TransferID: p.RecoveryID, ParentID: p.ParentID, SourceID: p.Root.SourceID, From: 1, To: 2, Remaining: p.Remaining, SealedSequence: p.SealedSequence, RecordSHA256: p.ManifestSHA256}
			if mode == "wrong-hash" {
				h.RecordSHA256 = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
			}
			update.Handoffs = []EpochHandoff{h}
			var records []AuthorizationRecord
			if mode == "new-grant" {
				r := grant
				r.AuthorizationID = shareID(6201)
				r.SignerEpoch = 2
				r.AmountBytes = 1
				records = []AuthorizationRecord{r}
			}
			if mode == "policy-transition" {
				update.PolicyTransitions = []PolicyTransition{{}}
			}
			if mode == "valid" {
				if _, e = s.db.Exec(`CREATE TRIGGER fail_stopped_handoff BEFORE INSERT ON authorization_handoffs BEGIN SELECT RAISE(ABORT,'FULL failure'); END`); e != nil {
					t.Fatal(e)
				}
				if e = s.PersistAuthorizationUpdate(ctx, update, nil, nil); e == nil {
					t.Fatal("FULL failure returned resumed history")
				}
				var count int
				if e = s.db.QueryRow(`SELECT COUNT(*) FROM authorization_handoffs`).Scan(&count); e != nil || count != 0 {
					t.Fatal("partial handoff escaped", count, e)
				}
				if _, e = s.db.Exec(`DROP TRIGGER fail_stopped_handoff`); e != nil {
					t.Fatal(e)
				}
			}
			e = s.PersistAuthorizationUpdate(ctx, update, records, nil)
			if mode != "valid" && mode != "pending-ticket" {
				if e == nil {
					t.Fatal("unproven/stopped source minted rights", mode)
				}
				return
			}
			if e != nil {
				t.Fatal("exact settlement-only recovery refused", e)
			}
			if e = s.PersistAuthorizationUpdate(ctx, update, nil, nil); e != nil {
				t.Fatal("exact replay", e)
			}
			var wg sync.WaitGroup
			for range 8 {
				wg.Go(func() {
					if err := s.PersistAuthorizationUpdate(ctx, update, nil, nil); err != nil {
						t.Error("concurrent replay", err)
					}
				})
			}
			wg.Wait()
			if e = s.Close(); e != nil {
				t.Fatal(e)
			}
			s, e = Open(path, "agent")
			if e != nil {
				t.Fatal(e)
			}
			defer s.Close()
			if e = s.PersistAuthorizationUpdate(ctx, update, nil, nil); e != nil {
				t.Fatal("reopen replay", e)
			}
			if mode == "pending-ticket" {
				if _, e = s.FinalizeStoppedCreditGrant(ctx, u.NodeID, u.BootID, u.IssuerID, grant.AuthorizationID); !errors.Is(e, ErrCreditGrantUnsettled) {
					t.Fatal("handoff finalized unresolved ticket", e)
				}
			}
			after, e := s.AuditAuthorizationBalance(ctx, u.NodeID, grant.AuthorizationID)
			if e != nil || after != before {
				t.Fatal("recovery changed credit debt", before, after, e)
			}
			use := AuthorizationUse{NodeID: u.NodeID, BootID: u.BootID, AuthorizationID: grant.AuthorizationID, Kind: "credit_grant", RuleID: grant.RuleID, RuleRevision: grant.RuleRevision, PathGeneration: grant.PathGeneration, BillingSnapshotID: grant.BillingSnapshotID, Earliest: p.IssuedAt, Latest: p.IssuedAt, TrustedTime: true, ExistingSession: true}
			if _, e = s.CheckStoredAuthorization(ctx, use); !errors.Is(e, ErrAuthorizationDenied) {
				t.Fatal("stopped original credit revived", e)
			}
			r := grant
			r.AuthorizationID = shareID(6202)
			r.SignerEpoch = 2
			r.AmountBytes = 1
			fresh := shareUpdate(update, 6203, next)
			fresh.Handoffs = nil
			if e = s.PersistAuthorizationUpdate(ctx, fresh, []AuthorizationRecord{r}, nil); e == nil {
				t.Fatal("recorded history reopened stopped source")
			}
		})
	}
}
