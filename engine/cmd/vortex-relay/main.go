package main

import (
	"context"
	"crypto/sha256"
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

	runtimeaccounting "vortex/engine/runtime/accounting"
	"vortex/engine/runtime/config"
	"vortex/engine/runtime/link"
	"vortex/engine/runtime/relay"
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
	supervisor, err := link.New(cfg)
	if err != nil {
		fatal(err.Error())
	}
	policy, err := relay.StaticPolicy(cfg)
	if err != nil {
		fatal(err.Error())
	}
	gate, err := runtimeaccounting.NewGate(1000000)
	if err != nil {
		fatal(err.Error())
	}
	service := relay.Service{LocalNode: supervisor.NodeID, Policy: policy, Dial: dialTarget, Gate: gate}
	defer func() {
		if cfg.EvidenceFile == "" {
			return
		}
		file, openErr := os.OpenFile(cfg.EvidenceFile, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
		if openErr == nil {
			defer file.Close()
			_ = gate.WriteJSONL(file)
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
				if err := serveUDPSource(ctx, cfg, supervisor); err != nil && ctx.Err() == nil {
					fatal(err.Error())
				}
			}()
		}
		if err := serveSource(ctx, cfg, supervisor); err != nil && ctx.Err() == nil {
			fatal(err.Error())
		}
		if udpDone != nil {
			<-udpDone
		}
	case config.RoleRelay:
		var nextMu sync.Mutex
		var next *vlink.MuxLink
		service.Next = func() *vlink.MuxLink { nextMu.Lock(); defer nextMu.Unlock(); return next }
		go func() {
			for ctx.Err() == nil {
				err := supervisor.Dial(ctx, func(ctx context.Context, link *vlink.MuxLink, _ vlink.NodeID) error {
					nextMu.Lock()
					next = link
					nextMu.Unlock()
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
				} else if err == nil {
					slog.Info("vlink next-hop established")
				}
				select {
				case <-ctx.Done():
					return
				case <-time.After(500 * time.Millisecond):
				}
			}
		}()
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

func serveSource(ctx context.Context, cfg config.Config, supervisor *link.Supervisor) error {
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
		go func(local net.Conn) {
			defer local.Close()
			if err := supervisor.Dial(ctx, func(ctx context.Context, mux *vlink.MuxLink, _ vlink.NodeID) error {
				request := openRequest(cfg, local)
				stream, err := mux.Open(ctx, request)
				if err != nil {
					return err
				}
				if err := stream.WaitOpen(ctx); err != nil {
					return err
				}
				slog.Info("vlink source OPEN authorized")
				return relay.BridgeConn(ctx, stream, local)
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

func serveUDPSource(ctx context.Context, cfg config.Config, supervisor *link.Supervisor) error {
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
		out  chan []byte
		done chan struct{}
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
			current = &flow{out: make(chan []byte, 64), done: make(chan struct{})}
			flows[key] = current
			workers.Add(1)
			go func(current *flow, peer net.Addr) {
				defer workers.Done()
				runUDPSourceFlow(ctx, cfg, supervisor, packet, peer, current.out, current.done)
			}(current, peer)
		}
		payload := append([]byte(nil), buffer[:n]...)
		if len(payload) > vlink.MaxPayloadSize {
			oversizedDrops.Add(1)
			continue
		}
		select {
		case current.out <- payload:
		default:
			queueDrops.Add(1)
		}
	}
}

func runUDPSourceFlow(ctx context.Context, cfg config.Config, supervisor *link.Supervisor, packet net.PacketConn, peer net.Addr, outgoing <-chan []byte, done chan<- struct{}) {
	defer close(done)
	if err := supervisor.Dial(ctx, func(ctx context.Context, mux *vlink.MuxLink, _ vlink.NodeID) error {
		stream, err := mux.Open(ctx, makeOpenRequest(cfg, peer, 2))
		if err != nil {
			return err
		}
		if err := stream.WaitOpen(ctx); err != nil {
			return err
		}
		recvErr := make(chan error, 1)
		activity := make(chan struct{}, 1)
		go func() {
			for {
				frame, err := stream.Receive(ctx)
				if err != nil {
					recvErr <- err
					return
				}
				if frame.Type != vlink.FrameDgram {
					recvErr <- vlink.ErrProtocol
					return
				}
				if _, err := packet.WriteTo(frame.Payload, peer); err != nil {
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
				accepted, err := stream.WriteDgram(ctx, payload)
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
