package agent

import (
	"context"
	"encoding/hex"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"vortex/agent/store"
)

type fakeApplier struct{}

type persistedApplier struct {
	t     *testing.T
	store *store.Store
}

func (persistedApplier) Prepare([16]byte, uint64) (any, error) { return struct{}{}, nil }
func (a persistedApplier) Commit(id [16]byte, uncertain bool) (any, error) {
	result, err := a.store.GetConfigResult(context.Background(), hex.EncodeToString(id[:]))
	if err != nil || result.State != "committing" {
		a.t.Fatalf("Commit issued before durable intent: result=%+v err=%v", result, err)
	}
	if uncertain {
		return nil, errors.New("commit result unknown")
	}
	return []ObjectResult{{ObjectID: "rule-1", State: "active"}}, nil
}
func (persistedApplier) Abort([16]byte) (any, error) { return struct{}{}, nil }

func (fakeApplier) Prepare([16]byte, uint64) (any, error) { return struct{}{}, nil }
func (fakeApplier) Commit(_ [16]byte, uncertain bool) (any, error) {
	if uncertain {
		return nil, errors.New("commit result unknown")
	}
	return struct{}{}, nil
}
func (fakeApplier) Abort([16]byte) (any, error) { return struct{}{}, nil }

func TestReconcilerPersistsActivationUnknownBeforeReturning(t *testing.T) {
	s, err := store.Open(filepath.Join(t.TempDir(), "agent.db"), "agent")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	r := Reconciler{Store: s, Apply: persistedApplier{t: t, store: s}}
	if err := r.ApplyConfig(context.Background(), "00112233445566778899aabbccddeeff", 2, 1, true); err == nil {
		t.Fatal("expected unknown commit result")
	}
	unknown, err := s.GetConfigResult(context.Background(), "00112233445566778899aabbccddeeff")
	if err != nil || unknown.State != "activation-unknown" {
		t.Fatalf("unknown result=%+v err=%v", unknown, err)
	}
	if err := r.ApplyConfig(context.Background(), "10112233445566778899aabbccddeeff", 3, 2, false); !errors.Is(err, store.ErrConfigUnresolved) {
		t.Fatalf("new transaction bypassed unknown activation: %v", err)
	}
	if err := r.ApplyConfig(context.Background(), "00112233445566778899aabbccddeeff", 2, 1, false); !errors.Is(err, ErrActivationUnknown) {
		t.Fatalf("replay unknown transaction err=%v", err)
	}
	if err := s.PersistControlAck(context.Background(), "version-4", 4, 4, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	if err := r.ApplyConfig(context.Background(), "20112233445566778899aabbccddeeff", 1, 3, false); !errors.Is(err, ErrStaleRevision) {
		t.Fatalf("stale revision err=%v", err)
	}
}

func TestDirectReconcileRetainsFencingAndUnknownAcrossReopen(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "agent.db")
	s, err := store.Open(path, "agent")
	if err != nil {
		t.Fatal(err)
	}
	r := &Reconciler{Store: s, Apply: fakeApplier{}}
	const id = "00112233445566778899aabbccddeeff"
	if err := r.ApplyConfig(ctx, id, 1, 7, true); err == nil {
		t.Fatal("expected unknown")
	}
	row, err := s.GetConfigResult(ctx, id)
	if err != nil || row.FencingGeneration != 7 {
		t.Fatal("fence not durable", row, err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = store.Open(path, "agent")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	r = &Reconciler{Store: s, Apply: fakeApplier{}}
	if err := r.ApplyConfig(ctx, id, 1, 8, false); !errors.Is(err, ErrConfigConflict) {
		t.Fatal("changed fencing accepted", err)
	}
	if err := r.ApplyConfig(ctx, id, 1, 7, false); !errors.Is(err, ErrActivationUnknown) {
		t.Fatal("restart resolved uncertainty", err)
	}
	if err := r.ApplyConfig(ctx, "10112233445566778899aabbccddeeff", 2, 8, false); !errors.Is(err, store.ErrConfigUnresolved) {
		t.Fatal("new transaction bypassed persisted uncertainty", err)
	}
}
