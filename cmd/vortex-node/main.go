package main

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"vortex/agent"
	"vortex/agent/store"
)

func main() {
	nodeID := flag.String("node-id", "node-1", "node identity")
	dbPath := flag.String("db", "agent.db", "Agent SQLite path")
	hubAddr := flag.String("hub", "", "protocol-test Hub address")
	certPath := flag.String("tls-cert", "", "node certificate PEM")
	keyPath := flag.String("tls-key", "", "node private key PEM")
	caPath := flag.String("tls-ca", "", "trusted Hub CA PEM")
	hubName := flag.String("hub-name", "", "Hub certificate DNS identity")
	engineConfig := flag.String("engine-config", "", "Engine runtime JSON configuration")
	flag.Parse()
	s, err := store.Open(*dbPath, "agent")
	if err != nil {
		fatal(err)
	}
	defer s.Close()
	var engine agent.Engine
	if *engineConfig != "" {
		engine, err = agent.NewRuntimeEngine(*engineConfig)
		if err != nil {
			fatal(err)
		}
	}
	n, err := agent.New(*nodeID, s, engine)
	if err != nil {
		fatal(err)
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	if err := n.Start(ctx); err != nil {
		fatal(err)
	}
	if *hubAddr == "" {
		fmt.Printf("vortex-node node_id=%s boot_id=%s\n", n.NodeID, n.BootID)
		<-ctx.Done()
		return
	}
	if *certPath == "" || *keyPath == "" || *caPath == "" || *hubName == "" {
		fatal(fmt.Errorf("hub connection requires tls-cert, tls-key, tls-ca and hub-name"))
	}
	certificate, err := tls.LoadX509KeyPair(*certPath, *keyPath)
	if err != nil {
		fatal(err)
	}
	leaf, err := x509.ParseCertificate(certificate.Certificate[0])
	if err != nil {
		fatal(err)
	}
	if len(leaf.DNSNames) != 1 || leaf.DNSNames[0] != n.NodeID {
		fatal(fmt.Errorf("node certificate SAN does not match node-id"))
	}
	caPEM, err := os.ReadFile(*caPath)
	if err != nil {
		fatal(err)
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(caPEM) {
		fatal(fmt.Errorf("invalid Hub CA"))
	}
	client := &agent.ControlClient{Node: n, TLS: &tls.Config{MinVersion: tls.VersionTLS13, MaxVersion: tls.VersionTLS13, Certificates: []tls.Certificate{certificate}, RootCAs: roots, ServerName: *hubName}}
	if err := client.Connect(ctx, *hubAddr); err != nil {
		fatal(err)
	}
	if err := n.Heartbeat(ctx, 1); err != nil {
		fatal(err)
	}
	fmt.Printf("vortex-node connected node_id=%s boot_id=%s\n", n.NodeID, n.BootID)
}

func fatal(err error) { fmt.Fprintln(os.Stderr, err); os.Exit(1) }
