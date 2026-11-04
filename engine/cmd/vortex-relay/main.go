package main

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"runtime/pprof"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
	coreaccounting "vortex/engine/accounting"

	runtimeaccounting "vortex/engine/runtime/accounting"
	"vortex/engine/runtime/config"
	"vortex/engine/runtime/link"
	"vortex/engine/runtime/relay"
	"vortex/engine/share"
	"vortex/engine/vlink"
)

func main() {
	configPath := flag.String("config", "", "runtime JSON configuration")
	role := flag.String("role", "", "optional role override")
	cpuProfile := flag.String("cpu-profile", "", "optional diagnostic CPU profile file")
	flag.Parse()
	if *cpuProfile != "" {
		file, err := os.Create(*cpuProfile)
		if err != nil {
			fatal(err.Error())
		}
		if err := pprof.StartCPUProfile(file); err != nil {
			fatal(err.Error())
		}
		defer file.Close()
		defer pprof.StopCPUProfile()
	}
	if *configPath == "" {
		fatal("--config is required")
	}
	cfg, err := config.LoadFile(*configPath)
	if err != nil {
		fatal(err.Error())
	}
	if *role != "" && config.Role(*role) != cfg.Role {
		fatal("--role does not match config role")
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	initialConfig := cfg
	cfg = cfg.Effective()
	supervisor, err := link.New(cfg)
	if err != nil {
		fatal(err.Error())
	}
	policy, err := relay.StaticPolicy(cfg)
	if err != nil {
		fatal(err.Error())
	}
	maxEvents, err := accountingEventLimit(cfg.EvidenceFile, os.Getenv("VORTEX_QUALIFICATION_EVENT_LIMIT"))
	if err != nil {
		fatal(err.Error())
	}
	gate, err := runtimeaccounting.NewGate(maxEvents)
	if err != nil {
		fatal(err.Error())
	}
	var sourceGate coreaccounting.Gate
	if cfg.SourceCreditBook != nil {
		book, err := runtimeaccounting.NewCreditBook(*cfg.SourceCreditBook)
		if err != nil {
			fatal(err.Error())
		}
		var injectedGate coreaccounting.Gate = book
		if cfg.PolicyWindow != nil {
			w := cfg.PolicyWindow
			boundary, boundaryErr := runtimeaccounting.NewPolicyBoundary(book,
				runtimeaccounting.SystemPolicyClock{Uncertainty: time.Duration(w.UncertaintyMS) * time.Millisecond},
				runtimeaccounting.PolicyWindow{
					OldSnapshot: w.OldSnapshot, NewSnapshot: w.NewSnapshot, At: w.At,
					PreparedAt: w.PreparedAt, ExpiresAt: w.ExpiresAt,
					PolicyLeaseUntil: w.PolicyLeaseUntil, RuntimeLeaseUntil: w.RuntimeLeaseUntil,
					ShareLeaseUntil: w.ShareLeaseUntil, HardDeadline: w.HardDeadline,
					SelectionRevision: w.SelectionRevision,
				})
			if boundaryErr != nil {
				fatal(fmt.Sprintf("policy window: %v", boundaryErr))
			}
			injectedGate = boundary
		}
		sourceGate = injectedGate
		stopReload := watchCreditBook(ctx, *configPath, initialConfig, book)
		defer func() {
			stopReload()
			data, err := json.MarshalIndent(book.State(), "", "  ")
			if err == nil {
				err = os.WriteFile(cfg.EvidenceFile+".credit-book.json", append(data, '\n'), 0600)
			}
			if err != nil {
				fatal(fmt.Sprintf("write snapshot evidence: %v", err))
			}
		}()
	}

	if cfg.SourceCredit != nil {
		credit, creditErr := runtimeaccounting.NewCreditGate(*cfg.SourceCredit)
		if creditErr != nil {
			fatal(creditErr.Error())
		}
		sourceGate = credit
		defer func() {
			data, err := json.MarshalIndent(credit.State(), "", "  ")
			if err == nil {
				err = os.WriteFile(cfg.EvidenceFile+".credit.json", append(data, '\n'), 0600)
			}
			if err != nil {
				fatal(fmt.Sprintf("write credit evidence: %v", err))
			}
		}()
	}
	if cfg.Role == config.RoleSource && sourceGate == nil {
		if !cfg.QualificationOnly {
			fatal("source role requires a funded source credit gate")
		}
		// This bounded in-memory adapter is permitted only for explicit test topology runs.
		sourceGate = gate
	}
	if cfg.Share != nil {
		wrapped, err := configureShareGate(cfg, sourceGate)
		if err != nil {
			fatal(err.Error())
		}
		sourceGate = wrapped
		slog.Info("source share lease active", "generation", cfg.Share.Revision, "lease_until", cfg.Share.LeaseUntil)
	}
	service := relay.Service{LocalNode: supervisor.NodeID, Policy: policy, Dial: dialTarget, Gate: gate}
	if cfg.TransportMode != "" && cfg.Role != config.RoleDestination {
		supervisor.Transport, err = link.NewTransport(ctx, supervisor, cfg.TransportMode, cfg.PoolBounds())
		if err != nil {
			fatal(err.Error())
		}
		defer supervisor.Transport.Close()
		if cfg.TransportMode == vlink.ModeAuto && cfg.Role == config.RoleRelay {
			service.ObserveFirstTargetByte = func(latency time.Duration) {
				if err := supervisor.Transport.ObserveFirstPacket(latency); err != nil {
					slog.Warn("vlink auto-mode sample rejected", "err", err)
				}
			}
		}
		if cfg.Role == config.RoleSource {
			if err := warmTransport(ctx, supervisor.Transport); err != nil {
				return
			}
		}
	}
	if len(initialConfig.Routes) > 0 {
		supervisor.Routes, err = link.NewRoutes(ctx, initialConfig, supervisor)
		if err != nil {
			fatal(err.Error())
		}
		defer supervisor.Routes.Close()
		if cfg.TransportMode == vlink.ModeAuto {
			service.ObserveRouteFirstTargetByte = supervisor.Routes.Observe
		}
		service.Policy.GenerationAdmission = func(_ context.Context, req vlink.OpenRequest) error {
			return supervisor.Routes.Admit(req.PathGeneration)
		}
		originalLookup := service.Policy.Lookup
		service.Policy.Lookup = func(ctx context.Context, id [16]byte) (vlink.RuleSnapshot, bool) {
			active := supervisor.Routes.Snapshot()
			dynamic, err := relay.StaticPolicy(active)
			if err != nil {
				return vlink.RuleSnapshot{}, false
			}
			if dynamic.Lookup == nil {
				return originalLookup(ctx, id)
			}
			return dynamic.Lookup(ctx, id)
		}
		reload := make(chan os.Signal, 1)
		signal.Notify(reload, syscall.SIGHUP)
		defer signal.Stop(reload)
		go func() {
			for {
				select {
				case <-ctx.Done():
					return
				case <-reload:
					next, err := config.LoadFile(*configPath)
					if err == nil {
						applyCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
						err = supervisor.Routes.Apply(applyCtx, next)
						cancel()
					}
					if err != nil {
						slog.Warn("runtime route reload rejected", "err", err)
					}
				}
			}
		}()
	}
	defer func() {
		if cfg.EvidenceFile == "" {
			return
		}
		file, openErr := os.OpenFile(cfg.EvidenceFile, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
		if openErr != nil {
			fatal(fmt.Sprintf("open accounting evidence: %v", openErr))
		}
		writeErr := gate.WriteJSONL(file)
		closeErr := file.Close()
		if err := errors.Join(writeErr, closeErr); err != nil {
			fatal(fmt.Sprintf("write accounting evidence: %v", err))
		}
	}()
	defer func() {
		supervisor.Close()
		service.Shutdown()
	}()
	fmt.Printf("vortex-relay ready node_id=%s role=%s listen=%s\n", cfg.NodeID, cfg.Role, cfg.Listen)
	switch cfg.Role {
	case config.RoleSource:
		var udpDone chan struct{}
		if cfg.UDPServiceListen != "" {
			udpDone = make(chan struct{})
			go func() {
				defer close(udpDone)
				if err := serveUDPSource(ctx, cfg, supervisor, sourceGate); err != nil && ctx.Err() == nil {
					fatal(err.Error())
				}
			}()
		}
		if err := serveSource(ctx, cfg, supervisor, sourceGate); err != nil && ctx.Err() == nil {
			fatal(err.Error())
		}
		if udpDone != nil {
			<-udpDone
		}
	case config.RoleRelay:
		if supervisor.Transport != nil {
			if supervisor.Routes != nil {
				service.AcquireRoute = func(ctx context.Context, req vlink.OpenRequest) (*vlink.MuxLink, func(), context.Context, error) {
					return supervisor.Routes.Acquire(ctx, req.PathGeneration, vlink.StreamNetwork(req.Network))
				}
			} else {
				service.AcquireNext = supervisor.Transport.Acquire
			}
			go func() {
				if warmTransport(ctx, supervisor.Transport) == nil {
					slog.Info("vlink next-hop ready", "mode", cfg.TransportMode)
				}
			}()
		} else {
			var nextMu sync.Mutex
			var next *vlink.MuxLink
			service.Next = func() *vlink.MuxLink { nextMu.Lock(); defer nextMu.Unlock(); return next }
			go func() {
				for ctx.Err() == nil {
					err := supervisor.Dial(ctx, func(ctx context.Context, link *vlink.MuxLink, _ vlink.NodeID) error {
						nextMu.Lock()
						next = link
						nextMu.Unlock()
						slog.Info("vlink next-hop ready")
						<-link.Closed()
						nextMu.Lock()
						if next == link {
							next = nil
						}
						nextMu.Unlock()
						if err := link.Err(); err != nil && !errors.Is(err, vlink.ErrProtocol) {
							return err
						}
						return nil
					})
					if err != nil && ctx.Err() == nil {
						slog.Error("vlink next-hop dial failed", "err", err)
					}
					select {
					case <-ctx.Done():
						return
					case <-time.After(500 * time.Millisecond):
					}
				}
			}()
		}
		if err := supervisor.Serve(ctx, func(ctx context.Context, mux *vlink.MuxLink, _ vlink.NodeID) error {
			return service.ServeLink(ctx, mux)
		}); err != nil && ctx.Err() == nil {
			fatal(err.Error())
		}
	case config.RoleDestination:
		if err := supervisor.Serve(ctx, func(ctx context.Context, mux *vlink.MuxLink, _ vlink.NodeID) error {
			return service.ServeLink(ctx, mux)
		}); err != nil && ctx.Err() == nil {
			fatal(err.Error())
		}
	}
}

func configureShareGate(cfg config.Config, inner coreaccounting.Gate) (coreaccounting.Gate, error) {
	if cfg.Share == nil || inner == nil {
		return nil, errors.New("share: configured source gate is required")
	}
	nodeID, err := vlink.ParseNodeID(cfg.NodeID)
	if err != nil {
		return nil, fmt.Errorf("share: node_id: %w", err)
	}
	bootID, err := vlink.ParseNodeID(cfg.Share.BootID)
	if err != nil {
		return nil, fmt.Errorf("share: boot_id: %w", err)
	}
	transactionID, err := vlink.ParseNodeID(cfg.Share.TransactionID)
	if err != nil {
		return nil, fmt.Errorf("share: transaction_id: %w", err)
	}
	key := share.Key{Node: [16]byte(nodeID), Boot: [16]byte(bootID)}
	manager := share.NewManager()
	if err := manager.BeginBoot(key); err != nil {
		return nil, fmt.Errorf("share: begin boot: %w", err)
	}
	revision, err := manager.PrepareWithID(key, [16]byte(transactionID), cfg.Share.Rate, cfg.Share.Connections)
	if err != nil {
		return nil, fmt.Errorf("share: prepare: %w", err)
	}
	if cfg.Share.Revision != 0 && cfg.Share.Revision != revision.Generation {
		return nil, fmt.Errorf("share: stale revision %d, prepared %d", cfg.Share.Revision, revision.Generation)
	}
	if _, err := manager.Commit(key, revision.Generation); err != nil {
		return nil, fmt.Errorf("share: commit: %w", err)
	}
	return &runtimeaccounting.ShareGate{Manager: manager, Key: key, Inner: inner, LeaseUntil: cfg.Share.LeaseUntil}, nil
}

func warmTransport(ctx context.Context, transport *link.Transport) error {
	backoff := vlink.Backoff{Initial: 250 * time.Millisecond, Maximum: 3 * time.Second}
	for {
		if err := transport.Warm(ctx); err == nil {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(backoff.Next()):
		}
	}
}

func resetStream(stream *vlink.MuxStream) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_ = stream.Reset(ctx, 0)
}

func accountingEventLimit(evidenceFile, configured string) (int, error) {
	const defaultLimit = 1000000
	if evidenceFile == "" || configured == "" {
		return defaultLimit, nil
	}
	parsed, err := strconv.Atoi(configured)
	if err != nil || parsed < 1 || parsed > 10000000 {
		return 0, errors.New("VORTEX_QUALIFICATION_EVENT_LIMIT must be between 1 and 10000000")
	}
	return parsed, nil
}

func serveSource(ctx context.Context, cfg config.Config, supervisor *link.Supervisor, gate coreaccounting.Gate) error {
	var workers sync.WaitGroup
	defer workers.Wait()
	listener, err := net.Listen("tcp", cfg.ServiceListen)
	if err != nil {
		return err
	}
	defer listener.Close()
	go func() { <-ctx.Done(); _ = listener.Close(); supervisor.Close() }()
	for {
		conn, err := listener.Accept()
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			continue
		}
		workers.Add(1)
		go func(local net.Conn) {
			defer workers.Done()
			defer local.Close()
			openedAt := time.Now()
			flowConfig := cfg
			if supervisor.Routes != nil {
				flowConfig = supervisor.Routes.Snapshot()
			}
			if err := useRoute(ctx, flowConfig, supervisor, vlink.StreamTCP, func(ctx context.Context, mux *vlink.MuxLink, _ vlink.NodeID) error {
				request := openRequest(flowConfig, local)
				stream, err := mux.Open(ctx, request)
				if err != nil {
					return err
				}
				if supervisor.Transport != nil {
					defer resetStream(stream)
				}
				if err := stream.WaitOpen(ctx); err != nil {
					return err
				}
				slog.Info("vlink source OPEN authorized", "flow_key", stream.FlowKey(), "mode", cfg.TransportMode)
				return relay.BridgeConnWithGateObserver(ctx, stream, local, gate, func() {
					if supervisor.Transport != nil && cfg.TransportMode == vlink.ModeAuto {
						if supervisor.Routes != nil {
							supervisor.Routes.Observe(flowConfig.PathGeneration, time.Since(openedAt))
							return
						}
						if err := supervisor.Transport.ObserveFirstPacket(time.Since(openedAt)); err != nil {
							slog.Warn("vlink auto-mode sample rejected", "err", err)
						}
					}
				})
			}); err != nil && ctx.Err() == nil {
				slog.Error("vlink source dial failed", "err", err)
			}
		}(conn)
	}
}

