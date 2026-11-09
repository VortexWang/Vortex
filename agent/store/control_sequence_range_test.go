package store

import (
	"context"
	"path/filepath"
	"testing"
)

func TestControlSequenceRangeSurvivesUnsentSuffixAndOverflow(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "agent.db")
	s, err := Open(path, "agent")
	if err != nil {
		t.Fatal(err)
	}
	first, err := s.NextControlSequenceRange(ctx, 32)
	if err != nil || first != 1 {
		t.Fatalf("first range: %d %v", first, err)
	}
	// Simulate a crash after only part of the range reached the peer.
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = Open(path, "agent")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	next, err := s.NextControlSequence(ctx)
	if err != nil || next != 33 {
		t.Fatalf("reused unsent sequence after restart: %d %v", next, err)
	}
	for _, count := range []uint64{0, 33, ^uint64(0)} {
		if _, err := s.NextControlSequenceRange(ctx, count); err == nil {
			t.Fatalf("accepted invalid range %d", count)
		}
	}
	if _, err := s.db.ExecContext(ctx, `UPDATE agent_state SET value=? WHERE key='control_sequence'`, "9223372036854775805"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.NextControlSequenceRange(ctx, 3); err == nil {
		t.Fatal("range wrapped past MaxInt64")
	}
	first, err = s.NextControlSequenceRange(ctx, 2)
	if err != nil || first != uint64(1<<63-2) {
		t.Fatalf("rejected range consumed sequence: %d %v", first, err)
	}
	if _, err := s.NextControlSequence(ctx); err == nil {
		t.Fatal("exhausted counter wrapped")
	}
}
