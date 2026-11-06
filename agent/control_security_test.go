package agent

import (
	"context"
	"crypto/tls"
	"path/filepath"
	"testing"
	"time"

	"vortex/agent/store"
	"vortex/hub"
)

// Defense in depth: even an accidentally permissive TLS listener must not
// turn an unverified certificate SAN into an authenticated control identity.
func TestControlHubRejectsUnverifiedPeerChain(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	serverTLS, clientTLS, _ := controlTLS(t, controlTestNodeID)
	_, rogueTLS, _ := controlTLS(t, controlTestNodeID)
	clientTLS.Certificates = rogueTLS.Certificates
	serverTLS.ClientAuth = tls.RequestClientCert
	h, err := store.Open(filepath.Join(t.TempDir(), "hub.db"), "hub")
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close()
	s, err := store.Open(filepath.Join(t.TempDir(), "agent.db"), "agent")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	n, err := New(controlTestNodeID, s, nil)
	if err != nil {
		t.Fatal(err)
	}
	listener := startTestControlHub(t, ctx, &hub.TestServer{Store: h}, serverTLS)
	client := &ControlClient{Node: n, TLS: clientTLS}
	if err := client.Connect(ctx, listener.Addr().String()); err == nil {
		t.Fatal("untrusted client certificate accepted as control identity")
	}
	if count, err := h.ReceiptCount(ctx); err != nil || count != 0 {
		t.Fatalf("untrusted peer persisted receipt: count=%d err=%v", count, err)
	}
}
