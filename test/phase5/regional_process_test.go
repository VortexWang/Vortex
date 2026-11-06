package phase5_test

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"database/sql"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"sync/atomic"
	"testing"
	"time"
	"vortex/engine/runtime/config"
	"vortex/engine/runtime/link"
	"vortex/engine/vlink"

	"vortex/agent/identity"
	"vortex/agent/store"
	"vortex/contracts"
	controlv1 "vortex/contracts/protobuf/v1"
)

const regionalNode = "550e8400-e29b-41d4-a716-446655440100"
const regionalFirst = "550e8400-e29b-41d4-a716-446655440101"
const regionalSecond = "550e8400-e29b-41d4-a716-446655440102"
const regionalBoot = "550e8400-e29b-41d4-a716-446655440103"

func processCredential(t *testing.T, dir, id string, ca *identity.CA, der []byte) (string, string) {
	t.Helper()
	key, csr, err := identity.GenerateCSR(id)
	if err != nil {
		t.Fatal(err)
	}
	c, err := ca.SignCSR(csr, id, time.Now(), time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	c, err = identity.AttachKey(c, key, der)
	if err != nil {
		t.Fatal(err)
	}
	keyDER, err := x509.MarshalPKCS8PrivateKey(c.Certificate.PrivateKey.(*ecdsa.PrivateKey))
	if err != nil {
		t.Fatal(err)
	}
	certPath, keyPath := filepath.Join(dir, id+".pem"), filepath.Join(dir, id+".key")
	if err = os.WriteFile(certPath, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: c.Certificate.Certificate[0]}), 0600); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(keyPath, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER}), 0600); err != nil {
		t.Fatal(err)
	}
	return certPath, keyPath
}
func regionalPort(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	s := l.Addr().String()
	l.Close()
	return s
}

