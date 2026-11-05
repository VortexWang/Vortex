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
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

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
	for _, network := range []uint8{1, 2} {
		t.Run(fmt.Sprint(network), func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			const peerID = "550e8400-e29b-41d4-a716-446655440099"
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
			if err := node.Start(ctx); err != nil {
				t.Fatal(err)
			}
			defer node.Stop(context.Background())
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
			serverTLS, clientTLS, _ := controlTLS(t, controlTestNodeID)
			hubDB := filepath.Join(dir, "hub.db")
			if fullDiskRoot != "" {
				hubDB = filepath.Join(filepath.Dir(agentDB), "hub.db")
			}
			hs, err := store.Open(hubDB, "hub")
			if err != nil {
				t.Fatal(err)
			}
			defer hs.Close()
			// The protocol backend installs a fixed allocation. The Hub must
			// reserve it durably before its control response becomes visible.
			issuedAt := time.Now().UTC()
			delegation := store.HubDelegation{
				ID: "550e8400-e29b-41d4-a716-446655440022", HubID: "550e8400-e29b-41d4-a716-446655440060",
				SourceID: "550e8400-e29b-41d4-a716-446655440023", RuntimeSourceID: "550e8400-e29b-41d4-a716-446655440061", ShareSourceID: "550e8400-e29b-41d4-a716-446655440062",
				NodeID: node.NodeID, BootID: node.BootID, BillingSnapshotID: "550e8400-e29b-41d4-a716-446655440024",
				RuleID: cfg.RuleID, RuleRevision: cfg.RuleRevision, PathGeneration: cfg.PathGeneration, FencingFloor: 8, SignerEpoch: 1,
				RawBytes: 8192, RateBPS: 1000000, Connections: 1,
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
			const issueID = "550e8400-e29b-41d4-a716-446655440020"
			prepared, err := issuer.Issue(ctx, delegation.ID, node.NodeID, node.BootID, issueID, 4096, 2*time.Minute)
			if err != nil {
				t.Fatal(err)
			}
			preparedPayload, err := validateAuthorizationUpdate(prepared.Payload, node.NodeID, node.BootID, 4)
			if err != nil {
				t.Fatal(err)
			}
			ln := startTestControlHub(t, ctx, &hub.TestServer{Store: hs, AuthorizationUpdate: func(id, boot string) (*controlv1.AuthorizationUpdate, error) {
				return issuer.Issue(ctx, delegation.ID, id, boot, issueID, 4096, 2*time.Minute)
			}}, serverTLS)
			if err := (&ControlClient{Node: node, TLS: clientTLS}).Connect(ctx, ln.Addr().String()); err != nil {
				t.Fatal(err)
			}
			// Wait until the conservative lower clock bound reaches not_before.
			for {
				low, _, trusted := node.TrustedTime()
				if trusted && !low.Before(preparedPayload.IssuedAt) {
					break
				}
				select {
				case <-ctx.Done():
					t.Fatal(ctx.Err())
				case <-time.After(time.Millisecond):
				}
			}
			stream := open()
			if err := stream.WaitOpen(ctx); err != nil {
				t.Fatal(err)
			}
			second := open()
			if err := second.WaitOpen(ctx); err == nil {
				t.Fatal("persisted connection share limit bypassed")
			}
			message := []byte("metered runtime payload")
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
			frame, err := stream.Receive(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(frame.Payload, message) {
				t.Fatalf("response=%q", frame.Payload)
			}
			frame.Release()
			for {
				b, err := s.AuditAuthorizationSource(ctx, node.NodeID, "550e8400-e29b-41d4-a716-446655440023")
				if err != nil {
					t.Fatal(err)
				}
				if b.Committed == uint64(2*len(message)) {
					break
				}
				select {
				case <-ctx.Done():
					t.Fatalf("metering not settled: %+v", b)
				case <-time.After(time.Millisecond):
				}
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
				if targetBytes.Load() != int64(len(message)) {
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
			usageHub := startTestControlHub(t, ctx, &hub.TestServer{Store: hs, AcceptUsage: true, UsageBatchLimit: 2}, serverTLS)
			if err := (&ControlClient{Node: node, TLS: clientTLS}).Connect(ctx, usageHub.Addr().String()); err != nil {
				t.Fatal(err)
			}
			if pending, err := s.PendingUsageOutbox(ctx, node.NodeID); err != nil || len(pending) != 0 {
				t.Fatalf("outbox not acknowledged: %d %v", len(pending), err)
			}
			balance, err := hs.AuditHubDelegation(ctx, delegation.ID)
			if err != nil || balance.Initial != 8192 || balance.Remaining != 4096 || balance.Issued != 4096 {
				t.Fatalf("Hub delegated reserve: %+v %v", balance, err)
			}
			if err := s.Close(); err != nil {
				t.Fatal(err)
			}
			if err := hs.Close(); err != nil {
				t.Fatal(err)
			}
			assertRawLedger(t, hubDB, 2)
			saveRuntimeLedger(t, network, agentDB, hubDB)
		})
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
	t.Helper()
	out := os.Getenv("PHASE5_AUTHORIZATION_EVIDENCE_DIR")
	if out == "" {
		return
	}
	if err := os.MkdirAll(out, 0755); err != nil {
		t.Fatal(err)
	}
	checksums := map[string]string{}
	for name, path := range map[string]string{"agent": agentDB, "hub": hubDB} {
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		digest := sha256.Sum256(raw)
		checksums[name] = fmt.Sprintf("%x", digest)
		f, err := os.Create(filepath.Join(out, fmt.Sprintf("runtime-%d-%s.sqlite.gz", network, name)))
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
	if err := os.WriteFile(filepath.Join(out, fmt.Sprintf("runtime-%d-ledgers.json", network)), encoded, 0644); err != nil {
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
