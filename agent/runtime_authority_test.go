package agent

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"database/sql"
	"encoding/binary"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"sync/atomic"
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
	"vortex/hub"
)

func leasedPayload(t *testing.T, node, boot string) []byte {
	t.Helper()
	var p map[string]any
	if err := json.Unmarshal(authorizationPayloadForTest(t, node, boot, 1, 4), &p); err != nil {
		t.Fatal(err)
	}
	grant := p["authorizations"].([]any)[0].(map[string]any)
	entries := []any{grant}
	for i, kind := range []string{"runtime_lease", "share_lease"} {
		r := map[string]any{}
		for k, v := range grant {
			r[k] = v
		}
		delete(r, "amount_bytes")
		delete(r, "billing_snapshot_id")
		r["kind"] = kind
		r["authorization_id"] = fmt.Sprintf("550e8400-e29b-41d4-a716-44665544003%d", i)
		if kind == "share_lease" {
			r["rate_bps"] = 1000000
			r["connections"] = 1
		}
		entries = append(entries, r)
	}
	p["authorizations"] = entries
	raw, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func runtimeIdentities(t *testing.T, dir string, ids ...string) map[string]config.Config {
	t.Helper()
	ca, _, err := identity.NewCA(time.Now())
	if err != nil {
		t.Fatal(err)
	}
	write := func(name, kind string, der []byte) string {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, pem.EncodeToMemory(&pem.Block{Type: kind, Bytes: der}), 0600); err != nil {
			t.Fatal(err)
		}
		return path
	}
	trust := write("ca.pem", "CERTIFICATE", ca.Certificate.Raw)
	result := map[string]config.Config{}
	for i, id := range ids {
		key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		if err != nil {
			t.Fatal(err)
		}
		uri, _ := url.Parse("urn:vortex:node:" + id)
		cert := &x509.Certificate{SerialNumber: big.NewInt(int64(i + 10)), URIs: []*url.URL{uri}, DNSNames: []string{"vortex"}, NotBefore: time.Now().Add(-time.Minute), NotAfter: time.Now().Add(time.Hour), KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth, x509.ExtKeyUsageServerAuth}}
		der, err := x509.CreateCertificate(rand.Reader, cert, ca.Certificate, &key.PublicKey, ca.Key)
		if err != nil {
			t.Fatal(err)
		}
		keyDER, err := x509.MarshalPKCS8PrivateKey(key)
		if err != nil {
			t.Fatal(err)
		}
		result[id] = config.Config{NodeID: id, Version: 1, Listen: "127.0.0.1:0", Certificate: write(id+".pem", "CERTIFICATE", der), PrivateKey: write(id+".key", "PRIVATE KEY", keyDER), TrustBundle: trust, RuleID: "rule-1", RuleRevision: 2, PathGeneration: 4}
	}
	return result
}

func TestRuntimeAutomaticallyUsesDurableLeaseAuthorityBeforeTarget(t *testing.T) {
	runDurableRuntimeAuthority(t, 0)
}

func TestRuntimeContinuousDurableAdaptiveRenewal(t *testing.T) {
	runDurableRuntimeAuthority(t, 8)
}

func TestRuntimeLongLivedDurableAdaptiveRenewal(t *testing.T) {
	if testing.Short() {
		t.Skip("long-lived durable renewal integration")
	}
	runDurableRuntimeAuthority(t, 32)
}

func TestRuntimeDelayedDurableAdaptiveRenewal(t *testing.T) {
	runDurableRuntimeAuthority(t, 8, true)
}

func TestRuntimeFundingBlockDelayedDurableRenewal(t *testing.T) {
	runDurableRuntimeAuthorityMode(t, 2, true, true)
}

func TestRuntimeFundingBlockDurableRenewal(t *testing.T) {
	runDurableRuntimeAuthorityMode(t, 2, true)
}

func runDurableRuntimeAuthority(t *testing.T, renewalRounds int, delayResponse ...bool) {
	runDurableRuntimeAuthorityMode(t, renewalRounds, false, delayResponse...)
}

func TestRuntimeFundingBlockContinuousTraffic(t *testing.T) {
	runDurableRuntimeAuthorityWorkload(t, 16, true, true)
}

func TestRuntimeDurableD04Candidate(t *testing.T) {
	if os.Getenv("VORTEX_D04_DURABLE") != "1" {
		t.Skip("durable D-04 candidate workload is opt-in")
	}
	runDurableRuntimeAuthorityWorkload(t, 0, true, false)
}

type durableD04Params struct {
	blockBytes, targetBytes, targetSeconds, waterlinePercent uint64
	initialGrant, renewalBytes, messageBytes                 uint64
	delegationBytes                                          uint64
	exchangeCount                                            int
	candidate                                                bool
}

func durableD04Parameters(t *testing.T) durableD04Params {
	t.Helper()
	params := durableD04Params{
		blockBytes:       2048,
		targetBytes:      4096,
		targetSeconds:    30,
		waterlinePercent: 30,
		initialGrant:     4096,
		renewalBytes:     3000,
		messageBytes:     1500,
	}
	if os.Getenv("VORTEX_D04_DURABLE") != "1" {
		params.delegationBytes = 12288
		return params
	}
	params.candidate = true
	parse := func(name string, fallback uint64) uint64 {
		value := os.Getenv(name)
		if value == "" {
			return fallback
		}
		parsed, err := strconv.ParseUint(value, 10, 64)
		if err != nil {
			t.Fatalf("invalid %s=%q: %v", name, value, err)
		}
		return parsed
	}
	params.blockBytes = parse("VORTEX_D04_BLOCK_BYTES", params.blockBytes)
	params.targetBytes = parse("VORTEX_D04_TARGET_BYTES", params.blockBytes*2)
	params.targetSeconds = parse("VORTEX_D04_TARGET_SECONDS", params.targetSeconds)
	params.waterlinePercent = parse("VORTEX_D04_RENEW_PERCENT", params.waterlinePercent)
	if params.blockBytes == 0 || params.blockBytes > 4<<20 || params.targetBytes < params.blockBytes || params.targetBytes > 16<<20 || params.targetSeconds == 0 || params.targetSeconds > 60 || params.waterlinePercent == 0 || params.waterlinePercent >= 100 {
		t.Fatalf("invalid durable D-04 parameters: %+v", params)
	}
	params.initialGrant = params.targetBytes
	required := params.targetBytes - (params.targetBytes*params.waterlinePercent)/100 + 1
	// The fixture's UDP target reads one 4 KiB datagram at a time; use the
	// common bound for both transports and repeat exchanges to cross the gate.
	params.messageBytes = 4096
	if params.messageBytes > params.blockBytes {
		params.messageBytes = params.blockBytes
	}
	params.exchangeCount = int((required + 2*params.messageBytes - 1) / (2 * params.messageBytes))
	params.renewalBytes = uint64(2 * params.exchangeCount * int(params.messageBytes))
	if params.messageBytes == 0 || params.messageBytes > params.blockBytes {
		t.Fatalf("durable D-04 traffic does not fit one funding block: %+v", params)
	}
	params.delegationBytes = params.initialGrant + params.renewalBytes + params.targetBytes
	return params
}

