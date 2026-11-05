package store

import (
	"context"
	"path/filepath"
	"testing"
	"time"
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

func TestControlSequenceAndStateSurviveReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "agent.db")
	s, err := Open(path, "agent")
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if sequence, err := s.NextControlSequence(ctx); err != nil || sequence != 1 {
		t.Fatalf("sequence=%d err=%v", sequence, err)
	}
	if err := s.PersistControlAck(ctx, "ack-1", 1, 7, time.Now().UTC()); err != nil {
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
	if sequence, err := s.NextControlSequence(ctx); err != nil || sequence != 2 {
		t.Fatalf("reopened sequence=%d err=%v", sequence, err)
	}
	if version, err := s.AgentState(ctx, "applied_version"); err != nil || version != "7" {
		t.Fatalf("version=%q err=%v", version, err)
	}
}

func TestControlReceiptCommitsHeartbeatAndRejectsSequenceConflict(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "agent.db"), "agent")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()
	heartbeat := time.Now().UTC()
	if err := s.PersistControlReceipt(ctx, "message-1", 4, heartbeat); err != nil {
		t.Fatal(err)
	}
	if err := s.PersistControlReceipt(ctx, "message-1", 4, heartbeat); err != nil {
		t.Fatalf("same receipt retry should be idempotent: %v", err)
	}
	if err := s.PersistControlReceipt(ctx, "message-1", 5, heartbeat); err == nil {
		t.Fatal("same message ID with conflicting sequence was accepted")
	}
	if _, err := s.AgentState(ctx, "backend_heartbeat"); err != nil {
		t.Fatalf("backend heartbeat not persisted with receipt: %v", err)
	}
	if _, err := s.AgentState(ctx, "applied_version"); err == nil {
		t.Fatal("control receipt must not advance applied config version")
	}
}
