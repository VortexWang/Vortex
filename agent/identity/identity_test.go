package identity

import (
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"os"
	"testing"
	"time"
)

func TestCSRIsSignedWithNodeSANAndShortTTL(t *testing.T) {
	now := time.Now().UTC()
	ca, caDER, err := NewCA(now)
	if err != nil {
		t.Fatal(err)
	}
	key, csr, err := GenerateCSR("node-1")
	if err != nil {
		t.Fatal(err)
	}
	cred, err := ca.SignCSR(csr, "node-1", now, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	cred, err = AttachKey(cred, key, caDER)
	if err != nil {
		t.Fatal(err)
	}
	if cred.Leaf.DNSNames[0] != "node-1" || !cred.Leaf.NotAfter.Before(now.Add(2*time.Hour)) {
		t.Fatalf("bad certificate: %+v", cred.Leaf)
	}
	pool := x509.NewCertPool()
	pool.AddCert(ca.Certificate)
	if _, ok := cred.Certificate.PrivateKey.(*ecdsa.PrivateKey); !ok {
		t.Fatalf("private key not retained")
	}
	if _, err := TLSConfig(cred, pool, nil, false); err != nil {
		t.Fatal(err)
	}
}

func TestEnrollmentTokenIsSingleUseAndPrivateKeyStaysLocal(t *testing.T) {
	now := time.Now().UTC()
	ca, caDER, err := NewCA(now)
	if err != nil {
		t.Fatal(err)
	}
	key, csr, err := GenerateCSR("node-enroll")
	if err != nil {
		t.Fatal(err)
	}
	enroller := &Enroller{CA: ca, Tokens: map[string]bool{"one-shot": true}}
	cred, err := enroller.Redeem("one-shot", "node-enroll", csr, now, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	cred, err = AttachKey(cred, key, caDER)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := enroller.Redeem("one-shot", "node-enroll", csr, now, time.Hour); err == nil {
		t.Fatal("token reused")
	}
	dir := t.TempDir()
	if err := SaveFiles(dir, cred, caDER); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(dir + "/runtime-key.pem")
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0600 {
		t.Fatalf("private key mode: %o", info.Mode().Perm())
	}
}

func TestAttachKeyRejectsKeyThatDoesNotMatchCSR(t *testing.T) {
	now := time.Now().UTC()
	ca, _, err := NewCA(now)
	if err != nil {
		t.Fatal(err)
	}
	_, csr, err := GenerateCSR("node-key-check")
	if err != nil {
		t.Fatal(err)
	}
	cred, err := ca.SignCSR(csr, "node-key-check", now, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	wrongKey, _, err := GenerateCSR("node-key-check")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := AttachKey(cred, wrongKey, nil); err == nil {
		t.Fatal("mismatched key accepted")
	}
}

func TestDetachedUpgradeSignature(t *testing.T) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	payload := []byte("artifact")
	sig := ed25519.Sign(priv, payload)
	if err := VerifyDetachedSignature(pub, payload, sig); err != nil {
		t.Fatal(err)
	}
	if err := VerifyDetachedSignature(pub, []byte("tampered"), sig); err == nil {
		t.Fatal("tampered artifact accepted")
	}
}

func TestRevokedCertificateIsRejected(t *testing.T) {
	now := time.Now().UTC()
	ca, _, err := NewCA(now)
	if err != nil {
		t.Fatal(err)
	}
	key, csr, err := GenerateCSR("node-2")
	if err != nil {
		t.Fatal(err)
	}
	cred, err := ca.SignCSR(csr, "node-2", now, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	cred, err = AttachKey(cred, key, nil)
	if err != nil {
		t.Fatal(err)
	}
	pool := x509.NewCertPool()
	pool.AddCert(ca.Certificate)
	conf, err := TLSConfig(cred, pool, func(cert *x509.Certificate) bool { return cert.SerialNumber.Cmp(cred.Leaf.SerialNumber) == 0 }, false)
	if err != nil {
		t.Fatal(err)
	}
	if err := conf.VerifyConnection(tls.ConnectionState{PeerCertificates: []*x509.Certificate{cred.Leaf}}); err == nil {
		t.Fatal("revoked certificate accepted")
	}
}
