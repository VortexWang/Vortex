package agent

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net"
	"strconv"
	"sync"
	"time"

	"google.golang.org/protobuf/proto"
	"vortex/contracts"

	controlv1 "vortex/contracts/protobuf/v1"
)

// ProbeTarget contains a locally resolved operation, never an address supplied
// by a control peer. A nil probe (including unsupported UDP health protocols)
// reports unknown instead of treating a successful datagram send as health.
type ProbeTarget struct {
	ID, RuleID, Protocol string
	Probe                func(context.Context) error
}

var ErrProbeUnknown = errors.New("agent: probe not available for current applied rule")

type SchedulerConfig struct {
	ProbeInterval, ProbeTimeout, ReportInterval time.Duration
	ProbeWorkers                                int
}

func (c SchedulerConfig) validated() (SchedulerConfig, error) {
	if c.ProbeInterval == 0 {
		c.ProbeInterval = 3 * time.Second
	}
	if c.ProbeTimeout == 0 {
		c.ProbeTimeout = time.Second
	}
	if c.ReportInterval == 0 {
		c.ReportInterval = 10 * time.Second
	}
	if c.ProbeWorkers == 0 {
		c.ProbeWorkers = 16
	}
	if c.ProbeInterval < 100*time.Millisecond || c.ProbeInterval > time.Minute || c.ProbeTimeout < time.Millisecond || c.ProbeTimeout > time.Second || c.ReportInterval < time.Second || c.ReportInterval > time.Minute || c.ProbeWorkers < 1 || c.ProbeWorkers > 16 {
		return c, fmt.Errorf("agent: scheduler bounds exceeded")
	}
	return c, nil
}

// NodeScheduler retains only the latest bounded probe snapshot. It owns a
// fixed worker pool for its entire lifetime and joins it before returning.
// Probe and report timestamps never update Node.TrustedTime or authority.
type NodeScheduler struct {
	Config       SchedulerConfig
	Targets      []ProbeTarget
	BuildVersion string
	mu           sync.Mutex
	observations map[string]*controlv1.ProbeObservation
	running      bool
}

func (s *NodeScheduler) validate() (SchedulerConfig, error) {
	if s == nil || len(s.Targets) > 1024 || len(s.BuildVersion) > 128 {
		return SchedulerConfig{}, fmt.Errorf("agent: invalid bounded scheduler")
	}
	c, err := s.Config.validated()
	if err != nil {
		return c, err
	}
	seen := make(map[string]bool, len(s.Targets))
	for _, t := range s.Targets {
		if !canonicalUUID(t.ID) || !canonicalUUID(t.RuleID) || seen[t.ID] || (t.Protocol != "tcp" && t.Protocol != "udp") {
			return c, fmt.Errorf("agent: invalid or duplicate probe reference")
		}
		seen[t.ID] = true
	}
	return c, nil
}

func (s *NodeScheduler) record(t ProbeTarget, err error, elapsed time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.observations == nil {
		s.observations = make(map[string]*controlv1.ProbeObservation)
	}
	o := s.observations[t.ID]
	if o == nil {
		o = &controlv1.ProbeObservation{TargetId: t.ID, State: "unknown"}
		s.observations[t.ID] = o
	}
	o.ObservedAt = time.Now().UTC().Format(time.RFC3339Nano)
	if elapsed < 0 {
		elapsed = 0
	}
	if elapsed > time.Second {
		elapsed = time.Second
	}
	o.LatencyUs = uint64(elapsed / time.Microsecond)
	if t.Probe == nil || errors.Is(err, ErrProbeUnknown) {
		o.State = "unknown"
		o.Failures = 0
		o.Successes = 0
		return
	}
	if err != nil {
		o.Successes = 0
		if o.Failures < 3 {
			o.Failures++
		}
		if o.Failures == 3 {
			o.State = "unhealthy"
		}
	} else {
		o.Failures = 0
		if o.Successes < 2 {
			o.Successes++
		}
		if o.Successes == 2 {
			o.State = "healthy"
		}
	}
}

func (c *ControlClient) reportQueue() chan *controlv1.NodeReport {
	c.reportMu.Lock()
	defer c.reportMu.Unlock()
	if c.reports == nil {
		c.reports = make(chan *controlv1.NodeReport, 1)
	}
	return c.reports
}

// EnqueueNodeReport coalesces telemetry while disconnected. The control loop
// alone sends it and assigns the durable stream sequence. No per-report outbox
// or per-probe event rows accumulate; financial outboxes remain independent.
func (c *ControlClient) EnqueueNodeReport(ctx context.Context, r *controlv1.NodeReport) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := contracts.ValidateNodeReport(r); err != nil {
		return err
	}
	q := c.reportQueue()
	r = proto.Clone(r).(*controlv1.NodeReport)
	select {
	case q <- r:
		return nil
	default:
	}
	select {
	case <-q:
	default:
	}
	select {
	case q <- r:
	default:
	}
	return nil
}

