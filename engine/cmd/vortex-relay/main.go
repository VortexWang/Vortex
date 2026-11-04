package main

import (
	"context"
	"crypto/sha256"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync"
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
	flag.Parse()
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
	gate, err := runtimeaccounting.NewGate(100000)
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
	fmt.Printf("vortex-relay ready node_id=%s role=%s listen=%s\n", cfg.NodeID, cfg.Role, cfg.Listen)
	switch cfg.Role {
	case config.RoleSource:
		if err := serveSource(ctx, cfg, supervisor); err != nil && ctx.Err() == nil {
			fatal(err.Error())
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
	digest := sha256.Sum256([]byte(cfg.RuleID))
	request := vlink.OpenRequest{RuleRevision: cfg.RuleRevision, PathGeneration: cfg.PathGeneration, HopIndex: cfg.HopIndex, Network: 1}
	copy(request.RuleID[:], digest[:16])
	if tcp, ok := conn.RemoteAddr().(*net.TCPAddr); ok {
		if ip := tcp.IP.To4(); ip != nil {
			request.ClientAddress = append([]byte(nil), ip...)
		} else {
			request.ClientAddress = append([]byte(nil), tcp.IP.To16()...)
		}
	}
	return request
}

func dialTarget(_ context.Context, target vlink.LocalTarget) (net.Conn, error) {
	port := strconv.Itoa(int(target.Port))
	return net.Dial("tcp", net.JoinHostPort(strings.Trim(target.Host, "[]"), port))
}

func fatal(message string) {
	_, _ = fmt.Fprintln(os.Stderr, message)
	os.Exit(2)
}
