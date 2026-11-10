package agent

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"
	"vortex/agent/store"
	"vortex/contracts"
	controlv1 "vortex/contracts/protobuf/v1"
)

func TestUsageWindowBoundsUnackedReportsAndReplaysDurableRows(t *testing.T) {
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
	for i := 0; i < 20; i++ {
		if err := n.PersistUsageBatch(ctx, &controlv1.UsageBatch{BatchId: fmt.Sprintf("known-%02d", i), BillingSnapshotId: snapshot, Deltas: []*controlv1.UsageDelta{{RuleId: "rule", Role: "source", Direction: "egress", Bytes: 1, GrantId: grant}}}, 0); err != nil {
			t.Fatal(err)
		}
	}
	payload, err := contracts.EncodeUnknownUsage(&controlv1.UnknownUsageBatch{BatchId: "unknown", OriginBootId: n.BootID, BillingSnapshotId: snapshot, GrantId: grant, RuleId: "rule", Role: "source", Direction: "egress", UnknownUpperBound: 7})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.PersistUnknownUsage(ctx, n.NodeID, payload); err != nil {
		t.Fatal(err)
	}
	c := &ControlClient{Node: n}
	stream := &capturedUsageStream{}
	window := &usageDeliveryWindow{limit: 4}
	cursor := usageDeliveryCursor{window: window}
	var sequence uint64
	more, err := c.sendUsageDeliveryPage(ctx, stream, &sequence, &cursor)
	if err != nil || more || len(stream.sent) != 4 || window.remaining() != 0 {
		t.Fatalf("initial window: %d %t %v", len(stream.sent), more, err)
	}
	if _, err := c.sendUsageDeliveryPage(ctx, stream, &sequence, &cursor); err != nil || len(stream.sent) != 4 {
		t.Fatal("full window dispatched more reports")
	}
	if err := c.applyOrdinaryUsageAcknowledgements(ctx, []*controlv1.ControlMessage{ordinaryAck("known-00", "known-01")}); err != nil {
		t.Fatal(err)
	}
	window.confirm(false, "known-00", "known-01")
	if _, err := c.sendUsageDeliveryPage(ctx, stream, &sequence, &cursor); err != nil {
		t.Fatal(err)
	}
	if len(stream.sent) != 5 || stream.sent[4].GetUnknownUsageBatch().GetBatchId() != "unknown" {
		t.Fatal("typed unknown lost fairness after ordinary ACK")
	}
	unknown, err := db.PendingUnknownUsage(ctx, n.NodeID)
	if err != nil || len(unknown) != 1 || unknown[0].Upper != 7 {
		t.Fatalf("dispatch released unknown liability: %+v %v", unknown, err)
	}
	rows, err := db.PendingUsageOutbox(ctx, n.NodeID)
	if err != nil || len(rows) != 18 {
		t.Fatalf("window acted as durable ACK: %d %v", len(rows), err)
	}
	// A periodic retry clears only transport hints, including an unacknowledged
	// unknown report. Retained ordinary rows are replayed from their first ID.
	cursor.reset()
	stream.sent = nil
	if _, err := c.sendUsageDeliveryPage(ctx, stream, &sequence, &cursor); err != nil {
		t.Fatal(err)
	}
	if len(stream.sent) != 4 || stream.sent[0].GetUsageBatch().GetBatchId() != "known-02" {
		t.Fatal("retry lost unacknowledged prefix")
	}
	fresh := usageDeliveryCursor{window: &usageDeliveryWindow{limit: 4}}
	stream.sent = nil
	if _, err := c.sendUsageDeliveryPage(ctx, stream, &sequence, &fresh); err != nil {
		t.Fatal(err)
	}
	if len(stream.sent) != 4 || stream.sent[0].GetUsageBatch().GetBatchId() != "known-02" {
		t.Fatal("reconnect lost durable rows")
	}
}

func TestUsageWindowSeparatesKnownAndUnknownIdentities(t *testing.T) {
	w := &usageDeliveryWindow{limit: 2}
	if !w.claim(false, "same") || !w.claim(true, "same") || w.claim(false, "other") {
		t.Fatal("window exceeded limit")
	}
	if !w.confirm(false, "same") || w.remaining() != 1 {
		t.Fatal("ordinary ACK released typed unknown slot")
	}
	if w.confirm(false, "same") || w.remaining() != 1 {
		t.Fatal("duplicate ACK released another slot")
	}
	if !w.claim(false, "new") {
		t.Fatal("confirmed slot not reusable")
	}
	if !w.confirm(true, "same") || w.remaining() != 1 {
		t.Fatal("typed unknown slot missing")
	}
}
