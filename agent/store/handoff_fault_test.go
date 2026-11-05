package store

import (
	"bufio"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// This child deliberately does not close SQLite. Parent SIGKILLs it either
// before the handoff or immediately after its durable commit acknowledgement.
func TestHandoffCrashWorker(t *testing.T) {
	path := os.Getenv("VORTEX_HANDOFF_CHILD_DB")
	if path == "" {
		return
	}
	s, err := Open(path, "hub")
	if err != nil {
		t.Fatal(err)
	}
	var h HandoffRequest
	if err = json.Unmarshal([]byte(os.Getenv("VORTEX_HANDOFF_CHILD_REQUEST")), &h); err != nil {
		t.Fatal(err)
	}
	if os.Getenv("VORTEX_HANDOFF_CRASH_STAGE") == "after" {
		if _, err = s.HandoffHub(context.Background(), h, IssuerTime{Earliest: h.Heartbeat, Latest: h.Heartbeat.Add(time.Second), Trusted: true}); err != nil {
			t.Fatal(err)
		}
	}
	os.Stdout.WriteString("kill-ready\n")
	// Wait indefinitely without timers; test parent owns termination.
	var input [1]byte
	os.Stdin.Read(input[:])
	os.Exit(2)
}

func TestHandoffSIGKILLRecovery(t *testing.T) {
	for _, stage := range []string{"before", "after"} {
		t.Run(stage, func(t *testing.T) {
			s, d, clock, path := issuerFixture(t)
			ctx := context.Background()
			if _, err := s.IssueHubAuthorization(ctx, issuerRequest(d, 200), clock); err != nil {
				t.Fatal(err)
			}
			if err := s.Close(); err != nil {
				t.Fatal(err)
			}
			h := HandoffRequest{ID: "550e8400-e29b-41d4-a716-446655440800", HubID: d.HubID, FromEpoch: 1, ToEpoch: 2, Heartbeat: d.BackendHeartbeat.Add(time.Minute)}
			raw, err := json.Marshal(h)
			if err != nil {
				t.Fatal(err)
			}
			binary, err := os.Executable()
			if err != nil {
				t.Fatal(err)
			}
			childCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
			defer cancel()
			cmd := exec.CommandContext(childCtx, binary, "-test.run=^TestHandoffCrashWorker$")
			cmd.Env = append(os.Environ(), "VORTEX_HANDOFF_CHILD_DB="+path, "VORTEX_HANDOFF_CHILD_REQUEST="+string(raw), "VORTEX_HANDOFF_CRASH_STAGE="+stage)
			stdout, err := cmd.StdoutPipe()
			if err != nil {
				t.Fatal(err)
			}
			stdin, err := cmd.StdinPipe()
			if err != nil {
				t.Fatal(err)
			}
			defer stdin.Close()
			if err = cmd.Start(); err != nil {
				t.Fatal(err)
			}
			scan := bufio.NewScanner(stdout)
			ready := false
			for scan.Scan() {
				if scan.Text() == "kill-ready" {
					ready = true
					break
				}
			}
			if !ready {
				cmd.Wait()
				t.Fatalf("child failed before crash point: %v", scan.Err())
			}
			if err = cmd.Process.Kill(); err != nil {
				t.Fatal(err)
			}
			_ = cmd.Wait()
			recovered, err := Open(path, "hub")
			if err != nil {
				t.Fatal(err)
			}
			defer recovered.Close()
			var epoch string
			if err = recovered.db.QueryRow(`SELECT signer_epoch FROM issuer_epochs WHERE hub_id=?`, d.HubID).Scan(&epoch); err != nil {
				t.Fatal(err)
			}
			expected := "1"
			if stage == "after" {
				expected = "2"
			}
			if epoch != expected {
				t.Fatalf("partial state epoch=%s", epoch)
			}
			clock = IssuerTime{Earliest: h.Heartbeat, Latest: h.Heartbeat.Add(time.Second), Trusted: true}
			r, err := recovered.HandoffHub(ctx, h, clock)
			if err != nil || r.Remaining[d.ID] != 768 {
				t.Fatalf("recovery %+v %v", r, err)
			}
			b, err := recovered.AuditHubDelegation(ctx, d.ID)
			if err != nil || b.Issued != 256 || b.Remaining != 768 {
				t.Fatalf("conservation %+v %v", b, err)
			}
		})
	}
}

func TestHandoffFullDiskDoesNotPartiallyTransfer(t *testing.T) {
	root := os.Getenv("VORTEX_TEST_FULL_DISK_DIR")
	if root == "" {
		t.Skip("requires isolated bounded filesystem")
	}
	dir, err := os.MkdirTemp(root, "handoff-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	fixture, d, clock, _ := issuerFixture(t)
	fixture.Close()
	s, err := Open(filepath.Join(dir, "hub.db"), "hub")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()
	if err = s.InstallHubDelegation(ctx, d, clock); err != nil {
		t.Fatal(err)
	}
	if _, err = s.db.Exec(`PRAGMA wal_checkpoint(TRUNCATE)`); err != nil {
		t.Fatal(err)
	}
	filler := filepath.Join(dir, "fill")
	f, err := os.Create(filler)
	if err != nil {
		t.Fatal(err)
	}
	chunk := make([]byte, 64*1024)
	for {
		if _, err = f.Write(chunk); err != nil {
			break
		}
	}
	f.Close()
	h := HandoffRequest{ID: "550e8400-e29b-41d4-a716-446655440800", HubID: d.HubID, FromEpoch: 1, ToEpoch: 2, Heartbeat: d.BackendHeartbeat.Add(time.Minute)}
	clock = IssuerTime{Earliest: h.Heartbeat, Latest: h.Heartbeat.Add(time.Second), Trusted: true}
	if _, err = s.HandoffHub(ctx, h, clock); err == nil {
		t.Fatal("full-disk handoff unexpectedly succeeded")
	}
	if err = os.Remove(filler); err != nil {
		t.Fatal(err)
	}
	var epoch string
	if err = s.db.QueryRow(`SELECT signer_epoch FROM issuer_epochs WHERE hub_id=?`, d.HubID).Scan(&epoch); err != nil || epoch != "1" {
		t.Fatalf("partial epoch %s %v", epoch, err)
	}
	var count int
	if err = s.db.QueryRow(`SELECT COUNT(*) FROM hub_handoffs`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("partial transfer %d %v", count, err)
	}
	if _, err = s.HandoffHub(ctx, h, clock); err != nil {
		t.Fatal(err)
	}
}
