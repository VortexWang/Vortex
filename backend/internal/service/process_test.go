package service_test

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/google/uuid"
	"vortex/agent/store"
	"vortex/backend/internal/service"
	"vortex/backend/internal/testutil"
)

type certificateAuthority struct {
	certificate *x509.Certificate
	key         *ecdsa.PrivateKey
	pem         []byte
}

func newCA(t *testing.T) certificateAuthority {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 120))
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{SerialNumber: serial, Subject: pkix.Name{CommonName: uuid.NewString()}, NotBefore: time.Now().Add(-time.Minute), NotAfter: time.Now().Add(time.Hour), IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	return certificateAuthority{certificate: cert, key: key, pem: pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})}
}
func (c certificateAuthority) issue(t *testing.T, dir, name string) (string, string) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 120))
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{SerialNumber: serial, Subject: pkix.Name{CommonName: name}, DNSNames: []string{name}, NotBefore: time.Now().Add(-time.Minute), NotAfter: time.Now().Add(time.Hour), KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth}}
	der, err := x509.CreateCertificate(rand.Reader, template, c.certificate, &key.PublicKey, c.key)
	if err != nil {
		t.Fatal(err)
	}
	rawKey, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	certPath, keyPath := filepath.Join(dir, name+".pem"), filepath.Join(dir, name+".key")
	if err = os.WriteFile(certPath, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0600); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(keyPath, pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: rawKey}), 0600); err != nil {
		t.Fatal(err)
	}
	return certPath, keyPath
}
func freeAddress(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := ln.Addr().String()
	ln.Close()
	return address
}
func eventually(t *testing.T, description string, check func() bool) {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		if check() {
			return
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatalf("timeout waiting for %s", description)
}

func TestTracerRealProcesses(t *testing.T) {
	backend, peer := os.Getenv("PHASE6_BACKEND_BINARY"), os.Getenv("PHASE6_PEER_BINARY")
	if backend == "" || peer == "" {
		binaries := t.TempDir()
		backend = filepath.Join(binaries, "vortex-backend")
		peer = filepath.Join(binaries, "vortex-control-peer")
		for _, build := range []struct{ path, target string }{{backend, "./cmd/vortex-backend"}, {peer, "./cmd/vortex-control-peer"}} {
			cmd := exec.Command("go", "build", "-o", build.path, build.target)
			cmd.Dir = "../.."
			if output, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("build real process: %s %v", output, err)
			}
		}
	}
	pool := testutil.Postgres(t)
	ctx := context.Background()
	work := t.TempDir()
	evidence := os.Getenv("PHASE6_PROCESS_EVIDENCE_DIR")
	if evidence == "" {
		evidence = work
	}
	if err := os.MkdirAll(evidence, 0755); err != nil {
		t.Fatal(err)
	}
	stamp := uuid.NewString()
	start := func(name, binary string, args ...string) *exec.Cmd {
		t.Helper()
		file, err := os.Create(filepath.Join(evidence, stamp+"-"+name+".log"))
		if err != nil {
			t.Fatal(err)
		}
		cmd := exec.Command(binary, args...)
		cmd.Stdout = file
		cmd.Stderr = file
		if err = cmd.Start(); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = cmd.Process.Kill(); _ = cmd.Wait(); file.Close() })
		return cmd
	}
	backendID, nodeID, hubID := uuid.NewString(), uuid.NewString(), uuid.NewString()
	backendCA, nodeCA, hubCA := newCA(t), newCA(t), newCA(t)
	writeRoot := func(name string, raw []byte) string {
		path := filepath.Join(work, name)
		if err := os.WriteFile(path, raw, 0600); err != nil {
			t.Fatal(err)
		}
		return path
	}
	backendRoot, nodeRoot, hubRoot := writeRoot("backend-root.pem", backendCA.pem), writeRoot("node-root.pem", nodeCA.pem), writeRoot("hub-root.pem", hubCA.pem)
	backendCert, backendKey := backendCA.issue(t, work, backendID)
	nodeCert, nodeKey := nodeCA.issue(t, work, nodeID)
	hubCert, hubKey := hubCA.issue(t, work, hubID)
	nodeAddress, hubAddress := freeAddress(t), freeAddress(t)
	b := start("backend", backend, "serve", "--node-listen", nodeAddress, "--hub-listen", hubAddress, "--cert", backendCert, "--key", backendKey, "--node-ca", nodeRoot, "--hub-ca", hubRoot, "--backend-id", backendID, "--hub-id", hubID)
	eventually(t, "backend listeners", func() bool {
		conn, err := net.DialTimeout("tcp", nodeAddress, 50*time.Millisecond)
		if err != nil {
			return false
		}
		conn.Close()
		return true
	})
	agentDB, hubDB := filepath.Join(work, "agent.db"), filepath.Join(work, "hub.db")
	a := start("agent", peer, "--mode", "agent", "--id", nodeID, "--backend-id", backendID, "--address", nodeAddress, "--cert", nodeCert, "--key", nodeKey, "--ca", backendRoot, "--db", agentDB)
	h := start("hub", peer, "--mode", "hub", "--id", hubID, "--backend-id", backendID, "--address", hubAddress, "--cert", hubCert, "--key", hubKey, "--ca", backendRoot, "--db", hubDB)
	for _, id := range []string{nodeID, hubID} {
		eventually(t, "verified peer observation "+id, func() bool {
			var count int
			err := pool.QueryRow(ctx, "SELECT count(*) FROM control_observations WHERE hub_id=$1", id).Scan(&count)
			return err == nil && count == 1
		})
	}
	// A node-root certificate cannot enter the Hub role listener.
	badCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	bad := exec.CommandContext(badCtx, peer, "--mode", "hub", "--id", nodeID, "--backend-id", backendID, "--address", hubAddress, "--cert", nodeCert, "--key", nodeKey, "--ca", backendRoot, "--db", filepath.Join(work, "wrong-role.db"))
	if err := bad.Run(); err == nil {
		t.Fatal("node certificate impersonated Hub")
	}
	cancel()
	tenant := scope(t)
	seedTracerTenant(t, pool, tenant)
	s := &service.Tracer{Pool: pool}
	m := mutation()
	m.ResourceID = nodeID
	m.Payload = []byte(fmt.Sprintf(`{"version":1,"node_id":%q,"listeners":[],"rules":[],"revocations":[]}`, nodeID))
	payloadPath := filepath.Join(work, "desired.json")
	if err := os.WriteFile(payloadPath, m.Payload, 0600); err != nil {
		t.Fatal(err)
	}
	actor := uuid.NewString()
	command := exec.Command(backend, "mutate", "--actor", actor, "--tenant", tenant.TenantID(), "--scope", tenant.TenantID(), "--target", nodeID, "--key", m.IdempotencyKey, "--request", m.RequestID, "--payload", payloadPath)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("backend mutation: %s %v", output, err)
	}
	var accepted struct {
		ID string `json:"operation_id"`
	}
	if json.Unmarshal(output, &accepted) != nil || accepted.ID == "" {
		t.Fatalf("invalid accepted result: %s", output)
	}
	eventually(t, "matching Agent ApplyResult", func() bool {
		op, err := s.Operation(ctx, tenant, accepted.ID)
		return err == nil && op.State == "succeeded" && op.TargetRevision == 1
	})
	// Re-lease the same committed task as a delivery-loss fault. The same wire
	// transaction must replay its SQLite outcome without a second application.
	if _, err = pool.Exec(ctx, "UPDATE tasks SET state='pending' WHERE tenant_id=$1 AND operation_id=$2;", tenant.TenantID(), accepted.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, "UPDATE outbox SET delivered_at=NULL WHERE tenant_id=$1 AND operation_id=$2", tenant.TenantID(), accepted.ID); err != nil {
		t.Fatal(err)
	}
	eventually(t, "duplicate delivery durable replay", func() bool {
		var state string
		var attempts int
		err := pool.QueryRow(ctx, "SELECT state,attempts FROM tasks WHERE tenant_id=$1 AND operation_id=$2", tenant.TenantID(), accepted.ID).Scan(&state, &attempts)
		return err == nil && state == "succeeded" && attempts == 2
	})
	sqlite, err := store.Open(agentDB, "agent")
	if err != nil {
		t.Fatal(err)
	}
	count, err := sqlite.ConfigResultCount(ctx)
	sqlite.Close()
	if err != nil || count != 1 {
		t.Fatalf("duplicate Agent effects count=%d %v", count, err)
	}
	hubStore, err := store.Open(hubDB, "hub")
	if err != nil {
		t.Fatal(err)
	}
	hubStatus, err := hubStore.CurrentAutonomyStatus(ctx, time.Now(), true)
	blocked, e := hubStore.HubControlSnapshot(ctx, hubID, time.Now())
	hubStore.Close()
	if err != nil || e != nil || hubStatus.BackendHeartbeat.IsZero() || blocked.Mode != "blocked" || blocked.Epoch != 0 {
		t.Fatalf("Hub persisted liveness %v %v %+v %+v", err, e, hubStatus, blocked)
	}
	// Freeze the real Agent after handshake; the TLS connection stays open
	// while delivery expires. Its late result must keep unknown occupation held.
	if err = a.Process.Signal(syscall.SIGSTOP); err != nil {
		t.Fatal(err)
	}
	m.Revision = 2
	m.ExpectedRevision = 1
	m.Generation = 2
	m.IdempotencyKey = uuid.NewString()
	m.RequestID = uuid.NewString()
	m.Payload = []byte(fmt.Sprintf(`{"version":2,"node_id":%q,"listeners":[],"rules":[],"revocations":[]}`, nodeID))
	timedOut, err := s.Mutate(ctx, tenant, m)
	if err != nil {
		t.Fatal(err)
	}
	eventually(t, "real Agent timeout unknown", func() bool {
		op, err := s.Operation(ctx, tenant, timedOut.ID)
		return err == nil && op.State == "unknown" && op.OccupationHeld
	})
	if err = a.Process.Signal(syscall.SIGCONT); err != nil {
		t.Fatal(err)
	}
	eventually(t, "late Agent result committed locally", func() bool {
		local, e := store.Open(agentDB, "agent")
		if e != nil {
			return false
		}
		defer local.Close()
		version, e := local.AgentState(ctx, "applied_version")
		return e == nil && version == "2"
	})
	stillUnknown, err := s.Operation(ctx, tenant, timedOut.ID)
	if err != nil || stillUnknown.State != "unknown" || !stillUnknown.OccupationHeld {
		t.Fatal("late delivery resolved unknown without reconciliation")
	}
	// Loss after a previously successful delivery cannot turn a fresh target
	// mutation into a safe failure or release its occupation.
	if err = a.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	time.Sleep(100 * time.Millisecond)
	m.Revision = 3
	m.ExpectedRevision = 2
	m.Generation = 3
	m.IdempotencyKey = uuid.NewString()
	m.RequestID = uuid.NewString()
	m.Payload = []byte(fmt.Sprintf(`{"version":3,"node_id":%q,"listeners":[],"rules":[],"revocations":[]}`, nodeID))
	unknown, err := s.Mutate(ctx, tenant, m)
	if err != nil {
		t.Fatal(err)
	}
	eventually(t, "disconnect persisted unknown", func() bool {
		op, err := s.Operation(ctx, tenant, unknown.ID)
		return err == nil && op.State == "unknown" && op.OccupationHeld
	})
	// The same-source exact result is evidence, not a financial release proof.
	if err = s.Reconcile(ctx, tenant.TenantID(), unknown.ID, service.Receipt{Revision: 1, Generation: 1, State: "succeeded", Payload: []byte("late")}); err != service.ErrStaleReceipt {
		t.Fatalf("late revision %v", err)
	}
	report := map[string]any{"commit": os.Getenv("PHASE6_EXPECTED_SHA"), "backend_pid": b.Process.Pid, "agent_pid": a.Process.Pid, "hub_pid": h.Process.Pid, "node_revision": 1, "state": "succeeded", "agent_config_results": count, "deliveries": 2, "disconnect_state": "unknown", "occupation_held": true, "hub_mode": blocked.Mode, "hub_signer_epoch": blocked.Epoch, "hub_heartbeat": hubStatus.BackendHeartbeat, "wrong_role_rejected": true, "wire_contract": "1.3.0", "scope": "empty Agent runtime control; Hub liveness only"}
	report["timeout_state"] = "unknown"
	report["late_result_state"] = "unknown_until_reconciliation"
	raw, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(evidence, stamp+"-result.json"), raw, 0644); err != nil {
		t.Fatal(err)
	}
	t.Logf("real backend/Agent/Hub processes succeeded operation=%s duplicate=2 SQLite-effects=1 disconnect=unknown", accepted.ID)
}
