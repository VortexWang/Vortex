package store

import (
	"context"
	"errors"
	"testing"
)

func TestRuntimeFundingBlockReservesWithoutSpendingRateAndCannotReplayAsWrite(t *testing.T) {
	s, scope, _ := runtimeStore(t)
	ctx := context.Background()
	a, err := s.OpenRuntimeSession(ctx, scope, "session", "flow")
	if err != nil {
		t.Fatal(err)
	}
	block := runtimeReservation("block", 256<<10)
	id, err := s.ReserveRuntimeFundingBlock(ctx, block, a.Use)
	if err != nil || id != "grant" {
		t.Fatalf("block=%s %v", id, err)
	}
	if _, err := s.ReserveRuntimeFundingBlock(ctx, block, a.Use); err != nil {
		t.Fatal(err)
	}
	var buckets int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM runtime_rate_buckets`).Scan(&buckets); err != nil || buckets != 0 {
		t.Fatalf("allocation consumed rate: %d %v", buckets, err)
	}
	if _, err := s.ReserveRuntimeUsage(ctx, block, a.Use); !errors.Is(err, ErrUsageBatchConflict) {
		t.Fatalf("block replayed as runtime write: %v", err)
	}
	if _, err := s.ReserveAuthorizedUsage(ctx, block, a.Use); !errors.Is(err, ErrUsageBatchConflict) {
		t.Fatalf("block replayed as authorized write: %v", err)
	}
	another := block
	another.BatchID = "another"
	if _, err := s.ReserveRuntimeFundingBlock(ctx, another, a.Use); err == nil {
		t.Fatal("second live direction block accepted")
	}
	other := block
	other.BatchID = "ingress"
	other.Direction = "ingress"
	if _, err := s.ReserveRuntimeFundingBlock(ctx, other, a.Use); err != nil {
		t.Fatal(err)
	}
	b, err := s.AuditAuthorizationSource(ctx, "node", "funding")
	if err != nil || b.Initial != 1<<20 || b.Reserved != 512<<10 || b.Remaining != 512<<10 || b.Unknown != 0 || b.Committed != 0 {
		t.Fatalf("conservation: %+v %v", b, err)
	}
	// A real write still has to pass the shared rate limiter, independently of
	// the larger amount held for possible future writes.
	write := runtimeReservation("write", 65536)
	if _, err := s.ReserveRuntimeUsage(ctx, write, a.Use); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ReserveRuntimeUsage(ctx, runtimeReservation("over-rate", 1), a.Use); err == nil {
		t.Fatal("rate bypass after funding block")
	}
	if err := s.SettleUsageReservation(ctx, block, "returned", 0, nil, 0); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ReserveRuntimeFundingBlock(ctx, another, a.Use); err != nil {
		t.Fatalf("returned block did not release direction slot: %v", err)
	}
}

func TestRuntimeFundingBlockFailureAndOldBootRecovery(t *testing.T) {
	s, scope, path := runtimeStore(t)
	ctx := context.Background()
	a, err := s.OpenRuntimeSession(ctx, scope, "session", "flow")
	if err != nil {
		t.Fatal(err)
	}
	block := runtimeReservation("block", 256<<10)
	if _, err := s.db.Exec(`CREATE TRIGGER fail_block BEFORE INSERT ON runtime_funding_blocks BEGIN SELECT RAISE(ABORT,'injected block failure'); END`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ReserveRuntimeFundingBlock(ctx, block, a.Use); err == nil {
		t.Fatal("partial allocation succeeded")
	}
	b, err := s.AuditAuthorizationSource(ctx, "node", "funding")
	if err != nil || b.Remaining != 1<<20 || b.Reserved != 0 {
		t.Fatalf("partial allocation: %+v %v", b, err)
	}
	if _, err := s.db.Exec(`DROP TRIGGER fail_block`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ReserveRuntimeFundingBlock(ctx, block, a.Use); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(path, "agent")
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	pending, err := reopened.UnresolvedUsageReservations(ctx, "node", "new-boot")
	if err != nil || len(pending) != 1 || pending[0].Reserved != block.Reserved || pending[0].AuthorizationID != "grant" {
		t.Fatalf("recovery upper bound: %+v %v", pending, err)
	}
	// Store-level recovery preserves the entire unproven block. Typed wire
	// recovery is already exercised by the Node recovery integration suite.
	if err := reopened.SettleUsageReservation(ctx, pending[0], "unknown", block.Reserved, []byte("unknown-block"), block.Reserved); err != nil {
		t.Fatal(err)
	}
	if err := reopened.SettleUsageReservation(ctx, pending[0], "returned", 0, nil, 0); !errors.Is(err, ErrUsageBatchConflict) {
		t.Fatalf("unknown block refunded: %v", err)
	}
	b, err = reopened.AuditAuthorizationSource(ctx, "node", "funding")
	if err != nil || b.Unknown != block.Reserved || b.Remaining != (1<<20)-block.Reserved || b.Reserved != 0 {
		t.Fatalf("recovered conservation: %+v %v", b, err)
	}
}

func TestRuntimeFundingBlockRejectsOversizeAndClosedSession(t *testing.T) {
	s, scope, _ := runtimeStore(t)
	ctx := context.Background()
	a, err := s.OpenRuntimeSession(ctx, scope, "session", "flow")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.ReserveRuntimeFundingBlock(ctx, runtimeReservation("too-big", (4<<20)+1), a.Use); err == nil {
		t.Fatal("unbounded block accepted")
	}
	if err := s.CloseRuntimeSession(ctx, "node", "boot", "session"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ReserveRuntimeFundingBlock(ctx, runtimeReservation("closed", 100), a.Use); err == nil {
		t.Fatal("closed session funded")
	}
	b, err := s.AuditAuthorizationSource(ctx, "node", "funding")
	if err != nil || b.Remaining != 1<<20 || b.Reserved != 0 {
		t.Fatalf("rejected block changed funds: %+v %v", b, err)
	}
}

func TestRuntimeFundingBlockCheckpointAtomicRecovery(t *testing.T) {
	s, scope, path := runtimeStore(t)
	ctx := context.Background()
	a, err := s.OpenRuntimeSession(ctx, scope, "session", "flow")
	if err != nil {
		t.Fatal(err)
	}
	block := runtimeReservation("block", 256<<10)
	if _, err := s.ReserveRuntimeFundingBlock(ctx, block, a.Use); err != nil {
		t.Fatal(err)
	}
	// A failure after splitting the hold must roll back both the split and outbox.
	if _, err := s.db.Exec(`CREATE TRIGGER fail_checkpoint BEFORE INSERT ON usage_batches BEGIN SELECT RAISE(ABORT,'checkpoint failure'); END`); err != nil {
		t.Fatal(err)
	}
	if err := s.CheckpointRuntimeFundingBlock(ctx, block, "checkpoint", 65536, []byte("known")); err == nil {
		t.Fatal("failed checkpoint committed")
	}
	b, err := s.AuditAuthorizationSource(ctx, "node", "funding")
	if err != nil || b.Reserved != block.Reserved || b.Committed != 0 {
		t.Fatalf("rollback: %+v %v", b, err)
	}
	if _, err := s.db.Exec(`DROP TRIGGER fail_checkpoint`); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if err := s.CheckpointRuntimeFundingBlock(ctx, block, "checkpoint", 65536, []byte("known")); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.CheckpointRuntimeFundingBlock(ctx, block, "checkpoint", 65536, []byte("changed")); !errors.Is(err, ErrUsageBatchConflict) {
		t.Fatalf("mutated retry: %v", err)
	}
	if err := s.CheckpointRuntimeFundingBlock(ctx, block, "stale", 65536, []byte("known")); !errors.Is(err, ErrUsageBatchConflict) {
		t.Fatalf("stale residual: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = Open(path, "agent")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	pending, err := s.UnresolvedUsageReservations(ctx, "node", "new-boot")
	if err != nil || len(pending) != 1 || pending[0].Reserved != (256<<10)-65536 {
		t.Fatalf("residual recovery: %+v %v", pending, err)
	}
	if err := s.SettleUsageReservation(ctx, pending[0], "unknown", pending[0].Reserved, []byte("unknown-tail"), pending[0].Reserved); err != nil {
		t.Fatal(err)
	}
	b, err = s.AuditAuthorizationSource(ctx, "node", "funding")
	if err != nil || b.Reserved != 0 || b.Committed != 65536 || b.Unknown != (256<<10)-65536 || b.Remaining != (1<<20)-(256<<10) {
		t.Fatalf("recovery conservation: %+v %v", b, err)
	}
	// A lost successful response can still replay after recovery settles the tail.
	if err := s.CheckpointRuntimeFundingBlock(ctx, block, "checkpoint", 65536, []byte("known")); err != nil {
		t.Fatal(err)
	}
}

func TestRuntimeFundingBlockCheckpointExhaustion(t *testing.T) {
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
	for _, used := range []uint64{40, 60} {
		id := "partial"
		if used == 60 {
			id = "final"
		}
		if err := s.CheckpointRuntimeFundingBlock(ctx, block, id, used, []byte(id)); err != nil {
			t.Fatal(err)
		}
		block.Reserved -= used
	}
	pending, err := s.UnresolvedUsageReservations(ctx, "node", "new-boot")
	if err != nil || len(pending) != 0 {
		t.Fatalf("exhausted recovery: %+v %v", pending, err)
	}
	if _, err := s.ReserveRuntimeFundingBlock(ctx, runtimeReservation("next", 100), a.Use); err != nil {
		t.Fatal(err)
	}
	b, err := s.AuditAuthorizationSource(ctx, "node", "funding")
	if err != nil || b.Committed != 100 || b.Reserved != 100 || b.Remaining != (1<<20)-200 {
		t.Fatalf("exhaustion: %+v %v", b, err)
	}
}
