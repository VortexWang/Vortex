package agent

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"database/sql"
	"fmt"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"io"
	"math"
	"net"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"google.golang.org/protobuf/proto"
	"vortex/agent/store"
	"vortex/contracts"
	controlv1 "vortex/contracts/protobuf/v1"
	"vortex/hub"
)

const regionalHubID = "550e8400-e29b-41d4-a716-446655440060"
const regionalOtherID = "550e8400-e29b-41d4-a716-446655440061"
const regionalSessionID = "550e8400-e29b-41d4-a716-446655440062"

func regionalSeed(t *testing.T, s *store.Store, id, node, boot string) {
	t.Helper()
	now := time.Now().UTC()
	d := store.HubDelegation{ID: regionalSessionID, HubID: id, SourceID: regionalOtherID, RuntimeSourceID: controlTestNodeID, ShareSourceID: regionalHubID, NodeID: node, BootID: boot, BillingSnapshotID: regionalOtherID, RuleID: "rule", RuleRevision: 1, PathGeneration: 1, FencingFloor: 1, SignerEpoch: 1, RawBytes: 4096, RateBPS: 1000000, Connections: 2, ValidFrom: now, ExpiresAt: now.Add(time.Hour), HardDeadline: now.Add(time.Hour), BackendHeartbeat: now, AutonomyDeadline: now.Add(time.Hour)}
	if err := s.InstallHubDelegation(context.Background(), d, store.IssuerTime{Earliest: now, Latest: now, Trusted: true}); err != nil {
		t.Fatal(err)
	}
}

func TestRegionalStatusRejectsUnauthenticatedOrStaleBounds(t *testing.T) {
	now := time.Now().UTC()
	e := HubEndpoint{HubID: regionalHubID, ServerName: "hub"}
	r := &RegionalConfig{Region: "east"}
	good := &controlv1.HubStatus{HubId: regionalHubID, RegionId: "east", SessionId: regionalSessionID, Mode: "online", ControlVersion: "1.3.0", Capabilities: []string{"status-v1"}, SignerEpoch: 1, BackendHeartbeatAt: now.Add(-time.Second).Format(time.RFC3339Nano), AutonomyDeadline: now.Add(time.Hour).Format(time.RFC3339Nano)}
	if err := validateRegionalStatus(good, e, r, regionalSessionID, "hub", now); err != nil {
		t.Fatal(err)
	}
	cases := map[string]func(*controlv1.HubStatus){
		"foreign-region": func(s *controlv1.HubStatus) { s.RegionId = "west" }, "wrong-hub": func(s *controlv1.HubStatus) { s.HubId = regionalOtherID }, "old-session": func(s *controlv1.HubStatus) { s.SessionId = regionalOtherID }, "no-mode": func(s *controlv1.HubStatus) { s.Mode = "" }, "blocked": func(s *controlv1.HubStatus) { s.Mode = "blocked" }, "no-epoch": func(s *controlv1.HubStatus) { s.SignerEpoch = 0 }, "overflow": func(s *controlv1.HubStatus) { s.RestrictionRevision = math.MaxUint64 }, "stale-online": func(s *controlv1.HubStatus) {
			s.BackendHeartbeatAt = now.Add(-31 * time.Second).Format(time.RFC3339Nano)
		}, "future-heartbeat": func(s *controlv1.HubStatus) { s.BackendHeartbeatAt = now.Add(time.Second).Format(time.RFC3339Nano) }, "expired": func(s *controlv1.HubStatus) { s.AutonomyDeadline = now.Add(-time.Millisecond).Format(time.RFC3339Nano) }, "unbounded": func(s *controlv1.HubStatus) { s.AutonomyDeadline = now.Add(24 * time.Hour).Format(time.RFC3339Nano) }, "unsupported-cap": func(s *controlv1.HubStatus) { s.Capabilities = []string{"status-v1", "renewal-v1"} }, "unknown-field": func(s *controlv1.HubStatus) { s.ProtoReflect().SetUnknown([]byte{0xa0, 0x06, 0x01}) },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			s := proto.Clone(good).(*controlv1.HubStatus)
			mutate(s)
			if validateRegionalStatus(s, e, r, regionalSessionID, "hub", now) == nil {
				t.Fatal("accepted invalid status")
			}
		})
	}
	if validateRegionalStatus(good, e, r, regionalSessionID, "foreign", now) == nil {
		t.Fatal("accepted different certificate identity")
	}
}

