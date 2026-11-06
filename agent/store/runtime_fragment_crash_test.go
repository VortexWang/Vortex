package store

import (
	"bufio"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"testing"
	"time"
)

func TestRuntimeFragmentCrashWorker(t *testing.T) {
	path := os.Getenv("VORTEX_FRAGMENT_CRASH_DB")
	if path == "" {
		return
	}
	s, err := Open(path, "agent")
	if err != nil {
		t.Fatal(err)
	}
	var use AuthorizationUse
	if err := json.Unmarshal([]byte(os.Getenv("VORTEX_FRAGMENT_USE")), &use); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	first, err := s.ReserveRuntimeUsageUpTo(ctx, runtimeReservation("first", 100), use)
	if err != nil {
		t.Fatal(err)
	}
	stage := os.Getenv("VORTEX_FRAGMENT_CRASH_STAGE")
	if stage != "first" {
		if _, err := s.ReserveRuntimeUsageUpTo(ctx, runtimeReservation("second", 100), use); err != nil {
			t.Fatal(err)
		}
	}
	if stage == "settled-first" {
		if err := s.SettleUsageReservation(ctx, first, "committed", 70, []byte("known"), 0); err != nil {
			t.Fatal(err)
		}
	}
	os.Stdout.WriteString("kill-ready\n")
	var input [1]byte
	os.Stdin.Read(input[:])
	os.Exit(2)
}

func TestRuntimeFragmentSIGKILLRecovery(t *testing.T) {
	for _, stage := range []string{"first", "all", "settled-first"} {
		t.Run(stage, func(t *testing.T) {
			s, scope, path := runtimeStore(t)
			ctx := context.Background()
			a, err := s.OpenRuntimeSession(ctx, scope, "session", "flow")
			if err != nil {
				t.Fatal(err)
			}
			raw, err := json.Marshal(a.Use)
			if err != nil {
				t.Fatal(err)
			}
			if err := s.Close(); err != nil {
				t.Fatal(err)
			}
			binary, err := os.Executable()
			if err != nil {
				t.Fatal(err)
			}
			childCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
			defer cancel()
			cmd := exec.CommandContext(childCtx, binary, "-test.run=^TestRuntimeFragmentCrashWorker$")
			cmd.Env = append(os.Environ(), "VORTEX_FRAGMENT_CRASH_DB="+path, "VORTEX_FRAGMENT_CRASH_STAGE="+stage, "VORTEX_FRAGMENT_USE="+string(raw))
			stdout, err := cmd.StdoutPipe()
			if err != nil {
				t.Fatal(err)
			}
			stdin, err := cmd.StdinPipe()
			if err != nil {
				t.Fatal(err)
			}
			defer stdin.Close()
			if err := cmd.Start(); err != nil {
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
				err := cmd.Wait()
				t.Fatalf("child failed before checkpoint boundary: %v %v", err, scan.Err())
			}
			if err := cmd.Process.Kill(); err != nil {
				t.Fatal(err)
			}
			_ = cmd.Wait()
			recovered, err := Open(path, "agent")
			if err != nil {
				t.Fatal(err)
			}
			defer recovered.Close()
			committed, residual, remaining, wantCount := uint64(0), uint64(100), uint64((1<<20)-100), 1
			if stage == "all" {
				residual, remaining, wantCount = 200, (1<<20)-200, 2
			}
			if stage == "settled-first" {
				committed, residual, remaining = 70, 100, (1<<20)-170
			}
			pending, err := recovered.UnresolvedUsageReservations(ctx, "node", "next-boot")
			if err != nil || len(pending) != wantCount {
				t.Fatalf("pending %+v %v", pending, err)
			}
			var bound uint64
			for _, r := range pending {
				bound += r.Reserved
				if err := recovered.SettleUsageReservation(ctx, r, "unknown", r.Reserved, []byte("unknown-"+r.BatchID), r.Reserved); err != nil {
					t.Fatal(err)
				}
			}
			if bound != residual {
				t.Fatalf("recovery bound=%d want=%d", bound, residual)
			}
			balance, err := recovered.AuditAuthorizationSource(ctx, "node", "funding")
			if err != nil || balance.Committed != committed || balance.Unknown != residual || balance.Reserved != 0 || balance.Remaining != remaining {
				t.Fatalf("fragment crash conservation %+v %v", balance, err)
			}
		})
	}
}
