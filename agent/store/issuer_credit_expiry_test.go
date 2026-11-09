package store

import (
	"context"
	"encoding/json"
	"testing"
	"time"
)

func TestFullyIssuedCreditExpirySealsBeforeLongDelegationDeadline(t *testing.T) {
	s, d, clock, _ := renewalIssuerFixture(t)
	ctx := context.Background()
	page, err := s.BootstrapHubAuthorizations(ctx, d.HubID, d.NodeID, d.BootID, "", clock)
	if err != nil || len(page.Updates) != 1 {
		t.Fatal(page, err)
	}
	var p struct {
		Records []struct {
			Kind      string    `json:"kind"`
			ExpiresAt time.Time `json:"expires_at"`
		} `json:"authorizations"`
	}
	if err = json.Unmarshal(page.Updates[0].Payload, &p); err != nil {
		t.Fatal(err)
	}
	var expires time.Time
	for _, r := range p.Records {
		if r.Kind == "credit_grant" {
			expires = r.ExpiresAt
		}
	}
	if expires.IsZero() || !expires.Before(d.ExpiresAt) {
		t.Fatal("fixture must have earlier child expiry")
	}
	before, err := s.AuditHubDelegation(ctx, d.ID)
	if err != nil {
		t.Fatal(err)
	}
	clock.Earliest, clock.Latest = expires.Add(-time.Second), expires.Add(time.Second)
	if _, _, err = s.SealExpiredHubDelegations(ctx, d.HubID, shareID(991), "", clock); err != nil {
		t.Fatal(err)
	}
	var count int
	if err = s.db.QueryRow(`SELECT count(*) FROM funded_delegation_seals`).Scan(&count); err != nil || count != 0 {
		t.Fatal("upper bound alone sealed credit", count, err)
	}
	clock.Earliest, clock.Latest = expires, expires
	if _, _, err = s.SealExpiredHubDelegations(ctx, d.HubID, shareID(991), "", clock); err != nil {
		t.Fatal(err)
	}
	if err = s.db.QueryRow(`SELECT count(*) FROM funded_delegation_seals`).Scan(&count); err != nil || count != 1 {
		t.Fatal("expired complete child inventory did not enter closure", count, err)
	}
	after, err := s.AuditHubDelegation(ctx, d.ID)
	if err != nil || after != before {
		t.Fatal("child expiry released or altered debt", after, err)
	}
}

func TestCreditExpiryWaitsForEveryChildAndPreservesUnissuedDelegations(t *testing.T) {
	for _, full := range []bool{false, true} {
		t.Run(map[bool]string{false: "unissued", true: "all-issued"}[full], func(t *testing.T) {
			s, d, clock, _ := renewalIssuerFixture(t)
			ctx := context.Background()
			r := issuerRequest(d, 901)
			r.RawBytes = d.RawBytes / 2
			r.TTL = time.Minute
			if _, err := s.IssueHubAuthorization(ctx, r, clock); err != nil {
				t.Fatal(err)
			}
			if full {
				r.ID = shareID(902)
				r.TTL = 2 * time.Minute
				if _, err := s.IssueHubAuthorization(ctx, r, clock); err != nil {
					t.Fatal(err)
				}
			}
			issuedAt := clock.Latest
			for _, elapsed := range []time.Duration{time.Minute, 2 * time.Minute} {
				clock.Earliest, clock.Latest = issuedAt.Add(elapsed), issuedAt.Add(elapsed)
				if _, _, err := s.SealExpiredHubDelegations(ctx, d.HubID, shareID(991), "", clock); err != nil {
					t.Fatal(err)
				}
				var n int
				if err := s.db.QueryRow(`SELECT count(*) FROM funded_delegation_seals`).Scan(&n); err != nil {
					t.Fatal(err)
				}
				want := 0
				if full && elapsed == 2*time.Minute {
					want = 1
				}
				if n != want {
					t.Fatal("premature or missing closure", elapsed, n, want)
				}
			}
		})
	}
}
