package service_test

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"github.com/google/uuid"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"net"
	"path/filepath"
	"sync"
	"testing"
	"time"
	"vortex/agent"
	"vortex/agent/store"
	"vortex/backend/db/generated"
	"vortex/backend/internal/control"
	"vortex/backend/internal/service"
	controlv1 "vortex/contracts/protobuf/v1"
	engineaccounting "vortex/engine/accounting"
	"vortex/hub"
)

type loseUsageAck struct {
	Client hub.BackendUsageClient
	mu     sync.Mutex
	Drop   bool
}

func (b *loseUsageAck) AcceptUsage(ctx context.Context, u store.HubUsage) ([]byte, error) {
	d, e := b.Client.AcceptUsage(ctx, u)
	if e != nil {
		return nil, e
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.Drop {
		b.Drop = false
		return nil, errors.New("injected receipt loss after PostgreSQL commit")
	}
	return d, nil
}

func TestUsageActualAgentHubPostgresLostAckRestartAndOriginalSequence(t *testing.T) {
	f, source, snapshot := ledgerFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	agentStore, e := store.Open(filepath.Join(t.TempDir(), "agent.db"), "agent")
	if e != nil {
		t.Fatal(e)
	}
	defer agentStore.Close()
	node, e := agent.New(f.nodes[0], agentStore, nil)
	if e != nil {
		t.Fatal(e)
	}
	now := time.Now().UTC().Truncate(time.Microsecond)
	r, e := (service.MeteringService{Pool: f.pool}).ReserveUsage(ctx, f.scope, service.ReserveInput{SourceID: source, SnapshotID: snapshot, NodeID: node.NodeID, BootID: node.BootID, Nonce: uuid.NewString(), RawBytes: 1000, Earliest: now, Latest: now, Deadline: now.Add(time.Hour)}, uuid.NewString())
	if e != nil {
		t.Fatal(e)
	}
	b, e := generated.New(f.pool).GetBillingSnapshot(ctx, generated.GetBillingSnapshotParams{TenantID: f.tenant, ID: snapshot})
	if e != nil {
		t.Fatal(e)
	}
	batch := &controlv1.UsageBatch{BatchId: uuid.NewString(), BillingSnapshotId: snapshot, Deltas: []*controlv1.UsageDelta{{RuleId: b.RuleID, Role: "source", Direction: "egress", Bytes: 123, GrantId: r.ID}}}
	if e = node.PersistUsageBatch(ctx, batch, 0); e != nil {
		t.Fatal(e)
	}
	hubID, e := generated.New(f.pool).UsageNodeHub(ctx, node.NodeID)
	if e != nil {
		t.Fatal(e)
	}
	backendID := uuid.NewString()
	hubBoot := uuid.NewString()
	ca, key := portCA(t)
	roots := x509.NewCertPool()
	roots.AddCert(ca)
	backendTLS := &tls.Config{Certificates: []tls.Certificate{portCertificate(t, ca, key, backendID, x509.ExtKeyUsageServerAuth)}, ClientCAs: roots, ClientAuth: tls.RequireAndVerifyClientCert, MinVersion: tls.VersionTLS13, MaxVersion: tls.VersionTLS13}
	clientTLS := &tls.Config{Certificates: []tls.Certificate{portCertificate(t, ca, key, hubID, x509.ExtKeyUsageClientAuth)}, RootCAs: roots, ServerName: backendID, MinVersion: tls.VersionTLS13, MaxVersion: tls.VersionTLS13}
	listener, e := net.Listen("tcp", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	server := grpc.NewServer(grpc.Creds(credentials.NewTLS(backendTLS)))
	controlv1.RegisterHubControlServer(server, &control.HubAdapter{BackendID: backendID, HubID: hubID, Region: "phase6", Metering: &service.MeteringService{Pool: f.pool}, Observe: func(context.Context, string, string, []byte) error { return nil }})
	go server.Serve(listener)
	defer server.Stop()
	hubPath := filepath.Join(t.TempDir(), "hub.db")
	hs, e := store.Open(hubPath, "hub")
	if e != nil {
		t.Fatal(e)
	}
	defer func() { hs.Close() }()
	if e = hs.RecordBackendHeartbeat(ctx, now, now.Add(time.Hour)); e != nil {
		t.Fatal(e)
	}
	cfg := &hub.BackendClient{HubID: hubID, BootID: hubBoot, BackendID: backendID, Region: "phase6", Address: listener.Addr().String(), TLS: clientTLS, Store: hs}
	backend := &loseUsageAck{Client: hub.BackendUsageClient{Config: cfg}, Drop: true}
	relay := &hub.UsageRelay{Store: hs, Backend: backend, Clock: func() store.IssuerTime {
		at := time.Now()
		return store.IssuerTime{Earliest: at, Latest: at, Trusted: true}
	}}
	nodeCA, nodeKey := portCA(t)
	nodeRoots := x509.NewCertPool()
	nodeRoots.AddCert(nodeCA)
	nodeHubID := uuid.NewString()
	hubTLS := &tls.Config{Certificates: []tls.Certificate{portCertificate(t, nodeCA, nodeKey, nodeHubID, x509.ExtKeyUsageServerAuth)}, ClientCAs: nodeRoots, ClientAuth: tls.RequireAndVerifyClientCert, MinVersion: tls.VersionTLS13, MaxVersion: tls.VersionTLS13}
	nodeTLS := &tls.Config{Certificates: []tls.Certificate{portCertificate(t, nodeCA, nodeKey, node.NodeID, x509.ExtKeyUsageClientAuth)}, RootCAs: nodeRoots, ServerName: nodeHubID, MinVersion: tls.VersionTLS13, MaxVersion: tls.VersionTLS13}
	nodeListener, e := net.Listen("tcp", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	nodeServer := grpc.NewServer(grpc.Creds(credentials.NewTLS(hubTLS)))
	controlv1.RegisterNodeControlServer(nodeServer, &hub.AccountingServer{Usage: relay})
	go nodeServer.Serve(nodeListener)
	defer nodeServer.Stop()
	client := &agent.ControlClient{Node: node, TLS: nodeTLS}
	if e = client.Connect(ctx, nodeListener.Addr().String()); e == nil {
		t.Fatal("Agent ACK before backend acknowledgement")
	}
	pending, e := hs.PendingBackendUsage(ctx)
	if e != nil || len(pending) != 1 || pending[0].Sequence == 0 || pending[0].ReportBootID != node.BootID {
		t.Fatal("original sequence not persisted", e, pending)
	}
	originalSequence := pending[0].Sequence
	if e = hs.Close(); e != nil {
		t.Fatal(e)
	}
	hs, e = store.Open(hubPath, "hub")
	if e != nil {
		t.Fatal(e)
	}
	relay.Store = hs
	cfg.Store = hs
	if e = relay.ReplayPending(ctx); e != nil {
		t.Fatal("real PostgreSQL replay after Hub restart", e)
	}
	pending, e = hs.PendingBackendUsage(ctx)
	if e != nil || len(pending) != 0 {
		t.Fatal(e, pending)
	}
	p, e := generated.New(f.pool).GetSourceProjection(ctx, generated.GetSourceProjectionParams{TenantID: f.tenant, SourceID: source})
	if e != nil || p.B != 9877 || p.F != 877 || p.Charged != 123 {
		t.Fatal("lost receipt double charged", e, p)
	}
	row, e := generated.New(f.pool).GetUsageTuple(ctx, generated.GetUsageTupleParams{NodeID: node.NodeID, BootID: node.BootID, Sequence: int64(originalSequence)})
	if e != nil || row.BatchID != batch.BatchId {
		t.Fatal("original Agent tuple absent from PG", e)
	}
	// The live Agent receives its actual Hub acknowledgement after replay, then
	// a new Engine unknown fact survives Agent boot recovery through both TLS legs.
	connectCtx, stopConnect := context.WithCancel(ctx)
	done := make(chan error, 1)
	go func() { done <- client.Connect(connectCtx, nodeListener.Addr().String()) }()
	for {
		pending, e := agentStore.PendingUsageOutbox(ctx, node.NodeID)
		if e != nil {
			t.Fatal(e)
		}
		if len(pending) == 0 {
			break
		}
		select {
		case e := <-done:
			t.Fatal("Agent replay receipt", e)
		case <-time.After(10 * time.Millisecond):
		}
	}
	stopConnect()
	<-done
	unknownGrant, e := (service.MeteringService{Pool: f.pool}).ReserveUsage(ctx, f.scope, service.ReserveInput{SourceID: source, SnapshotID: snapshot, NodeID: node.NodeID, BootID: node.BootID, Nonce: uuid.NewString(), RawBytes: 1000, Earliest: now, Latest: now, Deadline: now.Add(time.Hour)}, uuid.NewString())
	if e != nil {
		t.Fatal(e)
	}
	unknownID := uuid.NewString()
	if e = node.PersistUsage(ctx, engineaccounting.UsageRecord{BatchID: unknownID, SnapshotID: snapshot, RuleID: b.RuleID, AuthorizationID: unknownGrant.ID, Direction: engineaccounting.LeftToRight, Resolution: engineaccounting.Unknown, Reserved: 1000, Used: 1000}); e != nil {
		t.Fatal(e)
	}
	oldBoot := node.BootID
	restarted, e := agent.New(node.NodeID, agentStore, nil)
	if e != nil || restarted.BootID == oldBoot {
		t.Fatal("Agent boot recovery", e)
	}
	firstReportBoot := restarted.BootID
	backend.mu.Lock()
	backend.Drop = true
	backend.mu.Unlock()
	if e = (&agent.ControlClient{Node: restarted, TLS: nodeTLS}).Connect(ctx, nodeListener.Addr().String()); e == nil {
		t.Fatal("unknown usage acknowledged before dropped backend receipt")
	}
	restarted, e = agent.New(node.NodeID, agentStore, nil)
	if e != nil || restarted.BootID == firstReportBoot {
		t.Fatal("second Agent boot recovery", e)
	}
	connectCtx, stopConnect = context.WithCancel(ctx)
	defer stopConnect()
	go func() {
		done <- (&agent.ControlClient{Node: restarted, TLS: nodeTLS}).Connect(connectCtx, nodeListener.Addr().String())
	}()
	for {
		pending, e := agentStore.PendingUnknownUsage(ctx, node.NodeID)
		if e != nil {
			t.Fatal(e)
		}
		if len(pending) == 0 {
			break
		}
		select {
		case e := <-done:
			t.Fatal("Agent unknown receipt", e)
		case <-time.After(10 * time.Millisecond):
		}
	}
	stopConnect()
	<-done
	u, e := generated.New(f.pool).GetUnknownLiability(ctx, generated.GetUnknownLiabilityParams{TenantID: f.tenant, GrantID: unknownGrant.ID})
	if e != nil || u.UnknownRaw != 1000 || u.UnknownBill != 1000 {
		t.Fatal("boot restart did not freeze original source", e, u)
	}
	reportBoot, reportSequence, e := hs.UsageOrigin(ctx, node.NodeID, unknownID)
	if e != nil || reportBoot != firstReportBoot || reportSequence == 0 {
		t.Fatal("unknown report provenance", e)
	}
	unknownTuple, e := generated.New(f.pool).GetUsageTuple(ctx, generated.GetUsageTupleParams{NodeID: node.NodeID, BootID: reportBoot, Sequence: int64(reportSequence)})
	if e != nil || unknownTuple.BatchID != unknownID {
		t.Fatal("unknown tuple absent", e)
	}
	p, e = generated.New(f.pool).GetSourceProjection(ctx, generated.GetSourceProjectionParams{TenantID: f.tenant, SourceID: source})
	if e != nil || p.B != 9877 || p.F != 1877 || p.Charged != 123 {
		t.Fatal("unknown boot released or charged funds", e, p)
	}
	// Existing local facts without a captured envelope cannot be invented into a
	// new tuple; the production transport leaves them pending.
	if _, e = backend.Client.AcceptUsage(ctx, store.HubUsage{NodeID: node.NodeID, BootID: node.BootID, BatchID: "legacy"}); !errors.Is(e, store.ErrUsageOriginMissing) {
		t.Fatal("legacy sequence synthesized", e)
	}
}
