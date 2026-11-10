package agent

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"vortex/agent/store"
	controlv1 "vortex/contracts/protobuf/v1"
)

type recordingDemandStream struct {
	controlv1.NodeControl_ConnectClient
	messages []*controlv1.NodeMessage
}

func (s *recordingDemandStream) Send(m *controlv1.NodeMessage) error {
	s.messages = append(s.messages, m)
	return nil
}

// Missing-authority retries wake the controller every 10ms. A slow response
// must not turn those wakes into a queue of identical TLS requests and FULL
// sequence commits ahead of the response that supplies the missing authority.
func TestDemandWakeDoesNotFloodOutstandingRequest(t *testing.T) {
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
	scope := store.DemandScope{HubID: "550e8400-e29b-41d4-a716-446655440101", DelegationID: "550e8400-e29b-41d4-a716-446655440102", NodeID: n.NodeID, BootID: n.BootID, SourceID: "550e8400-e29b-41d4-a716-446655440103", SnapshotID: "550e8400-e29b-41d4-a716-446655440104", RuleID: "rule", SignerEpoch: 1, RuleRevision: 1, PathGeneration: 1}
	r, err := db.PrepareDemand(ctx, scope, 4096, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	other := scope
	other.RuleRevision++
	for _, tc := range []struct {
		scope store.DemandScope
		want  bool
	}{{scope, true}, {other, false}} {
		if got, err := db.HasPendingDemand(ctx, tc.scope); err != nil || got != tc.want {
			t.Fatalf("scoped pending=%t want=%t err=%v", got, tc.want, err)
		}
	}
	c := &ControlClient{Node: n, EnableCreditDemand: true}
	stream := &recordingDemandStream{}
	var delivery demandDeliveryState
	var sequence uint64
	for i := 0; i < 8; i++ {
		delivery.cursor = "" // the actual control-loop wake path restarts the page
		if err := c.sendPendingDemands(ctx, stream, scope.HubID, &delivery, &sequence); err != nil {
			t.Fatal(err)
		}
	}
	if len(stream.messages) != 1 {
		t.Fatalf("outstanding request sent %d times before retry interval", len(stream.messages))
	}
	pending, err := db.PendingDemands(ctx, n.NodeID, n.BootID, scope.HubID, "")
	if err != nil || len(pending) != 1 || pending[0].ID != r.ID {
		t.Fatalf("wake retired durable request: %+v %v", pending, err)
	}
	// A bounded retry reuses the durable ID. A new TLS session may replay
	// immediately without inheriting the old connection's throttle.
	delivery.sent[r.ID] = time.Now().Add(-demandResponseRetry)
	delivery.cursor = ""
	if err := c.sendPendingDemands(ctx, stream, scope.HubID, &delivery, &sequence); err != nil {
		t.Fatal(err)
	}
	if err := c.sendPendingDemands(ctx, stream, scope.HubID, &demandDeliveryState{}, &sequence); err != nil {
		t.Fatal(err)
	}
	if len(stream.messages) != 3 {
		t.Fatalf("retry/reconnect sends=%d", len(stream.messages))
	}
	for _, m := range stream.messages {
		if m.GetCreditDemand().GetRequestId() != r.ID {
			t.Fatal("retry changed durable identity")
		}
	}
}
