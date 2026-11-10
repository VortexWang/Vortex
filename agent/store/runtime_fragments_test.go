package store

import (
	"context"
	"testing"
)

func TestRuntimeFragmentsCommitAllOrLeaveNoPartialDebit(t *testing.T) {
	s, scope, path := runtimeStore(t)
	ctx := context.Background()
	a, err := s.OpenRuntimeSession(ctx, scope, "session", "flow")
	if err != nil {
		t.Fatal(err)
	}
	// Retain a real funded upper bound, leaving only 40 bytes in its grant.
	if _, err := s.ReserveRuntimeFundingBlock(ctx, runtimeReservation("held", (1<<20)-40), a.Use); err != nil {
		t.Fatal(err)
	}
	r, _, err := readAuthorizationTx(ctx, s.db, "node", "grant")
	if err != nil {
		t.Fatal(err)
	}
	r.AuthorizationID, r.AmountBytes = "topup", 64
	if err := s.PersistAuthorizationUpdate(ctx, AuthorizationUpdate{IssuerID: "hub-one", NodeID: "node", BootID: "boot", UpdateID: "topup", Payload: []byte("topup"), SignerEpoch: 1, PathGeneration: 1, FencingGenerationFloor: 1}, []AuthorizationRecord{r}, nil); err != nil {
		t.Fatal(err)
	}
	unchanged := func() {
		t.Helper()
		b, err := s.AuditAuthorizationSource(ctx, "node", "funding")
		if err != nil || b.Reserved != (1<<20)-40 || b.Remaining != 104 || b.Committed != 0 || b.Unknown != 0 {
			t.Fatalf("partial money debit: %+v %v", b, err)
		}
		var count int
		if err := s.db.QueryRow(`SELECT COUNT(*) FROM runtime_rate_buckets`).Scan(&count); err != nil || count != 0 {
			t.Fatalf("partial rate debit: %d %v", count, err)
		}
		if err := s.db.QueryRow(`SELECT COUNT(*) FROM usage_reservations`).Scan(&count); err != nil || count != 1 {
			t.Fatalf("partial child: %d %v", count, err)
		}
	}
	request := runtimeReservation("unused", 105)
	if children, err := s.ReserveRuntimeUsageFragments(ctx, request, a.Use, []string{"one", "two", "three"}); err == nil || children != nil {
		t.Fatal("incomplete write admitted", children, err)
	}
	unchanged()
	request.Reserved = 100
	if _, err := s.db.Exec(`CREATE TRIGGER fail_fragment BEFORE INSERT ON authorization_usage_holds WHEN NEW.batch_id='two' BEGIN SELECT RAISE(ABORT,'second fragment'); END`); err != nil {
		t.Fatal(err)
	}
	if children, err := s.ReserveRuntimeUsageFragments(ctx, request, a.Use, []string{"one", "two"}); err == nil || children != nil {
		t.Fatal("failed commit exposed fragments", children, err)
	}
	unchanged()
	if _, err := s.db.Exec(`DROP TRIGGER fail_fragment`); err != nil {
		t.Fatal(err)
	}
	children, err := s.ReserveRuntimeUsageFragments(ctx, request, a.Use, []string{"one", "two"})
	if err != nil || len(children) != 2 || children[0].AuthorizationID != "topup" || children[0].Reserved != 64 || children[1].AuthorizationID != "grant" || children[1].Reserved != 36 {
		t.Fatal("independent grant identities lost", children, err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = Open(path, "agent")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	// A lost successful result recovers both children as liabilities, without
	// requiring an Engine callback or fabricating confirmed consumption.
	pending, err := s.UnresolvedUsageReservations(ctx, "node", "new-boot")
	if err != nil || len(pending) != 3 {
		t.Fatal("committed upper bounds lost", pending, err)
	}
	b, err := s.AuditAuthorizationSource(ctx, "node", "funding")
	if err != nil || b.Reserved != (1<<20)+60 || b.Remaining != 4 || b.Committed != 0 || b.Unknown != 0 {
		t.Fatalf("reopen conservation: %+v %v", b, err)
	}
}
