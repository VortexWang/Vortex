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
func TestDurableAuthorityProcessesTCP(t *testing.T) {
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
	target, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer target.Close()
	go func() {
		for {
			c, e := target.Accept()
			if e != nil {
				return
			}
			go func() { defer c.Close(); _, _ = io.Copy(c, c) }()
		}
	}()
	identities := runtimeIdentities(t, dir, nodeID, peerID)
	cfg := identities[nodeID]
	cfg.Role = config.RoleDestination
	cfg.Listen = engineAddr
	cfg.Target = target.Addr().String()
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
	d := store.HubDelegation{ID: id(104), HubID: hubID, SourceID: id(105), RuntimeSourceID: id(106), ShareSourceID: id(107), NodeID: nodeID, BillingSnapshotID: id(108), RuleID: cfg.RuleID, RuleRevision: cfg.RuleRevision, PathGeneration: cfg.PathGeneration, SignerEpoch: 1, FencingFloor: 8, RawBytes: 1 << 20, RateBPS: 1000000, Connections: 16, ValidFrom: now, ExpiresAt: now.Add(time.Hour), HardDeadline: now.Add(time.Hour), BackendHeartbeat: now, AutonomyDeadline: now.Add(2 * time.Hour)}
	fixture := writeJSON("authority.json", map[string]any{"delegation": d, "initial_request_id": id(109), "initial_bytes": 4096, "ttl_seconds": 60})
	demand := writeJSON("demand.json", map[string]any{"interval_ms": 100, "demands": []any{map[string]any{"scope": store.DemandScope{HubID: hubID, DelegationID: d.ID, NodeID: nodeID, SourceID: d.SourceID, SnapshotID: d.BillingSnapshotID, RuleID: cfg.RuleID, SignerEpoch: 1, RuleRevision: cfg.RuleRevision, PathGeneration: cfg.PathGeneration}, "policy": DemandPolicyConfig{Minimum: 4096, MaxTarget: 4096, TargetSeconds: 30, RenewPercent: 30}, "ttl_seconds": 60}}})
	hubDB, backendDB, agentDB := filepath.Join(dir, "hub.db"), filepath.Join(dir, "backend.db"), filepath.Join(dir, "agent.db")
	start := func(name string, args ...string) func() {
		log, e := os.Create(filepath.Join(dir, name+".log"))
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
	stopHub := start("vortex-test-hub", "--listen", hubAddr, "--db", hubDB, "--tls-cert", hubCredentials+"/runtime-cert.pem", "--tls-key", hubCredentials+"/runtime-key.pem", "--tls-ca", hubCredentials+"/trust-ca.pem", "--authority-fixture", fixture, "--fixture-backend-db", backendDB)
	stopNode := start("vortex-node", "--node-id", nodeID, "--db", agentDB, "--hub", hubAddr, "--hub-name", hubID, "--credential-dir", nodeCredentials, "--enrollment-hub", hubAddr, "--renew-before", "1m", "--engine-config", engineFile, "--experimental-funding-block-bytes", "2048", "--credit-demand-config", demand)
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
	client := identities[peerID]
	client.Role = config.RoleSource
	client.Peer = engineAddr
	client.NextHopNodeID = nodeID
	client.ServiceListen = "127.0.0.1:0"
	sup, err := link.New(client)
	if err != nil {
		t.Fatal(err)
	}
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
	stream, err := mux.Open(ctx, vlink.OpenRequest{RuleID: rule, RuleRevision: cfg.RuleRevision, PathGeneration: cfg.PathGeneration, Network: 1, ClientAddress: []byte{127, 0, 0, 1}})
	if err != nil {
		t.Fatal(err)
	}
	if err = stream.WaitOpen(ctx); err != nil {
		t.Fatal(err)
	}
	message := bytes.Repeat([]byte{0x5a}, 1500)
	for round := 0; round < 16; round++ {
		if err := stream.WriteData(ctx, message); err != nil {
			t.Fatal(err)
		}
		received := []byte{}
		for len(received) < len(message) {
			frame, e := stream.Receive(ctx)
			if e != nil {
				t.Fatal(e)
			}
			if frame.Type != vlink.FrameData {
				frame.Release()
				t.Fatalf("non-DATA frame %d", frame.Type)
			}
			received = append(received, frame.Payload...)
			frame.Release()
		}
		if !bytes.Equal(received, message) {
			t.Fatal("TCP payload mismatch")
		}
	}
	wait(func() bool {
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
		return rows.Err() == nil && total == 48000
	})
	_ = stream.Reset(ctx, 0)
	stopNode()
	stopHub()
	db.Close()
	assertFragmentLedgers(t, []string{agentDB, hubDB, backendDB}, 48000)
	saveLedgerFiles(t, "process-tcp", map[string]string{"agent": agentDB, "hub": hubDB, "backend": backendDB})
}
