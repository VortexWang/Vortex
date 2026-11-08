package service_test

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"net"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"vortex/agent/store"
	"vortex/backend/db/generated"
	"vortex/backend/internal/control"
	controlv1 "vortex/contracts/protobuf/v1"
	"vortex/hub"
)

func TestHubEffectiveEpochActualMTLSIssuesLeaseSealLostReceipts(t *testing.T) {
	x, hs, a, r := effectiveAllocationFixture(t, true)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	issueAllocationFixture(t, x, hs, a, 10)
	now := x.now.Add(62 * time.Second)
	for _, seconds := range []int{20, 40, 60, 62} {
		if e := hs.RecordBackendHeartbeat(ctx, x.now.Add(time.Duration(seconds)*time.Second), a.AutonomyDeadline); e != nil {
			t.Fatal(e)
		}
	}
	if lease, e := hs.RefreshHubDelegationLeases(ctx, r.HubID, a.NodeID, a.BootID, a.DelegationID, store.IssuerTime{Earliest: now, Latest: now, Trusted: true}); e != nil || len(lease.Payload) == 0 {
		t.Fatal(lease, e)
	}
	if _, e := hs.SealHubDelegation(ctx, r.HubID, r.ToBootID, a.DelegationID); e != nil {
		t.Fatal(e)
	}
	backendCA, backendKey := portCA(t)
	hubCA, hubKey := portCA(t)
	backendRoots, hubRoots := x509.NewCertPool(), x509.NewCertPool()
	backendRoots.AddCert(backendCA)
	hubRoots.AddCert(hubCA)
	x.s.SignerCA = hubCA
	start := time.Now()
	clock := func() store.IssuerTime {
		current := now.Add(time.Second).Add(time.Since(start))
		return store.IssuerTime{Earliest: current, Latest: current, Trusted: true}
	}
	x.s.Clock = func() (time.Time, time.Time, bool) { c := clock(); return c.Earliest, c.Latest, c.Trusted }
	serverTLS := &tls.Config{Certificates: []tls.Certificate{portCertificate(t, backendCA, backendKey, shareBackendID, x509.ExtKeyUsageServerAuth)}, ClientCAs: hubRoots, ClientAuth: tls.RequireAndVerifyClientCert, MinVersion: tls.VersionTLS13, MaxVersion: tls.VersionTLS13}
	clientTLS := &tls.Config{Certificates: []tls.Certificate{portCertificate(t, hubCA, hubKey, r.HubID, x509.ExtKeyUsageClientAuth)}, RootCAs: backendRoots, ServerName: shareBackendID, MinVersion: tls.VersionTLS13, MaxVersion: tls.VersionTLS13}
	l, e := net.Listen("tcp", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	defer l.Close()
	server := grpc.NewServer(grpc.Creds(credentials.NewTLS(serverTLS)))
	defer server.Stop()
	adapter := &loseSealReceipts{HubAdapter: &control.HubAdapter{BackendID: shareBackendID, HubID: r.HubID, Region: a.Region, Shares: &x.s, Observe: func(context.Context, string, string, []byte) error { return nil }}}
	adapter.before.Store(true)
	adapter.after.Store(true)
	controlv1.RegisterHubControlServer(server, adapter)
	go server.Serve(l)
	cfg := &hub.BackendClient{HubID: r.HubID, BootID: r.ToBootID, BackendID: shareBackendID, Region: a.Region, Address: l.Addr().String(), TLS: clientTLS, Store: hs, Clock: clock}
	for attempt := 0; attempt < 2; attempt++ {
		if e = cfg.Run(ctx); e == nil {
			t.Fatal("injected seal loss did not disconnect", attempt)
		}
		t.Log("actual reconnect", attempt, e)
	}
	if adapter.before.Load() || adapter.after.Load() {
		t.Fatal("did not traverse both FULL receipt and PG ACK loss")
	}
	run, stop := context.WithCancel(ctx)
	defer stop()
	done := make(chan error, 1)
	go func() { done <- cfg.Run(run) }()
	waitHub(t, ctx, func() bool {
		rows, e := hs.PendingLedger(ctx, 1000)
		return e == nil && len(rows) == 0
	})
	stop()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("actual epoch transport did not stop")
	}
	q := generated.New(x.f.pool)
	issued, e := q.HubAllocationIssued(ctx, generated.HubAllocationIssuedParams{TenantID: x.f.tenant, AllocationID: a.AllocationID})
	if e != nil || issued != 47 {
		t.Fatal("transport created another financial partition", issued, e)
	}
	sealed, e := q.GetHubDelegationSeal(ctx, generated.GetHubDelegationSealParams{TenantID: x.f.tenant, AllocationID: a.AllocationID})
	if e != nil || sealed.HubBootID != r.ToBootID {
		t.Fatal("transport pinned original boot after admission", sealed, e)
	}
	accounting, e := q.ReservationAccounting(ctx, generated.ReservationAccountingParams{TenantID: x.f.tenant, GrantID: a.SourceID})
	if e != nil || accounting.ReturnedRaw != 56 || accounting.Terminal {
		t.Fatal("transport released issued debt", accounting, e)
	}
	var leases int
	if e = x.f.pool.QueryRow(ctx, "SELECT count(*) FROM hub_runtime_lease_receipts WHERE tenant_id=$1 AND allocation_id=$2 AND hub_boot_id=$3", x.f.tenant, a.AllocationID, r.ToBootID).Scan(&leases); e != nil || leases != 1 {
		t.Fatal("new-epoch lease not registered once", leases, e)
	}
}
