package agent

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
	"vortex/agent/store"
	"vortex/engine/runtime/relay"
)

type adminTestEngine struct {
	mu       sync.Mutex
	draining bool
}

func (e *adminTestEngine) Start(context.Context) error { return nil }
func (e *adminTestEngine) Stop(context.Context) error  { return nil }
func (e *adminTestEngine) BeginDrain()                 { e.mu.Lock(); e.draining = true; e.mu.Unlock() }
func (e *adminTestEngine) Drain(context.Context) error { e.BeginDrain(); return nil }
func (e *adminTestEngine) Stats() relay.Stats {
	e.mu.Lock()
	defer e.mu.Unlock()
	state := "running"
	if e.draining {
		state = "drained"
	}
	return relay.Stats{DrainState: state}
}
func TestAdminPrivateSocketStatusAndIdempotentDrain(t *testing.T) {
	root, err := os.MkdirTemp("/tmp", "vortex-admin-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(root)
	db, err := store.Open(filepath.Join(root, "agent.db"), "agent")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	n, err := New("node-test", db, &adminTestEngine{})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	socket := filepath.Join(root, "run", "admin.sock")
	stop, err := n.ServeAdmin(ctx, socket)
	if err != nil {
		t.Fatal(err)
	}
	defer stop()
	if info, err := os.Stat(socket); err != nil || info.Mode().Perm() != 0600 {
		t.Fatal("socket permissions")
	}
	if info, err := os.Stat(filepath.Dir(socket)); err != nil || info.Mode().Perm() != 0700 {
		t.Fatal("directory permissions")
	}
	status, err := AdminCall(ctx, socket, "status", time.Second)
	if err != nil || status.BootID != n.BootID || status.DrainState != "running" {
		t.Fatalf("status=%+v err=%v", status, err)
	}
	for i := 0; i < 2; i++ {
		status, err = AdminCall(ctx, socket, "drain", time.Second)
		if err != nil || status.DrainState != "drained" {
			t.Fatalf("drain=%+v err=%v", status, err)
		}
	}
	if _, err = n.ServeAdmin(ctx, socket); err == nil {
		t.Fatal("replaced a live socket")
	}
}
func TestAdminRejectsPublicDirectoryAndRecoversStaleSocket(t *testing.T) {
	root, err := os.MkdirTemp("/tmp", "vortex-admin-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(root)
	db, err := store.Open(filepath.Join(root, "agent.db"), "agent")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	n, err := New("node-test", db, &adminTestEngine{})
	if err != nil {
		t.Fatal(err)
	}
	socket := filepath.Join(root, "admin.sock")
	if err = os.Chmod(root, 0755); err != nil {
		t.Fatal(err)
	}
	if _, err = n.ServeAdmin(context.Background(), socket); err == nil {
		t.Fatal("public admin directory accepted")
	}
	os.Chmod(root, 0700)
	stale, err := net.ListenUnix("unix", &net.UnixAddr{Name: socket, Net: "unix"})
	if err != nil {
		t.Fatal(err)
	}
	stale.SetUnlinkOnClose(false)
	stale.Close()
	stop, err := n.ServeAdmin(context.Background(), socket)
	if err != nil {
		t.Fatal(err)
	}
	stop()
	if err = os.WriteFile(socket, []byte("preserve"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = n.ServeAdmin(context.Background(), socket); err == nil {
		t.Fatal("regular file removed")
	}
}
func TestProcessLockRejectsSecondWriterBeforeStoreOpen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "agent.db")
	release, err := LockProcess(path)
	if err != nil {
		t.Fatal(err)
	}
	if other, err := LockProcess(path); err == nil {
		other()
		t.Fatal("two writers admitted")
	}
	release()
	release2, err := LockProcess(path)
	if err != nil {
		t.Fatal(err)
	}
	release2()
}

func TestAdminStrictBoundedRequests(t *testing.T) {
	for _, raw := range []string{`{"command":"status","command":"drain","timeout_ms":1}`, `{"command":"status"} {}`, `{"command":"status","extra":1}`, `[]`} {
		if decodeAdminRequest([]byte(raw), &adminRequest{}) == nil {
			t.Fatalf("accepted %s", raw)
		}
	}
	if err := decodeAdminRequest([]byte("{\"command\":\"status\",\"timeout_ms\":0}\n"), &adminRequest{}); err != nil {
		t.Fatal(err)
	}
}
