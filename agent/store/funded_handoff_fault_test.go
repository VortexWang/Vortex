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

func TestFundedHandoffSIGKILLKeepsCompleteOriginalInventory(t *testing.T) {
	for _, stage := range []string{"before", "after"} {
		t.Run(stage, func(t *testing.T) {
			s, d, clock, path := renewalIssuerFixture(t)
			ctx := context.Background()
			if _, err := s.IssueHubAuthorization(ctx, issuerRequest(d, 920), clock); err != nil {
				t.Fatal(err)
			}
			h, clock := fundedHandoffRequest(t, d)
			if err := s.Close(); err != nil {
				t.Fatal(err)
			}
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
				_ = cmd.Wait()
				t.Fatalf("crash worker did not commit: %v", scan.Err())
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
			var roots int
			if err = recovered.db.QueryRow(`SELECT count(*) FROM share_source_anchors`).Scan(&roots); err != nil {
				t.Fatal(err)
			}
			want := 3
			if stage == "after" {
				want = 6
			}
			if roots != want {
				t.Fatalf("partial roots %d want %d", roots, want)
			}
			r, err := recovered.HandoffHub(ctx, h, clock)
			if err != nil || r.FundedInventory == nil || r.FundedInventory.Delegations[0].Issued != 256 || r.FundedInventory.Delegations[0].Remaining != 768 {
				t.Fatalf("lost manifest %+v %v", r, err)
			}
			if _, err = recovered.IssueHubAuthorization(ctx, issuerRequest(d, 921), clock); err == nil {
				t.Fatal("crash bypassed finite ACK barrier")
			}
			completeFundedHandoff(t, recovered, clock)
			if _, err = recovered.IssueHubAuthorization(ctx, issuerRequest(d, 921), clock); err != nil {
				t.Fatal(err)
			}
			b, err := recovered.AuditHubDelegation(ctx, d.ID)
			if err != nil || b.Issued != 512 || b.Remaining != 512 {
				t.Fatalf("crash refilled R %+v %v", b, err)
			}
		})
	}
}
