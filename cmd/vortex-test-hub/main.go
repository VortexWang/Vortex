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
	flag.Parse()
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
	if err := (&hub.TestServer{Store: s}).ServeGRPC(ctx, ln, config); err != nil && ctx.Err() == nil {
		panic(err)
	}
}
