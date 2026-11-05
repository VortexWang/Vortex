package agent

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"vortex/agent/identity"
	"vortex/agent/store"
	"vortex/hub"
)

func TestRegisterGeneratesLocalKeyAndPersistsIssuedCredential(t *testing.T) {
	now := time.Now().UTC()
	ca, caDER, err := identity.NewCA(now)
	if err != nil {
		t.Fatal(err)
	}
	hubKey, hubCSR, err := identity.GenerateCSR("hub")
	if err != nil {
		t.Fatal(err)
	}
	hubCredential, err := ca.SignCSR(hubCSR, "hub", now, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	hubCredential, err = identity.AttachKey(hubCredential, hubKey, caDER)
	if err != nil {
		t.Fatal(err)
	}
	serverTLS := &tls.Config{MinVersion: tls.VersionTLS13, MaxVersion: tls.VersionTLS13, Certificates: []tls.Certificate{hubCredential.Certificate}}
	server, err := hub.NewEnrollmentServer(ca, caDER, map[string]bool{"one-shot": true}, serverTLS, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = server.Serve(ctx, ln) }()
	roots := x509.NewCertPool()
	roots.AddCert(ca.Certificate)
	db, err := store.Open(filepath.Join(t.TempDir(), "agent.db"), "agent")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	certDir := filepath.Join(t.TempDir(), "credentials")
	credential, err := Register(ctx, "00000000-0000-0000-0000-000000000021", "one-shot", ln.Addr().String(), certDir, roots, "hub", db)
	if err != nil {
		t.Fatal(err)
	}
	if credential.Leaf.Subject.CommonName != credential.NodeID || len(credential.Leaf.DNSNames) != 1 || credential.Leaf.DNSNames[0] != credential.NodeID {
		t.Fatalf("issued certificate identity mismatch: %+v", credential.Leaf.Subject)
	}
	if _, err := os.Stat(filepath.Join(certDir, "runtime-key.pem")); err != nil {
		t.Fatalf("private key was not persisted locally: %v", err)
	}
	stored, expiry, err := db.Credential(context.Background(), credential.NodeID)
	if err != nil || string(stored) != string(credential.Leaf.Raw) || !expiry.Equal(credential.Leaf.NotAfter) {
		t.Fatalf("persisted credential mismatch: expiry=%v err=%v", expiry, err)
	}
	if _, err := Register(ctx, credential.NodeID, "one-shot", ln.Addr().String(), filepath.Join(t.TempDir(), "again"), roots, "hub", db); err == nil {
		t.Fatal("enrollment token was accepted a second time")
	}
}
