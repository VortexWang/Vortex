package store

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"
)

func TestUsageAcknowledgementChunksRemainAtomic(t *testing.T) {
	for _, fault := range []string{"missing", "unknown", "invalid-state", "write-failure", "ignored-write"} {
		t.Run(fault, func(t *testing.T) {
			ctx := context.Background()
			s, err := Open(filepath.Join(t.TempDir(), "agent.db"), "agent")
			if err != nil {
				t.Fatal(err)
			}
			defer s.Close()
			var ids []string
			for i := 0; i < 70; i++ {
				id := fmt.Sprintf("batch-%03d", i)
				if i == 65 {
					id = `batch-quoted-'"[x]`
				}
				ids = append(ids, id)
				if err := s.PersistUsageOutbox(ctx, UsageBatch{NodeID: "node", BootID: "boot", BatchID: id, SnapshotID: "snapshot", Payload: []byte(id)}); err != nil {
					t.Fatal(err)
				}
			}
			switch fault {
			case "missing":
				ids[65] = "absent"
			case "unknown":
				_, err = s.db.Exec(`UPDATE usage_batches SET unknown_upper_bound='1' WHERE batch_id=?`, ids[65])
			case "invalid-state":
				_, err = s.db.Exec(`UPDATE usage_batches SET state='ledgered' WHERE batch_id=?`, ids[65])
			case "write-failure":
				_, err = s.db.Exec(`CREATE TRIGGER fail_ack BEFORE UPDATE ON usage_batches WHEN NEW.batch_id='batch-069' BEGIN SELECT RAISE(ABORT,'disk'); END`)
			case "ignored-write":
				_, err = s.db.Exec(`CREATE TRIGGER ignore_ack BEFORE UPDATE ON usage_batches WHEN NEW.batch_id='batch-069' BEGIN SELECT RAISE(IGNORE); END`)
			}
			if err != nil {
				t.Fatal(err)
			}
			if err := s.AcknowledgeUsageBatches(ctx, "node", ids); err == nil {
				t.Fatal("invalid group acknowledged")
			}
			var count int
			if err := s.db.QueryRow(`SELECT count(*) FROM usage_batches WHERE state='hub_acked'`).Scan(&count); err != nil || count != 0 {
				t.Fatalf("committed prefix=%d err=%v", count, err)
			}
		})
	}
}

func TestUsageAcknowledgementChunksReplayAndTenantScope(t *testing.T) {
	ctx := context.Background()
	s, err := Open(filepath.Join(t.TempDir(), "agent.db"), "agent")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	var ids []string
	for i := 0; i < 70; i++ {
		id := fmt.Sprintf("batch-%d", i)
		if i == 65 {
			id = `batch-quoted-'"[x]`
		}
		ids = append(ids, id, id)
		for _, node := range []string{"node", "other"} {
			if err := s.PersistUsageOutbox(ctx, UsageBatch{NodeID: node, BootID: "boot", BatchID: id, SnapshotID: "snapshot", Payload: []byte(id)}); err != nil {
				t.Fatal(err)
			}
		}
	}
	for range 2 {
		if err := s.MarkUsageLocallyAcknowledged(ctx, "node", ids); err != nil {
			t.Fatal(err)
		}
	}
	for range 2 {
		if err := s.AcknowledgeUsageBatches(ctx, "node", ids); err != nil {
			t.Fatal(err)
		}
	}
	var count int
	if err := s.db.QueryRow(`SELECT count(*) FROM usage_batches WHERE node_id='node' AND state='hub_acked'`).Scan(&count); err != nil || count != 70 {
		t.Fatalf("ACK=%d err=%v", count, err)
	}
	if err := s.db.QueryRow(`SELECT count(*) FROM usage_batches WHERE node_id='other' AND state='pending'`).Scan(&count); err != nil || count != 70 {
		t.Fatalf("other pending=%d err=%v", count, err)
	}
}
