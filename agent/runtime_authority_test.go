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
	"fmt"
	"math/big"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"sync/atomic"
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
			s, err := store.Open(filepath.Join(dir, "agent.db"), "agent")
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
			hs, err := store.Open(filepath.Join(dir, "hub.db"), "hub")
			if err != nil {
				t.Fatal(err)
			}
			defer hs.Close()
			ln := startTestControlHub(t, ctx, &hub.TestServer{Store: hs, AuthorizationUpdate: func(id, boot string) (*controlv1.AuthorizationUpdate, error) {
				return &controlv1.AuthorizationUpdate{Generation: 4, Payload: leasedPayload(t, id, boot)}, nil
			}}, serverTLS)
			if err := (&ControlClient{Node: node, TLS: clientTLS}).Connect(ctx, ln.Addr().String()); err != nil {
				t.Fatal(err)
			}
			// Wait until the conservative lower clock bound reaches not_before.
			for {
				low, _, trusted := node.TrustedTime()
				if trusted && !low.Before(time.Date(2026, 11, 5, 12, 1, 0, 0, time.UTC)) {
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
			if err := s.Close(); err != nil {
				t.Fatal(err)
			}
			if err := hs.Close(); err != nil {
				t.Fatal(err)
			}
			assertRawLedger(t, filepath.Join(dir, "hub.db"), 2)
			saveRuntimeLedger(t, network, dir)
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

func saveRuntimeLedger(t *testing.T, network uint8, dir string) {
	t.Helper()
	out := os.Getenv("PHASE5_AUTHORIZATION_EVIDENCE_DIR")
	if out == "" {
		return
	}
	if err := os.MkdirAll(out, 0755); err != nil {
		t.Fatal(err)
	}
	checksums := map[string]string{}
	for _, name := range []string{"agent", "hub"} {
		raw, err := os.ReadFile(filepath.Join(dir, name+".db"))
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