func runDurableRuntimeAuthorityMode(t *testing.T, renewalRounds int, blockMode bool, delayResponse ...bool) {
	runDurableRuntimeAuthorityWorkload(t, renewalRounds, blockMode, false, delayResponse...)
}

func runDurableRuntimeAuthorityWorkload(t *testing.T, renewalRounds int, blockMode, continuous bool, delayResponse ...bool) {
	t.Helper()
	params := durableD04Parameters(t)
	for _, network := range []uint8{1, 2} {
		t.Run(fmt.Sprint(network), func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), (25+time.Duration(renewalRounds)*3)*time.Second)
			if params.candidate {
				cancel()
				ctx, cancel = context.WithTimeout(context.Background(), 90*time.Second)
			}
			defer cancel()
			const peerID = "550e8400-e29b-41d4-a716-446655440099"
			const runtimeHubID = "550e8400-e29b-41d4-a716-446655440060"
			dir := t.TempDir()
			identities := runtimeIdentities(t, dir, controlTestNodeID, peerID)
			var targets atomic.Int64
			var targetBytes atomic.Int64
			var targetAddr string
			if network == 1 {
				listener, err := net.Listen("tcp", "127.0.0.1:0")
				if err != nil {
					t.Fatal(err)
				}
				defer listener.Close()
				targetAddr = listener.Addr().String()
				go func() {
					for {
						c, err := listener.Accept()
						if err != nil {
							return
						}
						targets.Add(1)
						go func() {
							defer c.Close()
							b := make([]byte, 4096)
							for {
								n, err := c.Read(b)
								if n > 0 {
									targetBytes.Add(int64(n))
									if _, err := c.Write(b[:n]); err != nil {
										return
									}
								}
								if err != nil {
									return
								}
							}
						}()
					}
				}()
			} else {
				pc, err := net.ListenPacket("udp", "127.0.0.1:0")
				if err != nil {
					t.Fatal(err)
				}
				defer pc.Close()
				targetAddr = pc.LocalAddr().String()
				go func() {
					b := make([]byte, 4096)
					for {
						n, a, err := pc.ReadFrom(b)
						if err != nil {
							return
						}
						targets.Add(1)
						targetBytes.Add(int64(n))
						_, _ = pc.WriteTo(b[:n], a)
					}
				}()
			}
			cfg := identities[controlTestNodeID]
			cfg.Role = config.RoleDestination
			cfg.Target = targetAddr
			cfg.PeerNodeID = peerID
			cfg.ExecutionPoint = true
			raw, _ := json.Marshal(cfg)
			path := filepath.Join(dir, "engine.json")
			if err := os.WriteFile(path, raw, 0600); err != nil {
				t.Fatal(err)
			}
			agentDB := filepath.Join(dir, "agent.db")
			fullDiskRoot := os.Getenv("VORTEX_TEST_FULL_DISK_DIR")
			// The bounded fault filesystem is for the short failure scenario.
			// Long-lived workload ledgers must not fill it before deliberate injection.
			if renewalRounds > 0 || continuous {
				fullDiskRoot = ""
			}
			if fullDiskRoot != "" {
				diskDir, err := os.MkdirTemp(fullDiskRoot, "node-")
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { os.RemoveAll(diskDir) })
				agentDB = filepath.Join(diskDir, "agent.db")
			}
			s, err := store.Open(agentDB, "agent")
			if err != nil {
				t.Fatal(err)
			}
			defer s.Close()
			e, err := NewRuntimeEngine(path, controlTestNodeID)
			if err != nil {
				t.Fatal(err)
			}
			node, err := New(controlTestNodeID, s, e)
			if err != nil {
				t.Fatal(err)
			}
			if blockMode {
				for _, invalid := range []uint64{0, (4 << 20) + 1} {
					if err := node.EnableExperimentalFundingBlocks(invalid); err == nil {
						t.Fatalf("invalid block size accepted: %d", invalid)
					}
				}
				if err := node.EnableExperimentalFundingBlocks(params.blockBytes); err != nil {
					t.Fatal(err)
				}
			}
			if err := node.Start(ctx); err != nil {
				t.Fatal(err)
			}
			defer node.Stop(context.Background())
			defer func() {
				if !t.Failed() {
					return
				}
				low, high, trusted := node.TrustedTime()
				t.Logf("failed runtime authority bounds low=%s high=%s width=%s trusted=%t", low, high, high.Sub(low), trusted)
				reader, err := sql.Open("sqlite", agentDB)
				if err != nil {
					return
				}
				defer reader.Close()
				rows, err := reader.Query(`SELECT tokens,last_tick,boot_id FROM runtime_rate_buckets`)
				if err != nil {
					return
				}
				defer rows.Close()
				for rows.Next() {
					var tokens uint64
					var tick int64
					var boot string
					if rows.Scan(&tokens, &tick, &boot) == nil {
						t.Logf("failed runtime durable rate tokens=%d tick=%d boot=%s", tokens, tick, boot)
					}
				}
			}()
			if blockMode {
				if err := node.EnableExperimentalFundingBlocks(params.blockBytes / 2); err == nil {
					t.Fatal("live allocator replacement accepted")
				}
			}
			clientConfig := identities[peerID]
			clientConfig.Role = config.RoleSource
			clientConfig.Peer = e.runtime.Addr().String()
			clientConfig.NextHopNodeID = controlTestNodeID
			clientConfig.ServiceListen = "127.0.0.1:0"
			sup, err := link.New(clientConfig)
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
			digest := sha256.Sum256([]byte(cfg.RuleID))
			var rule [16]byte
			copy(rule[:], digest[:16])
			open := func() *vlink.MuxStream {
				s, err := mux.Open(ctx, vlink.OpenRequest{RuleID: rule, RuleRevision: 2, PathGeneration: 4, Network: network, ClientAddress: []byte{127, 0, 0, 1}})
				if err != nil {
					t.Fatal(err)
				}
				return s
			}
			denied := open()
			if err := denied.WaitOpen(ctx); err == nil {
				t.Fatal("runtime admitted traffic before authorization")
			}
			if targets.Load() != 0 {
				t.Fatal("unauthorized OPEN reached target")
			}
			serverTLS, clientTLS, _ := controlTLSForHub(t, controlTestNodeID, runtimeHubID)
			hubDB := filepath.Join(dir, "hub.db")
			if fullDiskRoot != "" {
				hubDB = filepath.Join(filepath.Dir(agentDB), "hub.db")
			}
			hs, err := store.Open(hubDB, "hub")
			if err != nil {
				t.Fatal(err)
			}
			defer hs.Close()
			backendDB := filepath.Join(dir, "backend.db")
			bs, err := store.Open(backendDB, "hub")
			if err != nil {
				t.Fatal(err)
			}
			defer bs.Close()
			// The protocol backend installs a fixed allocation. The Hub must
			// reserve it durably before its control response becomes visible.
			issuedAt := time.Now().UTC()
			delegation := store.HubDelegation{
				ID: "550e8400-e29b-41d4-a716-446655440022", HubID: runtimeHubID,
				SourceID: "550e8400-e29b-41d4-a716-446655440023", RuntimeSourceID: "550e8400-e29b-41d4-a716-446655440061", ShareSourceID: "550e8400-e29b-41d4-a716-446655440062",
				NodeID: node.NodeID, BootID: node.BootID, BillingSnapshotID: "550e8400-e29b-41d4-a716-446655440024",
				RuleID: cfg.RuleID, RuleRevision: cfg.RuleRevision, PathGeneration: cfg.PathGeneration, FencingFloor: 8, SignerEpoch: 1,
				RawBytes: params.delegationBytes + uint64(renewalRounds)*params.targetBytes, RateBPS: 1000000, Connections: 1,
				ValidFrom: issuedAt, ExpiresAt: issuedAt.Add(time.Hour), HardDeadline: issuedAt.Add(time.Hour),
				BackendHeartbeat: issuedAt, AutonomyDeadline: issuedAt.Add(2 * time.Hour),
			}
			issuerClock := func() store.IssuerTime {
				now := time.Now().UTC()
				return store.IssuerTime{Earliest: now, Latest: now, Trusted: true}
			}
			if err := hs.InstallHubDelegation(ctx, delegation, issuerClock()); err != nil {
				t.Fatal(err)
			}
			issuer := &hub.Issuer{Store: hs, HubID: delegation.HubID, Clock: issuerClock}
			usageRelay := &hub.UsageRelay{Store: hs, Clock: issuerClock, Backend: &durableUsageBackend{store: bs}}
			const issueID = "550e8400-e29b-41d4-a716-446655440020"
			initialTTL := 4 * time.Second
			if params.candidate {
				initialTTL = 30 * time.Second
			}
			prepared, err := issuer.Issue(ctx, delegation.ID, node.NodeID, node.BootID, issueID, params.initialGrant, initialTTL)
			if err != nil {
				t.Fatal(err)
			}
			initialPayload, err := validateAuthorizationUpdate(prepared.Payload, node.NodeID, node.BootID, 4)
			if err != nil {
				t.Fatal(err)
			}
			var initialGrantID string
			var initialExpiry time.Time
			for _, record := range initialPayload.Authorizations {
				if record.Kind == "credit_grant" {
					initialGrantID = record.AuthorizationID
					initialExpiry = record.ExpiresAt
				}
			}
			ln := startTestControlHub(t, ctx, &hub.TestServer{Store: hs, AuthorizationUpdate: func(id, boot string) (*controlv1.AuthorizationUpdate, error) {
				return issuer.Issue(ctx, delegation.ID, id, boot, issueID, params.initialGrant, initialTTL)
			}}, serverTLS)
			if err := (&ControlClient{Node: node, TLS: clientTLS, EnableCreditDemand: true}).Connect(ctx, ln.Addr().String()); err != nil {
				t.Fatal(err)
			}
			waitUntil := func(at time.Time) {
				t.Helper()
				lastHeartbeat := issuedAt
				for {
					if params.candidate && time.Since(lastHeartbeat) >= 5*time.Second {
						lastHeartbeat = time.Now().UTC()
						if err := hs.RecordBackendHeartbeat(ctx, lastHeartbeat, delegation.AutonomyDeadline); err != nil {
							t.Fatal(err)
						}
					}
					low, _, trusted := node.TrustedTime()
					if trusted && !low.Before(at) {
						return
					}
					select {
					case <-ctx.Done():
						t.Fatal(ctx.Err())
					case <-time.After(time.Millisecond):
					}
				}
			}
			waitUntil(initialPayload.IssuedAt)
			localNode, err := vlink.ParseNodeID(controlTestNodeID)
			if err != nil {
				t.Fatal(err)
			}
			peerNode, err := vlink.ParseNodeID(peerID)
			if err != nil {
				t.Fatal(err)
			}
			// An installed, funded durable lease must not turn forged route
			// metadata into permission. Each rejection precedes target access
			// and leaves both source funds and connection admission untouched.
			for _, attack := range []struct {
				name   string
				code   vlink.OpenErrorCode
				mutate func(*vlink.OpenRequest)
			}{
				{"unknown-rule", vlink.OpenStaleRevision, func(r *vlink.OpenRequest) { r.RuleID[0] ^= 0xff }},
				{"stale-revision", vlink.OpenStaleRevision, func(r *vlink.OpenRequest) { r.RuleRevision-- }},
				{"future-revision", vlink.OpenStaleRevision, func(r *vlink.OpenRequest) { r.RuleRevision++ }},
				{"stale-generation", vlink.OpenStaleRevision, func(r *vlink.OpenRequest) { r.PathGeneration-- }},
				{"future-generation", vlink.OpenStaleRevision, func(r *vlink.OpenRequest) { r.PathGeneration++ }},
				{"skipped-hop", vlink.OpenNoRoute, func(r *vlink.OpenRequest) { r.HopIndex++ }},
				{"visited-local", vlink.OpenNoRoute, func(r *vlink.OpenRequest) { r.Visited = []vlink.NodeID{localNode} }},
				{"duplicate-visited", vlink.OpenProtocolError, func(r *vlink.OpenRequest) { r.Visited = []vlink.NodeID{peerNode, peerNode} }},
				{"zero-visited", vlink.OpenProtocolError, func(r *vlink.OpenRequest) { r.Visited = []vlink.NodeID{{}} }},
				{"too-many-visited", vlink.OpenProtocolError, func(r *vlink.OpenRequest) {
					r.Visited = make([]vlink.NodeID, 17)
					for i := range r.Visited {
						r.Visited[i][0] = byte(i + 1)
					}
				}},
				{"invalid-network", vlink.OpenProtocolError, func(r *vlink.OpenRequest) { r.Network = 9 }},
				{"unspecified-client-address", vlink.OpenProtocolError, func(r *vlink.OpenRequest) { r.ClientAddress = []byte{0, 0, 0, 0} }},
			} {
				t.Run("funded-attack-"+attack.name, func(t *testing.T) {
					request := vlink.OpenRequest{RuleID: rule, RuleRevision: 2, PathGeneration: 4, Network: network, ClientAddress: []byte{127, 0, 0, 1}}
					attack.mutate(&request)
					forged, err := mux.Open(ctx, request)
					if err != nil {
						if attack.name != "too-many-visited" && attack.name != "invalid-network" {
							t.Fatalf("OPEN rejected before remote policy: %v", err)
						}
						balance, auditErr := s.AuditAuthorizationSource(ctx, node.NodeID, delegation.SourceID)
						if auditErr != nil || balance.Initial != params.initialGrant || balance.Remaining != params.initialGrant || balance.Reserved != 0 || balance.Committed != 0 || balance.Unknown != 0 || targets.Load() != 0 {
							t.Fatalf("local protocol rejection changed authority or target: %+v targets=%d err=%v", balance, targets.Load(), auditErr)
						}
						return
					}
					if attack.name == "too-many-visited" || attack.name == "invalid-network" {
						t.Fatal("malformed OPEN passed local protocol validation")
					}
					frame, err := forged.Receive(ctx)
					if err != nil {
						t.Fatal(err)
					}
					defer frame.Release()
					if frame.Type != vlink.FrameOpenErr || len(frame.Payload) != 18 || vlink.OpenErrorCode(binary.BigEndian.Uint16(frame.Payload[:2])) != attack.code || bytes.Equal(frame.Payload[2:], make([]byte, 16)) {
						t.Fatalf("wrong audited rejection: type=%d payload=%x", frame.Type, frame.Payload)
					}
					balance, err := s.AuditAuthorizationSource(ctx, node.NodeID, delegation.SourceID)
					if err != nil || balance.Initial != params.initialGrant || balance.Remaining != params.initialGrant || balance.Reserved != 0 || balance.Committed != 0 || balance.Unknown != 0 || targets.Load() != 0 {
						t.Fatalf("forged route changed authority or target: %+v targets=%d err=%v", balance, targets.Load(), err)
					}
				})
			}
			stream := open()
			if err := stream.WaitOpen(ctx); err != nil {
				t.Fatal(err)
			}
			second := open()
			if err := second.WaitOpen(ctx); err == nil {
				t.Fatal("persisted connection share limit bypassed")
			}
			// This exchange commits a bounded amount across both directions of
			// the durable grant, crossing the configured renewal waterline.
			message := bytes.Repeat([]byte{0x5a}, int(params.messageBytes))
			exchangeCount := params.exchangeCount
			if exchangeCount == 0 {
				exchangeCount = 1
			}
			trafficBytes := uint64(2 * len(message) * exchangeCount)
			exchange := func() {
				t.Helper()
				if network == 1 {
					err = stream.WriteData(ctx, message)
				} else {
					var accepted bool
					accepted, err = stream.WriteDgram(ctx, message)
					if !accepted {
						t.Fatalf("datagram not accepted: %v", err)
					}
				}
				if err != nil {
					t.Fatal(err)
				}
				if network == 1 {
					received := make([]byte, 0, len(message))
					for len(received) < len(message) {
						frame, receiveErr := stream.Receive(ctx)
						if receiveErr != nil {
							t.Fatal(receiveErr)
						}
						if frame.Type != vlink.FrameData {
							frame.Release()
							t.Fatalf("unexpected response frame type=%d", frame.Type)
						}
						received = append(received, frame.Payload...)
						frame.Release()
					}
					if !bytes.Equal(received, message) {
						t.Fatalf("response bytes=%d want=%d", len(received), len(message))
					}
					return
				}
				frame, err := stream.Receive(ctx)
				if err != nil {
					t.Fatal(err)
				}
				defer frame.Release()
				if !bytes.Equal(frame.Payload, message) {
					t.Fatalf("response=%q", frame.Payload)
				}
			}
			waitSettled := func(initial, committed uint64) {
				t.Helper()
				for {
					b, err := s.AuditAuthorizationSource(ctx, node.NodeID, delegation.SourceID)
					if err != nil {
						t.Fatal(err)
					}
					// A proactively delivered next grant may arrive before an idle
					// tail returns. Final totals are checked against Hub below.
					issuedMatches := b.Initial == initial || (blockMode && b.Initial > initial && b.Initial <= delegation.RawBytes)
					if b.Committed == committed && issuedMatches && (!blockMode || b.Reserved == 0) {
						return
					}
					select {
					case <-ctx.Done():
						t.Fatalf("metering not settled: %+v", b)
					case <-time.After(time.Millisecond):
					}
				}
			}
			// Open and consume on the first grant before delivering its renewal.
			for i := 0; i < exchangeCount; i++ {
				exchange()
			}
			waitSettled(params.initialGrant, trafficBytes)
			initialUsage, err := s.PendingUsageOutbox(ctx, node.NodeID)
			if err != nil || len(initialUsage) != 2*exchangeCount {
				t.Fatalf("initial usage outbox: %d %v", len(initialUsage), err)
			}
			// Actual committed TCP/UDP traffic has crossed the waterline. The
			// Node now derives the deficit from its durable scoped ledger, sends
			// migrated field-100 CreditDemand, and installs only the Hub's committed
			// response. No pre-created renewal is supplied by the test.
			demandScope := store.DemandScope{HubID: delegation.HubID, DelegationID: delegation.ID, NodeID: node.NodeID, BootID: node.BootID, SourceID: delegation.SourceID, SnapshotID: delegation.BillingSnapshotID, RuleID: delegation.RuleID, SignerEpoch: delegation.SignerEpoch, RuleRevision: delegation.RuleRevision, PathGeneration: delegation.PathGeneration}
			adaptive := AdaptiveDemandConfig{Scope: demandScope, Policy: DemandPolicyConfig{Minimum: params.targetBytes, MaxTarget: params.targetBytes, TargetSeconds: params.targetSeconds, RenewPercent: params.waterlinePercent}, TTL: 2 * time.Minute}
			// Lose the first response after the Hub committed its debit. The
			// Agent must retain its exact request and must not spend unseen credit.
			lostHub := startTestControlHub(t, ctx, &hub.TestServer{Store: hs, Usage: usageRelay, AcceptUsage: true, DemandIssuer: issuer, DropDemandResponse: true}, serverTLS)
			if err := (&ControlClient{Node: node, TLS: clientTLS, EnableCreditDemand: true, AdaptiveDemands: []AdaptiveDemandConfig{adaptive}}).Connect(ctx, lostHub.Addr().String()); err == nil {
				t.Fatal("injected lost response did not interrupt control connection")
			}
			requests, err := s.PendingDemands(ctx, node.NodeID, node.BootID, runtimeHubID, "")
			if err != nil || len(requests) != 1 || requests[0].RawBytes != params.renewalBytes {
				t.Fatalf("lost response retired or changed pending demand: %+v %v", requests, err)
			}
			retained, err := s.PrepareDemand(ctx, demandScope, params.targetBytes*2, time.Minute)
			if err != nil || !reflect.DeepEqual(retained, requests[0]) {
				t.Fatalf("changed estimate replaced in-flight identity: %+v %v", retained, err)
			}
			beforeReplay, err := hs.AuditHubDelegation(ctx, delegation.ID)
			expectedAfterReplay := params.initialGrant + params.renewalBytes
			postReplayAvailable := params.initialGrant - trafficBytes + params.renewalBytes
			if err != nil || beforeReplay.Issued != expectedAfterReplay || beforeReplay.Remaining != delegation.RawBytes-expectedAfterReplay {
				t.Fatalf("lost response was not durably funded: %+v %v", beforeReplay, err)
			}
			unseen, err := s.AuditAuthorizationSource(ctx, node.NodeID, delegation.SourceID)
			if err != nil || unseen.Initial != params.initialGrant || unseen.Remaining != params.initialGrant-trafficBytes || unseen.Committed != trafficBytes || unseen.Reserved != 0 || unseen.Unknown != 0 {
				t.Fatalf("unseen grant became spendable: %+v %v", unseen, err)
			}
			topupHub := startTestControlHub(t, ctx, &hub.TestServer{Store: hs, Usage: usageRelay, AcceptUsage: true, DemandIssuer: issuer, DemandLimit: 1}, serverTLS)
			if err := (&ControlClient{Node: node, TLS: clientTLS, EnableCreditDemand: true, AdaptiveDemands: []AdaptiveDemandConfig{adaptive}}).Connect(ctx, topupHub.Addr().String()); err != nil {
				t.Fatal(err)
			}
			requests, err = s.PendingDemands(ctx, node.NodeID, node.BootID, runtimeHubID, "")
			if err != nil || len(requests) != 0 {
				t.Fatalf("replayed response did not complete pending demand: %+v %v", requests, err)
			}
			for {
				balance, err := s.AuditAuthorizationSource(ctx, node.NodeID, delegation.SourceID)
				if err != nil {
					t.Fatal(err)
				}
				if balance.Initial == expectedAfterReplay && balance.Remaining == postReplayAvailable && balance.Committed == trafficBytes {
					break
				}
				select {
				case <-ctx.Done():
					t.Fatalf("durable adaptive grant was not installed: %+v", balance)
				case <-time.After(time.Millisecond):
				}
			}
			for {
				low, high, trusted := node.TrustedTime()
				ledger, err := s.ReadDemandLedger(ctx, demandScope, store.IssuerTime{Earliest: low, Latest: high, Trusted: trusted}, time.Now())
				if err != nil {
					t.Fatal(err)
				}
				if ledger.Available == postReplayAvailable {
					if ledger.MinuteBytes != trafficBytes {
						t.Fatalf("wrong durable consumption window: %+v", ledger)
					}
					break
				}
				select {
				case <-ctx.Done():
					t.Fatalf("new grant not yet active within conservative clock: %+v", ledger)
				case <-time.After(time.Millisecond):
				}
			}
			waitUntil(initialExpiry)
			if params.candidate {
				// Candidate traffic models an online backend. The longer initial
				// lease must not accidentally turn this into an autonomy fixture.
				if err := hs.RecordBackendHeartbeat(ctx, time.Now().UTC(), delegation.AutonomyDeadline); err != nil {
					t.Fatal(err)
				}
			}
			for i := 0; i < exchangeCount; i++ {
				exchange()
			}
			waitSettled(expectedAfterReplay, 2*trafficBytes)
			pending, err := s.PendingUsageOutbox(ctx, node.NodeID)
			if err != nil || len(pending) != 2*exchangeCount {
				t.Fatalf("renewal usage outbox: %d %v", len(pending), err)
			}
			counts := map[string]int{}
			for _, row := range append(initialUsage, pending...) {
				var usage controlv1.UsageBatch
				if err := proto.Unmarshal(row.Payload, &usage); err != nil {
					t.Fatal(err)
				}
				if len(usage.Deltas) != 1 {
					t.Fatalf("invalid metered batch: %+v", usage.Deltas)
				}
				counts[usage.Deltas[0].GrantId]++
			}
			var topupGrantID string
			for id := range counts {
				if id != initialGrantID {
					topupGrantID = id
				}
			}
			if counts[initialGrantID] != 2*exchangeCount || topupGrantID == "" || counts[topupGrantID] != 2*exchangeCount || len(counts) != 2 {
				t.Fatalf("old and adaptively issued usage lost their grant identities: %+v", counts)
			}
			t.Logf("same stream renewed after initial expiry %s; two directional batches per grant", initialExpiry.Format(time.RFC3339Nano))
			expectedIssued := expectedAfterReplay
			if continuous {
				defer func() {
					if !t.Failed() {
						return
					}
					auditCtx, stop := context.WithTimeout(context.Background(), time.Second)
					defer stop()
					agentBalance, ae := s.AuditAuthorizationSource(auditCtx, node.NodeID, delegation.SourceID)
					hubBalance, he := hs.AuditHubDelegation(auditCtx, delegation.ID)
					requests, re := s.PendingDemands(auditCtx, node.NodeID, node.BootID, runtimeHubID, "")
					low, high, trusted := node.TrustedTime()
					ledger, le := s.ReadDemandLedger(auditCtx, demandScope, store.IssuerTime{Earliest: low, Latest: high, Trusted: trusted}, time.Now())
					t.Logf("continuous failure agent=%+v err=%v hub=%+v err=%v pending=%+v err=%v ledger=%+v err=%v", agentBalance, ae, hubBalance, he, requests, re, ledger, le)
				}()
				// Keep sending on the existing business stream. Do not wait for
				// grants or idle-tail settlement between exchanges: real admission
				// waits and the live adaptive controller must bridge renewals.
				controlCtx, stopControl := context.WithCancel(ctx)
				defer stopControl()
				continuousHub := startTestControlHub(t, controlCtx, &hub.TestServer{Store: hs, Usage: usageRelay, AcceptUsage: true, DemandIssuer: issuer}, serverTLS)
				controlDone := make(chan error, 1)
				go func() {
					controlDone <- (&ControlClient{Node: node, TLS: clientTLS, EnableCreditDemand: true, AdaptiveDemands: []AdaptiveDemandConfig{adaptive}}).Connect(controlCtx, continuousHub.Addr().String())
				}()
				// Wait only for the authenticated control consumer to register;
				// never wait for its grant balance or a round's idle settlement.
				for {
					node.demandMu.Lock()
					ready := false
					for sub := range node.demandSubscriptions {
						for _, configured := range sub.scopes {
							if configured.Scope == adaptive.Scope {
								ready = true
							}
						}
					}
					node.demandMu.Unlock()
					if ready {
						break
					}
					select {
					case err := <-controlDone:
						t.Fatalf("control stopped before readiness: %v", err)
					case <-ctx.Done():
						t.Fatal(ctx.Err())
					case <-time.After(time.Millisecond):
					}
				}
				started := time.Now()
				for round := 0; round < renewalRounds; round++ {
					if err := hs.RecordBackendHeartbeat(ctx, time.Now().UTC(), delegation.AutonomyDeadline); err != nil {
						t.Fatal(err)
					}
					exchange()
				}
				waitSettled(expectedIssued, uint64(renewalRounds+2)*trafficBytes)
				stopControl()
				select {
				case err := <-controlDone:
					if err != nil && !errors.Is(err, context.Canceled) {
						t.Logf("control stop: %v", err)
					}
				case <-ctx.Done():
					t.Fatal(ctx.Err())
				}
				// A response in flight at shutdown must be replayed before final
				// reconciliation; the same durable identity prevents a new debit.
				requests, err := s.PendingDemands(ctx, node.NodeID, node.BootID, runtimeHubID, "")
				if err != nil {
					t.Fatal(err)
				}
				if len(requests) > 0 {
					replayHub := startTestControlHub(t, ctx, &hub.TestServer{Store: hs, Usage: usageRelay, AcceptUsage: true, DemandIssuer: issuer, DemandLimit: len(requests)}, serverTLS)
					if err := (&ControlClient{Node: node, TLS: clientTLS, EnableCreditDemand: true}).Connect(ctx, replayHub.Addr().String()); err != nil {
						t.Fatal(err)
					}
				}
				funded, err := hs.AuditHubDelegation(ctx, delegation.ID)
				if err != nil {
					t.Fatal(err)
				}
				expectedIssued = funded.Issued
				t.Logf("unpaced durable traffic exchanges=%d elapsed=%s issued=%d", renewalRounds, time.Since(started), expectedIssued)
			} else if renewalRounds > 0 {
				// Keep one real control connection and the original business stream
				// alive across repeated consumption-driven durable renewals.
				issued := make(chan struct{}, 1)
				release := make(chan struct{})
				var barrier func(context.Context) error
				if len(delayResponse) > 0 && delayResponse[0] {
					barrier = func(ctx context.Context) error {
						select {
						case issued <- struct{}{}:
						case <-ctx.Done():
							return ctx.Err()
						}
						select {
						case <-release:
							return nil
						case <-ctx.Done():
							return ctx.Err()
						}
					}
				}
				continuousHub := startTestControlHub(t, ctx, &hub.TestServer{Store: hs, Usage: usageRelay, AcceptUsage: true, DemandIssuer: issuer, DemandLimit: renewalRounds, BeforeDemandResponse: barrier}, serverTLS)
				controlDone := make(chan error, 1)
				go func() {
					controlDone <- (&ControlClient{Node: node, TLS: clientTLS, EnableCreditDemand: true, AdaptiveDemands: []AdaptiveDemandConfig{adaptive}}).Connect(ctx, continuousHub.Addr().String())
				}()
				for round := 0; round < renewalRounds; round++ {
					// This fixture models an online backend. Preserve the
					// original absolute deadline while refreshing liveness.
					if err := hs.RecordBackendHeartbeat(ctx, time.Now().UTC(), delegation.AutonomyDeadline); err != nil {
						t.Fatal(err)
					}

					// The first old grant has expired with 1096 unused bytes.
					// Those bytes must not count toward the next usable target.
					previousIssued := expectedIssued
					if round == 0 {
						expectedIssued += 4096
					} else {
						expectedIssued += 3000
					}
					if blockMode {
						for {
							funded, err := hs.AuditHubDelegation(ctx, delegation.ID)
							if err != nil {
								t.Fatal(err)
							}
							if funded.Issued > previousIssued {
								delta := funded.Issued - previousIssued
								if delta < 3000 || delta > 4096 || (round == 0 && delta != 4096) {
									t.Fatalf("unexpected block renewal size: %d", delta)
								}
								expectedIssued = funded.Issued
								break
							}
							select {
							case <-ctx.Done():
								t.Fatal(ctx.Err())
							case <-time.After(time.Millisecond):
							}
						}
					}
					if barrier != nil {
						select {
						case <-issued:
						case <-ctx.Done():
							t.Fatal(ctx.Err())
						}
						hubBalance, err := hs.AuditHubDelegation(ctx, delegation.ID)
						if err != nil || hubBalance.Issued != expectedIssued {
							t.Fatalf("delayed issue not funded: %+v %v", hubBalance, err)
						}
						agentBalance, err := s.AuditAuthorizationSource(ctx, node.NodeID, delegation.SourceID)
						if err != nil || agentBalance.Initial != previousIssued || agentBalance.Committed != uint64((round+2)*2*len(message)) || agentBalance.Reserved != 0 || agentBalance.Unknown != 0 {
							t.Fatalf("undelivered grant changed Agent: %+v %v", agentBalance, err)
						}
						requests, err := s.PendingDemands(ctx, node.NodeID, node.BootID, runtimeHubID, "")
						if err != nil || len(requests) != 1 || requests[0].RawBytes != expectedIssued-previousIssued {
							t.Fatalf("delayed request identity: %+v %v", requests, err)
						}
						retained, err := s.PrepareDemand(ctx, demandScope, 8192, time.Minute)
						if err != nil || !reflect.DeepEqual(retained, requests[0]) {
							t.Fatalf("delay permitted replacement: %+v %v", retained, err)
						}
						if targetBytes.Load() != int64((round+2)*len(message)) {
							t.Fatal("target bytes changed while grant withheld")
						}
						select {
						case release <- struct{}{}:
						case <-ctx.Done():
							t.Fatal(ctx.Err())
						}
					}
					for {
						low, high, trusted := node.TrustedTime()
						ledger, err := s.ReadDemandLedger(ctx, demandScope, store.IssuerTime{Earliest: low, Latest: high, Trusted: trusted}, time.Now())
						if err != nil {
							t.Fatal(err)
						}
						wantAvailable := uint64(4096)
						if blockMode {
							wantAvailable = expectedIssued - uint64((round+2)*2*len(message)) - 1096
						}
						if ledger.Available == wantAvailable {
							break
						}
						select {
						case <-ctx.Done():
							t.Fatalf("round %d renewal unavailable: %+v", round, ledger)
						case <-time.After(time.Millisecond):
						}
					}
					t.Logf("continuous renewal round=%d expected_issued=%d", round+1, expectedIssued)
					exchange()
					waitSettled(expectedIssued, uint64((round+3)*2*len(message)))
					balance, err := s.AuditAuthorizationSource(ctx, node.NodeID, delegation.SourceID)
					if err != nil || balance.Reserved != 0 || balance.Unknown != 0 || balance.Initial != balance.Remaining+balance.Committed {
						t.Fatalf("round %d conservation: %+v %v", round, balance, err)
					}
				}
				select {
				case err := <-controlDone:
					if err != nil {
						t.Fatal(err)
					}
				case <-ctx.Done():
					t.Fatal(ctx.Err())
				}
				if requests, err := s.PendingDemands(ctx, node.NodeID, node.BootID, runtimeHubID, ""); err != nil || len(requests) != 0 {
					t.Fatalf("continuous demand left pending requests: %+v %v", requests, err)
				}
				t.Logf("continuous durable renewals=%d issued=%d metered=%d", renewalRounds, expectedIssued, uint64(renewalRounds+2)*trafficBytes)
			}
			if fullDiskRoot != "" {
				filler := fillRuntimeDisk(t, filepath.Dir(agentDB))
				probeErr := s.AppendEvent(ctx, "full-disk-probe", "probe", []byte("must fail durably"))
				var sqliteErr interface{ Code() int }
				if !errors.As(probeErr, &sqliteErr) || sqliteErr.Code()&255 != 13 {
					t.Fatalf("expected SQLITE_FULL, got %v", probeErr)
				}
				if result, err := issuer.Issue(ctx, delegation.ID, node.NodeID, node.BootID, "550e8400-e29b-41d4-a716-446655440088", 64, time.Minute); err == nil || result != nil {
					t.Fatalf("full disk returned new Hub authority: %v", err)
				}
				if network == 1 {
					err = stream.WriteData(ctx, []byte("must-not-reach-target"))
				} else {
					_, err = stream.WriteDgram(ctx, []byte("must-not-reach-target"))
				}
				if err != nil {
					t.Fatal(err)
				}
				frame, readErr := stream.Receive(ctx)
				if ctx.Err() != nil {
					t.Fatal(ctx.Err())
				}
				if readErr == nil && frame.Type != vlink.FrameRST {
					frame.Release()
					t.Fatal("full disk did not reset runtime stream")
				}
				frame.Release()
				if targetBytes.Load() != int64(uint64(renewalRounds+2)*trafficBytes/2) {
					t.Fatalf("unrecordable traffic reached target: %d", targetBytes.Load())
				}
				if err := os.Remove(filler); err != nil {
					t.Fatal(err)
				}
				blocked := open()
				if err := blocked.WaitOpen(ctx); err == nil {
					t.Fatal("disk recovery silently reopened faulted gate")
				}
			}
			_ = stream.Reset(ctx, 0)
			if err := node.Stop(ctx); err != nil {
				t.Fatal(err)
			}
			// Control demand completion does not imply asynchronous usage ACKs
			// have drained. Freeze traffic first, then replay every pending batch.
			toDrain, err := s.PendingUsageOutbox(ctx, node.NodeID)
			if err != nil {
				t.Fatal(err)
			}
			// The live bidirectional consumer may have already durably ACKed all
			// traffic. Replay only actual pending rows, then reconcile all three raw
			// ledgers below; receipt timing is not a financial invariant.
			if len(toDrain) > 0 {
				usageHub := startTestControlHub(t, ctx, &hub.TestServer{Store: hs, Usage: usageRelay, AcceptUsage: true, UsageBatchLimit: len(toDrain)}, serverTLS)
				if err := (&ControlClient{Node: node, TLS: clientTLS}).Connect(ctx, usageHub.Addr().String()); err != nil {
					t.Fatal(err)
				}
			}
			if pending, err := s.PendingUsageOutbox(ctx, node.NodeID); err != nil || len(pending) != 0 {
				t.Fatalf("outbox not acknowledged: %d %v", len(pending), err)
			}
			balance, err := hs.AuditHubDelegation(ctx, delegation.ID)
			if err != nil || balance.Initial != delegation.RawBytes || balance.Remaining != delegation.RawBytes-expectedIssued || balance.Issued != expectedIssued {
				t.Fatalf("Hub delegated reserve: %+v %v", balance, err)
			}
			agentFinal, err := s.AuditAuthorizationSource(ctx, node.NodeID, delegation.SourceID)
			if err != nil || agentFinal.Initial != expectedIssued || agentFinal.Committed != uint64(renewalRounds+2)*trafficBytes || agentFinal.Reserved != 0 || agentFinal.Unknown != 0 || agentFinal.Initial != agentFinal.Remaining+agentFinal.Committed {
				t.Fatalf("final Agent/Hub funding reconciliation: %+v expected=%d err=%v", agentFinal, expectedIssued, err)
			}
			if err := s.Close(); err != nil {
				t.Fatal(err)
			}
			if err := hs.Close(); err != nil {
				t.Fatal(err)
			}
			if err := bs.Close(); err != nil {
				t.Fatal(err)
			}
			if continuous {
				assertFragmentLedgers(t, []string{agentDB, hubDB, backendDB}, uint64(renewalRounds+2)*trafficBytes)
			} else {
				assertRawLedger(t, hubDB, 4*exchangeCount+2*renewalRounds)
				assertRawLedger(t, backendDB, 4*exchangeCount+2*renewalRounds)
			}
			if params.candidate {
				writeDurableD04CandidateResult(t, network, params, delegation.RawBytes, expectedIssued, uint64(renewalRounds+2)*trafficBytes, agentFinal, balance)
			}
			if continuous {
				saveLedgerFiles(t, fmt.Sprintf("runtime-block-continuous-%d-rounds-%d", network, renewalRounds), map[string]string{"agent": agentDB, "hub": hubDB, "backend": backendDB})
			} else if blockMode {
				saveLedgerFiles(t, fmt.Sprintf("runtime-block-%d-rounds-%d-delay-%t", network, renewalRounds, len(delayResponse) > 0 && delayResponse[0]), map[string]string{"agent": agentDB, "hub": hubDB, "backend": backendDB})
			} else if renewalRounds == 0 {
				saveRuntimeLedger(t, network, agentDB, hubDB)
				saveLedgerFiles(t, fmt.Sprintf("runtime-handoff-%d", network), map[string]string{"agent": agentDB, "hub": hubDB, "backend": backendDB})
			} else {
				saveLedgerFiles(t, fmt.Sprintf("runtime-continuous-%d-rounds-%d-delay-%t", network, renewalRounds, len(delayResponse) > 0 && delayResponse[0]), map[string]string{"agent": agentDB, "hub": hubDB, "backend": backendDB})
			}
		})
	}
}

