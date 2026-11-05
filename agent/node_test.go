package agent

import (
	"context"
	"path/filepath"
	"testing"

	"vortex/agent/store"
)

func TestNodeStartsThroughEngineBoundary(t *testing.T) {
	s, err := store.Open(filepath.Join(t.TempDir(), "node.db"), "agent")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	n, err := New("node-1", s, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := n.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := n.Heartbeat(context.Background(), 1); err != nil {
		t.Fatal(err)
	}
	count, err := s.EventCount(context.Background())
	if err != nil || count != 2 {
		t.Fatalf("count=%d err=%v", count, err)
	}
}
