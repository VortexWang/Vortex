package main

import (
	"context"
	"crypto/ecdsa"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"vortex/agent"
	"vortex/agent/identity"
	"vortex/agent/store"
)

func main() {
	nodeID := flag.String("node-id", "node-1", "node identity")
	dbPath := flag.String("db", "agent.db", "Agent SQLite path")
	hubAddr := flag.String("hub", "", "protocol-test Hub address")
	enrollmentAddr := flag.String("enrollment-hub", "", "server-authenticated one-time enrollment endpoint")
	certPath := flag.String("tls-cert", "", "node certificate PEM (legacy import path)")
	keyPath := flag.String("tls-key", "", "node private key PEM (legacy import path)")
	caPath := flag.String("tls-ca", "", "trusted Hub CA PEM")
	hubName := flag.String("hub-name", "", "Hub certificate DNS identity")
	engineConfig := flag.String("engine-config", "", "Engine runtime JSON configuration")
	credentialDir := flag.String("credential-dir", "credentials", "directory for locally generated node credentials")
	renewBefore := flag.Duration("renew-before", 24*time.Hour, "renew runtime certificate before expiry")
	credentialOverlap := flag.Duration("credential-overlap", 5*time.Minute, "old/new credential overlap after validated renewal")
	renewRetry := flag.Duration("renew-retry", time.Minute, "retry delay after a failed certificate renewal")
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
	if *hubAddr == "" {
		if err := n.Start(ctx); err != nil {
			fatal(err)
		}
		defer stopNode(n)
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
	if *caPath == "" {
		*caPath = *credentialDir + "/trust-ca.pem"
	}
	if *certPath == "" {
		*certPath = *credentialDir + "/runtime-cert.pem"
	}
	if *keyPath == "" {
		*keyPath = *credentialDir + "/runtime-key.pem"
	}
	bootstrapCA := os.Getenv("VORTEX_HUB_CA")
	trustPath := *caPath
	if bootstrapCA != "" {
		trustPath = bootstrapCA
	}
	caPEM, err := os.ReadFile(trustPath)
	if err != nil {
		fatal(fmt.Errorf("read Hub trust certificate: %w", err))
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(caPEM) {
		fatal(errors.New("invalid Hub CA"))
	}
	caBlock, _ := pem.Decode(caPEM)
	if caBlock == nil || caBlock.Type != "CERTIFICATE" {
		fatal(errors.New("invalid Hub CA PEM"))
	}
	versions, err := s.CredentialVersionRecords(ctx, n.NodeID, time.Now())
	if err != nil {
		fatal(err)
	}
	var credential identity.Credential
	var credentialManager *agent.CredentialManager
	if len(versions) != 0 {
		credentialManager, err = agent.LoadCredentialManager(ctx, s, n.NodeID, *credentialDir, *hubName, roots, nil, time.Now())
		if err != nil {
			fatal(err)
		}
		credential = credentialManager.Snapshot().Active
		if err := identity.SaveActiveFiles(*credentialDir, credential, caBlock.Bytes); err != nil {
			fatal(fmt.Errorf("repair active credential aliases: %w", err))
		}
	} else {
		token := os.Getenv("VORTEX_ENROLLMENT_TOKEN")
		if token == "" || bootstrapCA == "" || *enrollmentAddr == "" {
			legacy, loadErr := tls.LoadX509KeyPair(*certPath, *keyPath)
			if loadErr != nil {
				fatal(fmt.Errorf("node credentials are missing; set VORTEX_ENROLLMENT_TOKEN, VORTEX_HUB_CA and --enrollment-hub, or provide a valid legacy TLS certificate/key pair: %w", loadErr))
			}
			leaf, parseErr := x509.ParseCertificate(legacy.Certificate[0])
			if parseErr != nil {
				fatal(parseErr)
			}
			if leaf.Subject.CommonName != n.NodeID || len(leaf.DNSNames) != 1 || leaf.DNSNames[0] != n.NodeID {
				fatal(errors.New("legacy node certificate SAN does not match node-id"))
			}
			if _, verifyErr := leaf.Verify(x509.VerifyOptions{Roots: roots, CurrentTime: time.Now(), KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}}); verifyErr != nil {
				fatal(fmt.Errorf("verify legacy node certificate: %w", verifyErr))
			}
			privateKey, ok := legacy.PrivateKey.(*ecdsa.PrivateKey)
			if !ok {
				fatal(errors.New("legacy runtime credential private key type is unsupported"))
			}
			credential, err = identity.CredentialFromTLS(n.NodeID, legacy, leaf, privateKey)
			if err != nil {
				fatal(err)
			}
			if err := identity.SaveFiles(*credentialDir, credential, caBlock.Bytes); err != nil {
				fatal(err)
			}
			if err := agent.PersistCredential(ctx, s, credential); err != nil {
				fatal(err)
			}
			fmt.Printf("vortex-node imported node_id=%s expires=%s\n", n.NodeID, credential.Leaf.NotAfter.UTC().Format(time.RFC3339))
		} else {
			credential, err = agent.Register(ctx, n.NodeID, token, *enrollmentAddr, *credentialDir, roots, *hubName, s)
			if err != nil {
				fatal(err)
			}
			if err := os.Unsetenv("VORTEX_ENROLLMENT_TOKEN"); err != nil {
				fatal(err)
			}
		}
		credentialManager, err = agent.NewCredentialManager(credential, roots, *hubName, nil)
		if err != nil {
			fatal(err)
		}
		fmt.Printf("vortex-node enrolled node_id=%s expires=%s\n", n.NodeID, credential.Leaf.NotAfter.UTC().Format(time.RFC3339))
	}
	if credential.NodeID != n.NodeID {
		fatal(errors.New("persisted node credential identity does not match node-id"))
	}
	leaf := credential.Leaf
	if *renewBefore <= 0 || *credentialOverlap <= 0 || *renewRetry <= 0 {
		fatal(errors.New("renew-before, credential-overlap and renew-retry must be positive"))
	}
	if time.Until(leaf.NotAfter) <= *renewBefore {
		if *enrollmentAddr == "" {
			fatal(errors.New("runtime credential is inside renewal window but enrollment-hub is not configured"))
		}
		if err := credentialManager.Renew(ctx, *enrollmentAddr, *credentialDir, s, *credentialOverlap); err != nil {
			fatal(err)
		}
		fmt.Printf("vortex-node credential renewed expires=%s\n", credentialManager.Snapshot().Active.Leaf.NotAfter.UTC().Format(time.RFC3339))
	}
	credential = credentialManager.Snapshot().Active
	leaf = credential.Leaf
	if *enrollmentAddr == "" {
		fatal(errors.New("enrollment-hub is required for automatic runtime credential renewal"))
	}
	storedCertificate, storedExpiry, credentialErr := s.Credential(ctx, n.NodeID)
	if credentialErr != nil {
		fatal(fmt.Errorf("durable runtime credential is unavailable or inconsistent: %w", credentialErr))
	}
	if string(storedCertificate) != string(leaf.Raw) || !storedExpiry.Equal(leaf.NotAfter) {
		fatal(errors.New("durable runtime credential is inconsistent with the recovered credential"))
	}
	var runtimeApply *agent.RuntimeApplier
	if _, ok := engine.(*agent.RuntimeEngine); ok {
		runtimeApply, err = n.RuntimeApplier()
		if err != nil {
			fatal(err)
		}
	}
	if err := n.Start(ctx); err != nil {
		fatal(err)
	}
	defer stopNode(n)
	reconciler := &agent.Reconciler{Store: s, Apply: agent.EngineApplier{}}
	if runtimeApply != nil {
		reconciler.Apply = runtimeApply
	}

	client := &agent.ControlClient{Node: n, TLSProvider: credentialManager.TLSConfig, Reconciler: reconciler}
	fmt.Printf("vortex-node control reconnecting node_id=%s boot_id=%s\n", n.NodeID, n.BootID)
	runErr := make(chan error, 1)
	go func() { runErr <- client.Run(ctx, *hubAddr) }()
	renewErr := make(chan error, 1)
	go func() {
		renewErr <- credentialManager.RunRenewal(ctx, *enrollmentAddr, *credentialDir, s, *renewBefore, *credentialOverlap, *renewRetry)
	}()
	if runtimeEngine, ok := engine.(*agent.RuntimeEngine); ok {
		select {
		case <-ctx.Done():
		case err := <-runErr:
			if ctx.Err() == nil && err != nil {
				fatal(err)
			}
		case err := <-renewErr:
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
		case err := <-renewErr:
			if ctx.Err() == nil && err != nil {
				fatal(err)
			}
		}
	}
}

func stopNode(n *agent.Node) {
	stopCtx, stopCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer stopCancel()
	if err := n.Stop(stopCtx); err != nil {
		fmt.Fprintln(os.Stderr, "engine shutdown:", err)
	}
}

func fatal(err error) { fmt.Fprintln(os.Stderr, err); os.Exit(1) }