func writeDurableD04CandidateResult(t *testing.T, network uint8, params durableD04Params, delegationBytes, issued, committed uint64, agent store.AuthorizationBalance, hub store.DelegationBalance) {
	t.Helper()
	root := os.Getenv("VORTEX_D04_RESULT_DIR")
	if root == "" {
		return
	}
	if err := os.MkdirAll(root, 0755); err != nil {
		t.Fatal(err)
	}
	result := map[string]any{
		"schema":                        1,
		"network":                       map[uint8]string{1: "tcp", 2: "udp"}[network],
		"block_bytes":                   params.blockBytes,
		"target_bytes":                  params.targetBytes,
		"target_seconds":                params.targetSeconds,
		"renew_percent":                 params.waterlinePercent,
		"initial_grant_bytes":           params.initialGrant,
		"renewal_bytes":                 params.renewalBytes,
		"message_bytes":                 params.messageBytes,
		"delegation_bytes":              delegationBytes,
		"issued_bytes":                  issued,
		"committed_bytes":               committed,
		"source_conserved":              agent.Initial == agent.Remaining+agent.Committed+agent.Unknown+agent.Reserved,
		"source_settled":                agent.Unknown == 0 && agent.Reserved == 0,
		"pending_unknown_held_zero":     agent.Unknown == 0 && agent.Reserved == 0,
		"issuer_conserved":              hub.Initial == hub.Remaining+hub.Issued,
		"issuer_balance_residual_bytes": hub.Initial - hub.Remaining - hub.Issued,
		"issuer_outstanding_bytes":      hub.Issued,
		"qualification_scope":           "finite fixed-target durable renewal; excludes dynamic-target performance qualification",
		"idle_tail_reclaimed":           agent.Reserved == 0,
		"pending_demand_settled":        true,
		"pending_usage_settled":         true,
		"agent_initial":                 agent.Initial,
		"agent_remaining":               agent.Remaining,
		"agent_committed":               agent.Committed,
		"agent_unknown":                 agent.Unknown,
		"agent_reserved":                agent.Reserved,
		"hub_initial":                   hub.Initial,
		"hub_remaining":                 hub.Remaining,
		"hub_issued":                    hub.Issued,
	}
	raw, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, fmt.Sprintf("network-%d.json", network))
	if err := os.WriteFile(path, append(raw, '\n'), 0644); err != nil {
		t.Fatal(err)
	}
}

