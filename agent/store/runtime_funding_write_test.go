package store

import (
	"context"
	"testing"
)

func TestRuntimeFundingWriteReclaimsBothTailsAtomically(t *testing.T) {
	s, scope, _ := runtimeStore(t)
	ctx := context.Background()
	a, err := s.OpenRuntimeSession(ctx, scope, "session", "flow")
	if err != nil {
		t.Fatal(err)
	}
	// Real completed consumption leaves a small tail in each direction.
	left := runtimeReservation("left", (1<<20)-40)
	if _, err = s.ReserveRuntimeFundingBlock(ctx, left, a.Use); err != nil {
		t.Fatal(err)
	}
	if err = s.CheckpointRuntimeFundingBlock(ctx, left, "completed", (1<<20)-80, []byte("known")); err != nil {
		t.Fatal(err)
	}
	left.Reserved = 40
	r := runtimeReservation("right", 40)
	r.Direction = "ingress"
	right, err := s.ReserveRuntimeFundingBlockUpTo(ctx, r, a.Use, 40)
	if err != nil {
		t.Fatal(err)
	}
	grant, _, err := readAuthorizationTx(ctx, s.db, "node", "grant")
	if err != nil {
		t.Fatal(err)
	}
	grant.AuthorizationID = "topup"
	grant.AmountBytes = 64
	if err = s.PersistAuthorizationUpdate(ctx, AuthorizationUpdate{IssuerID: "hub-one", NodeID: "node", BootID: "boot", UpdateID: "topup", Payload: []byte("topup"), SignerEpoch: 1, PathGeneration: 1, FencingGenerationFloor: 1}, []AuthorizationRecord{grant}, nil); err != nil {
		t.Fatal(err)
	}
	unchanged := func() {
		t.Helper()
		b, e := s.AuditAuthorizationSource(ctx, "node", "funding")
		if e != nil || b.Reserved != 80 || b.Remaining != 64 {
			t.Fatal("partial tail return/debit", b, e)
		}
		var buckets int
		if e := s.db.QueryRow(`SELECT COUNT(*) FROM runtime_rate_buckets`).Scan(&buckets); e != nil || buckets != 0 {
			t.Fatal("partial rate debit", buckets, e)
		}
	}
	next := runtimeReservation("next", 200)
	if result, e := s.ReserveAndAdmitRuntimeFundingWrite(ctx, next, a.Use, 145, []UsageReservation{left}, []UsageReservation{right}, []string{"a", "b", "c"}); e == nil || len(result.Children) > 0 {
		t.Fatal("insufficient collection admitted", result, e)
	}
	unchanged()
	// The first single-grant attempt fails at 100; fragments must preserve the
	// grant identity of each debit and roll back both returns on storage error.
	if _, err = s.db.Exec(`CREATE TRIGGER fail_atomic_funding BEFORE INSERT ON authorization_usage_holds WHEN NEW.batch_id='b' BEGIN SELECT RAISE(ABORT,'second fragment'); END`); err != nil {
		t.Fatal(err)
	}
	if result, e := s.ReserveAndAdmitRuntimeFundingWrite(ctx, next, a.Use, 100, []UsageReservation{left}, []UsageReservation{right}, []string{"a", "b", "c"}); e == nil || len(result.Children) > 0 {
		t.Fatal("storage failure exposed children", result, e)
	}
	unchanged()
	if _, err = s.db.Exec(`DROP TRIGGER fail_atomic_funding`); err != nil {
		t.Fatal(err)
	}
	result, err := s.ReserveAndAdmitRuntimeFundingWrite(ctx, next, a.Use, 100, []UsageReservation{left}, []UsageReservation{right}, []string{"a", "b", "c"})
	if err != nil || len(result.Children) != 2 || len(result.Reclaimed) != 2 || result.Block.BatchID != "" {
		t.Fatal("atomic fallback", result, err)
	}
	if result.Children[0].AuthorizationID != "topup" || result.Children[0].Reserved != 64 || result.Children[1].AuthorizationID != "grant" || result.Children[1].Reserved != 36 {
		t.Fatal("child identities", result)
	}
	b, err := s.AuditAuthorizationSource(ctx, "node", "funding")
	if err != nil || b.Reserved != 100 || b.Remaining != 44 || b.Unknown != 0 || b.Committed != (1<<20)-80 {
		t.Fatal("conservation", b, err)
	}
	if err = s.AdmitRuntimeFundingBlockWrite(ctx, left, a.Use, 1); err == nil {
		t.Fatal("returned tail retained capability")
	}
}
