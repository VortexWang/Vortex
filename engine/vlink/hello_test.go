package vlink

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"errors"
	"math/big"
	"net"
	"net/url"
	"testing"
	"time"
)

func TestHelloSignatureBindsExporterAndDirection(t *testing.T) {
	serverKey, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	client, server := helloPair()
	transcript, err := HelloTranscript(client, server)
	if err != nil {
		t.Fatal(err)
	}
	exporter := []byte("exporter A")
	sig, err := signVLink(serverKey, helloSignatureInput(transcript, exporter))
	if err != nil {
		t.Fatal(err)
	}
	server.Signature = sig
	if err := VerifyHelloTranscript(client, server, exporter, serverKey.Public()); err != nil {
		t.Fatal(err)
	}
	if err := VerifyHelloTranscript(client, server, []byte("exporter B"), serverKey.Public()); err == nil {
		t.Fatal("accepted a different exporter")
	}
	client.Role = 1
	if err := VerifyHelloTranscript(client, server, exporter, serverKey.Public()); err == nil {
		t.Fatal("accepted a different role")
	}
}

func TestHelloRoundTripAndRejectsUnsupportedVersion(t *testing.T) {
	message, _ := helloPair()
	message.Phase = HelloOffer
	message.PeerNodeID, message.PeerBootID, message.PeerNonce = NodeID{}, BootID{}, HelloNonce{}
	message.PeerGeneration = 0
	message.LocalCapabilities = []uint16{1, 7}
	message.Signature = []byte{1}
	payload, err := MarshalHello(message)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := ParseHello(payload)
	if err != nil {
		t.Fatal(err)
	}
	if decoded.LocalNodeID != message.LocalNodeID || !equalCapabilities(decoded.LocalCapabilities, message.LocalCapabilities) {
		t.Fatalf("round trip mismatch: %#v", decoded)
	}
	decoded.Version++
	if _, err := MarshalHello(decoded); err == nil {
		t.Fatal("accepted unsupported version")
	}
}

func TestNodeIDFromCertificateRequiresCanonicalSAN(t *testing.T) {
	id := NodeID{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16}
	uri, _ := url.Parse("urn:vortex:node:" + id.String())
	got, err := NodeIDFromCertificate(&x509.Certificate{URIs: []*url.URL{uri}})
	if err != nil || got != id {
		t.Fatalf("got %v, %v", got, err)
	}
	if _, err := NodeIDFromCertificate(&x509.Certificate{}); err == nil {
		t.Fatal("accepted missing node SAN")
	}
	badURI, _ := url.Parse("urn:vortex:node:AAAAAAAA-AAAA-AAAA-AAAA-AAAAAAAAAAAA")
	if _, err := NodeIDFromCertificate(&x509.Certificate{URIs: []*url.URL{badURI}}); err == nil {
		t.Fatal("accepted non-canonical node SAN")
	}
}

func TestHandshakeHelloTLS13MutualIdentity(t *testing.T) {
	clientID := NodeID{1}
	serverID := NodeID{2}
	clientTLS, clientSigner := testTLSIdentity(t, clientID)
	serverTLS, serverSigner := testTLSIdentity(t, serverID)
	trustTLS13Peers(t, clientTLS, serverTLS)
	clientConn, serverConn := net.Pipe()
	client := tls.Client(clientConn, clientTLS)
	server := tls.Server(serverConn, serverTLS)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	verify := func(context.Context, *x509.Certificate, HelloMessage) error { return nil }
	result := make(chan error, 2)
	go func() {
		peer, err := HandshakeHello(ctx, client, HelloConfig{Client: true, NodeID: clientID, BootID: BootID{3}, Generation: 1, Signer: clientSigner, VerifyPeer: verify})
		if err == nil && peer != serverID {
			t.Errorf("client peer = %v", peer)
		}
		result <- err
	}()
	go func() {
		peer, err := HandshakeHello(ctx, server, HelloConfig{NodeID: serverID, BootID: BootID{4}, Generation: 2, Signer: serverSigner, VerifyPeer: verify})
		if err == nil && peer != clientID {
			t.Errorf("server peer = %v", peer)
		}
		result <- err
	}()
	for range 2 {
		if err := <-result; err != nil {
			t.Fatal(err)
		}
	}
	_ = client.Close()
	_ = server.Close()
}

