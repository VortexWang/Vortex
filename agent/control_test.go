package agent

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"net"
	"path/filepath"
	"testing"
	"time"

	"google.golang.org/protobuf/proto"
	"vortex/agent/identity"
	"vortex/agent/store"
	controlv1 "vortex/contracts/protobuf/v1"
	"vortex/engine/apply"
	"vortex/hub"
)

const controlTestNodeID = "550e8400-e29b-41d4-a716-446655440000"

func controlTLS(t *testing.T, clientID string) (*tls.Config, *tls.Config, *x509.CertPool) {
	t.Helper()
	now := time.Now().UTC()
	ca, caDER, err := identity.NewCA(now)
	if err != nil {
		t.Fatal(err)
	}
	issue := func(id string) identity.Credential {
		key, csr, err := identity.GenerateCSR(id)
		if err != nil {
			t.Fatal(err)
		}
		cred, err := ca.SignCSR(csr, id, now, time.Hour)
		if err != nil {
			t.Fatal(err)
		}
		cred, err = identity.AttachKey(cred, key, caDER)
		if err != nil {
			t.Fatal(err)
		}
		return cred
	}
	pool := x509.NewCertPool()
	pool.AddCert(ca.Certificate)
	serverTLS, err := identity.TLSConfig(issue("hub"), pool, nil, true)
	if err != nil {
		t.Fatal(err)
	}
	clientTLS, err := identity.TLSConfig(issue(clientID), pool, nil, false)
	if err != nil {
		t.Fatal(err)
	}
	clientTLS.ServerName = "hub"
	return serverTLS, clientTLS, pool
}

func startTestControlHub(t *testing.T, ctx context.Context, server *hub.TestServer, tlsConfig *tls.Config) net.Listener {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go func() { _ = server.ServeGRPC(ctx, listener, tlsConfig) }()
	t.Cleanup(func() { _ = listener.Close() })
	return listener
}

