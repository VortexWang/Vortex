package agent

import (
	"bufio"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"net"
	"path/filepath"
	"testing"
	"time"

	"vortex/agent/identity"
	"vortex/agent/store"
)

func x509Pool(cert *x509.Certificate) *x509.CertPool {
	p := x509.NewCertPool()
	p.AddCert(cert)
	return p
}

func TestPersistCredentialStoresNodeIdentityAndExpiry(t *testing.T) {
	now := time.Now().UTC()
	ca, _, err := identity.NewCA(now)
	if err != nil {
		t.Fatal(err)
	}
	key, csr, err := identity.GenerateCSR("node-credential")
	if err != nil {
		t.Fatal(err)
	}
	credential, err := ca.SignCSR(csr, "node-credential", now, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	credential, err = identity.AttachKey(credential, key, nil)
	if err != nil {
		t.Fatal(err)
	}
	db, err := store.Open(filepath.Join(t.TempDir(), "node.db"), "agent")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := PersistCredential(context.Background(), db, credential); err != nil {
		t.Fatal(err)
	}
	cert, expiry, err := db.Credential(context.Background(), credential.NodeID)
	if err != nil || string(cert) != string(credential.Leaf.Raw) || !expiry.Equal(credential.Leaf.NotAfter) {
		t.Fatalf("credential expiry=%s err=%v", expiry, err)
	}
}

func TestControlClientUsesMutualTLSAndPersistsAck(t *testing.T) {
	now := time.Now().UTC()
	ca, caDER, err := identity.NewCA(now)
	if err != nil {
		t.Fatal(err)
	}
	serverKey, serverCSR, err := identity.GenerateCSR("hub")
	if err != nil {
		t.Fatal(err)
	}
	serverCred, err := ca.SignCSR(serverCSR, "hub", now, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	serverCred, err = identity.AttachKey(serverCred, serverKey, caDER)
	if err != nil {
		t.Fatal(err)
	}
	clientKey, clientCSR, err := identity.GenerateCSR("node-1")
	if err != nil {
		t.Fatal(err)
	}
	clientCred, err := ca.SignCSR(clientCSR, "node-1", now, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	clientCred, err = identity.AttachKey(clientCred, clientKey, caDER)
	if err != nil {
		t.Fatal(err)
	}
	pool := x509Pool(ca.Certificate)
	serverTLS, err := identity.TLSConfig(serverCred, pool, nil, true)
	if err != nil {
		t.Fatal(err)
	}
	clientTLS, err := identity.TLSConfig(clientCred, pool, nil, false)
	if err != nil {
		t.Fatal(err)
	}
	clientTLS.ServerName = "hub"
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	defer ln.Close()
	tlsListener := tls.NewListener(ln, serverTLS)
	go func() {
		for {
			conn, acceptErr := tlsListener.Accept()
			if acceptErr != nil {
				return
			}
			go func(conn net.Conn) {
				defer conn.Close()
				var in controlWire
				if json.NewDecoder(bufio.NewReader(conn)).Decode(&in) == nil {
					_ = json.NewEncoder(conn).Encode(controlWire{Type: "ack", ID: in.ID, Version: in.Version, Sequence: in.Sequence})
				}
			}(conn)
		}
	}()
	s, err := store.Open(filepath.Join(t.TempDir(), "node.db"), "agent")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	n, err := New("node-1", s, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := n.Start(ctx); err != nil {
		t.Fatal(err)
	}
	client := &ControlClient{Node: n, TLS: clientTLS, MaxAttempts: 1}
	if err := client.ConnectRegion(ctx, "region-a", []HubEndpoint{{Region: "region-b", Address: "127.0.0.1:1"}, {Region: "region-a", Address: "127.0.0.1:1"}, {Region: "region-a", Address: ln.Addr().String()}}); err != nil {
		t.Fatal(err)
	}
	if count, err := s.ReceiptCount(ctx); err != nil || count != 1 {
		t.Fatalf("receipts=%d err=%v", count, err)
	}
	for key, want := range map[string]string{"control_sequence": "1"} {
		if got, err := s.AgentState(ctx, key); err != nil || got != want {
			t.Fatalf("%s=%q err=%v", key, got, err)
		}
	}
	if contact, err := s.AgentState(ctx, "hub_contact"); err != nil || contact == "" {
		t.Fatalf("successful Hub ACK did not persist Hub contact: %q %v", contact, err)
	}
	if _, err := s.AgentState(ctx, "backend_heartbeat"); err == nil {
		t.Fatal("Hub ACK incorrectly recorded as backend heartbeat")
	}
	if _, err := s.AgentState(ctx, "applied_version"); err == nil {
		t.Fatal("Hub ACK incorrectly recorded as applied configuration")
	}
}

func TestControlRejectsAcknowledgementForDifferentMessage(t *testing.T) {
	now := time.Now().UTC()
	ca, caDER, err := identity.NewCA(now)
	if err != nil {
		t.Fatal(err)
	}
	makeCredential := func(id string) identity.Credential {
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
	pool := x509Pool(ca.Certificate)
	serverTLS, err := identity.TLSConfig(makeCredential("hub"), pool, nil, true)
	if err != nil {
		t.Fatal(err)
	}
	clientTLS, err := identity.TLSConfig(makeCredential("node-ack"), pool, nil, false)
	if err != nil {
		t.Fatal(err)
	}
	clientTLS.ServerName = "hub"
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() {
		conn, err := tls.NewListener(ln, serverTLS).Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		var in controlWire
		if json.NewDecoder(conn).Decode(&in) == nil {
			_ = json.NewEncoder(conn).Encode(controlWire{Type: "ack", ID: "wrong-id", Version: in.Version, Sequence: in.Sequence})
		}
	}()
	s, err := store.Open(filepath.Join(t.TempDir(), "node.db"), "agent")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	n, err := New("node-ack", s, nil)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := (&ControlClient{Node: n, TLS: clientTLS, MaxAttempts: 1}).Connect(ctx, ln.Addr().String()); err == nil {
		t.Fatal("mismatched acknowledgement accepted")
	}
}

func TestControlRunnerReconnectsAfterLostStream(t *testing.T) {
	now := time.Now().UTC()
	ca, caDER, err := identity.NewCA(now)
	if err != nil {
		t.Fatal(err)
	}
	makeCredential := func(id string) identity.Credential {
		key, csr, err := identity.GenerateCSR(id)
		if err != nil {
			t.Fatal(err)
		}
		credential, err := ca.SignCSR(csr, id, now, time.Hour)
		if err != nil {
			t.Fatal(err)
		}
		credential, err = identity.AttachKey(credential, key, caDER)
		if err != nil {
			t.Fatal(err)
		}
		return credential
	}
	pool := x509Pool(ca.Certificate)
	serverTLS, err := identity.TLSConfig(makeCredential("hub"), pool, nil, true)
	if err != nil {
		t.Fatal(err)
	}
	clientTLS, err := identity.TLSConfig(makeCredential("node-reconnect"), pool, nil, false)
	if err != nil {
		t.Fatal(err)
	}
	clientTLS.ServerName = "hub"
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	listener := tls.NewListener(ln, serverTLS)
	accepted := make(chan int, 2)
	go func() {
		for attempt := 1; attempt <= 2; attempt++ {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			var in controlWire
			if json.NewDecoder(bufio.NewReader(conn)).Decode(&in) != nil {
				_ = conn.Close()
				continue
			}
			accepted <- attempt
			if attempt == 1 {
				_ = conn.Close()
				continue
			}
			_ = json.NewEncoder(conn).Encode(controlWire{Type: "ack", ID: in.ID, Version: in.Version, Sequence: in.Sequence})
			_ = conn.Close()
		}
	}()
	db, err := store.Open(filepath.Join(t.TempDir(), "node.db"), "agent")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	node, err := New("node-reconnect", db, nil)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	client := &ControlClient{Node: node, TLS: clientTLS, MaxAttempts: 1, RetryMin: 10 * time.Millisecond, RetryMax: 20 * time.Millisecond}
	done := make(chan error, 1)
	go func() { done <- client.Run(ctx, ln.Addr().String()) }()
	deadline := time.Now().Add(2 * time.Second)
	for {
		count, countErr := db.ReceiptCount(ctx)
		if countErr != nil {
			t.Fatal(countErr)
		}
		if count == 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("control runner did not recover after dropped stream")
		}
		time.Sleep(5 * time.Millisecond)
	}
	cancel()
	if err := <-done; err != context.Canceled {
		t.Fatalf("runner exit = %v, want context canceled", err)
	}
	if first, second := <-accepted, <-accepted; first != 1 || second != 2 {
		t.Fatalf("accepted connection order %d, %d", first, second)
	}
	sequence, err := db.AgentState(context.Background(), "control_sequence")
	if err != nil || sequence != "2" {
		t.Fatalf("sequence=%q err=%v, want both attempted exchanges durable", sequence, err)
	}
}
