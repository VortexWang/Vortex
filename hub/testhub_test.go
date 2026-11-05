package hub

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

func TestProtocolTestHubPersistsHelloReceipt(t *testing.T) {
	s, err := store.Open(filepath.Join(t.TempDir(), "hub.db"), "hub")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = (&TestServer{Store: s}).Serve(ctx, ln) }()
	conn, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	enc := json.NewEncoder(conn)
	if err := enc.Encode(Message{Type: "hello", NodeID: "n1", BootID: "b1", ID: "m1", Version: 1, Sequence: 7}); err != nil {
		t.Fatal(err)
	}
	conn.SetReadDeadline(time.Now().Add(time.Second))
	line, err := bufio.NewReader(conn).ReadBytes('\n')
	if err != nil || len(line) == 0 {
		t.Fatalf("ack: %v", err)
	}
	var ack Message
	if err := json.Unmarshal(line, &ack); err != nil || ack.Type != "config" || ack.ID != "m1" || ack.Sequence != 7 {
		t.Fatalf("ack=%+v err=%v", ack, err)
	}
	deadline := time.Now().Add(time.Second)
	for {
		n, countErr := s.ReceiptCount(context.Background())
		if countErr == nil && n == 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("receipt count=%d err=%v", n, countErr)
		}
	}
}

func TestTLSHelloBindsCertificateIdentity(t *testing.T) {
	now := time.Now()
	ca, caDER, err := identity.NewCA(now)
	if err != nil {
		t.Fatal(err)
	}
	issue := func(id string) tls.Certificate {
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
		return cred.Certificate
	}
	pool := x509.NewCertPool()
	pool.AddCert(ca.Certificate)
	serverConfig := &tls.Config{MinVersion: tls.VersionTLS13, Certificates: []tls.Certificate{issue("hub")}, ClientCAs: pool, ClientAuth: tls.RequireAndVerifyClientCert}
	nodeCertificate := issue("node-1")
	clientConfig := &tls.Config{MinVersion: tls.VersionTLS13, Certificates: []tls.Certificate{nodeCertificate}, RootCAs: pool, ServerName: "hub"}
	s, err := store.Open(filepath.Join(t.TempDir(), "hub.db"), "hub")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	server := &TestServer{Store: s}
	go func() { _ = server.ServeTLS(ctx, ln, serverConfig) }()
	for _, tc := range []struct{ node, want string }{{"node-1", "config"}, {"node-2", "error"}} {
		conn, err := tls.Dial("tcp", ln.Addr().String(), clientConfig)
		if err != nil {
			t.Fatal(err)
		}
		_ = conn.SetDeadline(time.Now().Add(time.Second))
		if err := json.NewEncoder(conn).Encode(Message{Type: "hello", NodeID: tc.node, BootID: "boot", ID: tc.node, Sequence: 1, Version: 1}); err != nil {
			t.Fatal(err)
		}
		var response Message
		if err := json.NewDecoder(conn).Decode(&response); err != nil {
			t.Fatal(err)
		}
		_ = conn.Close()
		if response.Type != tc.want {
			t.Fatalf("node=%s response=%s want=%s", tc.node, response.Type, tc.want)
		}
	}
	if count, err := s.ReceiptCount(ctx); err != nil || count != 1 {
		t.Fatalf("receipts=%d err=%v", count, err)
	}
	nodeLeaf, err := x509.ParseCertificate(nodeCertificate.Certificate[0])
	if err != nil {
		t.Fatal(err)
	}
	server.Revoke(nodeLeaf.SerialNumber.String())
	conn, err := tls.Dial("tcp", ln.Addr().String(), clientConfig)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(time.Second))
	if err := json.NewEncoder(conn).Encode(Message{Type: "hello", NodeID: "node-1", BootID: "boot", ID: "revoked-node", Sequence: 2, Version: 1}); err != nil {
		t.Fatal(err)
	}
	var response Message
	if err := json.NewDecoder(conn).Decode(&response); err != nil {
		t.Fatal(err)
	}
	if response.Type != "error" {
		t.Fatalf("revoked certificate response=%s, want error", response.Type)
	}
}
