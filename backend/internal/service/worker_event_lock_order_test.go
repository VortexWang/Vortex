package service_test

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"vortex/backend/db/generated"
	"vortex/backend/internal/service"
	"vortex/backend/internal/worker"
	controlv1 "vortex/contracts/protobuf/v1"
)

// Operation events acquire a tenant FK key-share lock even when a command
// needs no activation authority. A worker must not own child rows while waiting
// for that tenant; otherwise an unrelated tenant mutation completes a cycle.
func TestWorkerEventWaitDoesNotHoldBusinessLocks(t *testing.T) {
	for _, kind := range []string{"control", "expired-control", "control-receipt", "port-release", "rule-query", "port-projection", "control-superseded", "port-release-superseded"} {
		t.Run(kind, func(t *testing.T) {
			superseded := strings.HasSuffix(kind, "-superseded")
			kind := strings.TrimSuffix(kind, "-superseded")
			isolatePackageAuthority(t)
			f := newPortFixture(t)
			ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
			defer cancel()
			var calls atomic.Int64
			var work func() error
			switch kind {
			case "control", "expired-control", "control-receipt":
				tracer := &service.Tracer{Pool: f.pool}
				m := mutation()
				m.ResourceID = f.nodes[0]
				op, err := tracer.Mutate(ctx, f.scope, m)
				if err != nil {
					t.Fatal(err)
				}
				if kind == "control-receipt" {
					work = func() error {
						return tracer.Reconcile(ctx, f.tenant, op.ID, service.Receipt{Revision: op.TargetRevision, Generation: op.Generation, State: "succeeded", Payload: []byte("proof")})
					}
				}
				if kind != "control-receipt" {
					w := worker.Worker{Pool: f.pool, Adapter: workerEventAdapter{calls: &calls}}
					work = func() error { _, err := w.Once(ctx); return err }
					if kind == "expired-control" {
						if _, err = f.pool.Exec(ctx, `UPDATE tasks SET state='running',lease_token='expired-test',lease_until=now()-interval '1 second' WHERE tenant_id=$1 AND id=$2`, f.tenant, op.ID); err != nil {
							t.Fatal(err)
						}
					}
				}
			case "port-release", "rule-query":
				rules := service.RuleService{Pool: f.pool}
				in := ruleInput(f.group)
				reserveRulePort(t, f, in)
				if _, err := rules.Mutate(ctx, f.scope, in, 0, uuid.NewString()); err != nil {
					t.Fatal(err)
				}
				compiled, err := rules.CompileRevision(ctx, f.scope, in.ID, 1, durableBilling(t, f, in.ID, 1, 3), 2, 3, 5, uuid.NewString())
				if err != nil {
					t.Fatal(err)
				}
				if _, err = (service.RuleOrchestrator{Pool: f.pool}).Begin(ctx, f.scope, in.ID, 1, compiled.InputDigest, uuid.NewString()); err != nil {
					t.Fatal(err)
				}
				if kind == "port-release" {
					var allocation string
					if err = f.pool.QueryRow(ctx, `SELECT id FROM port_allocations WHERE tenant_id=$1 AND rule_id=$2`, f.tenant, in.ID).Scan(&allocation); err != nil {
						t.Fatal(err)
					}
					if err = f.ports.RequestRelease(ctx, f.scope, allocation, "explicit", uuid.NewString()); err != nil {
						t.Fatal(err)
					}
					// Revoking activation must not prevent a stop of the exact old artifact.
					if _, err = f.pool.Exec(ctx, `UPDATE tenants SET account_state='suspended' WHERE id=$1`, f.tenant); err != nil {
						t.Fatal(err)
					}
					w := worker.PortWorker{Pool: f.pool, Adapter: portReleaseAdapter(func(_ context.Context, _ service.TenantScope, _, _ string, c *controlv1.RuleCommand, _ service.PortProof) error {
						if c.Phase != "release" {
							t.Error("release path sent activation", c.Phase)
						}
						calls.Add(1)
						return nil
					})}
					work = func() error { _, err := w.Once(ctx); return err }
				} else {
					w := worker.RuleWorker{Pool: f.pool, Adapter: portRuleAdapter(func(_ context.Context, _, _ string, c *controlv1.RuleCommand) (*controlv1.RuleReceipt, error) {
						return receipt(c, "unknown"), nil
					})}
					if _, err = w.Once(ctx); err != nil {
						t.Fatal(err)
					}
					if _, err = f.pool.Exec(ctx, `UPDATE tasks SET state='superseded' WHERE tenant_id=$1 AND task_kind='rule_prepare' AND state='pending'`, f.tenant); err != nil {
						t.Fatal(err)
					}
					if _, err = f.pool.Exec(ctx, `UPDATE operations SET created_at=now()-interval '2 seconds' WHERE tenant_id=$1 AND id IN(SELECT task_id FROM rule_delivery_bindings WHERE tenant_id=$1 AND phase='query')`, f.tenant); err != nil {
						t.Fatal(err)
					}
					w.Adapter = portRuleAdapter(func(_ context.Context, _, _ string, c *controlv1.RuleCommand) (*controlv1.RuleReceipt, error) {
						if c.Phase != "query" {
							t.Error(c.Phase)
						}
						calls.Add(1)
						return receipt(c, "unknown"), nil
					})
					work = func() error { _, err := w.Once(ctx); return err }
				}
			case "port-projection":
				r, err := f.ports.PreparePortReservation(ctx, f.scope, f.request(t, 20000), uuid.NewString())
				if err != nil {
					t.Fatal(err)
				}
				f.commit(t, r.PortAllocation)
				work = func() error { _, err := (worker.PortOperationWorker{Pool: f.pool}).Once(ctx); return err }
			}
			holder, err := f.pool.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer holder.Rollback(context.Background())
			if _, err = holder.Exec(ctx, `SELECT id FROM tenants WHERE id=$1 FOR UPDATE`, f.tenant); err != nil {
				t.Fatal(err)
			}
			done := make(chan error, 1)
			go func() { done <- work() }()
			joined := false
			defer func() {
				holder.Rollback(context.Background())
				cancel()
				if !joined {
					<-done
				}
			}()
			waiting := false
			deadline := time.Now().Add(3 * time.Second)
			for time.Now().Before(deadline) {
				if err = f.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE application_name=current_setting('application_name') AND state='active' AND wait_event_type='Lock')`).Scan(&waiting); err != nil {
					t.Fatal(err)
				}
				if waiting {
					break
				}
				time.Sleep(5 * time.Millisecond)
			}
			if !waiting {
				t.Fatal("worker did not reach tenant/event boundary")
			}
			for _, query := range []string{`SELECT id FROM tasks WHERE tenant_id=$1 FOR UPDATE NOWAIT`, `SELECT id FROM operations WHERE tenant_id=$1 FOR UPDATE NOWAIT`, `SELECT id FROM port_allocations WHERE tenant_id=$1 FOR UPDATE NOWAIT`} {
				if _, err = holder.Exec(ctx, query, f.tenant); err != nil {
					t.Fatal("tenant wait retained a business lock", err)
				}
			}
			if superseded {
				if _, err = holder.Exec(ctx, `UPDATE tasks SET state='superseded' WHERE tenant_id=$1 AND state='leased'`, f.tenant); err != nil {
					t.Fatal(err)
				}
			}
			if err = holder.Commit(ctx); err != nil {
				t.Fatal(err)
			}
			select {
			case err = <-done:
				joined = true
				if err != nil {
					t.Fatal(err)
				}
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
			want := int64(0)
			if kind == "control" || kind == "port-release" || kind == "rule-query" {
				want = 1
			}
			if superseded {
				want = 0
			}
			if calls.Load() != want {
				t.Fatal("unexpected transport count", calls.Load(), want)
			}
			if kind == "port-release" || kind == "rule-query" {
				var freed int
				if err = f.pool.QueryRow(ctx, `SELECT count(*) FROM port_allocations WHERE tenant_id=$1 AND state IN('released','aborted')`, f.tenant).Scan(&freed); err != nil || freed != 0 {
					t.Fatal("absent stop proof freed occupation", freed, err)
				}
			}
		})
	}
}

type workerEventAdapter struct{ calls *atomic.Int64 }

func (a workerEventAdapter) Deliver(_ context.Context, op generated.Operation) (service.Receipt, error) {
	a.calls.Add(1)
	return service.Receipt{Revision: op.TargetRevision, Generation: op.Generation, State: "succeeded", Payload: []byte("proof")}, nil
}

func TestExpiredControlEventFailureKeepsAttemptRecoverable(t *testing.T) {
	isolatePackageAuthority(t)
	f := newPortFixture(t)
	ctx := context.Background()
	tracer := &service.Tracer{Pool: f.pool}
	m := mutation()
	op, err := tracer.Mutate(ctx, f.scope, m)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.pool.Exec(ctx, `UPDATE tasks SET state='running',lease_token='crashed',lease_until=now()-interval '1 second' WHERE tenant_id=$1 AND id=$2`, f.tenant, op.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = f.pool.Exec(ctx, `CREATE FUNCTION fail_expiry_event() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.state='unknown' THEN RAISE EXCEPTION 'injected expiry event failure'; END IF; RETURN NEW; END $$; CREATE TRIGGER fail_expiry_event BEFORE UPDATE ON operations FOR EACH ROW EXECUTE FUNCTION fail_expiry_event()`); err != nil {
		t.Fatal(err)
	}
	var calls atomic.Int64
	w := worker.Worker{Pool: f.pool, Adapter: workerEventAdapter{calls: &calls}}
	if _, err = w.Once(ctx); err == nil || !strings.Contains(err.Error(), "injected expiry event failure") {
		t.Fatal("event failure did not abort expiry", err)
	}
	var task, state string
	var leased, held bool
	read := func() {
		t.Helper()
		if err = f.pool.QueryRow(ctx, `SELECT t.state,t.lease_until IS NOT NULL,o.state,o.occupation_held FROM tasks t JOIN operations o ON o.tenant_id=t.tenant_id AND o.id=t.operation_id WHERE t.tenant_id=$1 AND t.id=$2`, f.tenant, op.ID).Scan(&task, &leased, &state, &held); err != nil {
			t.Fatal(err)
		}
	}
	read()
	if task != "running" || !leased || state != "pending" || !held {
		t.Fatal("partial failed expiry", task, leased, state, held)
	}
	if _, err = f.pool.Exec(ctx, `DROP TRIGGER fail_expiry_event ON operations`); err != nil {
		t.Fatal(err)
	}
	if worked, err := w.Once(ctx); err != nil || worked {
		t.Fatal(worked, err)
	}
	read()
	if task != "unknown" || leased || state != "unknown" || !held || calls.Load() != 0 {
		t.Fatal("retry did not retain uncertainty", task, leased, state, held, calls.Load())
	}
}
