package agent

import (
	"context"
	"crypto/ecdsa"
	"crypto/tls"
	"crypto/x509"
	"google.golang.org/protobuf/proto"
	"net"
	"path/filepath"
	"sync"
	"testing"
	"time"
	"vortex/hub"

	"vortex/agent/identity"
	"vortex/agent/store"
	controlv1 "vortex/contracts/protobuf/v1"
)

const runtimeIdentityNode = "550e8400-e29b-41d4-a716-446655440100"

func runtimeCredential(t *testing.T, ca *identity.CA, der []byte) identity.Credential {
	t.Helper()
	key, csr, err := identity.GenerateCSR(runtimeIdentityNode)
	if err != nil {
		t.Fatal(err)
	}
	c, err := ca.SignCSR(csr, runtimeIdentityNode, time.Now(), time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	c, err = identity.AttachKey(c, key, der)
	if err != nil {
		t.Fatal(err)
	}
	return c
}
func TestRuntimeIdentityProfileAndSignerRejections(t *testing.T) {
	ca, der, err := identity.NewCA(time.Now())
	if err != nil {
		t.Fatal(err)
	}
	c := runtimeCredential(t, ca, der)
	other := runtimeCredential(t, ca, der)
	roots := x509.NewCertPool()
	roots.AddCert(ca.Certificate)
	for _, name := range []string{"valid", "dns-only", "wrong-dns", "wrong-eku", "wrong-key-usage", "signer-mismatch", "tls-signer-mismatch", "expired", "unrelated-ca", "local-revoked"} {
		t.Run(name, func(t *testing.T) {
			candidate := c
			leaf := *c.Leaf
			candidate.Leaf = &leaf
			candidate.Certificate.Leaf = &leaf
			trust := roots
			revoked := func(*x509.Certificate) bool { return name == "local-revoked" }
			switch name {
			case "dns-only":
				leaf.URIs = nil
			case "wrong-dns":
				leaf.DNSNames = []string{"vortex"}
			case "wrong-eku":
				leaf.ExtKeyUsage = []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}
			case "wrong-key-usage":
				leaf.KeyUsage |= x509.KeyUsageKeyEncipherment
			case "signer-mismatch":
				candidate.PrivateKey = other.PrivateKey
			case "tls-signer-mismatch":
				candidate.Certificate.PrivateKey = other.PrivateKey
			case "expired":
				leaf.NotAfter = time.Now().Add(-time.Second)
			case "unrelated-ca":
				trust = x509.NewCertPool()
			}
			m, err := NewCredentialManager(candidate, trust, "hub", revoked)
			if err != nil {
				t.Fatal(err)
			}
			snap, err := m.RuntimeIdentity()
			if name != "valid" {
				if err == nil {
					t.Fatal("invalid runtime identity accepted")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if !snap.Signer.Public().(*ecdsa.PublicKey).Equal(c.Leaf.PublicKey) || snap.Check(c.Leaf) != nil {
				t.Fatal("incoherent runtime snapshot")
			}
		})
	}
}
func TestRuntimeIdentityConcurrentPublicationAndCurrentRevocation(t *testing.T) {
	ca, der, err := identity.NewCA(time.Now())
	if err != nil {
		t.Fatal(err)
	}
	first, next := runtimeCredential(t, ca, der), runtimeCredential(t, ca, der)
	roots := x509.NewCertPool()
	roots.AddCert(ca.Certificate)
	var revokeMu sync.RWMutex
	revoked := map[string]bool{}
	m, err := NewCredentialManager(first, roots, "hub", func(c *x509.Certificate) bool {
		revokeMu.RLock()
		defer revokeMu.RUnlock()
		return revoked[c.SerialNumber.String()]
	})
	if err != nil {
		t.Fatal(err)
	}
	original, err := m.RuntimeIdentity()
	if err != nil {
		t.Fatal(err)
	}
	var workers sync.WaitGroup
	for i := 0; i < 8; i++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for j := 0; j < 50; j++ {
				snap, e := m.RuntimeIdentity()
				if e != nil {
					t.Error(e)
					return
				}
				leaf, e := x509.ParseCertificate(snap.Certificate.Certificate[0])
				if e != nil || !snap.Signer.Public().(*ecdsa.PublicKey).Equal(leaf.PublicKey) {
					t.Error("mixed TLS/HELLO signer")
					return
				}
			}
		}()
	}
	// Publication has the same lock as the durable renewal commit path. Actual
	// commit/crash boundaries are also exercised by registration/reopen tests.
	for i := 0; i < 50; i++ {
		m.mu.Lock()
		m.set.Active = next
		m.mu.Unlock()
		m.mu.Lock()
		m.set.Active = first
		m.mu.Unlock()
	}
	workers.Wait()
	revokeMu.Lock()
	revoked[first.Leaf.SerialNumber.String()] = true
	revokeMu.Unlock()
	if original.Check(next.Leaf) == nil {
		t.Fatal("captured local revocation ignored")
	}
	if _, err = m.RuntimeIdentity(); err == nil {
		t.Fatal("revoked local snapshot published")
	}
	if _, err = m.TLSConfig(); err == nil {
		t.Fatal("revoked identity can reconnect control")
	}
	m.mu.Lock()
	m.set.Active = next
	m.mu.Unlock()
	valid, err := m.RuntimeIdentity()
	if err != nil {
		t.Fatal(err)
	}
	if valid.Check(first.Leaf) == nil {
		t.Fatal("current peer revocation ignored")
	}
}
func TestRuntimeIdentityRevocationsDurableAdditiveAndTimeBounded(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "identity.db")
	db, err := store.Open(path, "agent")
	if err != nil {
		t.Fatal(err)
	}
	ca, der, err := identity.NewCA(time.Now())
	if err != nil {
		t.Fatal(err)
	}
	c := runtimeCredential(t, ca, der)
	now := time.Now().UTC()
	trusted := true
	clock := func() (time.Time, time.Time, bool) { return now, now, trusted }
	view, err := LoadCredentialRevocations(ctx, db, clock)
	if err != nil {
		t.Fatal(err)
	}
	r := &controlv1.CredentialRevocations{IssuerId: regionalHubID, Revision: 1, CertificateSerials: []string{c.Leaf.SerialNumber.String()}, EffectiveAt: now.Add(time.Minute).Format(time.RFC3339Nano)}
	if err = view.Apply(ctx, regionalHubID, r); err != nil {
		t.Fatal(err)
	}
	if view.IsRevoked(c.Leaf) {
		t.Fatal("future revocation applied before boundary")
	}
	trusted = false
	if !view.IsRevoked(c.Leaf) {
		t.Fatal("unknown time bypassed affected revocation")
	}
	trusted = true
	now = now.Add(time.Minute)
	if !view.IsRevoked(c.Leaf) {
		t.Fatal("exact effective boundary ignored")
	}
	if err = view.Apply(ctx, regionalHubID, r); err != nil {
		t.Fatal(err)
	}
	changed := proto.Clone(r).(*controlv1.CredentialRevocations)
	changed.CertificateSerials = nil
	if err = view.Apply(ctx, regionalHubID, changed); err == nil {
		t.Fatal("changed replay accepted")
	}
	changed.Revision = 2
	if err = view.Apply(ctx, regionalHubID, changed); err != nil {
		t.Fatal(err)
	}
	if !view.IsRevoked(c.Leaf) {
		t.Fatal("empty next batch restored identity")
	}
	if err = view.Apply(ctx, runtimeIdentityNode, changed); err == nil {
		t.Fatal("foreign issuer accepted")
	}
	if err = db.Close(); err != nil {
		t.Fatal(err)
	}
	db, err = store.Open(path, "agent")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	view, err = LoadCredentialRevocations(ctx, db, clock)
	if err != nil {
		t.Fatal(err)
	}
	if !view.IsRevoked(c.Leaf) {
		t.Fatal("restart lost cumulative revocation")
	}
	// A failed persistence cannot publish a new revocation.
	next := runtimeCredential(t, ca, der)
	db.Close()
	r.Revision = 3
	r.CertificateSerials = []string{next.Leaf.SerialNumber.String()}
	if err = view.Apply(ctx, regionalHubID, r); err == nil {
		t.Fatal("closed store accepted revocation")
	}
	if view.IsRevoked(next.Leaf) {
		t.Fatal("uncommitted revocation published")
	}
}

