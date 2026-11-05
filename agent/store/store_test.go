package store

import (
	"context"
	"path/filepath"
	"testing"
)

func TestDurableEventSurvivesReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "agent.db")
	ctx := context.Background()
	s, err := Open(path, "agent")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.AppendEvent(ctx, "evt-1", "hello", []byte("payload")); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = Open(path, "agent")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	n, err := s.EventCount(ctx)
	if err != nil || n != 1 {
		t.Fatalf("count=%d err=%v", n, err)
	}
	i, err := s.Info(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if i.JournalMode != "wal" || i.Synchronous != "2" {
		t.Fatalf("unexpected sqlite settings: %+v", i)
	}
}

func TestReceiptIsIdempotent(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "hub.db"), "hub")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := s.RecordReceipt(context.Background(), "m-1", 1); err != nil {
		t.Fatal(err)
	}
	if err := s.RecordReceipt(context.Background(), "m-1", 1); err != nil {
		t.Fatal(err)
	}
}
