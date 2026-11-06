package store

import (
	"context"
	"errors"
	"runtime"
	"testing"
	"time"
)

func waitRuntimeQueue(t *testing.T, q *runtimeTransactionQueue, n int) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		q.mu.Lock()
		count := q.waiting.Len()
		q.mu.Unlock()
		if count == n {
			return
		}
		runtime.Gosched()
	}
	t.Fatal("queue did not reach expected length")
}

func TestRuntimeQueueFIFOAndCanceledWaiter(t *testing.T) {
	var q runtimeTransactionQueue
	ctx, stop := context.WithTimeout(context.Background(), 5*time.Second)
	defer stop()
	release, err := q.acquire(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	order := make(chan int, 3)
	cancelCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	for i := 0; i < 3; i++ {
		c := ctx
		if i == 1 {
			c = cancelCtx
		}
		go func(i int, c context.Context) {
			unlock, err := q.acquire(c)
			if err != nil {
				order <- -i
				return
			}
			order <- i
			unlock()
			unlock() // A duplicate finish cannot release another owner.
		}(i, c)
		waitRuntimeQueue(t, &q, i+1)
	}
	cancel()
	if got := <-order; got != -1 {
		t.Fatalf("cancel=%d", got)
	}
	waitRuntimeQueue(t, &q, 2)
	release()
	for _, want := range []int{0, 2} {
		select {
		case got := <-order:
			if got != want {
				t.Fatalf("order=%d want=%d", got, want)
			}
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		}
	}
}

func TestRuntimeTransactionCanceledRollbackReleasesQueue(t *testing.T) {
	s, _, _ := runtimeStore(t)
	ctx, cancel := context.WithCancel(context.Background())
	tx, err := s.beginRuntimeTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tx.ExecContext(ctx, `UPDATE authorization_records SET state='revoked' WHERE authorization_id='grant'`); err != nil {
		t.Fatal(err)
	}
	cancel()
	_ = tx.Rollback()
	nextCtx, stop := context.WithTimeout(context.Background(), time.Second)
	defer stop()
	next, err := s.beginRuntimeTx(nextCtx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer next.Rollback()
	_, state, err := readAuthorizationTx(nextCtx, next, "node", "grant")
	if err != nil || state != "active" {
		t.Fatalf("rollback state=%s err=%v", state, err)
	}
	if _, err := s.runtimeQueue.acquire(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled admission=%v", err)
	}
}
