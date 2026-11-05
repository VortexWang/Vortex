package store

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"
)

func TestAutonomyUsesFixedHeartbeatDeadlineAcrossRestart(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "agent.db")
	base := time.Date(2026, 11, 5, 12, 0, 0, 0, time.UTC)
	deadline := base.Add(2 * time.Hour)
	s, err := Open(path, "agent")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.RecordBackendHeartbeat(ctx, base, deadline); err != nil {
		t.Fatal(err)
	}
	status, err := s.CurrentAutonomyStatus(ctx, base.Add(BackendLossGrace-time.Nanosecond), true)
	if err != nil || status.Mode != AutonomyOnline || !status.SignerEpochOpen || !status.AllowsNewAuthorization {
		t.Fatalf("pre-grace status=%+v err=%v", status, err)
	}
	status, err = s.CurrentAutonomyStatus(ctx, base.Add(BackendLossGrace), true)
	if err != nil || status.Mode != AutonomyOffline || status.SignerEpochOpen || !status.AllowsExistingRenewal || status.AllowsNewAuthorization {
		t.Fatalf("autonomy status=%+v err=%v", status, err)
	}
	if status.AutonomyStartedAt != base.Add(BackendLossGrace) || status.HardDeadline != deadline {
		t.Fatalf("autonomy boundary moved: %+v", status)
	}
	if err := s.CheckAuthorization(ctx, base.Add(time.Minute), true, true, true, true); !errors.Is(err, ErrNewAuthorityInAutonomy) {
		t.Fatalf("caller booleans bypassed durable ownership checks: %v", err)
	}
	if err := s.CheckAuthorization(ctx, base.Add(time.Minute), true, true, false, true); !errors.Is(err, ErrNewAuthorityInAutonomy) {
		t.Fatalf("partial existing-resource match was allowed: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = Open(path, "agent")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	status, err = s.CurrentAutonomyStatus(ctx, base.Add(90*time.Minute), true)
	if err != nil || status.Mode != AutonomyOffline || status.HardDeadline != deadline || status.SignerEpochOpen {
		t.Fatalf("reopened status=%+v err=%v", status, err)
	}
	if err := s.RecordBackendHeartbeat(ctx, base.Add(90*time.Minute), base.Add(4*time.Hour)); !errors.Is(err, ErrAuthorityHandoffRequired) {
		t.Fatalf("reconnect extended autonomous deadline without handoff: %v", err)
	}
	status, err = s.CurrentAutonomyStatus(ctx, deadline, true)
	if err != nil || status.Mode != AutonomyExpired || status.AllowsExistingRenewal {
		t.Fatalf("hard-deadline status=%+v err=%v", status, err)
	}
}

func TestAutonomyTimeUncertaintyAndLateHeartbeatFailClosed(t *testing.T) {
	ctx := context.Background()
	base := time.Date(2026, 11, 5, 12, 0, 0, 0, time.UTC)
	s, err := Open(filepath.Join(t.TempDir(), "agent.db"), "agent")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	deadline := base.Add(time.Hour)
	if err := s.RecordBackendHeartbeat(ctx, base, deadline); err != nil {
		t.Fatal(err)
	}
	status, err := s.CurrentAutonomyStatus(ctx, base.Add(40*time.Second), false)
	if err != nil || status.Mode != AutonomyTimeUncertain || status.AllowsExistingRenewal || status.AllowsNewAuthorization {
		t.Fatalf("uncertain clock status=%+v err=%v", status, err)
	}
	if err := s.CheckAuthorization(ctx, base.Add(40*time.Second), false, true, true, true); !errors.Is(err, ErrAuthorityTimeUncertain) {
		t.Fatalf("authorization under uncertain clock err=%v", err)
	}
	if err := s.RecordBackendHeartbeat(ctx, base.Add(40*time.Second), base.Add(2*time.Hour)); !errors.Is(err, ErrAuthorityHandoffRequired) {
		t.Fatalf("late heartbeat bypassed autonomy handoff: %v", err)
	}
	status, err = s.CurrentAutonomyStatus(ctx, base.Add(time.Hour), true)
	if err != nil || status.Mode != AutonomyExpired {
		t.Fatalf("late observation skipped hard expiry: %+v err=%v", status, err)
	}
	var transitions int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM autonomy_transitions`).Scan(&transitions); err != nil {
		t.Fatal(err)
	}
	if transitions != 2 {
		t.Fatalf("expected persisted autonomous and expired transitions, got %d", transitions)
	}
}

func TestFreshHeartbeatBeforeGraceRenewsOnlyFromOnlineMode(t *testing.T) {
	ctx := context.Background()
	base := time.Date(2026, 11, 5, 12, 0, 0, 0, time.UTC)
	s, err := Open(filepath.Join(t.TempDir(), "agent.db"), "agent")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := s.RecordBackendHeartbeat(ctx, base, base.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err := s.RecordBackendHeartbeat(ctx, base.Add(20*time.Second), base.Add(time.Hour+20*time.Second)); err != nil {
		t.Fatal(err)
	}
	status, err := s.CurrentAutonomyStatus(ctx, base.Add(30*time.Second), true)
	if err != nil || status.Mode != AutonomyOnline || status.HardDeadline != base.Add(time.Hour+20*time.Second) {
		t.Fatalf("in-window heartbeat status=%+v err=%v", status, err)
	}
}

func TestAutonomyRejectsHeartbeatWithoutFixedDeadline(t *testing.T) {
	ctx := context.Background()
	s, err := Open(filepath.Join(t.TempDir(), "agent.db"), "agent")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := s.PersistControlAck(ctx, "legacy-ack", 1, 1, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CurrentAutonomyStatus(ctx, time.Now().UTC(), true); err == nil {
		t.Fatal("heartbeat without a durable hard deadline was treated as authority")
	}
}
