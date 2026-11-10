package main

import (
	"context"
	"crypto/ecdsa"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"os"
	"os/signal"
	"runtime"
	"sync"
	"syscall"
	"time"

	"github.com/google/uuid"
	"vortex/agent"
	"vortex/agent/identity"
	"vortex/agent/store"
	controlv1 "vortex/contracts/protobuf/v1"
)

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	if err := runCommand(ctx, os.Args[1:], os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func runNode(parent context.Context, args []string, output, diagnostics io.Writer) error {
	flags := flag.NewFlagSet("run", flag.ContinueOnError)
	flags.SetOutput(diagnostics)

	nodeID := flags.String("node-id", "node-1", "node identity")
	dbPath := flags.String("db", "agent.db", "Agent SQLite path")
	region := flags.String("region", "default", "configured control region")
	controlConfig := flags.String("control-config", "", "approved runtime envelope containing lifecycle region/endpoints")
	sourceBackendID := flags.String("source-backend-id", "", "explicit direct backend UUID allowed to provision finite source roots")
	backendAddr := flags.String("backend", "", "direct backend NodeControl address, alongside regional Hub control")
	backendCA := flags.String("backend-ca", "", "backend-role server CA PEM for direct NodeControl")
	runtimeHubCA := flags.String("hub-ca", "", "Hub-role server CA PEM; defaults to tls-ca for legacy deployments")
	hubID := flags.String("hub-id", "", "configured Hub UUID for single --hub compatibility")
	dialTimeout := flags.Duration("control-dial-timeout", 5*time.Second, "TLS and lifecycle readiness timeout")
	watchdog := flags.Duration("control-watchdog", 15*time.Second, "current Hub status timeout")
	failoverBudget := flags.Duration("control-failover-budget", 60*time.Second, "bounded candidate sweep budget")
	hubAddr := flags.String("hub", "", "protocol-test Hub address")
	enrollmentAddr := flags.String("enrollment-hub", "", "server-authenticated one-time enrollment endpoint")
	certPath := flags.String("tls-cert", "", "node certificate PEM (legacy import path)")
	keyPath := flags.String("tls-key", "", "node private key PEM (legacy import path)")
	caPath := flags.String("tls-ca", "", "trusted Hub CA PEM")
	hubName := flags.String("hub-name", "", "Hub certificate DNS identity")
	engineConfig := flags.String("engine-config", "", "Engine runtime JSON configuration")
	credentialDir := flags.String("credential-dir", "credentials", "directory for locally generated node credentials")
	renewBefore := flags.Duration("renew-before", 24*time.Hour, "renew runtime certificate before expiry")
	credentialOverlap := flags.Duration("credential-overlap", 5*time.Minute, "old/new credential overlap after validated renewal")
	renewRetry := flags.Duration("renew-retry", time.Minute, "retry delay after a failed certificate renewal")
	adminSocket := flags.String("admin-socket", "", "private local management socket (default: private per-database Unix path)")
	drainTimeout := flags.Duration("drain-timeout", 30*time.Second, "bounded graceful drain before forced shutdown")
	fundingBlockBytes := flags.Uint64("experimental-funding-block-bytes", 0, "experimental durable funding block size")
	demandConfig := flags.String("credit-demand-config", "", "explicit migrated experimental demand configuration")
	diagnosticsDir := flags.String("diagnostics-dir", "", "fresh local diagnostic profile directory")
	if err := flags.Parse(args); err != nil {
		return err
	}
	stopDiagnostics, err := startDiagnostics(*diagnosticsDir)
	if err != nil {
		return err
	}
	defer stopDiagnostics()
	if *demandConfig != "" && (*hubAddr == "" || *hubName == "" || *controlConfig != "" || *backendAddr != "" || *backendCA != "" || *sourceBackendID != "") {
		return errors.New("credit-demand-config requires explicit legacy fixture hub and hub-name")
	}
	if flags.NArg() != 0 {
		return errors.New("unexpected positional arguments")
	}
	if *backendAddr != "" || *backendCA != "" || *sourceBackendID != "" {
		id, e := uuid.Parse(*sourceBackendID)
		if e != nil || id.String() != *sourceBackendID || *backendAddr == "" || *backendCA == "" || (*hubAddr == "" && *controlConfig == "") {
			return errors.New("direct backend requires backend, backend-ca, canonical source-backend-id and regional Hub configuration")
		}
		if _, _, e = net.SplitHostPort(*backendAddr); e != nil {
			return fmt.Errorf("invalid backend address: %w", e)
		}
	}
	if *drainTimeout <= 0 || *drainTimeout > 5*time.Minute {
		return errors.New("drain-timeout must be between zero and five minutes")
	}
	if *adminSocket == "" {
		var err error
		*adminSocket, err = agent.DefaultAdminSocket(*dbPath)
		if err != nil {
			return err
		}
	}
	unlock, err := agent.LockProcess(*dbPath)
	if err != nil {
		return err
	}
	defer unlock()
	ctx, cancel := context.WithCancel(context.WithoutCancel(parent))
	defer cancel()

	s, err := store.Open(*dbPath, "agent")
	if err != nil {
		return err
	}
	defer s.Close()
	var engine agent.Engine
	if *engineConfig != "" {
		engine, err = agent.NewManagedRuntimeEngine(*engineConfig, *nodeID)
		if err != nil {
			return err
		}
	}
	n, err := agent.New(*nodeID, s, engine)
	if err != nil {
		return err
	}
	if *fundingBlockBytes != 0 {
		if err := n.EnableExperimentalFundingBlocks(*fundingBlockBytes); err != nil {
			return err
		}
	}
	var adaptiveDemands []agent.AdaptiveDemandConfig
	var demandInterval time.Duration
	if *demandConfig != "" {
		adaptiveDemands, demandInterval, err = loadDemandConfig(*demandConfig, n.NodeID, n.BootID, *hubName)
		if err != nil {
			return err
		}
	}
	executable, err := os.Executable()
	if err != nil {
		return err
	}
	if runtime.GOOS == "linux" {
		executable = "/proc/self/exe"
	}
	binary, err := os.ReadFile(executable)
	if err != nil {
		return err
	}
	buildDigest := sha256.Sum256(binary)
	n.SetBuildSHA256(hex.EncodeToString(buildDigest[:]))

	var regional agent.RegionalConfig
	if *controlConfig != "" {
		if *hubAddr != "" {
			return errors.New("use either control-config or hub")
		}
		payload, e := os.ReadFile(*controlConfig)
		if e != nil {
			return e
		}
		regional, e = agent.RegionalConfigFromRuntime(payload, n.NodeID)
		if e != nil {
			return e
		}
		explicitRegion := false
		flags.Visit(func(f *flag.Flag) {
			if f.Name == "region" {
				explicitRegion = true
			}
		})
		if explicitRegion && *region != regional.Region {
			return errors.New("region differs from configured lifecycle region")
		}
	} else if *hubAddr != "" {
		if *hubID == "" {
			*hubID = *hubName
		}
		regional = agent.RegionalConfig{Region: *region, Endpoints: []agent.HubEndpoint{{Region: *region, Address: *hubAddr, HubID: *hubID, ServerName: *hubName}}, DialTimeout: *dialTimeout, Watchdog: *watchdog, FailoverBudget: *failoverBudget}
	}
	if *hubAddr == "" && *controlConfig == "" {

		if err := n.Start(ctx); err != nil {
			return err
		}
		defer stopNode(n)
		adminStop, err := n.ServeAdmin(ctx, *adminSocket)
		if err != nil {
			return err
		}
		defer adminStop()
		fmt.Fprintf(output, "vortex-node node_id=%s boot_id=%s\n", n.NodeID, n.BootID)
		if runtimeEngine, ok := engine.(*agent.RuntimeEngine); ok {
			select {
			case <-parent.Done():
			case <-runtimeEngine.Done():
				if err := runtimeEngine.Err(); err != nil {
					return err
				}
			}
		} else {
			<-parent.Done()
		}
		return shutdownNode(n, *drainTimeout)
	}
	if *hubName == "" {
		return errors.New("hub-name is required for a Hub connection")
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
		return fmt.Errorf("read Hub trust certificate: %w", err)
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(caPEM) {
		return errors.New("invalid Hub CA")
	}
	caBlock, _ := pem.Decode(caPEM)
	if caBlock == nil || caBlock.Type != "CERTIFICATE" {
		return errors.New("invalid Hub CA PEM")
	}
	versions, err := s.CredentialVersionRecords(ctx, n.NodeID, time.Now())
	if err != nil {
		return err
	}
	revocations, err := agent.LoadCredentialRevocations(ctx, s, n.TrustedTime)
	if err != nil {
		return err
	}
	var credential identity.Credential
	var credentialManager *agent.CredentialManager
	if len(versions) != 0 {
		credentialManager, err = agent.LoadCredentialManager(ctx, s, n.NodeID, *credentialDir, *hubName, roots, revocations.IsRevoked, time.Now())
		if err != nil {
			return err
		}
		credential = credentialManager.Snapshot().Active
		if err := identity.SaveActiveFiles(*credentialDir, credential, caBlock.Bytes); err != nil {
			return fmt.Errorf("repair active credential aliases: %w", err)
		}
	} else {
		token := os.Getenv("VORTEX_ENROLLMENT_TOKEN")
		if token == "" || bootstrapCA == "" || *enrollmentAddr == "" {
			legacy, loadErr := tls.LoadX509KeyPair(*certPath, *keyPath)
			if loadErr != nil {
				return fmt.Errorf("node credentials are missing; set VORTEX_ENROLLMENT_TOKEN, VORTEX_HUB_CA and --enrollment-hub, or provide a valid legacy TLS certificate/key pair: %w", loadErr)
			}
			leaf, parseErr := x509.ParseCertificate(legacy.Certificate[0])
			if parseErr != nil {
				return parseErr
			}
			if leaf.Subject.CommonName != n.NodeID || len(leaf.DNSNames) != 1 || leaf.DNSNames[0] != n.NodeID {
				return errors.New("legacy node certificate SAN does not match node-id")
			}
			if _, verifyErr := leaf.Verify(x509.VerifyOptions{Roots: roots, CurrentTime: time.Now(), KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}}); verifyErr != nil {
				return fmt.Errorf("verify legacy node certificate: %w", verifyErr)
			}
			privateKey, ok := legacy.PrivateKey.(*ecdsa.PrivateKey)
			if !ok {
				return errors.New("legacy runtime credential private key type is unsupported")
			}
			credential, err = identity.CredentialFromTLS(n.NodeID, legacy, leaf, privateKey)
			if err != nil {
				return err
			}
			if err := identity.SaveFiles(*credentialDir, credential, caBlock.Bytes); err != nil {
				return err
			}
			if err := agent.PersistCredential(ctx, s, credential); err != nil {
				return err
			}
			fmt.Fprintf(output, "vortex-node imported node_id=%s expires=%s\n", n.NodeID, credential.Leaf.NotAfter.UTC().Format(time.RFC3339))
		} else {
			credential, err = agent.Register(ctx, n.NodeID, token, *enrollmentAddr, *credentialDir, roots, *hubName, s)
			if err != nil {
				return err
			}
			if err := os.Unsetenv("VORTEX_ENROLLMENT_TOKEN"); err != nil {
				return err
			}
		}
		credentialManager, err = agent.NewCredentialManager(credential, roots, *hubName, revocations.IsRevoked)
		if err != nil {
			return err
		}
		fmt.Fprintf(output, "vortex-node enrolled node_id=%s expires=%s\n", n.NodeID, credential.Leaf.NotAfter.UTC().Format(time.RFC3339))
	}
	if credential.NodeID != n.NodeID {
		return errors.New("persisted node credential identity does not match node-id")
	}
	leaf := credential.Leaf
	if *renewBefore <= 0 || *credentialOverlap <= 0 || *renewRetry <= 0 {
		return errors.New("renew-before, credential-overlap and renew-retry must be positive")
	}
	if time.Until(leaf.NotAfter) <= *renewBefore {
		if *enrollmentAddr == "" {
			return errors.New("runtime credential is inside renewal window but enrollment-hub is not configured")
		}
		if err := credentialManager.Renew(ctx, *enrollmentAddr, *credentialDir, s, *credentialOverlap); err != nil {
			return err
		}
		fmt.Fprintf(output, "vortex-node credential renewed expires=%s\n", credentialManager.Snapshot().Active.Leaf.NotAfter.UTC().Format(time.RFC3339))
	}
	credential = credentialManager.Snapshot().Active
	leaf = credential.Leaf
	if *enrollmentAddr == "" {
		return errors.New("enrollment-hub is required for automatic runtime credential renewal")
	}
	storedCertificate, storedExpiry, credentialErr := s.Credential(ctx, n.NodeID)
	if credentialErr != nil {
		return fmt.Errorf("durable runtime credential is unavailable or inconsistent: %w", credentialErr)
	}
	if string(storedCertificate) != string(leaf.Raw) || !storedExpiry.Equal(leaf.NotAfter) {
		return errors.New("durable runtime credential is inconsistent with the recovered credential")
	}
	var runtimeApply *agent.RuntimeApplier
	if _, ok := engine.(*agent.RuntimeEngine); ok {
		runtimeApply, err = n.RuntimeApplier()
		if err != nil {
			return err
		}
	}
	if runtimeEngine, ok := engine.(*agent.RuntimeEngine); ok {
		if err := runtimeEngine.SetIdentityProvider(credentialManager, n.BootID); err != nil {
			return err
		}
	}
	if err := n.Start(ctx); err != nil {
		return err
	}
	defer stopNode(n)
	adminStop, err := n.ServeAdmin(ctx, *adminSocket)
	if err != nil {
		return err
	}
	defer adminStop()
	reconciler := &agent.Reconciler{Store: s, Apply: agent.EngineApplier{}}
	if runtimeApply != nil {
		reconciler.Apply = runtimeApply
	}

	hubTLS := credentialManager.TLSConfig
	if *runtimeHubCA != "" {
		hubTLS, err = roleTLSProvider(credentialManager, *runtimeHubCA, *hubName)
		if err != nil {
			return err
		}
	}
	client := &agent.ControlClient{Node: n, TLSProvider: hubTLS, CredentialRevocations: revocations, Reconciler: reconciler, EnableCreditDemand: *demandConfig != "", AdaptiveDemands: adaptiveDemands, DemandInterval: demandInterval}
	var backendClient *agent.ControlClient
	if *backendAddr != "" {
		regional.LeaseRefresh = true
		provider, e := roleTLSProvider(credentialManager, *backendCA, *sourceBackendID)
		if e != nil {
			return e
		}
		backendClient = &agent.ControlClient{SourceBackendID: *sourceBackendID, Node: n, TLSProvider: provider, CredentialRevocations: revocations, Reconciler: reconciler, ControlOnly: true}
		// Two clients own independent session locks. Only the direct backend may
		// reconcile configuration; Hub failover never cancels that channel.
		client.Reconciler = nil
	}
	n.SetHubModeObserver(func() string {
		status := client.CurrentHubStatus()
		if status == nil {
			return "unknown"
		}
		return status.Mode
	})
	scheduler, err := n.SchedulerForRegion(regional)
	if err != nil {
		return err
	}
	fmt.Fprintf(output, "vortex-node control reconnecting node_id=%s boot_id=%s\n", n.NodeID, n.BootID)
	var workers sync.WaitGroup
	workers.Add(2)
	defer func() { cancel(); workers.Wait() }()
	runErr := make(chan error, 3)
	go func() {
		defer workers.Done()
		if *demandConfig != "" {
			runErr <- client.Run(ctx, *hubAddr)
		} else {
			runErr <- client.RunRegion(ctx, regional)
		}
	}()
	if backendClient != nil {
		workers.Add(1)
		go func() { defer workers.Done(); runErr <- backendClient.Run(ctx, *backendAddr) }()
	}
	if scheduler != nil {
		workers.Add(1)
		go func() {
			defer workers.Done()
			runErr <- scheduler.Run(ctx, func(ctx context.Context, r *controlv1.NodeReport) error {
				if err := n.ObserveReport(ctx, r); err != nil {
					return err
				}
				return client.EnqueueNodeReport(ctx, r)
			})
		}()
	}
	renewErr := make(chan error, 1)
	go func() {
		defer workers.Done()
		renewErr <- credentialManager.RunRenewal(ctx, *enrollmentAddr, *credentialDir, s, *renewBefore, *credentialOverlap, *renewRetry)
	}()
	if runtimeEngine, ok := engine.(*agent.RuntimeEngine); ok {
		select {
		case <-parent.Done():
		case err := <-runErr:
			if ctx.Err() == nil && err != nil {
				return err
			}
		case err := <-renewErr:
			if ctx.Err() == nil && err != nil {
				return err
			}
		case <-runtimeEngine.Done():
			if err := runtimeEngine.Err(); err != nil {
				return err
			}
		}
	} else {
		select {
		case <-parent.Done():
		case err := <-runErr:
			if ctx.Err() == nil && err != nil {
				return err
			}
		case err := <-renewErr:
			if ctx.Err() == nil && err != nil {
				return err
			}
		}
	}
	return shutdownNode(n, *drainTimeout)
}

// Share the rotating Node credential, while keeping server-role roots separate.
// The existing validity/revocation verifier remains attached to each snapshot.
func roleTLSProvider(manager *agent.CredentialManager, caPath, serverName string) (func() (*tls.Config, error), error) {
	raw, e := os.ReadFile(caPath)
	if e != nil {
		return nil, e
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(raw) {
		return nil, errors.New("invalid control role CA")
	}
	return func() (*tls.Config, error) {
		cfg, e := manager.TLSConfig()
		if e != nil {
			return nil, e
		}
		cfg.RootCAs, cfg.ClientCAs, cfg.ServerName = roots.Clone(), nil, serverName
		return cfg, nil
	}, nil
}

func shutdownNode(n *agent.Node, timeout time.Duration) error {
	drainCtx, finish := context.WithTimeout(context.Background(), timeout)
	defer finish()
	if _, ok := n.Engine.(agent.OperationalEngine); ok {
		if err := n.Drain(drainCtx); err != nil {
			return err
		}
	}
	stopCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return n.Stop(stopCtx)
}

func stopNode(n *agent.Node) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = n.Stop(ctx)
}
