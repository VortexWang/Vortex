package store

import (
	"context"
	"errors"
	"testing"
	"time"
)

func topupRecord(t *testing.T, s *Store, use AuthorizationUse) AuthorizationRecord {
	t.Helper()
	r, err := s.CheckStoredAuthorization(context.Background(), use)
	if err != nil {
		t.Fatal(err)
	}
	r.AuthorizationID = "topup"
	r.AmountBytes = 1000
	r.IssuedAt = use.Latest.Format(time.RFC3339Nano)
	return r
}

func TestSourceTopupKeepsUnknownAndRefundsInOriginalGrant(t *testing.T) {
	s, use := fundedStore(t)
	ctx := context.Background()
	r := UsageReservation{NodeID: "node", BootID: "boot", AuthorizationID: "grant", BatchID: "old-unknown", SnapshotID: "snapshot", FlowKey: "old", RuleID: "rule", Direction: "egress", Reserved: 1000}
	if _, err := s.ReserveAuthorizedUsage(ctx, r, use); err != nil {
		t.Fatal(err)
	}
	if err := s.SettleUsageReservation(ctx, r, "unknown", 1000, []byte("unknown"), 1000); err != nil {
		t.Fatal(err)
	}
	newGrant := topupRecord(t, s, use)
	u := AuthorizationUpdate{NodeID: "node", BootID: "boot", UpdateID: "topup-update", Payload: []byte("signed-topup"), SignerEpoch: 1, PathGeneration: 1, FencingGenerationFloor: 1}
	if err := s.PersistAuthorizationUpdate(ctx, u, []AuthorizationRecord{newGrant}, nil); err != nil {
		t.Fatal(err)
	}
	if err := s.PersistAuthorizationUpdate(ctx, u, []AuthorizationRecord{newGrant}, nil); err != nil {
		t.Fatal(err)
	}
	use.AuthorizationID = "topup"
	r.AuthorizationID = "topup"
	r.BatchID = "new-usage"
	r.Reserved = 300
	if _, err := s.ReserveAuthorizedUsage(ctx, r, use); err != nil {
		t.Fatal(err)
	}
	if err := s.SettleUsageReservation(ctx, r, "committed", 100, []byte("usage"), 0); err != nil {
		t.Fatal(err)
	}
	for id, want := range map[string]uint64{"grant": 3096, "topup": 900} {
		got, err := s.RemainingAuthorizationBytes(ctx, "node", id)
		if err != nil || got != want {
			t.Fatalf("%s=%d want %d: %v", id, got, want, err)
		}
	}
	b, err := s.AuditAuthorizationSource(ctx, "node", "source")
	if err != nil || b.Initial != 5096 || b.Remaining != 3996 || b.Unknown != 1000 || b.Committed != 100 {
		t.Fatalf("topup/source accounting: %+v %v", b, err)
	}
}

func TestTopupCannotChangeDelegationEpochBootScopeOrBoundary(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*AuthorizationRecord)
	}{
		{"parent", func(r *AuthorizationRecord) { r.ParentAuthorizationID = "new-parent" }},
		{"signer", func(r *AuthorizationRecord) { r.SignerEpoch = 2 }},
		{"boot", func(r *AuthorizationRecord) { r.BootID = "new-boot" }},
		{"rule", func(r *AuthorizationRecord) { r.RuleID = "new-rule" }},
		{"revision", func(r *AuthorizationRecord) { r.RuleRevision = 2 }},
		{"path", func(r *AuthorizationRecord) { r.PathGeneration = 2 }},
		{"snapshot", func(r *AuthorizationRecord) { r.BillingSnapshotID = "new-snapshot" }},
		{"deadline", func(r *AuthorizationRecord) {
			end, _ := time.Parse(time.RFC3339Nano, r.HardDeadline)
			r.HardDeadline = end.Add(time.Second).Format(time.RFC3339Nano)
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, use := fundedStore(t)
			ctx := context.Background()
			r := topupRecord(t, s, use)
			tc.change(&r)
			u := AuthorizationUpdate{NodeID: "node", BootID: r.BootID, UpdateID: "bad-topup", Payload: []byte("changed-scope"), SignerEpoch: r.SignerEpoch, PathGeneration: r.PathGeneration, FencingGenerationFloor: 1}
			if err := s.PersistAuthorizationUpdate(ctx, u, []AuthorizationRecord{r}, nil); !errors.Is(err, ErrAuthorityHandoffRequired) {
				t.Fatalf("changed scope accepted: %v", err)
			}
			b, err := s.AuditAuthorizationSource(ctx, "node", "source")
			if err != nil || b.Initial != 4096 || b.Remaining != 4096 {
				t.Fatalf("failed topup changed ledger: %+v %v", b, err)
			}
		})
	}
}
