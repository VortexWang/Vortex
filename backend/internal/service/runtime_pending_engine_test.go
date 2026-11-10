package service_test

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"vortex/agent"
	"vortex/agent/store"
	"vortex/backend/db/generated"
	"vortex/backend/internal/auth"
	"vortex/backend/internal/control"
	"vortex/backend/internal/policy"
	"vortex/backend/internal/service"
	controlv1 "vortex/contracts/protobuf/v1"
	"vortex/engine/apply"
	"vortex/hub"
)

type pendingRecoveryRule struct {
	node       *agent.Node
	reconciler *agent.Reconciler
	applier    *agent.RuntimeApplier
	prepare    *controlv1.RuleCommand
	commitTask string
	transport  *control.Adapter
}

func preparePendingRecoveryRule(t *testing.T, x *shareFixture, ns *store.Store, a hub.Allocation, n *agent.Node) *pendingRecoveryRule {
	t.Helper()
	ctx := context.Background()
	// Keep the existing live Node/boot; calling New here would retire its credit.
	dir := t.TempDir()
	ca, key := portCA(t)
	cert := portCertificate(t, ca, key, n.NodeID, x509.ExtKeyUsageClientAuth)
	leaf, err := x509.ParseCertificate(cert.Certificate[0])
	if err != nil {
		t.Fatal(err)
	}
	uri, _ := url.Parse("urn:vortex:node:" + n.NodeID)
	leaf.URIs = []*url.URL{uri}
	signed, err := x509.CreateCertificate(rand.Reader, leaf, ca, leaf.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	cert.Certificate[0] = signed

	certPath, keyPath, rootPath := filepath.Join(dir, "cert.pem"), filepath.Join(dir, "key.pem"), filepath.Join(dir, "ca.pem")
	der, err := x509.MarshalPKCS8PrivateKey(cert.PrivateKey)
	if err != nil {
		t.Fatal(err)
	}
	for path, raw := range map[string][]byte{certPath: pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: cert.Certificate[0]}), keyPath: pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der}), rootPath: pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: ca.Raw})} {
		if err = os.WriteFile(path, raw, 0600); err != nil {
			t.Fatal(err)
		}
	}
	config := filepath.Join(dir, "engine.json")
	raw := fmt.Sprintf(`{"node_id":%q,"role":"destination","certificate":%q,"private_key":%q,"trust_bundle":%q,"listen":"127.0.0.1:0","version":1,"rule_id":"legacy","rule_revision":1,"path_generation":1,"target":"127.0.0.1:9","execution_point":true}`, n.NodeID, certPath, keyPath, rootPath)
	if err = os.WriteFile(config, []byte(raw), 0600); err != nil {
		t.Fatal(err)
	}
	engine, err := agent.NewRuntimeEngine(config, n.NodeID)
	if err != nil {
		t.Fatal(err)
	}
	n.Engine = engine
	applier, err := n.RuntimeApplier()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if err := applier.Stop(ctx); err != nil {
			t.Error(err)
		}
	})
	q := generated.New(x.f.pool)
	for _, kind := range []string{"rate", "connections"} {
		h, err := q.ShareHandoffByGeneration(ctx, generated.ShareHandoffByGenerationParams{TenantID: x.f.tenant, NodeID: n.NodeID, Kind: kind, Generation: 1})
		if err != nil {
			t.Fatal(err)
		}
		if _, err = ns.ApplyShareHandoff(ctx, n.NodeID, n.BootID, shareBackendID, "cas", h.Command, store.IssuerTime{Earliest: x.now.Add(time.Second), Latest: x.now.Add(time.Second), Trusted: true}); err != nil {
			t.Fatal(err)
		}
	}

	// The immutable deployment has two members. Reserve both kinds for its other
	// member through the production service before asking PG for a real Commit.
	other, err := q.GetNodeIncarnation(ctx, x.f.nodes[1])
	if err != nil {
		t.Fatal(err)
	}
	signer := uuid.NewString()
	if _, err = x.s.AuthorizeSigner(ctx, policyScope(t, x.f.tenant, auth.PlatformAdmin, policy.Grant), signer, 7, x.now.Add(-time.Second), x.now.Add(time.Hour), make([]byte, 32), uuid.NewString()); err != nil {
		t.Fatal(err)
	}
	for _, kind := range []string{"rate", "connections"} {
		in := x.in
		in.NodeID = x.f.nodes[1]
		in.BootID = other.BootID
		in.SignerID = signer
		in.Kind = kind
		in.Amount = 100000
		in.BurstBytes = 12500
		in.Nonce = uuid.NewString()
		if kind == "connections" {
			in.Amount = 1
			in.BurstBytes = 0
		}
		if _, err = x.s.PrepareShare(ctx, x.f.scope, in, uuid.NewString()); err != nil {
			t.Fatal(err)
		}
	}
	var prepareTask, commitTask string
	for _, item := range []struct {
		phase string
		out   *string
	}{{"prepare", &prepareTask}, {"commit", &commitTask}} {
		if err = x.f.pool.QueryRow(ctx, `SELECT b.task_id FROM rule_delivery_bindings b JOIN rule_deployments d ON d.tenant_id=b.tenant_id AND d.id=b.deployment_id WHERE b.tenant_id=$1 AND d.rule_id=$2 AND b.node_id=$3 AND b.phase=$4`, x.f.tenant, a.RuleID, n.NodeID, item.phase).Scan(item.out); err != nil {
			t.Fatal(err)
		}
	}
	command, _, _, err := service.RuleCommand(ctx, q, x.f.tenant, prepareTask)
	if err != nil {
		t.Fatal(err)
	}
	reconciler := &agent.Reconciler{Store: ns, Apply: applier}
	if r := reconciler.ApplyRuleCommand(ctx, n.NodeID, command); r.State != "prepared" {
		t.Fatal("original actual Engine Prepare", r)
	}
	return &pendingRecoveryRule{node: n, reconciler: reconciler, applier: applier, prepare: command, commitTask: commitTask}
}

