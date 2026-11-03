package accounting

import (
	"context"
	"errors"
	"testing"

	engineaccounting "vortex/engine/accounting"
)

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
