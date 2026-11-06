//go:build nodecontainer

package phase5_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
	"vortex/agent"
	"vortex/agent/store"
	"vortex/contracts"
	controlv1 "vortex/contracts/protobuf/v1"
)

// This separate required deployment gate uses the real image, named volume,
// non-root PID 1, SIGTERM and SIGKILL. Traffic drain has its own real mux test.
func TestNodeContainerLifecycle(t *testing.T) {
	image := os.Getenv("VORTEX_NODE_TEST_IMAGE")
	if image == "" {
		t.Fatal("built image required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	docker := func(args ...string) []byte {
		t.Helper()
		out, err := exec.CommandContext(ctx, "docker", args...).CombinedOutput()
		if err != nil {
			t.Fatalf("docker %v: %v %s", args, err, out)
		}
		return out
	}
	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	name := "vortex-node-test-" + suffix
	volume := name + "-state"
	docker("volume", "create", volume)
	defer exec.Command("docker", "volume", "rm", "-f", volume).Run()
	defer exec.Command("docker", "rm", "-f", name).Run()
	seed := t.TempDir()
	db, err := store.Open(filepath.Join(seed, "agent.db"), "agent")
	if err != nil {
		t.Fatal(err)
	}
	payload, err := contracts.EncodeUnknownUsage(&controlv1.UnknownUsageBatch{BatchId: "container-debt", OriginBootId: regionalBoot, BillingSnapshotId: regionalBoot, RuleId: "rule", GrantId: regionalFirst, Role: "source", Direction: "egress", UnknownUpperBound: 512})
	if err != nil {
		t.Fatal(err)
	}
	if err = db.PersistUnknownUsage(ctx, regionalNode, payload); err != nil {
		t.Fatal(err)
	}
	db.Close()
	docker("run", "--rm", "-v", volume+":/state", "-v", seed+":/seed:ro", "golang:1.27.3@sha256:c292963ad0e5f51e843c726edc204a4e4c26a859ba1e4d736d91028aa3633048", "sh", "-c", "cp /seed/agent.db /state/agent.db && chown -R 65532:65532 /state && chmod 700 /state && chmod 600 /state/agent.db")
	docker("run", "-d", "--name", name, "-v", volume+":/state", image, "run", "--node-id", regionalNode, "--db", "/state/agent.db", "--admin-socket", "/state/run/admin.sock")
	status := func() agent.NodeStatus {
		t.Helper()
		for {
			out, err := exec.CommandContext(ctx, "docker", "exec", name, "/vortex-node", "status", "--admin-socket", "/state/run/admin.sock").CombinedOutput()
			var s agent.NodeStatus
			if err == nil && json.Unmarshal(out, &s) == nil {
				return s
			}
			select {
			case <-ctx.Done():
				t.Fatalf("status: %s", out)
			case <-time.After(100 * time.Millisecond):
			}
		}
	}
	first := status()
	var inspected []struct {
		Config struct{ User string }
		State  struct{ Pid int }
	}
	if err = json.Unmarshal(docker("inspect", name), &inspected); err != nil || inspected[0].Config.User != "65532:65532" || inspected[0].State.Pid == 0 {
		t.Fatal("non-root process not running")
	}
	docker("stop", "--time", "15", name)
	if code := string(docker("inspect", "--format", "{{.State.ExitCode}}", name)); code != "0\n" {
		t.Fatalf("SIGTERM exit %q", code)
	}
	docker("start", name)
	second := status()
	if second.BootID == first.BootID {
		t.Fatal("SIGTERM restart reused boot")
	}
	docker("kill", "--signal", "KILL", name)
	docker("start", name)
	third := status()
	if third.BootID == second.BootID {
		t.Fatal("SIGKILL restart reused boot")
	}
	docker("stop", "--time", "15", name)
	saved := t.TempDir()
	docker("cp", name+":/state/agent.db", filepath.Join(saved, "agent.db"))
	recovered, err := store.Open(filepath.Join(saved, "agent.db"), "agent")
	if err != nil {
		t.Fatal(err)
	}
	defer recovered.Close()
	records, err := recovered.PendingUnknownUsage(ctx, regionalNode)
	if err != nil || len(records) != 1 {
		t.Fatalf("debt lost %+v %v", records, err)
	}
	// Read the durable row independently: startup and transport are not release.
	audit, err := sql.Open("sqlite", "file:"+filepath.Join(saved, "agent.db")+"?mode=ro")
	if err != nil {
		t.Fatal(err)
	}
	defer audit.Close()
	rows, err := audit.QueryContext(ctx, "SELECT unknown_upper_bound,liability_state FROM unknown_usage_batches WHERE batch_id='container-debt'")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	if !rows.Next() {
		t.Fatal("missing debt")
	}
	var upper, state string
	if err = rows.Scan(&upper, &state); err != nil || upper != "512" || state != "unresolved" {
		t.Fatalf("debt changed %s %s %v", upper, state, err)
	}
	t.Logf("non-root PID1 SIGTERM exit0; fresh boots %s %s %s; unknown512 preserved", first.BootID, second.BootID, third.BootID)
}
