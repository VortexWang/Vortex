package store

import (
	"context"
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
