package accounting

import (
	"context"
	"errors"
	"testing"
)

func TestBatchCommitResolvesOnceWithAcceptedBytes(t *testing.T) {
	var calls []Outcome
	batch, err := NewBatch(100, func(_ context.Context, outcome Outcome) error {
		calls = append(calls, outcome)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := batch.Commit(context.Background(), 37); err != nil {
		t.Fatal(err)
	}
	if err := batch.Return(context.Background()); !errors.Is(err, ErrAlreadyResolved) {
		t.Fatalf("second resolution = %v, want ErrAlreadyResolved", err)
	}
	if len(calls) != 1 || calls[0] != (Outcome{Resolution: Committed, Reserved: 100, Used: 37}) {
		t.Fatalf("resolver outcomes = %#v", calls)
	}
	if state, used := batch.State(); state != Committed || used != 37 {
		t.Fatalf("batch state = %q/%d", state, used)
	}
}

func TestBatchReturnAndUnknownKeepReservedAmount(t *testing.T) {
	for _, resolution := range []Resolution{Returned, Unknown} {
		t.Run(string(resolution), func(t *testing.T) {
			var got Outcome
			batch, err := NewBatch(64, func(_ context.Context, outcome Outcome) error {
				got = outcome
				return nil
			})
			if err != nil {
				t.Fatal(err)
			}
			if resolution == Returned {
				err = batch.Return(context.Background())
			} else {
				err = batch.MarkUnknown(context.Background())
			}
			if err != nil {
				t.Fatal(err)
			}
			if got.Resolution != resolution || got.Reserved != 64 || got.Used != 0 {
				t.Fatalf("resolver outcome = %#v", got)
			}
		})
	}
}

func TestBatchResolverFailureBecomesUnknownAndCannotRetry(t *testing.T) {
	wantErr := errors.New("ack lost")
	calls := 0
	batch, err := NewBatch(25, func(context.Context, Outcome) error {
		calls++
		return wantErr
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := batch.Commit(context.Background(), 20); !errors.Is(err, wantErr) {
		t.Fatalf("commit error = %v", err)
	}
	if state, used := batch.State(); state != Unknown || used != 25 {
		t.Fatalf("failed resolution state = %q/%d, want unknown upper bound", state, used)
	}
	if err := batch.Return(context.Background()); !errors.Is(err, ErrAlreadyResolved) {
		t.Fatalf("retry error = %v, want ErrAlreadyResolved", err)
	}
	if calls != 1 {
		t.Fatalf("resolver called %d times, want once", calls)
	}
}

func TestBatchRejectsInvalidAmounts(t *testing.T) {
	batch, err := NewBatch(10, func(context.Context, Outcome) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	if err := batch.Commit(context.Background(), 11); !errors.Is(err, ErrInvalidUsage) {
		t.Fatalf("oversized commit = %v", err)
	}
	if err := batch.Commit(context.Background(), -1); !errors.Is(err, ErrInvalidUsage) {
		t.Fatalf("negative commit = %v", err)
	}
	if state, _ := batch.State(); state != "" {
		t.Fatalf("invalid commit resolved the batch as %q", state)
	}
}