func TestRegionalProcessFailover(t *testing.T) {
	// These are the actual shipped entry points, not a testing-only worker.
	binaries := t.TempDir()
	for _, name := range []string{"vortex-node", "vortex-test-hub"} {
		cmd := exec.Command("go", "build", "-o", filepath.Join(binaries, name), "./cmd/"+name)
		cmd.Dir = "../.."
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("build %s: %v\n%s", name, err, out)
		}
	}
	for _, fault := range []string{"silent-stream", "silent-established", "autonomous", "recovering"} {
		t.Run(fault, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			dir := t.TempDir()
			now := time.Now().UTC()
			ca, der, err := identity.NewCA(now)
			if err != nil {
				t.Fatal(err)
			}
			caPath := filepath.Join(dir, "ca.pem")
			if err = os.WriteFile(caPath, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0600); err != nil {
				t.Fatal(err)
			}
			nodeCert, nodeKey := processCredential(t, dir, regionalNode, ca, der)
			addresses := []string{regionalPort(t), regionalPort(t)}
			logs := []*os.File{}
			launch := func(name string, args ...string) *exec.Cmd {
				t.Helper()
				log, err := os.CreateTemp(dir, name+"-*.log")
				if err != nil {
					t.Fatal(err)
				}
				logs = append(logs, log)
				cmd := exec.CommandContext(ctx, filepath.Join(binaries, name), args...)
				cmd.Stdout = log
				cmd.Stderr = log
				if err = cmd.Start(); err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() {
					if cmd.ProcessState == nil {
						_ = cmd.Process.Kill()
						_ = cmd.Wait()
					}
					log.Close()
					if t.Failed() {
						raw, _ := os.ReadFile(log.Name())
						t.Log(string(raw))
					}
				})
				return cmd
			}
			for i, id := range []string{regionalFirst, regionalSecond} {
				path := filepath.Join(dir, fmt.Sprintf("hub%d.db", i))
				s, e := store.Open(path, "hub")
				if e != nil {
					t.Fatal(e)
				}
				d := store.HubDelegation{ID: regionalBoot, HubID: id, SourceID: regionalFirst, RuntimeSourceID: regionalSecond, ShareSourceID: regionalNode, NodeID: regionalNode, BootID: regionalBoot, BillingSnapshotID: regionalBoot, RuleID: "rule", RuleRevision: 1, PathGeneration: 1, FencingFloor: 1, SignerEpoch: 1, RawBytes: 4096, RateBPS: 1000000, Connections: 2, ValidFrom: now, ExpiresAt: now.Add(time.Hour), HardDeadline: now.Add(time.Hour), BackendHeartbeat: now, AutonomyDeadline: now.Add(time.Hour)}
				if e = s.InstallHubDelegation(ctx, d, store.IssuerTime{Earliest: now, Latest: now, Trusted: true}); e != nil {
					t.Fatal(e)
				}
				s.Close()
				if i == 0 && (fault == "autonomous" || fault == "recovering") {
					db, e := sql.Open("sqlite", path)
					if e != nil {
						t.Fatal(e)
					}
					_, e = db.Exec("UPDATE agent_state SET value=? WHERE key='authority_mode'", fault)
					db.Close()
					if e != nil {
						t.Fatal(e)
					}
				}
				cert, key := processCredential(t, dir, id, ca, der)
				args := []string{"--listen", addresses[i], "--db", path, "--tls-cert", cert, "--tls-key", key, "--tls-ca", caPath, "--hub-id", id, "--region", "east"}
				if i == 1 {
					args = append(args, "--test-report-output", filepath.Join(dir, "current-report.json"))
				}
				if i == 0 && fault == "silent-stream" {
					args = append(args, "--stall-readiness")
				}
				if i == 0 && fault == "silent-established" {
					args = append(args, "--stall-after-readiness")
				}
				launch("vortex-test-hub", args...)
			}
			// Persist unknown debt before the actual node's fresh boot. Transport ACK
			// must not convert it to actual consumption or erase it on failover.
			nodePath := filepath.Join(dir, "node.db")
			ns, e := store.Open(nodePath, "agent")
			if e != nil {
				t.Fatal(e)
			}
			payload, e := contracts.EncodeUnknownUsage(&controlv1.UnknownUsageBatch{BatchId: "process-unknown", OriginBootId: regionalBoot, BillingSnapshotId: regionalBoot, RuleId: "rule", GrantId: regionalFirst, Role: "source", Direction: "egress", UnknownUpperBound: 512})
			if e != nil {
				t.Fatal(e)
			}
			if e = ns.PersistUnknownUsage(ctx, regionalNode, payload); e != nil {
				t.Fatal(e)
			}
			ns.Close()
			hubs := []map[string]any{}
			for i, id := range []string{regionalFirst, regionalSecond} {
				hubs = append(hubs, map[string]any{"hub_id": id, "endpoint": addresses[i], "server_name": id})
			}
			cfg, _ := json.Marshal(map[string]any{"version": 1, "node_id": regionalNode, "listeners": []any{}, "rules": []any{}, "revocations": []string{}, "lifecycle": map[string]any{"region": map[string]any{"region_id": "east", "hubs": hubs}, "dial_timeout_seconds": 1, "watchdog_seconds": 2, "failover_budget_seconds": 5, "retry_min_ms": 20, "retry_max_ms": 50, "report_interval_seconds": 1}})
			cfgPath := filepath.Join(dir, "control.json")
			if e = os.WriteFile(cfgPath, cfg, 0600); e != nil {
				t.Fatal(e)
			}
			node := launch("vortex-node", "--node-id", regionalNode, "--db", nodePath, "--control-config", cfgPath, "--hub-name", regionalSecond, "--enrollment-hub", addresses[1], "--tls-ca", caPath, "--tls-cert", nodeCert, "--tls-key", nodeKey, "--credential-dir", filepath.Join(dir, "credentials"), "--renew-before", "1m")
			db, e := sql.Open("sqlite", filepath.Join(dir, "hub1.db"))
			if e != nil {
				t.Fatal(e)
			}
			defer db.Close()
			start := time.Now()
			for {
				var upper, state string
				e = db.QueryRowContext(ctx, "SELECT unknown_upper_bound,liability_state FROM unknown_usage_batches WHERE batch_id='process-unknown'").Scan(&upper, &state)
				if e == nil {
					if upper != "512" || state != "unresolved" {
						t.Fatalf("Hub changed unknown liability: %s %s", upper, state)
					}
					break
				}
				select {
				case <-ctx.Done():
					t.Fatal("actual node failed bounded same-region handoff")
				case <-time.After(20 * time.Millisecond):
				}
			}
			if time.Since(start) > 5*time.Second {
				t.Fatal("process failover exceeded configured sweep budget")
			}
			nd, e := sql.Open("sqlite", nodePath)
			if e != nil {
				t.Fatal(e)
			}
			defer nd.Close()
			var upper, state string
			if e = nd.QueryRowContext(ctx, "SELECT unknown_upper_bound,liability_state FROM unknown_usage_batches WHERE batch_id='process-unknown'").Scan(&upper, &state); e != nil || upper != "512" || state != "unresolved" {
				t.Fatalf("Agent debt lost: %s %s %v", upper, state, e)
			}
			// Current boot differs but original liability identity is immutable.
			var origin string
			if e = nd.QueryRowContext(ctx, "SELECT origin_boot_id FROM unknown_usage_batches WHERE batch_id='process-unknown'").Scan(&origin); e != nil || origin != regionalBoot {
				t.Fatalf("origin changed: %s %v", origin, e)
			}
			var count int
			if e = nd.QueryRowContext(ctx, "SELECT count(*) FROM agent_state WHERE key='backend_heartbeat'").Scan(&count); e != nil || count != 0 {
				t.Fatal("status minted authority time")
			}
			for {
				raw, err := os.ReadFile(filepath.Join(dir, "current-report.json"))
				if err == nil {
					var snapshot struct {
						NodeID, BootID, SessionID string
						Report                    *controlv1.NodeReport
					}
					if err := json.Unmarshal(raw, &snapshot); err != nil {
						t.Fatal(err)
					}
					if snapshot.NodeID != regionalNode || snapshot.BootID == regionalBoot || snapshot.SessionID == "" {
						t.Fatalf("report identity: %+v", snapshot)
					}
					if err := contracts.ValidateNodeReport(snapshot.Report); err != nil {
						t.Fatal(err)
					}
					break
				}
				select {
				case <-ctx.Done():
					t.Fatal("actual node never reported over current Hub stream")
				case <-time.After(20 * time.Millisecond):
				}
			}
			// Node is still alive after successful handoff; normal termination joins
			// control ownership and leaves the authoritative database intact.
			if err = node.Process.Signal(os.Interrupt); err != nil {
				t.Fatal(err)
			}
			exited := make(chan error, 1)
			go func() { exited <- node.Wait() }()
			select {
			case e := <-exited:
				if e != nil {
					t.Fatal(e)
				}
			case <-time.After(3 * time.Second):
				t.Fatal("node failed bounded shutdown")
			}
		})
	}
	t.Run("tcp-original-authorization", func(t *testing.T) { regionalTCPContinuity(t, binaries) })
}

