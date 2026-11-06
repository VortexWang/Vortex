package agent

import (
	"context"
	"errors"
	"fmt"
	"net"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"vortex/agent/store"
	controlv1 "vortex/contracts/protobuf/v1"
	engineconfig "vortex/engine/runtime/config"
	"vortex/hub"
)

func TestSchedulerProbeThresholdsAndUnsupportedUDP(t *testing.T) {
	target := ProbeTarget{ID: regionalNodeIDForProbe, RuleID: controlTestNodeID, Protocol: "tcp", Probe: func(context.Context) error { return nil }}
	s := &NodeScheduler{Targets: []ProbeTarget{target}}
	assert := func(state string, failures, successes uint64) {
		t.Helper()
		report, err := s.Report()
		if err != nil {
			t.Fatal(err)
		}
		p := report.Probes[0]
		if p.State != state || p.Failures != failures || p.Successes != successes || p.LatencyUs > 1000000 {
			t.Fatalf("observation %+v", p)
		}
	}
	assert("unknown", 0, 0)
	s.record(target, nil, time.Millisecond)
	assert("unknown", 0, 1)
	s.record(target, nil, time.Millisecond)
	assert("healthy", 0, 2)
	for range 2 {
		s.record(target, errors.New("down"), 2*time.Second)
	}
	assert("healthy", 2, 0)
	for range 20 {
		s.record(target, errors.New("down"), 2*time.Second)
	}
	assert("unhealthy", 3, 0)
	s.record(target, nil, time.Millisecond)
	assert("unhealthy", 0, 1)
	s.record(target, nil, time.Millisecond)
	assert("healthy", 0, 2)
	target.Protocol = "udp"
	target.Probe = nil
	s.record(target, nil, 0)
	assert("unknown", 0, 0)
	report, _ := s.Report()
	report.Probes[0].State = "forged"
	assert("unknown", 0, 0) // callers cannot mutate the retained snapshot
}

const regionalNodeIDForProbe = "550e8400-e29b-41d4-a716-446655440031"

func TestSchedulerBoundsConcurrencyAndJoinsCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var active, peak atomic.Int32
	entered := make(chan struct{}, 8)
	s := &NodeScheduler{Config: SchedulerConfig{ProbeInterval: 100 * time.Millisecond, ProbeTimeout: time.Second, ProbeWorkers: 3}}
	for n := range 32 {
		s.Targets = append(s.Targets, ProbeTarget{ID: fmt.Sprintf("550e8400-e29b-41d4-a716-%012d", n), RuleID: controlTestNodeID, Protocol: "tcp", Probe: func(ctx context.Context) error {
			n := active.Add(1)
			defer active.Add(-1)
			for {
				p := peak.Load()
				if n <= p || peak.CompareAndSwap(p, n) {
					break
				}
			}
			select {
			case entered <- struct{}{}:
			default:
			}
			<-ctx.Done()
			return ctx.Err()
		}})
	}
	done := make(chan error, 1)
	go func() { done <- s.Run(ctx, func(context.Context, *controlv1.NodeReport) error { return nil }) }()
	for range 3 {
		select {
		case <-entered:
		case <-time.After(time.Second):
			t.Fatal("workers not started")
		}
	}
	if err := s.Run(ctx, func(context.Context, *controlv1.NodeReport) error { return nil }); err == nil {
		t.Fatal("second scheduler multiplied worker pool")
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("workers not joined")
	}
	if active.Load() != 0 || peak.Load() > 3 {
		t.Fatalf("workers active=%d peak=%d", active.Load(), peak.Load())
	}
}

func TestSchedulerKeepsReportResponsiveDuringSlowProbe(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2500*time.Millisecond)
	defer cancel()
	var active, peak atomic.Int32
	s := &NodeScheduler{Config: SchedulerConfig{ProbeInterval: 100 * time.Millisecond, ProbeTimeout: time.Second, ProbeWorkers: 16, ReportInterval: time.Second}, Targets: []ProbeTarget{{ID: regionalNodeIDForProbe, RuleID: controlTestNodeID, Protocol: "tcp", Probe: func(ctx context.Context) error {
		n := active.Add(1)
		defer active.Add(-1)
		if n > peak.Load() {
			peak.Store(n)
		}
		<-ctx.Done()
		return ctx.Err()
	}}}}
	count := 0
	err := s.Run(ctx, func(_ context.Context, report *controlv1.NodeReport) error {
		count++
		if len(report.Probes) != 1 {
			t.Error("missing target")
		}
		if count == 2 {
			cancel()
		}
		return nil
	})
	if !errors.Is(err, context.Canceled) || count != 2 || peak.Load() != 1 || active.Load() != 0 {
		t.Fatalf("reports=%d peak=%d active=%d err=%v", count, peak.Load(), active.Load(), err)
	}
}

