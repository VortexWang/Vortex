package main

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"vortex/agent/identity"
	controlv1 "vortex/contracts/protobuf/v1"
)

type splitControlServer struct {
	controlv1.UnimplementedNodeControlServer
	hub, region string
	connected   chan struct{}
	active      atomic.Int32
	attempts    atomic.Int32
}

func (s *splitControlServer) Connect(stream grpc.BidiStreamingServer[controlv1.NodeMessage, controlv1.ControlMessage]) error {
	first, e := stream.Recv()
	if e != nil {
		return e
	}
	if first.GetHeartbeat() == nil {
		return io.ErrUnexpectedEOF
	}
	s.attempts.Add(1)
	s.active.Add(1)
	defer s.active.Add(-1)
	if s.hub == "" {
		found := false
		for _, cap := range first.GetHeartbeat().Capabilities {
			found = found || cap == "source-provision-v1"
		}
		if !found {
			return io.ErrUnexpectedEOF
		}
	} else {
		for _, cap := range first.GetHeartbeat().Capabilities {
			if cap == "source-provision-v1" {
				return io.ErrUnexpectedEOF
			}
		}
		hello, e := stream.Recv()
		if e != nil {
			return e
		}
		if hello.GetLifecycleHello() == nil {
			return io.ErrUnexpectedEOF
		}
		now := time.Now().UTC()
		if e = stream.Send(&controlv1.ControlMessage{MessageId: uuid.NewString(), Sequence: 1, Body: &controlv1.ControlMessage_HubStatus{HubStatus: &controlv1.HubStatus{HubId: s.hub, RegionId: s.region, SessionId: hello.GetLifecycleHello().SessionId, ControlVersion: "1.3.0", Capabilities: []string{"status-v1"}, Mode: "online", SignerEpoch: 7, BackendHeartbeatAt: now.Format(time.RFC3339Nano), AutonomyDeadline: now.Add(time.Hour).Format(time.RFC3339Nano)}}}); e != nil {
			return e
		}
	}
	select {
	case s.connected <- struct{}{}:
	default:
	}
	if s.hub != "" && s.attempts.Load() == 1 {
		return nil
	} // force Hub reconnect only
	for {
		m, e := stream.Recv()
		if e != nil {
			return e
		}
		if s.hub == "" && (m.GetLifecycleHello() != nil || m.GetUsageBatch() != nil || m.GetUnknownUsageBatch() != nil) {
			return io.ErrUnexpectedEOF
		}
	}
}

func TestRunNodeSplitBackendAndHubActualMTLSIndependentSessions(t *testing.T) {
	dir := t.TempDir()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	nodeID, backendID, hubID := uuid.NewString(), uuid.NewString(), uuid.NewString()
	nodeCA, nodeDER, e := identity.NewCA(time.Now())
	if e != nil {
		t.Fatal(e)
	}
	sign := func(ca *identity.CA, der []byte, id string) identity.Credential {
		t.Helper()
		key, csr, e := identity.GenerateCSR(id)
		if e != nil {
			t.Fatal(e)
		}
		cred, e := ca.SignCSR(csr, id, time.Now(), time.Hour)
		if e != nil {
			t.Fatal(e)
		}
		cred, e = identity.AttachKey(cred, key, der)
		if e != nil {
			t.Fatal(e)
		}
		return cred
	}
	client := sign(nodeCA, nodeDER, nodeID)
	credentialDir := filepath.Join(dir, "credentials")
	if e = identity.SaveFiles(credentialDir, client, nodeDER); e != nil {
		t.Fatal(e)
	}
	start := func(id string, handler *splitControlServer) (string, string) {
		t.Helper()
		ca, der, e := identity.NewCA(time.Now())
		if e != nil {
			t.Fatal(e)
		}
		cred := sign(ca, der, id)
		roots := x509.NewCertPool()
		roots.AppendCertsFromPEM(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: nodeDER}))
		server := grpc.NewServer(grpc.Creds(credentials.NewTLS(&tls.Config{Certificates: []tls.Certificate{cred.Certificate}, ClientCAs: roots, ClientAuth: tls.RequireAndVerifyClientCert, MinVersion: tls.VersionTLS13, MaxVersion: tls.VersionTLS13})))
		controlv1.RegisterNodeControlServer(server, handler)
		ln, e := net.Listen("tcp", "127.0.0.1:0")
		if e != nil {
			t.Fatal(e)
		}
		t.Cleanup(func() { server.Stop(); ln.Close() })
		go server.Serve(ln)
		caPath := filepath.Join(dir, id+"-ca.pem")
		if e = os.WriteFile(caPath, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0600); e != nil {
			t.Fatal(e)
		}
		return ln.Addr().String(), caPath
	}
	backend := &splitControlServer{connected: make(chan struct{}, 4)}
	runtime := &splitControlServer{hub: hubID, region: "phase6", connected: make(chan struct{}, 4)}
	backendAddr, backendCA := start(backendID, backend)
	hubAddr, hubCA := start(hubID, runtime)
	args := []string{"--node-id", nodeID, "--db", filepath.Join(dir, "agent.db"), "--credential-dir", credentialDir, "--hub", hubAddr, "--hub-id", hubID, "--hub-name", hubID, "--region", "phase6", "--hub-ca", hubCA, "--enrollment-hub", hubAddr, "--renew-before", "1m", "--backend", backendAddr, "--backend-ca", backendCA, "--source-backend-id", backendID}
	done := make(chan error, 1)
	run, stop := context.WithCancel(ctx)
	defer stop()
	go func() { done <- runNode(run, args, io.Discard, io.Discard) }()
	for _, ready := range []chan struct{}{backend.connected, runtime.connected, runtime.connected} {
		select {
		case <-ready:
		case err := <-done:
			t.Fatalf("node exited before dual readiness: %v", err)
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		}
	}
	if backend.attempts.Load() != 1 || backend.active.Load() != 1 || runtime.attempts.Load() < 2 {
		t.Fatal("Hub reconnect cancelled direct backend", backend.attempts.Load(), backend.active.Load(), runtime.attempts.Load())
	}
	stop()
	select {
	case e = <-done:
		if e != nil {
			t.Fatal(e)
		}
	case <-ctx.Done():
		t.Fatal("split workers did not join", ctx.Err())
	}
}

func TestRunNodeRejectsIncompleteDirectBackendBeforeOpeningStore(t *testing.T) {
	path := filepath.Join(t.TempDir(), "unopened.db")
	e := runNode(context.Background(), []string{"--db", path, "--source-backend-id", uuid.NewString()}, io.Discard, io.Discard)
	if e == nil || !strings.Contains(e.Error(), "direct backend requires") {
		t.Fatal(e)
	}
	if _, e = os.Stat(path); !os.IsNotExist(e) {
		t.Fatal("invalid role configuration opened SQLite", e)
	}
}
