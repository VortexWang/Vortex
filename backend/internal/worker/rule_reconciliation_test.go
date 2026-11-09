package worker

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestRuleReconciliationDrainsReadyWorkAndCancels(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	n := 0
	err := runRuleReconciliation(ctx, func(context.Context) (bool, error) {
		n++
		if n == 2000 {
			cancel()
		}
		return true, nil
	})
	if n != 2000 || !errors.Is(err, context.Canceled) {
		t.Fatal(n, err)
	}
}

func TestRuleReconciliationIdleAndFailure(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	n := 0
	err := runRuleReconciliation(ctx, func(context.Context) (bool, error) {
		n++
		cancel()
		return false, nil
	})
	if n != 1 || !errors.Is(err, context.Canceled) {
		t.Fatal(n, err)
	}
	want := errors.New("durability failure")
	if err = runRuleReconciliation(context.Background(), func(context.Context) (bool, error) { return true, want }); !errors.Is(err, want) {
		t.Fatal(err)
	}
}
