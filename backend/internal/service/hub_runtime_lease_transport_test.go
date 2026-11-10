package service_test

import (
	"bytes"
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

	"github.com/google/uuid"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"vortex/agent"
	"vortex/agent/store"
	"vortex/backend/db/generated"
	"vortex/backend/internal/control"
	"vortex/backend/internal/service"
	controlv1 "vortex/contracts/protobuf/v1"
	"vortex/hub"
)

type loseRuntimeLease struct {
	*control.HubAdapter
	before, after atomic.Bool
}
type runtimeLeaseStream struct {
	grpc.BidiStreamingServer[controlv1.HubMessage, controlv1.BackendMessage]
	owner *loseRuntimeLease
}

func (s *runtimeLeaseStream) Recv() (*controlv1.HubMessage, error) {
	m, e := s.BidiStreamingServer.Recv()
	if e == nil && m.GetLedgerRecord().GetKind() == "runtime-lease-v1" && twoLeasePayload(m.GetLedgerRecord().Payload) && s.owner.before.Swap(false) {
		return nil, errors.New("injected FULL runtime lease receipt loss")
	}
	return m, e
}
func (s *runtimeLeaseStream) Send(m *controlv1.BackendMessage) error {
	if m.GetLedgerAck().GetKind() == "runtime-lease-v1" && !s.owner.before.Load() && s.owner.after.Swap(false) {
		return errors.New("injected runtime lease PG ACK loss")
	}
	return s.BidiStreamingServer.Send(m)
}

func twoLeasePayload(raw []byte) bool {
	var p struct {
		Records []json.RawMessage `json:"authorizations"`
	}
	return json.Unmarshal(raw, &p) == nil && len(p.Records) == 2
}
func (s *loseRuntimeLease) Connect(stream grpc.BidiStreamingServer[controlv1.HubMessage, controlv1.BackendMessage]) error {
	return s.HubAdapter.Connect(&runtimeLeaseStream{stream, s})
}

