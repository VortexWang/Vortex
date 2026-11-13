package agent

import (
	"context"
	"testing"
	"time"

	accounting "vortex/engine/accounting"
)

func TestFundingBlockRefreshesClockAtDurableAdmission(t *testing.T) {
	for _, path := range []string{"first", "existing"} {
		t.Run(path, func(t *testing.T) {
			n, session := fundingSession(t)
			s := session.(*fundingBlockSession)
			if path == "existing" {
				batch, err := session.Reserve(context.Background(), accounting.LeftToRight, 32)
				if err != nil {
					t.Fatal(err)
				}
				if err := batch.Commit(context.Background(), 32); err != nil {
					t.Fatal(err)
				}
			}
			before := fundingBalance(t, n)
			valid, _, _ := s.inner.gate.config.Clock()
			calls := 0
			s.inner.gate.config.Clock = func() (time.Time, time.Time, bool) {
				calls++
				if calls == 1 {
					return valid, valid, true
				}
				expired := valid.Add(time.Hour)
				return expired, expired, true
			}
			batch, err := session.Reserve(context.Background(), accounting.LeftToRight, 32)
			if batch != nil {
				_ = batch.Return(context.Background())
			}
			if err == nil || calls < 2 {
				t.Fatalf("admission did not refresh expired authority: calls=%d err=%v", calls, err)
			}
			after := fundingBalance(t, n)
			// An idle expired block can be returned by the original allocator.
			// It cannot create consumption, unknown debt or additional credit.
			if before.Initial != after.Initial || before.Committed != after.Committed || before.Unknown != after.Unknown || before.Remaining+before.Reserved != after.Remaining+after.Reserved {
				t.Fatalf("expired admission changed conservation: before=%+v after=%+v", before, after)
			}
		})
	}
}
