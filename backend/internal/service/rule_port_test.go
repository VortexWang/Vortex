package service_test

import (
	"context"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"vortex/agent"
	"vortex/agent/store"
	"vortex/backend/db/generated"
	"vortex/backend/internal/compiler"
	"vortex/backend/internal/control"
	"vortex/backend/internal/service"
	"vortex/backend/internal/testutil"
	"vortex/backend/internal/worker"
	controlv1 "vortex/contracts/protobuf/v1"
	"vortex/hub"
)

type portRuleAdapter func(context.Context, string, string, *controlv1.RuleCommand) (*controlv1.RuleReceipt, error)

func (f portRuleAdapter) DeliverRule(ctx context.Context, node, boot string, c *controlv1.RuleCommand) (*controlv1.RuleReceipt, error) {
	return f(ctx, node, boot, c)
}

func reserveRulePort(t *testing.T, f *portFixture, in service.RuleInput) {
	t.Helper()
	req := f.request(t, in.Port)
	req.RuleID = in.ID
	req.Protocols = []string{in.Protocol}
	if in.Protocol == "tcp_udp" {
		req.Protocols = []string{"tcp", "udp"}
	}
	if _, err := f.ports.PreparePortReservation(context.Background(), f.scope, req, uuid.NewString()); err != nil {
		t.Fatal(err)
	}
}

func TestRuleFixedPortRejectsUnallocatedCompilePaths(t *testing.T) {
	f := newPortFixture(t)
	ctx := context.Background()
	s := service.RuleService{Pool: f.pool}
	in := ruleInput(f.group)
	m, e := s.Mutate(ctx, f.scope, in, 0, uuid.NewString())
	if e != nil {
		t.Fatal(e)
	}
	b := durableBilling(t, f, in.ID, 1, 3)
	before := authoritySnapshot(t, f.pool, f.tenant, []string{"rule_compile_inputs", "rule_artifacts", "tasks", "operations", "outbox", "audit_events"})
	if _, e = s.CompileRevision(ctx, f.scope, in.ID, 1, b, 2, 3, 4, uuid.NewString()); e == nil {
		t.Fatal("legacy compile bypassed durable fixed port authority")
	}
	if _, e = s.BindCompileInput(ctx, f.scope, m.OperationID, b, 2, 3, 4, uuid.NewString()); e == nil {
		t.Fatal("fixed port compile binding bypassed allocation")
	}
	if string(before) != string(authoritySnapshot(t, f.pool, f.tenant, []string{"rule_compile_inputs", "rule_artifacts", "tasks", "operations", "outbox", "audit_events"})) {
		t.Fatal("rejected compile changed authority")
	}
}

