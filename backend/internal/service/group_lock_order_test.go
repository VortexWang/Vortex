package service_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"vortex/backend/internal/service"
)

// A blocked compiler/group editor must not keep a rule head/group locked while
// waiting for the tenant owner, which may need that same row before commit.
// This uses real PostgreSQL lock waits rather than inferring order from code.
func TestGroupAndCompileWaitForTenantBeforeBusinessRows(t *testing.T) {
	isolatePackageAuthority(t)
	f := newPortFixture(t)
	ctx := context.Background()
	rules := service.RuleService{Pool: f.pool}
	in := ruleInput(f.group)
	reserveRulePort(t, f, in)
	mutation, e := rules.Mutate(ctx, f.scope, in, 0, uuid.NewString())
	if e != nil {
		t.Fatal(e)
	}
	billing := durableBilling(t, f, in.ID, 1, 3)
	for _, action := range []string{"bind", "compile", "members"} {
		t.Run(action, func(t *testing.T) {
			run, cancel := context.WithTimeout(ctx, 6*time.Second)
			defer cancel()
			holder, e := f.pool.Begin(run)
			if e != nil {
				t.Fatal(e)
			}
			defer holder.Rollback(ctx)
			if _, e = holder.Exec(run, `SELECT id FROM tenants WHERE id=$1 FOR UPDATE`, f.tenant); e != nil {
				t.Fatal(e)
			}
			done := make(chan error, 1)
			go func() {
				switch action {
				case "bind":
					_, e := rules.BindCompileInput(run, f.scope, mutation.OperationID, billing, 2, 3, 4, uuid.NewString())
					done <- e
				case "compile":
					_, e := rules.CompileRevision(run, f.scope, in.ID, 1, billing, 2, 3, 4, uuid.NewString())
					done <- e
				case "members":
					e := f.nodeService.SetMembers(run, f.scope, f.group, 2, []service.GroupMember{{NodeID: f.nodes[0], Weight: 1}}, uuid.NewString())
					done <- e
				}
			}()
			blocked := false
			deadline := time.Now().Add(3 * time.Second)
			for time.Now().Before(deadline) {
				if e = f.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE application_name=current_setting('application_name') AND wait_event_type='Lock' AND state='active')`).Scan(&blocked); e != nil {
					t.Fatal(e)
				}
				if blocked {
					break
				}
				time.Sleep(5 * time.Millisecond)
			}
			if !blocked {
				cancel()
				holder.Rollback(ctx)
				<-done
				t.Fatal("expected real tenant lock wait")
			}
			// If the blocked producer already owns this business row, NOWAIT fails.
			// Rolling back first makes the failed-path goroutine cleanup deterministic.
			if action == "members" {
				_, e = holder.Exec(run, `SELECT id FROM node_groups WHERE id=$1 FOR UPDATE NOWAIT`, f.group)
			} else {
				_, e = holder.Exec(run, `SELECT id FROM rule_heads WHERE tenant_id=$1 AND id=$2 FOR UPDATE NOWAIT`, f.tenant, in.ID)
			}
			if e != nil {
				holder.Rollback(ctx)
				cancel()
				<-done
				t.Fatal("producer acquired business row before tenant", e)
			}
			if e = holder.Commit(run); e != nil {
				t.Fatal(e)
			}
			if e = <-done; e != nil {
				t.Fatal("ordered producer failed", e)
			}
		})
	}
}
