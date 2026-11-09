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

// Force a backend heartbeat commit between the old caller-time sample and the
// eventual snapshot read. The observer must keep one consistent read snapshot
// and sample time afterward; neither a future heartbeat nor a stale signer may
// be repaired by fabricating authority.
func TestHubControlObservationPinsSnapshotBeforeClock(t *testing.T) {
	ctx := context.Background()
	s, d, base, path := issuerFixture(t)
	defer s.Close()
	writer, err := Open(path, "hub")
	if err != nil {
		t.Fatal(err)
	}
	defer writer.Close()
	advanced := base.Latest.Add(time.Millisecond)
	calls := 0
	got, err := s.ObserveHubControlSnapshot(ctx, d.HubID, func() IssuerTime {
		calls++
		if err := writer.RecordBackendHeartbeat(ctx, advanced, d.AutonomyDeadline); err != nil {
			t.Fatal(err)
		}
		return base
	})
	if err != nil || calls != 1 || got.Mode != "online" || !got.BackendHeartbeat.Equal(d.BackendHeartbeat) || !got.Deadline.Equal(d.AutonomyDeadline) {
		t.Fatal("mixed observation snapshot/time", got, calls, err)
	}
	stale, err := s.HubControlSnapshot(ctx, d.HubID, base.Latest)
	if err != nil || stale.Mode != "blocked" {
		t.Fatal("future durable heartbeat was accepted", stale, err)
	}
	fresh := IssuerTime{Earliest: advanced, Latest: advanced, Trusted: true}
	got, err = s.ObserveHubControlSnapshot(ctx, d.HubID, func() IssuerTime { return fresh })
	if err != nil || got.Mode != "online" || !got.BackendHeartbeat.Equal(advanced) || !got.Deadline.Equal(d.AutonomyDeadline) {
		t.Fatal(got, err)
	}
	for _, clock := range []func() IssuerTime{nil, func() IssuerTime { v := fresh; v.Trusted = false; return v }, func() IssuerTime { v := fresh; v.Latest = v.Earliest.Add(-time.Second); return v }} {
		if _, err := s.ObserveHubControlSnapshot(ctx, d.HubID, clock); err != ErrAuthorityTimeUncertain {
			t.Fatal("untrusted observation", err)
		}
	}
	deadline, err := s.AgentState(ctx, "autonomy_deadline")
	if err != nil || deadline != d.AutonomyDeadline.UTC().Format(time.RFC3339Nano) {
		t.Fatal("observation changed original deadline", deadline, err)
	}
}
