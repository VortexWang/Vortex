package store

import (
	"context"
	"testing"
	"time"
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

func TestFundingWriteRefreshesTrustedTimeInsideTransaction(t *testing.T) {
	for _, mode := range []string{"active", "expired", "untrusted", "revoked"} {
		t.Run(mode, func(t *testing.T) {
			s, scope, _ := runtimeStore(t)
			ctx := context.Background()
			session, err := s.OpenRuntimeSession(ctx, scope, "session", "flow")
			if err != nil {
				t.Fatal(err)
			}
			stale := session.Use
			stale.Earliest = scope.Earliest.Add(-2 * time.Second)
			stale.Latest = stale.Earliest
			fresh := scope.Earliest
			if mode == "expired" {
				fresh = fresh.Add(10 * time.Minute)
			}
			if mode == "revoked" {
				if err := s.PersistAuthorizationUpdate(ctx, AuthorizationUpdate{NodeID: "node", BootID: "boot", UpdateID: "revoke", Payload: []byte("revoke"), IssuerID: "hub-one", SignerEpoch: 1, PathGeneration: 1, FencingGenerationFloor: 1}, nil, []string{"grant"}); err != nil {
					t.Fatal(err)
				}
			}
			var called bool
			clock := func() (time.Time, time.Time, bool) {
				called = true
				if s.db.Stats().InUse != 1 {
					t.Error("clock sampled before transaction owned connection")
				}
				return fresh, fresh, mode != "untrusted"
			}
			result, err := s.ReserveAndAdmitRuntimeFundingWrite(ctx, runtimeReservation("fresh", 128), stale, 100, nil, nil, []string{"fragment"}, clock)
			if !called {
				t.Fatal("clock not sampled")
			}
			if mode == "active" {
				if err != nil || result.Block.Reserved != 128 {
					t.Fatalf("fresh conservative authority not admitted: %+v %v", result, err)
				}
			} else {
				if err == nil || result.Block.BatchID != "" || len(result.Children) != 0 {
					t.Fatalf("invalid refreshed authority admitted: %+v %v", result, err)
				}
				balance, e := s.AuditAuthorizationSource(ctx, "node", "funding")
				if e != nil || balance.Reserved != 0 || balance.Remaining != balance.Initial {
					t.Fatalf("failed refresh mutated funding: %+v %v", balance, e)
				}
			}
		})
	}
}
