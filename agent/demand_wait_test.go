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
	if _, err := withAuthorityWait(ctx, n, scope, attempt); !errors.Is(err, context.Canceled) || calls.Load() != before {
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

func TestAuthorityDeadlineIncludesInitialAttempt(t *testing.T) {
	n := &Node{NodeID: "node", BootID: "boot"}
	scope := RuntimeScope{RuleID: "rule", RuleRevision: 1, PathGeneration: 1}
	_, release, err := n.subscribeDemands([]AdaptiveDemandConfig{{Scope: store.DemandScope{NodeID: "node", BootID: "boot", RuleID: "rule", RuleRevision: 1, PathGeneration: 1}}})
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	var original time.Time
	calls := 0
	started := time.Now()
	_, err = withAuthorityWait(context.Background(), n, scope, func(ctx context.Context) (int, error) {
		deadline, ok := ctx.Deadline()
		if !ok {
			t.Fatal("initial attempt lacks deadline")
		}
		calls++
		if calls == 1 {
			original = deadline
			select {
			case <-time.After(120 * time.Millisecond):
				return 0, store.ErrAuthorizationDenied
			case <-ctx.Done():
				return 0, ctx.Err()
			}
		}
		if !deadline.Equal(original) {
			t.Fatal("retry extended deadline")
		}
		<-ctx.Done()
		return 0, ctx.Err()
	})
	if !errors.Is(err, context.DeadlineExceeded) || calls != 2 || original.Sub(started) > missingAuthorityWait+time.Millisecond {
		t.Fatalf("deadline/attempts: %v calls=%d budget=%s", err, calls, original.Sub(started))
	}
}

func TestMissingAuthoritySlowDenialGetsFreshRetryInterval(t *testing.T) {
	n := &Node{NodeID: "node", BootID: "boot"}
	scope := RuntimeScope{RuleID: "rule", RuleRevision: 1, PathGeneration: 1}
	_, release, err := n.subscribeDemands([]AdaptiveDemandConfig{{Scope: store.DemandScope{NodeID: "node", BootID: "boot", RuleID: "rule", RuleRevision: 1, PathGeneration: 1}}})
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	calls := 0
	var finished time.Time
	value, err := withAuthorityWait(context.Background(), n, scope, func(ctx context.Context) (int, error) {
		calls++
		switch calls {
		case 1:
			return 0, store.ErrAuthorizationDenied
		case 2:
			// A periodic10ms ticker would already be ready when this check ends.
			select {
			case <-time.After(40 * time.Millisecond):
			case <-ctx.Done():
				return 0, ctx.Err()
			}
			finished = time.Now()
			return 0, store.ErrAuthorizationDenied
		default:
			if gap := time.Since(finished); gap < 20*time.Millisecond {
				t.Errorf("slow denial immediately requeued another authority check: gap=%s", gap)
			}
			return 42, nil
		}
	})
	if err != nil || value != 42 || calls != 3 {
		t.Fatalf("retry failed: value=%d err=%v calls=%d", value, err, calls)
	}
}

func TestAuthorityNotificationIsOnlyRetryHint(t *testing.T) {
	n := &Node{NodeID: "node", BootID: "boot"}
	before := n.authorityChanges()
	n.notifyAuthorityChanged()
	select {
	case <-before:
	default:
		t.Fatal("commit notification did not close captured generation")
	}
	select {
	case <-n.authorityChanges():
		t.Fatal("new generation already signaled")
	default:
	}
	scope := RuntimeScope{RuleID: "rule", RuleRevision: 1, PathGeneration: 1}
	_, release, err := n.subscribeDemands([]AdaptiveDemandConfig{{Scope: store.DemandScope{NodeID: "node", BootID: "boot", RuleID: "rule", RuleRevision: 1, PathGeneration: 1}}})
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	calls := 0
	_, err = withAuthorityWait(context.Background(), n, scope, func(context.Context) (int, error) {
		calls++
		if calls == 1 {
			n.notifyAuthorityChanged()
		}
		return 0, store.ErrAuthorizationDenied
	})
	if !errors.Is(err, context.DeadlineExceeded) || calls < 2 {
		t.Fatalf("notification bypassed durable check: calls=%d err=%v", calls, err)
	}
}
