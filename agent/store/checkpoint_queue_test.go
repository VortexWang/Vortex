package store

import (
	"context"
	"testing"
)

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
