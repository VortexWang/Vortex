package service_test

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"net"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/protobuf/proto"
	"vortex/agent"
	"vortex/agent/store"
	"vortex/backend/db/generated"
	"vortex/backend/internal/control"
	"vortex/backend/internal/service"
	controlv1 "vortex/contracts/protobuf/v1"
	"vortex/hub"
)

type loseFundedReceipts struct {
	*control.HubAdapter
	allocation, issue atomic.Bool
}
type fundedStream struct {
	grpc.BidiStreamingServer[controlv1.HubMessage, controlv1.BackendMessage]
	server *loseFundedReceipts
}

func (s *fundedStream) Recv() (*controlv1.HubMessage, error) {
	m, e := s.BidiStreamingServer.Recv()
	if e == nil && m.GetLedgerRecord().GetKind() == "allocation-installed-v1" && s.server.allocation.Swap(false) {
		return nil, errors.New("injected lost FULL allocation receipt before PG commit")
	}
	return m, e
}
func (s *fundedStream) Send(m *controlv1.BackendMessage) error {
	if m.GetLedgerAck().GetKind() == "issue-v1" && s.server.issue.Swap(false) {
		return errors.New("injected lost issue ACK after PG commit")
	}
	return s.BidiStreamingServer.Send(m)
}
func (s *loseFundedReceipts) Connect(stream grpc.BidiStreamingServer[controlv1.HubMessage, controlv1.BackendMessage]) error {
	return s.HubAdapter.Connect(&fundedStream{BidiStreamingServer: stream, server: s})
}

