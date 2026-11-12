package store

import (
	"context"
	"fmt"
	"testing"
)

func distinctCheckpointParents(t *testing.T, count int, reserved uint64) (*Store, []RuntimeBlockCheckpoint) {
	t.Helper()
	s, scope, _ := runtimeStore(t)
	// Set the fixture's initial slot count before any session or funding exists.
	if _, err := s.db.Exec(`UPDATE authorization_records SET connections=? WHERE authorization_id='share'`, fmt.Sprint(count)); err != nil {
		t.Fatal(err)
	}
	entries := make([]RuntimeBlockCheckpoint, count)
	for i := range entries {
		flow := fmt.Sprintf("flow-%02d", i)
		a, err := s.OpenRuntimeSession(context.Background(), scope, fmt.Sprintf("session-%02d", i), flow)
		if err != nil {
			t.Fatal(err)
		}
		block := runtimeReservation(fmt.Sprintf("block-%02d", i), reserved)
		block.FlowKey = flow
		if _, err := s.ReserveRuntimeFundingBlock(context.Background(), block, a.Use); err != nil {
			t.Fatal(err)
		}
		entries[i] = RuntimeBlockCheckpoint{block, fmt.Sprintf("child-%02d", i), 100, []byte{0, byte(i), 255}}
	}
	return s, entries
}

func TestRuntimeCheckpointMissingParentUpdateRollsBackEveryChild(t *testing.T) {
	for _, count := range []int{1, 3} {
		for _, table := range []string{"usage_reservations", "authorization_usage_holds"} {
			t.Run(fmt.Sprintf("%d/%s", count, table), func(t *testing.T) {
				s, entries := distinctCheckpointParents(t, count, 107)
				ctx := context.Background()
				if _, err := s.db.Exec(`CREATE TRIGGER ignore_parent BEFORE UPDATE ON ` + table + ` WHEN OLD.batch_id='block-00' BEGIN SELECT RAISE(IGNORE); END`); err != nil {
					t.Fatal(err)
				}
				if err := s.CheckpointRuntimeFundingBlocks(ctx, entries); err == nil {
					t.Fatal("missing parent update acknowledged")
				}
				balance, err := s.AuditAuthorizationSource(ctx, "node", "funding")
				if err != nil || balance.Committed != 0 || balance.Reserved != uint64(count)*107 || balance.Unknown != 0 {
					t.Fatalf("partial parent/child conservation %+v %v", balance, err)
				}
				var n int
				if err := s.db.QueryRow(`SELECT COUNT(*) FROM usage_batches`).Scan(&n); err != nil || n != 0 {
					t.Fatalf("partial outbox %d %v", n, err)
				}
			})
		}
	}
}
