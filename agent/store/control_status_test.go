package store

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func TestHubControlSnapshotNeverCreatesOrRefreshesAuthority(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "hub.db")
	s, err := Open(path, "hub")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	const hub = "550e8400-e29b-41d4-a716-446655440010"
	before, err := s.HubControlSnapshot(ctx, hub, time.Now())
	if err != nil || before.Mode != "blocked" || before.Epoch != 0 {
		t.Fatalf("uninitialized status %+v %v", before, err)
	}
	if _, err = s.AgentState(ctx, "backend_heartbeat"); err == nil {
		t.Fatal("status created backend time")
	}
	s.Close()
	s, d, clock, seedPath := issuerFixture(t)
	path = seedPath
	first, err := s.HubControlSnapshot(ctx, d.HubID, clock.Earliest)
	if err != nil || first.Mode != "online" {
		t.Fatalf("online snapshot %+v %v", first, err)
	}
	late, err := s.HubControlSnapshot(ctx, d.HubID, d.BackendHeartbeat.Add(31*time.Second))
	if err != nil || late.Mode != "autonomous" || !late.Deadline.Equal(first.Deadline) || !late.BackendHeartbeat.Equal(first.BackendHeartbeat) {
		t.Fatalf("status extended authority: %+v %v", late, err)
	}
	expired, err := s.HubControlSnapshot(ctx, d.HubID, d.AutonomyDeadline)
	if err != nil || expired.Mode != "blocked" {
		t.Fatalf("expired status %+v %v", expired, err)
	}
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(path, "hub")
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	status, err := reopened.HubControlSnapshot(ctx, d.HubID, d.BackendHeartbeat.Add(31*time.Second))
	if err != nil || status.Mode != "autonomous" || !status.Deadline.Equal(d.AutonomyDeadline) {
		t.Fatalf("restart extended deadline: %+v %v", status, err)
	}
}

func TestControlSequenceExhaustionPersistsAcrossReopen(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "agent.db")
	s, err := Open(path, "agent")
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.db.Exec("INSERT INTO agent_state(key,value,updated_at) VALUES('control_sequence','9223372036854775807','now')")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.NextControlSequence(ctx); err == nil {
		t.Fatal("control sequence wrapped")
	}
	s.Close()
	s, err = Open(path, "agent")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if _, err = s.NextControlSequence(ctx); err == nil {
		t.Fatal("reopen reset exhausted sequence")
	}
	if v, err := s.AgentState(ctx, "control_sequence"); err != nil || v != "9223372036854775807" {
		t.Fatalf("sequence mutated: %s %v", v, err)
	}
}
