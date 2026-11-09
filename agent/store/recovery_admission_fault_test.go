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

func TestRecoveryAdmissionCrashWorker(t *testing.T) {
	path := os.Getenv("VORTEX_ADMISSION_CHILD_DB")
	if path == "" {
		return
	}
	s, e := Open(path, "hub")
	if e != nil {
		t.Fatal(e)
	}
	raw := []byte(os.Getenv("VORTEX_ADMISSION_CHILD_PAYLOAD"))
	p, e := DecodeHubRecoveryAdmission(raw)
	if e != nil {
		t.Fatal(e)
	}
	if os.Getenv("VORTEX_ADMISSION_CHILD_STAGE") == "after" {
		if _, e = s.ApplyHubRecoveryAdmission(context.Background(), p.BackendID, p.HubID, p.BootID, raw, IssuerTime{Earliest: p.ObservedAt, Latest: p.ObservedAt.Add(time.Second), Trusted: true}); e != nil {
			t.Fatal(e)
		}
	}
	os.Stdout.WriteString("kill-ready\n")
	var input [1]byte
	os.Stdin.Read(input[:])
	os.Exit(2)
}

func TestRecoveryAdmissionSIGKILLKeepsFiniteFULLBarrierAndOriginalDebt(t *testing.T) {
	for _, stage := range []string{"before", "after"} {
		t.Run(stage, func(t *testing.T) {
			s, d, p, clock, path := recoveryAdmissionFixture(t)
			ctx := context.Background()
			before, e := s.AuditHubDelegation(ctx, d.ID)
			if e != nil {
				t.Fatal(e)
			}
			if e = s.Close(); e != nil {
				t.Fatal(e)
			}
			raw, _ := json.Marshal(p)
			binary, e := os.Executable()
			if e != nil {
				t.Fatal(e)
			}
			child, cancel := context.WithTimeout(ctx, 20*time.Second)
			defer cancel()
			cmd := exec.CommandContext(child, binary, "-test.run=^TestRecoveryAdmissionCrashWorker$")
			cmd.Env = append(os.Environ(), "VORTEX_ADMISSION_CHILD_DB="+path, "VORTEX_ADMISSION_CHILD_PAYLOAD="+string(raw), "VORTEX_ADMISSION_CHILD_STAGE="+stage)
			stdout, e := cmd.StdoutPipe()
			if e != nil {
				t.Fatal(e)
			}
			stdin, e := cmd.StdinPipe()
			if e != nil {
				t.Fatal(e)
			}
			defer stdin.Close()
			if e = cmd.Start(); e != nil {
				t.Fatal(e)
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
				t.Fatal("admission worker failed before crash point", scan.Err())
			}
			if e = cmd.Process.Kill(); e != nil {
				t.Fatal(e)
			}
			cmd.Wait()
			reopened, e := Open(path, "hub")
			if e != nil {
				t.Fatal(e)
			}
			defer reopened.Close()
			var confirmed, completed, admissions int
			if e = reopened.db.QueryRow(`SELECT (SELECT count(*) FROM hub_ledger WHERE confirmed=1),(SELECT sum(completed) FROM hub_recoveries),(SELECT count(*) FROM hub_recovery_admissions)`).Scan(&confirmed, &completed, &admissions); e != nil {
				t.Fatal(e)
			}
			want := 0
			if stage == "after" {
				want = 1
			}
			if confirmed != want*int(p.SealedSequence+1) || completed != want || admissions != want {
				t.Fatal("partial FULL admission survived SIGKILL", confirmed, completed, admissions)
			}
			if _, e = reopened.ApplyHubRecoveryAdmission(ctx, p.BackendID, p.HubID, p.BootID, raw, clock); e != nil {
				t.Fatal(e)
			}
			after, e := reopened.AuditHubDelegation(ctx, d.ID)
			if e != nil || before != after {
				t.Fatal("SIGKILL/replay refilled original R or released old debt", before, after, e)
			}
		})
	}
}
