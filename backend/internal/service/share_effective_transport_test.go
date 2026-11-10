package service_test

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"github.com/google/uuid"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"net"
	"sync/atomic"
	"testing"
	"time"
	"vortex/agent/store"
	"vortex/backend/db/generated"
	"vortex/backend/internal/control"
	controlv1 "vortex/contracts/protobuf/v1"
	"vortex/hub"
)

func TestRecoveredShareClosureActualMTLSFULLLossAndPGAckReplay(t *testing.T) {
	x, hs, a, r := effectiveAllocationFixture(t, true)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	x.in.SignerEpoch = r.ToEpoch
	next := prepareClosureSuccessor(t, x)
	if e := x.s.PrepareRenewalClosures(ctx, x.f.scope, next.Generation.ID, shareBackendID, uuid.NewString()); e != nil {
		t.Fatal(e)
	}
	backendCA, backendKey := portCA(t)
	hubCA, hubKey := portCA(t)
	backendRoots, hubRoots := x509.NewCertPool(), x509.NewCertPool()
	backendRoots.AddCert(backendCA)
	hubRoots.AddCert(hubCA)
	serverTLS := &tls.Config{Certificates: []tls.Certificate{portCertificate(t, backendCA, backendKey, shareBackendID, x509.ExtKeyUsageServerAuth)}, ClientCAs: hubRoots, ClientAuth: tls.RequireAndVerifyClientCert, MinVersion: tls.VersionTLS13, MaxVersion: tls.VersionTLS13}
	clientTLS := &tls.Config{Certificates: []tls.Certificate{portCertificate(t, hubCA, hubKey, r.HubID, x509.ExtKeyUsageClientAuth)}, RootCAs: backendRoots, ServerName: shareBackendID, MinVersion: tls.VersionTLS13, MaxVersion: tls.VersionTLS13}
	x.s.SignerCA = hubCA
	start := time.Now()
	base := x.now.Add(time.Second)
	clock := func() store.IssuerTime {
		now := base.Add(time.Since(start))
		return store.IssuerTime{Earliest: now, Latest: now, Trusted: true}
	}
	x.s.Clock = func() (time.Time, time.Time, bool) { now := clock(); return now.Earliest, now.Latest, now.Trusted }
	listener, e := net.Listen("tcp", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	server := grpc.NewServer(grpc.Creds(credentials.NewTLS(serverTLS)))
	defer server.Stop()
	var observations atomic.Int32
	adapter := &loseClosureReceipts{HubAdapter: &control.HubAdapter{BackendID: shareBackendID, HubID: r.HubID, Region: a.Region, Shares: &x.s, Observe: func(context.Context, string, string, []byte) error { observations.Add(1); return nil }}}
	controlv1.RegisterHubControlServer(server, adapter)
	go server.Serve(listener)
	adapter.before.Store(true)
	adapter.after.Store(true)
	cfg := &hub.BackendClient{HubID: r.HubID, BootID: r.ToBootID, BackendID: shareBackendID, Region: a.Region, Address: listener.Addr().String(), TLS: clientTLS, Store: hs, Clock: clock}
	tables := []string{"share_renewal_authorities", "share_generations", "share_chains", "reservations", "settlements", "ledger_events", "ledger_projections", "hub_funded_allocations"}
	before := authoritySnapshot(t, x.f.pool, x.f.tenant, tables)
	if e = cfg.Run(ctx); e == nil {
		t.Fatal("lost FULL receipt acknowledged")
	}
	t.Log("first disconnect", e)
	if adapter.before.Load() {
		t.Fatal("did not reach FULL closure", e)
	}
	q := generated.New(x.f.pool)
	commands, e := q.ShareClosureCommands(ctx, generated.ShareClosureCommandsParams{TenantID: x.f.tenant, HandoffID: next.Generation.ID, SignerID: r.HubID})
	if e != nil || len(commands) != 1 {
		t.Fatal(commands, e)
	}
	if _, e = q.GetShareClosureReceipt(ctx, generated.GetShareClosureReceiptParams{TenantID: x.f.tenant, CommandID: commands[0].ID}); e == nil {
		t.Fatal("lost FULL became PG proof")
	}
	if e = x.s.QueueHandoff(ctx, x.f.scope, next.Generation.ID, uuid.NewString()); e == nil {
		t.Fatal("missing PG closure authorized CAS")
	}
	if _, e = hs.IssueHubAuthorization(ctx, store.IssueRequest{ID: uuid.NewString(), HubID: r.HubID, NodeID: a.NodeID, BootID: a.BootID, DelegationID: a.DelegationID, RawBytes: 1, TTL: time.Minute}, cfg.Clock()); e == nil {
		t.Fatal("FULL closed recovered delegation issued")
	}
	if e = cfg.Run(ctx); e == nil {
		t.Fatal("lost PG ACK acknowledged")
	}
	if _, e = q.GetShareClosureReceipt(ctx, generated.GetShareClosureReceiptParams{TenantID: x.f.tenant, CommandID: commands[0].ID}); e != nil {
		t.Fatal("PG did not commit before lost ACK", e)
	}
	prior := observations.Load()
	runCtx, stop := context.WithCancel(ctx)
	done := make(chan error, 1)
	go func() { done <- cfg.Run(runCtx) }()
	waitHub(t, ctx, func() bool { return observations.Load() > prior })
	stop()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("reconnect did not stop")
	}
	if !bytes.Equal(before, authoritySnapshot(t, x.f.pool, x.f.tenant, tables)) {
		t.Fatal("closure loss/replay changed original proof/funds/capacity")
	}
	if e = x.s.QueueHandoff(ctx, x.f.scope, next.Generation.ID, uuid.NewString()); e != nil {
		t.Fatal("authenticated effective closure did not permit CAS", e)
	}
}
