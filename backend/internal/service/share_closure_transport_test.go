package service_test

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"net"
	"strings"
	"sync/atomic"
	"testing"
	"time"
	"vortex/contracts"

	"github.com/google/uuid"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"vortex/agent/store"
	"vortex/backend/db/generated"
	"vortex/backend/internal/control"
	controlv1 "vortex/contracts/protobuf/v1"
	"vortex/hub"
)

type loseClosureReceipts struct {
	*control.HubAdapter
	before, after atomic.Bool
}
type closureStream struct {
	grpc.BidiStreamingServer[controlv1.HubMessage, controlv1.BackendMessage]
	server *loseClosureReceipts
}

func (s *closureStream) Recv() (*controlv1.HubMessage, error) {
	m, e := s.BidiStreamingServer.Recv()
	if e == nil && m.GetLedgerRecord().GetKind() == "share-renewal-closed-v1" && s.server.before.Swap(false) {
		return nil, errors.New("injected lost FULL closure receipt before PG")
	}
	return m, e
}
func (s *closureStream) Send(m *controlv1.BackendMessage) error {
	if m.GetLedgerAck().GetKind() == "share-renewal-closed-v1" && s.server.after.Swap(false) {
		return errors.New("injected lost closure ACK after PG")
	}
	return s.BidiStreamingServer.Send(m)
}
func (s *loseClosureReceipts) Connect(stream grpc.BidiStreamingServer[controlv1.HubMessage, controlv1.BackendMessage]) error {
	return s.HubAdapter.Connect(&closureStream{BidiStreamingServer: stream, server: s})
}

