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
	"vortex/agent/store"
	"vortex/backend/db/generated"
	"vortex/backend/internal/control"
	"vortex/backend/internal/service"
	"vortex/backend/internal/worker"
	"vortex/contracts"
	controlv1 "vortex/contracts/protobuf/v1"
)

type loseCreditFinal struct {
	*control.Adapter
	once sync.Once
}

type creditCloseStream struct {
	grpc.BidiStreamingServer[controlv1.NodeMessage, controlv1.ControlMessage]
	owner *loseCreditFinal
}

func (s *creditCloseStream) Recv() (*controlv1.NodeMessage, error) {
	m, err := s.BidiStreamingServer.Recv()
	if err == nil && m.GetCreditCloseReceipt() != nil {
		r, e := contracts.DecodeCreditCloseReceipt(m.GetCreditCloseReceipt().Payload)
		if e == nil && r.State == "final" {
			drop := false
			s.owner.once.Do(func() { drop = true })
			if drop {
				return nil, errors.New("injected FULL credit final receipt loss")
			}
		}
	}
	return m, err
}

func (s *loseCreditFinal) Connect(stream grpc.BidiStreamingServer[controlv1.NodeMessage, controlv1.ControlMessage]) error {
	return s.Adapter.Connect(&creditCloseStream{stream, s})
}

func TestCreditCloseActualMTLSWorkerPendingLostFinalAndPGInventory(t *testing.T) {
	f := newCreditCloseFixture(t)
	x := f.x
	batch := f.commitLocalUsage(t, 37)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	facts, err := agent.InspectAuthorizationUpdate(f.issued.Payload, f.in.NodeID, f.in.BootID, f.a.PathGeneration)
	if err != nil {
		t.Fatal(err)
	}
	when := facts.IssuedAt.Add(time.Millisecond)
	u := store.UsageReservation{AuthorizationID: f.grant, NodeID: f.in.NodeID, BootID: f.in.BootID, BatchID: uuid.NewString(), SnapshotID: f.a.SnapshotID, RuleID: f.a.RuleID, FlowKey: "pending-wire-ticket", Direction: "egress", Reserved: 1}
	_, err = f.st.ReserveAuthorizedUsage(ctx, u, store.AuthorizationUse{NodeID: f.in.NodeID, BootID: f.in.BootID, AuthorizationID: f.grant, Kind: "credit_grant", RuleID: f.a.RuleID, RuleRevision: f.a.RuleRevision, PathGeneration: f.a.PathGeneration, BillingSnapshotID: f.a.SnapshotID, Earliest: when, Latest: when, TrustedTime: true})
	if err != nil {
		t.Fatal(err)
	}
	f.seal(t)
	backendCA, backendKey := portCA(t)
	backendRoots, nodeRoots := x509.NewCertPool(), x509.NewCertPool()
	backendRoots.AddCert(backendCA)
	nodeRoots.AddCert(x.s.NodeCA)
	serverTLS := &tls.Config{Certificates: []tls.Certificate{portCertificate(t, backendCA, backendKey, shareBackendID, x509.ExtKeyUsageServerAuth)}, ClientCAs: nodeRoots, ClientAuth: tls.RequireAndVerifyClientCert, MinVersion: tls.VersionTLS13, MaxVersion: tls.VersionTLS13}
	clientTLS := &tls.Config{Certificates: []tls.Certificate{portCertificate(t, x.s.NodeCA, x.f.caKey, f.in.NodeID, x509.ExtKeyUsageClientAuth)}, RootCAs: backendRoots, ServerName: shareBackendID, MinVersion: tls.VersionTLS13, MaxVersion: tls.VersionTLS13}
	ready := make(chan struct{}, 4)
	adapter := &control.Adapter{Shares: &x.s, Observe: func(context.Context, string, string, []byte) error { ready <- struct{}{}; return nil }}
	server := grpc.NewServer(grpc.Creds(credentials.NewTLS(serverTLS)))
	defer server.Stop()
	controlv1.RegisterNodeControlServer(server, &loseCreditFinal{Adapter: adapter})
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	go server.Serve(listener)
	connect := func(pinned bool) (context.CancelFunc, <-chan error) {
		t.Helper()
		run, stop := context.WithCancel(ctx)
		client := &agent.ControlClient{Node: f.node, TLS: clientTLS}
		if pinned {
			client.SourceBackendID = shareBackendID
			client.ControlOnly = true
		}
		done := make(chan error, 1)
		go func() { done <- client.Connect(run, listener.Addr().String()) }()
		select {
		case <-ready:
		case err := <-done:
			t.Fatal(err)
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		}
		return stop, done
	}
	finish := func(stop context.CancelFunc, done <-chan error) {
		t.Helper()
		stop()
		select {
		case <-done:
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		}
	}
	w := worker.CreditCloseWorker{Service: x.s, BackendID: shareBackendID, Adapter: adapter}
	stop, done := connect(false)
	if _, err = w.Once(ctx); err == nil {
		t.Fatal("legacy Node received close command")
	}
	finish(stop, done)
	stop, done = connect(true)
	if _, err = w.Once(ctx); !errors.Is(err, service.ErrShareUnknown) {
		t.Fatal("writable ticket yielded final", err)
	}
	q := generated.New(x.f.pool)
	if _, err = q.GetCreditCloseReceipt(ctx, generated.GetCreditCloseReceiptParams{TenantID: x.f.tenant, GrantID: f.grant}); err == nil {
		t.Fatal("unsettled receipt persisted as final")
	}
	if err = f.st.SettleUsageReservation(ctx, u, "returned", 0, nil, 0); err != nil {
		t.Fatal(err)
	}
	if _, err = w.Once(ctx); err == nil {
		t.Fatal("lost FULL receipt committed")
	}
	finish(stop, done)
	// Node final survives reconnect, but PG still must receive its exact known batch.
	stop, done = connect(true)
	if _, err = w.Once(ctx); !errors.Is(err, service.ErrShareUnknown) {
		t.Fatal("missing PG inventory accepted", err)
	}
	if _, err = (service.MeteringService{Pool: x.f.pool}).IngestWireUsage(ctx, f.in.NodeID, f.in.BootID, 1, batch); err != nil {
		t.Fatal(err)
	}
	if worked, err := w.Once(ctx); err != nil || !worked {
		t.Fatal("PG inventory replay failed", worked, err)
	}
	if worked, err := w.Once(ctx); err != nil || worked {
		t.Fatal("accepted child remained deliverable", worked, err)
	}
	finish(stop, done)
	p, err := q.GetCreditCloseCommand(ctx, generated.GetCreditCloseCommandParams{TenantID: x.f.tenant, GrantID: f.grant})
	if err != nil {
		t.Fatal(err)
	}
	op, err := q.GetOperation(ctx, generated.GetOperationParams{TenantID: x.f.tenant, ID: p.OperationID})
	if err != nil || op.State != "succeeded" {
		t.Fatal(op, err)
	}
	projection, err := q.GetSourceProjection(ctx, generated.GetSourceProjectionParams{TenantID: x.f.tenant, SourceID: f.in.SourceID})
	if err != nil || projection.F != 18 || projection.Charged != 44 {
		t.Fatal("child transport released parent funds", projection, err)
	}
	t.Log("actual TLS1.3 capability isolation, pending stop, FULL final loss/replay and independent PG known inventory; parent hold retained")
}