func TestRegionalStaleIncarnationCannotMutateAuthority(t *testing.T) {
	c := new(ControlClient)
	old, cancel, id := c.beginSession(context.Background())
	defer cancel()
	_, cancelNew, _ := c.beginSession(context.Background())
	defer cancelNew()
	called := false
	if err := c.withSession(old, id, func() error { called = true; return nil }); err == nil || called {
		t.Fatal("late old stream installed authority")
	}
}

func TestRegionalFailoverRetainsUnknownDebt(t *testing.T) {
	for _, fault := range []string{"tls-blackhole", "silent-stream", "silent-established", "autonomous", "recovering"} {
		t.Run(fault, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			nodePath := filepath.Join(t.TempDir(), "node.db")
			ns, err := store.Open(nodePath, "agent")
			if err != nil {
				t.Fatal(err)
			}
			defer ns.Close()
			n, err := New(controlTestNodeID, ns, nil)
			if err != nil {
				t.Fatal(err)
			}
			unknown, err := contracts.EncodeUnknownUsage(&controlv1.UnknownUsageBatch{BatchId: "pending", OriginBootId: n.BootID, BillingSnapshotId: regionalOtherID, RuleId: "rule", GrantId: regionalHubID, Role: "source", Direction: "egress", UnknownUpperBound: 512})
			if err != nil {
				t.Fatal(err)
			}
			if err = ns.PersistUnknownUsage(ctx, n.NodeID, unknown); err != nil {
				t.Fatal(err)
			}
			serverTLS, clientTLS, _ := controlTLS(t, n.NodeID)
			hs, err := store.Open(filepath.Join(t.TempDir(), "healthy.db"), "hub")
			if err != nil {
				t.Fatal(err)
			}
			defer hs.Close()
			regionalSeed(t, hs, regionalOtherID, n.NodeID, n.BootID)
			good := startTestControlHub(t, ctx, &hub.TestServer{Store: hs, HubID: regionalOtherID, Region: "east", AcceptUsage: true}, serverTLS)
			var bad net.Listener
			var loserConnections atomic.Int64
			if fault == "tls-blackhole" {
				bad, err = net.Listen("tcp", "127.0.0.1:0")
				if err != nil {
					t.Fatal(err)
				}
				defer bad.Close()
				go func() {
					for {
						conn, e := bad.Accept()
						if e != nil {
							return
						}
						loserConnections.Add(1)
						go func() { defer conn.Close(); defer loserConnections.Add(-1); _, _ = io.Copy(io.Discard, conn) }()
					}
				}()
			} else {
				path := filepath.Join(t.TempDir(), "unhealthy.db")
				bs, e := store.Open(path, "hub")
				if e != nil {
					t.Fatal(e)
				}
				defer bs.Close()
				if fault != "silent-stream" {
					regionalSeed(t, bs, regionalHubID, n.NodeID, n.BootID)
					db, e := sql.Open("sqlite", path)
					if e != nil {
						t.Fatal(e)
					}
					if fault == "autonomous" || fault == "recovering" {
						_, e = db.Exec("UPDATE agent_state SET value=? WHERE key='authority_mode'", fault)
					}
					db.Close()
					if e != nil {
						t.Fatal(e)
					}
				}
				bad = startTestControlHub(t, ctx, &hub.TestServer{Store: bs, HubID: regionalHubID, Region: "east", StallAfterReadiness: fault == "silent-established", StallReadiness: fault == "silent-stream"}, serverTLS)
			}
			c := &ControlClient{Node: n, TLS: clientTLS, UsageInterval: 20 * time.Millisecond}
			cfg := RegionalConfig{Region: "east", Endpoints: []HubEndpoint{{Region: "west", Address: "127.0.0.1:1"}, {Region: "east", Address: bad.Addr().String(), HubID: regionalHubID, ServerName: "hub"}, {Region: "east", Address: good.Addr().String(), HubID: regionalOtherID, ServerName: "hub"}}, DialTimeout: 200 * time.Millisecond, Watchdog: time.Second, FailoverBudget: 2 * time.Second, RetryMin: 20 * time.Millisecond, RetryMax: 40 * time.Millisecond}
			done := make(chan error, 1)
			go func() { done <- c.RunRegion(ctx, cfg) }()
			for {
				pending, e := ns.PendingUnknownUsage(ctx, n.NodeID)
				if e != nil {
					t.Fatal(e)
				}
				status := c.CurrentHubStatus()
				if len(pending) == 0 && status != nil && status.HubId == regionalOtherID {
					break
				}
				select {
				case e := <-done:
					t.Fatalf("regional loop exited early: %v", e)
				case <-ctx.Done():
					t.Fatal("bounded failover did not hand off debt")
				case <-time.After(10 * time.Millisecond):
				}
			}
			deadline := time.Now().Add(100 * time.Millisecond)
			for loserConnections.Load() != 0 && time.Now().Before(deadline) {
				time.Sleep(time.Millisecond)
			}
			if loserConnections.Load() != 0 {
				t.Fatal("losing TLS transport leaked")
			}
			if _, e := ns.AgentState(ctx, "backend_heartbeat"); e == nil {
				t.Fatal("status minted trusted authority time")
			}
			// ACK only records durable transport proof; unresolved financial hold stays.
			db, e := sql.Open("sqlite", nodePath)
			if e != nil {
				t.Fatal(e)
			}
			var upper, state string
			e = db.QueryRowContext(ctx, "SELECT unknown_upper_bound,liability_state FROM unknown_usage_batches WHERE batch_id='pending'").Scan(&upper, &state)
			db.Close()
			if e != nil || upper != "512" || state != "unresolved" {
				t.Fatalf("unknown liability changed: %s %s %v", upper, state, e)
			}

			cancel()
			select {
			case <-done:
			case <-time.After(time.Second):
				t.Fatal("loser not joined")
			}
			if c.CurrentHubStatus() != nil {
				t.Fatal("disconnected Hub advertised current")
			}
		})
	}
}

