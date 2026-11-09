package agent

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"testing"

	"vortex/agent/store"
	controlv1 "vortex/contracts/protobuf/v1"
)

func usageAckGroupFixture(t *testing.T) (*Node, *sql.Conn) {
	t.Helper()
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "agent.db")
	db, err := store.Open(path, "agent")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	n, err := New(controlTestNodeID, db, nil)
	if err != nil {
		t.Fatal(err)
	}
	reader, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = reader.Close() })
	conn, err := reader.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return n, conn
}

func persistAckGroupBatch(t *testing.T, n *Node, id string, unknown uint64) {
	t.Helper()
	batch := &controlv1.UsageBatch{BatchId: id, BillingSnapshotId: "550e8400-e29b-41d4-a716-446655440024", Deltas: []*controlv1.UsageDelta{{RuleId: "rule", GrantId: "550e8400-e29b-41d4-a716-446655440021", Role: "source", Direction: "egress", Bytes: 1}}}
	if err := n.PersistUsageBatch(context.Background(), batch, unknown); err != nil {
		t.Fatal(err)
	}
}

func ordinaryAck(ids ...string) *controlv1.ControlMessage {
	return &controlv1.ControlMessage{Body: &controlv1.ControlMessage_UsageAck{UsageAck: &controlv1.UsageAcknowledgement{BatchIds: ids}}}
}

func ackDataVersion(t *testing.T, conn *sql.Conn) int64 {
	t.Helper()
	var version int64
	if err := conn.QueryRowContext(context.Background(), "PRAGMA data_version").Scan(&version); err != nil {
		t.Fatal(err)
	}
	return version
}

func TestOrdinaryUsageAcknowledgementGroupCommitsOnceBeforeHooks(t *testing.T) {
	n, observer := usageAckGroupFixture(t)
	ctx := context.Background()
	for _, id := range []string{"first", "second", "third"} {
		persistAckGroupBatch(t, n, id, 0)
	}
	before := ackDataVersion(t, observer)
	var hooks [][]string
	c := &ControlClient{Node: n, AcknowledgeUsage: func(ctx context.Context, ids []string) error {
		// The first callback must see every member durably committed from an
		// independent connection, and exactly one external write transaction.
		var remaining int
		if err := observer.QueryRowContext(ctx, "SELECT COUNT(*) FROM usage_batches WHERE state!='hub_acked'").Scan(&remaining); err != nil {
			return err
		}
		if remaining != 0 || ackDataVersion(t, observer) != before+1 {
			return fmt.Errorf("callback preceded whole-group commit: pending=%d", remaining)
		}
		hooks = append(hooks, append([]string(nil), ids...))
		return nil
	}}
	if err := c.applyOrdinaryUsageAcknowledgements(ctx, []*controlv1.ControlMessage{ordinaryAck("first"), ordinaryAck("second", "third")}); err != nil {
		t.Fatal(err)
	}
	if len(hooks) != 2 || len(hooks[0]) != 1 || hooks[0][0] != "first" || len(hooks[1]) != 2 || hooks[1][0] != "second" || hooks[1][1] != "third" {
		t.Fatalf("callback envelope ordering changed: %v", hooks)
	}
}

func TestOrdinaryUsageAcknowledgementGroupRejectsUnknownAtomically(t *testing.T) {
	n, observer := usageAckGroupFixture(t)
	ctx := context.Background()
	persistAckGroupBatch(t, n, "known", 0)
	persistAckGroupBatch(t, n, "unknown", 1)
	before := ackDataVersion(t, observer)
	hooks := 0
	c := &ControlClient{Node: n, AcknowledgeUsage: func(context.Context, []string) error { hooks++; return nil }}
	if err := c.applyOrdinaryUsageAcknowledgements(ctx, []*controlv1.ControlMessage{ordinaryAck("known"), ordinaryAck("unknown")}); err == nil {
		t.Fatal("ordinary ACK accepted unknown upper bound")
	}
	if hooks != 0 || ackDataVersion(t, observer) != before {
		t.Fatal("rejected group committed its known prefix or invoked hook")
	}
	rows, err := n.Store.PendingUsageOutbox(ctx, n.NodeID)
	if err != nil || len(rows) != 2 {
		t.Fatalf("rejected group lost pending rows: %v %v", rows, err)
	}
	var upper, state string
	if err := observer.QueryRowContext(ctx, "SELECT unknown_upper_bound,state FROM usage_batches WHERE batch_id='unknown'").Scan(&upper, &state); err != nil || upper != "1" || state != "pending" {
		t.Fatalf("unknown liability changed: %s %s %v", upper, state, err)
	}
}

