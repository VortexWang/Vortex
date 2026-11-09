package store

import (
	"context"
	"sync"
	"testing"
	"time"
)

// Delay propagation to the coordinator without delaying the member's own Err.
// The group must observe cancellation before mutating, even if AfterFunc has
// not yet been scheduled on a busy runner.
type delayedCheckpointCancellation struct {
	context.Context
	allow <-chan struct{}
}

func (c delayedCheckpointCancellation) Value(any) any { return nil }
func (c delayedCheckpointCancellation) AfterFunc(f func()) func() bool {
	return context.AfterFunc(c.Context, func() { <-c.allow; f() })
}

func TestCrossRequestCheckpointsRollbackAndReplay(t *testing.T) {
	s, scope, _ := runtimeStore(t)
	ctx := context.Background()
	if _, err := s.db.Exec(`UPDATE authorization_records SET connections='2' WHERE kind='share_lease'`); err != nil {
		t.Fatal(err)
	}
	var requests []*checkpointRequest
	for _, direction := range []string{"egress", "ingress"} {
		a, err := s.OpenRuntimeSession(ctx, scope, "session-"+direction, "flow-"+direction)
		if err != nil {
			t.Fatal(err)
		}
		block := runtimeReservation("block-"+direction, 1000)
		block.Direction = direction
		block.FlowKey = "flow-" + direction
		if _, err := s.ReserveRuntimeFundingBlock(ctx, block, a.Use); err != nil {
			t.Fatal(err)
		}
		requests = append(requests, &checkpointRequest{ctx: ctx, entries: []RuntimeBlockCheckpoint{{Block: block, CheckpointID: "child-" + direction, Used: 100, Payload: []byte(direction)}}, result: make(chan error, 1)})
	}
	if _, err := s.db.Exec(`CREATE TRIGGER fail_cross_request BEFORE INSERT ON usage_batches WHEN NEW.batch_id='child-ingress' BEGIN SELECT RAISE(ABORT,'second caller'); END`); err != nil {
		t.Fatal(err)
	}
	// Seed both complete caller groups before draining so batching is deterministic.
	s.checkpoints.pending, s.checkpoints.count, s.checkpoints.running = requests, 2, true
	s.drainCheckpoints()
	for _, r := range requests {
		if err := <-r.result; err == nil {
			t.Fatal("partial success acknowledged")
		}
	}
	b, err := s.AuditAuthorizationSource(ctx, "node", "funding")
	if err != nil || b.Committed != 0 || b.Reserved != 2000 {
		t.Fatalf("rollback: %+v %v", b, err)
	}
	if _, err := s.db.Exec(`DROP TRIGGER fail_cross_request`); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if err := s.commitCheckpointRequests(requests); err != nil {
			t.Fatal(err)
		}
	}
	b, err = s.AuditAuthorizationSource(ctx, "node", "funding")
	if err != nil || b.Committed != 200 || b.Reserved != 1800 {
		t.Fatalf("replay: %+v %v", b, err)
	}
}

