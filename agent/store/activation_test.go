package store

import (
	"context"
	"crypto/sha256"
	"errors"
	"path/filepath"
	"testing"
)

func TestActivationTransitionsRejectLateAndConcurrentResults(t *testing.T) {
	ctx := context.Background()
	s, err := Open(filepath.Join(t.TempDir(), "agent.db"), "agent")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	payload := []byte("config")
	sum := sha256.Sum256(payload)
	req := ConfigResult{TransactionID: "01000000000000000000000000000000", Version: 1, FencingGeneration: 1, Payload: payload, PayloadSHA256: sum[:]}
	if _, err = s.SaveConfigRequest(ctx, req); err != nil {
		t.Fatal(err)
	}
	if err = s.FinalizeConfigResult(ctx, req.TransactionID, 1, nil); !errors.Is(err, ErrConfigRequestConflict) {
		t.Fatalf("unprepared finalize %v", err)
	}
	next := req
	next.TransactionID = "02000000000000000000000000000000"
	next.FencingGeneration = 2
	if _, err = s.SaveConfigRequest(ctx, next); !errors.Is(err, ErrConfigUnresolved) {
		t.Fatalf("concurrent received %v", err)
	}
	for _, state := range []string{"prepared", "commit-requested", "activation-unknown"} {
		if err = s.SaveConfigResult(ctx, req.TransactionID, 1, state, ""); err != nil {
			t.Fatal(err)
		}
	}
	if err = s.SaveConfigResult(ctx, req.TransactionID, 2, "active", ""); !errors.Is(err, ErrConfigRequestConflict) {
		t.Fatalf("version mutation %v", err)
	}
	if err = s.SaveConfigResult(ctx, req.TransactionID, 1, "aborted", ""); !errors.Is(err, ErrConfigRequestConflict) {
		t.Fatalf("unknown abort %v", err)
	}
	if err = s.FinalizeConfigResult(ctx, req.TransactionID, 1, nil); err != nil {
		t.Fatal(err)
	}
	if err = s.SaveConfigResult(ctx, req.TransactionID, 1, "prepared", ""); !errors.Is(err, ErrConfigRequestConflict) {
		t.Fatalf("late prepare %v", err)
	}
	next.Version = 2
	next.ExpectedVersion = 1
	if _, err = s.SaveConfigRequest(ctx, next); err != nil {
		t.Fatal(err)
	}
	for _, state := range []string{"prepared", "commit-requested"} {
		if err = s.SaveConfigResult(ctx, next.TransactionID, 2, state, ""); err != nil {
			t.Fatal(err)
		}
	}
	if err = s.FinalizeConfigResult(ctx, next.TransactionID, 2, nil); err != nil {
		t.Fatal(err)
	}
	if err = s.FinalizeConfigResult(ctx, req.TransactionID, 1, nil); !errors.Is(err, ErrConfigVersionStale) {
		t.Fatalf("late active regressed version %v", err)
	}
	value, err := s.AgentState(ctx, "applied_version")
	if err != nil || value != "2" {
		t.Fatal(value, err)
	}
}

func TestCorruptAppliedVersionDoesNotResolveUnknown(t *testing.T) {
	ctx := context.Background()
	s, err := Open(filepath.Join(t.TempDir(), "agent.db"), "agent")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	p := []byte("config")
	sum := sha256.Sum256(p)
	id := "01000000000000000000000000000000"
	if _, err = s.SaveConfigRequest(ctx, ConfigResult{TransactionID: id, Version: 1, FencingGeneration: 9, Payload: p, PayloadSHA256: sum[:]}); err != nil {
		t.Fatal(err)
	}
	if _, err = s.db.Exec(`INSERT INTO agent_state(key,value,updated_at) VALUES('applied_version','corrupt','now')`); err != nil {
		t.Fatal(err)
	}
	if err = s.ObserveActivation(ctx, id, 9, "active"); err == nil {
		t.Fatal("corrupt applied version accepted")
	}
	row, err := s.GetConfigResult(ctx, id)
	if err != nil || row.State != "received" {
		t.Fatal(row, err)
	}
	var count int
	if err = s.db.QueryRow(`SELECT COUNT(*) FROM activation_observations`).Scan(&count); err != nil || count != 0 {
		t.Fatal("failed observation left evidence", count, err)
	}
}
