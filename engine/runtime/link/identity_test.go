package link

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"math/big"
	"sync/atomic"
	"testing"
	"time"
	"vortex/engine/vlink"
)

func supervisorIdentity(t *testing.T, serial int64) RuntimeIdentity {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{SerialNumber: big.NewInt(serial), NotBefore: time.Now().Add(-time.Minute), NotAfter: time.Now().Add(time.Hour), KeyUsage: x509.KeyUsageDigitalSignature}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	return RuntimeIdentity{NodeID: vlink.NodeID{1}, Certificate: tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}, Signer: key}
}
func TestRuntimeIdentitySnapshotCapturedOnceForTLSAndHello(t *testing.T) {
	first, next := supervisorIdentity(t, 1), supervisorIdentity(t, 2)
	var count atomic.Int64
	s := &Supervisor{NodeID: first.NodeID, TLS: &tls.Config{GetCertificate: func(*tls.ClientHelloInfo) (*tls.Certificate, error) { return &next.Certificate, nil }, ClientSessionCache: tls.NewLRUClientSessionCache(1)}, Generation: 4, ExpectedPeer: vlink.NodeID{2}}
	provider := func() (RuntimeIdentity, error) {
		if count.Add(1)%2 == 1 {
			return first, nil
		}
		return next, nil
	}
	boot := vlink.BootID{9}
	if err := s.SetIdentityProvider(provider, boot); err != nil {
		t.Fatal(err)
	}
	count.Store(0)
	for i := 0; i < 100; i++ {
		before := count.Load()
		snapshot, config, err := s.capture(true)
		if err != nil {
			t.Fatal(err)
		}
		if count.Load() != before+1 {
			t.Fatal("provider fetched multiple times")
		}
		hello := s.hello(true, snapshot)
		leaf, err := x509.ParseCertificate(config.Certificates[0].Certificate[0])
		if err != nil {
			t.Fatal(err)
		}
		if !hello.Signer.Public().(*ecdsa.PublicKey).Equal(leaf.PublicKey) || hello.BootID != boot || hello.Generation != 4 || hello.NodeID != s.NodeID {
			t.Fatal("mixed handshake identity/binding")
		}
		if config.GetCertificate != nil || config.GetClientCertificate != nil || config.GetConfigForClient != nil || !config.SessionTicketsDisabled || config.ClientSessionCache != nil || config.ServerName != s.ExpectedPeer.String() {
			t.Fatal("late TLS selection or resumption bypass enabled")
		}
	}
}
func TestRuntimeIdentityMismatchedSignerFailsBeforeHandshake(t *testing.T) {
	first, next := supervisorIdentity(t, 1), supervisorIdentity(t, 2)
	s := &Supervisor{NodeID: first.NodeID, TLS: &tls.Config{}}
	for _, name := range []string{"hello", "tls", "node"} {
		t.Run(name, func(t *testing.T) {
			id := first
			switch name {
			case "hello":
				id.Signer = next.Signer
			case "tls":
				id.Certificate.PrivateKey = next.Signer
			case "node":
				id.NodeID = vlink.NodeID{2}
			}
			s.identityProvider = func() (RuntimeIdentity, error) { return id, nil }
			if _, _, err := s.capture(false); err == nil {
				t.Fatal("mixed identity admitted")
			}
		})
	}
}