func TestRegionalConfigurationAndRetryBounds(t *testing.T) {
	for _, d := range []time.Duration{time.Millisecond, time.Second, 30 * time.Second} {
		for i := 0; i < 50; i++ {
			v := regionalJitter(d)
			if v < d/2 || v > d {
				t.Fatal("unbounded jitter")
			}
		}
	}
	base := RegionalConfig{Region: "east", Endpoints: []HubEndpoint{{Region: "east", Address: "127.0.0.1:1", HubID: regionalHubID, ServerName: "hub"}}}
	if _, err := base.validated(); err != nil {
		t.Fatal(err)
	}
	base.Endpoints = append(base.Endpoints, base.Endpoints[0])
	if _, err := base.validated(); err == nil {
		t.Fatal("duplicate accepted")
	}
	payload := []byte(fmt.Sprintf(`{"version":1,"node_id":%q,"listeners":[],"rules":[],"revocations":[],"lifecycle":{"region":{"region_id":"east","hubs":[{"hub_id":%q,"endpoint":"127.0.0.1:1","server_name":"hub"}]},"retry_min_ms":20,"retry_max_ms":40}}`, controlTestNodeID, regionalHubID))
	if _, err := RegionalConfigFromRuntime(payload, controlTestNodeID); err != nil {
		t.Fatal(err)
	}
}

type regionalScriptServer struct {
	controlv1.UnimplementedNodeControlServer
	run func(controlv1.NodeControl_ConnectServer) error
}

