package service_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"vortex/backend/db/generated"
	"vortex/backend/internal/compiler"
	"vortex/backend/internal/service"
	controlv1 "vortex/contracts/protobuf/v1"
)

// An in-flight old command may still report its actual observation. That must
// retain its occupation and allow query/abort, without creating fresh activation
// work after a newer desired revision has superseded the old pending tasks.
func TestApplySupersededLateObservationCannotQueueCommit(t *testing.T) {
	for _, phase := range []string{"prepare", "query_prepared", "partial_commit"} {
		t.Run(phase, func(t *testing.T) {
			f := newPortFixture(t)
			ctx := context.Background()
			s := service.RuleService{Pool: f.pool}
			o := service.RuleOrchestrator{Pool: f.pool}
			in := ruleInput(f.group)
			reserveRulePort(t, f, in)
			if _, err := s.Mutate(ctx, f.scope, in, 0, uuid.NewString()); err != nil {
				t.Fatal(err)
			}
			r, err := s.CompileRevision(ctx, f.scope, in.ID, 1, compiler.BillingSnapshot{ID: uuid.NewString(), Version: 1, RuleRevision: 1, PathGeneration: 3}, 2, 3, 5, uuid.NewString())
			if err != nil {
				t.Fatal(err)
			}
			d, err := o.Begin(ctx, f.scope, in.ID, 1, r.InputDigest, uuid.NewString())
			if err != nil {
				t.Fatal(err)
			}
			commands := func(phase string) []*controlv1.RuleCommand {
				t.Helper()
				rows, err := f.pool.Query(ctx, `SELECT task_id FROM rule_delivery_bindings WHERE tenant_id=$1 AND deployment_id=$2 AND phase=$3 ORDER BY node_id,task_id`, f.tenant, d.ID, phase)
				if err != nil {
					t.Fatal(err)
				}
				var ids []string
				for rows.Next() {
					var id string
					if err = rows.Scan(&id); err != nil {
						t.Fatal(err)
					}
					ids = append(ids, id)
				}
				rows.Close()
				if err = rows.Err(); err != nil {
					t.Fatal(err)
				}
				var result []*controlv1.RuleCommand
				for _, id := range ids {
					c, _, _, err := service.RuleCommand(ctx, generated.New(f.pool), f.tenant, id)
					if err != nil {
						t.Fatal(err)
					}
					result = append(result, c)
				}
				return result
			}
			lease := uuid.NewString()
			start := func(c *controlv1.RuleCommand) {
				t.Helper()
				if _, err := f.pool.Exec(ctx, `UPDATE tasks SET state='running',lease_token=$3 WHERE tenant_id=$1 AND id=$2`, f.tenant, c.TaskKey, lease); err != nil {
					t.Fatal(err)
				}
			}
			accept := func(c *controlv1.RuleCommand, state string) {
				t.Helper()
				if err := o.ReceiptWithLease(ctx, f.scope, c.TaskKey, lease, receipt(c, state), uuid.NewString()); err != nil {
					t.Fatal(err)
				}
			}
			prepare := commands("prepare")
			if len(prepare) != 2 {
				t.Fatal("expected two fixed members", len(prepare))
			}
			for _, c := range prepare {
				start(c)
			}
			var late *controlv1.RuleCommand
			switch phase {
			case "prepare":
				accept(prepare[0], "prepared")
				late = prepare[1]
			case "query_prepared":
				accept(prepare[0], "prepared")
				accept(prepare[1], "unknown")
				late = commands("query")[0]
				start(late)
			case "partial_commit":
				for _, c := range prepare {
					accept(c, "prepared")
				}
				commit := commands("commit")
				if len(commit) != 2 {
					t.Fatal("expected all-member commit", len(commit))
				}
				for _, c := range commit {
					start(c)
				}
				accept(commit[0], "active")
				accept(commit[1], "unknown")
				late = commands("query")[0]
				start(late)
			}
			in.Name = "new desired revision"
			if _, err := s.Mutate(ctx, f.scope, in, 1, uuid.NewString()); err != nil {
				t.Fatal(err)
			}
			accept(late, "prepared")
			var commits int
			if err := f.pool.QueryRow(ctx, `SELECT count(*) FROM rule_delivery_bindings b JOIN tasks t ON t.tenant_id=b.tenant_id AND t.id=b.task_id WHERE b.tenant_id=$1 AND b.deployment_id=$2 AND b.phase='commit' AND t.state IN ('pending','leased','running')`, f.tenant, d.ID).Scan(&commits); err != nil {
				t.Fatal(err)
			}
			if commits != 0 {
				t.Fatalf("late %s observation recreated %d superseded commit tasks", phase, commits)
			}
			wantAborts := 2
			if phase == "partial_commit" {
				wantAborts = 1
			}
			if got := len(commands("abort")); got != wantAborts {
				t.Fatalf("prepared obsolete resources need scoped abort: got %d want %d", got, wantAborts)
			}
			var desired, active int64
			var state string
			if err := f.pool.QueryRow(ctx, `SELECT desired_revision,active_revision,sync_status FROM rule_heads WHERE tenant_id=$1 AND id=$2`, f.tenant, in.ID).Scan(&desired, &active, &state); err != nil || desired != 2 || active != 0 || state != "pending" {
				t.Fatal("old observation altered new authority", err, desired, active, state)
			}
			var released int
			if err := f.pool.QueryRow(ctx, `SELECT count(*) FROM operations WHERE tenant_id=$1 AND NOT occupation_held`, f.tenant).Scan(&released); err != nil || released != 0 {
				t.Fatal("supersession released unproved occupation", err, released)
			}
		})
	}
}
