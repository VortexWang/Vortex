package agent

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
	"vortex/agent/store"
	"vortex/engine/apply"
)

type lostCommitReceipt struct{ EngineApplier }

func (a lostCommitReceipt) Commit(id [16]byte, uncertain bool) (any, error) {
	result, err := a.EngineApplier.Commit(id, false)
	if err != nil {
		return result, err
	}
	return result, ErrActivationUnknown
}
func TestRecoveryQueriesExecutionBeforeReleasingResources(t *testing.T) {
	for _, lost := range []bool{false, true} {
		name := "missing-after-restart"
		if lost {
			name = "lost-ack-executed"
		}
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			path := filepath.Join(t.TempDir(), "agent.db")
			s, err := store.Open(path, "agent")
			if err != nil {
				t.Fatal(err)
			}
			defer s.Close()
			mgr := apply.NewManager()
			adapter := EngineApplier{Manager: mgr}
			var applier Applier = adapter
			if lost {
				applier = lostCommitReceipt{adapter}
			}
			r := &Reconciler{Store: s, Apply: applier}
			const id = "01000000000000000000000000000000"
			if err := r.ApplyConfig(ctx, id, 1, 9, true); !errors.Is(err, ErrActivationUnknown) {
				t.Fatal(err)
			}
			if err := s.Close(); err != nil {
				t.Fatal(err)
			}
			s, err = store.Open(path, "agent")
			if err != nil {
				t.Fatal(err)
			}
			defer s.Close()
			if !lost {
				adapter = EngineApplier{Manager: apply.NewManager()}
			}
			r = &Reconciler{Store: s, Apply: adapter}
			err = r.RecoverConfig(ctx, id)
			if lost && err != nil {
				t.Fatal(err)
			}
			if !lost && !errors.Is(err, ErrActivationUnknown) {
				t.Fatal("empty manager became inactivity proof", err)
			}
			result, err := s.GetConfigResult(ctx, id)
			if err != nil {
				t.Fatal(err)
			}
			want := "activation-unknown"
			if lost {
				want = "active"
			}
			if result.State != want {
				t.Fatal(result.State)
			}
			db, err := sql.Open("sqlite", path)
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			var hold string
			if err := db.QueryRow(`SELECT state FROM activation_holds WHERE transaction_id=?`, id).Scan(&hold); err != nil || hold != "held" {
				t.Fatal("hold released", hold, err)
			}
			if err := s.ObserveActivation(ctx, id, 10, "released"); err == nil {
				t.Fatal("wrong-generation proof accepted")
			}
		})
	}
}

func TestRecoveryReleasesOnlyAbortedExecution(t *testing.T) {
	ctx := context.Background()
	s, err := store.Open(filepath.Join(t.TempDir(), "agent.db"), "agent")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	mgr := apply.NewManager()
	r := &Reconciler{Store: s, Apply: EngineApplier{Manager: mgr}}
	const id = "01000000000000000000000000000000"
	// Persist an uncertain outcome while execution is still prepared.
	if err := s.SaveConfigResult(ctx, id, 1, "activation-unknown", ""); err != nil {
		t.Fatal(err)
	}
	// Legacy rows with no fencing proof must not become releasable.
	if _, err := mgr.Prepare([16]byte{1}, 9); err != nil {
		t.Fatal(err)
	}
	if _, err := mgr.Abort([16]byte{1}); err != nil {
		t.Fatal(err)
	}
	if err := r.RecoverConfig(ctx, id); !errors.Is(err, ErrConfigConflict) {
		t.Fatal("legacy missing fence accepted", err)
	}
}

func TestRecoveryAcceptsMatchingAbortProof(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "agent.db")
	s, err := store.Open(path, "agent")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	const id = "01000000000000000000000000000000"
	digest := sha256.Sum256([]byte("config"))
	if _, err := s.SaveConfigRequest(ctx, store.ConfigResult{TransactionID: id, Version: 1, FencingGeneration: 9, Payload: []byte("config"), PayloadSHA256: digest[:]}); err != nil {
		t.Fatal(err)
	}
	mgr := apply.NewManager()
	if err := s.BindActivationExecutor(ctx, id, mgr.InstanceID()); err != nil {
		t.Fatal(err)
	}
	if _, err := mgr.Prepare([16]byte{1}, 9); err != nil {
		t.Fatal(err)
	}
	if _, err := mgr.Abort([16]byte{1}); err != nil {
		t.Fatal(err)
	}
	r := &Reconciler{Store: s, Apply: EngineApplier{Manager: mgr}}
	if err := r.RecoverConfig(ctx, id); err != nil {
		t.Fatal(err)
	}
	if err := r.RecoverConfig(ctx, id); err != nil {
		t.Fatal("abort replay", err)
	}
	row, err := s.GetConfigResult(ctx, id)
	if err != nil || row.State != "released" {
		t.Fatal(row, err)
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var state string
	if err := db.QueryRow(`SELECT state FROM activation_holds WHERE transaction_id=?`, id).Scan(&state); err != nil || state != "released" {
		t.Fatal(state, err)
	}
}

func TestNewExecutorCannotForgeOldAbortProof(t *testing.T) {
	ctx := context.Background()
	s, err := store.Open(filepath.Join(t.TempDir(), "agent.db"), "agent")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	const id = "01000000000000000000000000000000"
	original := &Reconciler{Store: s, Apply: lostCommitReceipt{EngineApplier{Manager: apply.NewManager()}}}
	if err = original.ApplyConfig(ctx, id, 1, 9, false); !errors.Is(err, ErrActivationUnknown) {
		t.Fatal(err)
	}
	replacement := apply.NewManager()
	if _, err = replacement.Prepare([16]byte{1}, 9); err != nil {
		t.Fatal(err)
	}
	if _, err = replacement.Abort([16]byte{1}); err != nil {
		t.Fatal(err)
	}
	recovered := &Reconciler{Store: s, Apply: EngineApplier{Manager: replacement}}
	if err = recovered.RecoverConfig(ctx, id); !errors.Is(err, ErrActivationUnknown) {
		t.Fatalf("forged proof: %v", err)
	}
	row, err := s.GetConfigResult(ctx, id)
	if err != nil || row.State != "activation-unknown" {
		t.Fatal(row, err)
	}
}
