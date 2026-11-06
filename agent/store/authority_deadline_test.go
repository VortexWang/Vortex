package store

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"
)

func TestRuntimeAdmissionDeadlineWhileWriterLocked(t *testing.T) {
	s, scope, path := runtimeStore(t)
	other, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()
	conn, err := other.Conn(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if _, err := conn.ExecContext(context.Background(), "BEGIN IMMEDIATE"); err != nil {
		t.Fatal(err)
	}
	defer conn.ExecContext(context.Background(), "ROLLBACK")
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	started := time.Now()
	auth, err := s.OpenRuntimeSession(ctx, scope, "blocked-session", "blocked-flow")
	elapsed := time.Since(started)
	t.Logf("locked writer admission returned after %s: %v", elapsed, err)
	if err == nil || auth.Use.SessionID != "" {
		t.Fatalf("locked transaction returned authority: %+v %v", auth, err)
	}
	if elapsed > 500*time.Millisecond {
		t.Fatalf("SQLite lock wait escaped 200ms context: %s", elapsed)
	}
	if _, err := conn.ExecContext(context.Background(), "ROLLBACK"); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM runtime_sessions WHERE session_id='blocked-session'`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("partial session after locked write: %d %v", count, err)
	}
	b, err := s.AuditAuthorizationSource(context.Background(), "node", "funding")
	if err != nil || b.Remaining != b.Initial || b.Reserved != 0 {
		t.Fatalf("lock failure touched funds: %+v %v", b, err)
	}
}

func TestRuntimeAdmissionDeadlineWhileConnectionOccupied(t *testing.T) {
	s, scope, _ := runtimeStore(t)
	held, err := s.db.Conn(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Millisecond)
	defer cancel()
	started := time.Now()
	auth, err := s.OpenRuntimeSession(ctx, scope, "waiting-session", "flow")
	elapsed := time.Since(started)
	held.Close()
	if !errors.Is(err, context.DeadlineExceeded) || auth.Use.SessionID != "" || elapsed > 300*time.Millisecond {
		t.Fatalf("connection queue ignored deadline: %+v %s %v", auth, elapsed, err)
	}
	var count int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM runtime_sessions`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("queued request created session: %d %v", count, err)
	}
}