func TestHandshakeHelloPeerVerifierRejectsStaleGenerationAndReplay(t *testing.T) {
	for _, testCase := range []struct {
		name       string
		generation uint64
		want       string
	}{{"stale generation", 1, "stale generation"}, {"replayed nonce", 2, "replayed nonce"}} {
		t.Run(testCase.name, func(t *testing.T) {
			clientID, serverID := NodeID{31}, NodeID{32}
			clientTLS, clientSigner := testTLSIdentity(t, clientID)
			serverTLS, serverSigner := testTLSIdentity(t, serverID)
			trustTLS13Peers(t, clientTLS, serverTLS)
			clientRaw, serverRaw := net.Pipe()
			client, server := tls.Client(clientRaw, clientTLS), tls.Server(serverRaw, serverTLS)
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			knownNonce := HelloNonce{9}
			accepted := map[HelloNonce]bool{knownNonce: true}
			serverVerifier := func(_ context.Context, _ *x509.Certificate, message HelloMessage) error {
				if message.LocalGeneration < 2 {
					return errors.New("stale generation")
				}
				if accepted[message.LocalNonce] {
					return errors.New("replayed nonce")
				}
				accepted[message.LocalNonce] = true
				return nil
			}
			serverResult := make(chan error, 1)
			go func() {
				_, err := HandshakeHello(ctx, server, HelloConfig{NodeID: serverID, BootID: BootID{2}, Generation: 1, Signer: serverSigner, VerifyPeer: serverVerifier})
				serverResult <- err
			}()
			_, clientErr := HandshakeHello(ctx, client, HelloConfig{Client: true, NodeID: clientID, BootID: BootID{1}, Generation: testCase.generation, Signer: clientSigner, NewNonce: func() (HelloNonce, error) { return knownNonce, nil }, VerifyPeer: func(context.Context, *x509.Certificate, HelloMessage) error { return nil }})
			if clientErr != nil {
				t.Fatalf("client handshake: %v", clientErr)
			}
			if err := <-serverResult; err == nil || err.Error() != testCase.want {
				t.Fatalf("server freshness result = %v", err)
			}
			_ = clientRaw.Close()
			_ = serverRaw.Close()
		})
	}
}

func helloPair() (HelloMessage, HelloMessage) {
	client := HelloMessage{Phase: HelloResponse, Version: ProtocolVersion, Role: 0, LocalNodeID: NodeID{1}, LocalBootID: BootID{2}, LocalGeneration: 3, LocalNonce: HelloNonce{4}, LocalCapabilities: []uint16{1}}
	server := HelloMessage{Phase: HelloResponse, Version: ProtocolVersion, Role: 1, LocalNodeID: NodeID{5}, LocalBootID: BootID{6}, LocalGeneration: 7, LocalNonce: HelloNonce{8}, LocalCapabilities: []uint16{2}}
	client.PeerNodeID, client.PeerBootID, client.PeerGeneration, client.PeerNonce = server.LocalNodeID, server.LocalBootID, server.LocalGeneration, server.LocalNonce
	server.PeerNodeID, server.PeerBootID, server.PeerGeneration, server.PeerNonce = client.LocalNodeID, client.LocalBootID, client.LocalGeneration, client.LocalNonce
	return client, server
}

func testTLSIdentity(t testing.TB, id NodeID) (*tls.Config, *ecdsa.PrivateKey) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	serial, _ := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 120))
	uri, _ := url.Parse("urn:vortex:node:" + id.String())
	template := &x509.Certificate{SerialNumber: serial, Subject: pkix.Name{CommonName: id.String()}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour), KeyUsage: x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth}, BasicConstraintsValid: true, IsCA: true, URIs: []*url.URL{uri}, DNSNames: []string{"node.vortex.test"}}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	cert := tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}
	return &tls.Config{MinVersion: tls.VersionTLS13, MaxVersion: tls.VersionTLS13, Certificates: []tls.Certificate{cert}}, key
}

func trustTLS13Peers(t testing.TB, client, server *tls.Config) {
	t.Helper()
	clientCert, err := x509.ParseCertificate(server.Certificates[0].Certificate[0])
	if err != nil {
		t.Fatal(err)
	}
	serverCert, err := x509.ParseCertificate(client.Certificates[0].Certificate[0])
	if err != nil {
		t.Fatal(err)
	}
	client.RootCAs = x509.NewCertPool()
	client.RootCAs.AddCert(clientCert)
	client.ServerName = "node.vortex.test"
	server.ClientCAs = x509.NewCertPool()
	server.ClientCAs.AddCert(serverCert)
	server.ClientAuth = tls.RequireAndVerifyClientCert
}