func TestSchedulerRejectsUnboundedConfiguration(t *testing.T) {
	for _, s := range []*NodeScheduler{
		{Config: SchedulerConfig{ProbeWorkers: 17}},
		{Config: SchedulerConfig{ProbeTimeout: 2 * time.Second}},
		{Targets: make([]ProbeTarget, 1025)},
		{Targets: []ProbeTarget{{ID: "remote-address", RuleID: controlTestNodeID, Protocol: "tcp"}}},
		{Targets: []ProbeTarget{{ID: regionalNodeIDForProbe, RuleID: controlTestNodeID, Protocol: "tcp"}, {ID: regionalNodeIDForProbe, RuleID: controlTestNodeID, Protocol: "udp"}}},
	} {
		if _, err := s.validate(); err == nil {
			t.Fatal("invalid scheduler accepted")
		}
	}
}

func TestSchedulerReportsOverCurrentMutualTLSStreamWithoutAuthority(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	ns, err := store.Open(filepath.Join(t.TempDir(), "agent.db"), "agent")
	if err != nil {
		t.Fatal(err)
	}
	defer ns.Close()
	hs, err := store.Open(filepath.Join(t.TempDir(), "hub.db"), "hub")
	if err != nil {
		t.Fatal(err)
	}
	defer hs.Close()
	n, err := New(controlTestNodeID, ns, nil)
	if err != nil {
		t.Fatal(err)
	}
	regionalSeed(t, hs, regionalHubID, n.NodeID, n.BootID)
	serverTLS, clientTLS, _ := controlTLS(t, n.NodeID)
	server := &hub.TestServer{Store: hs, HubID: regionalHubID, Region: "east", AcceptUsage: true}
	listener := startTestControlHub(t, ctx, server, serverTLS)
	c := &ControlClient{Node: n, TLS: clientTLS}
	r := RegionalConfig{Region: "east", Endpoints: []HubEndpoint{{Region: "east", HubID: regionalHubID, Address: listener.Addr().String(), ServerName: "hub"}}, DialTimeout: time.Second, Watchdog: 2 * time.Second, FailoverBudget: 2 * time.Second}
	done := make(chan error, 1)
	go func() { done <- c.RunRegion(ctx, r) }()
	defer func() {
		cancel()
		select {
		case <-done:
		case <-time.After(time.Second):
			t.Error("control sender did not join")
		}
	}()
	s := &NodeScheduler{Targets: []ProbeTarget{{ID: regionalNodeIDForProbe, RuleID: controlTestNodeID, Protocol: "udp"}}}
	report, err := s.Report()
	if err != nil {
		t.Fatal(err)
	}
	if err := n.ObserveReport(ctx, report); err != nil {
		t.Fatal(err)
	}
	// Coalescing before readiness must never become an unbounded durable queue.
	for range 100 {
		if err := c.EnqueueNodeReport(ctx, report); err != nil {
			t.Fatal(err)
		}
	}
	for {
		if snapshot := server.CurrentNodeReport(n.NodeID); snapshot != nil {
			if snapshot.BootID != n.BootID || snapshot.Report.Probes[0].State != "unknown" {
				t.Fatalf("snapshot %+v", snapshot)
			}
			break
		}
		if err := c.EnqueueNodeReport(ctx, report); err != nil {
			t.Fatal(err)
		}
		select {
		case <-ctx.Done():
			t.Fatal("report never reached current authenticated stream")
		case <-time.After(10 * time.Millisecond):
		}
	}
	if _, _, trusted := n.TrustedTime(); trusted {
		t.Fatal("telemetry minted trusted time")
	}
	if _, err := ns.AgentState(ctx, "backend_heartbeat"); err == nil {
		t.Fatal("telemetry changed backend heartbeat")
	}
}

func TestSchedulerProbesOnlyInstalledRuleAndKeepsUDPUnknown(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			conn.Close()
		}
	}()
	defer func() { listener.Close(); <-done }()
	e := &RuntimeEngine{config: engineconfig.Config{RuleID: controlTestNodeID, Target: listener.Addr().String()}}
	n := &Node{Engine: e}
	r := RegionalConfig{Probes: &SchedulerConfig{}, ProbeReferences: []ProbeTarget{{ID: regionalNodeIDForProbe, RuleID: controlTestNodeID, Protocol: "tcp"}}}
	s, err := n.SchedulerForRegion(r)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := s.Targets[0].Probe(ctx); err != nil {
		t.Fatal(err)
	}
	e.activationRequired.Store(true)
	if err := s.Targets[0].Probe(ctx); !errors.Is(err, ErrProbeUnknown) {
		t.Fatalf("unapplied rule probed: %v", err)
	}
	r.ProbeReferences[0].RuleID = regionalNodeIDForProbe
	if _, err := n.SchedulerForRegion(r); err == nil {
		t.Fatal("foreign rule reference accepted")
	}
	r.ProbeReferences[0].RuleID = controlTestNodeID
	r.ProbeReferences[0].Protocol = "udp"
	s, err = n.SchedulerForRegion(r)
	if err != nil || s.Targets[0].Probe != nil {
		t.Fatalf("unsupported UDP was assumed healthy: %+v %v", s, err)
	}
}
