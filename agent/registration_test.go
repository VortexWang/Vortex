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
	dbPath := filepath.Join(t.TempDir(), "agent.db")
	db, err := store.Open(dbPath, "agent")
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
	keyInfo, err := os.Stat(filepath.Join(certDir, "runtime-key.pem"))
	if err != nil || keyInfo.Mode().Perm() != 0600 {
		t.Fatalf("private key mode=%v err=%v", keyInfo.Mode().Perm(), err)
	}
	dirInfo, err := os.Stat(certDir)
	if err != nil || dirInfo.Mode().Perm() != 0700 {
		t.Fatalf("credential directory mode=%v err=%v", dirInfo.Mode().Perm(), err)
	}
	stored, expiry, err := db.Credential(context.Background(), credential.NodeID)
	if err != nil || string(stored) != string(credential.Leaf.Raw) || !expiry.Equal(credential.Leaf.NotAfter) {
		t.Fatalf("persisted credential mismatch: expiry=%v err=%v", expiry, err)
	}
	if _, err := Register(ctx, credential.NodeID, "one-shot", ln.Addr().String(), filepath.Join(t.TempDir(), "again"), roots, "hub", db); err == nil {
		t.Fatal("enrollment token was accepted a second time")
	}
	manager, err := NewCredentialManager(credential, roots, "hub", nil)
	if err != nil {
		t.Fatal(err)
	}
	aliasBefore, err := os.ReadFile(filepath.Join(certDir, "runtime-cert.pem"))
	if err != nil {
		t.Fatal(err)
	}
	rejectingManager, err := NewCredentialManager(credential, roots, "hub", func(*x509.Certificate) bool { return true })
	if err != nil {
		t.Fatal(err)
	}
	if err := rejectingManager.Renew(ctx, ln.Addr().String(), certDir, db, 5*time.Minute); err == nil {
		t.Fatal("replacement rejected by local validation was accepted")
	}
	storedAfterRejectedRenewal, _, err := db.Credential(ctx, credential.NodeID)
	if err != nil || string(storedAfterRejectedRenewal) != string(credential.Leaf.Raw) {
		t.Fatalf("rejected renewal changed durable active credential: %v", err)
	}
	aliasAfter, err := os.ReadFile(filepath.Join(certDir, "runtime-cert.pem"))
	if err != nil || string(aliasAfter) != string(aliasBefore) {
		t.Fatalf("rejected renewal changed active certificate alias: %v", err)
	}
	if err := manager.Renew(ctx, ln.Addr().String(), certDir, db, 5*time.Minute); err != nil {
		t.Fatalf("authenticated renewal: %v", err)
	}
	rotated := manager.Snapshot()
	if rotated.Active.Leaf.SerialNumber.Cmp(credential.Leaf.SerialNumber) == 0 {
		t.Fatal("renewal did not issue a new certificate")
	}
	if rotated.Previous == nil || rotated.Previous.Leaf.SerialNumber.Cmp(credential.Leaf.SerialNumber) != 0 {
		t.Fatal("validated rotation did not preserve previous credential for overlap")
	}
	persistedVersions, err := db.CredentialVersions(ctx, credential.NodeID, time.Now())
	if err != nil || len(persistedVersions) != 2 {
		t.Fatalf("persisted overlap credential count=%d err=%v", len(persistedVersions), err)
	}
	clientTLS, err := manager.TLSConfig()
	if err != nil || len(clientTLS.Certificates) != 2 {
		t.Fatalf("overlap TLS certificate set=%d err=%v", len(clientTLS.Certificates), err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	db, err = store.Open(dbPath, "agent")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	restored, err := LoadCredentialManager(ctx, db, credential.NodeID, certDir, "hub", roots, nil, time.Now())
	if err != nil {
		t.Fatalf("restore credential overlap after restart: %v", err)
	}
	restoredTLS, err := restored.TLSConfig()
	if err != nil || len(restoredTLS.Certificates) != 2 {
		t.Fatalf("restored overlap TLS certificate set=%d err=%v", len(restoredTLS.Certificates), err)
	}
	if err := identity.SaveActiveFiles(certDir, credential, caDER); err != nil {
		t.Fatal(err)
	}
	recoveredActive, err := LoadCredentialManager(ctx, db, credential.NodeID, certDir, "hub", roots, nil, time.Now())
	if err != nil {
		t.Fatalf("recover from durable rotation after active alias rollback: %v", err)
	}
	if recoveredActive.Snapshot().Active.Leaf.SerialNumber.Cmp(rotated.Active.Leaf.SerialNumber) != 0 {
		t.Fatal("active credential was selected from stale alias instead of durable state")
	}
	if err := identity.SaveActiveFiles(certDir, recoveredActive.Snapshot().Active, caDER); err != nil {
		t.Fatal(err)
	}
	repairedAlias, err := tls.LoadX509KeyPair(filepath.Join(certDir, "runtime-cert.pem"), filepath.Join(certDir, "runtime-key.pem"))
	if err != nil || string(repairedAlias.Certificate[0]) != string(rotated.Active.Leaf.Raw) {
		t.Fatalf("active alias repair failed: %v", err)
	}
	revokedPrevious, err := LoadCredentialManager(ctx, db, credential.NodeID, certDir, "hub", roots, func(cert *x509.Certificate) bool {
		return cert.SerialNumber.Cmp(credential.Leaf.SerialNumber) == 0
	}, time.Now())
	if err != nil {
		t.Fatalf("revoked overlap credential prevented active credential recovery: %v", err)
	}
	revokedPreviousTLS, err := revokedPrevious.TLSConfig()
	if err != nil || len(revokedPreviousTLS.Certificates) != 1 {
		t.Fatalf("revoked overlap credential remained available: certs=%d err=%v", len(revokedPreviousTLS.Certificates), err)
	}
	future := time.Now().Add(6 * time.Minute)
	afterOverlap, err := LoadCredentialManager(ctx, db, credential.NodeID, certDir, "hub", roots, nil, future)
	if err != nil {
		t.Fatalf("load active credential after overlap: %v", err)
	}
	afterOverlapTLS, err := afterOverlap.TLSConfig()
	if err != nil || len(afterOverlapTLS.Certificates) != 1 {
		t.Fatalf("expired overlap was retained: certs=%d err=%v", len(afterOverlapTLS.Certificates), err)
	}
	if _, err := os.Stat(filepath.Join(certDir, "runtime-key-"+credential.Leaf.SerialNumber.String()+".pem")); !os.IsNotExist(err) {
		t.Fatalf("expired overlap private key was not pruned: %v", err)
	}
	server.Revoke(rotated.Active.Leaf.SerialNumber.String())
	if err := manager.Renew(ctx, ln.Addr().String(), certDir, db, 5*time.Minute); err == nil {
		t.Fatal("revoked credential was allowed to renew")
	}
}
