package phase5_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
	"vortex/agent/identity"
	"vortex/agent/store"
)

func TestRecoveringProcessCatchup(t *testing.T) {
	binaries := t.TempDir()
	for _, name := range []string{"vortex-node", "vortex-test-hub"} {
		cmd := exec.Command("go", "build", "-o", filepath.Join(binaries, name), "./cmd/"+name)
		cmd.Dir = "../.."
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("build %s %v %s", name, err, out)
		}
	}
	for _, scenario := range []string{"recovering-lost-ack", "recovering-restart", "recovering-before-handoff", "recovering-after-catchup", "recovering-wrong-hash"} {
		t.Run(scenario, func(t *testing.T) { regionalTrafficContinuity(t, binaries, scenario) })
	}
}

// Real TLS backend fixture commits exact bytes in its own FULL SQLite database
// before producing any receipt. No production backend behavior is claimed.
func recoveringHubArgs(t *testing.T, ctx context.Context, dir string, ca *identity.CA, der []byte) []string {
	t.Helper()
	const backendID = "550e8400-e29b-41d4-a716-446655440188"
	certPath, keyPath := processCredential(t, dir, backendID, ca, der)
	pair, err := tls.LoadX509KeyPair(certPath, keyPath)
	if err != nil {
		t.Fatal(err)
	}
	roots := x509.NewCertPool()
	roots.AddCert(ca.Certificate)
	backend, err := sql.Open("sqlite", filepath.Join(dir, "recovery-backend.db"))
	if err != nil {
		t.Fatal(err)
	}
	backend.SetMaxOpenConns(1)
	t.Cleanup(func() { backend.Close() })
	for _, q := range []string{`PRAGMA journal_mode=WAL`, `PRAGMA synchronous=FULL`, `CREATE TABLE records(sequence INTEGER PRIMARY KEY,kind TEXT,id TEXT,payload BLOB,digest BLOB)`} {
		if _, err = backend.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.TLS == nil || len(r.TLS.VerifiedChains) == 0 || len(r.TLS.PeerCertificates) == 0 || r.TLS.PeerCertificates[0].VerifyHostname(regionalFirst) != nil {
			http.Error(w, "identity", 403)
			return
		}
		var record store.LedgerRecord
		raw, err := io.ReadAll(io.LimitReader(r.Body, 2<<20))
		if err != nil || json.Unmarshal(raw, &record) != nil || record.Sequence <= 0 {
			http.Error(w, "record", 400)
			return
		}
		sum := sha256.Sum256(record.Payload)
		if !bytes.Equal(sum[:], record.Digest) {
			http.Error(w, "digest", 400)
			return
		}
		tx, err := backend.BeginTx(ctx, nil)
		if err != nil {
			http.Error(w, "begin", 500)
			return
		}
		defer tx.Rollback()
		if _, err = tx.ExecContext(ctx, `INSERT INTO records VALUES(?,?,?,?,?) ON CONFLICT DO NOTHING`, record.Sequence, record.Kind, record.ID, record.Payload, record.Digest); err != nil {
			http.Error(w, "write", 500)
			return
		}
		var kind, id string
		var payload, digest []byte
		if err = tx.QueryRowContext(ctx, `SELECT kind,id,payload,digest FROM records WHERE sequence=?`, record.Sequence).Scan(&kind, &id, &payload, &digest); err != nil || kind != record.Kind || id != record.ID || !bytes.Equal(payload, record.Payload) || !bytes.Equal(digest, record.Digest) {
			http.Error(w, "conflict", 409)
			return
		}
		if err = tx.Commit(); err != nil {
			http.Error(w, "commit", 500)
			return
		}
		policy, _ := os.ReadFile(filepath.Join(dir, "recovery-ack-policy"))
		switch string(policy) {
		case "allow":
			w.Write(digest)
		case "wrong":
			w.Write(make([]byte, 32))
		default:
			http.Error(w, "lost after durable commit", 503)
		}
	}))
	server.TLS = &tls.Config{MinVersion: tls.VersionTLS13, MaxVersion: tls.VersionTLS13, Certificates: []tls.Certificate{pair}, ClientCAs: roots, ClientAuth: tls.RequireAndVerifyClientCert}
	server.StartTLS()
	t.Cleanup(server.Close)
	return []string{"--test-recovery-request", filepath.Join(dir, "recovery-request.json"), "--test-recovery-backend", server.URL, "--test-recovery-backend-name", backendID}
}

