package service_test

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"vortex/agent"
	"vortex/agent/store"
	"vortex/backend/db/generated"
	"vortex/backend/internal/control"
	"vortex/backend/internal/worker"
	controlv1 "vortex/contracts/protobuf/v1"
)

type loseSourceReceipt struct {
	*control.Adapter
	once sync.Once
}
type sourceStream struct {
	grpc.BidiStreamingServer[controlv1.NodeMessage, controlv1.ControlMessage]
	owner *loseSourceReceipt
}

func (s *sourceStream) Recv() (*controlv1.NodeMessage, error) {
	m, e := s.BidiStreamingServer.Recv()
	if e == nil && m.GetSourceProvisionReceipt() != nil {
		drop := false
		s.owner.once.Do(func() { drop = true })
		if drop {
			return nil, errors.New("injected Node FULL source receipt loss")
		}
	}
	return m, e
}
func (s *loseSourceReceipt) Connect(stream grpc.BidiStreamingServer[controlv1.NodeMessage, controlv1.ControlMessage]) error {
	return s.Adapter.Connect(&sourceStream{stream, s})
}

func TestNodeSourceProvisionActualMTLSWorkerLostReceiptAndExplicitBackend(t *testing.T) {
	x, in, _ := fundedHubFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	st, e := store.Open(t.TempDir()+"/agent.db", "agent")
	if e != nil {
		t.Fatal(e)
	}
	defer st.Close()
	node, e := agent.New(in.NodeID, st, nil)
	if e != nil {
		t.Fatal(e)
	}
	// Reuse the fixture's already authenticated/pinned Node incarnation. This
	// adapter test does not claim an end-to-end production enrollment/reboot.
	node.BootID = in.BootID
	x.now = time.Now().UTC().Truncate(time.Microsecond)
	a, e := x.s.PrepareHubAllocation(ctx, x.f.scope, in, uuid.NewString())
	if e != nil {
		t.Fatal(e)
	}
	backendCA, backendKey := portCA(t)
	backendRoots, nodeRoots := x509.NewCertPool(), x509.NewCertPool()
	backendRoots.AddCert(backendCA)
	nodeRoots.AddCert(x.s.NodeCA)
	serverTLS := &tls.Config{Certificates: []tls.Certificate{portCertificate(t, backendCA, backendKey, shareBackendID, x509.ExtKeyUsageServerAuth)}, ClientCAs: nodeRoots, ClientAuth: tls.RequireAndVerifyClientCert, MinVersion: tls.VersionTLS13, MaxVersion: tls.VersionTLS13}
	clientTLS := &tls.Config{Certificates: []tls.Certificate{portCertificate(t, x.s.NodeCA, x.f.caKey, in.NodeID, x509.ExtKeyUsageClientAuth)}, RootCAs: backendRoots, ServerName: shareBackendID, MinVersion: tls.VersionTLS13, MaxVersion: tls.VersionTLS13}
	connected := make(chan struct{}, 4)
	adapter := &control.Adapter{Shares: &x.s, Observe: func(context.Context, string, string, []byte) error { connected <- struct{}{}; return nil }}
	server := grpc.NewServer(grpc.Creds(credentials.NewTLS(serverTLS)))
	defer server.Stop()
	controlv1.RegisterNodeControlServer(server, &loseSourceReceipt{Adapter: adapter})
	listener, e := net.Listen("tcp", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	defer listener.Close()
	go server.Serve(listener)
	client := &agent.ControlClient{Node: node, TLS: clientTLS, SourceBackendID: shareBackendID}
	w := worker.NodeSourceWorker{Service: x.s, BackendID: shareBackendID, Adapter: adapter}
	done := make(chan error, 1)
	go func() { done <- client.Connect(ctx, listener.Addr().String()) }()
	<-connected
	if _, e = w.Once(ctx); e == nil {
		t.Fatal("injected lost receipt unexpectedly committed")
	}
	select {
	case <-done:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	q := generated.New(x.f.pool)
	if _, e = q.GetNodeSourceReceipt(ctx, generated.GetNodeSourceReceiptParams{TenantID: x.f.tenant, AllocationID: a.AllocationID}); e == nil {
		t.Fatal("lost PG receipt persisted")
	}
	// Local FULL success is observable by an exact no-op replay, while PG still
	// owns the unconfirmed delivery and all source-funded occupation.
	p, e := q.GetNodeSourceProvision(ctx, generated.GetNodeSourceProvisionParams{TenantID: x.f.tenant, AllocationID: a.AllocationID})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = st.ApplySourceProvision(ctx, in.NodeID, in.BootID, shareBackendID, p.Payload, store.IssuerTime{Earliest: x.now, Latest: x.now.Add(time.Second), Trusted: true}); e != nil {
		t.Fatal("FULL source roots lost", e)
	}
	runCtx, stop := context.WithCancel(ctx)
	go func() { done <- client.Connect(runCtx, listener.Addr().String()) }()
	<-connected
	if worked, e := w.Once(ctx); e != nil || !worked {
		t.Fatal("source receipt replay", e, worked)
	}
	op, e := q.GetOperation(ctx, generated.GetOperationParams{TenantID: x.f.tenant, ID: p.OperationID})
	if e != nil || op.State != "succeeded" {
		t.Fatal(e, op)
	}
	stop()
	select {
	case <-done:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	// A valid backend certificate still cannot provision an unconfigured client.
	legacy := &agent.ControlClient{Node: node, TLS: clientTLS}
	legacyCtx, legacyStop := context.WithCancel(ctx)
	go func() { done <- legacy.Connect(legacyCtx, listener.Addr().String()) }()
	<-connected
	if e = adapter.DeliverSources(ctx, x.f.scope, in.NodeID, in.BootID, &controlv1.SourceProvisionCommand{Payload: p.Payload, PayloadSha256: p.PayloadDigest}); e == nil {
		t.Fatal("unconfigured client received source command")
	}
	legacyStop()
	select {
	case <-done:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	// The same trusted role CA and valid certificate name cannot bypass the
	// configured backend UUID. Reject at TLS, before even sending a heartbeat.
	wrongTLS := clientTLS.Clone()
	var verified atomic.Int32
	wrongTLS.VerifyConnection = func(state tls.ConnectionState) error {
		if len(state.VerifiedChains) > 0 {
			verified.Add(1)
		}
		return nil
	}
	wrong := &agent.ControlClient{Node: node, TLS: wrongTLS, SourceBackendID: uuid.NewString(), ControlOnly: true}
	wrongCtx, wrongStop := context.WithTimeout(ctx, time.Second)
	defer wrongStop()
	go func() { done <- wrong.Connect(wrongCtx, listener.Addr().String()) }()
	select {
	case e = <-done:
		if e == nil || verified.Load() == 0 {
			t.Fatal("wrong backend was not rejected after verified TLS", e, verified.Load())
		}
	case <-connected:
		t.Fatal("valid CA but wrong backend reached application control stream")
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	t.Log("actual direct backend TLS, opt-in capability, atomic FULL roots, lost receipt/replay, PG task completion verified")
}
