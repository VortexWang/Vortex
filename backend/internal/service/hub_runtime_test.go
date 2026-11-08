package service_test

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"net"
	"testing"
	"time"

	"github.com/google/uuid"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"vortex/agent"
	"vortex/agent/store"
	"vortex/backend/db/generated"
	controlv1 "vortex/contracts/protobuf/v1"
	"vortex/hub"
)

func TestProductionHubRuntimeActualMTLSFundedBootstrapReconnectAndBootIsolation(t *testing.T) {
	x, in, hs := fundedHubFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	a, e := x.s.PrepareHubAllocation(ctx, x.f.scope, in, uuid.NewString())
	if e != nil {
		t.Fatal(e)
	}
	installAllocationFixture(t, hs, a, x.now.Add(time.Second))
	clock := func() store.IssuerTime {
		return store.IssuerTime{Earliest: x.now.Add(time.Second), Latest: x.now.Add(time.Second), Trusted: true}
	}
	hubCA, hubKey := portCA(t)
	clients, servers := x509.NewCertPool(), x509.NewCertPool()
	clients.AddCert(x.s.NodeCA)
	servers.AddCert(hubCA)
	serverTLS := &tls.Config{Certificates: []tls.Certificate{portCertificate(t, hubCA, hubKey, in.HubID, x509.ExtKeyUsageServerAuth)}, ClientCAs: clients, ClientAuth: tls.RequireAndVerifyClientCert, MinVersion: tls.VersionTLS13, MaxVersion: tls.VersionTLS13}
	clientTLS := &tls.Config{Certificates: []tls.Certificate{portCertificate(t, x.s.NodeCA, x.f.caKey, in.NodeID, x509.ExtKeyUsageClientAuth)}, RootCAs: servers, ServerName: in.HubID, MinVersion: tls.VersionTLS13, MaxVersion: tls.VersionTLS13}
	listener, e := net.Listen("tcp", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	defer listener.Close()
	server := grpc.NewServer(grpc.Creds(credentials.NewTLS(serverTLS)))
	defer server.Stop()
	issuer := &hub.Issuer{Store: hs, HubID: in.HubID, Clock: clock}
	controlv1.RegisterNodeControlServer(server, &hub.RuntimeServer{HubID: in.HubID, Region: in.Region, Issuer: issuer, Usage: &hub.UsageRelay{Store: hs, Clock: clock}})
	go server.Serve(listener)
	dialCtx, stopDial := context.WithTimeout(ctx, 3*time.Second)
	defer stopDial()
	conn, e := grpc.DialContext(dialCtx, listener.Addr().String(), grpc.WithBlock(), grpc.WithTransportCredentials(credentials.NewTLS(clientTLS)))
	if e != nil {
		t.Fatal(e)
	}
	defer conn.Close()
	open := func(boot string) (grpc.BidiStreamingClient[controlv1.NodeMessage, controlv1.ControlMessage], context.CancelFunc) {
		t.Helper()
		c, stop := context.WithCancel(ctx)
		stream, e := controlv1.NewNodeControlClient(conn).Connect(c)
		if e != nil {
			t.Fatal(e)
		}
		if e = stream.Send(&controlv1.NodeMessage{NodeId: in.NodeID, BootId: boot, Sequence: 1, Body: &controlv1.NodeMessage_Heartbeat{Heartbeat: &controlv1.Heartbeat{}}}); e != nil {
			t.Fatal(e)
		}
		if e = stream.Send(&controlv1.NodeMessage{NodeId: in.NodeID, BootId: boot, Sequence: 2, Body: &controlv1.NodeMessage_LifecycleHello{LifecycleHello: &controlv1.LifecycleHello{ControlVersion: "1.3.0", Capabilities: []string{"status-v1"}, SessionId: uuid.NewString(), RegionId: in.Region}}}); e != nil {
			t.Fatal(e)
		}
		m, e := stream.Recv()
		if e != nil || m.GetHubStatus().GetMode() != "online" || m.GetHubStatus().GetSignerEpoch() != 7 {
			t.Fatal("persisted runtime status", m, e)
		}
		return stream, stop
	}
	first, stop := open(in.BootID)
	m, e := first.Recv()
	if e != nil || m.GetAuthorizationUpdate() == nil {
		t.Fatal("funded bootstrap", m, e)
	}
	update := m.GetAuthorizationUpdate()
	stop() // response may be lost after FULL; no refill on reconnect
	second, stop2 := open(in.BootID)
	defer stop2()
	replay, e := second.Recv()
	if e != nil || !bytes.Equal(replay.GetAuthorizationUpdate().GetPayload(), update.Payload) {
		t.Fatal("bootstrap bytes changed", e)
	}
	balance, e := hs.AuditHubDelegation(ctx, a.DelegationID)
	if e != nil || balance.Issued != uint64(in.RawBytes) || balance.Remaining != 0 {
		t.Fatal("duplicate source partition", balance, e)
	}
	// Real issuer journal registers with PG without another source reservation.
	journal, e := hs.PendingLedger(ctx, 1000)
	if e != nil || len(journal) != 1 {
		t.Fatal(journal, e)
	}
	if e = x.s.RecordHubIssue(x.f.contexts[in.HubID], in.HubID, in.HubBootID, journal[0].Sequence, journal[0].Payload, uuid.NewString()); e != nil {
		t.Fatal(e)
	}
	facts, e := agent.InspectAuthorizationUpdate(update.Payload, in.NodeID, in.BootID, uint64(in.PathGeneration))
	if e != nil || len(facts.Records) != 3 {
		t.Fatal(facts, e)
	}
	q := generated.New(x.f.pool)
	issued, e := q.HubAllocationIssued(ctx, generated.HubAllocationIssuedParams{TenantID: x.f.tenant, AllocationID: a.AllocationID})
	if e != nil || issued != in.RawBytes {
		t.Fatal(issued, e)
	}
	// A new authenticated boot can observe Hub status but selects no old funding.
	wrong, stopWrong := open(uuid.NewString())
	defer stopWrong()
	if e = wrong.Send(&controlv1.NodeMessage{NodeId: in.NodeID, BootId: in.BootID, Sequence: 3, Body: &controlv1.NodeMessage_Heartbeat{Heartbeat: &controlv1.Heartbeat{}}}); e != nil {
		t.Fatal(e)
	}
	if m, e = wrong.Recv(); e == nil {
		t.Fatal("new boot received old authority", m)
	}
	// The production Agent consumes the same persisted roots and issuer update.
	st, e := store.Open(t.TempDir()+"/node.db", "agent")
	if e != nil {
		t.Fatal(e)
	}
	defer st.Close()
	node, e := agent.New(in.NodeID, st, nil)
	if e != nil {
		t.Fatal(e)
	}
	node.BootID = in.BootID
	for _, root := range a.ShareReferences {
		if e = st.InstallShareSourceAnchor(ctx, in.NodeID, shareBackendID, root); e != nil {
			t.Fatal(e)
		}
	}
	client := &agent.ControlClient{Node: node, TLS: clientTLS}
	run, stopClient := context.WithCancel(ctx)
	done := make(chan error, 1)
	go func() {
		done <- client.ConnectRegion(run, in.Region, []agent.HubEndpoint{{HubID: in.HubID, Address: listener.Addr().String(), ServerName: in.HubID, Region: in.Region}})
	}()
	waitHub(t, ctx, func() bool {
		low, high, trusted := node.TrustedTime()
		return st.CheckRuntimeReady(ctx, store.AuthorizationUse{NodeID: in.NodeID, BootID: in.BootID, RuleID: a.RuleID, RuleRevision: a.RuleRevision, PathGeneration: a.PathGeneration, FencingGeneration: a.FencingFloor, BillingSnapshotID: a.SnapshotID, Earliest: low, Latest: high, TrustedTime: trusted}) == nil
	})
	stopClient()
	select {
	case <-done:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	t.Log("production RuntimeServer, actual Node/Hub mTLS, exact FULL replay and PG child mapping, boot isolation and actual Agent runtime-ready boundary verified")
}