func TestRuleAutoPortConsumesDurableAllocation(t *testing.T) {
	f := newPortFixture(t)
	ctx := context.Background()
	s := service.RuleService{Pool: f.pool}
	in := ruleInput(f.group)
	in.Protocol = "tcp_udp"
	in.Port = 0
	m, e := s.Mutate(ctx, f.scope, in, 0, uuid.NewString())
	if e != nil {
		t.Fatal(e)
	}
	req := f.request(t, 0)
	req.RuleID = in.ID
	a, e := f.ports.PreparePortReservation(ctx, f.scope, req, uuid.NewString())
	if e != nil {
		t.Fatal(e)
	}
	b := durableBilling(t, f, in.ID, 1, 3)
	digest, e := s.BindCompileInput(ctx, f.scope, m.OperationID, b, 2, 3, 4, uuid.NewString())
	if e != nil {
		t.Fatal(e)
	}
	request := uuid.NewString()
	failAudit(t, f.pool, request)
	tables := []string{"port_allocations", "port_reservations", "rule_artifacts", "rule_deployments", "rule_delivery_bindings", "operations", "tasks", "outbox", "audit_events"}
	before := authoritySnapshot(t, f.pool, f.tenant, tables)
	if e = s.ConsumeCompile(ctx, f.scope, m.OperationID, request); e == nil {
		t.Fatal("port handoff escaped failed audit")
	}
	if string(before) != string(authoritySnapshot(t, f.pool, f.tenant, tables)) {
		t.Fatal("partial port handoff")
	}
	if e = s.ConsumeCompile(ctx, f.scope, m.OperationID, uuid.NewString()); e != nil {
		t.Fatal(e)
	}
	rows, e := generated.New(f.pool).ListCommittedRuleArtifacts(ctx, generated.ListCommittedRuleArtifactsParams{TenantID: f.tenant, RuleID: in.ID, RuleRevision: 1, InputDigest: digest})
	if e != nil || len(rows) != len(f.nodes) {
		t.Fatal(e, len(rows))
	}
	for _, row := range rows {
		env, e := compiler.DecodeArtifact(row.Canonical, row.Digest)
		if e != nil {
			t.Fatal(e)
		}
		if env.Intent.Rule.Port != a.Port || env.Intent.Rule.Port == 0 {
			t.Fatalf("compiled port=%d; durable allocation=%d", env.Intent.Rule.Port, a.Port)
		}
	}
	var original compiler.Rule
	if e = json.Unmarshal(m.Revision.Payload, &original); e != nil || original.Port != 0 {
		t.Fatal("immutable auto intent changed", e, original.Port)
	}
	reservations, e := generated.New(f.pool).PortReservations(ctx, generated.PortReservationsParams{TenantID: f.tenant, AllocationID: a.ID})
	if e != nil {
		t.Fatal(e)
	}
	for _, r := range reservations {
		if r.ConfigConfirmed {
			t.Fatal("compile claimed active sockets")
		}
	}
}

func TestRulePortLegacyInflightCannotBeOverwritten(t *testing.T) {
	for _, state := range []string{"leased", "running", "unknown"} {
		t.Run(state, func(t *testing.T) {
			f := newPortFixture(t)
			ctx := context.Background()
			s := service.RuleService{Pool: f.pool}
			in := ruleInput(f.group)
			in.Port = 0
			in.Protocol = "tcp_udp"
			m, e := s.Mutate(ctx, f.scope, in, 0, uuid.NewString())
			if e != nil {
				t.Fatal(e)
			}
			req := f.request(t, 0)
			req.RuleID = in.ID
			a, e := f.ports.PreparePortReservation(ctx, f.scope, req, uuid.NewString())
			if e != nil {
				t.Fatal(e)
			}
			if _, e = f.pool.Exec(ctx, `UPDATE tasks SET state=$2 WHERE tenant_id=$1 AND task_kind='port_prepare'`, f.tenant, state); e != nil {
				t.Fatal(e)
			}
			b := durableBilling(t, f, in.ID, 1, 3)
			if _, e = s.BindCompileInput(ctx, f.scope, m.OperationID, b, 2, 3, 4, uuid.NewString()); e != nil {
				t.Fatal(e)
			}
			tables := []string{"port_allocations", "port_reservations", "rule_artifacts", "rule_deployments", "rule_delivery_bindings", "operations", "tasks", "outbox", "audit_events"}
			before := authoritySnapshot(t, f.pool, f.tenant, tables)
			if e = s.ConsumeCompile(ctx, f.scope, m.OperationID, uuid.NewString()); e == nil {
				t.Fatal("unobserved legacy socket task replaced", state)
			}
			if string(before) != string(authoritySnapshot(t, f.pool, f.tenant, tables)) {
				t.Fatal("partial handoff or physical occupation changed", a.ID)
			}
		})
	}
}