func TestHubFundedActualMTLSLostReceiptsReconnectAndNewBootDenied(t *testing.T) {
	x, in, _ := fundedHubFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	a, e := x.s.PrepareHubAllocation(ctx, x.f.scope, in, uuid.NewString())
	if e != nil {
		t.Fatal(e)
	}
	// Independent backend and Hub trust roots; the original Node root is not
	// accepted by this listener or the service's signer-role verification.
	backendCA, backendKey := portCA(t)
	hubCA, hubKey := portCA(t)
	backendRoots, hubRoots := x509.NewCertPool(), x509.NewCertPool()
	backendRoots.AddCert(backendCA)
	hubRoots.AddCert(hubCA)
	backendID := uuid.NewString()
	serverTLS := &tls.Config{Certificates: []tls.Certificate{portCertificate(t, backendCA, backendKey, backendID, x509.ExtKeyUsageServerAuth)}, ClientCAs: hubRoots, ClientAuth: tls.RequireAndVerifyClientCert, MinVersion: tls.VersionTLS13, MaxVersion: tls.VersionTLS13}
	clientTLS := &tls.Config{Certificates: []tls.Certificate{portCertificate(t, hubCA, hubKey, in.HubID, x509.ExtKeyUsageClientAuth)}, RootCAs: backendRoots, ServerName: backendID, MinVersion: tls.VersionTLS13, MaxVersion: tls.VersionTLS13}
	now := x.now.Add(time.Second)
	x.s.SignerCA = hubCA
	x.s.Clock = func() (time.Time, time.Time, bool) { return now, now, true }
	clock := func() store.IssuerTime { return store.IssuerTime{Earliest: now, Latest: now, Trusted: true} }
	listener, e := net.Listen("tcp", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	server := grpc.NewServer(grpc.Creds(credentials.NewTLS(serverTLS)))
	defer server.Stop()
	adapter := &loseFundedReceipts{HubAdapter: &control.HubAdapter{BackendID: backendID, HubID: in.HubID, Region: in.Region, Shares: &x.s, Metering: &service.MeteringService{Pool: x.f.pool}, Observe: func(context.Context, string, string, []byte) error { return nil }}}
	adapter.allocation.Store(true)
	adapter.issue.Store(true)
	controlv1.RegisterHubControlServer(server, adapter)
	go server.Serve(listener)
	hs, e := store.Open(filepath.Join(t.TempDir(), "actual-hub.db"), "hub")
	if e != nil {
		t.Fatal(e)
	}
	defer hs.Close()
	cfg := &hub.BackendClient{RecoveryPreflight: true, HubID: in.HubID, BootID: in.HubBootID, BackendID: backendID, Region: in.Region, Address: listener.Addr().String(), TLS: clientTLS, Store: hs, Clock: clock}
	if e = cfg.Run(ctx); e == nil {
		t.Fatal("lost allocation receipt unexpectedly succeeded")
	}
	snapshot, e := hs.HubControlSnapshot(ctx, in.HubID, now)
	if e != nil || snapshot.Epoch != 7 || snapshot.Mode != "online" {
		t.Fatal("real FULL allocation not installed", e, snapshot)
	}
	q := generated.New(x.f.pool)
	if _, e = q.GetHubAllocationReceipt(ctx, generated.GetHubAllocationReceiptParams{TenantID: x.f.tenant, AllocationID: a.AllocationID}); e == nil {
		t.Fatal("injected receipt was not lost")
	}
	issued, grant, seq := issueAllocationFixture(t, x, hs, a, 51)
	batch := &controlv1.UsageBatch{BatchId: uuid.NewString(), BillingSnapshotId: a.SnapshotID, Deltas: []*controlv1.UsageDelta{{RuleId: a.RuleID, GrantId: grant, Role: "source", Direction: "ingress", Bytes: 9}}}
	raw, e := proto.Marshal(batch)
	if e != nil {
		t.Fatal(e)
	}
	sum := sha256.Sum256(raw)
	usage := store.HubUsage{NodeID: a.NodeID, BootID: a.BootID, ReportBootID: a.BootID, BatchID: batch.BatchId, Sequence: 1, Kind: "known", Payload: raw, Digest: sum[:]}
	usageClient := hub.BackendUsageClient{Config: cfg}
	if _, e = usageClient.AcceptUsage(ctx, usage); e == nil {
		t.Fatal("unmapped child usage acknowledged before issue registration")
	}

	if e = cfg.Run(ctx); e == nil {
		t.Fatal("lost issue acknowledgement unexpectedly succeeded")
	}
	facts, e := agent.InspectAuthorizationUpdate(issued.Payload, a.NodeID, a.BootID, a.PathGeneration)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = q.GetHubIssue(ctx, generated.GetHubIssueParams{TenantID: x.f.tenant, IssueID: facts.UpdateID}); e != nil {
		t.Fatal("PG issue did not commit before lost acknowledgement", e)
	}
	journal, e := hs.PendingLedger(ctx, 1000)
	if e != nil || len(journal) != 1 || journal[0].Sequence != seq {
		t.Fatal("lost acknowledgement removed journal", e, journal)
	}
	runCtx, stop := context.WithCancel(ctx)
	done := make(chan error, 1)
	go func() { done <- cfg.Run(runCtx) }()
	defer func() {
		stop()
		select {
		case <-done:
		case <-time.After(time.Second):
			t.Error("funded stream did not stop")
		}
	}()
	waitHub(t, ctx, func() bool { r, e := hs.PendingLedger(ctx, 1000); return e == nil && len(r) == 0 })
	issuedTotal, e := q.HubAllocationIssued(ctx, generated.HubAllocationIssuedParams{TenantID: x.f.tenant, AllocationID: a.AllocationID})
	if e != nil || issuedTotal != 51 {
		t.Fatal("issue replay duplicated raw partition", e, issuedTotal, grant)
	}
	if _, e = hs.IssueHubAuthorization(ctx, store.IssueRequest{ID: uuid.NewString(), HubID: a.HubID, DelegationID: a.DelegationID, NodeID: a.NodeID, BootID: a.BootID, RawBytes: 53, TTL: time.Minute}, clock()); e == nil {
		t.Fatal("reinstalled allocation refilled spent budget")
	}
	for i := 0; i < 2; i++ {
		if _, e = usageClient.AcceptUsage(ctx, usage); e != nil {
			t.Fatal("active Hub usage receipt/replay", e)
		}
	}
	// Empty local state plus a different boot cannot reactivate the old money.
	empty, e := store.Open(filepath.Join(t.TempDir(), "empty-hub.db"), "hub")
	if e != nil {
		t.Fatal(e)
	}
	defer empty.Close()
	other := *cfg
	other.Store, other.BootID = empty, uuid.NewString()
	if e = other.Run(ctx); e == nil {
		t.Fatal("new boot bypassed recovery")
	}
	other.BootID = cfg.BootID
	if e = other.Run(ctx); e == nil {
		t.Fatal("empty local state reused installed boot authority")
	}
	emptyState, e := empty.HubControlSnapshot(ctx, in.HubID, now)
	if e != nil || emptyState.Epoch != 0 {
		t.Fatal("new boot received authority", e, emptyState)
	}
	p, e := q.GetSourceProjection(ctx, generated.GetSourceProjectionParams{TenantID: x.f.tenant, SourceID: in.SourceID})
	if e != nil || p.F != in.RawBytes-9 || p.Charged != 9 {
		t.Fatal("disconnect/new boot released funding", e, p)
	}
	wrong := *cfg
	wrong.TLS = cfg.TLS.Clone()
	wrong.TLS.Certificates = []tls.Certificate{portCertificate(t, x.s.NodeCA, x.f.caKey, in.HubID, x509.ExtKeyUsageClientAuth)}
	wrongCtx, wrongCancel := context.WithTimeout(ctx, 250*time.Millisecond)
	defer wrongCancel()
	if e = wrong.Run(wrongCtx); e == nil {
		t.Fatal("Node root impersonated Hub root")
	}
	t.Log("real TLS 1.3 role roots, FULL lost receipt/replay, PG issue lost ACK/replay, boot refusal and funded hold verified")
}

func waitHub(t *testing.T, ctx context.Context, ready func() bool) {
	t.Helper()
	timer := time.NewTimer(5 * time.Second)
	defer timer.Stop()
	tick := time.NewTicker(10 * time.Millisecond)
	defer tick.Stop()
	for {
		if ready() {
			return
		}
		select {
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		case <-timer.C:
			t.Fatal("Hub transport did not reach durable state")
		case <-tick.C:
		}
	}
}
