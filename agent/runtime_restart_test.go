//go:build linux

package agent

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"vortex/agent/store"
	controlv1 "vortex/contracts/protobuf/v1"
	"vortex/engine/runtime/config"
	"vortex/engine/runtime/link"
	"vortex/engine/vlink"
	"vortex/hub"
)

type runtimeRestartReady struct{ Boot, Address, Transaction, Grant, Source string }

// Worker owns a real Engine listener and WAL. Parent kills it without shutdown.
func TestRuntimeRestartWorker(t *testing.T) {
	dir := os.Getenv("VORTEX_RESTART_WORKER")
	if dir == "" {
		return
	}
	ctx := context.Background()
	stage := os.Getenv("VORTEX_RESTART_STAGE")
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	s, err := store.Open(filepath.Join(dir, "agent.db"), "agent")
	must(err)
	e, err := NewRuntimeEngine(filepath.Join(dir, "engine.json"), controlTestNodeID)
	must(err)
	n, err := New(controlTestNodeID, s, e)
	must(err)
	applier, err := n.RuntimeApplier()
	must(err)
	must(n.Start(ctx))
	var version, generation uint64 = 1, 9
	expected := uint64(0)
	if stage == "restart" {
		version, generation, expected = 2, 10, 1
	}
	host, portText, err := net.SplitHostPort(e.runtime.Addr().String())
	must(err)
	port, err := strconv.Atoi(portText)
	must(err)
	rule := map[string]any{"rule_id": e.config.RuleID, "revision": e.config.RuleRevision, "path_generation": e.config.PathGeneration, "role": "exit", "hop_index": e.config.HopIndex, "mode": "tunnel", "targets": []string{e.config.Target}, "allowed_upstream_node_ids": []string{e.config.PeerNodeID}}
	raw, err := json.Marshal(map[string]any{"version": version, "node_id": n.NodeID, "listeners": []any{map[string]any{"port_id": "link", "kind": "link", "address": host, "port": port}}, "rules": []any{rule}, "revocations": []string{}})
	must(err)
	must(validateRuntimeConfig(raw, n.NodeID, version))
	txid := bytes.Repeat([]byte{byte(version)}, 16)
	digest := sha256.Sum256(raw)
	update := &controlv1.RuntimeConfigUpdate{Version: version, Payload: raw, PayloadSha256: digest[:], TransactionId: txid, FencingGeneration: generation, ExpectedVersion: expected}
	reconciler := &Reconciler{Store: s, Apply: applier}
	first := reconciler.ApplyRuntimeConfig(ctx, n.NodeID, update)
	if first.State != "prepared" {
		t.Fatalf("before current-boot authority: %+v", first)
	}
	if e.activated.Load() {
		t.Fatal("activated without authority")
	}
	hs, err := store.Open(filepath.Join(dir, "hub.db"), "hub")
	must(err)
	now := time.Now().UTC()
	newID := func() string {
		id, err := authorizationBatchID()
		must(err)
		return id[:8] + "-" + id[8:12] + "-" + id[12:16] + "-" + id[16:20] + "-" + id[20:]
	}
	d := store.HubDelegation{ID: newID(), HubID: "550e8400-e29b-41d4-a716-446655440060", SourceID: newID(), RuntimeSourceID: newID(), ShareSourceID: newID(), NodeID: n.NodeID, BootID: n.BootID, BillingSnapshotID: "550e8400-e29b-41d4-a716-446655440024", RuleID: e.scope.RuleID, RuleRevision: e.scope.RuleRevision, PathGeneration: e.scope.PathGeneration, FencingFloor: generation, SignerEpoch: 1, RawBytes: 16384, RateBPS: 10000000, Connections: 2, ValidFrom: now, ExpiresAt: now.Add(time.Hour), HardDeadline: now.Add(time.Hour), BackendHeartbeat: now, AutonomyDeadline: now.Add(2 * time.Hour)}
	clock := func() store.IssuerTime {
		at := time.Now().UTC()
		return store.IssuerTime{Earliest: at, Latest: at, Trusted: true}
	}
	must(hs.InstallHubDelegation(ctx, d, clock()))
	issuer := &hub.Issuer{Store: hs, HubID: d.HubID, Clock: clock}
	auth, err := issuer.Issue(ctx, d.ID, n.NodeID, n.BootID, newID(), 8192, time.Minute)
	must(err)
	serverTLS, clientTLS, _ := controlTLS(t, n.NodeID)
	server := startTestControlHub(t, ctx, &hub.TestServer{Store: hs, ConfigUpdate: update, AuthorizationUpdate: func(id, boot string) (*controlv1.AuthorizationUpdate, error) { return auth, nil }}, serverTLS)
	client := &ControlClient{Node: n, Reconciler: reconciler, TLS: clientTLS}
	must(client.Connect(ctx, server.Addr().String()))
	payload, err := validateAuthorizationUpdate(auth.Payload, n.NodeID, n.BootID, 4)
	must(err)
	result, err := s.GetConfigResult(ctx, hex.EncodeToString(txid))
	must(err)
	if result.State != "active" || !e.activated.Load() {
		t.Fatalf("authenticated prepared request did not activate: %+v", result)
	}
	// Bootstrap bounds still conservatively cover the whole first connection.
	for {
		low, _, ok := n.TrustedTime()
		if ok && !low.Before(payload.IssuedAt) {
			break
		}
		time.Sleep(time.Millisecond)
	}
	var grant string
	for _, r := range payload.Authorizations {
		if r.Kind == "credit_grant" {
			grant = r.AuthorizationID
		}
	}
	if stage == "initial" {
		low, high, trusted := n.TrustedTime()
		_, err = s.ReserveAuthorizedUsage(ctx, store.UsageReservation{NodeID: n.NodeID, BootID: n.BootID, AuthorizationID: grant, BatchID: "interrupted-write", SnapshotID: d.BillingSnapshotID, FlowKey: "interrupted-flow", RuleID: d.RuleID, Direction: "egress", Reserved: 512}, store.AuthorizationUse{NodeID: n.NodeID, BootID: n.BootID, AuthorizationID: grant, Kind: "credit_grant", RuleID: d.RuleID, RuleRevision: d.RuleRevision, PathGeneration: d.PathGeneration, BillingSnapshotID: d.BillingSnapshotID, Earliest: low, Latest: high, TrustedTime: trusted})
		must(err)
	} else {
		db, err := sql.Open("sqlite", filepath.Join(dir, "agent.db"))
		must(err)
		var upper, liability string
		err = db.QueryRow(`SELECT unknown_upper_bound,liability_state FROM unknown_usage_batches WHERE batch_id='interrupted-write'`).Scan(&upper, &liability)
		must(err)
		must(db.Close())
		if upper != "512" || liability != "unresolved" {
			t.Fatalf("old liability changed after transport ACK: %s %s", upper, liability)
		}
	}

	ready, _ := json.Marshal(runtimeRestartReady{n.BootID, e.runtime.Addr().String(), hex.EncodeToString(txid), grant, d.SourceID})
	fmt.Printf("restart-ready:%s\n", ready)
	var input [1]byte
	_, _ = os.Stdin.Read(input[:])
	os.Exit(2)
}

