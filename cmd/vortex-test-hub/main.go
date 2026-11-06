package main

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"flag"
	"fmt"
	"net"
	"os"
	"os/signal"
	"syscall"

	"vortex/agent/store"
	"vortex/hub"
)

func main() {
	listen := flag.String("listen", "127.0.0.1:19090", "test Hub listen address")
	dbPath := flag.String("db", "hub.db", "Hub SQLite path")
	certPath := flag.String("tls-cert", "", "Hub certificate PEM")
	keyPath := flag.String("tls-key", "", "Hub private key PEM")
	caPath := flag.String("tls-ca", "", "trusted node CA PEM")
	fixturePath := flag.String("authority-fixture", "", "explicit protocol-backend delegation fixture JSON")
	backendDB := flag.String("fixture-backend-db", "", "separate durable protocol-backend ledger; requires authority-fixture")
	flag.Parse()
	if (*fixturePath == "") != (*backendDB == "") {
		panic("authority-fixture and fixture-backend-db must be supplied together")
	}
	if *fixturePath != "" && *backendDB == *dbPath {
		panic("fixture backend requires a separate database")
	}
	s, err := store.Open(*dbPath, "hub")
	if err != nil {
		panic(err)
	}
	defer s.Close()
	ln, err := net.Listen("tcp", *listen)
	if err != nil {
		panic(err)
	}
	defer ln.Close()
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	fmt.Printf("vortex-test-hub listening=%s protocol=test-only\n", ln.Addr())
	if *certPath == "" || *keyPath == "" || *caPath == "" {
		panic("test Hub requires tls-cert, tls-key and tls-ca")
	}
	cert, err := tls.LoadX509KeyPair(*certPath, *keyPath)
	if err != nil {
		panic(err)
	}
	caPEM, err := os.ReadFile(*caPath)
	if err != nil {
		panic(err)
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(caPEM) {
		panic("invalid node CA")
	}
	config := &tls.Config{MinVersion: tls.VersionTLS13, MaxVersion: tls.VersionTLS13, Certificates: []tls.Certificate{cert}, ClientCAs: pool, ClientAuth: tls.RequireAndVerifyClientCert}
	server := &hub.TestServer{Store: s}
	if *fixturePath != "" {
		fixture, err := loadAuthorityFixture(*fixturePath)
		if err != nil {
			panic(err)
		}
		leaf, err := x509.ParseCertificate(cert.Certificate[0])
		if err != nil {
			panic(err)
		}
		if leaf.Subject.CommonName != fixture.Delegation.HubID || len(leaf.DNSNames) != 1 || leaf.DNSNames[0] != fixture.Delegation.HubID {
			panic("fixture Hub identity must match TLS certificate")
		}
		hubInfo, err := os.Stat(*dbPath)
		if err != nil {
			panic(err)
		}
		if backendInfo, err := os.Stat(*backendDB); err == nil {
			if os.SameFile(hubInfo, backendInfo) {
				panic("fixture backend aliases Hub database")
			}
		} else if !os.IsNotExist(err) {
			panic(err)
		}
		backend, err := store.Open(*backendDB, "hub")
		if err != nil {
			panic(err)
		}
		defer backend.Close()
		server = configureAuthorityFixture(ctx, s, backend, fixture)
		// The explicit backend fixture has no revocation stream. Replay accounting
		// only; do not refresh the heartbeat or reopen the signing epoch.
		if err := server.Usage.ReplayPending(ctx); err != nil {
			if ctx.Err() != nil {
				return
			}
			panic(err)
		}
	}
	if err := server.ServeGRPC(ctx, ln, config); err != nil && ctx.Err() == nil {
		panic(err)
	}
}