func regionalTCPContinuity(t *testing.T, binaries string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()
	dir := t.TempDir()
	now := time.Now().UTC()
	ca, der, err := identity.NewCA(now)
	if err != nil {
		t.Fatal(err)
	}
	caPath := filepath.Join(dir, "ca.pem")
	os.WriteFile(caPath, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0600)
	nodeCert, nodeKey := processCredential(t, dir, regionalNode, ca, der)
	start := func(name string, args ...string) *exec.Cmd {
		t.Helper()
		out, e := os.CreateTemp(dir, name+"-*.log")
		if e != nil {
			t.Fatal(e)
		}
		cmd := exec.CommandContext(ctx, filepath.Join(binaries, name), args...)
		cmd.Stdout = out
		cmd.Stderr = out
		if e = cmd.Start(); e != nil {
			t.Fatal(e)
		}
		t.Cleanup(func() {
			if cmd.ProcessState == nil {
				cmd.Process.Kill()
				cmd.Wait()
			}
			out.Close()
			if t.Failed() {
				raw, _ := os.ReadFile(out.Name())
				t.Log(string(raw))
			}
		})
		return cmd
	}
	// Current Engine fixture uses the historical qualification DNS identity;
	// unified production identity rotation is a separate 05-09 gate.
	const peerID = "550e8400-e29b-41d4-a716-446655440199"
	engineCA, _, e := identity.NewCA(now)
	if e != nil {
		t.Fatal(e)
	}
	engineTrust := filepath.Join(dir, "engine-ca.pem")
	os.WriteFile(engineTrust, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: engineCA.Certificate.Raw}), 0600)
	engineConfig := func(id string) config.Config {
		key, e := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		if e != nil {
			t.Fatal(e)
		}
		uri, _ := url.Parse("urn:vortex:node:" + id)
		cert := &x509.Certificate{SerialNumber: big.NewInt(time.Now().UnixNano()), URIs: []*url.URL{uri}, DNSNames: []string{"vortex"}, NotBefore: now.Add(-time.Minute), NotAfter: now.Add(time.Hour), KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth, x509.ExtKeyUsageServerAuth}}
		certDER, e := x509.CreateCertificate(rand.Reader, cert, engineCA.Certificate, &key.PublicKey, engineCA.Key)
		if e != nil {
			t.Fatal(e)
		}
		keyDER, e := x509.MarshalPKCS8PrivateKey(key)
		if e != nil {
			t.Fatal(e)
		}
		certPath, keyPath := filepath.Join(dir, id+"-engine.pem"), filepath.Join(dir, id+"-engine.key")
		os.WriteFile(certPath, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER}), 0600)
		os.WriteFile(keyPath, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER}), 0600)
		return config.Config{NodeID: id, Version: 1, Listen: regionalPort(t), Certificate: certPath, PrivateKey: keyPath, TrustBundle: engineTrust, RuleID: regionalBoot, RuleRevision: 2, PathGeneration: 4}
	}
	target, e := net.Listen("tcp", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	defer target.Close()
	var accepted atomic.Int64
	go func() {
		for {
			conn, e := target.Accept()
			if e != nil {
				return
			}
			accepted.Add(1)
			go func() { defer conn.Close(); io.Copy(conn, conn) }()
		}
	}()
	cfg := engineConfig(regionalNode)
	cfg.Role = config.RoleDestination
	cfg.Target = target.Addr().String()
	cfg.PeerNodeID = peerID
	cfg.ExecutionPoint = true
	raw, _ := json.Marshal(cfg)
	enginePath := filepath.Join(dir, "engine.json")
	os.WriteFile(enginePath, raw, 0600)
	host, portText, _ := net.SplitHostPort(cfg.Listen)
	port, _ := strconv.Atoi(portText)
	runtimePayload, _ := json.Marshal(map[string]any{"version": 1, "node_id": regionalNode, "listeners": []any{map[string]any{"port_id": "link", "kind": "link", "address": host, "port": port}}, "rules": []any{map[string]any{"rule_id": cfg.RuleID, "revision": cfg.RuleRevision, "path_generation": cfg.PathGeneration, "role": "exit", "hop_index": 0, "mode": "tunnel", "targets": []string{cfg.Target}, "allowed_upstream_node_ids": []string{peerID}}}, "revocations": []string{}})
	digest := sha256.Sum256(runtimePayload)
	update := &controlv1.RuntimeConfigUpdate{Version: 1, Payload: runtimePayload, PayloadSha256: digest[:], TransactionId: []byte("0123456789abcdef"), FencingGeneration: 9}
	updateRaw, _ := json.Marshal(update)
	updatePath := filepath.Join(dir, "update.json")
	os.WriteFile(updatePath, updateRaw, 0600)
	d := store.HubDelegation{ID: regionalBoot, HubID: regionalFirst, SourceID: regionalFirst, RuntimeSourceID: regionalSecond, ShareSourceID: regionalNode, NodeID: regionalNode, BootID: regionalBoot, BillingSnapshotID: regionalBoot, RuleID: cfg.RuleID, RuleRevision: 2, PathGeneration: 4, FencingFloor: 9, SignerEpoch: 1, RawBytes: 16384, RateBPS: 10000000, Connections: 2, ValidFrom: now, ExpiresAt: now.Add(time.Hour), HardDeadline: now.Add(time.Hour), BackendHeartbeat: now, AutonomyDeadline: now.Add(time.Hour)}
	allocation, _ := json.Marshal(d)
	allocationPath := filepath.Join(dir, "allocation.json")
	os.WriteFile(allocationPath, allocation, 0600)
	addresses := []string{regionalPort(t), regionalPort(t)}
	var first *exec.Cmd
	for i, id := range []string{regionalFirst, regionalSecond} {
		cert, key := processCredential(t, dir, id, ca, der)
		dbPath := filepath.Join(dir, fmt.Sprintf("hub%d.db", i))
		args := []string{"--listen", addresses[i], "--db", dbPath, "--tls-cert", cert, "--tls-key", key, "--tls-ca", caPath, "--hub-id", id, "--region", "east"}
		if i == 0 {
			args = append(args, "--test-delegation-template", allocationPath, "--test-config-update", updatePath)
		} else {
			s, e := store.Open(dbPath, "hub")
			if e != nil {
				t.Fatal(e)
			}
			other := d
			other.HubID = id
			if e = s.InstallHubDelegation(ctx, other, store.IssuerTime{Earliest: now, Latest: now, Trusted: true}); e != nil {
				t.Fatal(e)
			}
			s.Close()
		}
		child := start("vortex-test-hub", args...)
		if i == 0 {
			first = child
		}
	}
	hubs := []map[string]string{}
	for i, id := range []string{regionalFirst, regionalSecond} {
		hubs = append(hubs, map[string]string{"hub_id": id, "endpoint": addresses[i], "server_name": id})
	}
	controlRaw, _ := json.Marshal(map[string]any{"version": 1, "node_id": regionalNode, "listeners": []any{}, "rules": []any{}, "revocations": []string{}, "lifecycle": map[string]any{"region": map[string]any{"region_id": "east", "hubs": hubs}, "dial_timeout_seconds": 1, "watchdog_seconds": 2, "failover_budget_seconds": 5, "retry_min_ms": 20, "retry_max_ms": 50}})
	controlPath := filepath.Join(dir, "control.json")
	os.WriteFile(controlPath, controlRaw, 0600)
	nodePath := filepath.Join(dir, "agent.db")
	start("vortex-node", "--node-id", regionalNode, "--db", nodePath, "--engine-config", enginePath, "--control-config", controlPath, "--hub-name", regionalFirst, "--enrollment-hub", addresses[0], "--tls-ca", caPath, "--tls-cert", nodeCert, "--tls-key", nodeKey, "--credential-dir", filepath.Join(dir, "credentials"), "--renew-before", "1m")
	db, e := sql.Open("sqlite", nodePath)
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	for {
		var state string
		e = db.QueryRowContext(ctx, "SELECT state FROM config_results WHERE version=1").Scan(&state)
		if e == nil && state == "active" {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal("node did not activate current boot authority")
		case <-time.After(20 * time.Millisecond):
		}
	}
	peer := engineConfig(peerID)
	peer.Role = config.RoleSource
	peer.Peer = cfg.Listen
	peer.NextHopNodeID = regionalNode
	peer.ServiceListen = "127.0.0.1:0"
	supervisor, e := link.New(peer)
	if e != nil {
		t.Fatal(e)
	}
	defer supervisor.Close()
	ready := make(chan *vlink.MuxLink, 1)
	go func() {
		_ = supervisor.Dial(ctx, func(c context.Context, m *vlink.MuxLink, _ vlink.NodeID) error {
			ready <- m
			<-c.Done()
			return c.Err()
		})
	}()
	var mux *vlink.MuxLink
	select {
	case mux = <-ready:
	case <-ctx.Done():
		t.Fatal("VLink connection failed")
	}
	ruleHash := sha256.Sum256([]byte(cfg.RuleID))
	var rule [16]byte
	copy(rule[:], ruleHash[:16])
	var stream *vlink.MuxStream
	for {
		stream, e = mux.Open(ctx, vlink.OpenRequest{RuleID: rule, RuleRevision: 2, PathGeneration: 4, Network: 1, ClientAddress: []byte{127, 0, 0, 1}})
		if e == nil {
			e = stream.WaitOpen(ctx)
		}
		if e == nil {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal("TCP OPEN never became authorized")
		case <-time.After(30 * time.Millisecond):
		}
	}
	exchange := func(payload string) {
		t.Helper()
		if e = stream.WriteData(ctx, []byte(payload)); e != nil {
			t.Fatal(e)
		}
		frame, e := stream.Receive(ctx)
		if e != nil {
			t.Fatal(e)
		}
		defer frame.Release()
		if string(frame.Payload) != payload {
			t.Fatalf("changed TCP payload: %q", frame.Payload)
		}
	}
	exchange("before-control-switch")
	var boot, grant, source, expires string
	if e = db.QueryRowContext(ctx, "SELECT boot_id,authorization_id,source_id,expires_at FROM authorization_records WHERE kind='credit_grant' AND state='active'").Scan(&boot, &grant, &source, &expires); e != nil {
		t.Fatal(e)
	}
	// Add a real funded unresolved reservation through the same durable Store
	// interface. The successor's transport ACK cannot refund this reservation.
	s, e := store.Open(nodePath, "agent")
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	at := time.Now().UTC()
	reservation := store.UsageReservation{NodeID: regionalNode, BootID: boot, AuthorizationID: grant, BatchID: "switch-debt", SnapshotID: regionalBoot, FlowKey: "interrupted", RuleID: cfg.RuleID, Direction: "egress", Reserved: 512}
	if _, e = s.ReserveAuthorizedUsage(ctx, reservation, store.AuthorizationUse{NodeID: regionalNode, BootID: boot, AuthorizationID: grant, Kind: "credit_grant", RuleID: cfg.RuleID, RuleRevision: 2, PathGeneration: 4, BillingSnapshotID: regionalBoot, Earliest: at, Latest: at, TrustedTime: true}); e != nil {
		t.Fatal(e)
	}
	unknown, e := contracts.EncodeUnknownUsage(&controlv1.UnknownUsageBatch{BatchId: "switch-debt", OriginBootId: boot, BillingSnapshotId: regionalBoot, RuleId: cfg.RuleID, GrantId: grant, Role: "source", Direction: "egress", UnknownUpperBound: 512})
	if e != nil {
		t.Fatal(e)
	}
	if e = s.SettleUnknownUsageReservation(ctx, reservation, unknown); e != nil {
		t.Fatal(e)
	}
	if e = first.Process.Kill(); e != nil {
		t.Fatal(e)
	}
	_ = first.Wait()
	secondDB, e := sql.Open("sqlite", filepath.Join(dir, "hub1.db"))
	if e != nil {
		t.Fatal(e)
	}
	defer secondDB.Close()
	for {
		var count int
		e = secondDB.QueryRowContext(ctx, "SELECT count(*) FROM durable_events WHERE kind='lifecycle-hello'").Scan(&count)
		if e == nil && count > 0 {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal("second actual Hub never selected")
		case <-time.After(20 * time.Millisecond):
		}
	}
	exchange("after-control-switch")
	if accepted.Load() != 1 {
		t.Fatalf("TCP reconnected rather than drained: %d", accepted.Load())
	}
	var after string
	if e = db.QueryRowContext(ctx, "SELECT expires_at FROM authorization_records WHERE authorization_id=?", grant).Scan(&after); e != nil || after != expires {
		t.Fatal("switch changed original authorization expiry")
	}
	audit, e := s.AuditAuthorizationSource(ctx, regionalNode, source)
	if e != nil {
		t.Fatalf("source conservation failed: %+v %v", audit, e)
	}
	var upper, state string
	if e = db.QueryRowContext(ctx, "SELECT unknown_upper_bound,liability_state FROM unknown_usage_batches WHERE batch_id='switch-debt'").Scan(&upper, &state); e != nil || upper != "512" || state != "unresolved" {
		t.Fatalf("funded unknown liability was released: %s %s %v", upper, state, e)
	}
}