func TestOrdinaryUsageAcknowledgementGroupBoundsAndConfigurationOnly(t *testing.T) {
	ctx := context.Background()
	for _, scenario := range []string{"empty", "over-limit", "nil-message", "empty-ids", "typed-unknown-message", "configuration-only"} {
		t.Run(scenario, func(t *testing.T) {
			n, observer := usageAckGroupFixture(t)
			persistAckGroupBatch(t, n, "batch", 0)
			before := ackDataVersion(t, observer)
			hooks := 0
			c := &ControlClient{Node: n, AcknowledgeUsage: func(context.Context, []string) error { hooks++; return nil }}
			messages := []*controlv1.ControlMessage{ordinaryAck("batch")}
			switch scenario {
			case "empty":
				messages = nil
			case "over-limit":
				for len(messages) < 33 {
					messages = append(messages, ordinaryAck("batch"))
				}
			case "nil-message":
				messages = append(messages, nil)
			case "empty-ids":
				messages = append(messages, ordinaryAck())
			case "typed-unknown-message":
				messages = append(messages, &controlv1.ControlMessage{Body: &controlv1.ControlMessage_UnknownUsageAck{UnknownUsageAck: &controlv1.UnknownUsageAcknowledgement{BatchId: "batch"}}})
			case "configuration-only":
				c.ControlOnly = true
			}
			if err := c.applyOrdinaryUsageAcknowledgements(ctx, messages); err == nil {
				t.Fatal("accepted invalid ordinary ACK group")
			}
			if hooks != 0 || ackDataVersion(t, observer) != before {
				t.Fatal("invalid group committed or invoked hook")
			}
		})
	}
	t.Run("exactly-32", func(t *testing.T) {
		n, observer := usageAckGroupFixture(t)
		var messages []*controlv1.ControlMessage
		for i := 0; i < 32; i++ {
			id := fmt.Sprintf("batch-%d", i)
			persistAckGroupBatch(t, n, id, 0)
			messages = append(messages, ordinaryAck(id))
		}
		before := ackDataVersion(t, observer)
		if err := (&ControlClient{Node: n}).applyOrdinaryUsageAcknowledgements(ctx, messages); err != nil {
			t.Fatal(err)
		}
		if ackDataVersion(t, observer) != before+1 {
			t.Fatal("bounded ACK group did not commit together")
		}
	})
}

func TestOrdinaryUsageAcknowledgementGroupHookFailureRetainsDurableAck(t *testing.T) {
	n, _ := usageAckGroupFixture(t)
	ctx := context.Background()
	persistAckGroupBatch(t, n, "batch", 0)
	failure := errors.New("hook unavailable")
	c := &ControlClient{Node: n, AcknowledgeUsage: func(context.Context, []string) error { return failure }}
	if err := c.applyOrdinaryUsageAcknowledgements(ctx, []*controlv1.ControlMessage{ordinaryAck("batch")}); !errors.Is(err, failure) {
		t.Fatalf("hook failure lost: %v", err)
	}
	rows, err := n.Store.PendingUsageOutbox(ctx, n.NodeID)
	if err != nil || len(rows) != 0 {
		t.Fatalf("post-commit hook error undid durable ACK: %v %v", rows, err)
	}
}
