package service_test

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"net"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"vortex/agent/store"
	"vortex/backend/db/generated"
	"vortex/backend/internal/control"
	controlv1 "vortex/contracts/protobuf/v1"
	"vortex/hub"
)

type loseSealReceipts struct {
	*control.HubAdapter
	before, after atomic.Bool
}
type sealStream struct {
	grpc.BidiStreamingServer[controlv1.HubMessage, controlv1.BackendMessage]
	server *loseSealReceipts
}

func (s *sealStream) Recv() (*controlv1.HubMessage, error) {
	m, e := s.BidiStreamingServer.Recv()
	if e == nil && m.GetLedgerRecord().GetKind() == "delegation-sealed-v1" && s.server.before.Swap(false) {
		return nil, errors.New("injected lost FULL seal before PG")
	}
	return m, e
}
func (s *sealStream) Send(m *controlv1.BackendMessage) error {
	if m.GetLedgerAck().GetKind() == "delegation-sealed-v1" && s.server.after.Swap(false) {
		return errors.New("injected lost seal ACK after PG")
	}
	return s.BidiStreamingServer.Send(m)
}
func (s *loseSealReceipts) Connect(stream grpc.BidiStreamingServer[controlv1.HubMessage, controlv1.BackendMessage]) error {
	return s.HubAdapter.Connect(&sealStream{BidiStreamingServer: stream, server: s})
}
func TestDelegationSealActualMTLSIssueInventoryBeforeReturnAndLostACKReplay(t *testing.T) {
	x, in, hs := fundedHubFixture(t)
	// Expire the delegation within the heartbeat loss grace. A long offline
	// clock jump correctly requires a separate authority recovery handshake.
	in.ExpiresAt = x.now.Add(5 * time.Second)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	a, e := x.s.PrepareHubAllocation(ctx, x.f.scope, in, uuid.NewString())
	if e != nil {
		t.Fatal(e)
	}
	installAllocationFixture(t, hs, a, x.now)
	issueAllocationFixture(t, x, hs, a, 51)
	backendCA, backendKey := portCA(t)
	hubCA, hubKey := portCA(t)
	backendRoots, hubRoots := x509.NewCertPool(), x509.NewCertPool()
	backendRoots.AddCert(backendCA)
	hubRoots.AddCert(hubCA)
	serverTLS := &tls.Config{Certificates: []tls.Certificate{portCertificate(t, backendCA, backendKey, shareBackendID, x509.ExtKeyUsageServerAuth)}, ClientCAs: hubRoots, ClientAuth: tls.RequireAndVerifyClientCert, MinVersion: tls.VersionTLS13, MaxVersion: tls.VersionTLS13}
	clientTLS := &tls.Config{Certificates: []tls.Certificate{portCertificate(t, hubCA, hubKey, in.HubID, x509.ExtKeyUsageClientAuth)}, RootCAs: backendRoots, ServerName: shareBackendID, MinVersion: tls.VersionTLS13, MaxVersion: tls.VersionTLS13}
	now := x.now.Add(6 * time.Second)
	x.s.SignerCA = hubCA
	x.s.Clock = func() (time.Time, time.Time, bool) { return now, now, true }
	clock := func() store.IssuerTime { return store.IssuerTime{Earliest: now, Latest: now, Trusted: true} }
	listener, e := net.Listen("tcp", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	server := grpc.NewServer(grpc.Creds(credentials.NewTLS(serverTLS)))
	defer server.Stop()
	adapter := &loseSealReceipts{HubAdapter: &control.HubAdapter{BackendID: shareBackendID, HubID: in.HubID, Region: in.Region, Shares: &x.s, Observe: func(context.Context, string, string, []byte) error { return nil }}}
	controlv1.RegisterHubControlServer(server, adapter)
	go server.Serve(listener)
	// Earlier funded/closure peers and usage-only peers cannot submit the new
	// ledger kind, even after authenticated status confirmation.
	for _, caps := range [][]string{
		{"status-v1", "backend-allocation-v1", "backend-ledger-v1"},
		{"status-v1", "backend-allocation-v1", "backend-ledger-v1", "share-renewal-close-v1"},
		{"status-v1", "usage-ledger-v1"},
	} {
		conn, err := grpc.NewClient(listener.Addr().String(), grpc.WithTransportCredentials(credentials.NewTLS(clientTLS.Clone())))
		if err != nil {
			t.Fatal(err)
		}
		legacyCtx, stop := context.WithTimeout(ctx, 2*time.Second)
		stream, err := controlv1.NewHubControlClient(conn).Connect(legacyCtx)
		if err != nil {
			t.Fatal(err)
		}
		session := uuid.NewString()
		if err = stream.Send(&controlv1.HubMessage{HubId: in.HubID, BootId: in.HubBootID, Sequence: 1, Body: &controlv1.HubMessage_LifecycleHello{LifecycleHello: &controlv1.LifecycleHello{ControlVersion: "1.6.0", Capabilities: caps, SessionId: session, RegionId: in.Region}}}); err != nil {
			t.Fatal(err)
		}
		hb, err := stream.Recv()
		if err != nil {
			t.Fatal(err)
		}
		if err = stream.Send(&controlv1.HubMessage{HubId: in.HubID, BootId: in.HubBootID, Sequence: 2, Body: &controlv1.HubMessage_Status{Status: &controlv1.HubStatus{HubId: in.HubID, RegionId: in.Region, SessionId: session, ControlVersion: "1.6.0", Capabilities: caps, Mode: "online", SignerEpoch: 7, BackendHeartbeatAt: hb.GetHeartbeat().ObservedAt, AutonomyDeadline: hb.GetHeartbeat().AutonomyDeadline}}}); err != nil {
			t.Fatal(err)
		}
		forged := []byte(`{}`)
		digest := sha256.Sum256(forged)
		if err = stream.Send(&controlv1.HubMessage{HubId: in.HubID, BootId: in.HubBootID, Sequence: 3, Body: &controlv1.HubMessage_LedgerRecord{LedgerRecord: &controlv1.LedgerRecord{RecordId: uuid.NewString(), LedgerSequence: 1, Kind: "delegation-sealed-v1", Payload: forged, PayloadSha256: digest[:]}}}); err != nil {
			t.Fatal(err)
		}
		for {
			message, recvErr := stream.Recv()
			if recvErr != nil {
				if !strings.Contains(recvErr.Error(), "delegation seal capability not negotiated") {
					t.Fatal("legacy seal rejected for wrong reason", caps, recvErr)
				}
				break
			}
			if message.GetLedgerAck() != nil {
				t.Fatal("legacy peer acknowledged seal", caps)
			}
		}
		stop()
		conn.Close()
	}
	adapter.before.Store(true)
	adapter.after.Store(true)
	cfg := &hub.BackendClient{HubID: in.HubID, BootID: in.HubBootID, BackendID: shareBackendID, Region: in.Region, Address: listener.Addr().String(), TLS: clientTLS, Store: hs, Clock: clock}
	if e = cfg.Run(ctx); e == nil {
		t.Fatal("lost seal reached PG")
	} else {
		t.Log("first transport ended:", e)
	}
	q := generated.New(x.f.pool)
	issued, e := q.HubAllocationIssued(ctx, generated.HubAllocationIssuedParams{TenantID: x.f.tenant, AllocationID: a.AllocationID})
	if e != nil || issued != 51 {
		t.Fatal("seal raced ahead of child mapping", e, issued)
	}
	projection, e := q.GetSourceProjection(ctx, generated.GetSourceProjectionParams{TenantID: x.f.tenant, SourceID: in.SourceID})
	if e != nil || projection.F != 103 {
		t.Fatal("lost seal released funds", e, projection)
	}
	if e = cfg.Run(ctx); e == nil {
		t.Fatal("lost PG ACK")
	}
	projection, e = q.GetSourceProjection(ctx, generated.GetSourceProjectionParams{TenantID: x.f.tenant, SourceID: in.SourceID})
	if e != nil || projection.F != 51 || projection.Charged != 0 {
		t.Fatal("seal did not return only unissued52", e, projection)
	}
	runCtx, stop := context.WithCancel(ctx)
	done := make(chan error, 1)
	go func() { done <- cfg.Run(runCtx) }()
	waitHub(t, ctx, func() bool { records, e := hs.PendingLedger(ctx, 100); return e == nil && len(records) == 0 })
	stop()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("replay client did not stop")
	}
	projection, e = q.GetSourceProjection(ctx, generated.GetSourceProjectionParams{TenantID: x.f.tenant, SourceID: in.SourceID})
	if e != nil || projection.F != 51 {
		t.Fatal("ACK replay repeated return", e, projection)
	}
	t.Log("actual mTLS automatic expiry seal, issue-before-seal ordering and lost FULL/PG ACK keep issued51 held and return unissued52 exactly once")
}
