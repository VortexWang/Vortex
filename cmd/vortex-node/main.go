package main

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"database/sql"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"vortex/agent"
	"vortex/agent/store"
)

func main() {
	nodeID := flag.String("node-id", "node-1", "node identity")
	dbPath := flag.String("db", "agent.db", "Agent SQLite path")
	hubAddr := flag.String("hub", "", "protocol-test Hub address")
	enrollmentAddr := flag.String("enrollment-hub", "", "server-authenticated one-time enrollment endpoint")
	certPath := flag.String("tls-cert", "", "node certificate PEM")
	keyPath := flag.String("tls-key", "", "node private key PEM")
	caPath := flag.String("tls-ca", "", "trusted Hub CA PEM")
	hubName := flag.String("hub-name", "", "Hub certificate DNS identity")
	engineConfig := flag.String("engine-config", "", "Engine runtime JSON configuration")
	credentialDir := flag.String("credential-dir", "credentials", "directory for locally generated node credentials")
	flag.Parse()
	s, err := store.Open(*dbPath, "agent")
	if err != nil {
		fatal(err)
	}
	defer s.Close()
	var engine agent.Engine
	if *engineConfig != "" {
		engine, err = agent.NewRuntimeEngine(*engineConfig, *nodeID)
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
	defer func() {
		stopCtx, stopCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer stopCancel()
		if err := n.Stop(stopCtx); err != nil {
			fmt.Fprintln(os.Stderr, "engine shutdown:", err)
		}
	}()
	if *hubAddr == "" {
		fmt.Printf("vortex-node node_id=%s boot_id=%s\n", n.NodeID, n.BootID)
		if runtimeEngine, ok := engine.(*agent.RuntimeEngine); ok {
			select {
			case <-ctx.Done():
			case <-runtimeEngine.Done():
				if err := runtimeEngine.Err(); err != nil {
					fatal(err)
				}
			}
		} else {
			<-ctx.Done()
		}
		return
	}
	if *hubName == "" {
		fatal(errors.New("hub-name is required for a Hub connection"))
	}
	if *certPath == "" {
		*certPath = *credentialDir + "/runtime-cert.pem"
	}
	if *keyPath == "" {
		*keyPath = *credentialDir + "/runtime-key.pem"
	}
	if *caPath == "" {
		*caPath = *credentialDir + "/trust-ca.pem"
	}
	certificate, loadErr := tls.LoadX509KeyPair(*certPath, *keyPath)
	var roots *x509.CertPool
	if loadErr != nil {
		if !errors.Is(loadErr, os.ErrNotExist) {
			fatal(loadErr)
		}
		token := os.Getenv("VORTEX_ENROLLMENT_TOKEN")
		bootstrapCA := os.Getenv("VORTEX_HUB_CA")
		if token == "" || bootstrapCA == "" || *enrollmentAddr == "" {
			fatal(fmt.Errorf("node credentials are missing; set VORTEX_ENROLLMENT_TOKEN, VORTEX_HUB_CA and --enrollment-hub for initial registration"))
		}
		caPEM, readErr := os.ReadFile(bootstrapCA)
		if readErr != nil {
			fatal(readErr)
		}
		roots = x509.NewCertPool()
		if !roots.AppendCertsFromPEM(caPEM) {
			fatal(errors.New("invalid bootstrap Hub CA"))
		}
		credential, registerErr := agent.Register(ctx, n.NodeID, token, *enrollmentAddr, *credentialDir, roots, *hubName, s)
		if registerErr != nil {
			fatal(registerErr)
		}
		certificate = credential.Certificate
		if err := os.Unsetenv("VORTEX_ENROLLMENT_TOKEN"); err != nil {
			fatal(err)
		}
		fmt.Printf("vortex-node enrolled node_id=%s expires=%s\n", n.NodeID, credential.Leaf.NotAfter.UTC().Format(time.RFC3339))
	} else {
		caPEM, readErr := os.ReadFile(*caPath)
		if readErr != nil {
			fatal(readErr)
		}
		roots = x509.NewCertPool()
		if !roots.AppendCertsFromPEM(caPEM) {
			fatal(errors.New("invalid Hub CA"))
		}
	}
	leaf, err := x509.ParseCertificate(certificate.Certificate[0])
	if err != nil {
		fatal(err)
	}
	if len(leaf.DNSNames) != 1 || leaf.DNSNames[0] != n.NodeID {
		fatal(errors.New("node certificate SAN does not match node-id"))
	}
	storedCertificate, storedExpiry, credentialErr := s.Credential(ctx, n.NodeID)
	if credentialErr == nil {
		if string(storedCertificate) != string(leaf.Raw) || !storedExpiry.Equal(leaf.NotAfter) {
			fatal(errors.New("local runtime certificate differs from durable credential state"))
		}
	} else if credentialErr != sql.ErrNoRows {
		fatal(credentialErr)
	}
	client := &agent.ControlClient{Node: n, TLS: &tls.Config{MinVersion: tls.VersionTLS13, MaxVersion: tls.VersionTLS13, Certificates: []tls.Certificate{certificate}, RootCAs: roots, ServerName: *hubName}}
	fmt.Printf("vortex-node control reconnecting node_id=%s boot_id=%s\n", n.NodeID, n.BootID)
	runErr := make(chan error, 1)
	go func() { runErr <- client.Run(ctx, *hubAddr) }()
	if runtimeEngine, ok := engine.(*agent.RuntimeEngine); ok {
		select {
		case <-ctx.Done():
		case err := <-runErr:
			if ctx.Err() == nil && err != nil {
				fatal(err)
			}
		case <-runtimeEngine.Done():
			if err := runtimeEngine.Err(); err != nil {
				fatal(err)
			}
		}
	} else {
		select {
		case <-ctx.Done():
		case err := <-runErr:
			if ctx.Err() == nil && err != nil {
				fatal(err)
			}
		}
	}
}

func fatal(err error) { fmt.Fprintln(os.Stderr, err); os.Exit(1) }
