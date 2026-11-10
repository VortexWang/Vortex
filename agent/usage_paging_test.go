package agent

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"testing"

	"vortex/agent/store"
	"vortex/contracts"
	controlv1 "vortex/contracts/protobuf/v1"
)

func TestUsagePagesRetainUnacknowledgedRowsAndReplay(t *testing.T) {
	ctx := context.Background()
	db, err := store.Open(filepath.Join(t.TempDir(), "agent.db"), "agent")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	n, err := New(controlTestNodeID, db, nil)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 70; i++ {
		batch := &controlv1.UsageBatch{BatchId: fmt.Sprintf("batch-%03d", i), BillingSnapshotId: "snapshot", Deltas: []*controlv1.UsageDelta{{RuleId: "rule", Role: "source", Direction: "egress", Bytes: 1, GrantId: "grant"}}}
		if err := n.PersistUsageBatch(ctx, batch, 0); err != nil {
			t.Fatal(err)
		}
	}
	c := &ControlClient{Node: n}
	stream := &capturedUsageStream{}
	var sequence uint64
	var cursor string
	for _, want := range []int{32, 64, 70, 70} {
		if err := c.sendPendingUsage(ctx, stream, &sequence, &cursor); err != nil {
			t.Fatal(err)
		}
		if len(stream.sent) != want {
			t.Fatalf("page sent %d total, want %d", len(stream.sent), want)
		}
	}
	if cursor != "" {
		t.Fatal("end of scan did not wrap")
	}
	for i, message := range stream.sent {
		if message.GetUsageBatch().GetBatchId() != fmt.Sprintf("batch-%03d", i) || message.Sequence != uint64(i+1) {
			t.Fatal("page skipped or reordered a durable identity")
		}
	}
	rows, err := db.PendingUsageOutbox(ctx, n.NodeID)
	if err != nil || len(rows) != 70 {
		t.Fatalf("dispatch acted as ACK: %d %v", len(rows), err)
	}
	// A reconnect has no cursor and must replay even the already dispatched page.
	reconnected := &capturedUsageStream{}
	if err := (&ControlClient{Node: n}).sendPendingUsage(ctx, reconnected, &sequence); err != nil {
		t.Fatal(err)
	}
	if len(reconnected.sent) != 32 || reconnected.sent[0].GetUsageBatch().GetBatchId() != "batch-000" {
		t.Fatal("reconnect lost unacknowledged page")
	}
	if err := db.AcknowledgeUsageBatches(ctx, n.NodeID, []string{"batch-000"}); err != nil {
		t.Fatal(err)
	}
	stream.sent = nil
	if err := c.sendPendingUsage(ctx, stream, &sequence, &cursor); err != nil {
		t.Fatal(err)
	}
	if len(stream.sent) != 32 || stream.sent[0].GetUsageBatch().GetBatchId() != "batch-001" {
		t.Fatal("wrap did not honor actual durable ACK")
	}
}

