package agent

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"vortex/agent/store"
)

type fakeApplier struct{}

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
	r := Reconciler{Store: s, Apply: fakeApplier{}}
	if err := r.ApplyConfig(context.Background(), "00112233445566778899aabbccddeeff", 2, 1, true); err == nil {
		t.Fatal("expected unknown commit result")
	}
	unknown, err := s.GetConfigResult(context.Background(), "00112233445566778899aabbccddeeff")
	if err != nil || unknown.State != "activation-unknown" {
		t.Fatalf("unknown result=%+v err=%v", unknown, err)
	}
	if err := r.ApplyConfig(context.Background(), "10112233445566778899aabbccddeeff", 3, 2, false); err != nil {
		t.Fatal(err)
	}
}
