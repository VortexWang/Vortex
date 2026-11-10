package service_test

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"vortex/agent"
	"vortex/backend/db/generated"
	"vortex/backend/internal/auth"
	"vortex/backend/internal/control"
	"vortex/backend/internal/policy"
	"vortex/backend/internal/worker"
	controlv1 "vortex/contracts/protobuf/v1"
)

type loseBootReceipt struct {
	*control.Adapter
	once sync.Once
}
type bootReceiptStream struct {
	grpc.BidiStreamingServer[controlv1.NodeMessage, controlv1.ControlMessage]
	owner *loseBootReceipt
}

func (s *bootReceiptStream) Recv() (*controlv1.NodeMessage, error) {
	m, e := s.BidiStreamingServer.Recv()
	if e == nil && m.GetNodeBootRecoveryReceipt() != nil {
		drop := false
		s.owner.once.Do(func() { drop = true })
		if drop {
			return nil, errors.New("injected Node FULL boot retirement receipt loss")
		}
	}
	return m, e
}
func (s *loseBootReceipt) Connect(stream grpc.BidiStreamingServer[controlv1.NodeMessage, controlv1.ControlMessage]) error {
	return s.Adapter.Connect(&bootReceiptStream{stream, s})
}

func TestNodeBootRecoveryActualMTLSCapabilityLostFULLReceiptReplay(t *testing.T) {
	x, in, st := nodeColdFixture(t)
	x.now = x.now.Add(time.Hour)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	p, e := x.s.PrepareNodeBootRecovery(ctx, policyScope(t, x.f.tenant, auth.PlatformAdmin, policy.Grant), in, uuid.NewString())
	if e != nil {
		t.Fatal(e)
	}
	node, e := agent.New(in.NodeID, st, nil)
	if e != nil {
		t.Fatal(e)
	}
	// Exact already authenticated incarnation is pinned by the PG proof; no
	// enrollment/Engine stop claim is made by this adapter fixture.
	node.BootID = in.ToBootID
	backendCA, backendKey := portCA(t)
	backendRoots, nodeRoots := x509.NewCertPool(), x509.NewCertPool()
	backendRoots.AddCert(backendCA)
	nodeRoots.AddCert(x.s.NodeCA)
	serverTLS := &tls.Config{Certificates: []tls.Certificate{portCertificate(t, backendCA, backendKey, shareBackendID, x509.ExtKeyUsageServerAuth)}, ClientCAs: nodeRoots, ClientAuth: tls.RequireAndVerifyClientCert, MinVersion: tls.VersionTLS13, MaxVersion: tls.VersionTLS13}
	clientTLS := &tls.Config{Certificates: []tls.Certificate{portCertificate(t, x.s.NodeCA, x.f.caKey, in.NodeID, x509.ExtKeyUsageClientAuth)}, RootCAs: backendRoots, ServerName: shareBackendID, MinVersion: tls.VersionTLS13, MaxVersion: tls.VersionTLS13}
	ready := make(chan struct{}, 8)
	adapter := &control.Adapter{Shares: &x.s, Observe: func(context.Context, string, string, []byte) error { ready <- struct{}{}; return nil }}
	server := grpc.NewServer(grpc.Creds(credentials.NewTLS(serverTLS)))
	defer server.Stop()
	controlv1.RegisterNodeControlServer(server, &loseBootReceipt{Adapter: adapter})
	listener, e := net.Listen("tcp", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	defer listener.Close()
	go server.Serve(listener)
	connect := func(pinned bool) (context.CancelFunc, <-chan error) {
		run, stop := context.WithCancel(ctx)
		client := &agent.ControlClient{Node: node, TLS: clientTLS}
		if pinned {
			client.SourceBackendID = shareBackendID
			client.ControlOnly = true
		}
		done := make(chan error, 1)
		go func() { done <- client.Connect(run, listener.Addr().String()) }()
		select {
		case <-ready:
		case e := <-done:
			t.Fatal(e)
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		}
		return stop, done
	}
	finish := func(stop context.CancelFunc, done <-chan error) {
		stop()
		select {
		case <-done:
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		}
	}
	w := worker.NodeBootRecoveryWorker{Service: x.s, BackendID: shareBackendID, Adapter: adapter}
	stop, done := connect(false)
	if _, e = w.Once(ctx); e == nil {
		t.Fatal("unconfigured backend capability accepted")
	}
	finish(stop, done)
	stop, done = connect(true)
	if _, e = w.Once(ctx); e == nil {
		t.Fatal("lost FULL receipt committed")
	}
	finish(stop, done)
	q := generated.New(x.f.pool)
	if _, e = q.GetNodeBootRecoveryReceipt(ctx, generated.GetNodeBootRecoveryReceiptParams{TenantID: x.f.tenant, RecoveryID: p.ID}); e == nil {
		t.Fatal("missing receipt became PG proof")
	}
	for _, kind := range []string{"rate", "connections"} {
		c, e := q.ReadShareChain(ctx, generated.ReadShareChainParams{TenantID: x.f.tenant, NodeID: in.NodeID, Kind: kind})
		if e != nil || c.BootID != in.FromBootID || c.ActiveGeneration == 0 {
			t.Fatal("lost receipt migrated PG", c, e)
		}
	}
	stop, done = connect(true)
	if worked, e := w.Once(ctx); e != nil || !worked {
		t.Fatal("exact FULL replay", worked, e)
	}
	if worked, e := w.Once(ctx); e != nil || worked {
		t.Fatal("accepted recovery remained pending", worked, e)
	}
	finish(stop, done)
	for _, kind := range []string{"rate", "connections"} {
		c, e := q.ReadShareChain(ctx, generated.ReadShareChainParams{TenantID: x.f.tenant, NodeID: in.NodeID, Kind: kind})
		if e != nil || c.BootID != in.ToBootID || c.ActiveGeneration != 0 || c.RetiredGeneration == 0 || c.HeldAmount == 0 {
			t.Fatal("wrong PG retirement", c, e)
		}
	}
	op, e := q.GetOperation(ctx, generated.GetOperationParams{TenantID: x.f.tenant, ID: p.OperationID})
	if e != nil || op.State != "succeeded" {
		t.Fatal(op, e)
	}
	t.Log("actual role-mTLS independent capability, lost SQLite FULL receipt, exact reconnect replay and zero-capacity PG retired floors")
}
