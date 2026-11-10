package service_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"errors"
	"github.com/google/uuid"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"net"
	"testing"
	"time"
	"vortex/agent"
	"vortex/agent/store"
	"vortex/backend/db/generated"
	"vortex/backend/internal/auth"
	"vortex/backend/internal/control"
	"vortex/backend/internal/policy"
	"vortex/backend/internal/service"
	"vortex/backend/internal/worker"
	controlv1 "vortex/contracts/protobuf/v1"
)

func TestNodeRecoverySourcesActualMTLSLostFULLReceiptKeepsHubHeld(t *testing.T) {
	x, f, st := preparedCreditFence(t)

	q0 := generated.New(x.f.pool)
	inventory, e := q0.HubAllocationIssueInventory(context.Background(), generated.HubAllocationIssueInventoryParams{TenantID: x.f.tenant, AllocationID: f.AllocationID})
	if e != nil || len(inventory) != 1 {
		t.Fatal(inventory, e)
	}
	issue, e := q0.GetHubIssue(context.Background(), generated.GetHubIssueParams{TenantID: x.f.tenant, IssueID: inventory[0].IssueID})
	if e != nil {
		t.Fatal(e)
	}
	fence, e := store.DecodeRecoveryCreditFence(f.Payload)
	if e != nil {
		t.Fatal(e)
	}
	facts, e := agent.InspectAuthorizationUpdate(issue.Payload, f.NodeID, f.BootID, fence.Root.PathGeneration)
	if e != nil {
		t.Fatal(e)
	}
	credit, e := agent.InspectIssuedCreditGrant(issue.Payload, f.NodeID, f.BootID, fence.HubID, fence.Credits[0].GrantID, fence.Root.PathGeneration)
	if e != nil {
		t.Fatal(e)
	}
	h := sha256.Sum256(issue.Payload)
	if e = st.PersistAuthorizationUpdate(context.Background(), store.AuthorizationUpdate{ShareReferences: facts.ShareReferences, IssuerID: fence.HubID, BackendHeartbeat: facts.BackendHeartbeat, AutonomyDeadline: facts.AutonomyDeadline, NodeID: f.NodeID, BootID: f.BootID, UpdateID: facts.UpdateID, Payload: issue.Payload, Digest: h[:], SignerEpoch: facts.SignerEpoch, PathGeneration: facts.PathGeneration, FencingGenerationFloor: facts.FencingFloor}, []store.AuthorizationRecord{credit}, nil); e != nil {
		t.Fatal(e)
	}
	localBefore, e := st.AuditAuthorizationBalance(context.Background(), f.NodeID, credit.AuthorizationID)
	if e != nil {
		t.Fatal(e)
	}
	recordRecoveryFence(t, x, f, st)
	p, e := x.s.PrepareNodeRecoverySources(context.Background(), policyScope(t, x.f.tenant, auth.PlatformAdmin, policy.Grant), f.RecoveryID, f.AllocationID, uuid.NewString())
	if e != nil {
		t.Fatal(e)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	node, e := agent.New(p.NodeID, st, nil)
	if e != nil {
		t.Fatal(e)
	}
	// Exact already authenticated incarnation is pinned by the PG proof; no
	// enrollment/Engine stop claim is made by this adapter fixture.
	node.BootID = p.BootID
	backendCA, backendKey := portCA(t)
	backendRoots, nodeRoots := x509.NewCertPool(), x509.NewCertPool()
	backendRoots.AddCert(backendCA)
	nodeRoots.AddCert(x.s.NodeCA)
	serverTLS := &tls.Config{Certificates: []tls.Certificate{portCertificate(t, backendCA, backendKey, shareBackendID, x509.ExtKeyUsageServerAuth)}, ClientCAs: nodeRoots, ClientAuth: tls.RequireAndVerifyClientCert, MinVersion: tls.VersionTLS13, MaxVersion: tls.VersionTLS13}
	clientTLS := &tls.Config{Certificates: []tls.Certificate{portCertificate(t, x.s.NodeCA, x.f.caKey, p.NodeID, x509.ExtKeyUsageClientAuth)}, RootCAs: backendRoots, ServerName: shareBackendID, MinVersion: tls.VersionTLS13, MaxVersion: tls.VersionTLS13}
	ready := make(chan struct{}, 8)
	adapter := &control.Adapter{Shares: &x.s, Observe: func(context.Context, string, string, []byte) error { ready <- struct{}{}; return nil }}
	server := grpc.NewServer(grpc.Creds(credentials.NewTLS(serverTLS)))
	defer server.Stop()
	controlv1.RegisterNodeControlServer(server, &loseSourceReceipt{Adapter: adapter})
	listener, e := net.Listen("tcp", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	defer listener.Close()
	go server.Serve(listener)
	connect := func(pinned bool) (context.CancelFunc, <-chan error) {
		run, stop := context.WithCancel(ctx)
		client := &agent.ControlClient{Node: node, TLS: clientTLS}
		if pinned {
			client.SourceBackendID = shareBackendID
			client.ControlOnly = true
		}
		done := make(chan error, 1)
		go func() { done <- client.Connect(run, listener.Addr().String()) }()
		select {
		case <-ready:
		case e := <-done:
			t.Fatal(e)
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		}
		return stop, done
	}
	finish := func(stop context.CancelFunc, done <-chan error) {
		stop()
		select {
		case <-done:
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		}
	}
	held := authoritySnapshot(t, x.f.pool, x.f.tenant, []string{"reservations", "ledger_projections", "ledger_events", "hub_credit_grants", "share_chains", "share_generations", "settlements"})
	w := worker.NodeRecoverySourceWorker{Service: x.s, BackendID: shareBackendID, Adapter: adapter}
	stop, done := connect(false)
	if _, e = w.Once(ctx); e == nil {
		t.Fatal("unconfigured backend capability accepted")
	}
	finish(stop, done)
	stop, done = connect(true)
	if _, e = w.Once(ctx); e == nil {
		t.Fatal("lost FULL receipt committed")
	}
	finish(stop, done)
	q := generated.New(x.f.pool)
	if _, e = q.GetNodeRecoverySourceReceipt(ctx, generated.GetNodeRecoverySourceReceiptParams{TenantID: x.f.tenant, ProvisionID: p.ID}); e == nil {
		t.Fatal("missing receipt became PG proof")
	}
	stop, done = connect(true)
	if worked, e := w.Once(ctx); e != nil || !worked {
		t.Fatal("exact FULL replay", worked, e)
	}
	if worked, e := w.Once(ctx); e != nil || worked {
		t.Fatal("accepted recovery remained pending", worked, e)
	}
	finish(stop, done)
	if !bytes.Equal(held, authoritySnapshot(t, x.f.pool, x.f.tenant, []string{"reservations", "ledger_projections", "ledger_events", "hub_credit_grants", "share_chains", "share_generations", "settlements"})) {
		t.Fatal("transport changed funds or capacity")
	}
	saved, e := q.GetNodeRecoverySourceReceipt(ctx, generated.GetNodeRecoverySourceReceiptParams{TenantID: x.f.tenant, ProvisionID: p.ID})
	if e != nil {
		t.Fatal(e)
	}
	var received store.SourceProvisionReceipt
	if json.Unmarshal(saved.Payload, &received) != nil || received.ID != p.ID || received.Digest != hex.EncodeToString(p.PayloadDigest) {
		t.Fatal("exact successor root receipt not retained", received)
	}
	op, e := q.GetOperation(ctx, generated.GetOperationParams{TenantID: x.f.tenant, ID: p.OperationID})
	if e != nil || op.State != "succeeded" {
		t.Fatal(op, e)
	}
	localAfter, e := st.AuditAuthorizationBalance(ctx, f.NodeID, credit.AuthorizationID)
	if e != nil || localBefore != localAfter || localAfter.Initial != 37 {
		t.Fatal("old local credit changed", localBefore, localAfter, e)
	}
	approval, e := q.HubSameStoreRecoveryByID(ctx, f.RecoveryID)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = x.s.BindHubSession(x.f.contexts[approval.HubID], approval.HubID, approval.ToBootID, uuid.NewString()); !errors.Is(e, service.ErrShareUnknown) {
		t.Fatal("roots admitted Hub", e)
	}
	t.Log("actual role-mTLS direct successor roots, lost SQLite FULL receipt, exact reconnect replay; no funds, share state or Hub admission changed")
}
