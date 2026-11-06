package store

import (
	"context"
	"testing"
	"time"

	"vortex/contracts"
	controlv1 "vortex/contracts/protobuf/v1"
)

func TestRenewalCandidatesRespectLowWaterTTLAndOriginalBounds(t *testing.T) {
	for _, scenario := range []string{"above", "low-water", "ttl", "expired", "unknown-time", "old-boot", "stale-root"} {
		t.Run(scenario, func(t *testing.T) {
			s, r, u, _ := shareFixture(t)
			ctx := context.Background()
			if err := s.InstallShareSourceAnchor(ctx, u.NodeID, shareID(90), r); err != nil {
				t.Fatal(err)
			}
			a := shareGrant(r, u)
			if err := s.PersistAuthorizationUpdate(ctx, u, []AuthorizationRecord{a}, nil); err != nil {
				t.Fatal(err)
			}
			use := AuthorizationUse{NodeID: u.NodeID, BootID: u.BootID, RuleID: a.RuleID, RuleRevision: a.RuleRevision, PathGeneration: a.PathGeneration, BillingSnapshotID: a.BillingSnapshotID, AuthorizationID: a.AuthorizationID, Kind: a.Kind, Earliest: r.NotBefore.Add(time.Second), Latest: r.NotBefore.Add(time.Second), TrustedTime: true, FencingGeneration: 1}
			switch scenario {
			case "low-water":
				reservation := UsageReservation{NodeID: u.NodeID, BootID: u.BootID, BatchID: "renewal-hold", SnapshotID: a.BillingSnapshotID, RuleID: a.RuleID, FlowKey: "flow", Direction: "egress", Reserved: 200, AuthorizationID: a.AuthorizationID}
				if _, err := s.ReserveAuthorizedUsage(ctx, reservation, use); err != nil {
					t.Fatal(err)
				}
			case "ttl":
				use.Earliest = r.NotBefore.Add(41 * time.Second)
				use.Latest = use.Earliest
			case "expired":
				use.Earliest = r.NotBefore.Add(time.Minute)
				use.Latest = use.Earliest
			case "unknown-time":
				use.TrustedTime = false
			case "old-boot":
				use.BootID = shareID(99)
			case "stale-root":
				next := r
				next.Revision++
				if err := s.InstallShareSourceAnchor(ctx, u.NodeID, shareID(90), next); err != nil {
					t.Fatal(err)
				}
			}
			due, err := s.DueRenewals(ctx, use, u.IssuerID, 30)
			if scenario == "unknown-time" {
				if err == nil {
					t.Fatal("untrusted time produced candidate")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			want := scenario == "low-water" || scenario == "ttl"
			if want && len(due) != 1 || !want && len(due) != 0 {
				t.Fatalf("due=%+v", due)
			}
			if !want {
				return
			}
			candidate := due[0]
			request := candidate.Request
			if request.DelegationId != a.DelegationID || request.SourceId != a.SourceID || request.RequestedRawBytes != 256 || request.RequestedTtlSeconds != 60 || request.RuleRevision != a.RuleRevision || request.PathGeneration != a.PathGeneration {
				t.Fatalf("scope changed: %+v", request)
			}
			request.RequestId = shareID(100)
			request.RequestSha256, err = contracts.RenewalDigest(request)
			if err != nil {
				t.Fatal(err)
			}
			if err := s.PersistTriggeredRenewal(ctx, u.IssuerID, request, candidate.GrantID); err != nil {
				t.Fatal(err)
			}
			if err := s.PersistRenewalResult(ctx, u.IssuerID, &controlv1.RenewalResult{RequestId: request.RequestId, RequestSha256: request.RequestSha256, Code: "quota_exhausted"}); err != nil {
				t.Fatal(err)
			}
			due, err = s.DueRenewals(ctx, use, u.IssuerID, 30)
			if err != nil || len(due) != 0 {
				t.Fatalf("terminal refusal spawned fresh requests: %+v %v", due, err)
			}
			request.RequestId = shareID(101)
			request.RequestSha256, _ = contracts.RenewalDigest(request)
			if err := s.PersistTriggeredRenewal(ctx, u.IssuerID, request, candidate.GrantID); err == nil {
				t.Fatal("same grant retriggered under fresh ID")
			}
			if scenario == "low-water" {
				var state, amount string
				if err := s.db.QueryRow(`SELECT state,reserved_bytes FROM usage_reservations WHERE batch_id='renewal-hold'`).Scan(&state, &amount); err != nil || state != "reserved" || amount != "200" {
					t.Fatalf("renewal released hold: %s %s %v", state, amount, err)
				}
			}
		})
	}
}
