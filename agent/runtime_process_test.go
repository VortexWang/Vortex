package agent

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"syscall"
	"testing"
	"time"

	"google.golang.org/protobuf/proto"
	"vortex/agent/identity"
	"vortex/agent/store"
	controlv1 "vortex/contracts/protobuf/v1"
	"vortex/engine/runtime/config"
	"vortex/engine/runtime/link"
	"vortex/engine/vlink"
)

// Explicit binary qualification entry; ordinary unit runs do not build or
// silently substitute in-process services for the two process boundaries.
func TestDurableAuthorityProcessesTCP(t *testing.T) { runDurableAuthorityProcesses(t, 1, 1, 16) }
func TestDurableAuthorityProcessesUDP(t *testing.T) { runDurableAuthorityProcesses(t, 2, 1, 16) }
func TestDurableAuthorityProcessesConcurrentTCP(t *testing.T) {
	runDurableAuthorityProcesses(t, 1, 16, 16)
}

// Exceeds the initial 8 MiB grant and exercises automatic replenishment while
// the same 16 streams remain open. Still bounded by the original 60s test limit.
func TestDurableAuthorityProcessesConcurrentRenewal(t *testing.T) {
	runDurableAuthorityProcesses(t, 1, 16, 256)
}
func runDurableAuthorityProcesses(t *testing.T, network uint8, streamCount, rounds int) {
	bin := os.Getenv("VORTEX_PROCESS_BIN_DIR")
	if bin == "" {
		t.Skip("run tools/phase4/durable-process.sh with built binaries")
	}
	dir := t.TempDir()
	if out := os.Getenv("VORTEX_PROCESS_EVIDENCE_DIR"); out != "" {
		var err error
		dir, err = os.MkdirTemp(out, "process-")
		if err != nil {
			t.Fatal(err)
		}
	}
	t.Logf("process evidence: %s", dir)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	id := func(n int) string { return fmt.Sprintf("550e8400-e29b-41d4-a716-%012d", n) }
	nodeID, peerID, hubID := id(101), id(102), id(103)
	address := func() string {
		l, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		a := l.Addr().String()
		l.Close()
		return a
	}
	hubAddr, engineAddr := address(), address()
	var targetAddr string
	if network == 1 {
		target, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		defer target.Close()
		targetAddr = target.Addr().String()
		go func() {
			for {
				c, e := target.Accept()
				if e != nil {
					return
				}
				go func() { defer c.Close(); _, _ = io.Copy(c, c) }()
			}
		}()
	} else {
		target, err := net.ListenPacket("udp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		defer target.Close()
		targetAddr = target.LocalAddr().String()
		go func() {
			buf := make([]byte, 4096)
			for {
				n, peer, e := target.ReadFrom(buf)
				if e != nil {
					return
				}
				_, _ = target.WriteTo(buf[:n], peer)
			}
		}()
	}
	identities := runtimeIdentities(t, dir, nodeID, peerID)
	cfg := identities[nodeID]
	cfg.RuleID = id(110)
	cfg.Role = config.RoleDestination
	cfg.Listen = engineAddr
	cfg.Target = targetAddr
	cfg.PeerNodeID = peerID
	cfg.ExecutionPoint = true
	writeJSON := func(name string, v any) string {
		raw, e := json.Marshal(v)
		if e != nil {
			t.Fatal(e)
		}
		p := filepath.Join(dir, name)
		if e = os.WriteFile(p, raw, 0600); e != nil {
			t.Fatal(e)
		}
		return p
	}
	engineFile := writeJSON("engine.json", cfg)
	now := time.Now().UTC()
	ca, caDER, err := identity.NewCA(now)
	if err != nil {
		t.Fatal(err)
	}
	credentials := func(name string) string {
		key, csr, e := identity.GenerateCSR(name)
		if e != nil {
			t.Fatal(e)
		}
		cred, e := ca.SignCSR(csr, name, now, time.Hour)
		if e != nil {
			t.Fatal(e)
		}
		cred, e = identity.AttachKey(cred, key, caDER)
		if e != nil {
			t.Fatal(e)
		}
		p := filepath.Join(dir, "control-"+name)
		if e = identity.SaveFiles(p, cred, caDER); e != nil {
			t.Fatal(e)
		}
		return p
	}
	hubCredentials, nodeCredentials := credentials(hubID), credentials(nodeID)
	// Managed Node and VLink peer use the same approved runtime identity CA.
	// Bootstrap aliases cannot replace the credential manager's active signer.
	peerCredentials := credentials(peerID)
	cfg.Certificate, cfg.PrivateKey, cfg.TrustBundle = nodeCredentials+"/runtime-cert.pem", nodeCredentials+"/runtime-key.pem", nodeCredentials+"/trust-ca.pem"
	engineFile = writeJSON("engine.json", cfg)
	peerConfig := identities[peerID]
	peerConfig.RuleID = cfg.RuleID
	peerConfig.Certificate, peerConfig.PrivateKey, peerConfig.TrustBundle = peerCredentials+"/runtime-cert.pem", peerCredentials+"/runtime-key.pem", peerCredentials+"/trust-ca.pem"
	identities[peerID] = peerConfig
	host, portText, _ := net.SplitHostPort(cfg.Listen)
	port, _ := strconv.Atoi(portText)
	configPayload, _ := json.Marshal(map[string]any{"version": 1, "node_id": nodeID, "listeners": []any{map[string]any{"port_id": "link", "kind": "link", "address": host, "port": port}}, "rules": []any{map[string]any{"rule_id": cfg.RuleID, "revision": cfg.RuleRevision, "path_generation": cfg.PathGeneration, "role": "exit", "hop_index": cfg.HopIndex, "mode": "tunnel", "targets": []string{cfg.Target}, "allowed_upstream_node_ids": []string{peerID}}}, "revocations": []string{}})
	if err := validateRuntimeConfig(configPayload, nodeID, 1); err != nil {
		t.Fatal(err)
	}
	configDigest := sha256.Sum256(configPayload)
	configUpdate := writeJSON("config-update.json", &controlv1.RuntimeConfigUpdate{Version: 1, Payload: configPayload, PayloadSha256: configDigest[:], TransactionId: []byte("0123456789abcdef"), FencingGeneration: 8})
	initial, block, budget, rate := uint64(4096), uint64(2048), uint64(1<<20), uint64(1000000)
	if streamCount > 1 {
		initial, block, budget, rate = 8<<20, 256<<10, 64<<20, 100000000
	}
	expectedBytes := uint64(streamCount) * uint64(rounds) * 3000
	writeJSON("workload.json", map[string]any{"network": network, "streams": streamCount, "exchanges_per_stream": rounds, "payload_bytes": 1500, "initial_bytes": initial, "block_bytes": block, "rate_bps": rate, "expected_bidirectional_bytes": expectedBytes, "performance_qualified": false})
	d := store.HubDelegation{ID: id(104), HubID: hubID, SourceID: id(105), RuntimeSourceID: id(106), ShareSourceID: id(107), NodeID: nodeID, BillingSnapshotID: id(108), RuleID: cfg.RuleID, RuleRevision: cfg.RuleRevision, PathGeneration: cfg.PathGeneration, SignerEpoch: 1, FencingFloor: 8, RawBytes: budget, RateBPS: rate, Connections: 16, ValidFrom: now, ExpiresAt: now.Add(time.Hour), HardDeadline: now.Add(time.Hour), BackendHeartbeat: now, AutonomyDeadline: now.Add(2 * time.Hour)}
	fixture := writeJSON("authority.json", map[string]any{"delegation": d, "initial_request_id": id(109), "initial_bytes": initial, "ttl_seconds": 60})
	demand := writeJSON("demand.json", map[string]any{"interval_ms": 100, "demands": []any{map[string]any{"scope": store.DemandScope{HubID: hubID, DelegationID: d.ID, NodeID: nodeID, SourceID: d.SourceID, SnapshotID: d.BillingSnapshotID, RuleID: cfg.RuleID, SignerEpoch: 1, RuleRevision: cfg.RuleRevision, PathGeneration: cfg.PathGeneration}, "policy": DemandPolicyConfig{Minimum: initial, MaxTarget: initial, TargetSeconds: 30, RenewPercent: 30}, "ttl_seconds": 60}}})
	hubDB, backendDB, agentDB := filepath.Join(dir, "hub.db"), filepath.Join(dir, "backend.db"), filepath.Join(dir, "agent.db")
	starts := map[string]int{}
	start := func(name string, args ...string) func() {
		logName := name
		if starts[name] > 0 {
			logName = fmt.Sprintf("%s-restart-%d", name, starts[name])
		}
		starts[name]++
		log, e := os.Create(filepath.Join(dir, logName+".log"))
		if e != nil {
			t.Fatal(e)
		}
		command := exec.CommandContext(ctx, filepath.Join(bin, name), args...)
		command.Stdout = log
		command.Stderr = log
		if e = command.Start(); e != nil {
			log.Close()
			t.Fatal(e)
		}
		done := make(chan error, 1)
		go func() { done <- command.Wait(); log.Close() }()
		stopped := false
		stop := func() {
			if stopped {
				return
			}
			stopped = true
			_ = command.Process.Signal(syscall.SIGTERM)
			select {
			case e := <-done:
				if e != nil && ctx.Err() == nil {
					t.Errorf("%s exited: %v; inspect %s", name, e, dir)
				}
			case <-time.After(5 * time.Second):
				_ = command.Process.Kill()
				<-done
				t.Errorf("%s failed graceful shutdown", name)
			}
		}
		t.Cleanup(stop)
		return stop
	}
	stopHub := start("vortex-test-hub", "--listen", hubAddr, "--db", hubDB, "--tls-cert", hubCredentials+"/runtime-cert.pem", "--tls-key", hubCredentials+"/runtime-key.pem", "--tls-ca", hubCredentials+"/trust-ca.pem", "--authority-fixture", fixture, "--fixture-backend-db", backendDB, "--test-config-update", configUpdate)
	defer stopHub()
	stopNode := start("vortex-node", "--node-id", nodeID, "--db", agentDB, "--hub", hubAddr, "--hub-name", hubID, "--credential-dir", nodeCredentials, "--enrollment-hub", hubAddr, "--renew-before", "1m", "--engine-config", engineFile, "--experimental-funding-block-bytes", fmt.Sprint(block), "--credit-demand-config", demand)
	defer stopNode()
	// Read-only observation of durable installation; never provision the Agent DB.
	wait := func(check func() bool) {
		t.Helper()
		for !check() {
			select {
			case <-ctx.Done():
				t.Fatalf("process deadline; inspect %s", dir)
			case <-time.After(10 * time.Millisecond):
			}
		}
	}
	db, err := sql.Open("sqlite", "file:"+agentDB+"?mode=ro")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	wait(func() bool {
		var n int
		return db.QueryRowContext(ctx, "SELECT COUNT(*) FROM authorization_records WHERE kind='credit_grant'").Scan(&n) == nil && n > 0
	})
	// Durable config ACK is required before any business OPEN. A grant alone
	// cannot bypass the main activation fence.
	wait(func() bool {
		var value string
		return db.QueryRowContext(ctx, "SELECT value FROM agent_state WHERE key='applied_version'").Scan(&value) == nil && value == "1"
	})
	client := identities[peerID]
	client.Role = config.RoleSource
	client.Peer = engineAddr
	client.NextHopNodeID = nodeID
	client.ServiceListen = "127.0.0.1:0"
	sup, err := link.New(client)
	if err != nil {
		t.Fatal(err)
	}
	// The managed Node certificate uses its UUID DNS SAN, not the legacy
	// qualification-only "vortex" validation alias. Keep normal TLS verification.
	sup.TLS.ServerName = nodeID
	defer sup.Close()
	connected := make(chan *vlink.MuxLink, 1)
	go func() {
		_ = sup.Dial(ctx, func(ctx context.Context, m *vlink.MuxLink, _ vlink.NodeID) error {
			connected <- m
			<-ctx.Done()
			return ctx.Err()
		})
	}()
	var mux *vlink.MuxLink
	select {
	case mux = <-connected:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	hash := sha256.Sum256([]byte(cfg.RuleID))
	var rule [16]byte
	copy(rule[:], hash[:16])
	streams := make([]*vlink.MuxStream, streamCount)
	for i := range streams {
		stream, e := mux.Open(ctx, vlink.OpenRequest{RuleID: rule, RuleRevision: cfg.RuleRevision, PathGeneration: cfg.PathGeneration, Network: network, ClientAddress: []byte{127, 0, 0, 1}})
		if e != nil {
			t.Fatal(e)
		}
		if e = stream.WaitOpen(ctx); e != nil {
			t.Fatal(e)
		}
		streams[i] = stream
	}
	trafficStarted := time.Now()
	latencies := make([][]time.Duration, streamCount)
	for i := range latencies {
		latencies[i] = make([]time.Duration, rounds)
	}
	results := make(chan error, streamCount)
	for index, stream := range streams {
		go func(index int, stream *vlink.MuxStream) {
			message := bytes.Repeat([]byte{byte(index + 1)}, 1500)
			for round := 0; round < rounds; round++ {
				started := time.Now()
				if network == 1 {
					if e := stream.WriteData(ctx, message); e != nil {
						results <- e
						return
					}
				} else {
					accepted, e := stream.WriteDgram(ctx, message)
					if e != nil || !accepted {
						results <- fmt.Errorf("datagram rejected: %v", e)
						return
					}
				}
				received := []byte{}
				for len(received) < len(message) {
					frame, e := stream.Receive(ctx)
					if e != nil {
						results <- e
						return
					}
					expectedType := vlink.FrameData
					if network == 2 {
						expectedType = vlink.FrameDgram
					}
					if frame.Type != expectedType || network == 2 && len(frame.Payload) != len(message) {
						frameErr := fmt.Errorf("stream=%d round=%d unexpected frame=%d payload=%x", index, round, frame.Type, frame.Payload)
						frame.Release()
						results <- frameErr
						return
					}
					received = append(received, frame.Payload...)
					frame.Release()
				}
				latencies[index][round] = time.Since(started)
				if !bytes.Equal(received, message) {
					results <- fmt.Errorf("payload mismatch on stream %d", index)
					return
				}
			}
			results <- nil
		}(index, stream)
	}
	for range streams {
		if e := <-results; e != nil {
			t.Fatal(e)
		}
	}
	elapsed := time.Since(trafficStarted)
	var samples []time.Duration
	for _, streamSamples := range latencies {
		samples = append(samples, streamSamples...)
	}
	sort.Slice(samples, func(i, j int) bool { return samples[i] < samples[j] })
	percentile := func(p int) float64 { return float64(samples[(len(samples)*p+99)/100-1]) / float64(time.Millisecond) }
	writeJSON("traffic.json", map[string]any{"streams": streamCount, "network": network, "round_trips": len(samples), "round_trip_ms": map[string]float64{"p50": percentile(50), "p95": percentile(95), "p99": percentile(99)}, "traffic_elapsed_ns": elapsed.Nanoseconds(), "bidirectional_payload_bytes": expectedBytes, "bidirectional_payload_mbps": float64(expectedBytes) * 8 / elapsed.Seconds() / 1e6, "timing_scope": "request-response traffic; excludes later ACK drain", "performance_qualified": false})

	wait(func() bool {
		// Poll the pending predicate without repeatedly decoding thousands of
		// immutable payloads while the independent processes drain their ACKs.
		// Once drained, still verify every durable payload and its exact total.
		var pending bool
		if err := db.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM usage_batches WHERE state!='hub_acked')").Scan(&pending); err != nil || pending {
			return false
		}
		rows, err := db.QueryContext(ctx, "SELECT payload,state FROM usage_batches")
		if err != nil {
			return false
		}
		defer rows.Close()
		var total uint64
		for rows.Next() {
			var payload []byte
			var state string
			if rows.Scan(&payload, &state) != nil || state != "hub_acked" {
				return false
			}
			var batch controlv1.UsageBatch
			if proto.Unmarshal(payload, &batch) != nil {
				return false
			}
			for _, delta := range batch.Deltas {
				total += delta.Bytes
			}
		}
		return rows.Err() == nil && total == expectedBytes
	})
	for _, stream := range streams {
		_ = stream.Reset(ctx, 0)
	}
	stopNode()
	stopHub()
	// A fixed fixture heartbeat intentionally enters autonomy after 30 seconds.
	// Its ACK then proves Hub durability, not backend delivery. Restart the Hub
	// with the same databases to exercise durable accounting replay explicitly.
	stopRecoveredHub := start("vortex-test-hub", "--listen", hubAddr, "--db", hubDB, "--tls-cert", hubCredentials+"/runtime-cert.pem", "--tls-key", hubCredentials+"/runtime-key.pem", "--tls-ca", hubCredentials+"/trust-ca.pem", "--authority-fixture", fixture, "--fixture-backend-db", backendDB, "--test-config-update", configUpdate)
	defer stopRecoveredHub()
	hubObservation, err := sql.Open("sqlite", "file:"+hubDB+"?mode=ro")
	if err != nil {
		t.Fatal(err)
	}
	defer hubObservation.Close()
	wait(func() bool {
		connection, err := net.DialTimeout("tcp", hubAddr, 100*time.Millisecond)
		if err != nil {
			return false
		}
		connection.Close()
		// An existence check has the same zero-pending condition, but stops at
		// the first missing receipt while the recovered process drains the rest.
		var pending bool
		err = hubObservation.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM usage_batches u WHERE NOT EXISTS (SELECT 1 FROM usage_backend_receipts r WHERE r.node_id=u.node_id AND r.batch_id=u.batch_id))`).Scan(&pending)
		return err == nil && !pending
	})
	stopRecoveredHub()
	db.Close()
	// Writers have stopped: audit durable source funds as well as usage rows.
	agentAudit, err := store.Open(agentDB, "agent")
	if err != nil {
		t.Fatal(err)
	}
	defer agentAudit.Close()
	source, err := agentAudit.AuditAuthorizationSource(ctx, nodeID, d.SourceID)
	if err != nil || source.Committed != expectedBytes || source.Unknown != 0 || source.Reserved != 0 || source.Initial != source.Remaining+source.Committed {
		t.Fatalf("Agent source conservation %+v %v", source, err)
	}
	hubAudit, err := store.Open(hubDB, "hub")
	if err != nil {
		t.Fatal(err)
	}
	defer hubAudit.Close()
	delegated, err := hubAudit.AuditHubDelegation(ctx, d.ID)
	if err != nil || delegated.Initial != d.RawBytes || delegated.Issued != source.Initial || delegated.Initial != delegated.Remaining+delegated.Issued {
		t.Fatalf("Hub/Agent funding reconciliation %+v agent=%+v err=%v", delegated, source, err)
	}
	if expectedBytes > initial && source.Initial <= initial {
		t.Fatalf("workload did not obtain additional durable funding: initial=%d funded=%d", initial, source.Initial)
	}
	if err := agentAudit.Close(); err != nil {
		t.Fatal(err)
	}
	if err := hubAudit.Close(); err != nil {
		t.Fatal(err)
	}
	assertFragmentLedgers(t, []string{agentDB, hubDB, backendDB}, expectedBytes)
	saveLedgerFiles(t, fmt.Sprintf("process-network-%d", network), map[string]string{"agent": agentDB, "hub": hubDB, "backend": backendDB})
}
