package agent

import (
	"fmt"
	"testing"
)

func TestControlReplayGuardBoundsMemoryWithoutForgettingIDs(t *testing.T) {
	var guard controlReplayGuard
	id := func(i int) string { return fmt.Sprintf("550e8400-e29b-41d4-a716-%012x", i) }
	for i := 1; i <= maxControlMessageIDs; i++ {
		if err := guard.accept(uint64(i), id(i)); err != nil {
			t.Fatal(err)
		}
	}
	if len(guard.ids) != maxControlMessageIDs || guard.last != maxControlMessageIDs {
		t.Fatalf("unexpected bound: count=%d last=%d", len(guard.ids), guard.last)
	}
	// Oldest ID remains a duplicate even with a new valid sequence.
	if err := guard.accept(maxControlMessageIDs+1, id(1)); err == nil {
		t.Fatal("forgot oldest replay identity")
	}
	for i := maxControlMessageIDs + 1; i <= maxControlMessageIDs+100; i++ {
		if err := guard.accept(uint64(i), id(i)); err == nil {
			t.Fatal("accepted beyond memory bound")
		}
	}
	if len(guard.ids) != maxControlMessageIDs || guard.last != maxControlMessageIDs {
		t.Fatal("failed messages mutated replay state")
	}
	// A new connection has its own sequence/ID space. Durable business request
	// deduplication is separate and is exercised by the TLS demand replay tests.
	var next controlReplayGuard
	if err := next.accept(1, id(1)); err != nil {
		t.Fatal(err)
	}
}

func TestControlReplayGuardRejectsInvalidSequenceBeforeMutation(t *testing.T) {
	const first = "550e8400-e29b-41d4-a716-446655440001"
	const second = "550e8400-e29b-41d4-a716-446655440002"
	var guard controlReplayGuard
	if err := guard.accept(10, first); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		seq uint64
		id  string
	}{{0, second}, {9, second}, {10, second}, {11, "bad-id"}, {11, first}} {
		if err := guard.accept(c.seq, c.id); err == nil {
			t.Fatalf("accepted invalid identity: %+v", c)
		}
		if guard.last != 10 || len(guard.ids) != 1 {
			t.Fatal("invalid message poisoned valid sequence or IDs")
		}
	}
	if err := guard.accept(11, second); err != nil {
		t.Fatal(err)
	}
}