// This uses a real60s interval: advancing the issuer clock while an already
// anchored Agent clock stood still would correctly fail continuity validation.
func TestRuntimeLeaseActualMTLS60SecondsAgentInstallAndLostBackendReceipts(t *testing.T) {
	x, in, hs := fundedHubFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 85*time.Second)
	defer cancel()
	a, e := x.s.PrepareHubAllocation(ctx, x.f.scope, in, "runtime-lease-transport")
	if e != nil {
		t.Fatal(e)
	}
	installAllocationFixture(t, hs, a, x.now.Add(time.Second))
	start := time.Now()
	base := x.now.Add(time.Second)
	clock := func() store.IssuerTime {
		now := base.Add(time.Since(start))
		return store.IssuerTime{Earliest: now, Latest: now, Trusted: true}
	}
	x.s.Clock = func() (time.Time, time.Time, bool) { c := clock(); return c.Earliest, c.Latest, c.Trusted }
	backendCA, backendKey := portCA(t)
	hubCA, hubKey := portCA(t)
	backendRoots, hubRoots, nodeRoots := x509.NewCertPool(), x509.NewCertPool(), x509.NewCertPool()
	backendRoots.AddCert(backendCA)
	hubRoots.AddCert(hubCA)
	nodeRoots.AddCert(x.s.NodeCA)
	x.s.SignerCA = hubCA
	backendTLS := &tls.Config{Certificates: []tls.Certificate{portCertificate(t, backendCA, backendKey, shareBackendID, x509.ExtKeyUsageServerAuth)}, ClientCAs: hubRoots, ClientAuth: tls.RequireAndVerifyClientCert, MinVersion: tls.VersionTLS13, MaxVersion: tls.VersionTLS13}
	hubClientTLS := &tls.Config{Certificates: []tls.Certificate{portCertificate(t, hubCA, hubKey, in.HubID, x509.ExtKeyUsageClientAuth)}, RootCAs: backendRoots, ServerName: shareBackendID, MinVersion: tls.VersionTLS13, MaxVersion: tls.VersionTLS13}
	hubServerTLS := &tls.Config{Certificates: []tls.Certificate{portCertificate(t, hubCA, hubKey, in.HubID, x509.ExtKeyUsageServerAuth)}, ClientCAs: nodeRoots, ClientAuth: tls.RequireAndVerifyClientCert, MinVersion: tls.VersionTLS13, MaxVersion: tls.VersionTLS13}
	nodeTLS := &tls.Config{Certificates: []tls.Certificate{portCertificate(t, x.s.NodeCA, x.f.caKey, in.NodeID, x509.ExtKeyUsageClientAuth)}, RootCAs: hubRoots, ServerName: in.HubID, MinVersion: tls.VersionTLS13, MaxVersion: tls.VersionTLS13}
	listen := func() *net.TCPListener {
		t.Helper()
		l, e := net.Listen("tcp", "127.0.0.1:0")
		if e != nil {
			t.Fatal(e)
		}
		t.Cleanup(func() { l.Close() })
		return l.(*net.TCPListener)
	}
	bl, nl := listen(), listen()
	backendServer := grpc.NewServer(grpc.Creds(credentials.NewTLS(backendTLS)))
	defer backendServer.Stop()
	adapter := &loseRuntimeLease{HubAdapter: &control.HubAdapter{BackendID: shareBackendID, HubID: in.HubID, Region: in.Region, Shares: &x.s, Metering: &service.MeteringService{Pool: x.f.pool}, Observe: func(context.Context, string, string, []byte) error { return nil }}}
	adapter.before.Store(true)
	adapter.after.Store(true)
	controlv1.RegisterHubControlServer(backendServer, adapter)
	go backendServer.Serve(bl)
	for _, caps := range [][]string{
		{"status-v1", "backend-allocation-v1", "backend-ledger-v1"},
		{"status-v1", "backend-allocation-v1", "backend-ledger-v1", "share-renewal-close-v1"},
		{"status-v1", "backend-allocation-v1", "backend-ledger-v1", "share-renewal-close-v1", "delegation-seal-v1"},
		{"status-v1", "usage-ledger-v1"},
	} {
		conn, err := grpc.NewClient(bl.Addr().String(), grpc.WithTransportCredentials(credentials.NewTLS(hubClientTLS.Clone())))
		if err != nil {
			t.Fatal(err)
		}
		legacyCtx, cancelLegacy := context.WithTimeout(ctx, 3*time.Second)
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
		if err = stream.Send(&controlv1.HubMessage{HubId: in.HubID, BootId: in.HubBootID, Sequence: 2, Body: &controlv1.HubMessage_Status{Status: &controlv1.HubStatus{HubId: in.HubID, RegionId: in.Region, SessionId: session, ControlVersion: "1.6.0", Capabilities: caps, Mode: "online", SignerEpoch: a.SignerEpoch, BackendHeartbeatAt: hb.GetHeartbeat().ObservedAt, AutonomyDeadline: hb.GetHeartbeat().AutonomyDeadline}}}); err != nil {
			t.Fatal(err)
		}
		raw := []byte(`{}`)
		digest := sha256.Sum256(raw)
		if err = stream.Send(&controlv1.HubMessage{HubId: in.HubID, BootId: in.HubBootID, Sequence: 3, Body: &controlv1.HubMessage_LedgerRecord{LedgerRecord: &controlv1.LedgerRecord{RecordId: uuid.NewString(), LedgerSequence: 1, Kind: "runtime-lease-v1", Payload: raw, PayloadSha256: digest[:]}}}); err != nil {
			t.Fatal(err)
		}
		for {
			message, recvErr := stream.Recv()
			if recvErr != nil {
				if !strings.Contains(recvErr.Error(), "runtime lease capability not negotiated") {
					t.Fatal("legacy lease rejected for wrong reason", caps, recvErr)
				}
				break
			}
			if message.GetLedgerAck() != nil {
				t.Fatal("legacy peer acknowledged lease", caps)
			}
		}
		cancelLegacy()
		conn.Close()
	}
	server := grpc.NewServer(grpc.Creds(credentials.NewTLS(hubServerTLS)))
	defer server.Stop()
	issuer := &hub.Issuer{Store: hs, HubID: in.HubID, Clock: clock}
	controlv1.RegisterNodeControlServer(server, &hub.RuntimeServer{HubID: in.HubID, Region: in.Region, Issuer: issuer, Usage: &hub.UsageRelay{Store: hs, Clock: clock}})
	go server.Serve(nl)
	st, e := store.Open(t.TempDir()+"/actual-node.db", "agent")
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
	run, stop := context.WithCancel(ctx)
	defer stop()
	cfg := &hub.BackendClient{HubID: in.HubID, BootID: in.HubBootID, BackendID: shareBackendID, Region: in.Region, Address: bl.Addr().String(), TLS: hubClientTLS, Store: hs, Clock: clock}
	backendDone := make(chan error, 1)
	go func() {
		for i := 0; i < 3; i++ {
			err := cfg.Run(run)
			if i == 2 || run.Err() != nil {
				backendDone <- err
				return
			}
		}
	}()
	client := &agent.ControlClient{Node: node, TLS: nodeTLS}
	nodeDone := make(chan error, 1)
	go func() {
		nodeDone <- client.RunRegion(run, agent.RegionalConfig{Region: in.Region, Endpoints: []agent.HubEndpoint{{Region: in.Region, HubID: in.HubID, ServerName: in.HubID, Address: nl.Addr().String()}}, LeaseRefresh: true})
	}()
	defer func() {
		stop()
		for _, done := range []<-chan error{backendDone, nodeDone} {
			select {
			case <-done:
			case <-time.After(2 * time.Second):
				t.Error("runtime transport did not stop")
			}
		}
	}()
	q := generated.New(x.f.pool)
	var initial agent.AuthorizationInspection
	waitHub(t, ctx, func() bool {
		rows, err := hs.PendingLedger(ctx, 1000)
		if err != nil {
			return false
		}
		for _, r := range rows {
			if r.Kind == "issue" {
				initial, err = agent.InspectAuthorizationUpdate(r.Payload, a.NodeID, a.BootID, a.PathGeneration)
				return err == nil
			}
		}
		var payload []byte
		if err = x.f.pool.QueryRow(ctx, `SELECT payload FROM hub_issue_receipts WHERE tenant_id=$1 AND allocation_id=$2`, x.f.tenant, a.AllocationID).Scan(&payload); err != nil {
			return false
		}
		initial, err = agent.InspectAuthorizationUpdate(payload, a.NodeID, a.BootID, a.PathGeneration)
		return err == nil
	})
	var grant string
	for _, r := range initial.Records {
		if r.Kind == "credit_grant" {
			grant = r.AuthorizationID
		}
	}
	if grant == "" {
		t.Fatal("initial funded credit missing")
	}
	var leasePayload []byte
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		if x.f.pool.QueryRow(ctx, `SELECT payload FROM hub_runtime_lease_receipts WHERE tenant_id=$1 AND jsonb_array_length(convert_from(payload,'UTF8')::jsonb->'authorizations')=2`, x.f.tenant).Scan(&leasePayload) == nil && !adapter.before.Load() && !adapter.after.Load() {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal("60s refresh did not converge", ctx.Err())
		case <-ticker.C:
		}
	}
	facts, e := agent.InspectAuthorizationUpdate(leasePayload, a.NodeID, a.BootID, a.PathGeneration)
	if e != nil || len(facts.Records) != 2 || facts.IssuedAt.Before(initial.IssuedAt.Add(time.Minute)) {
		t.Fatal("cadence/payload", facts, e)
	}
	waitHub(t, ctx, func() bool {
		low, high, trusted := node.TrustedTime()
		if !trusted {
			return false
		}
		for _, r := range facts.Records {
			_, err := st.CheckStoredAuthorization(ctx, store.AuthorizationUse{NodeID: a.NodeID, BootID: a.BootID, AuthorizationID: r.AuthorizationID, Kind: r.Kind, RuleID: a.RuleID, RuleRevision: a.RuleRevision, PathGeneration: a.PathGeneration, Earliest: low, Latest: high, TrustedTime: trusted})
			if err != nil {
				return false
			}
		}
		return true
	})
	remaining, e := st.RemainingAuthorizationBytes(ctx, a.NodeID, grant)
	if e != nil || remaining != a.RawBytes {
		t.Fatal("refresh changed original Node credit", remaining, e)
	}
	issued, e := q.HubAllocationIssued(ctx, generated.HubAllocationIssuedParams{TenantID: x.f.tenant, AllocationID: a.AllocationID})
	if e != nil || issued != int64(a.RawBytes) {
		t.Fatal("refresh created PG credit", issued, e)
	}
	waitHub(t, ctx, func() bool { rows, err := hs.PendingLedger(ctx, 1000); return err == nil && len(rows) == 0 })
	var replay []byte
	if e = x.f.pool.QueryRow(ctx, `SELECT payload FROM hub_runtime_lease_receipts WHERE tenant_id=$1 AND jsonb_array_length(convert_from(payload,'UTF8')::jsonb->'authorizations')=2`, x.f.tenant).Scan(&replay); e != nil || !bytes.Equal(leasePayload, replay) {
		t.Fatal("lost ACK replay changed lease", e)
	}
	t.Log("actual60s runtime/share refresh, actual Agent FULL install, independent role mTLS, FULL receipt/PG ACK loss and replay, no additional Node/PG credit")
}
