package agent

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"net"
	"path/filepath"
	"testing"
	"time"

	"vortex/agent/identity"
	"vortex/agent/store"
	controlv1 "vortex/contracts/protobuf/v1"
	"vortex/engine/apply"
	"vortex/hub"
)

const controlTestNodeID = "550e8400-e29b-41d4-a716-446655440000"

func controlTLS(t *testing.T, clientID string) (*tls.Config, *tls.Config, *x509.CertPool) {
	t.Helper()
	now := time.Now().UTC()
	ca, caDER, err := identity.NewCA(now)
	if err != nil {
		t.Fatal(err)
	}
	issue := func(id string) identity.Credential {
		key, csr, err := identity.GenerateCSR(id)
		if err != nil {
			t.Fatal(err)
		}
		cred, err := ca.SignCSR(csr, id, now, time.Hour)
		if err != nil {
			t.Fatal(err)
		}
		cred, err = identity.AttachKey(cred, key, caDER)
		if err != nil {
			t.Fatal(err)
		}
		return cred
	}
	pool := x509.NewCertPool()
	pool.AddCert(ca.Certificate)
	serverTLS, err := identity.TLSConfig(issue("hub"), pool, nil, true)
	if err != nil {
		t.Fatal(err)
	}
	clientTLS, err := identity.TLSConfig(issue(clientID), pool, nil, false)
	if err != nil {
		t.Fatal(err)
	}
	clientTLS.ServerName = "hub"
	return serverTLS, clientTLS, pool
}

func startTestControlHub(t *testing.T, ctx context.Context, server *hub.TestServer, tlsConfig *tls.Config) net.Listener {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go func() { _ = server.ServeGRPC(ctx, listener, tlsConfig) }()
	t.Cleanup(func() { _ = listener.Close() })
	return listener
}

func TestControlClientAppliesRuntimeConfigOverMutualTLSGRPC(t *testing.T) {
	ctx := context.Background()
	serverTLS, clientTLS, _ := controlTLS(t, controlTestNodeID)
	hubStore, err := store.Open(filepath.Join(t.TempDir(), "hub.db"), "hub")
	if err != nil {
		t.Fatal(err)
	}
	defer hubStore.Close()
	nodeStore, err := store.Open(filepath.Join(t.TempDir(), "agent.db"), "agent")
	if err != nil {
		t.Fatal(err)
	}
	defer nodeStore.Close()
	manager := apply.NewManager()
	reconciler := &Reconciler{Store: nodeStore, Apply: EngineApplier{Manager: manager}}
	payload := []byte(`{"version":2,"node_id":"` + controlTestNodeID + `","listeners":[],"rules":[],"revocations":[]}`)
	digest := sha256.Sum256(payload)
	update := &controlv1.RuntimeConfigUpdate{Version: 2, Payload: payload, TransactionId: []byte("0123456789abcdef"), FencingGeneration: 1, PayloadSha256: digest[:], ExpectedVersion: 0}
	server := &hub.TestServer{Store: hubStore, ConfigUpdate: update}
	listener := startTestControlHub(t, ctx, server, serverTLS)
	node, err := New(controlTestNodeID, nodeStore, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := node.Start(ctx); err != nil {
		t.Fatal(err)
	}
	client := &ControlClient{Node: node, TLS: clientTLS, Reconciler: reconciler, MaxAttempts: 1}
	if err := client.Connect(ctx, listener.Addr().String()); err != nil {
		t.Fatal(err)
	}
	if applied, err := nodeStore.AgentState(ctx, "applied_version"); err != nil || applied != "2" {
		t.Fatalf("applied version=%q err=%v", applied, err)
	}
	if receipts, err := hubStore.ReceiptCount(ctx); err != nil || receipts != 1 {
		t.Fatalf("Hub receipts=%d err=%v", receipts, err)
	}
	if events, err := hubStore.EventCount(ctx); err != nil || events != 1 {
		t.Fatalf("Hub apply result events=%d err=%v", events, err)
	}
	if contact, err := nodeStore.AgentState(ctx, "hub_contact"); err != nil || contact == "" {
		t.Fatalf("Hub contact=%q err=%v", contact, err)
	}
	if _, err := nodeStore.AgentState(ctx, "backend_heartbeat"); err == nil {
		t.Fatal("Hub control stream incorrectly recorded a backend heartbeat")
	}
}

func TestControlClientRejectsCertificateNodeMismatch(t *testing.T) {
	ctx := context.Background()
	serverTLS, clientTLS, _ := controlTLS(t, "550e8400-e29b-41d4-a716-446655440001")
	hubStore, err := store.Open(filepath.Join(t.TempDir(), "hub.db"), "hub")
	if err != nil {
		t.Fatal(err)
	}
	defer hubStore.Close()
	nodeStore, err := store.Open(filepath.Join(t.TempDir(), "agent.db"), "agent")
	if err != nil {
		t.Fatal(err)
	}
	defer nodeStore.Close()
	listener := startTestControlHub(t, ctx, &hub.TestServer{Store: hubStore}, serverTLS)
	node, err := New(controlTestNodeID, nodeStore, nil)
	if err != nil {
		t.Fatal(err)
	}
	err = (&ControlClient{Node: node, TLS: clientTLS, MaxAttempts: 1}).Connect(ctx, listener.Addr().String())
	if err == nil {
		t.Fatal("control session with mismatched certificate identity was accepted")
	}
	if count, err := hubStore.ReceiptCount(ctx); err != nil || count != 0 {
		t.Fatalf("mismatched identity persisted receipt count=%d err=%v", count, err)
	}
}

func TestControlRunnerReconnectsAfterServerClosesStream(t *testing.T) {
	serverTLS, clientTLS, _ := controlTLS(t, controlTestNodeID)
	hubStore, err := store.Open(filepath.Join(t.TempDir(), "hub.db"), "hub")
	if err != nil {
		t.Fatal(err)
	}
	defer hubStore.Close()
	nodeStore, err := store.Open(filepath.Join(t.TempDir(), "agent.db"), "agent")
	if err != nil {
		t.Fatal(err)
	}
	defer nodeStore.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	listener := startTestControlHub(t, ctx, &hub.TestServer{Store: hubStore}, serverTLS)
	node, err := New(controlTestNodeID, nodeStore, nil)
	if err != nil {
		t.Fatal(err)
	}
	client := &ControlClient{Node: node, TLS: clientTLS, Interval: 10 * time.Millisecond, RetryMin: time.Millisecond, RetryMax: 10 * time.Millisecond}
	runErr := make(chan error, 1)
	go func() { runErr <- client.Run(ctx, listener.Addr().String()) }()
	deadline := time.Now().Add(2 * time.Second)
	for {
		count, countErr := hubStore.ReceiptCount(context.Background())
		if countErr == nil && count >= 2 {
			cancel()
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("control stream did not reconnect, receipts=%d err=%v", count, countErr)
		}
		time.Sleep(5 * time.Millisecond)
	}
	if err := <-runErr; !errors.Is(err, context.Canceled) {
		t.Fatalf("runner result=%v", err)
	}
}
