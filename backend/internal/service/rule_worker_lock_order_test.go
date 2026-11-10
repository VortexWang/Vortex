package service_test

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"vortex/backend/internal/service"
	"vortex/backend/internal/worker"
	controlv1 "vortex/contracts/protobuf/v1"
)

// A current-authority wait must not hold the task needed by a tenant mutation.
// Otherwise tenant -> task and task -> tenant form a real PostgreSQL deadlock.
func TestRuleWorkerAuthorityWaitDoesNotHoldTaskLock(t *testing.T) {
	isolatePackageAuthority(t)
	f := newPortFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	s := service.RuleService{Pool: f.pool}
	in := ruleInput(f.group)
	reserveRulePort(t, f, in)
	if _, err := s.Mutate(ctx, f.scope, in, 0, uuid.NewString()); err != nil {
		t.Fatal(err)
	}
	r, err := s.CompileRevision(ctx, f.scope, in.ID, 1, durableBilling(t, f, in.ID, 1, 3), 2, 3, 5, uuid.NewString())
	if err != nil {
		t.Fatal(err)
	}
	if _, err = (service.RuleOrchestrator{Pool: f.pool}).Begin(ctx, f.scope, in.ID, 1, r.InputDigest, uuid.NewString()); err != nil {
		t.Fatal(err)
	}
	mutation, err := f.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer mutation.Rollback(context.Background())
	if _, err = mutation.Exec(ctx, `SELECT id FROM tenants WHERE id=$1 FOR UPDATE`, f.tenant); err != nil {
		t.Fatal(err)
	}
	var calls atomic.Int64
	w := worker.RuleWorker{Pool: f.pool, Adapter: portRuleAdapter(func(_ context.Context, _, _ string, c *controlv1.RuleCommand) (*controlv1.RuleReceipt, error) {
		calls.Add(1)
		return receipt(c, "prepared"), nil
	})}
	done := make(chan error, 1)
	go func() { _, err := w.Once(ctx); done <- err }()
	joined := false
	defer func() {
		_ = mutation.Rollback(context.Background())
		cancel()
		if !joined {
			<-done
		}
	}()
	deadline := time.Now().Add(3 * time.Second)
	waiting := false
	for time.Now().Before(deadline) {
		if err = f.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE datname=current_database() AND wait_event_type='Lock' AND query ILIKE '%FROM tenants%FOR UPDATE%')`).Scan(&waiting); err != nil {
			t.Fatal(err)
		}
		if waiting {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if !waiting {
		t.Fatal("worker did not reach the concurrent authority boundary")
	}
	if _, err = mutation.Exec(ctx, `SELECT id FROM tasks WHERE tenant_id=$1 AND task_kind='rule_prepare' FOR UPDATE NOWAIT`, f.tenant); err != nil {
		t.Fatal("authority wait retained a task lock needed by the tenant transaction", err)
	}
	// The tenant transaction wins the authority change while the leased worker
	// waits. The resumed worker must recheck that durable lease before transport.
	if _, err = mutation.Exec(ctx, `UPDATE tasks SET state='superseded' WHERE tenant_id=$1 AND task_kind='rule_prepare'`, f.tenant); err != nil {
		t.Fatal(err)
	}
	if err = mutation.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	select {
	case err = <-done:
		joined = true
		if err != nil || calls.Load() != 0 {
			t.Fatal("superseded leased work reached transport", err, calls.Load())
		}
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
}