func TestControlClientAppliesRuntimeConfigOverMutualTLSGRPC(t *testing.T) {
	ctx := context.Background()
	serverTLS, clientTLS, _ := controlTLS(t, controlTestNodeID)
	hubStore, err := store.Open(filepath.Join(t.TempDir(), "hub.db"), "hub")
	if err != nil {
		t.Fatal(err)
	}
	defer hubStore.Close()
	nodeStore, err := store.Open(filepath.Join(t.TempDir(), "agent.db"), "agent")
	if err != nil {
		t.Fatal(err)
	}
	defer nodeStore.Close()
	manager := apply.NewManager()
	reconciler := &Reconciler{Store: nodeStore, Apply: EngineApplier{Manager: manager}}
	payload := []byte(`{"version":2,"node_id":"` + controlTestNodeID + `","listeners":[],"rules":[],"revocations":[]}`)
	digest := sha256.Sum256(payload)
	update := &controlv1.RuntimeConfigUpdate{Version: 2, Payload: payload, TransactionId: []byte("0123456789abcdef"), FencingGeneration: 1, PayloadSha256: digest[:], ExpectedVersion: 0}
	server := &hub.TestServer{Store: hubStore, ConfigUpdate: update}
	listener := startTestControlHub(t, ctx, server, serverTLS)
	node, err := New(controlTestNodeID, nodeStore, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := node.Start(ctx); err != nil {
		t.Fatal(err)
	}
	client := &ControlClient{Node: node, TLS: clientTLS, Reconciler: reconciler, MaxAttempts: 1}
	if err := client.Connect(ctx, listener.Addr().String()); err != nil {
		t.Fatal(err)
	}
	if applied, err := nodeStore.AgentState(ctx, "applied_version"); err != nil || applied != "2" {
		t.Fatalf("applied version=%q err=%v", applied, err)
	}
	if receipts, err := hubStore.ReceiptCount(ctx); err != nil || receipts != 1 {
		t.Fatalf("Hub receipts=%d err=%v", receipts, err)
	}
	if events, err := hubStore.EventCount(ctx); err != nil || events != 1 {
		t.Fatalf("Hub apply result events=%d err=%v", events, err)
	}
	if contact, err := nodeStore.AgentState(ctx, "hub_contact"); err != nil || contact == "" {
		t.Fatalf("Hub contact=%q err=%v", contact, err)
	}
	if _, err := nodeStore.AgentState(ctx, "backend_heartbeat"); err == nil {
		t.Fatal("Hub control stream incorrectly recorded a backend heartbeat")
	}
}

func TestControlClientHandsOffDurableUsageBeforeHubAck(t *testing.T) {
	ctx := context.Background()
	serverTLS, clientTLS, _ := controlTLS(t, controlTestNodeID)
	hubStore, err := store.Open(filepath.Join(t.TempDir(), "hub.db"), "hub")
	if err != nil {
		t.Fatal(err)
	}
	defer hubStore.Close()
	nodeStore, err := store.Open(filepath.Join(t.TempDir(), "agent.db"), "agent")
	if err != nil {
		t.Fatal(err)
	}
	defer nodeStore.Close()
	node, err := New(controlTestNodeID, nodeStore, nil)
	if err != nil {
		t.Fatal(err)
	}
	batch := &controlv1.UsageBatch{BatchId: "batch-1", BillingSnapshotId: "snapshot-1", Deltas: []*controlv1.UsageDelta{{RuleId: "rule-1", Role: "source", Direction: "egress", Bytes: 512, GrantId: "grant-1"}}}
	if err := node.PersistUsageBatch(ctx, batch, 0); err != nil {
		t.Fatal(err)
	}
	unknown := &controlv1.UsageBatch{BatchId: "unknown-batch", BillingSnapshotId: "550e8400-e29b-41d4-a716-446655440024", Deltas: []*controlv1.UsageDelta{{RuleId: "rule-1", Role: "source", Direction: "ingress", Bytes: 1024, GrantId: "550e8400-e29b-41d4-a716-446655440021"}}}
	if err := node.PersistUsageBatch(ctx, unknown, 1024); err != nil {
		t.Fatal(err)
	}
	server := &hub.TestServer{Store: hubStore, AcceptUsage: true, UsageBatchLimit: 2}
	listener := startTestControlHub(t, ctx, server, serverTLS)
	client := &ControlClient{Node: node, TLS: clientTLS}
	if err := client.Connect(ctx, listener.Addr().String()); err != nil {
		t.Fatal(err)
	}
	if pending, err := nodeStore.PendingUsageOutbox(ctx, controlTestNodeID); err != nil || len(pending) != 1 || pending[0].BatchID != "unknown-batch" || pending[0].UnknownUpper != 1024 {
		t.Fatalf("known ACK did not retain unknown liability=%+v err=%v", pending, err)
	}
	if pending, err := nodeStore.PendingUnknownUsage(ctx, controlTestNodeID); err != nil || len(pending) != 0 {
		t.Fatalf("typed handoff pending: %+v %v", pending, err)
	}
	if err := nodeStore.AcknowledgeUsageBatches(ctx, controlTestNodeID, []string{"unknown-batch"}); err == nil {
		t.Fatal("ordinary ACK accepted unsent unknown record")
	}
	if err := hubStore.PersistUsageLedger(ctx, store.UsageBatch{NodeID: controlTestNodeID, BootID: "replay-boot", BatchID: batch.GetBatchId(), SnapshotID: batch.GetBillingSnapshotId(), Payload: pendingPayload(t, batch)}); err != nil {
		t.Fatalf("Hub ledger replay was not idempotent: %v", err)
	}
}

func pendingPayload(t *testing.T, batch *controlv1.UsageBatch) []byte {
	t.Helper()
	payload, err := (proto.MarshalOptions{Deterministic: true}).Marshal(batch)
	if err != nil {
		t.Fatal(err)
	}
	return payload
}

type capturedUsageStream struct {
	controlv1.NodeControl_ConnectClient
	sent []*controlv1.NodeMessage
}

func (s *capturedUsageStream) Send(message *controlv1.NodeMessage) error {
	s.sent = append(s.sent, message)
	return nil
}

func TestPendingUnknownUsageNeverUsesOrdinaryWireOrAck(t *testing.T) {
	ctx := context.Background()
	s, err := store.Open(filepath.Join(t.TempDir(), "agent.db"), "agent")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	node, err := New(controlTestNodeID, s, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		id    string
		upper uint64
	}{{"unknown", 1024}, {"known", 0}} {
		batch := &controlv1.UsageBatch{BatchId: tc.id, BillingSnapshotId: "550e8400-e29b-41d4-a716-446655440024", Deltas: []*controlv1.UsageDelta{{RuleId: "rule", Role: "source", Direction: "egress", Bytes: 1024, GrantId: "550e8400-e29b-41d4-a716-446655440021"}}}
		if err := node.PersistUsageBatch(ctx, batch, tc.upper); err != nil {
			t.Fatal(err)
		}
	}
	stream := &capturedUsageStream{}
	var sequence uint64
	for range 2 {
		if err := (&ControlClient{Node: node}).sendPendingUsage(ctx, stream, &sequence); err != nil {
			t.Fatal(err)
		}
	}
	if len(stream.sent) != 4 {
		t.Fatalf("unexpected sent count: %d", len(stream.sent))
	}
	known, unknown := 0, 0
	for _, message := range stream.sent {
		if ordinary := message.GetUsageBatch(); ordinary != nil {
			if ordinary.GetBatchId() != "known" {
				t.Fatal("unknown liability sent as consumed bytes")
			}
			known++
		} else if typed := message.GetUnknownUsageBatch(); typed != nil {
			if typed.GetBatchId() != "unknown" || typed.GetUnknownUpperBound() != 1024 {
				t.Fatal("wrong unknown wire")
			}
			unknown++
		} else {
			t.Fatal("unexpected wire message")
		}
	}
	if known != 2 || unknown != 2 {
		t.Fatal("missing idempotent wire retry")
	}
	if err := s.AcknowledgeUsageBatches(ctx, controlTestNodeID, []string{"known", "unknown"}); err == nil {
		t.Fatal("mixed ACK acknowledged unknown liability")
	}
	pending, err := s.PendingUsageOutbox(ctx, controlTestNodeID)
	if err != nil || len(pending) != 2 {
		t.Fatalf("partial ACK changed outbox: %+v %v", pending, err)
	}
	if err := s.AcknowledgeUsageBatches(ctx, controlTestNodeID, []string{"known"}); err != nil {
		t.Fatal(err)
	}
	typed, err := s.PendingUnknownUsage(ctx, controlTestNodeID)
	if err != nil || len(typed) != 1 || len(typed[0].LegacyBeforeImage) == 0 {
		t.Fatalf("missing migration before-image: %+v %v", typed, err)
	}
}

func TestControlClientRejectsCertificateNodeMismatch(t *testing.T) {
	ctx := context.Background()
	serverTLS, clientTLS, _ := controlTLS(t, "550e8400-e29b-41d4-a716-446655440001")
	hubStore, err := store.Open(filepath.Join(t.TempDir(), "hub.db"), "hub")
	if err != nil {
		t.Fatal(err)
	}
	defer hubStore.Close()
	nodeStore, err := store.Open(filepath.Join(t.TempDir(), "agent.db"), "agent")
	if err != nil {
		t.Fatal(err)
	}
	defer nodeStore.Close()
	listener := startTestControlHub(t, ctx, &hub.TestServer{Store: hubStore}, serverTLS)
	node, err := New(controlTestNodeID, nodeStore, nil)
	if err != nil {
		t.Fatal(err)
	}
	err = (&ControlClient{Node: node, TLS: clientTLS, MaxAttempts: 1}).Connect(ctx, listener.Addr().String())
	if err == nil {
		t.Fatal("control session with mismatched certificate identity was accepted")
	}
	if count, err := hubStore.ReceiptCount(ctx); err != nil || count != 0 {
		t.Fatalf("mismatched identity persisted receipt count=%d err=%v", count, err)
	}
}

func TestControlRunnerReconnectsAfterServerClosesStream(t *testing.T) {
	serverTLS, clientTLS, _ := controlTLS(t, controlTestNodeID)
	hubStore, err := store.Open(filepath.Join(t.TempDir(), "hub.db"), "hub")
	if err != nil {
		t.Fatal(err)
	}
	defer hubStore.Close()
	nodeStore, err := store.Open(filepath.Join(t.TempDir(), "agent.db"), "agent")
	if err != nil {
		t.Fatal(err)
	}
	defer nodeStore.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	listener := startTestControlHub(t, ctx, &hub.TestServer{Store: hubStore}, serverTLS)
	node, err := New(controlTestNodeID, nodeStore, nil)
	if err != nil {
		t.Fatal(err)
	}
	client := &ControlClient{Node: node, TLS: clientTLS, Interval: 10 * time.Millisecond, RetryMin: time.Millisecond, RetryMax: 10 * time.Millisecond}
	runErr := make(chan error, 1)
	go func() { runErr <- client.Run(ctx, listener.Addr().String()) }()
	deadline := time.Now().Add(2 * time.Second)
	for {
		count, countErr := hubStore.ReceiptCount(context.Background())
		if countErr == nil && count >= 2 {
			cancel()
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("control stream did not reconnect, receipts=%d err=%v", count, countErr)
		}
		time.Sleep(5 * time.Millisecond)
	}
	if err := <-runErr; !errors.Is(err, context.Canceled) {
		t.Fatalf("runner result=%v", err)
	}
}

func TestAuthenticatedAuthorizationCarriesBackendHeartbeatNotHubContact(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	serverTLS, clientTLS, _ := controlTLS(t, controlTestNodeID)
	hs, err := store.Open(filepath.Join(t.TempDir(), "hub.db"), "hub")
	if err != nil {
		t.Fatal(err)
	}
	defer hs.Close()
	ns, err := store.Open(filepath.Join(t.TempDir(), "agent.db"), "agent")
	if err != nil {
		t.Fatal(err)
	}
	defer ns.Close()
	node, err := New(controlTestNodeID, ns, nil)
	if err != nil {
		t.Fatal(err)
	}
	listener := startTestControlHub(t, ctx, &hub.TestServer{Store: hs, AuthorizationUpdate: func(id, boot string) (*controlv1.AuthorizationUpdate, error) {
		return &controlv1.AuthorizationUpdate{Generation: 4, Payload: authorizationPayloadForTest(t, id, boot, 1, 4)}, nil
	}}, serverTLS)
	if err := (&ControlClient{Node: node, TLS: clientTLS}).Connect(ctx, listener.Addr().String()); err != nil {
		t.Fatal(err)
	}
	if _, _, trusted := node.TrustedTime(); !trusted {
		t.Fatal("authenticated boot-bound update did not establish time")
	}
	base := time.Date(2026, 11, 5, 12, 0, 0, 0, time.UTC)
	status, err := ns.CurrentAutonomyStatus(ctx, base.Add(30*time.Second), true)
	if err != nil || status.BackendHeartbeat != base || status.HardDeadline != base.Add(12*time.Hour) || status.Mode != store.AutonomyOffline {
		t.Fatalf("payload heartbeat not persisted: %+v %v", status, err)
	}
	// A normal Hub reconnect cannot replace the backend evidence or restart grace.
	plain := startTestControlHub(t, ctx, &hub.TestServer{Store: hs}, serverTLS)
	if err := (&ControlClient{Node: node, TLS: clientTLS}).Connect(ctx, plain.Addr().String()); err != nil {
		t.Fatal(err)
	}
	after, err := ns.CurrentAutonomyStatus(ctx, base.Add(time.Minute), true)
	if err != nil || after.BackendHeartbeat != base || after.HardDeadline != status.HardDeadline || after.SignerEpochOpen {
		t.Fatalf("Hub reconnect moved authority: %+v %v", after, err)
	}
}
