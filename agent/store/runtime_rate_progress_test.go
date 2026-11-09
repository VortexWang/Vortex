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
	for _, width := range []time.Duration{200 * time.Millisecond, 12 * time.Second} {
		t.Run(width.String(), func(t *testing.T) { runtimeRateUncertaintyEnvelope(t, width) })
	}
}

func runtimeRateUncertaintyEnvelope(t *testing.T, width time.Duration) {
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
	actual := scope.Earliest.Add(30 * time.Second)
	for i := 0; i < 500; i++ {
		actual = actual.Add(time.Duration(rng.Intn(20)) * time.Millisecond)
		use := a.Use
		use.Earliest = actual.Add(-time.Duration(rng.Int63n(int64(width))))
		use.Latest = actual.Add(time.Duration(rng.Int63n(int64(width))))
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

// A wide absolute clock interval earns no refill, but cannot erase the
// existing initial burst. This is the regional path; no monotonic opt-in.
func TestRuntimeRateWideIntervalRetainsFundedBurstWithoutRefill(t *testing.T) {
	s, scope, _ := runtimeStore(t)
	ctx := context.Background()
	a, err := s.OpenRuntimeSession(ctx, scope, "wide-session", "wide-flow")
	if err != nil {
		t.Fatal(err)
	}
	use := a.Use
	use.Latest = use.Earliest.Add(10 * time.Second) // burst window is 8s
	admit := func(n uint64) error {
		tx, err := s.beginRuntimeTx(ctx, nil)
		if err != nil {
			t.Fatal(err)
		}
		err = consumeRuntimeRate(ctx, tx, use, n)
		if err != nil {
			_ = tx.Rollback()
			return err
		}
		return tx.Commit()
	}
	if err = admit(32768); err != nil {
		t.Fatalf("initial burst rejected: %v", err)
	}
	use.Earliest = use.Earliest.Add(time.Second)
	use.Latest = use.Latest.Add(time.Second)
	if err = admit(32768); err != nil {
		t.Fatalf("remaining burst rejected: %v", err)
	}
	var limited *RateLimited
	if err = admit(1); !errors.As(err, &limited) {
		t.Fatalf("overlapping bounds minted refill: %v", err)
	}
	use.Earliest = use.Latest.Add(time.Second)
	use.Latest = use.Earliest.Add(10 * time.Second)
	if err = admit(8192); err != nil {
		t.Fatalf("proven elapsed second earned no refill: %v", err)
	}
	if err = admit(1); !errors.As(err, &limited) {
		t.Fatalf("exceeded proven refill: %v", err)
	}
}
