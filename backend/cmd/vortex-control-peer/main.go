// vortex-control-peer composes the real Phase 5 control clients and SQLite
// stores for the Phase 6 process tracer. The Agent applies a configuration with
// no listeners/rules using the Engine transaction manager; this command never
// claims business-forwarding or funded-runtime activation coverage.
package main

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/google/uuid"
	"vortex/agent"
	"vortex/agent/store"
	"vortex/engine/apply"
	"vortex/hub"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
func run() error {
	mode := flag.String("mode", "agent", "agent or hub")
	id := flag.String("id", "", "UUID identity")
	backend := flag.String("backend-id", "", "expected backend UUID")
	address := flag.String("address", "", "control address")
	database := flag.String("db", "", "SQLite file")
	certPath := flag.String("cert", "", "client certificate")
	keyPath := flag.String("key", "", "client key")
	rootPath := flag.String("ca", "", "backend-role root")
	flag.Parse()
	pair, err := tls.LoadX509KeyPair(*certPath, *keyPath)
	if err != nil {
		return err
	}
	raw, err := os.ReadFile(*rootPath)
	if err != nil {
		return err
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(raw) {
		return errors.New("invalid backend root")
	}
	cfg := &tls.Config{MinVersion: tls.VersionTLS13, MaxVersion: tls.VersionTLS13, Certificates: []tls.Certificate{pair}, RootCAs: roots, ServerName: *backend}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	owner := "agent"
	if *mode == "hub" {
		owner = "hub"
	} else if *mode != "agent" {
		return errors.New("unsupported role")
	}
	s, err := store.Open(*database, owner)
	if err != nil {
		return err
	}
	defer s.Close()
	if owner == "hub" {
		return (&hub.BackendClient{Store: s, HubID: *id, BootID: uuid.NewString(), BackendID: *backend, Region: "phase6", Address: *address, TLS: cfg}).Run(ctx)
	}
	node, err := agent.New(*id, s, nil)
	if err != nil {
		return err
	}
	reconciler := &agent.Reconciler{Store: s, Apply: agent.EngineApplier{Manager: apply.NewManager()}}
	return (&agent.ControlClient{Node: node, TLS: cfg, Reconciler: reconciler}).Connect(ctx, *address)
}
