package service_test

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"
	"vortex/agent/store"
	"vortex/backend/internal/worker"
)

func TestShareHandoffSIGKILLChild(t *testing.T) {
	if os.Getenv("VORTEX_SHARE_CRASH_CHILD") != "1" {
		return
	}
	st, e := store.Open(os.Getenv("VORTEX_SHARE_PATH"), "agent")
	if e != nil {
		t.Fatal(e)
	}
	defer st.Close()
	raw, e := base64.StdEncoding.DecodeString(os.Getenv("VORTEX_SHARE_COMMAND"))
	if e != nil {
		t.Fatal(e)
	}
	var now time.Time
	if e = json.Unmarshal([]byte(os.Getenv("VORTEX_SHARE_NOW")), &now); e != nil {
		t.Fatal(e)
	}
	_, e = st.ApplyShareHandoff(context.Background(), os.Getenv("VORTEX_SHARE_NODE"), os.Getenv("VORTEX_SHARE_BOOT"), shareBackendID, "cas", raw, store.IssuerTime{Earliest: now, Latest: now, Trusted: true})
	if e != nil {
		t.Fatal(e)
	}
	if e = os.WriteFile(os.Getenv("VORTEX_SHARE_READY"), []byte("durable CAS committed"), 0600); e != nil {
		t.Fatal(e)
	}
	// The parent observes the post-FULL-commit barrier, then kills the process.
	select {}
}
func TestShareHandoffFaultSIGKILLAfterDurableCASAndExpiredWorkerRecovery(t *testing.T) {
	x := newShareFixture(t)
	p := x.prepare(t)
	if e := x.s.QueueHandoff(context.Background(), x.f.scope, p.Generation.ID, uuid.NewString()); e != nil {
		t.Fatal(e)
	}
	task, d := leaseShareTask(t, x, p.Generation.ID)
	dir := t.TempDir()
	path := filepath.Join(dir, "node.db")
	ready := filepath.Join(dir, "committed")
	now, _ := json.Marshal(x.now.Add(time.Second))
	child := exec.Command(os.Args[0], "-test.run=^TestShareHandoffSIGKILLChild$", "-test.v")
	child.Env = append(os.Environ(), "VORTEX_SHARE_CRASH_CHILD=1", "VORTEX_SHARE_PATH="+path, "VORTEX_SHARE_COMMAND="+base64.StdEncoding.EncodeToString(d.Binding.Command), "VORTEX_SHARE_NOW="+string(now), "VORTEX_SHARE_NODE="+x.in.NodeID, "VORTEX_SHARE_BOOT="+x.in.BootID, "VORTEX_SHARE_READY="+ready)
	output, e := os.Create(filepath.Join(dir, "child.log"))
	if e != nil {
		t.Fatal(e)
	}
	defer output.Close()
	child.Stdout = output
	child.Stderr = output
	if e = child.Start(); e != nil {
		t.Fatal(e)
	}
	defer func() { child.Process.Kill(); child.Wait() }()
	deadline := time.Now().Add(10 * time.Second)
	observed := false
	for time.Now().Before(deadline) {
		if _, e = os.Stat(ready); e == nil {
			observed = true
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if e = child.Process.Kill(); e != nil {
		t.Fatal(e)
	}
	child.Wait()
	if !observed {
		raw, _ := os.ReadFile(filepath.Join(dir, "child.log"))
		t.Fatal("child never committed SQLite CAS", string(raw))
	}
	if c := shareChain(t, x); c.ActiveGeneration != 0 || c.HeldAmount != 600000 {
		t.Fatal("lost receipt became success/release", c)
	}
	// Accelerate only this task's lease boundary; production Recover reads the
	// persisted expiry and uses the exact worker token. No authority deadline is
	// changed and no release proof is fabricated.
	if _, e = x.f.pool.Exec(context.Background(), "UPDATE tasks SET lease_until=now()-interval '1 second' WHERE tenant_id=$1 AND id=$2", x.f.tenant, task); e != nil {
		t.Fatal(e)
	}
	w := worker.ShareReconciliationWorker{Service: x.s}
	if e = w.Recover(context.Background()); e != nil {
		t.Fatal(e)
	}
	st, e := store.Open(path, "agent")
	if e != nil {
		t.Fatal(e)
	}
	defer st.Close()
	r := confirmHandoff(t, x, st, p.Generation.ID)
	if r.MinimumGeneration != 1 || r.ActiveGeneration != 1 || shareChain(t, x).ActiveGeneration != 1 {
		t.Fatal("restart did not query actual durable gate", r)
	}
}