func TestShareRenewalClosureActualMTLSLossReconnectAndCapabilityIsolation(t *testing.T) {
	x, in, hs := fundedHubFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	a, e := x.s.PrepareHubAllocation(ctx, x.f.scope, in, uuid.NewString())
	if e != nil {
		t.Fatal(e)
	}
	installAllocationFixture(t, hs, a, x.now)
	next := prepareClosureSuccessor(t, x)
	backendCA, backendKey := portCA(t)
	hubCA, hubKey := portCA(t)
	backendRoots, hubRoots := x509.NewCertPool(), x509.NewCertPool()
	backendRoots.AddCert(backendCA)
	hubRoots.AddCert(hubCA)
	serverTLS := &tls.Config{Certificates: []tls.Certificate{portCertificate(t, backendCA, backendKey, shareBackendID, x509.ExtKeyUsageServerAuth)}, ClientCAs: hubRoots, ClientAuth: tls.RequireAndVerifyClientCert, MinVersion: tls.VersionTLS13, MaxVersion: tls.VersionTLS13}
	clientTLS := &tls.Config{Certificates: []tls.Certificate{portCertificate(t, hubCA, hubKey, in.HubID, x509.ExtKeyUsageClientAuth)}, RootCAs: backendRoots, ServerName: shareBackendID, MinVersion: tls.VersionTLS13, MaxVersion: tls.VersionTLS13}
	now := x.now.Add(time.Second)
	x.s.SignerCA = hubCA
	x.s.Clock = func() (time.Time, time.Time, bool) { return now, now, true }
	clock := func() store.IssuerTime { return store.IssuerTime{Earliest: now, Latest: now, Trusted: true} }
	if e = x.s.PrepareRenewalClosures(ctx, x.f.scope, next.Generation.ID, shareBackendID, uuid.NewString()); e != nil {
		t.Fatal(e)
	}
	listener, e := net.Listen("tcp", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	server := grpc.NewServer(grpc.Creds(credentials.NewTLS(serverTLS)))
	defer server.Stop()
	var observations atomic.Int32
	adapter := &loseClosureReceipts{HubAdapter: &control.HubAdapter{BackendID: shareBackendID, HubID: in.HubID, Region: in.Region, Shares: &x.s, Observe: func(context.Context, string, string, []byte) error { observations.Add(1); return nil }}}
	controlv1.RegisterHubControlServer(server, adapter)
	go server.Serve(listener)
	commands, e := generated.New(x.f.pool).ShareClosureCommands(ctx, generated.ShareClosureCommandsParams{TenantID: x.f.tenant, HandoffID: next.Generation.ID, SignerID: in.HubID})
	if e != nil || len(commands) != 1 {
		t.Fatal(e)
	}
	forged, _ := json.Marshal(contracts.ShareRenewalClosureReceipt{Version: 1, ID: commands[0].ID, CommandDigest: contracts.SharePayloadDigest(commands[0].Payload)})
	forgedDigest := sha256.Sum256(forged)
	// Both older funded and observation-only peers remain isolated from this new command.
	for _, caps := range [][]string{{"status-v1", "backend-allocation-v1", "backend-ledger-v1"}, {"status-v1", "usage-ledger-v1"}} {
		conn, e := grpc.NewClient(listener.Addr().String(), grpc.WithTransportCredentials(credentials.NewTLS(clientTLS.Clone())))
		if e != nil {
			t.Fatal(e)
		}
		legacyCtx, stop := context.WithTimeout(ctx, 250*time.Millisecond)
		stream, e := controlv1.NewHubControlClient(conn).Connect(legacyCtx)
		if e != nil {
			t.Fatal(e)
		}
		session := uuid.NewString()
		if e = stream.Send(&controlv1.HubMessage{HubId: in.HubID, BootId: in.HubBootID, Sequence: 1, Body: &controlv1.HubMessage_LifecycleHello{LifecycleHello: &controlv1.LifecycleHello{ControlVersion: "1.6.0", Capabilities: caps, SessionId: session, RegionId: in.Region}}}); e != nil {
			t.Fatal(e)
		}
		hb, e := stream.Recv()
		if e != nil {
			t.Fatal(e)
		}
		if e = stream.Send(&controlv1.HubMessage{HubId: in.HubID, BootId: in.HubBootID, Sequence: 2, Body: &controlv1.HubMessage_Status{Status: &controlv1.HubStatus{HubId: in.HubID, RegionId: in.Region, SessionId: session, ControlVersion: "1.6.0", Capabilities: caps, Mode: "online", SignerEpoch: 7, BackendHeartbeatAt: hb.GetHeartbeat().ObservedAt, AutonomyDeadline: hb.GetHeartbeat().AutonomyDeadline}}}); e != nil {
			t.Fatal(e)
		}
		if len(caps) == 3 {
			m, e := stream.Recv()
			if e == nil {
				t.Fatal("legacy peer received new command", m)
			}
		} else {
			if e = stream.Send(&controlv1.HubMessage{HubId: in.HubID, BootId: in.HubBootID, Sequence: 3, Body: &controlv1.HubMessage_LedgerRecord{LedgerRecord: &controlv1.LedgerRecord{RecordId: commands[0].ID, LedgerSequence: 1, Kind: "share-renewal-closed-v1", Payload: forged, PayloadSha256: forgedDigest[:]}}}); e != nil {
				t.Fatal(e)
			}
			if _, e = stream.Recv(); e == nil || !strings.Contains(e.Error(), "closure capability not negotiated") {
				t.Fatal("observation-only peer forged closure")
			}
		}
		stop()
		conn.Close()
	}
	adapter.before.Store(true)
	adapter.after.Store(true)
	cfg := &hub.BackendClient{HubID: in.HubID, BootID: in.HubBootID, BackendID: shareBackendID, Region: in.Region, Address: listener.Addr().String(), TLS: clientTLS, Store: hs, Clock: clock}
	if e = cfg.Run(ctx); e == nil {
		t.Fatal("lost FULL closure receipt unexpectedly acknowledged")
	}
	if _, e = hs.IssueHubAuthorization(ctx, store.IssueRequest{ID: uuid.NewString(), HubID: a.HubID, DelegationID: a.DelegationID, NodeID: a.NodeID, BootID: a.BootID, RawBytes: 1, TTL: time.Minute}, clock()); e == nil {
		t.Fatal("old signer remained open after FULL closure")
	}
	q := generated.New(x.f.pool)
	a1, e := q.ShareRenewalAuthority(ctx, generated.ShareRenewalAuthorityParams{TenantID: x.f.tenant, NodeID: in.NodeID, Kind: "rate", Generation: 1, SignerID: in.HubID})
	if e != nil || a1.ClosedAt.Valid {
		t.Fatal("lost receipt became PG closure", e)
	}
	if e = cfg.Run(ctx); e == nil {
		t.Fatal("lost PG ACK unexpectedly acknowledged")
	}
	a1, e = q.ShareRenewalAuthority(ctx, generated.ShareRenewalAuthorityParams{TenantID: x.f.tenant, NodeID: in.NodeID, Kind: "rate", Generation: 1, SignerID: in.HubID})
	if e != nil || !a1.ClosedAt.Valid {
		t.Fatal("PG proof not committed before lost ACK", e)
	}
	if shareChain(t, x).HeldAmount != 600000 {
		t.Fatal("lost receipt/ACK released occupation")
	}
	previous := observations.Load()
	runCtx, stop := context.WithCancel(ctx)
	done := make(chan error, 1)
	go func() { done <- cfg.Run(runCtx) }()
	waitHub(t, ctx, func() bool { return observations.Load() > previous })
	stop()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("reconnect did not stop")
	}
	p, e := q.GetSourceProjection(ctx, generated.GetSourceProjectionParams{TenantID: x.f.tenant, SourceID: in.SourceID})
	if e != nil || p.F != in.RawBytes {
		t.Fatal("closure returned parent funds", e, p)
	}
	if e = x.s.QueueHandoff(ctx, x.f.scope, next.Generation.ID, uuid.NewString()); e != nil {
		t.Fatal("actual closure failed to unblock CAS", e)
	}
	t.Log("actual TLS1.3 FULL closure loss/replay, PG ACK loss/reconnect, old-capability isolation and retained funding passed")
}
