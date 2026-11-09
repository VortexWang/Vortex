package store

import (
	"context"
	"testing"
)

func TestRuntimeBlockCheckpointBatchAtomicReplayRecovery(t *testing.T) {
	s, scope, path := runtimeStore(t)
	ctx := context.Background()
	a, err := s.OpenRuntimeSession(ctx, scope, "session", "flow")
	if err != nil {
		t.Fatal(err)
	}
	block := runtimeReservation("block", 1000)
	if _, err := s.ReserveRuntimeFundingBlock(ctx, block, a.Use); err != nil {
		t.Fatal(err)
	}
	tail := block
	tail.Reserved = 900
	entries := []RuntimeBlockCheckpoint{{block, "one", 100, []byte("first")}, {tail, "two", 200, []byte("second")}}
	if _, err := s.db.Exec(`CREATE TRIGGER fail_second BEFORE INSERT ON usage_batches WHEN NEW.batch_id='two' BEGIN SELECT RAISE(ABORT,'second checkpoint'); END`); err != nil {
		t.Fatal(err)
	}
	if err := s.CheckpointRuntimeFundingBlocks(ctx, entries); err == nil {
		t.Fatal("partial checkpoint acknowledged")
	}
	b, err := s.AuditAuthorizationSource(ctx, "node", "funding")
	if err != nil || b.Committed != 0 || b.Reserved != 1000 {
		t.Fatalf("rollback %+v %v", b, err)
	}
	var count int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM usage_batches`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("partial outbox %d %v", count, err)
	}
	if _, err := s.db.Exec(`DROP TRIGGER fail_second`); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if err := s.CheckpointRuntimeFundingBlocks(ctx, entries); err != nil {
			t.Fatal(err)
		}
	}
	changed := append([]RuntimeBlockCheckpoint(nil), entries...)
	changed[1].Payload = []byte("mutated")
	if err := s.CheckpointRuntimeFundingBlocks(ctx, changed); err == nil {
		t.Fatal("mutated replay")
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(path, "agent")
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	pending, err := reopened.UnresolvedUsageReservations(ctx, "node", "next-boot")
	if err != nil || len(pending) != 1 || pending[0].Reserved != 700 {
		t.Fatalf("recovery %+v %v", pending, err)
	}
	if err := reopened.SettleUsageReservation(ctx, pending[0], "unknown", 700, []byte("unknown"), 700); err != nil {
		t.Fatal(err)
	}
	if err := reopened.CheckpointRuntimeFundingBlocks(ctx, entries); err != nil {
		t.Fatal(err)
	}
	b, err = reopened.AuditAuthorizationSource(ctx, "node", "funding")
	if err != nil || b.Committed != 300 || b.Unknown != 700 || b.Reserved != 0 || b.Remaining != (1<<20)-1000 {
		t.Fatalf("conservation %+v %v", b, err)
	}
}

func TestRuntimeBlockCheckpointBatchRejectsInvalidSequence(t *testing.T) {
	for _, variant := range []string{"stale", "duplicate", "overflow", "too-many", "canceled"} {
		t.Run(variant, func(t *testing.T) {
			s, scope, _ := runtimeStore(t)
			ctx := context.Background()
			a, err := s.OpenRuntimeSession(ctx, scope, "session", "flow")
			if err != nil {
				t.Fatal(err)
			}
			block := runtimeReservation("block", 100)
			if _, err := s.ReserveRuntimeFundingBlock(ctx, block, a.Use); err != nil {
				t.Fatal(err)
			}
			tail := block
			tail.Reserved = 60
			entries := []RuntimeBlockCheckpoint{{block, "one", 40, []byte("one")}, {tail, "two", 30, []byte("two")}}
			switch variant {
			case "stale":
				entries[1].Block.Reserved = 100
			case "duplicate":
				entries[1].CheckpointID = "one"
			case "overflow":
				entries[1].Used = 61
			case "too-many":
				for len(entries) < 65 {
					entries = append(entries, entries[0])
				}
			case "canceled":
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			}
			if err := s.CheckpointRuntimeFundingBlocks(ctx, entries); err == nil {
				t.Fatal("invalid group succeeded")
			}
			b, err := s.AuditAuthorizationSource(context.Background(), "node", "funding")
			if err != nil || b.Reserved != 100 || b.Committed != 0 {
				t.Fatalf("partial group %+v %v", b, err)
			}
		})
	}
}
