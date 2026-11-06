package phase5_test

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"testing"
	"time"
	"vortex/agent"
	"vortex/agent/upgrade"
	"vortex/engine/runtime/config"
)

func TestUpgradeProcessRollback(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("requires same-kernel Linux process termination witness; required Linux gates reject skipped scenarios")
	}
	binaries := t.TempDir()
	for _, name := range []string{"vortex-node", "vortex-test-hub"} {
		cmd := exec.Command("go", "build", "-o", filepath.Join(binaries, name), "./cmd/"+name)
		cmd.Dir = "../.."
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("build %v %s", err, out)
		}
	}
	candidate := filepath.Join(binaries, "candidate")
	cmd := exec.Command("go", "build", "-ldflags=-buildid=phase5-upgrade-candidate", "-o", candidate, "./cmd/vortex-node")
	cmd.Dir = "../.."
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build candidate %v %s", err, out)
	}
	for _, mode := range []string{"upgrade-healthy", "upgrade-rollback"} {
		t.Run(mode, func(t *testing.T) { regionalTrafficContinuity(t, binaries, mode) })
	}
}

type nodeProcessService struct {
	current         *exec.Cmd
	start           func(string, ...string) *exec.Cmd
	args            []string
	brokenCandidate bool
}

func (s *nodeProcessService) StopAndWait(ctx context.Context) error {
	if s.current == nil || s.current.ProcessState != nil {
		return nil
	}
	if err := s.current.Process.Signal(syscall.SIGTERM); err != nil && !errors.Is(err, os.ErrProcessDone) {
		return err
	}
	done := make(chan error, 1)
	go func() { done <- s.current.Wait() }()
	select {
	case <-done:
		if !s.current.ProcessState.Exited() {
			return errors.New("process termination unproven")
		}
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
func (s *nodeProcessService) Start(ctx context.Context, path string) error {
	if s.current != nil && s.current.ProcessState == nil {
		return errors.New("overlapping writer refused")
	}
	args := append([]string{}, s.args...)
	if s.brokenCandidate && !strings.Contains(path, "/old/") {
		args = append(args, "--engine-config", "/missing-candidate-runtime.json")
	}
	s.current = s.start(path, args...)
	return ctx.Err()
}
func exerciseUpgradeProcess(t *testing.T, ctx context.Context, binaries, mode, database string, peer config.Config, args []string, initial *exec.Cmd, exchange func(string), start func(string, ...string) *exec.Cmd) {
	t.Helper()
	exchange("confirmed-before-upgrade")
	auditBefore, err := sql.Open("sqlite", "file:"+database+"?mode=ro")
	if err != nil {
		t.Fatal(err)
	}
	original := map[string]string{}
	for len(original) == 0 {
		rows, err := auditBefore.QueryContext(ctx, "SELECT batch_id,payload_sha256 FROM usage_batches ORDER BY batch_id")
		if err != nil {
			t.Fatal(err)
		}
		for rows.Next() {
			var id string
			var digest []byte
			if err = rows.Scan(&id, &digest); err != nil {
				t.Fatal(err)
			}
			original[id] = hex.EncodeToString(digest)
		}
		if err = rows.Err(); err != nil {
			t.Fatal(err)
		}
		rows.Close()
		if len(original) > 0 {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal("confirmed traffic never durably settled")
		case <-time.After(10 * time.Millisecond):
		}
	}
	auditBefore.Close()
	root := t.TempDir()
	old := filepath.Join(root, "releases/old/vortex-node")
	os.MkdirAll(filepath.Dir(old), 0700)
	oldBytes, err := os.ReadFile(filepath.Join(binaries, "vortex-node"))
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(old, oldBytes, 0755); err != nil {
		t.Fatal(err)
	}
	active := filepath.Join(root, "current")
	if err = os.Symlink("releases/old/vortex-node", active); err != nil {
		t.Fatal(err)
	}
	socket, err := agent.DefaultAdminSocket(database)
	if err != nil {
		t.Fatal(err)
	}
	before, err := agent.AdminCall(ctx, socket, "status", time.Second)
	if err != nil {
		t.Fatal(err)
	}
	service := &nodeProcessService{current: initial, start: start, args: args, brokenCandidate: mode == "upgrade-rollback"}
	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	artifact, err := os.ReadFile(filepath.Join(binaries, "candidate"))
	if err != nil {
		t.Fatal(err)
	}
	probe := upgrade.ForwardingProbe{Peer: peer, Request: []byte("upgraded-TCP-health"), Response: []byte("upgraded-TCP-health")}
	installer := &upgrade.Installer{Root: root, ActiveLink: active, PublicKey: public, Service: service, DrainTimeout: 100 * time.Millisecond, HealthTimeout: 6 * time.Second}
	installer.CheckCompatibility = func(ctx context.Context, artifact []byte) error {
		// Both actual binaries inspect the existing schema in read-only mode.
		path := filepath.Join(root, "preflight-candidate")
		if err := os.WriteFile(path, artifact, 0700); err != nil {
			return err
		}
		for _, binary := range []string{old, path} {
			out, err := exec.CommandContext(ctx, binary, "upgrade-preflight", "--db", database).CombinedOutput()
			if err != nil {
				return errors.New(string(out))
			}
		}
		return nil
	}
	installer.SnapshotCompatible = func(context.Context) ([]byte, error) { return []byte("no-settings-changed"), nil }
	installer.RestoreCompatible = func(_ context.Context, snapshot []byte) error {
		if string(snapshot) != "no-settings-changed" {
			return errors.New("unapproved snapshot")
		}
		return nil
	}
	installer.IrreversibleMigration = func(context.Context) (bool, error) { return false, nil }
	installer.Drain = func(ctx context.Context) error {
		_, err := agent.AdminCall(ctx, socket, "drain", 100*time.Millisecond)
		return err
	}
	var verifiedBoot string
	installer.Health = func(ctx context.Context, path string) error {
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		sum := sha256.Sum256(raw)
		for {
			status, err := agent.AdminCall(ctx, socket, "status", time.Second)
			if err == nil && status.BootID != before.BootID && status.HubMode == "online" && status.DurableStore && status.AppliedVersion != "0" && status.BuildSHA256 == fmtDigest(sum) {
				if err = probe.Check(ctx); err == nil {
					verifiedBoot = status.BootID
					return nil
				}
			}
			select {
			case <-ctx.Done():
				audit, _ := sql.Open("sqlite", "file:"+database+"?mode=ro")
				if audit != nil {
					rows, e := audit.Query("SELECT version,fencing_generation,state,reason FROM config_results")
					if e == nil {
						for rows.Next() {
							var v, f, st, reason string
							rows.Scan(&v, &f, &st, &reason)
							t.Logf("config %s fence%s %s %s", v, f, st, reason)
						}
						rows.Close()
					}
					rows, e = audit.Query("SELECT boot_id,kind,state,authorization_id FROM authorization_records")
					if e == nil {
						for rows.Next() {
							var boot, kind, state, id string
							rows.Scan(&boot, &kind, &state, &id)
							t.Logf("authorization boot=%s %s %s %s", boot, kind, state, id)
						}
						rows.Close()
					}
					audit.Close()
				}
				return fmt.Errorf("health status=%+v err=%v: %w", status, err, ctx.Err())
			case <-time.After(30 * time.Millisecond):
			}
		}
	}
	request := upgrade.InstallRequest{TransactionID: "70112233-4455-6677-8899-aabbccddeeff", Artifact: artifact, Signature: ed25519.Sign(private, artifact), SHA256: sha256.Sum256(artifact)}
	err = installer.Install(ctx, request)
	if mode == "upgrade-healthy" && err != nil {
		t.Fatal(err)
	}
	if mode == "upgrade-rollback" && err == nil {
		t.Fatal("unhealthy candidate reported success")
	}
	raw, readErr := os.ReadFile(filepath.Join(root, "transaction.json"))
	if readErr != nil {
		t.Fatal(readErr)
	}
	var state upgrade.InstallState
	if readErr = json.Unmarshal(raw, &state); readErr != nil {
		t.Fatal(readErr)
	}
	expected := "healthy"
	if mode == "upgrade-rollback" {
		expected = "rolled-back"
	}
	if state.State != expected || verifiedBoot == "" || verifiedBoot == before.BootID {
		t.Fatalf("unproven current service %+v boot=%s err=%v", state, verifiedBoot, err)
	}
	audit, err := sql.Open("sqlite", "file:"+database+"?mode=ro")
	if err != nil {
		t.Fatal(err)
	}
	defer audit.Close()
	var upper, liability, origin string
	if err = audit.QueryRowContext(ctx, "SELECT unknown_upper_bound,liability_state,origin_boot_id FROM unknown_usage_batches WHERE batch_id='command-debt'").Scan(&upper, &liability, &origin); err != nil || upper != "512" || liability != "unresolved" || origin != regionalBoot {
		t.Fatalf("upgrade changed liability %s %s %s %v", upper, liability, origin, err)
	}
	for id, digest := range original {
		var after []byte
		if err = audit.QueryRowContext(ctx, "SELECT payload_sha256 FROM usage_batches WHERE batch_id=?", id).Scan(&after); err != nil || hex.EncodeToString(after) != digest {
			t.Fatalf("confirmed consumption rolled back %s %v", id, err)
		}
	}
	var floor uint64
	if err = audit.QueryRowContext(ctx, "SELECT value FROM agent_state WHERE key='authorization_fencing_floor'").Scan(&floor); err != nil || floor < 9 {
		t.Fatalf("fencing floor decreased %d %v", floor, err)
	}
	var restoredOld int
	if err = audit.QueryRowContext(ctx, "SELECT count(*) FROM authorization_records WHERE boot_id=? AND state='active'", before.BootID).Scan(&restoredOld); err != nil || restoredOld != 0 {
		t.Fatalf("old boot authority revived %d %v", restoredOld, err)
	}
	t.Logf("real node %s verified fresh boot %s, FULL/WAL, current authority and TCP response; unknown512 retained", expected, verifiedBoot)
}
func fmtDigest(sum [32]byte) string {
	const hex = "0123456789abcdef"
	out := make([]byte, 64)
	for i, b := range sum {
		out[2*i] = hex[b>>4]
		out[2*i+1] = hex[b&15]
	}
	return string(out)
}
