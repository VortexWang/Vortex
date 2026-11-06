package phase5_test

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
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
	"strings"
	"sync/atomic"
	"testing"
	"time"
	"vortex/engine/runtime/config"
	"vortex/engine/runtime/link"
	"vortex/engine/vlink"
	"vortex/hub"

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
					db, e := openProcessLedger(path)
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
			db, e := openProcessLedger(filepath.Join(dir, "hub1.db"))
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
			nd, e := openProcessLedger(nodePath)
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

func regionalTCPContinuity(t *testing.T, binaries string) { regionalTrafficContinuity(t, binaries, "") }

func regionalTrafficContinuity(t *testing.T, binaries, renewalMode string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
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
		out, e := os.CreateTemp(dir, filepath.Base(name)+"-*.log")
		if e != nil {
			t.Fatal(e)
		}
		executable := name
		if !filepath.IsAbs(name) {
			executable = filepath.Join(binaries, name)
		}
		cmd := exec.CommandContext(ctx, executable, args...)
		cmd.Stdout = out
		cmd.Stderr = out
		if e = cmd.Start(); e != nil {
			t.Fatal(e)
		}
		binary, e := os.ReadFile(executable)
		if e != nil {
			t.Fatal(e)
		}
		digest := sha256.Sum256(binary)
		t.Logf("process-start name=%s pid=%d binary_sha256=%x", filepath.Base(name), cmd.Process.Pid, digest)
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
	// The node publishes its durable control identity to VLink. Both fixture
	// trust roles use this explicit CA; peers use exact node UUID DNS+URI SANs.
	const peerID = "550e8400-e29b-41d4-a716-446655440199"
	engineCA := ca
	var e error
	engineTrust := filepath.Join(dir, "engine-ca.pem")
	os.WriteFile(engineTrust, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: engineCA.Certificate.Raw}), 0600)
	engineConfig := func(id string) config.Config {
		key, e := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		if e != nil {
			t.Fatal(e)
		}
		uri, _ := url.Parse("urn:vortex:node:" + id)
		cert := &x509.Certificate{SerialNumber: big.NewInt(time.Now().UnixNano()), URIs: []*url.URL{uri}, DNSNames: []string{id}, NotBefore: now.Add(-time.Minute), NotAfter: now.Add(time.Hour), KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth, x509.ExtKeyUsageServerAuth}}
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
	if renewalMode != "" {
		udp, e := net.ListenPacket("udp", target.Addr().String())
		if e != nil {
			t.Fatal(e)
		}
		defer udp.Close()
		go func() {
			var b [2048]byte
			for {
				n, addr, e := udp.ReadFrom(b[:])
				if e != nil {
					return
				}
				if _, e = udp.WriteTo(b[:n], addr); e != nil {
					return
				}
			}
		}()
	}
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
	if renewalMode != "" {
		d.RawBytes = 65536
		if renewalMode == "quota-exhausted" {
			d.RawBytes = 8192
		}
		if renewalMode == "expiry-with-pending-result" {
			// Hard absolute boundary is never extended by renewal or heartbeat.
			d.ExpiresAt = now.Add(10 * time.Second)
			d.HardDeadline = d.ExpiresAt
		}
		for i, source := range []string{d.SourceID, d.RuntimeSourceID, d.ShareSourceID} {
			d.ShareReferences = append(d.ShareReferences, store.ShareReference{ID: fmt.Sprintf("550e8400-e29b-41d4-a716-%012d", 970+i), SourceID: source, IssuerID: d.HubID, NodeID: d.NodeID, SignerEpoch: 1, Generation: 1, Revision: 1, RuleID: d.RuleID, RuleRevision: d.RuleRevision, PathGeneration: d.PathGeneration, RateBPS: d.RateBPS, Connections: d.Connections, NotBefore: d.ValidFrom, ExpiresAt: d.ExpiresAt, HardDeadline: d.HardDeadline})
		}
		for _, name := range []string{"hub0.db", "agent.db"} {
			owner := "hub"
			if name == "agent.db" {
				owner = "agent"
			}
			s, e := store.Open(filepath.Join(dir, name), owner)
			if e != nil {
				t.Fatal(e)
			}
			for _, root := range d.ShareReferences {
				if e := s.InstallShareSourceAnchor(ctx, d.NodeID, regionalBoot, root); e != nil {
					s.Close()
					t.Fatal(e)
				}
			}
			s.Close()
		}
	}
	var policyPath string
	if strings.HasPrefix(renewalMode, "policy-") {
		policyPath = preparePolicyProcess(t, ctx, dir, d, renewalMode)
	}
	if strings.HasPrefix(renewalMode, "share-invalid-") {
		valid, err := store.Open(filepath.Join(dir, "hub0.db"), "hub")
		if err != nil {
			t.Fatal(err)
		}
		at := time.Now().UTC()
		if err = valid.InstallHubDelegation(ctx, d, store.IssuerTime{Earliest: at, Latest: at, Trusted: true}); err != nil {
			valid.Close()
			t.Fatal(err)
		}
		valid.Close()
		switch renewalMode {
		case "share-invalid-parent":
			d.ShareReferences[0].ParentID = regionalSecond
			d.ShareReferences[0].ParentRevision = 1
		case "share-invalid-issuer":
			d.ShareReferences[0].IssuerID = regionalSecond
		case "share-invalid-cycle":
			d.ShareReferences[0].ParentID = d.ShareReferences[0].ID
			d.ShareReferences[0].ParentRevision = 1
		case "share-invalid-capacity":
			d.ShareReferences[0].RateBPS++
		case "share-invalid-revision":
			d.ShareReferences[0].Revision++
		}
	}
	allocation, _ := json.Marshal(d)
	allocationPath := filepath.Join(dir, "allocation.json")
	os.WriteFile(allocationPath, allocation, 0600)
	addresses := []string{regionalPort(t), regionalPort(t)}
	var first *exec.Cmd
	var restartHub func()
	for i, id := range []string{regionalFirst, regionalSecond} {
		cert, key := processCredential(t, dir, id, ca, der)
		dbPath := filepath.Join(dir, fmt.Sprintf("hub%d.db", i))
		args := []string{"--listen", addresses[i], "--db", dbPath, "--tls-cert", cert, "--tls-key", key, "--tls-ca", caPath, "--hub-id", id, "--region", "east"}
		if i == 0 && strings.HasPrefix(renewalMode, "recovering-") {
			args = append(args, recoveringHubArgs(t, ctx, dir, ca, der)...)
		}
		if i == 0 && policyPath != "" {
			args = append(args, "--test-policy-transition", policyPath, "--test-authorization-refresh", filepath.Join(dir, "policy-refresh.json"))
		}
		if i == 0 {
			args = append(args, "--test-delegation-template", allocationPath, "--test-config-update", updatePath)
			if renewalMode == "lost-result" {
				args = append(args, "--test-drop-first-renewal-reply")
			}
			if renewalMode == "expiry-with-pending-result" {
				args = append(args, "--test-hold-renewal-replies")
			}
			if renewalMode == "hub-restart" || renewalMode == "node-restart" || strings.HasPrefix(renewalMode, "recovering-") || strings.HasPrefix(renewalMode, "policy-") || strings.HasPrefix(renewalMode, "upgrade-") {
				restartArgs := append([]string(nil), args...)
				restartHub = func() { first = start("vortex-test-hub", restartArgs...) }
				if !strings.HasPrefix(renewalMode, "upgrade-") && !strings.HasPrefix(renewalMode, "recovering-") && !strings.HasPrefix(renewalMode, "policy-") {
					args = append(args, "--test-hold-renewal-replies")
				}
			}
		} else {
			s, e := store.Open(dbPath, "hub")
			if e != nil {
				t.Fatal(e)
			}
			other := d
			other.HubID = id
			other.ShareReferences = nil // fallback has no ownership of the first Hub's roots
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
	if renewalMode == "hub-restart" || renewalMode == "node-restart" || strings.HasPrefix(renewalMode, "upgrade-") {
		hubs = hubs[:1] // recovery must use the original issuer, never a fallback owner
	}
	lifecycle := map[string]any{"region": map[string]any{"region_id": "east", "hubs": hubs}, "dial_timeout_seconds": 1, "watchdog_seconds": 2, "failover_budget_seconds": 5, "retry_min_ms": 20, "retry_max_ms": 50}
	if renewalMode != "" && renewalMode != "identity-rotation" && !strings.HasPrefix(renewalMode, "commands-") && !strings.HasPrefix(renewalMode, "upgrade-") && !strings.HasPrefix(renewalMode, "recovering-") && !strings.HasPrefix(renewalMode, "policy-") {
		lifecycle["renew_percent"] = 30
		lifecycle["renew_retry_min_ms"] = 1000
	}
	controlRaw, _ := json.Marshal(map[string]any{"version": 1, "node_id": regionalNode, "listeners": []any{}, "rules": []any{}, "revocations": []string{}, "lifecycle": lifecycle})
	controlPath := filepath.Join(dir, "control.json")
	os.WriteFile(controlPath, controlRaw, 0600)
	nodePath := filepath.Join(dir, "agent.db")
	if strings.HasPrefix(renewalMode, "commands-") || strings.HasPrefix(renewalMode, "upgrade-") || strings.HasPrefix(renewalMode, "recovering-") || strings.HasPrefix(renewalMode, "policy-") {
		seed, err := store.Open(nodePath, "agent")
		if err != nil {
			t.Fatal(err)
		}
		payload, err := contracts.EncodeUnknownUsage(&controlv1.UnknownUsageBatch{BatchId: "command-debt", OriginBootId: regionalBoot, BillingSnapshotId: regionalBoot, RuleId: "rule", GrantId: regionalFirst, Role: "source", Direction: "egress", UnknownUpperBound: 512})
		if err != nil {
			t.Fatal(err)
		}
		if err = seed.PersistUnknownUsage(ctx, regionalNode, payload); err != nil {
			t.Fatal(err)
		}
		seed.Close()
	}
	nodeArgs := []string{"--node-id", regionalNode, "--db", nodePath, "--engine-config", enginePath, "--control-config", controlPath, "--hub-name", regionalFirst, "--enrollment-hub", addresses[0], "--tls-ca", caPath, "--tls-cert", nodeCert, "--tls-key", nodeKey, "--credential-dir", filepath.Join(dir, "credentials"), "--renew-before", "1m"}
	var rotatedIdentitySerial string
	var identityEnrollment *hub.EnrollmentServer
	if renewalMode == "identity-rotation" {
		hubPair, err := tls.LoadX509KeyPair(filepath.Join(dir, regionalFirst+".pem"), filepath.Join(dir, regionalFirst+".key"))
		if err != nil {
			t.Fatal(err)
		}
		identityEnrollment, err = hub.NewEnrollmentServer(ca, der, map[string]bool{"unused-renewal-fixture-token": true}, &tls.Config{MinVersion: tls.VersionTLS13, Certificates: []tls.Certificate{hubPair}}, time.Hour)
		if err != nil {
			t.Fatal(err)
		}
		listener, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		go func() { _ = identityEnrollment.Serve(ctx, listener) }()
		for i, arg := range nodeArgs {
			if arg == "--enrollment-hub" {
				nodeArgs[i+1] = listener.Addr().String()
			}
			if arg == "--renew-before" {
				nodeArgs[i+1] = "59m55s"
			}
		}
	}
	if strings.HasPrefix(renewalMode, "commands-") {
		nodeArgs = append([]string{"run"}, nodeArgs...)
	}
	nodeProcess := start("vortex-node", nodeArgs...)
	db, e := openProcessLedger(nodePath)
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	if strings.HasPrefix(renewalMode, "share-invalid-") {
		peer := engineConfig(peerID)
		peer.Role = config.RoleSource
		peer.Peer = cfg.Listen
		peer.NextHopNodeID = regionalNode
		peer.ServiceListen = "127.0.0.1:0"
		exerciseInvalidShareProcess(t, ctx, db, filepath.Join(dir, "hub0.db"), peer, cfg, func() int64 { return accepted.Load() })
		return
	}
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
	var currentBoot string
	if e = db.QueryRowContext(ctx, `SELECT boot_id FROM authorization_records WHERE state='active' LIMIT 1`).Scan(&currentBoot); e != nil {
		t.Fatal(e)
	}
	t.Logf("runtime-active node_id=%s boot_id=%s path_generation=%d", regionalNode, currentBoot, cfg.PathGeneration)
	peer := engineConfig(peerID)
	peer.Role = config.RoleSource
	peer.Peer = cfg.Listen
	peer.NextHopNodeID = regionalNode
	peer.ServiceListen = "127.0.0.1:0"
	supervisor, e := link.New(peer)
	if e != nil {
		t.Fatal(e)
	}
	supervisor.TLS.ServerName = regionalNode
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
		if frame.Type != vlink.FrameData || string(frame.Payload) != payload {
			t.Fatalf("unexpected TCP frame type=%v payload=%q", frame.Type, frame.Payload)
		}
	}
	if strings.HasPrefix(renewalMode, "policy-") {
		exercisePolicyProcess(t, ctx, dir, renewalMode, db, cfg, mux, stream, exchange, func() { killProcess(t, first); restartHub() })
		return
	}
	if strings.HasPrefix(renewalMode, "recovering-") {
		exerciseRecoveringProcess(t, ctx, dir, renewalMode, db, exchange, func() { killProcess(t, first); restartHub() })
		return
	}
	if strings.HasPrefix(renewalMode, "upgrade-") {
		freshConfig := func() {
			// Protocol-test backend explicitly supplies fresh fenced configuration for
			// each replacement boot; replaying the old closed transaction must fail.
			var applied uint64
			if err := db.QueryRowContext(ctx, "SELECT value FROM agent_state WHERE key='applied_version'").Scan(&applied); err != nil {
				t.Fatal(err)
			}
			var floor uint64
			if err := db.QueryRowContext(ctx, "SELECT COALESCE(MAX(CAST(fencing_generation AS INTEGER)),9) FROM config_results").Scan(&floor); err != nil {
				t.Fatal(err)
			}
			var payload map[string]any
			if err := json.Unmarshal(runtimePayload, &payload); err != nil {
				t.Fatal(err)
			}
			payload["version"] = applied + 1
			raw, err := json.Marshal(payload)
			if err != nil {
				t.Fatal(err)
			}
			sum := sha256.Sum256(raw)
			tx := make([]byte, 16)
			if _, err = rand.Read(tx); err != nil {
				t.Fatal(err)
			}
			next := &controlv1.RuntimeConfigUpdate{Version: applied + 1, ExpectedVersion: applied, Payload: raw, PayloadSha256: sum[:], TransactionId: tx, FencingGeneration: floor + 1}
			encoded, err := json.Marshal(next)
			if err != nil {
				t.Fatal(err)
			}
			if err = os.WriteFile(updatePath, encoded, 0600); err != nil {
				t.Fatal(err)
			}
			killProcess(t, first)
			// The protocol-test backend explicitly provisions separate funded sources
			// and trusted root anchors. Never reuse/refund prior boot's occupied shares.
			fresh := d
			fresh.ShareReferences = append([]store.ShareReference(nil), d.ShareReferences...)
			makeID := func() string {
				raw := make([]byte, 16)
				if _, err := rand.Read(raw); err != nil {
					t.Fatal(err)
				}
				id := hex.EncodeToString(raw)
				return id[:8] + "-" + id[8:12] + "-" + id[12:16] + "-" + id[16:20] + "-" + id[20:]
			}
			fresh.ID = makeID()
			fresh.SourceID = makeID()
			fresh.RuntimeSourceID = makeID()
			fresh.ShareSourceID = makeID()
			sources := []string{fresh.SourceID, fresh.RuntimeSourceID, fresh.ShareSourceID}
			for i := range fresh.ShareReferences {
				fresh.ShareReferences[i].ID = makeID()
				fresh.ShareReferences[i].SourceID = sources[i]
			}
			for _, entry := range []struct{ path, owner string }{{nodePath, "agent"}, {filepath.Join(dir, "hub0.db"), "hub"}} {
				writer, err := store.Open(entry.path, entry.owner)
				if err != nil {
					t.Fatal(err)
				}
				for _, root := range fresh.ShareReferences {
					if err = writer.InstallShareSourceAnchor(ctx, fresh.NodeID, regionalBoot, root); err != nil {
						writer.Close()
						t.Fatal(err)
					}
				}
				writer.Close()
			}
			encoded, err = json.Marshal(fresh)
			if err != nil {
				t.Fatal(err)
			}
			if err = os.WriteFile(allocationPath, encoded, 0600); err != nil {
				t.Fatal(err)
			}
			restartHub()
		}
		exerciseUpgradeProcess(t, ctx, binaries, renewalMode, nodePath, peer, nodeArgs, nodeProcess, exchange, func(path string, args ...string) *exec.Cmd { freshConfig(); return start(path, args...) })
		return
	}
	if renewalMode != "" && renewalMode != "identity-rotation" && !strings.HasPrefix(renewalMode, "commands-") && !strings.HasPrefix(renewalMode, "upgrade-") && !strings.HasPrefix(renewalMode, "recovering-") && !strings.HasPrefix(renewalMode, "policy-") {
		fault := func() bool {
			return schedulerRenewalFault(t, ctx, db, dir, renewalMode, stream, exchange, func() {
				killProcess(t, first)
				if renewalMode == "hub-restart" {
					restartHub()
				} else {
					killProcess(t, nodeProcess)
					nodeProcess = start("vortex-node", nodeArgs...)
				}
			})
		}
		schedulerRenewalTraffic(t, ctx, db, dir, cfg, mux, stream, exchange, fault)
		return
	}
	if strings.HasPrefix(renewalMode, "commands-") {
		exerciseNodeCommandDrain(t, ctx, binaries, renewalMode, nodePath, cfg, mux, stream, exchange, func() { killProcess(t, nodeProcess); nodeProcess = start("vortex-node", nodeArgs...) })
		return
	}
	exchange("before-control-switch")
	if renewalMode == "identity-rotation" {
		originalPair, err := tls.LoadX509KeyPair(nodeCert, nodeKey)
		if err != nil {
			t.Fatal(err)
		}
		original, err := x509.ParseCertificate(originalPair.Certificate[0])
		if err != nil {
			t.Fatal(err)
		}
		var active *x509.Certificate
		for {
			var raw []byte
			if err = db.QueryRowContext(ctx, `SELECT certificate FROM credentials WHERE node_id=?`, regionalNode).Scan(&raw); err == nil {
				active, err = x509.ParseCertificate(raw)
				if err == nil && active.SerialNumber.Cmp(original.SerialNumber) != 0 {
					break
				}
			}
			select {
			case <-ctx.Done():
				t.Fatal("live credential renewal never committed")
			case <-time.After(20 * time.Millisecond):
			}
		}
		rotatedIdentitySerial = active.SerialNumber.String()
		exchange("existing-TCP-after-identity-rotation")
		// A fresh full TLS+exporter HELLO validates the new serial and signer,
		// including stable Agent boot and path generation across rotation.
		pair, err := tls.LoadX509KeyPair(peer.Certificate, peer.PrivateKey)
		if err != nil {
			t.Fatal(err)
		}
		roots := x509.NewCertPool()
		roots.AddCert(ca.Certificate)
		connection, err := (&tls.Dialer{Config: &tls.Config{MinVersion: tls.VersionTLS13, MaxVersion: tls.VersionTLS13, RootCAs: roots, Certificates: []tls.Certificate{pair}, ServerName: regionalNode, SessionTicketsDisabled: true}}).DialContext(ctx, "tcp", cfg.Listen)
		if err != nil {
			t.Fatal(err)
		}
		defer connection.Close()
		tlsConn := connection.(*tls.Conn)
		if tlsConn.ConnectionState().DidResume || tlsConn.ConnectionState().PeerCertificates[0].SerialNumber.Cmp(active.SerialNumber) != 0 {
			t.Fatal("fresh VLink did not select durable replacement")
		}
		var boot string
		if err = db.QueryRowContext(ctx, `SELECT boot_id FROM authorization_records WHERE kind='credit_grant' AND state='active' LIMIT 1`).Scan(&boot); err != nil {
			t.Fatal(err)
		}
		bootID, _ := vlink.ParseNodeID(boot)
		peerNode, _ := vlink.ParseNodeID(peerID)
		_, err = vlink.HandshakeHello(ctx, tlsConn, vlink.HelloConfig{Client: true, NodeID: peerNode, BootID: vlink.BootID{9}, Generation: cfg.PathGeneration, Capabilities: []uint16{1, 2}, Signer: pair.PrivateKey.(*ecdsa.PrivateKey), VerifyPeer: func(_ context.Context, cert *x509.Certificate, message vlink.HelloMessage) error {
			if cert.SerialNumber.Cmp(active.SerialNumber) != 0 || message.LocalBootID != vlink.BootID(bootID) || message.LocalGeneration != cfg.PathGeneration || message.LocalNodeID != supervisor.ExpectedPeer {
				return fmt.Errorf("rotated identity changed HELLO binding")
			}
			return nil
		}})
		if err != nil {
			t.Fatal(err)
		}
		exchange("old-TCP-after-new-signer-handshake")
		t.Logf("rotated serial %s -> %s with boot=%s generation=%d; old TCP preserved", original.SerialNumber, active.SerialNumber, boot, cfg.PathGeneration)
	}

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
	secondDB, e := openProcessLedger(filepath.Join(dir, "hub1.db"))
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
	if renewalMode == "identity-rotation" {
		var count int
		if err := secondDB.QueryRowContext(ctx, `SELECT COUNT(*) FROM durable_events WHERE kind='control-identity' AND payload=?`, []byte(rotatedIdentitySerial)).Scan(&count); err != nil || count == 0 {
			t.Fatalf("successor control stream did not authenticate rotated serial: count=%d error=%v", count, err)
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
