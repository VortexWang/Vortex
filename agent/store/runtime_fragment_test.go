package store

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestRuntimeFragmentAdmissionPreservesIdentityAndUnknown(t *testing.T) {
	s, scope, _ := runtimeStore(t)
	ctx := context.Background()
	session, err := s.OpenRuntimeSession(ctx, scope, "session", "flow")
	if err != nil {
		t.Fatal(err)
	}
	// Freeze most of the first grant. Only the 1096-byte tail is spendable.
	frozen := runtimeReservation("frozen", (1<<20)-1096)
	if _, err := s.ReserveRuntimeFundingBlock(ctx, frozen, session.Use); err != nil {
		t.Fatal(err)
	}
	if err := s.SettleUsageReservation(ctx, frozen, "unknown", frozen.Reserved, []byte("unknown"), frozen.Reserved); err != nil {
		t.Fatal(err)
	}
	records := renewedRuntimeRecords(t, s, session.Use)
	for i := range records {
		if records[i].Kind == "credit_grant" {
			records[i].AmountBytes = 1096
		}
	}
	if err := installRuntimeRenewal(s, records); err != nil {
		t.Fatal(err)
	}
	use := session.Use
	use.Earliest, use.Latest = use.Earliest.Add(2*time.Second), use.Latest.Add(2*time.Second)
	demand := DemandScope{HubID: "hub-one", DelegationID: "parent", NodeID: "node", BootID: "boot", SourceID: "funding", SnapshotID: "snapshot", RuleID: "rule", SignerEpoch: 1, RuleRevision: 1, PathGeneration: 1}
	ledger, err := s.ReadDemandLedger(ctx, demand, IssuerTime{Earliest: use.Earliest, Latest: use.Latest, Trusted: true}, time.Now())
	if err != nil || ledger.Available != 2192 || ledger.LargestGrant != 1096 {
		t.Fatalf("fragmented ledger: %+v %v", ledger, err)
	}
	if _, err := s.ReserveRuntimeUsage(ctx, runtimeReservation("whole", 1500), use); !errors.Is(err, ErrAuthorizationDenied) {
		t.Fatalf("whole write: %v", err)
	}
	first, err := s.ReserveRuntimeUsageUpTo(ctx, runtimeReservation("first", 1500), use)
	if err != nil || first.Reserved != 1096 || first.AuthorizationID != "renewed-grant" {
		t.Fatalf("first fragment %+v %v", first, err)
	}
	if _, err := s.ReserveRuntimeUsageUpTo(ctx, runtimeReservation("first", 1500), use); !errors.Is(err, ErrUsageBatchConflict) {
		t.Fatalf("one-shot replay %v", err)
	}
	second, err := s.ReserveRuntimeUsageUpTo(ctx, runtimeReservation("second", 404), use)
	if err != nil || second.Reserved != 404 || second.AuthorizationID != "grant" {
		t.Fatalf("second fragment %+v %v", second, err)
	}
	// A short write is partitioned in reservation order; unused tail is returned.
	if err := s.SettleUsageReservation(ctx, first, "committed", 1000, []byte("known"), 0); err != nil {
		t.Fatal(err)
	}
	if err := s.SettleUsageReservation(ctx, second, "returned", 0, nil, 0); err != nil {
		t.Fatal(err)
	}
	b, err := s.AuditAuthorizationSource(ctx, "node", "funding")
	if err != nil || b.Committed != 1000 || b.Unknown != frozen.Reserved || b.Remaining != 1192 || b.Reserved != 0 {
		t.Fatalf("fragment conservation %+v %v", b, err)
	}
}
