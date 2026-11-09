package vlink

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"net"
	"strings"
	"testing"
	"time"
)

// Mutate mirrored fields together: signature validation must reject even when
// the client/server transcript is internally consistent.
func TestHelloSignedTranscriptTamperingMatrix(t *testing.T) {
	mutations := map[string]func(*HelloMessage, *HelloMessage){
		"client-node":         func(c, s *HelloMessage) { c.LocalNodeID[0] ^= 16; s.PeerNodeID = c.LocalNodeID },
		"server-node":         func(c, s *HelloMessage) { s.LocalNodeID[0] ^= 16; c.PeerNodeID = s.LocalNodeID },
		"client-boot":         func(c, s *HelloMessage) { c.LocalBootID[0] ^= 16; s.PeerBootID = c.LocalBootID },
		"server-boot":         func(c, s *HelloMessage) { s.LocalBootID[0] ^= 16; c.PeerBootID = s.LocalBootID },
		"client-generation":   func(c, s *HelloMessage) { c.LocalGeneration++; s.PeerGeneration = c.LocalGeneration },
		"server-generation":   func(c, s *HelloMessage) { s.LocalGeneration++; c.PeerGeneration = s.LocalGeneration },
		"client-nonce":        func(c, s *HelloMessage) { c.LocalNonce[0] ^= 16; s.PeerNonce = c.LocalNonce },
		"server-nonce":        func(c, s *HelloMessage) { s.LocalNonce[0] ^= 16; c.PeerNonce = s.LocalNonce },
		"client-capabilities": func(c, s *HelloMessage) { c.LocalCapabilities = []uint16{3}; s.PeerCapabilities = []uint16{3} },
		"server-capabilities": func(c, s *HelloMessage) { s.LocalCapabilities = []uint16{4}; c.PeerCapabilities = []uint16{4} },
	}
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	for name, mutate := range mutations {
		t.Run(name, func(t *testing.T) {
			c, s := helloPair()
			exporter := []byte("test exporter")
			s.Signature, err = SignHelloTranscript(c, s, exporter, key)
			if err != nil {
				t.Fatal(err)
			}
			if err := VerifyHelloTranscript(c, s, exporter, key.Public()); err != nil {
				t.Fatal(err)
			}
			mutate(&c, &s)
			if _, err := HelloTranscript(c, s); err != nil {
				t.Fatalf("invalid structure instead of signature test: %v", err)
			}
			if err := VerifyHelloTranscript(c, s, exporter, key.Public()); err == nil {
				t.Fatal("accepted altered signed transcript")
			}
		})
	}
}

func TestHandshakeHelloRejectsDifferentSigningKey(t *testing.T) {
	clientID, serverID := NodeID{71}, NodeID{72}
	clientTLS, _ := testTLSIdentity(t, clientID)
	serverTLS, serverSigner := testTLSIdentity(t, serverID)
	trustTLS13Peers(t, clientTLS, serverTLS)
	wrongKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	clientRaw, serverRaw := net.Pipe()
	defer clientRaw.Close()
	defer serverRaw.Close()
	client, server := tls.Client(clientRaw, clientTLS), tls.Server(serverRaw, serverTLS)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	serverResult := make(chan error, 1)
	verified := false
	go func() {
		_, err := HandshakeHello(ctx, server, HelloConfig{NodeID: serverID, BootID: BootID{2}, Generation: 1, Signer: serverSigner, VerifyPeer: func(context.Context, *x509.Certificate, HelloMessage) error { verified = true; return nil }})
		serverResult <- err
		serverRaw.Close()
	}()
	_, clientErr := HandshakeHello(ctx, client, HelloConfig{Client: true, NodeID: clientID, BootID: BootID{1}, Generation: 1, Signer: wrongKey, VerifyPeer: func(context.Context, *x509.Certificate, HelloMessage) error { return nil }})
	serverErr := <-serverResult
	if serverErr == nil || !strings.Contains(serverErr.Error(), "signature") {
		t.Fatalf("expected signature rejection, got %v", serverErr)
	}
	if clientErr == nil || verified {
		t.Fatalf("bad signer accepted: client=%v verified=%v", clientErr, verified)
	}
}
