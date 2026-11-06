package agent

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
	"vortex/agent/store"
)

func TestMissingAuthorityWaitRequiresSubscriptionAndDurableSuccess(t *testing.T) {
	n := &Node{NodeID: "node", BootID: "boot"}
	scope := RuntimeScope{RuleID: "rule", RuleRevision: 1, PathGeneration: 2}
	var calls atomic.Int32
	attempt := func(context.Context) (int, error) { calls.Add(1); return 0, store.ErrAuthorizationDenied }
	if _, err := withAuthorityWait(context.Background(), n, scope, attempt); !errors.Is(err, store.ErrAuthorizationDenied) || calls.Load() != 1 {
		t.Fatal("unconfigured gate waited")
	}
	wake, release, err := n.subscribeDemands([]AdaptiveDemandConfig{{Scope: store.DemandScope{NodeID: "node", BootID: "boot", RuleID: "rule", RuleRevision: 1, PathGeneration: 2}}})
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	var authorized atomic.Bool
	done := make(chan struct{})
	go func() { defer close(done); <-wake; time.Sleep(20 * time.Millisecond); authorized.Store(true) }()
	got, err := withAuthorityWait(context.Background(), n, scope, func(context.Context) (int, error) {
		if authorized.Load() {
			return 42, nil
		}
		return 0, store.ErrAuthorizationDenied
	})
	<-done
	if err != nil || got != 42 {
		t.Fatalf("authorization was not rechecked: %d %v", got, err)
	}
	started := time.Now()
	_, err = withAuthorityWait(context.Background(), n, scope, attempt)
	if !errors.Is(err, context.DeadlineExceeded) || time.Since(started) < 190*time.Millisecond || time.Since(started) > time.Second {
		t.Fatalf("unbounded/early wait %s %v", time.Since(started), err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	before := calls.Load()
	if _, err := withAuthorityWait(ctx, n, scope, attempt); !errors.Is(err, store.ErrAuthorizationDenied) || calls.Load() != before+1 {
		t.Fatal("cancellation retried")
	}
	before = calls.Load()
	if _, err := withAuthorityWait(context.Background(), n, RuntimeScope{RuleID: "other", RuleRevision: 1, PathGeneration: 2}, attempt); !errors.Is(err, store.ErrAuthorizationDenied) || calls.Load() != before+1 {
		t.Fatal("different scope waited")
	}
}

func TestMissingAuthorityWaitDoesNotRetryStorageFailure(t *testing.T) {
	n := &Node{NodeID: "node", BootID: "boot"}
	scope := RuntimeScope{RuleID: "rule", RuleRevision: 1, PathGeneration: 1}
	_, release, err := n.subscribeDemands([]AdaptiveDemandConfig{{Scope: store.DemandScope{NodeID: "node", BootID: "boot", RuleID: "rule", RuleRevision: 1, PathGeneration: 1}}})
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	failure := errors.New("disk failure")
	calls := 0
	_, err = withAuthorityWait(context.Background(), n, scope, func(context.Context) (int, error) { calls++; return 0, failure })
	if !errors.Is(err, failure) || calls != 1 {
		t.Fatalf("storage error retried: %d %v", calls, err)
	}
}
