package store

import (
	"bufio"
	"context"
	"os"
	"os/exec"
	"testing"
	"time"
)

func checkpointCrashEntries() []RuntimeBlockCheckpoint {
	block := runtimeReservation("block", 1000)
	tail := block
	tail.Reserved = 900
	return []RuntimeBlockCheckpoint{{block, "one", 100, []byte("first")}, {tail, "two", 200, []byte("second")}}
}

func TestRuntimeBlockCheckpointCrashWorker(t *testing.T) {
	path := os.Getenv("VORTEX_BLOCK_CRASH_DB")
	if path == "" {
		return
	}
	s, err := Open(path, "agent")
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	entries := checkpointCrashEntries()
	if os.Getenv("VORTEX_BLOCK_CRASH_STAGE") == "after" {
		if err := s.CheckpointRuntimeFundingBlocks(ctx, entries); err != nil {
			t.Fatal(err)
		}
	} else {
		tx, err := s.db.BeginTx(ctx, nil)
		if err != nil {
			t.Fatal(err)
		}
		// Kill after both children's mutations, but before their shared commit.
		for _, e := range entries {
			if err := checkpointRuntimeFundingBlockTx(ctx, tx, e.Block, e.CheckpointID, e.Used, e.Payload); err != nil {
				t.Fatal(err)
			}
		}
	}
	os.Stdout.WriteString("kill-ready\n")
	var input [1]byte
	os.Stdin.Read(input[:])
	os.Exit(2)
}

func TestRuntimeBlockCheckpointSIGKILLRecovery(t *testing.T) {
	for _, stage := range []string{"before", "after"} {
		t.Run(stage, func(t *testing.T) {
			s, scope, path := runtimeStore(t)
			ctx := context.Background()
			a, err := s.OpenRuntimeSession(ctx, scope, "session", "flow")
			if err != nil {
				t.Fatal(err)
			}
			if _, err := s.ReserveRuntimeFundingBlock(ctx, runtimeReservation("block", 1000), a.Use); err != nil {
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
			cmd := exec.CommandContext(childCtx, binary, "-test.run=^TestRuntimeBlockCheckpointCrashWorker$")
			cmd.Env = append(os.Environ(), "VORTEX_BLOCK_CRASH_DB="+path, "VORTEX_BLOCK_CRASH_STAGE="+stage)
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
			committed, residual, count := uint64(0), uint64(1000), 0
			if stage == "after" {
				committed, residual, count = 300, 700, 2
			}
			pending, err := recovered.UnresolvedUsageReservations(ctx, "node", "next-boot")
			if err != nil || len(pending) != 1 || pending[0].Reserved != residual {
				t.Fatalf("recovery bound: %+v %v", pending, err)
			}
			var batches int
			if err := recovered.db.QueryRow(`SELECT COUNT(*) FROM usage_batches`).Scan(&batches); err != nil || batches != count {
				t.Fatalf("partial outbox: %d %v", batches, err)
			}
			if err := recovered.SettleUsageReservation(ctx, pending[0], "unknown", residual, []byte("unknown"), residual); err != nil {
				t.Fatal(err)
			}
			b, err := recovered.AuditAuthorizationSource(ctx, "node", "funding")
			if err != nil || b.Committed != committed || b.Unknown != residual || b.Reserved != 0 || b.Remaining != (1<<20)-1000 {
				t.Fatalf("crash conservation: %+v %v", b, err)
			}
			err = recovered.CheckpointRuntimeFundingBlocks(ctx, checkpointCrashEntries())
			if (stage == "after" && err != nil) || (stage == "before" && err == nil) {
				t.Fatalf("post-recovery replay: %v", err)
			}
		})
	}
}