func (s *regionalScriptServer) Connect(stream controlv1.NodeControl_ConnectServer) error {
	return s.run(stream)
}

func TestRegionalLateAuthorizationCannotAdvanceClockOrConfig(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	ns, err := store.Open(filepath.Join(t.TempDir(), "node.db"), "agent")
	if err != nil {
		t.Fatal(err)
	}
	defer ns.Close()
	n, err := New(controlTestNodeID, ns, nil)
	if err != nil {
		t.Fatal(err)
	}
	serverTLS, clientTLS, _ := controlTLS(t, n.NodeID)
	hs, err := store.Open(filepath.Join(t.TempDir(), "hub.db"), "hub")
	if err != nil {
		t.Fatal(err)
	}
	defer hs.Close()
	regionalSeed(t, hs, regionalOtherID, n.NodeID, n.BootID)
	second := startTestControlHub(t, ctx, &hub.TestServer{Store: hs, HubID: regionalOtherID, Region: "east", AcceptUsage: true}, serverTLS)
	ready, late, attempted := make(chan struct{}), make(chan struct{}), make(chan struct{})
	server := &regionalScriptServer{run: func(stream controlv1.NodeControl_ConnectServer) error {
		if _, err := stream.Recv(); err != nil {
			return err
		}
		hello, err := stream.Recv()
		if err != nil {
			return err
		}
		now := time.Now().UTC()
		id, _ := newControlID()
		if err = stream.Send(&controlv1.ControlMessage{MessageId: id, Sequence: 1, Body: &controlv1.ControlMessage_HubStatus{HubStatus: &controlv1.HubStatus{HubId: regionalHubID, RegionId: "east", Mode: "online", SessionId: hello.GetLifecycleHello().GetSessionId(), ControlVersion: "1.3.0", Capabilities: []string{"status-v1"}, SignerEpoch: 1, BackendHeartbeatAt: now.Format(time.RFC3339Nano), AutonomyDeadline: now.Add(time.Hour).Format(time.RFC3339Nano)}}}); err != nil {
			return err
		}
		close(ready)
		select {
		case <-late:
		case <-ctx.Done():
			return ctx.Err()
		}
		defer close(attempted)
		// Valid grant and config bytes, deliberately emitted after a successor
		// stream took ownership; neither may install or advance the clock.
		payload := leasedPayload(t, n.NodeID, n.BootID)
		id, _ = newControlID()
		_ = stream.Send(&controlv1.ControlMessage{MessageId: id, Sequence: 2, Body: &controlv1.ControlMessage_AuthorizationUpdate{AuthorizationUpdate: &controlv1.AuthorizationUpdate{Generation: 4, Payload: payload}}})
		raw := []byte(fmt.Sprintf(`{"version":99,"node_id":%q,"listeners":[],"rules":[],"revocations":[]}`, n.NodeID))
		digest := sha256.Sum256(raw)
		id, _ = newControlID()
		return stream.Send(&controlv1.ControlMessage{MessageId: id, Sequence: 3, Body: &controlv1.ControlMessage_ConfigUpdate{ConfigUpdate: &controlv1.RuntimeConfigUpdate{Version: 99, Payload: raw, TransactionId: []byte("0123456789abcdef"), FencingGeneration: 99, PayloadSha256: digest[:]}}})
	}}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	rpc := grpc.NewServer(grpc.Creds(credentials.NewTLS(serverTLS)))
	controlv1.RegisterNodeControlServer(rpc, server)
	go rpc.Serve(listener)
	defer rpc.Stop()
	c := &ControlClient{Node: n, TLS: clientTLS, Reconciler: &Reconciler{Store: ns, Apply: EngineApplier{}}}
	r := RegionalConfig{Region: "east", DialTimeout: time.Second, Watchdog: time.Second, FailoverBudget: 2 * time.Second}
	oldDone, newDone := make(chan error, 1), make(chan error, 1)
	go func() {
		oldDone <- c.connect(ctx, HubEndpoint{Address: listener.Addr().String(), HubID: regionalHubID, ServerName: "hub"}, &r)
	}()
	select {
	case <-ready:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	go func() {
		newDone <- c.connect(ctx, HubEndpoint{Address: second.Addr().String(), HubID: regionalOtherID, ServerName: "hub"}, &r)
	}()
	for {
		status := c.CurrentHubStatus()
		if status != nil && status.HubId == regionalOtherID {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		case <-time.After(time.Millisecond):
		}
	}
	close(late)
	select {
	case <-attempted:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	select {
	case <-oldDone:
	case <-ctx.Done():
		t.Fatal("old receiver not joined")
	}
	if _, err := ns.AgentState(ctx, "applied_version"); err == nil {
		t.Fatal("late config applied")
	}
	if _, _, trusted := n.TrustedTime(); trusted {
		t.Fatal("late authorization advanced authority clock")
	}
	cancel()
	select {
	case <-newDone:
	case <-time.After(time.Second):
		t.Fatal("successor did not stop")
	}
}

func TestRegionalRevocationAndNoHealthyPeerAreBounded(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	ns, err := store.Open(filepath.Join(t.TempDir(), "node.db"), "agent")
	if err != nil {
		t.Fatal(err)
	}
	defer ns.Close()
	n, err := New(controlTestNodeID, ns, nil)
	if err != nil {
		t.Fatal(err)
	}
	serverTLS, clientTLS, _ := controlTLS(t, n.NodeID)
	hs, err := store.Open(filepath.Join(t.TempDir(), "hub.db"), "hub")
	if err != nil {
		t.Fatal(err)
	}
	defer hs.Close()
	regionalSeed(t, hs, regionalHubID, n.NodeID, n.BootID)
	listener := startTestControlHub(t, ctx, &hub.TestServer{Store: hs, HubID: regionalHubID, Region: "east", AcceptUsage: true}, serverTLS)
	var attempts atomic.Int64
	clientTLS.VerifyConnection = func(state tls.ConnectionState) error {
		attempts.Add(1)
		return fmt.Errorf("test: current Hub certificate is revoked")
	}
	c := &ControlClient{Node: n, TLS: clientTLS}
	err = c.RunRegion(ctx, RegionalConfig{Region: "east", Endpoints: []HubEndpoint{{Region: "east", Address: listener.Addr().String(), HubID: regionalHubID, ServerName: "hub"}}, DialTimeout: 50 * time.Millisecond, Watchdog: time.Second, FailoverBudget: 100 * time.Millisecond, RetryMin: 30 * time.Millisecond, RetryMax: 60 * time.Millisecond})
	if err == nil || ctx.Err() == nil {
		t.Fatal("unbounded or accepted revoked Hub")
	}
	if count := attempts.Load(); count < 1 || count > 20 {
		t.Fatalf("retry bound violated: %d", count)
	}
	if c.CurrentHubStatus() != nil {
		t.Fatal("revoked peer became current")
	}
	if _, _, trusted := n.TrustedTime(); trusted {
		t.Fatal("no healthy Hub minted rights")
	}
}

func TestRegionalSwitchCancelsBlockedMutation(t *testing.T) {
	parent, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	c := new(ControlClient)
	old, stop, id := c.beginSession(parent)
	defer stop()
	entered, returned := make(chan struct{}), make(chan error, 1)
	go func() {
		returned <- c.withSession(old, id, func() error { close(entered); <-old.Done(); return old.Err() })
	}()
	<-entered
	next := make(chan context.CancelFunc, 1)
	go func() { _, cancel, _ := c.beginSession(parent); next <- cancel }()
	select {
	case cancel := <-next:
		cancel()
	case <-time.After(200 * time.Millisecond):
		t.Fatal("switch waited on blocked sender before cancelling it")
	}
	select {
	case <-returned:
	case <-time.After(200 * time.Millisecond):
		t.Fatal("old mutation boundary never returned")
	}
}
