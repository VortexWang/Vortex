package service_test

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"net"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"vortex/agent"
	"vortex/agent/store"
	"vortex/backend/internal/auth"
	"vortex/backend/internal/control"
	"vortex/backend/internal/policy"
	"vortex/backend/internal/worker"
	controlv1 "vortex/contracts/protobuf/v1"
)

func TestShareHandoffRealMTLSAgentAndPostgresWorker(t *testing.T) {
	x := newShareFixture(t)
	f := x.f
	st, e := store.Open(filepath.Join(t.TempDir(), "agent.db"), "agent")
	if e != nil {
		t.Fatal(e)
	}
	defer st.Close()
	node, e := agent.New(x.in.NodeID, st, nil)
	if e != nil {
		t.Fatal(e)
	}
	x.in.BootID = node.BootID
	if e = f.ports.RegisterNodeSession(f.contexts[x.in.NodeID], policyScope(t, f.tenant, auth.PlatformAdmin, policy.Mutate), x.in.NodeID, node.BootID, 2, uuid.NewString()); e != nil {
		t.Fatal(e)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	roots := x509.NewCertPool()
	roots.AddCert(f.ports.NodeCA)
	backend := portCertificate(t, f.ports.NodeCA, f.caKey, shareBackendID, x509.ExtKeyUsageServerAuth)
	client := portCertificate(t, f.ports.NodeCA, f.caKey, x.in.NodeID, x509.ExtKeyUsageClientAuth)
	ready := make(chan struct{})
	var once sync.Once
	adapter := &control.Adapter{Shares: &x.s, Observe: func(context.Context, string, string, []byte) error { once.Do(func() { close(ready) }); return nil }}
	server := grpc.NewServer(grpc.Creds(credentials.NewTLS(&tls.Config{Certificates: []tls.Certificate{backend}, ClientCAs: roots, ClientAuth: tls.RequireAndVerifyClientCert, MinVersion: tls.VersionTLS13, MaxVersion: tls.VersionTLS13})))
	controlv1.RegisterNodeControlServer(server, adapter)
	listener, e := net.Listen("tcp", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	defer listener.Close()
	defer server.Stop()
	go server.Serve(listener)
	agentClient := &agent.ControlClient{Node: node, TLS: &tls.Config{Certificates: []tls.Certificate{client}, RootCAs: roots, ServerName: shareBackendID, MinVersion: tls.VersionTLS13, MaxVersion: tls.VersionTLS13}}
	ended := make(chan error, 1)
	go func() { ended <- agentClient.Connect(ctx, listener.Addr().String()) }()
	select {
	case <-ready:
	case e := <-ended:
		t.Fatal(e)
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	// Fresh backend UTC follows the boot challenge, as in production issuance.
	x.now = time.Now().UTC().Truncate(time.Microsecond)
	p := x.prepare(t)
	if e = x.s.QueueHandoff(ctx, f.scope, p.Generation.ID, uuid.NewString()); e != nil {
		t.Fatal(e)
	}
	w := worker.ShareReconciliationWorker{Service: x.s, Adapter: adapter, DeliveryTimeout: 2 * time.Second}
	worked, e := w.Once(ctx)
	if e != nil || !worked {
		t.Fatal(e, worked)
	}
	if c := shareChain(t, x); c.ActiveGeneration != 1 || c.PendingGeneration != 0 {
		select {
		case e := <-ended:
			t.Fatal("agent ended", e)
		default:
		}
		t.Fatal("real control path not confirmed", c)
	}
	cancel()
	select {
	case <-ended:
	case <-time.After(3 * time.Second):
		t.Fatal("Agent stream did not close")
	}
}