func assertRawLedger(t *testing.T, path string, want int) {
	t.Helper()
	db, err := sql.Open("sqlite", "file:"+path+"?mode=ro")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var integrity string
	if err := db.QueryRow("PRAGMA quick_check").Scan(&integrity); err != nil || integrity != "ok" {
		t.Fatalf("ledger integrity: %s %v", integrity, err)
	}
	var count int
	if err := db.QueryRow("SELECT COUNT(*) FROM usage_batches WHERE state='ledgered'").Scan(&count); err != nil || count != want {
		t.Fatalf("Hub ledger=%d want=%d err=%v", count, want, err)
	}
}

func saveRuntimeLedger(t *testing.T, network uint8, agentDB, hubDB string) {
	saveNamedLedger(t, fmt.Sprintf("runtime-%d", network), agentDB, hubDB)
}

func saveNamedLedger(t *testing.T, label, agentDB, hubDB string) {
	saveLedgerFiles(t, label, map[string]string{"agent": agentDB, "hub": hubDB})
}

func saveLedgerFiles(t *testing.T, label string, files map[string]string) {
	t.Helper()
	out := os.Getenv("PHASE5_AUTHORIZATION_EVIDENCE_DIR")
	if out == "" {
		return
	}
	if err := os.MkdirAll(out, 0755); err != nil {
		t.Fatal(err)
	}
	checksums := map[string]string{}
	for name, path := range files {
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		digest := sha256.Sum256(raw)
		checksums[name] = fmt.Sprintf("%x", digest)
		f, err := os.Create(filepath.Join(out, fmt.Sprintf("%s-%s.sqlite.gz", label, name)))
		if err != nil {
			t.Fatal(err)
		}
		z := gzip.NewWriter(f)
		if _, err := z.Write(raw); err != nil {
			t.Fatal(err)
		}
		if err := z.Close(); err != nil {
			t.Fatal(err)
		}
		if err := f.Close(); err != nil {
			t.Fatal(err)
		}
	}
	encoded, err := json.Marshal(checksums)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(out, label+"-ledgers.json"), encoded, 0644); err != nil {
		t.Fatal(err)
	}
}

