package phase5_test

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
	"vortex/agent"
	"vortex/engine/runtime/config"
	"vortex/engine/vlink"
)

func TestNodeCommandsDrain(t *testing.T) {
	binaries := t.TempDir()
	for _, name := range []string{"vortex-node", "vortex-test-hub"} {
		cmd := exec.Command("go", "build", "-o", filepath.Join(binaries, name), "./cmd/"+name)
		cmd.Dir = "../.."
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("build %s: %v\n%s", name, err, out)
		}
	}
	for _, mode := range []string{"commands-grace", "commands-force", "commands-restart"} {
		t.Run(mode, func(t *testing.T) { regionalTrafficContinuity(t, binaries, mode) })
	}
}
func exerciseNodeCommandDrain(t *testing.T, ctx context.Context, binaries, mode, database string, cfg config.Config, mux *vlink.MuxLink, stream *vlink.MuxStream, exchange func(string), restart func()) {
	t.Helper()
	defer func() {
		db, err := sql.Open("sqlite", "file:"+database+"?mode=ro")
		if err != nil {
			t.Fatal(err)
		}
		defer db.Close()
		var upper, state, origin string
		err = db.QueryRowContext(ctx, "SELECT unknown_upper_bound,liability_state,origin_boot_id FROM unknown_usage_batches WHERE batch_id='command-debt'").Scan(&upper, &state, &origin)
		if err != nil || upper != "512" || state != "unresolved" || origin != regionalBoot {
			t.Errorf("lifecycle changed debt %s %s %s %v", upper, state, origin, err)
		}
	}()
	socket, err := agent.DefaultAdminSocket(database)
	if err != nil {
		t.Fatal(err)
	}
	status := func() agent.NodeStatus {
		t.Helper()
		cmd := exec.CommandContext(ctx, filepath.Join(binaries, "vortex-node"), "status", "--admin-socket", socket)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("status command: %v %s", err, out)
		}
		var s agent.NodeStatus
		if err = json.Unmarshal(out, &s); err != nil {
			t.Fatal(err)
		}
		return s
	}
	first := status()
	if first.NodeID != regionalNode || first.ActiveSessions != 1 || first.DrainState != "running" || first.HubMode != "online" {
		t.Fatalf("unexpected current status %+v", first)
	}
	// The second writer must lose before SQLite boot/migrations can mutate state.
	duplicate := exec.CommandContext(ctx, filepath.Join(binaries, "vortex-node"), "run", "--node-id", regionalNode, "--db", database)
	if out, err := duplicate.CombinedOutput(); err == nil {
		t.Fatalf("second writer succeeded: %s", out)
	}
	if status().BootID != first.BootID {
		t.Fatal("second writer changed current boot")
	}
	if mode == "commands-restart" {
		restart()
		for {
			callCtx, cancel := context.WithTimeout(ctx, 100*time.Millisecond)
			s, err := agent.AdminCall(callCtx, socket, "status", time.Second)
			cancel()
			if err == nil && s.BootID != first.BootID && s.DrainState == "running" {
				break
			}
			select {
			case <-ctx.Done():
				t.Fatal("fresh process did not recover stale socket/current boot")
			case <-time.After(20 * time.Millisecond):
			}
		}
		return
	}
	if mode == "commands-force" {
		sum := sha256.Sum256([]byte(cfg.RuleID))
		var rule [16]byte
		copy(rule[:], sum[:16])
		udp, err := mux.Open(ctx, vlink.OpenRequest{RuleID: rule, RuleRevision: cfg.RuleRevision, PathGeneration: cfg.PathGeneration, Network: 2, ClientAddress: []byte{127, 0, 0, 1}})
		if err != nil {
			t.Fatal(err)
		}
		if err = udp.WaitOpen(ctx); err != nil {
			t.Fatal(err)
		}
		if ok, err := udp.WriteDgram(ctx, []byte("UDP-before-force")); !ok || err != nil {
			t.Fatalf("UDP send %v %v", ok, err)
		}
		reply, err := udp.Receive(ctx)
		if err != nil {
			t.Fatal(err)
		}
		reply.Release()
	}
	exchange("before-command-drain")
	timeout := "2s"
	if mode == "commands-force" {
		timeout = "100ms"
	}
	drain := exec.CommandContext(ctx, filepath.Join(binaries, "vortex-node"), "drain", "--admin-socket", socket, "--drain-timeout", timeout)
	result := make(chan struct {
		out []byte
		err error
	}, 1)
	go func() {
		out, err := drain.CombinedOutput()
		result <- struct {
			out []byte
			err error
		}{out, err}
	}()
	for {
		state := status().DrainState
		if state == "draining" || state == "drained" {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal("drain did not start")
		case <-time.After(10 * time.Millisecond):
		}
	}
	if mode == "commands-grace" {
		var rule [16]byte
		sum := sha256.Sum256([]byte(cfg.RuleID))
		copy(rule[:], sum[:16])
		rejected, err := mux.Open(ctx, vlink.OpenRequest{RuleID: rule, RuleRevision: cfg.RuleRevision, PathGeneration: cfg.PathGeneration, Network: 1, ClientAddress: []byte{127, 0, 0, 1}})
		if err == nil {
			if err = rejected.WaitOpen(ctx); err == nil {
				t.Fatal("drain admitted new OPEN on established mux")
			}
		}
		exchange("old-TCP-during-command-drain")
		if err = stream.CloseWrite(ctx); err != nil {
			t.Fatal(err)
		}
		frame, err := stream.Receive(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer frame.Release()
		if frame.Type != vlink.FrameFIN {
			t.Fatalf("half-close response %v", frame.Type)
		}
	}
	select {
	case done := <-result:
		if done.err != nil {
			t.Fatalf("drain command failed: %v %s", done.err, done.out)
		}
		var s agent.NodeStatus
		if err := json.Unmarshal(done.out, &s); err != nil || s.ActiveSessions != 0 || s.DrainState != "drained" {
			t.Fatalf("drain result %s err=%v", done.out, err)
		}
	case <-ctx.Done():
		t.Fatal("bounded drain never completed")
	}
	if mode == "commands-force" {
		select {
		case <-mux.Closed():
		case <-ctx.Done():
			t.Fatal("force drain left residual mux alive")
		}
	}
	again := exec.CommandContext(ctx, filepath.Join(binaries, "vortex-node"), "drain", "--admin-socket", socket, "--drain-timeout", "1s")
	if out, err := again.CombinedOutput(); err != nil {
		t.Fatalf("duplicate drain failed: %v %s", err, out)
	}
}