func TestRuntimeSIGKILLFreshBootReprepareAndRecoverDebt(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 35*time.Second)
	defer cancel()
	dir := t.TempDir()
	const peerID = "550e8400-e29b-41d4-a716-446655440099"
	identities := runtimeIdentities(t, dir, controlTestNodeID, peerID)
	target, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer target.Close()
	go func() {
		for {
			c, err := target.Accept()
			if err != nil {
				return
			}
			go func() { defer c.Close(); io.Copy(c, c) }()
		}
	}()
	cfg := identities[controlTestNodeID]
	cfg.RuleID = "550e8400-e29b-41d4-a716-446655440077"
	peerConfig := identities[peerID]
	peerConfig.RuleID = cfg.RuleID
	identities[peerID] = peerConfig
	cfg.Role = config.RoleDestination
	cfg.Target = target.Addr().String()
	cfg.PeerNodeID = peerID
	cfg.ExecutionPoint = true
	raw, _ := json.Marshal(cfg)
	if err = os.WriteFile(filepath.Join(dir, "engine.json"), raw, 0600); err != nil {
		t.Fatal(err)
	}
	launch := func(stage string) (*exec.Cmd, io.WriteCloser, runtimeRestartReady) {
		binary, err := os.Executable()
		if err != nil {
			t.Fatal(err)
		}
		cmd := exec.CommandContext(ctx, binary, "-test.run=^TestRuntimeRestartWorker$")
		cmd.Env = append(os.Environ(), "VORTEX_RESTART_WORKER="+dir, "VORTEX_RESTART_STAGE="+stage)
		stdout, err := cmd.StdoutPipe()
		if err != nil {
			t.Fatal(err)
		}
		stdin, err := cmd.StdinPipe()
		if err != nil {
			t.Fatal(err)
		}
		stderr := &bytes.Buffer{}
		cmd.Stderr = stderr
		if err = cmd.Start(); err != nil {
			t.Fatal(err)
		}
		scanner := bufio.NewScanner(stdout)
		var ready runtimeRestartReady
		found := false
		var childOutput strings.Builder
		for scanner.Scan() {
			childOutput.WriteString(scanner.Text() + "\n")
			if strings.HasPrefix(scanner.Text(), "restart-ready:") {
				if err = json.Unmarshal([]byte(strings.TrimPrefix(scanner.Text(), "restart-ready:")), &ready); err != nil {
					t.Fatal(err)
				}
				found = true
				break
			}
		}
		if !found {
			_ = cmd.Wait()
			t.Fatalf("worker %s failed: %s %s", stage, stderr.String(), childOutput.String())
		}
		t.Cleanup(func() { cmd.Process.Kill(); cmd.Wait(); stdin.Close() })
		return cmd, stdin, ready
	}
	exchange := func(ready runtimeRestartReady) {
		clientCfg := identities[peerID]
		clientCfg.Role = config.RoleSource
		clientCfg.Peer = ready.Address
		clientCfg.NextHopNodeID = controlTestNodeID
		clientCfg.ServiceListen = "127.0.0.1:0"
		sup, err := link.New(clientCfg)
		if err != nil {
			t.Fatal(err)
		}
		defer sup.Close()
		connected := make(chan *vlink.MuxLink, 1)
		dialCtx, stop := context.WithCancel(ctx)
		defer stop()
		go func() {
			_ = sup.Dial(dialCtx, func(ctx context.Context, m *vlink.MuxLink, _ vlink.NodeID) error {
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
		stream, err := mux.Open(ctx, vlink.OpenRequest{RuleID: rule, RuleRevision: 2, PathGeneration: 4, Network: 1, ClientAddress: []byte{127, 0, 0, 1}})
		if err != nil {
			t.Fatal(err)
		}
		if err = stream.WaitOpen(ctx); err != nil {
			t.Fatal(err)
		}
		message := []byte("real-runtime-after-boot")
		if err = stream.WriteData(ctx, message); err != nil {
			t.Fatal(err)
		}
		frame, err := stream.Receive(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer frame.Release()
		if !bytes.Equal(frame.Payload, message) {
			t.Fatalf("payload %q", frame.Payload)
		}
	}
	first, _, old := launch("initial")
	exchange(old)
	if err = first.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	_ = first.Wait()
	second, _, fresh := launch("restart")
	exchange(fresh)
	if old.Boot == fresh.Boot || old.Source == fresh.Source {
		t.Fatal("restart reused old authority")
	}
	if err = second.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	_ = second.Wait()
	s, err := store.Open(filepath.Join(dir, "agent.db"), "agent")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	previous, err := s.GetConfigResult(ctx, old.Transaction)
	if err != nil || previous.State != "released" {
		t.Fatalf("old process proof %+v %v", previous, err)
	}
	balance, err := s.AuditAuthorizationBalance(ctx, controlTestNodeID, old.Grant)
	if err != nil || balance.Unknown < 512 {
		t.Fatalf("unknown debt %+v %v", balance, err)
	}
	db, err := sql.Open("sqlite", filepath.Join(dir, "agent.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var proof int
	if err = db.QueryRow(`SELECT COUNT(*) FROM activation_observations WHERE execution_state='os-process-stopped'`).Scan(&proof); err != nil || proof < 1 {
		t.Fatal("missing process-death proof", proof, err)
	}
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	db.Close()
	hs, err := store.Open(filepath.Join(dir, "hub.db"), "hub")
	if err != nil {
		t.Fatal(err)
	}
	if err = hs.Close(); err != nil {
		t.Fatal(err)
	}
	saveLedgerFiles(t, "restart", map[string]string{"agent": filepath.Join(dir, "agent.db"), "hub": filepath.Join(dir, "hub.db")})
}
