package service_test

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"net"
	"testing"
	"time"

	"github.com/google/uuid"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"vortex/agent/store"
	"vortex/backend/internal/auth"
	"vortex/backend/internal/control"
	"vortex/backend/internal/policy"
	controlv1 "vortex/contracts/protobuf/v1"
	"vortex/hub"
)

func TestHubColdRecoveryActualMTLSFreshStoreAndOldBootDenial(t *testing.T) {
	x, in, _ := fundedHubFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()
	original := x.now
	r := coldRecoveryInput(t, x, in.HubID, in.HubBootID)
	backendCA, backendKey := portCA(t)
	hubCA, hubKey := portCA(t)
	backendRoots, hubRoots := x509.NewCertPool(), x509.NewCertPool()
	backendRoots.AddCert(backendCA)
	hubRoots.AddCert(hubCA)
	x.s.SignerCA = hubCA
	l, e := net.Listen("tcp", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	defer l.Close()
	server := grpc.NewServer(grpc.Creds(credentials.NewTLS(&tls.Config{Certificates: []tls.Certificate{portCertificate(t, backendCA, backendKey, shareBackendID, x509.ExtKeyUsageServerAuth)}, ClientCAs: hubRoots, ClientAuth: tls.RequireAndVerifyClientCert, MinVersion: tls.VersionTLS13, MaxVersion: tls.VersionTLS13})))
	defer server.Stop()
	observed := make(chan struct{}, 2)
	controlv1.RegisterHubControlServer(server, &control.HubAdapter{BackendID: shareBackendID, HubID: in.HubID, Region: in.Region, Shares: &x.s, Observe: func(context.Context, string, string, []byte) error { observed <- struct{}{}; return nil }})
	go server.Serve(l)
	clientTLS := &tls.Config{Certificates: []tls.Certificate{portCertificate(t, hubCA, hubKey, in.HubID, x509.ExtKeyUsageClientAuth)}, RootCAs: backendRoots, ServerName: shareBackendID, MinVersion: tls.VersionTLS13, MaxVersion: tls.VersionTLS13}
	conn, e := grpc.NewClient(l.Addr().String(), grpc.WithTransportCredentials(credentials.NewTLS(clientTLS)))
	if e != nil {
		t.Fatal(e)
	}
	defer conn.Close()
	denied := func(boot string) {
		t.Helper()
		s, e := controlv1.NewHubControlClient(conn).Connect(ctx)
		if e != nil {
			t.Fatal(e)
		}
		defer s.CloseSend()
		if e = s.Send(&controlv1.HubMessage{HubId: in.HubID, BootId: boot, Sequence: 1, Body: &controlv1.HubMessage_LifecycleHello{LifecycleHello: &controlv1.LifecycleHello{ControlVersion: "1.6.0", Capabilities: []string{"status-v1", "backend-allocation-v1", "backend-ledger-v1", "share-renewal-close-v1", "delegation-seal-v1", "runtime-lease-refresh-v1"}, SessionId: uuid.NewString(), RegionId: in.Region}}}); e != nil {
			t.Fatal(e)
		}
		if m, e := s.Recv(); e == nil {
			t.Fatal("unproved boot received authority", m)
		}
	}
	denied(r.ToBootID) // Approval of the new epoch alone cannot recover a boot.
	x.now = original.Add(time.Hour)
	if _, e = x.s.RecoverColdHubBoot(ctx, policyScope(t, x.f.tenant, auth.PlatformAdmin, policy.Grant), r, uuid.NewString()); e != nil {
		t.Fatal(e)
	}
	denied(in.HubBootID)
	fresh, e := store.Open(t.TempDir()+"/fresh-hub.db", "hub")
	if e != nil {
		t.Fatal(e)
	}
	defer fresh.Close()
	run, stop := context.WithCancel(ctx)
	defer stop()
	done := make(chan error, 1)
	clock := func() store.IssuerTime { return store.IssuerTime{Earliest: x.now, Latest: x.now, Trusted: true} }
	go func() {
		done <- (&hub.BackendClient{HubID: in.HubID, BootID: r.ToBootID, BackendID: shareBackendID, Region: in.Region, Address: l.Addr().String(), TLS: clientTLS, Store: fresh, Clock: clock}).Run(run)
	}()
	select {
	case <-observed:
	case e := <-done:
		t.Fatal("fresh recovered transport ended", e)
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	snapshot, e := fresh.HubControlSnapshot(ctx, in.HubID, x.now)
	if e != nil || snapshot.Mode != "blocked" || snapshot.Epoch != 0 {
		t.Fatal("cold recovery invented funded runtime authority", snapshot, e)
	}
	records, e := fresh.PendingLedger(ctx, 100)
	if e != nil || len(records) != 0 {
		t.Fatal("cold recovery minted credit", records, e)
	}
	stop()
	select {
	case <-done:
	case <-ctx.Done():
		t.Fatal("recovered transport failed to stop")
	}
	t.Log("TLS1.3 fresh-store new boot accepted only after immutable cold recovery; old boot denied; no credit or runtime authority created")
}
