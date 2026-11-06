package phase5_test

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"vortex/agent/store"
	"vortex/engine/runtime/config"
	"vortex/engine/vlink"
)

// Actual shipped node/Hub entry points, real VLink TCP and UDP, finite roots
// provisioned explicitly before launch. This is still a protocol backend
// fixture, not Phase 6 production provisioning or performance qualification.
func TestSchedulerProcessRenewal(t *testing.T) {
	binaries := t.TempDir()
	for _, name := range []string{"vortex-node", "vortex-test-hub"} {
		cmd := exec.Command("go", "build", "-o", filepath.Join(binaries, name), "./cmd/"+name)
		cmd.Dir = "../.."
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("build %s: %v\n%s", name, err, out)
		}
	}
	for _, scenario := range []string{"low-water-tcp-udp", "lost-result", "hub-restart", "node-restart", "quota-exhausted", "write-failure"} {
		t.Run(scenario, func(t *testing.T) { regionalTrafficContinuity(t, binaries, scenario) })
	}
}

func schedulerRenewalTraffic(t *testing.T, ctx context.Context, db *sql.DB, dir string, cfg config.Config, mux *vlink.MuxLink, tcp *vlink.MuxStream, exchange func(string), fault func() bool) {
	t.Helper()
	payload := strings.Repeat("r", 512)
	// Six request+echo pairs consume 6144 of the initial 8192-byte grant.
	// The original TCP stream stays open while the control loop renews it.
	for i := 0; i < 6; i++ {
		exchange(payload)
	}
	if fault() {
		return
	}
	for {
		var installed int
		err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM renewal_requests WHERE state='installed' AND trigger_authorization_id<>''`).Scan(&installed)
		if err == nil && installed > 0 {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal("automatic low-water renewal never installed")
		case <-time.After(20 * time.Millisecond):
		}
	}
	for i := 0; i < 4; i++ {
		exchange(payload)
	} // 10240 bytes > original grant
	var rule [16]byte
	hash := sha256.Sum256([]byte(cfg.RuleID))
	copy(rule[:], hash[:16])
	udp, err := mux.Open(ctx, vlink.OpenRequest{RuleID: rule, RuleRevision: cfg.RuleRevision, PathGeneration: cfg.PathGeneration, Network: 2, ClientAddress: []byte{127, 0, 0, 1}})
	if err != nil {
		t.Fatal(err)
	}
	defer udp.Reset(ctx, 0)
	if err := udp.WaitOpen(ctx); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 4; i++ {
		accepted, err := udp.WriteDgram(ctx, []byte(payload))
		if err != nil || !accepted {
			t.Fatalf("UDP continuation send: %t %v", accepted, err)
		}
		frame, err := udp.Receive(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if frame.Type != vlink.FrameDgram || string(frame.Payload) != payload {
			frame.Release()
			t.Fatal("UDP continuation changed payload")
		}
		if err := udp.Consume(ctx, uint32(len(frame.Payload))); err != nil {
			frame.Release()
			t.Fatal(err)
		}
		frame.Release()
	}
	// Both authoritative local stores must account for every issued grant; a
	// lost response does not duplicate debit. Audit reads transactional snapshots.
	ns, err := store.Open(filepath.Join(dir, "agent.db"), "agent")
	if err != nil {
		t.Fatal(err)
	}
	defer ns.Close()
	hs, err := store.Open(filepath.Join(dir, "hub0.db"), "hub")
	if err != nil {
		t.Fatal(err)
	}
	defer hs.Close()
	var delegation string
	if err := db.QueryRowContext(ctx, `SELECT delegation_id FROM authorization_records WHERE kind='credit_grant' LIMIT 1`).Scan(&delegation); err != nil {
		t.Fatal(err)
	}
	hubBalance, err := hs.AuditHubDelegation(ctx, delegation)
	if err != nil || hubBalance.Initial != 65536 || hubBalance.Issued < 16384 || hubBalance.Remaining+hubBalance.Issued != 65536 {
		t.Fatalf("Hub conservation: %+v %v", hubBalance, err)
	}
	var sourceBalance store.AuthorizationBalance
	for {
		sourceBalance, err = ns.AuditAuthorizationSource(ctx, regionalNode, regionalFirst)
		if err != nil {
			t.Fatal(err)
		}
		if sourceBalance.Committed >= 14336 {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatalf("missing TCP/UDP accounting: %+v", sourceBalance)
		case <-time.After(10 * time.Millisecond):
		}
	}
	if sourceBalance.Initial != hubBalance.Issued || sourceBalance.Initial != sourceBalance.Remaining+sourceBalance.Committed+sourceBalance.Unknown+sourceBalance.Reserved {
		t.Fatalf("Agent/Hub source mismatch: agent=%+v hub=%+v", sourceBalance, hubBalance)
	}
	var terminal, issues int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM renewal_requests WHERE state='installed'`).Scan(&terminal); err != nil {
		t.Fatal(err)
	}
	hdb, err := sql.Open("sqlite", filepath.Join(dir, "hub0.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer hdb.Close()
	if err := hdb.QueryRowContext(ctx, `SELECT COUNT(*) FROM issuer_issues`).Scan(&issues); err != nil {
		t.Fatal(err)
	}
	if issues != terminal+1 {
		t.Fatalf("duplicate response spent a second issue: issues=%d installed=%d", issues, terminal)
	}
	t.Logf("actual TCP/UDP continued beyond initial grant; Agent=%+v Hub=%+v installed=%d issues=%d", sourceBalance, hubBalance, terminal, issues)
	_ = tcp // the same TCP stream is deliberately retained throughout
}
