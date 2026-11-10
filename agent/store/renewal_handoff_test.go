package store

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"
	"vortex/contracts"
)

func TestRenewalCandidatesFollowOnlyExactLiveFundedHandoff(t *testing.T) {
	for _, mode := range []string{"valid", "stopped", "missing-handoff", "changed-limit", "old-path", "wrong-fence", "old-boot", "untrusted-time", "stopped-before-journal", "changed-root-before-journal", "new-epoch-before-journal"} {
		t.Run(mode, func(t *testing.T) {
			s, p, u, grant, path := creditFenceFixture(t)
			ctx := context.Background()
			use := AuthorizationUse{NodeID: u.NodeID, BootID: u.BootID, RuleID: grant.RuleID, RuleRevision: grant.RuleRevision, PathGeneration: grant.PathGeneration, BillingSnapshotID: grant.BillingSnapshotID, AuthorizationID: grant.AuthorizationID, Kind: grant.Kind, Earliest: p.IssuedAt, Latest: p.IssuedAt, TrustedTime: true, FencingGeneration: u.FencingGenerationFloor}
			hold := UsageReservation{NodeID: u.NodeID, BootID: u.BootID, BatchID: "renewal-before-handoff", SnapshotID: grant.BillingSnapshotID, RuleID: grant.RuleID, FlowKey: "pending", Direction: "egress", Reserved: 200, AuthorizationID: grant.AuthorizationID}
			if _, err := s.ReserveAuthorizedUsage(ctx, hold, use); err != nil {
				t.Fatal(err)
			}
			before, err := s.AuditAuthorizationBalance(ctx, u.NodeID, grant.AuthorizationID)
			if err != nil {
				t.Fatal(err)
			}
			if _, err = applyFence(t, s, p); err != nil {
				t.Fatal(err)
			}
			next := p.Root
			next.Revision++
			next.SignerEpoch++
			if mode == "changed-limit" {
				next.RateBPS--
			}
			if err = s.InstallShareSourceAnchor(ctx, p.NodeID, p.BackendID, next); err != nil {
				t.Fatal(err)
			}
			update := shareUpdate(u, 6400, next)
			update.SignerEpoch = 2
			if mode != "missing-handoff" {
				update.Handoffs = []EpochHandoff{{TransferID: p.RecoveryID, ParentID: p.ParentID, SourceID: p.Root.SourceID, From: 1, To: 2, Remaining: p.Remaining, SealedSequence: p.SealedSequence, RecordSHA256: p.ManifestSHA256}}
			}
			if err = s.PersistAuthorizationUpdate(ctx, update, nil, nil); err != nil {
				t.Fatal(err)
			}
			switch mode {
			case "stopped":
				if err = s.StopCreditGrant(ctx, u.NodeID, u.BootID, u.IssuerID, grant.AuthorizationID); err != nil {
					t.Fatal(err)
				}
			case "old-path":
				if _, err = s.db.Exec(`UPDATE agent_state SET value='2' WHERE key='authorization_path_generation'`); err != nil {
					t.Fatal(err)
				}
			case "wrong-fence":
				use.FencingGeneration++
			case "old-boot":
				use.BootID = shareID(6401)
			case "untrusted-time":
				use.TrustedTime = false
			}
			due, err := s.DueRenewals(ctx, use, u.IssuerID, 30)
			if mode == "untrusted-time" {
				if err == nil {
					t.Fatal("untrusted candidate")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if mode != "valid" && !strings.HasSuffix(mode, "before-journal") {
				if len(due) != 0 {
					t.Fatal("unsafe transferred trigger", due)
				}
				return
			}
			if len(due) != 1 {
				t.Fatal("live transferred source lost automatic trigger", due)
			}
			req := due[0].Request
			if req.SignerEpoch != 2 || req.SourceId != grant.SourceID || req.DelegationId != grant.DelegationID || req.RequestedRawBytes != grant.AmountBytes || req.RequestedTtlSeconds != uint64(time.Minute/time.Second) {
				t.Fatal(req)
			}
			req.RequestId = shareID(6402)
			req.RequestSha256, err = contracts.RenewalDigest(req)
			if err != nil {
				t.Fatal(err)
			}
			if strings.HasSuffix(mode, "before-journal") {
				switch mode {
				case "stopped-before-journal":
					err = s.StopCreditGrant(ctx, u.NodeID, u.BootID, u.IssuerID, grant.AuthorizationID)
				case "changed-root-before-journal":
					latest := next
					latest.Revision++
					err = s.InstallShareSourceAnchor(ctx, p.NodeID, p.BackendID, latest)
				case "new-epoch-before-journal":
					_, err = s.db.Exec(`UPDATE agent_state SET value='3' WHERE key='authorization_signer_epoch'`)
				}
				if err != nil {
					t.Fatal(err)
				}
				if err = s.PersistTriggeredRenewal(ctx, u.IssuerID, req, due[0].GrantID); err == nil {
					t.Fatal("stale observed trigger journaled")
				}
				return
			}
			if _, err = s.db.Exec(`CREATE TRIGGER fail_renewal_trigger BEFORE INSERT ON renewal_requests BEGIN SELECT RAISE(ABORT,'FULL trigger failure'); END`); err != nil {
				t.Fatal(err)
			}
			if err = s.PersistTriggeredRenewal(ctx, u.IssuerID, req, due[0].GrantID); err == nil {
				t.Fatal("failed FULL journal escaped")
			}
			var count int
			if err = s.db.QueryRow(`SELECT COUNT(*) FROM renewal_requests`).Scan(&count); err != nil || count != 0 {
				t.Fatal("partial trigger", count, err)
			}
			if _, err = s.db.Exec(`DROP TRIGGER fail_renewal_trigger`); err != nil {
				t.Fatal(err)
			}
			if err = s.PersistTriggeredRenewal(ctx, u.IssuerID, req, due[0].GrantID); err != nil {
				t.Fatal("effective trigger refused", err)
			}
			if err = s.PersistTriggeredRenewal(ctx, u.IssuerID, req, due[0].GrantID); err != nil {
				t.Fatal("trigger replay", err)
			}
			var wg sync.WaitGroup
			for range 8 {
				wg.Go(func() {
					if err := s.PersistTriggeredRenewal(ctx, u.IssuerID, req, due[0].GrantID); err != nil {
						t.Error(err)
					}
				})
			}
			wg.Wait()
			if err = s.Close(); err != nil {
				t.Fatal(err)
			}
			s, err = Open(path, "agent")
			if err != nil {
				t.Fatal(err)
			}
			defer s.Close()
			if err = s.PersistTriggeredRenewal(ctx, u.IssuerID, req, due[0].GrantID); err != nil {
				t.Fatal("reopen trigger", err)
			}
			after, err := s.AuditAuthorizationBalance(ctx, u.NodeID, grant.AuthorizationID)
			if err != nil || before != after {
				t.Fatal("trigger changed pending debt", before, after, err)
			}
			due, err = s.DueRenewals(ctx, use, u.IssuerID, 30)
			if err != nil || len(due) != 0 {
				t.Fatal("outstanding trigger duplicated", due, err)
			}
		})
	}
}
