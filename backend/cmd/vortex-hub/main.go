// vortex-hub hosts control RPCs and durable issuer/accounting journals only.
// Business sockets and traffic remain exclusively in the Node Engine.
package main

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/google/uuid"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"net"
	"vortex/agent"
	"vortex/agent/store"
	"vortex/backend/internal/timeauthority"
	controlv1 "vortex/contracts/protobuf/v1"
	"vortex/hub"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if e := run(ctx, os.Args[1:]); e != nil {
		fmt.Fprintln(os.Stderr, e)
		os.Exit(1)
	}
}

func run(parent context.Context, args []string) error {
	f := flag.NewFlagSet("vortex-hub", flag.ContinueOnError)
	id := f.String("hub-id", "", "Hub UUID")
	region := f.String("region", "", "configured region")
	backend := f.String("backend-id", "", "pinned backend UUID")
	address := f.String("backend", "", "backend HubControl address")
	listen := f.String("node-listen", "127.0.0.1:19093", "NodeControl address")
	path := f.String("db", "hub.db", "local FULL/WAL SQLite file")
	cert := f.String("tls-cert", "", "Hub role certificate PEM")
	key := f.String("tls-key", "", "Hub private key PEM")
	backendCA := f.String("backend-ca", "", "backend role server root PEM")
	nodeCA := f.String("node-ca", "", "Node role client root PEM")
	if e := f.Parse(args); e != nil {
		return e
	}
	for _, value := range []string{*id, *backend} {
		u, e := uuid.Parse(value)
		if e != nil || u.String() != value {
			return errors.New("canonical Hub and backend UUIDs required")
		}
	}
	if f.NArg() != 0 || *region == "" || *address == "" || *backendCA == "" || *nodeCA == "" {
		return errors.New("Hub region, backend and separate role roots required")
	}
	if _, _, e := net.SplitHostPort(*address); e != nil {
		return e
	}
	pair, e := tls.LoadX509KeyPair(*cert, *key)
	if e != nil {
		return e
	}
	leaf, e := x509.ParseCertificate(pair.Certificate[0])
	if e != nil {
		return e
	}
	if leaf.Subject.CommonName != *id || len(leaf.DNSNames) != 1 || leaf.DNSNames[0] != *id {
		return errors.New("Hub certificate identity mismatch")
	}
	readRoot := func(path string) (*x509.CertPool, []byte, error) {
		raw, e := os.ReadFile(path)
		if e != nil {
			return nil, nil, e
		}
		block, rest := pem.Decode(raw)
		if block == nil || block.Type != "CERTIFICATE" || len(bytes.TrimSpace(rest)) != 0 {
			return nil, nil, errors.New("one control role CA certificate required")
		}
		ca, e := x509.ParseCertificate(block.Bytes)
		if e != nil || !ca.IsCA {
			return nil, nil, errors.New("invalid control role CA")
		}
		pool := x509.NewCertPool()
		pool.AddCert(ca)
		return pool, ca.Raw, nil
	}
	nodes, nodeRaw, e := readRoot(*nodeCA)
	if e != nil {
		return e
	}
	backends, backendRaw, e := readRoot(*backendCA)
	if e != nil {
		return e
	}
	if string(nodeRaw) == string(backendRaw) {
		return errors.New("Node and backend role roots must differ")
	}
	unlock, e := agent.LockProcess(*path)
	if e != nil {
		return e
	}
	defer unlock()
	s, e := store.Open(*path, "hub")
	if e != nil {
		return e
	}
	defer s.Close()
	ctx, cancel := context.WithCancel(parent)
	defer cancel()
	clock := timeauthority.New()
	bounds := func() store.IssuerTime {
		lo, hi, trusted := clock.Bounds()
		return store.IssuerTime{Earliest: lo, Latest: hi, Trusted: trusted}
	}
	client := &hub.BackendClient{HubID: *id, BootID: uuid.NewString(), BackendID: *backend, Region: *region, Address: *address, Store: s, Clock: bounds, TLS: &tls.Config{Certificates: []tls.Certificate{pair}, RootCAs: backends, ServerName: *backend, MinVersion: tls.VersionTLS13, MaxVersion: tls.VersionTLS13}}
	relay := &hub.UsageRelay{Store: s, Clock: bounds, Backend: hub.BackendUsageClient{Config: client}}
	server := grpc.NewServer(grpc.Creds(credentials.NewTLS(&tls.Config{Certificates: []tls.Certificate{pair}, ClientCAs: nodes, ClientAuth: tls.RequireAndVerifyClientCert, MinVersion: tls.VersionTLS13, MaxVersion: tls.VersionTLS13})), grpc.MaxRecvMsgSize(1<<20), grpc.MaxSendMsgSize(1<<20))
	controlv1.RegisterNodeControlServer(server, &hub.RuntimeServer{HubID: *id, Region: *region, Issuer: &hub.Issuer{Store: s, HubID: *id, Clock: bounds}, Usage: relay})
	ln, e := net.Listen("tcp", *listen)
	if e != nil {
		return e
	}
	defer ln.Close()
	var workers sync.WaitGroup
	workers.Add(2)
	defer func() { cancel(); server.Stop(); workers.Wait() }()
	go func() {
		defer workers.Done()
		for ctx.Err() == nil {
			_ = client.Run(ctx)
			timer := time.NewTimer(time.Second)
			select {
			case <-ctx.Done():
				timer.Stop()
				return
			case <-timer.C:
			}
		}
	}()
	go func() {
		defer workers.Done()
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				now := bounds()
				if !now.Trusted {
					continue
				}
				status, e := s.HubControlSnapshot(ctx, *id, now.Latest)
				if e == nil && status.Mode == "online" {
					_ = relay.ReplayPending(ctx)
				}
			}
		}
	}()
	failures := make(chan error, 1)
	go func() { failures <- server.Serve(ln) }()
	fmt.Printf("vortex-hub listening hub_id=%s boot_id=%s node=%s; authority requires funded backend state\n", *id, client.BootID, ln.Addr())
	select {
	case <-parent.Done():
		return nil
	case e := <-failures:
		return e
	}
}
