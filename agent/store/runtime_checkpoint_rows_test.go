package store

import (
	"bytes"
	"context"
	"fmt"
	"testing"
)

func checkpointRowsFixture(t *testing.T, count int) (*Store, []RuntimeBlockCheckpoint) {
	t.Helper()
	s, scope, _ := runtimeStore(t)
	a, err := s.OpenRuntimeSession(context.Background(), scope, "session", "flow")
	if err != nil {
		t.Fatal(err)
	}
	block := runtimeReservation("block", uint64(count)*100+7)
	if _, err := s.ReserveRuntimeFundingBlock(context.Background(), block, a.Use); err != nil {
		t.Fatal(err)
	}
	entries := make([]RuntimeBlockCheckpoint, count)
	for i := range entries {
		entries[i] = RuntimeBlockCheckpoint{block, fmt.Sprintf("child-%02d", i), 100, []byte{0, byte(i), 255}}
		block.Reserved -= 100
	}
	return s, entries
}

func TestRuntimeCheckpointInsertShapesPreserveEveryChildAndOrderedTail(t *testing.T) {
	for _, count := range []int{2, 9, 17, 33, 64} {
		t.Run(fmt.Sprint(count), func(t *testing.T) {
			s, entries := checkpointRowsFixture(t, count)
			ctx := context.Background()
			for i := 0; i < 2; i++ {
				if err := s.CheckpointRuntimeFundingBlocks(ctx, entries); err != nil {
					t.Fatal(err)
				}
			}
			balance, err := s.AuditAuthorizationSource(ctx, "node", "funding")
			if err != nil || balance.Committed != uint64(count)*100 || balance.Reserved != 7 || balance.Unknown != 0 {
				t.Fatalf("conservation %+v %v", balance, err)
			}
			for _, entry := range entries {
				var payload []byte
				var prior string
				if err := s.db.QueryRow(`SELECT u.payload,c.prior_reserved FROM usage_batches u JOIN runtime_block_checkpoints c ON c.node_id=u.node_id AND c.batch_id=u.batch_id WHERE u.node_id=? AND u.batch_id=?`, entry.Block.NodeID, entry.CheckpointID).Scan(&payload, &prior); err != nil || !bytes.Equal(payload, entry.Payload) || prior != fmt.Sprint(entry.Block.Reserved) {
					t.Fatalf("child %s payload=%x prior=%s err=%v", entry.CheckpointID, payload, prior, err)
				}
			}
			changed := append([]RuntimeBlockCheckpoint(nil), entries...)
			changed[len(changed)-1].Payload = []byte("changed")
			if err := s.CheckpointRuntimeFundingBlocks(ctx, changed); err == nil {
				t.Fatal("changed replay accepted")
			}
		})
	}
}

func TestRuntimeCheckpointInsertFailureRollsBackParentsAndEveryTable(t *testing.T) {
	for _, table := range []string{"usage_reservations", "authorization_usage_holds", "runtime_block_checkpoints", "usage_batches"} {
		for _, failure := range []string{"RAISE(ABORT,'injected full failure')", "RAISE(IGNORE)"} {
			t.Run(table+failure, func(t *testing.T) {
				s, entries := checkpointRowsFixture(t, 9)
				ctx := context.Background()
				if _, err := s.db.Exec(`CREATE TRIGGER fail_child BEFORE INSERT ON ` + table + ` WHEN NEW.batch_id='child-08' BEGIN SELECT ` + failure + `; END`); err != nil {
					t.Fatal(err)
				}
				if err := s.CheckpointRuntimeFundingBlocks(ctx, entries); err == nil {
					t.Fatal("partial set acknowledged")
				}
				balance, err := s.AuditAuthorizationSource(ctx, "node", "funding")
				if err != nil || balance.Committed != 0 || balance.Reserved != 907 {
					t.Fatalf("partial parent %+v %v", balance, err)
				}
				for _, check := range []string{"usage_reservations", "authorization_usage_holds", "runtime_block_checkpoints", "usage_batches", "demand_consumption"} {
					var n int
					query := `SELECT COUNT(*) FROM ` + check
					if check != "demand_consumption" {
						query += ` WHERE batch_id LIKE 'child-%'`
					}
					if err := s.db.QueryRow(query).Scan(&n); err != nil || n != 0 {
						t.Fatalf("partial %s=%d %v", check, n, err)
					}
				}
				if _, err := s.db.Exec(`DROP TRIGGER fail_child`); err != nil {
					t.Fatal(err)
				}
				if err := s.CheckpointRuntimeFundingBlocks(ctx, entries); err != nil {
					t.Fatal(err)
				}
			})
		}
	}
}

func TestRuntimeCheckpointParentIdentityAndUnknownRemainAtomic(t *testing.T) {
	for _, change := range []string{"node", "boot", "snapshot", "flow", "rule", "direction", "authorization", "residual", "unknown"} {
		t.Run(change, func(t *testing.T) {
			s, entries := checkpointRowsFixture(t, 2)
			ctx := context.Background()
			b := &entries[1].Block
			switch change {
			case "node":
				b.NodeID = "other"
			case "boot":
				b.BootID = "other"
			case "snapshot":
				b.SnapshotID = "other"
			case "flow":
				b.FlowKey = "other"
			case "rule":
				b.RuleID = "other"
			case "direction":
				b.Direction = "ingress"
			case "authorization":
				b.AuthorizationID = "other"
			case "residual":
				b.Reserved++
			case "unknown":
				if err := s.PersistUnknownUsage(ctx, "node", unknownPayload(t, entries[1].CheckpointID)); err != nil {
					t.Fatal(err)
				}
			}
			if err := s.CheckpointRuntimeFundingBlocks(ctx, entries); err == nil {
				t.Fatal("invalid identity acknowledged")
			}
			balance, err := s.AuditAuthorizationSource(ctx, "node", "funding")
			if err != nil || balance.Reserved != 207 || balance.Committed != 0 {
				t.Fatalf("partial outcome %+v %v", balance, err)
			}
			var n int
			if err := s.db.QueryRow(`SELECT COUNT(*) FROM usage_batches`).Scan(&n); err != nil || n != 0 {
				t.Fatalf("partial outbox %d %v", n, err)
			}
			if change == "unknown" {
				var state string
				if err := s.db.QueryRow(`SELECT liability_state FROM unknown_usage_batches WHERE batch_id=?`, entries[1].CheckpointID).Scan(&state); err != nil || state != "unresolved" {
					t.Fatalf("unknown changed %s %v", state, err)
				}
			}
		})
	}
}