func (p *pendingRecoveryRule) assertPending(t *testing.T, ctx context.Context, x *shareFixture) {
	t.Helper()
	command, _, _, err := service.RuleCommand(ctx, generated.New(x.f.pool), x.f.tenant, p.commitTask)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(command.Payload, p.prepare.Payload) {
		t.Fatal("S/R rewrote original pending artifact")
	}
	if r := p.deliver(t, ctx, command); r.State != "unknown" {
		t.Fatal("old share revisions activated pending Engine", r)
	}
	var id [16]byte
	copy(id[:], command.TransactionId)
	if tx, ok := p.applier.Query(id); !ok || tx.State != apply.Prepared {
		t.Fatal("pending Commit lost same live executor", tx, ok)
	}
	if ok, err := p.node.Store.PendingRuleCommit(ctx, fmt.Sprintf("%x", id), p.applier.ExecutionID()); err != nil || !ok {
		record, readErr := p.node.Store.GetConfigResult(ctx, fmt.Sprintf("%x", id))
		t.Fatal("missing exact pending Commit witness", ok, err, record.State, record.Reason, readErr)
	}

}

func (p *pendingRecoveryRule) commit(t *testing.T, ctx context.Context, x *shareFixture) {
	t.Helper()
	var preparedID [16]byte
	copy(preparedID[:], p.prepare.TransactionId)
	if tx, ok := p.applier.Query(preparedID); !ok || tx.State != apply.Prepared {
		t.Fatal("renewal silently committed Engine", tx, ok)
	}

	command, _, _, err := service.RuleCommand(ctx, generated.New(x.f.pool), x.f.tenant, p.commitTask)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(command.Payload, p.prepare.Payload) {
		t.Fatal("renewal rewrote original pending artifact")
	}
	if r := p.deliver(t, ctx, command); r.State != "active" {
		t.Fatal("current recovered authority did not activate original Engine transaction", r)
	}
	var id [16]byte
	copy(id[:], command.TransactionId)
	if tx, ok := p.applier.Query(id); !ok || tx.State != apply.Active {
		t.Fatal(tx, ok)
	}
}

func (p *pendingRecoveryRule) deliver(t *testing.T, ctx context.Context, c *controlv1.RuleCommand) *controlv1.RuleReceipt {
	t.Helper()
	if p.transport == nil {
		t.Fatal("actual direct control transport missing")
	}
	r, err := p.transport.DeliverRule(ctx, p.node.NodeID, p.node.BootID, c)
	if err != nil {
		t.Fatal("actual direct rule delivery", err)
	}
	return r
}
func (p *pendingRecoveryRule) startControl(t *testing.T, ctx context.Context, serverTLS, clientTLS *tls.Config) func() {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ready := make(chan struct{}, 1)
	adapter := &control.Adapter{Observe: func(context.Context, string, string, []byte) error {
		select {
		case ready <- struct{}{}:
		default:
		}
		return nil
	}}
	server := grpc.NewServer(grpc.Creds(credentials.NewTLS(serverTLS)))
	controlv1.RegisterNodeControlServer(server, adapter)
	go server.Serve(listener)
	run, stop := context.WithCancel(ctx)
	done := make(chan error, 1)
	go func() {
		done <- (&agent.ControlClient{Node: p.node, Reconciler: p.reconciler, TLS: clientTLS, ControlOnly: true, SourceBackendID: shareBackendID}).Connect(run, listener.Addr().String())
	}()
	cleanup := func() {
		stop()
		server.Stop()
		listener.Close()
		select {
		case <-done:
		case <-time.After(3 * time.Second):
			t.Error("direct pending Engine client did not join")
		}
	}
	select {
	case <-ready:
	case err := <-done:
		server.Stop()
		listener.Close()
		stop()
		t.Fatal(err)
	case <-ctx.Done():
		cleanup()
		t.Fatal(ctx.Err())
	}
	p.transport = adapter
	if r := p.deliver(t, ctx, p.prepare); r.State != "prepared" {
		cleanup()
		t.Fatal("exact Prepare replay over direct mTLS", r)
	}
	return cleanup
}