func exerciseRecoveringProcess(t *testing.T, ctx context.Context, dir, mode string, nodeDB *sql.DB, exchange func(string), restart func()) {
	t.Helper()
	hubDB, err := openProcessLedger(filepath.Join(dir, "hub0.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer hubDB.Close()
	wait := func(label string, condition func() bool) {
		t.Helper()
		for !condition() {
			select {
			case <-ctx.Done():
				t.Fatal(label)
			case <-time.After(20 * time.Millisecond):
			}
		}
	}
	queryMode := func(want string) bool {
		var current string
		return hubDB.QueryRowContext(ctx, `SELECT value FROM agent_state WHERE key='authority_mode'`).Scan(&current) == nil && current == want
	}
	exchange("before-recovery")
	var beforeRemaining, delegation, boot string
	if err = hubDB.QueryRowContext(ctx, `SELECT delegation_id,remaining_bytes FROM issuer_delegations WHERE state='open' LIMIT 1`).Scan(&delegation, &beforeRemaining); err != nil {
		t.Fatal(err)
	}
	if err = nodeDB.QueryRowContext(ctx, `SELECT boot_id FROM authorization_records WHERE kind='credit_grant' AND state='active' LIMIT 1`).Scan(&boot); err != nil {
		t.Fatal(err)
	}
	request := store.HandoffRequest{ID: "550e8400-e29b-41d4-a716-446655440800", HubID: regionalFirst, FromEpoch: 1, ToEpoch: 2, Heartbeat: time.Now().UTC()}
	// Phase6 requires independently supplied exact successors for funded flat
	// roots. This offline protocol backend fixture reads the actual persisted
	// allocation after boot binding; it cannot infer ownership from expiry.
	var allocation []byte
	if err = hubDB.QueryRowContext(ctx, `SELECT payload FROM issuer_delegations WHERE delegation_id=?`, delegation).Scan(&allocation); err != nil {
		t.Fatal(err)
	}
	var original store.HubDelegation
	if err = json.Unmarshal(allocation, &original); err != nil {
		t.Fatal(err)
	}
	if len(original.ShareReferences) != 3 {
		t.Fatal("fixture missing funded roots")
	}
	digest := sha256.Sum256(allocation)
	roots := append([]store.ShareReference(nil), original.ShareReferences...)
	for i := range roots {
		roots[i].Revision++
		roots[i].SignerEpoch++
	}
	request.BackendID = regionalBoot // original explicit fixture source owner; receipt endpoint is a separate identity
	request.FundedSuccessors = []store.FundedHandoffSuccessor{{DelegationID: delegation, AllocationSHA256: hex.EncodeToString(digest[:]), ShareReferences: roots}}

	if mode == "recovering-wrong-hash" {
		if err = os.WriteFile(filepath.Join(dir, "recovery-ack-policy"), []byte("wrong"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if mode == "recovering-before-handoff" {
		restart()
		exchange("after-SIGKILL-before-handoff")
	}
	raw, _ := json.Marshal(request)
	if err = os.WriteFile(filepath.Join(dir, "recovery-request.json"), raw, 0600); err != nil {
		t.Fatal(err)
	}
	wait("handoff did not persist recovering", func() bool { return queryMode(store.AutonomyRecovering) })
	backend, err := openProcessLedger(filepath.Join(dir, "recovery-backend.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer backend.Close()
	wait("backend did not durably accept lost receipt", func() bool {
		var n int
		return backend.QueryRowContext(ctx, `SELECT count(*) FROM records`).Scan(&n) == nil && n > 0
	})
	exchange("during-recovery-without-ack")
	if !queryMode(store.AutonomyRecovering) {
		t.Fatal("lost or forged receipt promoted online")
	}
	if mode == "recovering-restart" {
		restart()
		exchange("after-Hub-SIGKILL-before-ack")
		wait("restart lost recovery", func() bool { return queryMode(store.AutonomyRecovering) })
	}
	// Allow a valid receipt for the already committed bytes; retry uses the same
	// handoff and same finite barrier, never another R allocation.
	if err = os.WriteFile(filepath.Join(dir, "recovery-ack-policy"), []byte("allow"), 0600); err != nil {
		t.Fatal(err)
	}
	wait("confirmed barrier did not become online", func() bool { return queryMode(store.AutonomyOnline) })
	exchange("after-confirmed-recovery")
	if mode == "recovering-after-catchup" {
		restart()
		exchange("after-SIGKILL-after-catchup")
		wait("restart reopened recovering", func() bool { return queryMode(store.AutonomyOnline) })
	}
	var transfers int
	var remaining, epoch string
	if err = hubDB.QueryRowContext(ctx, `SELECT COUNT(*) FROM hub_transfers WHERE handoff_id=? AND delegation_id=?`, request.ID, delegation).Scan(&transfers); err != nil || transfers != 1 {
		t.Fatalf("transfers=%d %v", transfers, err)
	}
	if err = hubDB.QueryRowContext(ctx, `SELECT remaining_bytes,signer_epoch FROM issuer_delegations WHERE delegation_id=?`, delegation).Scan(&remaining, &epoch); err != nil || remaining != beforeRemaining || epoch != "2" {
		t.Fatalf("R changed %s/%s epoch=%s %v", remaining, beforeRemaining, epoch, err)
	}
	var selectedBoot string
	if err = nodeDB.QueryRowContext(ctx, `SELECT boot_id FROM authorization_records WHERE kind='credit_grant' AND state='active' LIMIT 1`).Scan(&selectedBoot); err != nil || selectedBoot != boot {
		t.Fatalf("business boot changed %s %s %v", boot, selectedBoot, err)
	}
	var upper, liability, origin string
	if err = nodeDB.QueryRowContext(ctx, `SELECT unknown_upper_bound,liability_state,origin_boot_id FROM unknown_usage_batches WHERE batch_id='command-debt'`).Scan(&upper, &liability, &origin); err != nil || upper != "512" || liability != "unresolved" || origin != regionalBoot {
		t.Fatalf("unknown debt changed %s %s %s %v", upper, liability, origin, err)
	}
	var generations int
	if err = nodeDB.QueryRowContext(ctx, `SELECT count(*) FROM authorization_records WHERE CAST(path_generation AS INTEGER)!=4`).Scan(&generations); err != nil || generations != 0 {
		t.Fatalf("path generation changed %d %v", generations, err)
	}
}
