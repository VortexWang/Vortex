package agent

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"vortex/agent/store"
	"vortex/engine/apply"
)

func TestRuntimeCommitRejectsAdvancedAuthorizationFence(t *testing.T) {
	s, err := store.Open(filepath.Join(t.TempDir(), "agent.db"), "agent")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	u := store.AuthorizationUpdate{NodeID: "node", BootID: "boot", UpdateID: "new-floor", Payload: []byte("new-floor"), SignerEpoch: 1, PathGeneration: 1, FencingGenerationFloor: 2}
	if err := s.PersistAuthorizationUpdate(context.Background(), u, nil, nil); err != nil {
		t.Fatal(err)
	}
	m := apply.NewManager()
	id := [16]byte{1}
	if _, err := m.Prepare(id, 1); err != nil {
		t.Fatal(err)
	}
	a := &RuntimeApplier{EngineApplier: EngineApplier{Manager: m}, node: &Node{NodeID: "node", BootID: "boot", Store: s}, engine: &RuntimeEngine{}}
	if _, err := a.Commit(id, false); !errors.Is(err, store.ErrConfigGenerationStale) {
		t.Fatalf("stale commit: %v", err)
	}
	tx, _ := m.Query(id)
	if tx.State != apply.Prepared || a.engine.activated.Load() {
		t.Fatalf("stale transaction activated: %+v", tx)
	}
}
