package accounting

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"testing"

	engineaccounting "vortex/engine/accounting"
)

func TestEventChunksPreserveOrderSnapshotAndCapacity(t *testing.T) {
	ctx := context.Background()
	limit := 2*eventChunkSize + 3
	gate, err := NewGate(limit)
	if err != nil {
		t.Fatal(err)
	}
	session, err := gate.Open(ctx, engineaccounting.Flow{Key: "chunked"})
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < limit; i++ {
		batch, err := session.Reserve(ctx, engineaccounting.LeftToRight, int64(i))
		if err != nil {
			t.Fatal(err)
		}
		if err := batch.Commit(ctx, int64(i)); err != nil {
			t.Fatal(err)
		}
	}
	snapshot := gate.Events()
	snapshot[0].Used = -1
	var output bytes.Buffer
	if err := gate.WriteJSONL(&output); err != nil {
		t.Fatal(err)
	}
	decoder := json.NewDecoder(&output)
	for i := 0; i < limit; i++ {
		var event Event
		if err := decoder.Decode(&event); err != nil {
			t.Fatal(err)
		}
		if event.BatchID != uint64(i+1) || event.Used != int64(i) {
			t.Fatalf("event %d: %+v", i, event)
		}
	}
	var extra Event
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		t.Fatalf("unexpected extra event: %v", err)
	}
	batch, err := session.Reserve(ctx, engineaccounting.LeftToRight, 1)
	if err != nil {
		t.Fatal(err)
	}
	if err := batch.Commit(ctx, 1); err == nil {
		t.Fatal("event capacity was not enforced")
	}
	if len(gate.Events()) != limit {
		t.Fatal("failed commit changed event count")
	}
}

func TestUnknownOutcomeRetainsReservation(t *testing.T) {
	gate, err := NewGate(4)
	if err != nil {
		t.Fatal(err)
	}
	session, err := gate.Open(context.Background(), engineaccounting.Flow{Key: "flow-1"})
	if err != nil {
		t.Fatal(err)
	}
	batch, err := session.Reserve(context.Background(), engineaccounting.LeftToRight, 128)
	if err != nil {
		t.Fatal(err)
	}
	if err := batch.MarkUnknown(context.Background()); err != nil {
		t.Fatal(err)
	}
	events := gate.Events()
	if len(events) != 1 || events[0].Outcome != engineaccounting.Unknown || events[0].Reserved != 128 {
		t.Fatalf("unexpected events: %+v", events)
	}
	if err := batch.Return(context.Background()); !errors.Is(err, engineaccounting.ErrAlreadyResolved) {
		t.Fatalf("expected idempotence error, got %v", err)
	}
}
