package identity

import (
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"os"
	"sync"
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
	if err := os.Chmod(dir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dir+"/runtime-key.pem", []byte("old"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := SaveFiles(dir, cred, caDER); err != nil {
		t.Fatal(err)
	}
	dirInfo, err := os.Stat(dir)
	if err != nil {
		t.Fatal(err)
	}
	if dirInfo.Mode().Perm() != 0700 {
		t.Fatalf("credential directory mode=%o", dirInfo.Mode().Perm())
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

func TestEnrollmentTokenCanBeRedeemedOnlyOnceConcurrently(t *testing.T) {
	now := time.Now().UTC()
	ca, _, err := NewCA(now)
	if err != nil {
		t.Fatal(err)
	}
	_, csr, err := GenerateCSR("node-race")
	if err != nil {
		t.Fatal(err)
	}
	enroller := &Enroller{CA: ca, Tokens: map[string]bool{"single": true}}
	var wg sync.WaitGroup
	var mu sync.Mutex
	successes := 0
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := enroller.Redeem("single", "node-race", csr, now, time.Hour); err == nil {
				mu.Lock()
				successes++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	if successes != 1 {
		t.Fatalf("successful redemptions=%d, want 1", successes)
	}
}

func TestInvalidCSRDoesNotConsumeEnrollmentToken(t *testing.T) {
	now := time.Now().UTC()
	ca, _, err := NewCA(now)
	if err != nil {
		t.Fatal(err)
	}
	enroller := &Enroller{CA: ca, Tokens: map[string]bool{"retry": true}}
	if _, err := enroller.Redeem("retry", "node-retry", []byte("invalid"), now, time.Hour); err == nil {
		t.Fatal("invalid CSR accepted")
	}
	_, csr, err := GenerateCSR("node-retry")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := enroller.Redeem("retry", "node-retry", csr, now, time.Hour); err != nil {
		t.Fatalf("token consumed by invalid CSR: %v", err)
	}
}

func TestCredentialRotationValidatesBeforeOverlapAndExpiresOldCredential(t *testing.T) {
	now := time.Now().UTC()
	ca, caDER, err := NewCA(now)
	if err != nil {
		t.Fatal(err)
	}
	makeCredential := func() Credential {
		key, csr, err := GenerateCSR("node-rotate")
		if err != nil {
			t.Fatal(err)
		}
		cred, err := ca.SignCSR(csr, "node-rotate", now, time.Hour)
		if err != nil {
			t.Fatal(err)
		}
		cred, err = AttachKey(cred, key, caDER)
		if err != nil {
			t.Fatal(err)
		}
		return cred
	}
	old, next := makeCredential(), makeCredential()
	set := CredentialSet{Active: old}
	if _, err := set.Rotate(next, now, time.Minute, func(Credential) error { return errors.New("revoked") }); err == nil {
		t.Fatal("invalid replacement accepted")
	}
	rotated, err := set.Rotate(next, now, time.Minute, func(Credential) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	if len(rotated.ValidCredentials(now)) != 2 {
		t.Fatal("old credential not retained during overlap")
	}
	if len(rotated.ValidCredentials(now.Add(time.Minute))) != 1 {
		t.Fatal("old credential retained after overlap")
	}
}

func TestTLSConfigRejectsExpiredLocalCredential(t *testing.T) {
	now := time.Now().UTC()
	ca, _, err := NewCA(now.Add(-3 * time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	key, csr, err := GenerateCSR("node-expired")
	if err != nil {
		t.Fatal(err)
	}
	credential, err := ca.SignCSR(csr, "node-expired", now.Add(-2*time.Hour), time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	credential, err = AttachKey(credential, key, nil)
	if err != nil {
		t.Fatal(err)
	}
	pool := x509.NewCertPool()
	pool.AddCert(ca.Certificate)
	if _, err := TLSConfig(credential, pool, nil, false); err == nil {
		t.Fatal("expired local credential accepted")
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
