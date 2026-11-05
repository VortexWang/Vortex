package store

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"
)

func runtimeStore(t *testing.T) (*Store, AuthorizationUse, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "agent.db")
	s, err := Open(path, "agent")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	now := time.Date(2026, 11, 5, 12, 0, 0, 0, time.UTC)
	base := AuthorizationRecord{AuthorizationID: "grant", ParentAuthorizationID: "parent", SourceID: "funding", Kind: "credit_grant", RuleID: "rule", RuleRevision: 1, PathGeneration: 1, SignerEpoch: 1, BootID: "boot", IssuedAt: now.Format(time.RFC3339Nano), NotBefore: now.Format(time.RFC3339Nano), ExpiresAt: now.Add(5 * time.Minute).Format(time.RFC3339Nano), HardDeadline: now.Add(time.Hour).Format(time.RFC3339Nano), BillingSnapshotID: "snapshot", AmountBytes: 1 << 20}
	runtime := base
	runtime.AuthorizationID = "runtime"
	runtime.Kind = "runtime_lease"
	runtime.AmountBytes = 0
	runtime.BillingSnapshotID = ""
	share := runtime
	share.AuthorizationID = "share"
	share.Kind = "share_lease"
	share.SourceID = "share-source"
	share.RateBPS = 65536
	share.Connections = 1
	if err := s.PersistAuthorizationUpdate(context.Background(), AuthorizationUpdate{NodeID: "node", BootID: "boot", UpdateID: "initial", Payload: []byte("initial"), SignerEpoch: 1, PathGeneration: 1, FencingGenerationFloor: 1}, []AuthorizationRecord{base, runtime, share}, nil); err != nil {
		t.Fatal(err)
	}
	return s, AuthorizationUse{NodeID: "node", BootID: "boot", RuleID: "rule", RuleRevision: 1, PathGeneration: 1, Earliest: now.Add(time.Second), Latest: now.Add(time.Second), TrustedTime: true}, path
}

func TestRuntimeShareSlotsPersistAcrossStoreReopen(t *testing.T) {
	s, scope, path := runtimeStore(t)
	ctx := context.Background()
	first, err := s.OpenRuntimeSession(ctx, scope, "first", "flow-first")
	if err != nil {
		t.Fatal(err)
	}
	if first.Use.RuntimeLeaseID != "runtime" || first.Use.ShareLeaseID != "share" || first.Use.BillingSnapshotID != "snapshot" {
		t.Fatalf("incomplete authority %+v", first)
	}
	secondStore, err := Open(path, "agent")
	if err != nil {
		t.Fatal(err)
	}
	defer secondStore.Close()
	if _, err := secondStore.OpenRuntimeSession(ctx, scope, "second", "flow-second"); !errors.Is(err, ErrAuthorizationDenied) {
		t.Fatalf("second instance bypassed share slot: %v", err)
	}
	if err := s.CloseRuntimeSession(ctx, "node", "wrong-boot", "first"); err != nil {
		t.Fatal(err)
	}
	if _, err := secondStore.OpenRuntimeSession(ctx, scope, "second", "flow-second"); err == nil {
		t.Fatal("wrong boot released source slot")
	}
	if err := s.CloseRuntimeSession(ctx, "node", "boot", "first"); err != nil {
		t.Fatal(err)
	}
	if _, err := secondStore.OpenRuntimeSession(ctx, scope, "second", "flow-second"); err != nil {
		t.Fatal(err)
	}
}

func TestRuntimeRateAndClosedSessionCannotAllocateBytes(t *testing.T) {
	s, scope, _ := runtimeStore(t)
	ctx := context.Background()
	authority, err := s.OpenRuntimeSession(ctx, scope, "session", "flow")
	if err != nil {
		t.Fatal(err)
	}
	use := authority.Use
	r := UsageReservation{NodeID: "node", BootID: "boot", BatchID: "first", AuthorizationID: "grant", SnapshotID: "snapshot", FlowKey: "flow", RuleID: "rule", Direction: "egress", Reserved: 65536}
	if _, err := s.ReserveAuthorizedUsage(ctx, r, use); err != nil {
		t.Fatal(err)
	}
	if err := s.SettleUsageReservation(ctx, r, "returned", 0, nil, 0); err != nil {
		t.Fatal(err)
	}
	r.BatchID = "rate-denied"
	r.Reserved = 1
	if _, err := s.ReserveAuthorizedUsage(ctx, r, use); err == nil {
		t.Fatal("separate grant refund bypassed rate share")
	} else {
		var limited *RateLimited
		if !errors.As(err, &limited) {
			t.Fatal(err)
		}
	}
	use.Earliest = use.Earliest.Add(time.Second)
	use.Latest = use.Latest.Add(time.Second)
	if _, err := s.ReserveAuthorizedUsage(ctx, r, use); err != nil {
		t.Fatal(err)
	}
	if err := s.CloseRuntimeSession(ctx, "node", "boot", "session"); err != nil {
		t.Fatal(err)
	}
	r.BatchID = "closed-session"
	if _, err := s.ReserveAuthorizedUsage(ctx, r, use); err == nil {
		t.Fatal("closed runtime session acquired byte credit")
	}
	b, err := s.AuditAuthorizationSource(ctx, "node", "funding")
	if err != nil || b.Reserved != 1 || b.Remaining != (1<<20)-1 {
		t.Fatalf("rate rejection changed funding: %+v %v", b, err)
	}
}