func TestUsageDeliveryPagesAreBoundedFairAndAppendOrdered(t *testing.T) {
	ctx := context.Background()
	db, err := store.Open(filepath.Join(t.TempDir(), "agent.db"), "agent")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	n, err := New(controlTestNodeID, db, nil)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, grant := "550e8400-e29b-41d4-a716-446655440024", "550e8400-e29b-41d4-a716-446655440021"
	putKnown := func(id string, bytes uint64) error {
		return n.PersistUsageBatch(ctx, &controlv1.UsageBatch{BatchId: id, BillingSnapshotId: snapshot, Deltas: []*controlv1.UsageDelta{{RuleId: "rule", Role: "source", Direction: "egress", Bytes: bytes, GrantId: grant}}}, 0)
	}
	for i := 0; i < 70; i++ {
		// IDs deliberately sort opposite to insertion order.
		if err := putKnown(fmt.Sprintf("known-%03d", 70-i), 1); err != nil {
			t.Fatal(err)
		}
		payload, err := contracts.EncodeUnknownUsage(&controlv1.UnknownUsageBatch{BatchId: fmt.Sprintf("unknown-%03d", 70-i), OriginBootId: n.BootID, BillingSnapshotId: snapshot, GrantId: grant, RuleId: "rule", Role: "source", Direction: "egress", UnknownUpperBound: 3})
		if err != nil {
			t.Fatal(err)
		}
		if err := db.PersistUnknownUsage(ctx, n.NodeID, payload); err != nil {
			t.Fatal(err)
		}
	}
	c := &ControlClient{Node: n}
	stream := &capturedUsageStream{}
	var sequence uint64
	var cursor usageDeliveryCursor
	seen := map[string]bool{}
	for turn := 0; ; turn++ {
		before := len(stream.sent)
		more, err := c.sendUsageDeliveryPage(ctx, stream, &sequence, &cursor)
		if err != nil {
			t.Fatal(err)
		}
		if sent := len(stream.sent) - before; sent > 32 {
			t.Fatalf("turn sent %d records", sent)
		}
		for _, message := range stream.sent[before:] {
			var id string
			if b := message.GetUsageBatch(); b != nil {
				if turn%2 != 0 {
					t.Fatal("known page starved typed unknown")
				}
				id = b.BatchId
			} else if b := message.GetUnknownUsageBatch(); b != nil {
				if turn%2 != 1 || b.UnknownUpperBound != 3 {
					t.Fatal("unknown page lost type, bound or fairness")
				}
				id = b.BatchId
			} else {
				t.Fatal("unexpected delivery type")
			}
			if seen[id] {
				t.Fatal("unacknowledged row repeated within scan")
			}
			seen[id] = true
		}
		if !more {
			break
		}
		if turn > 8 {
			t.Fatal("scan did not terminate")
		}
	}
	if len(seen) != 140 || stream.sent[0].GetUsageBatch().BatchId != "known-070" || stream.sent[32].GetUnknownUsageBatch().BatchId != "unknown-070" {
		t.Fatal("scan lost records or used random-ID ordering")
	}
	if err := putKnown("aaa-new-lower-id", 7); err != nil {
		t.Fatal(err)
	}
	cursor.resume()
	before := len(stream.sent)
	for turns := 0; ; turns++ {
		more, err := c.sendUsageDeliveryPage(ctx, stream, &sequence, &cursor)
		if err != nil {
			t.Fatal(err)
		}
		if !more {
			break
		}
		if turns > 2 {
			t.Fatal("append hint created replay loop")
		}
	}
	if len(stream.sent) != before+1 || stream.sent[before].GetUsageBatch().BatchId != "aaa-new-lower-id" {
		t.Fatal("hint repeated in-flight rows or lost lower random ID")
	}
	known, err := db.PendingUsageOutbox(ctx, n.NodeID)
	if err != nil || len(known) != 71 {
		t.Fatalf("send became ordinary ACK: %d %v", len(known), err)
	}
	unknown, err := db.PendingUnknownUsage(ctx, n.NodeID)
	if err != nil || len(unknown) != 70 || unknown[0].LiabilityState != "unresolved" {
		t.Fatalf("send changed unknown liability: %d %v", len(unknown), err)
	}
	if err := db.AcknowledgeUsageBatches(ctx, n.NodeID, []string{"known-070"}); err != nil {
		t.Fatal(err)
	}
	if err := db.AcknowledgeUnknownUsage(ctx, n.NodeID, unknown[0].BatchID, unknown[0].Digest); err != nil {
		t.Fatal(err)
	}
	// A timer/reconnect replays every still-unacknowledged identity from zero.
	cursor.reset()
	replay := &capturedUsageStream{}
	for {
		more, err := c.sendUsageDeliveryPage(ctx, replay, &sequence, &cursor)
		if err != nil {
			t.Fatal(err)
		}
		if !more {
			break
		}
	}
	if len(replay.sent) != 139 {
		t.Fatalf("replay did not honor durable ACKs: %d", len(replay.sent))
	}
}

func TestUsageWakeBroadcastFollowsCommitAndDoesNotAcknowledge(t *testing.T) {
	ctx := context.Background()
	db, err := store.Open(filepath.Join(t.TempDir(), "agent.db"), "agent")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	n, err := New(controlTestNodeID, db, nil)
	if err != nil {
		t.Fatal(err)
	}
	first, cancelFirst, err := n.subscribeUsage()
	if err != nil {
		t.Fatal(err)
	}
	defer cancelFirst()
	second, cancelSecond, err := n.subscribeUsage()
	if err != nil {
		t.Fatal(err)
	}
	defer cancelSecond()
	batch := &controlv1.UsageBatch{BatchId: "batch", BillingSnapshotId: "snapshot", Deltas: []*controlv1.UsageDelta{{RuleId: "rule", Role: "source", Direction: "egress", Bytes: 1}}}
	if err := n.PersistUsageBatch(ctx, batch, 0); err != nil {
		t.Fatal(err)
	}
	for _, wake := range []<-chan struct{}{first, second} {
		select {
		case <-wake:
		default:
			t.Fatal("one control owner consumed another's hint")
		}
	}
	rows, err := db.PendingUsageOutbox(ctx, n.NodeID)
	if err != nil || len(rows) != 1 {
		t.Fatal("hint preceded durable visibility or became ACK")
	}
	cancelFirst()
	batch.Deltas[0].Bytes = 2
	if err := n.PersistUsageBatch(ctx, batch, 0); !errors.Is(err, store.ErrUsageBatchConflict) {
		t.Fatalf("changed replay accepted: %v", err)
	}
	select {
	case <-second:
		t.Fatal("failed transaction broadcast usage")
	default:
	}
	batch.BatchId = "new-batch"
	if err := n.PersistUsageBatch(ctx, batch, 0); err != nil {
		t.Fatal(err)
	}
	select {
	case <-first:
		t.Fatal("unsubscribed owner received new hint")
	default:
	}
	select {
	case <-second:
	default:
		t.Fatal("remaining owner lost hint")
	}
}