func TestRuleAutoPortRejectsMissingOrChangedMemberAllocation(t *testing.T) {
	f := newPortFixture(t)
	ctx := context.Background()
	s := service.RuleService{Pool: f.pool}
	in := ruleInput(f.group)
	in.Port = 0
	in.Protocol = "tcp_udp"
	m, e := s.Mutate(ctx, f.scope, in, 0, uuid.NewString())
	if e != nil {
		t.Fatal(e)
	}
	b := durableBilling(t, f, in.ID, 1, 3)
	if _, e = s.BindCompileInput(ctx, f.scope, m.OperationID, b, 2, 3, 4, uuid.NewString()); e == nil {
		t.Fatal("missing durable port allocation accepted")
	}
	req := f.request(t, 0)
	req.RuleID = in.ID
	if _, e = f.ports.PreparePortReservation(ctx, f.scope, req, uuid.NewString()); e != nil {
		t.Fatal(e)
	}
	if e = f.nodeService.SetMembers(ctx, f.scope, f.group, 2, []service.GroupMember{{NodeID: f.nodes[0], Weight: 1}}, uuid.NewString()); e != nil {
		t.Fatal(e)
	}
	before := authoritySnapshot(t, f.pool, f.tenant, []string{"port_allocations", "port_reservations", "rule_compile_inputs", "rule_artifacts", "tasks", "operations", "outbox", "audit_events"})
	if _, e = s.BindCompileInput(ctx, f.scope, m.OperationID, b, 2, 3, 4, uuid.NewString()); e == nil {
		t.Fatal("different member set reused old allocation")
	}
	if string(before) != string(authoritySnapshot(t, f.pool, f.tenant, []string{"port_allocations", "port_reservations", "rule_compile_inputs", "rule_artifacts", "tasks", "operations", "outbox", "audit_events"})) {
		t.Fatal("member mismatch freed or confirmed old occupation")
	}
}

func TestRulePortActualAgentSocketsUnknownAndQuery(t *testing.T) {
	for _, auto := range []bool{true, false} {
		t.Run(fmt.Sprintf("auto=%t", auto), func(t *testing.T) { testRulePortActualLifecycle(t, auto) })
	}
}

