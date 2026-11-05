package agent

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"vortex/agent/store"
	"vortex/engine/apply"
)

func TestEngineApplierReconcilesAndRetainsUnknown(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "node.db"), "agent")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	mgr := apply.NewManager()
	r := Reconciler{Store: db, Apply: EngineApplier{Manager: mgr}}
	ctx := context.Background()
	const unknown = "01000000000000000000000000000000"
	if err := r.ApplyConfig(ctx, unknown, 1, 1, true); !errors.Is(err, ErrActivationUnknown) {
		t.Fatalf("unknown commit: %v", err)
	}
	if _, err := mgr.Abort([16]byte{1}); !errors.Is(err, apply.ErrConflict) {
		t.Fatalf("unknown abort: %v", err)
	}
	if err := r.ApplyConfig(ctx, unknown, 1, 1, false); !errors.Is(err, ErrActivationUnknown) {
		t.Fatalf("unknown retry: %v", err)
	}
	const active = "02000000000000000000000000000000"
	if err := r.ApplyConfig(ctx, active, 2, 2, false); !errors.Is(err, store.ErrConfigUnresolved) {
		t.Fatalf("new activation bypassed unknown: %v", err)
	}
}