// fillRuntimeDisk is enabled only for an explicitly provided small isolated
// filesystem. Never fill an ordinary host filesystem during a default test.
func fillRuntimeDisk(t *testing.T, dir string) string {
	t.Helper()
	path := filepath.Join(dir, "filler")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Remove(path) })
	block := make([]byte, 64*1024)
	for written := 0; written < 16*1024*1024; written += len(block) {
		_, err = f.Write(block)
		if err != nil {
			f.Close()
			if !errors.Is(err, syscall.ENOSPC) {
				t.Fatalf("unexpected disk failure: %v", err)
			}
			return path
		}
	}
	f.Close()
	t.Fatal("full disk fixture must be an isolated filesystem of at most 16 MiB")
	return ""
}

// Fragment count is workload-dependent. Compare every durable identity/payload
// across all three ledgers and independently sum actual protobuf byte deltas.
func assertFragmentLedgers(t *testing.T, paths []string, wantBytes uint64) {
	t.Helper()
	var reference map[string][]byte
	for index, path := range paths {
		db, err := sql.Open("sqlite", "file:"+path+"?mode=ro")
		if err != nil {
			t.Fatal(err)
		}
		defer db.Close()
		var integrity string
		if err := db.QueryRow("PRAGMA quick_check").Scan(&integrity); err != nil || integrity != "ok" {
			t.Fatalf("ledger integrity %s %v", integrity, err)
		}
		rows, err := db.Query("SELECT batch_id,payload,state FROM usage_batches")
		if err != nil {
			t.Fatal(err)
		}
		entries := map[string][]byte{}
		var total uint64
		for rows.Next() {
			var id, state string
			var payload []byte
			if err := rows.Scan(&id, &payload, &state); err != nil {
				t.Fatal(err)
			}
			expected := "ledgered"
			if index == 0 {
				expected = "hub_acked"
			}
			if state != expected {
				t.Fatalf("unsettled batch %s state=%s", id, state)
			}
			var batch controlv1.UsageBatch
			if err := proto.Unmarshal(payload, &batch); err != nil {
				t.Fatal(err)
			}
			if batch.BatchId != id || len(batch.Deltas) != 1 || batch.Deltas[0].GrantId == "" {
				t.Fatalf("invalid usage %s", id)
			}
			total += batch.Deltas[0].Bytes
			entries[id] = payload
		}
		if err := rows.Err(); err != nil {
			t.Fatal(err)
		}
		rows.Close()
		if total != wantBytes {
			t.Fatalf("ledger bytes %d want %d", total, wantBytes)
		}
		if index == 0 {
			reference = entries
			continue
		}
		if len(entries) != len(reference) {
			t.Fatal("ledger identity count mismatch")
		}
		for id, payload := range reference {
			if !bytes.Equal(payload, entries[id]) {
				t.Fatalf("ledger payload mismatch %s", id)
			}
		}
	}
}
