package store

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"testing"
)

func TestDurableEventExactReplayAndConflictAfterReopen(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "hub.db")
	s, err := Open(path, "hub")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.AppendEvent(ctx, "apply-transaction", "control-apply-result", []byte("active")); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = Open(path, "hub")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := s.AppendEvent(ctx, "apply-transaction", "control-apply-result", []byte("active")); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	for _, change := range []struct{ kind, payload string }{{"other-kind", "active"}, {"control-apply-result", "different"}} {
		if err := s.AppendEvent(ctx, "apply-transaction", change.kind, []byte(change.payload)); !errors.Is(err, ErrEventConflict) {
			t.Fatalf("changed event accepted: %v", err)
		}
	}
	var count int
	var payload string
	if err := s.db.QueryRow(`SELECT COUNT(*),CAST(payload AS TEXT) FROM durable_events WHERE event_id='apply-transaction'`).Scan(&count, &payload); err != nil || count != 1 || payload != "active" {
		t.Fatalf("event changed: %d %q %v", count, payload, err)
	}
	if _, err := s.db.Exec(`CREATE TRIGGER fail_event BEFORE INSERT ON durable_events BEGIN SELECT RAISE(ABORT,'injected disk failure'); END`); err != nil {
		t.Fatal(err)
	}
	if err := s.AppendEvent(ctx, "new-event", "kind", []byte("payload")); err == nil {
		t.Fatal("uncommitted event acknowledged")
	}
	if err := s.AppendEvent(ctx, "apply-transaction", "control-apply-result", []byte("active")); err != nil {
		t.Fatalf("durable replay required another write: %v", err)
	}
}