func TestCheckpointQueueOverflowAndCanceledMember(t *testing.T) {
	s, _, _ := runtimeStore(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := s.commitCheckpointRequests([]*checkpointRequest{{ctx: ctx}}); err == nil {
		t.Fatal("cancelled member accepted")
	}
	s.checkpoints.count = 64
	if err := s.enqueueCheckpoints(context.Background(), nil); err == nil {
		t.Fatal("queue overflow accepted")
	}
	s.checkpoints.count = 0
}

// Exercise cancellation after the coordinator has combined independent callers
// and is waiting for the runtime transaction slot, rather than before enqueue.
func TestCheckpointCoordinatorInterruptedWhileWaiting(t *testing.T) {
	for _, action := range []string{"cancel-member", "delayed-cancel-member", "close-store"} {
		t.Run(action, func(t *testing.T) {
			s, scope, path := runtimeStore(t)
			ctx := context.Background()
			if _, err := s.db.Exec(`UPDATE authorization_records SET connections='2' WHERE kind='share_lease'`); err != nil {
				t.Fatal(err)
			}
			memberCtx, cancel := context.WithCancel(ctx)
			defer cancel()
			var requestMemberCtx context.Context = memberCtx
			allow := make(chan struct{})
			var allowOnce sync.Once
			allowPropagation := func() { allowOnce.Do(func() { close(allow) }) }
			defer allowPropagation()
			if action == "delayed-cancel-member" {
				requestMemberCtx = delayedCheckpointCancellation{Context: memberCtx, allow: allow}
			}
			var requests []*checkpointRequest
			for i, direction := range []string{"egress", "ingress"} {
				a, err := s.OpenRuntimeSession(ctx, scope, "session-"+direction, "flow-"+direction)
				if err != nil {
					t.Fatal(err)
				}
				block := runtimeReservation("block-"+direction, 1000)
				block.Direction, block.FlowKey = direction, "flow-"+direction
				if _, err := s.ReserveRuntimeFundingBlock(ctx, block, a.Use); err != nil {
					t.Fatal(err)
				}
				requestCtx := ctx
				if i == 1 {
					requestCtx = requestMemberCtx
				}
				requests = append(requests, &checkpointRequest{ctx: requestCtx, entries: []RuntimeBlockCheckpoint{{Block: block, CheckpointID: "child-" + direction, Used: 100, Payload: []byte(direction)}}, result: make(chan error, 1)})
			}
			release, err := s.runtimeQueue.acquire(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer release()
			s.checkpoints.pending, s.checkpoints.count, s.checkpoints.running = requests, 2, true
			done := make(chan struct{})
			go func() { s.drainCheckpoints(); close(done) }()
			deadline := time.After(5 * time.Second)
			for {
				s.runtimeQueue.mu.Lock()
				waiting := s.runtimeQueue.waiting.Len()
				s.runtimeQueue.mu.Unlock()
				if waiting == 1 {
					break
				}
				select {
				case <-deadline:
					t.Fatal("coordinator never reached transaction wait")
				case <-time.After(time.Millisecond):
				}
			}
			for _, r := range requests {
				select {
				case err := <-r.result:
					t.Fatalf("acknowledged before durable outcome: %v", err)
				default:
				}
			}
			if action == "delayed-cancel-member" {
				// A FIFO follower runs only after the combined transaction releases
				// its slot. Keep asynchronous cancellation delayed until then.
				go func() {
					unlock, err := s.runtimeQueue.acquire(ctx)
					if err == nil {
						unlock()
					}
					allowPropagation()
				}()
				waitRuntimeQueue(t, &s.runtimeQueue, 2)
			}
			if action == "cancel-member" || action == "delayed-cancel-member" {
				cancel()
			} else if err := s.Close(); err != nil {
				t.Fatal(err)
			}
			release()
			select {
			case <-done:
			case <-time.After(5 * time.Second):
				t.Fatal("coordinator did not finish")
			}
			for _, r := range requests {
				if err := <-r.result; err == nil {
					t.Fatal("interrupted group acknowledged success")
				}
			}
			if s.checkpoints.count != 0 || s.checkpoints.running || len(s.checkpoints.pending) != 0 {
				t.Fatal("coordinator leaked pending callers")
			}
			if err := s.Close(); err != nil {
				t.Fatal(err)
			}
			recovered, err := Open(path, "agent")
			if err != nil {
				t.Fatal(err)
			}
			defer recovered.Close()
			balance, err := recovered.AuditAuthorizationSource(ctx, "node", "funding")
			if err != nil || balance.Committed != 0 || balance.Reserved != 2000 || balance.Unknown != 0 {
				t.Fatalf("interrupted conservation: %+v %v", balance, err)
			}
			var rows int
			if err := recovered.db.QueryRow(`SELECT COUNT(*) FROM usage_batches`).Scan(&rows); err != nil || rows != 0 {
				t.Fatalf("partial outbox: %d %v", rows, err)
			}
			for _, r := range requests {
				r.ctx = ctx
			}
			for i := 0; i < 2; i++ {
				if err := recovered.commitCheckpointRequests(requests); err != nil {
					t.Fatal(err)
				}
			}
			balance, err = recovered.AuditAuthorizationSource(ctx, "node", "funding")
			if err != nil || balance.Committed != 200 || balance.Reserved != 1800 || balance.Unknown != 0 {
				t.Fatalf("replay conservation: %+v %v", balance, err)
			}
		})
	}
}