func openRequest(cfg config.Config, conn net.Conn) vlink.OpenRequest {
	return makeOpenRequest(cfg, conn.RemoteAddr(), 1)
}

func makeOpenRequest(cfg config.Config, addr net.Addr, network uint8) vlink.OpenRequest {
	digest := sha256.Sum256([]byte(cfg.RuleID))
	request := vlink.OpenRequest{RuleRevision: cfg.RuleRevision, PathGeneration: cfg.PathGeneration, HopIndex: cfg.HopIndex, Network: network}
	copy(request.RuleID[:], digest[:16])
	if tcp, ok := addr.(*net.TCPAddr); ok {
		if ip := tcp.IP.To4(); ip != nil {
			request.ClientAddress = append([]byte(nil), ip...)
		} else {
			request.ClientAddress = append([]byte(nil), tcp.IP.To16()...)
		}
	} else if udp, ok := addr.(*net.UDPAddr); ok {
		if ip := udp.IP.To4(); ip != nil {
			request.ClientAddress = append([]byte(nil), ip...)
		} else {
			request.ClientAddress = append([]byte(nil), udp.IP.To16()...)
		}
	}
	return request
}

func serveUDPSource(ctx context.Context, cfg config.Config, supervisor *link.Supervisor, gate coreaccounting.Gate) error {
	packet, err := net.ListenPacket("udp", cfg.UDPServiceListen)
	if err != nil {
		return err
	}
	defer packet.Close()
	if udp, ok := packet.(*net.UDPConn); ok {
		if err := udp.SetReadBuffer(4 * 1024 * 1024); err != nil {
			return err
		}
	}
	go func() { <-ctx.Done(); _ = packet.Close(); supervisor.Close() }()
	var workers sync.WaitGroup
	defer workers.Wait()
	var queueDrops, oversizedDrops, capacityDrops atomic.Uint64
	defer func() {
		slog.Info("udp source drop summary", "queue_drops", queueDrops.Load(), "oversized_drops", oversizedDrops.Load(), "capacity_drops", capacityDrops.Load())
	}()
	type flow struct {
		out    chan []byte
		done   chan struct{}
		queued atomic.Int64
	}
	flows := make(map[string]*flow)
	const maxFlows = 1024
	buffer := make([]byte, 65535)
	for {
		n, peer, err := packet.ReadFrom(buffer)
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return err
		}
		for key, stale := range flows {
			select {
			case <-stale.done:
				delete(flows, key)
			default:
			}
		}
		key := peer.String()
		current := flows[key]
		if current != nil {
			select {
			case <-current.done:
				delete(flows, key)
				current = nil
			default:
			}
		}
		if current == nil {
			if len(flows) >= maxFlows {
				capacityDrops.Add(1)
				continue
			}
			current = &flow{out: make(chan []byte, 256), done: make(chan struct{})}
			flows[key] = current
			workers.Add(1)
			go func(current *flow, peer net.Addr) {
				defer workers.Done()
				flowConfig := cfg
				if supervisor.Routes != nil {
					flowConfig = supervisor.Routes.Snapshot()
				}
				runUDPSourceFlow(ctx, flowConfig, supervisor, packet, peer, current.out, current.done, &current.queued, gate)
			}(current, peer)
		}
		payload := append([]byte(nil), buffer[:n]...)
		if len(payload) > vlink.MaxPayloadSize {
			oversizedDrops.Add(1)
			continue
		}
		if current.queued.Add(int64(len(payload))) > 256*1024 {
			current.queued.Add(-int64(len(payload)))
			queueDrops.Add(1)
			continue
		}
		select {
		case current.out <- payload:
		default:
			current.queued.Add(-int64(len(payload)))
			queueDrops.Add(1)
		}
	}
}

