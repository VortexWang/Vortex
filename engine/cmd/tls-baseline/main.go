// tls-baseline is a test-only, independent byte-copy baseline. It intentionally
// has no VLink frames, multiplexing, authorization or accounting execution.
package main

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"os/signal"
	"sort"
	"sync"
	"syscall"
	"time"

	"vortex/engine/runtime/config"
)

func main() {
	path := flag.String("config", "", "same role config as the runtime fixture")
	probe := flag.String("probe", "", "test-only tls or target segment RTT echo")
	output := flag.String("output", "", "probe JSON output")
	flag.Parse()
	cfg, err := config.LoadFile(*path)
	if err != nil {
		log.Fatal(err)
	}
	tlsConfig, err := cfg.TLSConfig(true)
	if err != nil {
		log.Fatal(err)
	}
	tlsConfig.MaxVersion = tls.VersionTLS13
	tlsConfig.NextProtos = []string{"vortex-baseline-rtt/1"}
	if *probe != "" {
		measureSegment(cfg, tlsConfig, *probe, *output)
		return
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()
	address := cfg.Listen
	if cfg.Role == config.RoleSource {
		address = cfg.ServiceListen
	}
	listener, err := net.Listen("tcp", address)
	if err != nil {
		log.Fatal(err)
	}
	var workers sync.WaitGroup
	go func() { <-ctx.Done(); listener.Close() }()
	fmt.Printf("minimal-tls ready role=%s listen=%s\n", cfg.Role, address)
	for {
		incoming, err := listener.Accept()
		if err != nil {
			break
		}
		workers.Add(1)
		go func() {
			defer workers.Done()
			defer incoming.Close()
			left := incoming
			if cfg.Role != config.RoleSource {
				secured := tls.Server(incoming, tlsConfig.Clone())
				handshakeCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
				err := secured.HandshakeContext(handshakeCtx)
				cancel()
				if err != nil {
					return
				}
				if secured.ConnectionState().NegotiatedProtocol == "vortex-baseline-rtt/1" {
					secured.SetDeadline(time.Now().Add(10 * time.Second))
					io.Copy(secured, secured)
					return
				}
				left = secured
			}
			var right net.Conn
			var err error
			if cfg.Role == config.RoleDestination {
				right, err = (&net.Dialer{Timeout: 5 * time.Second}).DialContext(ctx, "tcp", cfg.Target)
			} else {
				right, err = (&tls.Dialer{NetDialer: &net.Dialer{Timeout: 5 * time.Second}, Config: dataTLSConfig(tlsConfig)}).DialContext(ctx, "tcp", cfg.Peer)
			}
			if err != nil {
				return
			}
			defer right.Close()
			cancelIO := context.AfterFunc(ctx, func() { left.Close(); right.Close() })
			defer cancelIO()
			if secured, ok := right.(*tls.Conn); ok {
				state := secured.ConnectionState()
				log.Printf("baseline TLS version=%x cipher=%s", state.Version, tls.CipherSuiteName(state.CipherSuite))
			}
			done := make(chan struct{}, 2)
			copyHalf := func(dst, src net.Conn) {
				_, err := io.Copy(dst, src)
				if err != nil {
					left.Close()
					right.Close()
				} else if half, ok := dst.(interface{ CloseWrite() error }); ok {
					half.CloseWrite()
				}
				done <- struct{}{}
			}
			go copyHalf(right, left)
			go copyHalf(left, right)
			<-done
			<-done
		}()
	}
	workers.Wait()
}

func dataTLSConfig(original *tls.Config) *tls.Config {
	result := original.Clone()
	result.NextProtos = nil
	return result
}

func measureSegment(cfg config.Config, tlsConfig *tls.Config, mode, output string) {
	var conn net.Conn
	var err error
	cipher := "plain-target"
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if mode == "tls" {
		conn, err = (&tls.Dialer{Config: tlsConfig}).DialContext(ctx, "tcp", cfg.Peer)
		if err == nil {
			cipher = tls.CipherSuiteName(conn.(*tls.Conn).ConnectionState().CipherSuite)
		}
	} else if mode == "target" {
		conn, err = (&net.Dialer{}).DialContext(ctx, "tcp", cfg.Target)
	} else {
		log.Fatal("unknown probe mode")
	}
	if err != nil {
		log.Fatal(err)
	}
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(10 * time.Second))
	samples := make([]float64, 0, 50)
	for i := 0; i < 55; i++ {
		start := time.Now()
		if _, err = conn.Write([]byte{42}); err != nil {
			log.Fatal(err)
		}
		var echo [1]byte
		if _, err = io.ReadFull(conn, echo[:]); err != nil || echo[0] != 42 {
			log.Fatal("RTT echo failed: ", err)
		}
		if i >= 5 {
			samples = append(samples, float64(time.Since(start).Nanoseconds())/1e6)
		}
	}
	sorted := append([]float64(nil), samples...)
	sort.Float64s(sorted)
	data, _ := json.MarshalIndent(map[string]any{"samples_ms": samples, "median_ms": (sorted[24] + sorted[25]) / 2, "cipher": cipher, "role": cfg.Role, "measurement": "established TLS data echo RTT, or plain destination-target echo; userspace overhead included"}, "", "  ")
	if err = os.WriteFile(output, append(data, '\n'), 0644); err != nil {
		log.Fatal(err)
	}
}
