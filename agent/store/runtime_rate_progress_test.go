package store

import (
	"context"
	"errors"
	"math/rand"
	"testing"
	"time"
)

// At 8 KiB/s, 100 bytes every 20ms is below the allowed rate. A 100ms
// conservative time interval must not repeatedly erase elapsed-time credit.
func TestRuntimeRateProgressWithOverlappingTrustedIntervals(t *testing.T) {
	s, scope, _ := runtimeStore(t)
	ctx := context.Background()
	a, err := s.OpenRuntimeSession(ctx, scope, "session", "flow")
	if err != nil {
		t.Fatal(err)
	}
	use := a.Use
	for i := 0; i < 800; i++ {
		use.Earliest = scope.Earliest.Add(time.Duration(i) * 20 * time.Millisecond)
		use.Latest = use.Earliest.Add(100 * time.Millisecond)
		tx, err := s.beginRuntimeTx(ctx, nil)
		if err != nil {
			t.Fatal(err)
		}
		err = consumeRuntimeRate(ctx, tx, use, 100)
		if err != nil {
			_ = tx.Rollback()
			t.Fatalf("below-rate sustained traffic denied at write %d: %v", i, err)
		}
		if err := tx.Commit(); err != nil {
			t.Fatal(err)
		}
	}
}

func TestRuntimeRateUncertaintyNeverExceedsSlidingEnvelope(t *testing.T) {
	s, scope, _ := runtimeStore(t)
	ctx := context.Background()
	a, err := s.OpenRuntimeSession(ctx, scope, "session", "flow")
	if err != nil {
		t.Fatal(err)
	}
	rng := rand.New(rand.NewSource(41))
	type admission struct {
		at    time.Time
		bytes uint64
	}
	var accepted []admission
	actual := scope.Earliest.Add(time.Second)
	for i := 0; i < 500; i++ {
		actual = actual.Add(time.Duration(rng.Intn(20)) * time.Millisecond)
		use := a.Use
		use.Earliest = actual.Add(-time.Duration(rng.Intn(200)) * time.Millisecond)
		use.Latest = actual.Add(time.Duration(rng.Intn(200)) * time.Millisecond)
		n := uint64(1 + rng.Intn(8192))
		tx, err := s.beginRuntimeTx(ctx, nil)
		if err != nil {
			t.Fatal(err)
		}
		err = consumeRuntimeRate(ctx, tx, use, n)
		if err != nil {
			_ = tx.Rollback()
			var limited *RateLimited
			if !errors.As(err, &limited) {
				t.Fatal(err)
			}
			continue
		}
		if err := tx.Commit(); err != nil {
			t.Fatal(err)
		}
		accepted = append(accepted, admission{actual, n})
		var sum uint64
		for j := len(accepted) - 1; j >= 0; j-- {
			sum += accepted[j].bytes
			limit := uint64(65536) + uint64(actual.Sub(accepted[j].at))*65536/8000000000
			if sum > limit {
				t.Fatalf("rate envelope exceeded: bytes=%d limit=%d", sum, limit)
			}
		}
	}
	if len(accepted) < 10 {
		t.Fatal("test admitted too little traffic to exercise refill")
	}
}