func runUDPSourceFlow(ctx context.Context, cfg config.Config, supervisor *link.Supervisor, packet net.PacketConn, peer net.Addr, outgoing <-chan []byte, done chan<- struct{}, queued *atomic.Int64, gate coreaccounting.Gate) {
	defer close(done)
	if err := useRoute(ctx, cfg, supervisor, vlink.StreamUDP, func(ctx context.Context, mux *vlink.MuxLink, _ vlink.NodeID) error {
		stream, err := mux.Open(ctx, makeOpenRequest(cfg, peer, 2))
		if err != nil {
			return err
		}
		if supervisor.Transport != nil {
			defer resetStream(stream)
		}
		if err := stream.WaitOpen(ctx); err != nil {
			return err
		}
		var session coreaccounting.Session
		if gate != nil {
			session, err = gate.Open(ctx, coreaccounting.Flow{Key: stream.FlowKey()})
			if err != nil {
				return err
			}
			defer session.Close()
		}
		flowCtx, cancel := context.WithCancel(ctx)
		ctx = flowCtx
		receivedDone := make(chan struct{})
		// Join before closing the credit session. Cancellation/reset unblocks
		// Receive; an already received datagram settles before session.Close.
		defer func() { cancel(); resetStream(stream); <-receivedDone }()
		slog.Info("udp source session opened", "generation", cfg.PathGeneration, "flow_key", stream.FlowKey())
		defer slog.Info("udp source session closed", "generation", cfg.PathGeneration, "flow_key", stream.FlowKey())
		recvErr := make(chan error, 1)
		activity := make(chan struct{}, 1)
		go func() {
			defer close(receivedDone)
			var owned vlink.Frame
			defer func() { owned.Release() }()
			for {
				owned.Release()
				frame, err := stream.Receive(ctx)
				owned = frame
				if err != nil {
					recvErr <- err
					return
				}
				if frame.Type != vlink.FrameDgram {
					recvErr <- vlink.ErrProtocol
					return
				}
				if _, err := authorizedDatagram(ctx, session, coreaccounting.LeftToRight, frame.Payload, false, func(p []byte) (int, error) { return packet.WriteTo(p, peer) }); err != nil {
					recvErr <- err
					return
				}
				select {
				case activity <- struct{}{}:
				default:
				}
				if err := stream.Consume(ctx, uint32(len(frame.Payload))); err != nil {
					recvErr <- err
					return
				}
			}
		}()
		idle := time.NewTimer(60 * time.Second)
		defer idle.Stop()
		for {
			select {
			case payload := <-outgoing:
				queued.Add(-int64(len(payload)))
				accepted, err := authorizedDatagram(ctx, session, coreaccounting.RightToLeft, payload, true, func(p []byte) (int, error) {
					ok, err := stream.WriteDgram(ctx, p)
					if ok {
						return len(p), err
					}
					return 0, err
				})
				if err != nil {
					return err
				}
				if !accepted {
					continue
				} else {
					if !idle.Stop() {
						select {
						case <-idle.C:
						default:
						}
					}
					idle.Reset(60 * time.Second)
				}
			case <-activity:
				if !idle.Stop() {
					select {
					case <-idle.C:
					default:
					}
				}
				idle.Reset(60 * time.Second)
			case err := <-recvErr:
				return err
			case <-idle.C:
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		}
	}); err != nil && ctx.Err() == nil {
		slog.Warn("udp source session ended", "err", err)
	}
}

func dialTarget(_ context.Context, network uint8, target vlink.LocalTarget) (net.Conn, error) {
	port := strconv.Itoa(int(target.Port))
	protocol := "tcp"
	if network == 2 {
		protocol = "udp"
	}
	conn, err := net.Dial(protocol, net.JoinHostPort(strings.Trim(target.Host, "[]"), port))
	if err != nil {
		return nil, err
	}
	if udp, ok := conn.(*net.UDPConn); ok {
		if err := udp.SetReadBuffer(4 * 1024 * 1024); err != nil {
			_ = conn.Close()
			return nil, err
		}
	}
	return conn, nil
}

func fatal(message string) {
	_, _ = fmt.Fprintln(os.Stderr, message)
	os.Exit(2)
}

func useRoute(ctx context.Context, cfg config.Config, s *link.Supervisor, network vlink.StreamNetwork, handler link.LinkHandler) error {
	if s.Routes == nil {
		return s.Use(ctx, network, handler)
	}
	mux, release, udp, err := s.Routes.Acquire(ctx, cfg.PathGeneration, network)
	if err != nil {
		return err
	}
	defer release()
	if network == vlink.StreamUDP {
		ctx = udp
	}
	return handler(ctx, mux, vlink.NodeID{})
}
