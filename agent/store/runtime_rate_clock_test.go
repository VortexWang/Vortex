package store

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestMonotonicRatePreservesDebtAndAbsoluteAuthority(t *testing.T) {
	s, scope, path := runtimeStore(t)
	ctx := context.Background()
	a, err := s.OpenRuntimeSession(ctx, scope, "session", "flow")
	if err != nil {
		t.Fatal(err)
	}
	use := a.Use
	use.Latest = use.Earliest.Add(2 * time.Second)
	if err := s.EnableMonotonicRuntimeRate("boot"); err != nil {
		t.Fatal(err)
	}
	clock := s.rateClock.Load()
	now := clock.start
	clock.now = func() time.Time { return now }
	consume := func(db *Store, n uint64) error {
		tx, err := db.beginRuntimeTx(ctx, nil)
		if err != nil {
			return err
		}
		defer tx.Rollback()
		if _, err := checkAuthorizationTx(ctx, tx, use); err != nil {
			return err
		}
		if err := consumeRuntimeRate(ctx, tx, use, n); err != nil {
			return err
		}
		return tx.Commit()
	}
	if err := consume(s, 65536); err != nil {
		t.Fatal(err)
	}
	if err := consume(s, 1); err == nil {
		t.Fatal("same-instant burst exceeded")
	}
	now = now.Add(time.Second)
	if err := consume(s, 8192); err == nil {
		t.Fatal("drift safety margin absent")
	}
	if err := consume(s, 8183); err != nil {
		t.Fatal(err)
	}
	use.TrustedTime = false
	if err := consume(s, 1); !errors.Is(err, ErrAuthorityTimeUncertain) {
		t.Fatalf("untrusted time accepted: %v", err)
	}
	use.TrustedTime = true
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(path, "agent")
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	if err := consume(reopened, 1); !errors.Is(err, ErrAuthorityTimeUncertain) {
		t.Fatalf("clock domain downgrade: %v", err)
	}
	if err := reopened.EnableMonotonicRuntimeRate("boot"); err != nil {
		t.Fatal(err)
	}
	newClock := reopened.rateClock.Load()
	newClock.now = func() time.Time { return newClock.start }
	if err := consume(reopened, 1); err == nil {
		t.Fatal("reopen refilled empty bucket")
	}
	if err := reopened.EnableMonotonicRuntimeRate("new-boot"); !errors.Is(err, ErrAuthorityHandoffRequired) {
		t.Fatalf("clock boot changed: %v", err)
	}
}