func testRulePortActualLifecycle(t *testing.T, auto bool) {
	// A worker intentionally leases across tenants. Give this real execution
	// test its own database so unrelated pending test deliveries cannot run here.
	admin := testutil.Postgres(t)
	database := "port_" + fmt.Sprintf("%x", uuid.New())
	if _, e := admin.Exec(context.Background(), "CREATE DATABASE "+pgx.Identifier{database}.Sanitize()); e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() {
		if _, e := admin.Exec(context.Background(), "DROP DATABASE "+pgx.Identifier{database}.Sanitize()); e != nil {
			t.Error(e)
		}
	})
	u, e := url.Parse(os.Getenv("PHASE6_DATABASE_URL"))
	if e != nil {
		t.Fatal(e)
	}
	u.Path = "/" + database
	t.Setenv("PHASE6_DATABASE_URL", u.String())
	f := newPortFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	// One host has one physical wildcard socket namespace. Keep one real member;
	// the separate fixed-member tests exercise all-member atomicity.
	if e := f.nodeService.SetMembers(ctx, f.scope, f.group, 2, []service.GroupMember{{NodeID: f.nodes[0], Weight: 1}}, uuid.NewString()); e != nil {
		t.Fatal(e)
	}
	dir := t.TempDir()
	ca := newCA(t)
	cert, key := ca.issue(t, dir, f.nodes[0])
	// Runtime VLink identity requires the canonical URI in addition to DNS.
	pair, e := tls.LoadX509KeyPair(cert, key)
	if e != nil {
		t.Fatal(e)
	}
	leaf, e := x509.ParseCertificate(pair.Certificate[0])
	if e != nil {
		t.Fatal(e)
	}
	uri, _ := url.Parse("urn:vortex:node:" + f.nodes[0])
	leaf.URIs = []*url.URL{uri}
	der, e := x509.CreateCertificate(rand.Reader, leaf, ca.certificate, leaf.PublicKey, ca.key)
	if e != nil {
		t.Fatal(e)
	}
	if e = os.WriteFile(cert, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0600); e != nil {
		t.Fatal(e)
	}
	root := filepath.Join(dir, "root.pem")
	if e := os.WriteFile(root, ca.pem, 0600); e != nil {
		t.Fatal(e)
	}
	config := filepath.Join(dir, "engine.json")
	raw := fmt.Sprintf(`{"node_id":%q,"role":"destination","certificate":%q,"private_key":%q,"trust_bundle":%q,"listen":"127.0.0.1:0","version":1,"rule_id":"legacy","rule_revision":1,"path_generation":1,"target":"192.0.2.80:443","execution_point":true}`, f.nodes[0], cert, key, root)
	if e := os.WriteFile(config, []byte(raw), 0600); e != nil {
		t.Fatal(e)
	}
	as, e := store.Open(filepath.Join(dir, "agent.db"), "agent")
	if e != nil {
		t.Fatal(e)
	}
	defer as.Close()
	engine, e := agent.NewRuntimeEngine(config, f.nodes[0])
	if e != nil {
		t.Fatal(e)
	}
	node, e := agent.New(f.nodes[0], as, engine)
	if e != nil {
		t.Fatal(e)
	}
	applier, e := node.RuntimeApplier()
	if e != nil {
		t.Fatal(e)
	}
	defer applier.Stop(context.Background())
	if e = f.ports.RegisterNodeSession(f.contexts[node.NodeID], f.scope, node.NodeID, node.BootID, 2, uuid.NewString()); e != nil {
		t.Fatal(e)
	}
	s := service.RuleService{Pool: f.pool}
	in := ruleInput(f.group)
	if auto {
		in.Port = 0
	}
	in.Protocol = "tcp_udp"
	m, e := s.Mutate(ctx, f.scope, in, 0, uuid.NewString())
	if e != nil {
		t.Fatal(e)
	}
	req := f.request(t, in.Port)
	req.RuleID = in.ID
	a, e := f.ports.PreparePortReservation(ctx, f.scope, req, uuid.NewString())
	if e != nil {
		t.Fatal(e)
	}
	b := durableBilling(t, f, in.ID, 1, 3)
	// Establish real boot-local authority over mTLS through the existing issuer.
	hs, e := store.Open(filepath.Join(dir, "hub.db"), "hub")
	if e != nil {
		t.Fatal(e)
	}
	defer hs.Close()
	hubID := uuid.NewString()
	now := time.Now().UTC()
	clock := func() store.IssuerTime {
		at := time.Now().UTC()
		return store.IssuerTime{Earliest: at, Latest: at, Trusted: true}
	}
	d := store.HubDelegation{ID: uuid.NewString(), HubID: hubID, SourceID: uuid.NewString(), RuntimeSourceID: uuid.NewString(), ShareSourceID: uuid.NewString(), NodeID: node.NodeID, BootID: node.BootID, BillingSnapshotID: b.ID, RuleID: in.ID, RuleRevision: 1, PathGeneration: 3, FencingFloor: 4, SignerEpoch: 2, RawBytes: 16384, RateBPS: 10000000, Connections: 2, ValidFrom: now, ExpiresAt: now.Add(time.Hour), HardDeadline: now.Add(time.Hour), BackendHeartbeat: now, AutonomyDeadline: now.Add(2 * time.Hour)}
	if e = hs.InstallHubDelegation(ctx, d, clock()); e != nil {
		t.Fatal(e)
	}
	auth, e := (&hub.Issuer{Store: hs, HubID: hubID, Clock: clock}).Issue(ctx, d.ID, node.NodeID, node.BootID, uuid.NewString(), 8192, time.Minute)
	if e != nil {
		t.Fatal(e)
	}
	hubCert, hubKey := ca.issue(t, dir, hubID)
	serverPair, e := tls.LoadX509KeyPair(hubCert, hubKey)
	if e != nil {
		t.Fatal(e)
	}
	clientPair, e := tls.LoadX509KeyPair(cert, key)
	if e != nil {
		t.Fatal(e)
	}
	roots := x509.NewCertPool()
	roots.AddCert(ca.certificate)
	ln, e := net.Listen("tcp", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	defer ln.Close()
	var authMu sync.Mutex
	server := &hub.TestServer{Store: hs, AuthorizationUpdate: func(string, string) (*controlv1.AuthorizationUpdate, error) {
		authMu.Lock()
		defer authMu.Unlock()
		return auth, nil
	}}
	go func() {
		_ = server.ServeGRPC(ctx, ln, &tls.Config{MinVersion: tls.VersionTLS13, MaxVersion: tls.VersionTLS13, Certificates: []tls.Certificate{serverPair}, ClientCAs: roots, ClientAuth: tls.RequireAndVerifyClientCert})
	}()
	reconciler := &agent.Reconciler{Store: as, Apply: applier}
	if e = (&agent.ControlClient{Node: node, Reconciler: reconciler, TLS: &tls.Config{MinVersion: tls.VersionTLS13, MaxVersion: tls.VersionTLS13, Certificates: []tls.Certificate{clientPair}, RootCAs: roots, ServerName: hubID}}).Connect(ctx, ln.Addr().String()); e != nil {
		t.Fatal(e)
	}
	if _, _, trusted := node.TrustedTime(); !trusted {
		t.Fatal("mTLS did not establish current-boot time")
	}
	if _, e = s.BindCompileInput(ctx, f.scope, m.OperationID, b, 2, 3, 4, uuid.NewString()); e != nil {
		t.Fatal(e)
	}
	if e = s.ConsumeCompile(ctx, f.scope, m.OperationID, uuid.NewString()); e != nil {
		t.Fatal(e)
	}
	// Deliver production RuleCommand/RuleReceipt over the actual authenticated
	// backend adapter and Agent client, then inject loss after the wire receipt.
	connected := make(chan struct{})
	var authenticated context.Context
	ports := service.PortService{Pool: f.pool, NodeCA: ca.certificate}
	adapter := &control.Adapter{Ports: &ports, Observe: func(ctx context.Context, _ string, _ string, _ []byte) error {
		authenticated = ctx
		close(connected)
		return nil
	}}
	backendListener, e := net.Listen("tcp", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	backendServer := grpc.NewServer(grpc.Creds(credentials.NewTLS(&tls.Config{MinVersion: tls.VersionTLS13, MaxVersion: tls.VersionTLS13, Certificates: []tls.Certificate{serverPair}, ClientCAs: roots, ClientAuth: tls.RequireAndVerifyClientCert})))
	controlv1.RegisterNodeControlServer(backendServer, adapter)
	go func() { _ = backendServer.Serve(backendListener) }()
	defer backendServer.Stop()
	clientCtx, stopClient := context.WithCancel(ctx)
	clientDone := make(chan error, 1)
	go func() {
		clientDone <- (&agent.ControlClient{Node: node, Reconciler: reconciler, TLS: &tls.Config{MinVersion: tls.VersionTLS13, MaxVersion: tls.VersionTLS13, Certificates: []tls.Certificate{clientPair}, RootCAs: roots, ServerName: hubID}}).Connect(clientCtx, backendListener.Addr().String())
	}()
	defer func() { stopClient(); <-clientDone }()
	select {
	case <-connected:
	case e := <-clientDone:
		t.Fatal("backend control failed", e)
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	var prepared *controlv1.RuleReceipt
	w := worker.RuleWorker{Pool: f.pool, Adapter: portRuleAdapter(func(ctx context.Context, id, boot string, c *controlv1.RuleCommand) (*controlv1.RuleReceipt, error) {
		if id != node.NodeID || boot != node.BootID {
			t.Fatal("wrong member delivery")
		}
		r, e := adapter.DeliverRule(ctx, id, boot, c)
		if e != nil {
			return nil, e
		}
		if c.Phase == "prepare" {
			if r.State != "prepared" {
				t.Fatalf("actual Agent prepare: %+v", r)
			}
			var state, lease string
			if e := f.pool.QueryRow(ctx, `SELECT state,lease_token FROM tasks WHERE tenant_id=$1 AND id=$2`, f.tenant, c.TaskKey).Scan(&state, &lease); e != nil || state != "running" || lease == "" {
				t.Fatal("network I/O before durable running", e, state)
			}
			before := authoritySnapshot(t, f.pool, f.tenant, []string{"port_allocations", "port_reservations", "rule_delivery_receipts", "tasks", "operations", "outbox", "audit_events"})
			if e := (service.RuleOrchestrator{Pool: f.pool}).ReceiptWithLease(ctx, f.scope, c.TaskKey, uuid.NewString(), r, uuid.NewString()); !errors.Is(e, service.ErrStaleReceipt) {
				t.Fatal("wrong worker lease accepted", e)
			}
			if string(before) != string(authoritySnapshot(t, f.pool, f.tenant, []string{"port_allocations", "port_reservations", "rule_delivery_receipts", "tasks", "operations", "outbox", "audit_events"})) {
				t.Fatal("bad lease changed socket authority")
			}
			prepared = r
		}
		if c.Phase == "commit" {
			if r.State != "active" {
				t.Fatalf("actual Agent commit: %+v", r)
			}
			return nil, errors.New("lost active receipt")
		}
		return r, nil
	})}
	run := func() {
		t.Helper()
		if worked, e := w.Once(ctx); e != nil || !worked {
			t.Fatal(worked, e)
		}
	}
	assertHeld := func() {
		t.Helper()
		addr := fmt.Sprintf(":%d", a.Port)
		if l, e := net.Listen("tcp", addr); e == nil {
			l.Close()
			t.Fatal("TCP socket not held")
		}
		ua, _ := net.ResolveUDPAddr("udp", addr)
		if l, e := net.ListenUDP("udp", ua); e == nil {
			l.Close()
			t.Fatal("UDP socket not held")
		}
	}
	run()
	assertHeld()
	var confirmed int
	if e = f.pool.QueryRow(ctx, `SELECT count(*) FROM port_reservations WHERE tenant_id=$1 AND config_confirmed`, f.tenant).Scan(&confirmed); e != nil || confirmed != 0 {
		t.Fatal("prepare claimed serving", e, confirmed)
	}
	run()
	assertHeld()
	var allocationState string
	if e = f.pool.QueryRow(ctx, `SELECT state FROM port_allocations WHERE tenant_id=$1 AND id=$2`, f.tenant, a.ID).Scan(&allocationState); e != nil || allocationState != "preparing" {
		t.Fatal("unknown committed or freed allocation", e, allocationState)
	}
	// A new fence cannot take socket ownership while the actual prior commit
	// lacks a receipt. The failed handoff leaves its reservations and tasks intact.
	newCompile, err := s.CompileRevision(ctx, f.scope, in.ID, 1, b, 2, 3, 5, uuid.NewString())
	if err != nil {
		t.Fatal(err)
	}
	beforeHandoff := authoritySnapshot(t, f.pool, f.tenant, []string{"port_allocations", "port_reservations", "rule_deployments", "rule_delivery_bindings", "operations", "tasks", "outbox", "audit_events"})
	if _, err = (service.RuleOrchestrator{Pool: f.pool}).Begin(ctx, f.scope, in.ID, 1, newCompile.InputDigest, uuid.NewString()); !errors.Is(err, service.ErrConflict) {
		t.Fatal("unknown actual commit handed sockets to new fence", err)
	}
	if string(beforeHandoff) != string(authoritySnapshot(t, f.pool, f.tenant, []string{"port_allocations", "port_reservations", "rule_deployments", "rule_delivery_bindings", "operations", "tasks", "outbox", "audit_events"})) {
		t.Fatal("failed unknown handoff changed occupation")
	}
	if _, e = f.pool.Exec(ctx, `UPDATE operations SET created_at=now()-interval '2 seconds' WHERE tenant_id=$1 AND id IN(SELECT task_id FROM rule_delivery_bindings WHERE tenant_id=$1 AND phase='query')`, f.tenant); e != nil {
		t.Fatal(e)
	}
	run()
	assertHeld()
	if e = f.pool.QueryRow(ctx, `SELECT state FROM port_allocations WHERE tenant_id=$1 AND id=$2`, f.tenant, a.ID).Scan(&allocationState); e != nil || allocationState != "committed" {
		t.Fatal("query did not converge active sockets", e, allocationState)
	}
	// Replay is harmless; a changed proof cannot rewrite an accepted task.
	o := service.RuleOrchestrator{Pool: f.pool}
	if e = o.Receipt(ctx, f.scope, prepared.TaskKey, prepared, uuid.NewString()); e != nil {
		t.Fatal("duplicate", e)
	}
	prepared.FencingGeneration++
	if e = o.Receipt(ctx, f.scope, prepared.TaskKey, prepared, uuid.NewString()); !errors.Is(e, service.ErrStaleReceipt) {
		t.Fatal("stale generation", e)
	}
	prepared.FencingGeneration--
	// The second immutable revision reuses the same durable physical allocation
	// and actual Engine sockets, with a new billing scope and monotone fence.
	in.Name = "复用端口的新版本"
	m, e = s.Mutate(ctx, f.scope, in, 1, uuid.NewString())
	if e != nil {
		t.Fatal(e)
	}
	b = durableBilling(t, f, in.ID, 2, 4)
	d.ID, d.SourceID, d.RuntimeSourceID, d.ShareSourceID = uuid.NewString(), uuid.NewString(), uuid.NewString(), uuid.NewString()
	d.RuleRevision, d.PathGeneration, d.FencingFloor, d.BillingSnapshotID = 2, 4, 5, b.ID
	if e = hs.InstallHubDelegation(ctx, d, clock()); e != nil {
		t.Fatal(e)
	}
	nextAuth, e := (&hub.Issuer{Store: hs, HubID: hubID, Clock: clock}).Issue(ctx, d.ID, node.NodeID, node.BootID, uuid.NewString(), 8192, time.Minute)
	if e != nil {
		t.Fatal(e)
	}
	authMu.Lock()
	auth = nextAuth
	authMu.Unlock()
	if e = (&agent.ControlClient{Node: node, Reconciler: reconciler, TLS: &tls.Config{MinVersion: tls.VersionTLS13, MaxVersion: tls.VersionTLS13, Certificates: []tls.Certificate{clientPair}, RootCAs: roots, ServerName: hubID}}).Connect(ctx, ln.Addr().String()); e != nil {
		t.Fatal(e)
	}
	if _, e = s.BindCompileInput(ctx, f.scope, m.OperationID, b, 2, 4, 5, uuid.NewString()); e != nil {
		t.Fatal(e)
	}
	if e = s.ConsumeCompile(ctx, f.scope, m.OperationID, uuid.NewString()); e != nil {
		t.Fatal("completed rule allocation could not reapply", e)
	}
	assertHeld()
	w.Adapter = adapter
	run()
	run()
	assertHeld()
	var allocations, reservations int
	if e = f.pool.QueryRow(ctx, `SELECT count(*) FROM port_allocations WHERE tenant_id=$1`, f.tenant).Scan(&allocations); e != nil || allocations != 1 {
		t.Fatal("duplicated allocation", e, allocations)
	}
	if e = f.pool.QueryRow(ctx, `SELECT count(*) FROM port_reservations WHERE tenant_id=$1`, f.tenant).Scan(&reservations); e != nil || reservations != 2 {
		t.Fatal("duplicated physical ownership", e, reservations)
	}
	// Old artifact release cannot stop the new incarnation, even over mTLS.
	old, _, _, e := service.RuleCommand(ctx, generated.New(f.pool), f.tenant, prepared.TaskKey)
	if e != nil {
		t.Fatal(e)
	}
	old.Phase, old.TaskKey, old.IdempotencyKey = "release", uuid.NewString(), uuid.NewString()
	if r, err := adapter.DeliverRule(ctx, node.NodeID, node.BootID, old); err != nil || r.State != "unknown" {
		t.Fatal("stale stop accepted", err, r)
	}
	assertHeld()
	if e = ports.RequestRelease(ctx, f.scope, a.ID, "explicit", uuid.NewString()); e != nil {
		t.Fatal(e)
	}
	var releaseProof service.PortProof
	pw := worker.PortWorker{Pool: f.pool, Adapter: portReleaseAdapter(func(ctx context.Context, scope service.TenantScope, id, boot string, c *controlv1.RuleCommand, p service.PortProof) error {
		releaseProof = p
		// Before any network execution the release task must already be durable.
		var state string
		if err := f.pool.QueryRow(ctx, `SELECT state FROM tasks WHERE tenant_id=$1 AND id=$2`, f.tenant, p.TaskID).Scan(&state); err != nil || state != "running" {
			t.Fatal("release sent before durable running", err, state)
		}
		before := authoritySnapshot(t, f.pool, f.tenant, []string{"port_allocations", "port_reservations", "release_proofs", "tasks", "operations", "outbox", "audit_events"})
		for _, bad := range []service.PortProof{func() service.PortProof { x := p; x.LeaseToken = uuid.NewString(); return x }(), func() service.PortProof { x := p; x.Generation++; return x }(), func() service.PortProof { x := p; x.Revision++; return x }(), func() service.PortProof { x := p; x.BootID = uuid.NewString(); return x }()} {
			bad.Result = "released"
			if err := ports.RecordReleaseProof(authenticated, scope, bad, uuid.NewString()); !errors.Is(err, service.ErrStaleReceipt) {
				t.Fatal("stale release proof accepted", err, bad)
			}
		}
		if string(before) != string(authoritySnapshot(t, f.pool, f.tenant, []string{"port_allocations", "port_reservations", "release_proofs", "tasks", "operations", "outbox", "audit_events"})) {
			t.Fatal("stale proof changed authority")
		}
		return errors.New("disconnected before stop")
	})}
	if worked, err := pw.Once(ctx); err != nil || !worked {
		t.Fatal(worked, err)
	}
	assertHeld()
	assertOccupied := func() {
		t.Helper()
		var state string
		if err := f.pool.QueryRow(ctx, `SELECT state FROM port_allocations WHERE tenant_id=$1 AND id=$2`, f.tenant, a.ID).Scan(&state); err != nil || state != "releasing" {
			t.Fatal("absent stop proof released occupation", err, state)
		}
		conflictReq := req
		conflictReq.RuleID, conflictReq.Port = uuid.NewString(), a.Port
		if _, err := ports.PreparePortReservation(ctx, f.scope, conflictReq, uuid.NewString()); err == nil {
			t.Fatal("unknown stop permitted overlapping reservation")
		}
	}
	assertOccupied()
	pw.Adapter = portReleaseAdapter(func(ctx context.Context, scope service.TenantScope, id, boot string, c *controlv1.RuleCommand, p service.PortProof) error {
		releaseProof = p
		r, err := adapter.DeliverRule(ctx, id, boot, c)
		if err != nil || r.State != "released" {
			t.Fatal("actual Engine stop failed", err, r)
		}
		return errors.New("lost actual closed-socket receipt")
	})
	if worked, err := pw.Once(ctx); err != nil || !worked {
		t.Fatal(worked, err)
	}
	assertOccupied()
	// The listener is actually closed, but this observation alone is not a
	// release proof. Retrying the same durable task recovers the Agent receipt.
	addr := fmt.Sprintf(":%d", a.Port)
	tcp, err := net.Listen("tcp", addr)
	if err != nil {
		t.Fatal("TCP still open", err)
	}
	tcp.Close()
	ua, _ := net.ResolveUDPAddr("udp", addr)
	udp, err := net.ListenUDP("udp", ua)
	if err != nil {
		t.Fatal("UDP still open", err)
	}
	udp.Close()
	pw.Adapter = adapter
	if worked, err := pw.Once(ctx); err != nil || !worked {
		t.Fatal(worked, err)
	}
	if e = f.pool.QueryRow(ctx, `SELECT state FROM port_allocations WHERE tenant_id=$1 AND id=$2`, f.tenant, a.ID).Scan(&allocationState); e != nil || allocationState != "released" {
		t.Fatal("proof did not release", e, allocationState)
	}
	// The prior attempt lease cannot overwrite the successful retry proof.
	releaseProof.Result = "released"
	if e = ports.RecordReleaseProof(authenticated, f.scope, releaseProof, uuid.NewString()); !errors.Is(e, service.ErrConflict) {
		t.Fatal("old release lease rewrote proof", e)
	}
	if _, e = ports.PreparePortReservation(ctx, f.scope, req, uuid.NewString()); e != nil {
		t.Fatal("proof-backed port could not be allocated again", e)
	}
}

type portReleaseAdapter func(context.Context, service.TenantScope, string, string, *controlv1.RuleCommand, service.PortProof) error

func (f portReleaseAdapter) DeliverPortRelease(ctx context.Context, scope service.TenantScope, node, boot string, c *controlv1.RuleCommand, p service.PortProof) error {
	return f(ctx, scope, node, boot, c, p)
}
