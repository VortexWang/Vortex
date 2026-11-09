package store

import (
	"context"
	"errors"
	"testing"
)

func TestRuntimeFundingRangeAtomicSelectionReplayAndRollback(t *testing.T) {
	s, scope, _ := runtimeStore(t)
	ctx := context.Background()
	a, err := s.OpenRuntimeSession(ctx, scope, "session", "flow")
	if err != nil {
		t.Fatal(err)
	}
	request := runtimeReservation("first", 2<<20)
	funded, err := s.ReserveRuntimeFundingBlockUpTo(ctx, request, a.Use, 100)
	if err != nil || funded.Reserved != 1<<20 {
		t.Fatalf("range: %+v %v", funded, err)
	}
	if retry, err := s.ReserveRuntimeFundingBlockUpTo(ctx, request, a.Use, 100); err != nil || retry != funded {
		t.Fatalf("retry: %+v %v", retry, err)
	}
	if _, err := s.ReserveRuntimeFundingBlockUpTo(ctx, request, a.Use, 101); !errors.Is(err, ErrUsageBatchConflict) {
		t.Fatalf("minimum mutation: %v", err)
	}
	if _, err := s.ReserveRuntimeFundingBlock(ctx, funded, a.Use); !errors.Is(err, ErrUsageBatchConflict) {
		t.Fatalf("strict replay: %v", err)
	}
	next := runtimeReservation("next", 2<<20)
	next.Direction = "ingress"
	// Releasing the old hold must roll back if the new minimum is not funded.
	if _, err := s.ReserveRuntimeFundingBlockReplacingIdle(ctx, next, a.Use, (1<<20)+1, []UsageReservation{funded}); err == nil {
		t.Fatal("unfunded replacement")
	}
	balance, err := s.AuditAuthorizationSource(ctx, "node", "funding")
	if err != nil || balance.Reserved != 1<<20 || balance.Remaining != 0 {
		t.Fatalf("rollback: %+v %v", balance, err)
	}
	if _, err := s.db.Exec(`CREATE TRIGGER fail_range BEFORE INSERT ON runtime_funding_requests BEGIN SELECT RAISE(ABORT,'fail range'); END`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ReserveRuntimeFundingBlockReplacingIdle(ctx, next, a.Use, 100, []UsageReservation{funded}); err == nil {
		t.Fatal("partial request committed")
	}
	if _, err := s.db.Exec(`DROP TRIGGER fail_range`); err != nil {
		t.Fatal(err)
	}
	replacement, err := s.ReserveRuntimeFundingBlockReplacingIdle(ctx, next, a.Use, 100, []UsageReservation{funded})
	if err != nil || replacement.Reserved != 1<<20 {
		t.Fatalf("replacement: %+v %v", replacement, err)
	}
	if retry, err := s.ReserveRuntimeFundingBlockReplacingIdle(ctx, next, a.Use, 100, []UsageReservation{funded}); err != nil || retry != replacement {
		t.Fatalf("replacement retry: %+v %v", retry, err)
	}
	if _, err := s.ReserveRuntimeFundingBlockUpTo(ctx, next, a.Use, 100); !errors.Is(err, ErrUsageBatchConflict) {
		t.Fatalf("reclaim mutation: %v", err)
	}
	if err := s.SettleUsageReservation(ctx, replacement, "returned", 0, nil, 0); err != nil {
		t.Fatal(err)
	}
	balance, err = s.AuditAuthorizationSource(ctx, "node", "funding")
	if err != nil || balance.Remaining != 1<<20 || balance.Reserved != 0 {
		t.Fatalf("final: %+v %v", balance, err)
	}
}

func TestRuntimeFundingFirstAdmissionAtomicRate(t *testing.T) {
	s, scope, _ := runtimeStore(t)
	ctx := context.Background()
	a, err := s.OpenRuntimeSession(ctx, scope, "session", "flow")
	if err != nil {
		t.Fatal(err)
	}
	r := runtimeReservation("first-write", 256<<10)
	funded, err := s.ReserveAndAdmitRuntimeFundingBlock(ctx, r, a.Use, 65536, nil)
	if err != nil || funded.Reserved != 256<<10 {
		t.Fatalf("fund and admit: %+v %v", funded, err)
	}
	if _, err := s.ReserveAndAdmitRuntimeFundingBlock(ctx, r, a.Use, 65536, nil); !errors.Is(err, ErrUsageBatchConflict) {
		t.Fatalf("admission replay: %v", err)
	}
	if err := s.AdmitRuntimeFundingBlockWrite(ctx, funded, a.Use, 1); err == nil {
		t.Fatal("first write did not consume rate")
	}
	next := runtimeReservation("rate-denied", 256<<10)
	next.Direction = "ingress"
	if _, err := s.ReserveAndAdmitRuntimeFundingBlock(ctx, next, a.Use, 1, []UsageReservation{funded}); err == nil {
		t.Fatal("rate-denied allocation succeeded")
	}
	balance, err := s.AuditAuthorizationSource(ctx, "node", "funding")
	if err != nil || balance.Reserved != 256<<10 || balance.Remaining != (1<<20)-(256<<10) {
		t.Fatalf("rate denial did not roll back idle replacement: %+v %v", balance, err)
	}
	if err := s.SettleUsageReservation(ctx, funded, "returned", 0, nil, 0); err != nil {
		t.Fatal(err)
	}
}