// SchedulerForRegion resolves health targets against the installed local
// runtime, not a peer-provided address. Unsupported UDP health stays unknown.
func (n *Node) SchedulerForRegion(r RegionalConfig) (*NodeScheduler, error) {
	if r.Probes == nil {
		return nil, nil
	}
	s := &NodeScheduler{Config: *r.Probes, BuildVersion: "dev"}
	for _, ref := range r.ProbeReferences {
		e, ok := n.Engine.(*RuntimeEngine)
		if !ok || e.config.RuleID != ref.RuleID || e.config.Target == "" {
			return nil, fmt.Errorf("agent: probe reference is not an installed local target")
		}
		t := ref
		if t.Protocol == "tcp" {
			t.Probe = func(ctx context.Context) error {
				if e.activationRequired.Load() && !e.activated.Load() {
					return ErrProbeUnknown
				}
				conn, err := (&net.Dialer{}).DialContext(ctx, "tcp", e.config.Target)
				if err == nil {
					return conn.Close()
				}
				return err
			}
		}
		s.Targets = append(s.Targets, t)
	}
	_, err := s.validate()
	return s, err
}

// ObserveReport fills observational state only; it never calls the authority
// clock or changes the backend heartbeat, balances, holds or leases.
func (n *Node) ObserveReport(ctx context.Context, r *controlv1.NodeReport) error {
	value, err := n.Store.AgentState(ctx, "applied_version")
	if err == nil {
		r.AppliedConfigVersion, err = strconv.ParseUint(value, 10, 63)
	}
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	r.ActiveSessions, err = n.Store.RuntimeSessionCount(ctx, n.NodeID, n.BootID)
	if engine, ok := n.Engine.(OperationalEngine); ok {
		r.DrainState = engine.Stats().DrainState
		if r.DrainState == "drained" {
			r.DrainState = "stopped"
		}
	}
	return err
}

func (s *NodeScheduler) Report() (*controlv1.NodeReport, error) {
	id, err := newControlID()
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	r := &controlv1.NodeReport{ReportId: id, BuildVersion: s.BuildVersion, ObservedAt: time.Now().UTC().Format(time.RFC3339Nano), DrainState: "running"}
	if r.BuildVersion == "" {
		r.BuildVersion = "dev"
	}
	for _, target := range s.Targets {
		o := s.observations[target.ID]
		if o == nil {
			r.Probes = append(r.Probes, &controlv1.ProbeObservation{TargetId: target.ID, State: "unknown", ObservedAt: r.ObservedAt})
			continue
		}
		r.Probes = append(r.Probes, &controlv1.ProbeObservation{TargetId: o.TargetId, State: o.State, Failures: o.Failures, Successes: o.Successes, ObservedAt: o.ObservedAt, LatencyUs: o.LatencyUs})
	}
	return r, nil
}

// Run publishes reports through a caller-supplied serialized control sender.
// Probe implementations must honor cancellation; uncooperative callbacks are
// never multiplied into replacement goroutines on timeout.
func (s *NodeScheduler) Run(ctx context.Context, publish func(context.Context, *controlv1.NodeReport) error) error {
	c, err := s.validate()
	if err != nil {
		return err
	}
	if publish == nil {
		return fmt.Errorf("agent: report sender missing")
	}
	s.mu.Lock()
	if s.running {
		s.mu.Unlock()
		return fmt.Errorf("agent: scheduler already running")
	}
	s.running = true
	s.mu.Unlock()
	defer func() { s.mu.Lock(); s.running = false; s.mu.Unlock() }()
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	jobs := make(chan ProbeTarget)
	completed := make(chan string, c.ProbeWorkers)
	var workers sync.WaitGroup
	for range c.ProbeWorkers {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for {
				select {
				case <-ctx.Done():
					return
				case target, ok := <-jobs:
					if !ok {
						return
					}
					started := time.Now()
					probeCtx, stop := context.WithTimeout(ctx, c.ProbeTimeout)
					var err error
					if target.Probe != nil {
						err = target.Probe(probeCtx)
					}
					if err == nil && probeCtx.Err() != nil {
						err = probeCtx.Err()
					}
					stop()
					s.record(target, err, time.Since(started))
					select {
					case completed <- target.ID:
					case <-ctx.Done():
						return
					}
				}
			}
		}()
	}
	defer func() { cancel(); close(jobs); workers.Wait() }()
	probeTick := time.NewTicker(c.ProbeInterval)
	defer probeTick.Stop()
	reportTick := time.NewTicker(c.ReportInterval)
	defer reportTick.Stop()
	// One dispatcher, no per-tick goroutine and no unbounded backlog.
	next := 0
	probing := len(s.Targets) > 0
	inflight := make(map[string]bool, len(s.Targets))
	for {
		for probing && inflight[s.Targets[next].ID] {
			next++
			if next == len(s.Targets) {
				next = 0
				probing = false
			}
		}
		var output chan ProbeTarget
		var target ProbeTarget
		if probing {
			output = jobs
			target = s.Targets[next]
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case output <- target:
			inflight[target.ID] = true
			next++
			if next == len(s.Targets) {
				next = 0
				probing = false
			}
		case <-probeTick.C:
			if !probing {
				probing = len(s.Targets) > 0
				next = 0
			}
		case id := <-completed:
			delete(inflight, id)
		case <-reportTick.C:
			r, err := s.Report()
			if err != nil {
				return err
			}
			if err := publish(ctx, r); err != nil {
				return err
			}
		}
	}
}