func TestRuntimeIdentityCrashPublicationAndStaleAliases(t *testing.T) {
	ctx := context.Background()
	ca, der, err := identity.NewCA(time.Now())
	if err != nil {
		t.Fatal(err)
	}
	current, next := runtimeCredential(t, ca, der), runtimeCredential(t, ca, der)
	roots := x509.NewCertPool()
	roots.AddCert(ca.Certificate)
	path, dir := filepath.Join(t.TempDir(), "agent.db"), t.TempDir()
	db, err := store.Open(path, "agent")
	if err != nil {
		t.Fatal(err)
	}
	if err = identity.SaveFiles(dir, current, der); err != nil {
		t.Fatal(err)
	}
	if err = PersistCredential(ctx, db, current); err != nil {
		t.Fatal(err)
	}
	// Crash before transaction: a fully synced candidate file is not selection.
	if err = identity.SaveVersionFiles(dir, next); err != nil {
		t.Fatal(err)
	}
	db.Close()
	db, err = store.Open(path, "agent")
	if err != nil {
		t.Fatal(err)
	}
	m, err := LoadCredentialManager(ctx, db, current.NodeID, dir, "hub", roots, nil, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if m.Snapshot().Active.Leaf.SerialNumber.Cmp(current.Leaf.SerialNumber) != 0 {
		t.Fatal("uncommitted candidate became active")
	}
	// Commit then crash before alias publication: SQLite chooses next, not aliases.
	if err = identity.SaveVersionFiles(dir, next); err != nil {
		t.Fatal(err)
	}
	if err = db.SaveCredentialRotation(ctx, next.NodeID, next.Leaf.Raw, next.Leaf.NotAfter, current.Leaf.Raw, current.Leaf.NotAfter, time.Now().Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	db.Close()
	db, err = store.Open(path, "agent")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	recovered, err := LoadCredentialManager(ctx, db, next.NodeID, dir, "hub", roots, nil, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	snap, err := recovered.RuntimeIdentity()
	if err != nil {
		t.Fatal(err)
	}
	leaf, err := x509.ParseCertificate(snap.Certificate.Certificate[0])
	if err != nil || leaf.SerialNumber.Cmp(next.Leaf.SerialNumber) != 0 {
		t.Fatal("stale aliases replaced durable selection")
	}
	// Rejected durable write preserves already selected identity, and overlap
	// does not make a revoked active key eligible for control or VLink.
	tlsConfig, err := recovered.TLSConfig()
	if err != nil {
		t.Fatal(err)
	}
	if !tlsConfig.SessionTicketsDisabled || tlsConfig.ClientSessionCache != nil {
		t.Fatal("resumption enabled")
	}
	overlapExpired := recovered.Snapshot()
	overlapExpired.PreviousUntil = time.Now().Add(-time.Second)
	if len(overlapExpired.ValidCredentials(time.Now())) != 1 {
		t.Fatal("expired overlap retained")
	}
	revoked, err := LoadCredentialManager(ctx, db, next.NodeID, dir, "hub", roots, func(cert *x509.Certificate) bool { return cert.SerialNumber.Cmp(next.Leaf.SerialNumber) == 0 }, time.Now())
	if err == nil || revoked != nil {
		t.Fatal("revoked active key fell back to overlap")
	}
}

func TestRuntimeIdentityRenewalCommitFailureKeepsPriorSelection(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	ca, der, err := identity.NewCA(time.Now())
	if err != nil {
		t.Fatal(err)
	}
	c := runtimeCredential(t, ca, der)
	roots := x509.NewCertPool()
	roots.AddCert(ca.Certificate)
	serverTLS, err := identity.TLSConfig(c, roots, nil, true)
	if err != nil {
		t.Fatal(err)
	}
	server, err := hub.NewEnrollmentServer(ca, der, map[string]bool{"unused": true}, serverTLS, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go func() { _ = server.Serve(ctx, listener) }()
	db, err := store.Open(filepath.Join(t.TempDir(), "agent.db"), "agent")
	if err != nil {
		t.Fatal(err)
	}
	directory := t.TempDir()
	if err = identity.SaveFiles(directory, c, der); err != nil {
		t.Fatal(err)
	}
	if err = PersistCredential(ctx, db, c); err != nil {
		t.Fatal(err)
	}
	manager, err := NewCredentialManager(c, roots, c.NodeID, nil)
	if err != nil {
		t.Fatal(err)
	}
	db.Close()
	if err = manager.Renew(ctx, listener.Addr().String(), directory, db, time.Minute); err == nil {
		t.Fatal("closed FULL store accepted renewal")
	}
	snap, err := manager.RuntimeIdentity()
	if err != nil {
		t.Fatal(err)
	}
	leaf, err := x509.ParseCertificate(snap.Certificate.Certificate[0])
	if err != nil || leaf.SerialNumber.Cmp(c.Leaf.SerialNumber) != 0 {
		t.Fatal("uncommitted replacement published")
	}
	pair, err := tls.LoadX509KeyPair(filepath.Join(directory, "runtime-cert.pem"), filepath.Join(directory, "runtime-key.pem"))
	if err != nil {
		t.Fatal(err)
	}
	leaf, err = x509.ParseCertificate(pair.Certificate[0])
	if err != nil || leaf.SerialNumber.Cmp(c.Leaf.SerialNumber) != 0 {
		t.Fatal("uncommitted replacement changed aliases")
	}
}

func TestRuntimeIdentityCapturedControlRejectsExpiredPeerAndLocal(t *testing.T) {
	ca, der, err := identity.NewCA(time.Now())
	if err != nil {
		t.Fatal(err)
	}
	c := runtimeCredential(t, ca, der)
	roots := x509.NewCertPool()
	roots.AddCert(ca.Certificate)
	config, err := identity.TLSConfig(c, roots, nil, false)
	if err != nil {
		t.Fatal(err)
	}
	peer := *c.Leaf
	peer.NotAfter = time.Now().Add(-time.Second)
	if err = config.VerifyConnection(tls.ConnectionState{PeerCertificates: []*x509.Certificate{&peer}}); err == nil {
		t.Fatal("captured control config accepted expired peer")
	}
	local := *c.Leaf
	c.Leaf = &local
	config, err = identity.TLSConfig(c, roots, nil, false)
	if err != nil {
		t.Fatal(err)
	}
	local.NotAfter = time.Now().Add(-time.Second)
	if err = config.VerifyConnection(tls.ConnectionState{PeerCertificates: []*x509.Certificate{ca.Certificate}}); err == nil {
		t.Fatal("captured control config accepted expired local identity")
	}
}
