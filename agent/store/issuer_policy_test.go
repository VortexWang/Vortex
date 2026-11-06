package store

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"
	"time"
)

func TestIssuerFuturePeriodRequiresFundedDelegationAndBoundedPreissue(t *testing.T) {
	s, d, clock, _ := issuerFixture(t)
	ctx := context.Background()
	future := d
	future.ID = shareID(900)
	future.SourceID = shareID(901)
	future.RuntimeSourceID = shareID(902)
	future.ShareSourceID = shareID(903)
	future.BillingSnapshotID = shareID(904)
	future.ValidFrom = d.ValidFrom.Add(10 * time.Minute)
	if err := s.InstallHubDelegation(ctx, future, clock); err == nil {
		t.Fatal("future funding silently enabled without preissue")
	}
	future.PreissueSeconds = 86400
	if err := s.InstallHubDelegation(ctx, future, clock); err != nil {
		t.Fatal(err)
	}
	r := issuerRequest(future, 905)
	issued, err := s.IssueHubAuthorization(ctx, r, clock)
	if err != nil {
		t.Fatal(err)
	}
	var raw map[string]json.RawMessage
	if json.Unmarshal(issued.Payload, &raw) != nil {
		t.Fatal("invalid payload")
	}
	var records []struct {
		NotBefore time.Time `json:"not_before"`
		ExpiresAt time.Time `json:"expires_at"`
		Snapshot  string    `json:"billing_snapshot_id"`
	}
	if err = json.Unmarshal(raw["authorizations"], &records); err != nil {
		t.Fatal(err)
	}
	for _, a := range records {
		if !a.NotBefore.Equal(future.ValidFrom) || !a.ExpiresAt.After(future.ValidFrom) {
			t.Fatalf("future right unbounded %+v", a)
		}
	}
	again, err := s.IssueHubAuthorization(ctx, r, clock)
	if err != nil || !bytes.Equal(again.Payload, issued.Payload) {
		t.Fatal("preissue replay changed", err)
	}
	balance, err := s.AuditHubDelegation(ctx, future.ID)
	if err != nil || balance.Issued != 256 || balance.Remaining != 768 {
		t.Fatalf("future debit %+v %v", balance, err)
	}
	missing := r
	missing.ID = shareID(906)
	missing.DelegationID = shareID(907)
	if _, err = s.IssueHubAuthorization(ctx, missing, clock); err == nil {
		t.Fatal("request created unfunded future source")
	}
	tooSoon := future
	tooSoon.ID = shareID(908)
	tooSoon.ValidFrom = d.ValidFrom.Add(25 * time.Hour)
	tooSoon.ExpiresAt = tooSoon.ValidFrom.Add(time.Hour)
	tooSoon.HardDeadline = tooSoon.ExpiresAt
	tooSoon.AutonomyDeadline = tooSoon.HardDeadline
	if err = s.InstallHubDelegation(ctx, tooSoon, clock); err == nil {
		t.Fatal("out-of-window future provision accepted")
	}
}
