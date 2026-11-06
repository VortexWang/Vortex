package agent

import (
	"context"
	"crypto/ecdsa"
	"crypto/x509"
	"net"
	"path/filepath"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"vortex/agent/identity"
	"vortex/agent/store"
	controlv1 "vortex/contracts/protobuf/v1"
)

type identityRevocationServer struct {
	controlv1.UnimplementedNodeControlServer
	update *controlv1.CredentialRevocations
}

func (s *identityRevocationServer) Connect(stream grpc.BidiStreamingServer[controlv1.NodeMessage, controlv1.ControlMessage]) error {
	if _, err := stream.Recv(); err != nil {
		return err
	}
	if err := stream.Send(&controlv1.ControlMessage{MessageId: "550e8400-e29b-41d4-a716-446655440201", Sequence: 1, Body: &controlv1.ControlMessage_CredentialRevocations{CredentialRevocations: s.update}}); err != nil {
		return err
	}
	<-stream.Context().Done()
	return stream.Context().Err()
}

func TestRuntimeIdentityAuthenticatedControlRevocationStopsReconnectAndRenew(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	serverTLS, clientTLS, roots := controlTLSWithHub(t, runtimeIdentityNode, regionalHubID)
	pair := clientTLS.Certificates[0]
	leaf, err := x509.ParseCertificate(pair.Certificate[0])
	if err != nil {
		t.Fatal(err)
	}
	credential, err := identity.CredentialFromTLS(runtimeIdentityNode, pair, leaf, pair.PrivateKey.(*ecdsa.PrivateKey))
	if err != nil {
		t.Fatal(err)
	}
	db, err := store.Open(filepath.Join(t.TempDir(), "agent.db"), "agent")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	view, err := LoadCredentialRevocations(ctx, db, func() (time.Time, time.Time, bool) { now := time.Now(); return now, now, true })
	if err != nil {
		t.Fatal(err)
	}
	manager, err := NewCredentialManager(credential, roots, regionalHubID, view.IsRevoked)
	if err != nil {
		t.Fatal(err)
	}
	n, err := New(runtimeIdentityNode, db, NoopEngine{})
	if err != nil {
		t.Fatal(err)
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	server := grpc.NewServer(grpc.Creds(credentials.NewTLS(serverTLS)))
	controlv1.RegisterNodeControlServer(server, &identityRevocationServer{update: &controlv1.CredentialRevocations{IssuerId: regionalHubID, Revision: 1, CertificateSerials: []string{leaf.SerialNumber.String()}, EffectiveAt: time.Now().Add(-time.Second).UTC().Format(time.RFC3339Nano)}})
	go server.Serve(ln)
	defer server.Stop()
	client := &ControlClient{Node: n, TLSProvider: manager.TLSConfig, CredentialRevocations: view}
	err = client.Connect(ctx, ln.Addr().String())
	if err == nil || ctx.Err() != nil {
		t.Fatalf("revoked live stream did not terminate promptly: %v", err)
	}
	if !view.IsRevoked(leaf) {
		t.Fatal("authenticated body was not applied")
	}
	if _, err = manager.TLSConfig(); err == nil {
		t.Fatal("revoked node can reconnect")
	}
	if _, err = manager.RuntimeIdentity(); err == nil {
		t.Fatal("revoked node can handshake VLink")
	}
	if err = manager.Renew(ctx, "127.0.0.1:1", t.TempDir(), db, time.Minute); err == nil {
		t.Fatal("revoked node can renew")
	}
	batches, err := db.CredentialRevocations(ctx)
	if err != nil || len(batches) != 1 {
		t.Fatalf("revocation not durable: %v", err)
	}
}
