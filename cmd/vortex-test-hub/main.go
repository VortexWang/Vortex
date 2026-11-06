package main

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"
	controlv1 "vortex/contracts/protobuf/v1"

	"vortex/agent/store"
	"vortex/hub"
)

func main() {
	listen := flag.String("listen", "127.0.0.1:19090", "test Hub listen address")
	dbPath := flag.String("db", "hub.db", "Hub SQLite path")
	certPath := flag.String("tls-cert", "", "Hub certificate PEM")
	keyPath := flag.String("tls-key", "", "Hub private key PEM")
	caPath := flag.String("tls-ca", "", "trusted node CA PEM")
	hubID := flag.String("hub-id", "", "configured lifecycle Hub UUID; requires existing durable authority")
	region := flag.String("region", "default", "configured lifecycle region")
	stallAfter := flag.Bool("stall-after-readiness", false, "test-only fault: stop status after negotiation")
	stall := flag.Bool("stall-readiness", false, "test-only fault: stall until stream cancellation")
	delegationPath := flag.String("test-delegation-template", "", "test-only offline authenticated allocation fixture; never production backend authority")
	updatePath := flag.String("test-config-update", "", "test-only RuntimeConfigUpdate JSON fixture")
	reportPath := flag.String("test-report-output", "", "test-only latest observational node report; overwritten atomically")
	dropRenewal := flag.Bool("test-drop-first-renewal-reply", false, "test-only lost renewal result after durable commit")
	holdRenewal := flag.Bool("test-hold-renewal-replies", false, "test-only crash barrier: retain results without sending replies")
	recoveryRequest := flag.String("test-recovery-request", "", "test-only offline handoff JSON path; not production provisioning")
	recoveryBackend := flag.String("test-recovery-backend", "", "test-only mutual TLS ledger backend URL")
	recoveryBackendName := flag.String("test-recovery-backend-name", "", "test backend certificate identity")
	policyFixture := flag.String("test-policy-transition", "", "test-only funded future delegation and approved transition JSON")
	refreshPath := flag.String("test-authorization-refresh", "", "test-only exact approved AuthorizationUpdate JSON delivered on heartbeat")
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
	server := &hub.TestServer{Store: s, HubID: *hubID, Region: *region, StallReadiness: *stall, StallAfterReadiness: *stallAfter, AcceptUsage: *hubID != ""}
	if *refreshPath != "" {
		server.AuthorizationRefresh = func(node, boot string) (*controlv1.AuthorizationUpdate, error) {
			raw, err := os.ReadFile(*refreshPath)
			if os.IsNotExist(err) {
				return nil, nil
			}
			if err != nil || len(raw) > 2<<20 {
				return nil, fmt.Errorf("invalid refresh fixture")
			}
			var update controlv1.AuthorizationUpdate
			if err = json.Unmarshal(raw, &update); err != nil {
				return nil, err
			}
			return &update, nil
		}
	}
	if *reportPath != "" {
		server.ReportObserver = func(_ context.Context, nodeID, bootID, sessionID string, r *controlv1.NodeReport) error {
			raw, err := json.Marshal(struct {
				NodeID, BootID, SessionID string
				Report                    *controlv1.NodeReport
			}{nodeID, bootID, sessionID, r})
			if err != nil {
				return err
			}
			f, err := os.CreateTemp(filepath.Dir(*reportPath), ".node-report-*")
			if err != nil {
				return err
			}
			defer os.Remove(f.Name())
			if _, err := f.Write(raw); err != nil {
				f.Close()
				return err
			}
			if err := f.Close(); err != nil {
				return err
			}
			return os.Rename(f.Name(), *reportPath)
		}
	}
	if *updatePath != "" {
		raw, e := os.ReadFile(*updatePath)
		if e != nil {
			panic(e)
		}
		server.ConfigUpdate = new(controlv1.RuntimeConfigUpdate)
		if e = json.Unmarshal(raw, server.ConfigUpdate); e != nil {
			panic(e)
		}
	}
	if *delegationPath != "" {
		raw, e := os.ReadFile(*delegationPath)
		if e != nil {
			panic(e)
		}
		var template store.HubDelegation
		if e = json.Unmarshal(raw, &template); e != nil || template.HubID != *hubID {
			panic("invalid offline delegation fixture")
		}
		clock := func() store.IssuerTime {
			now := time.Now().UTC()
			return store.IssuerTime{Earliest: now, Latest: now, Trusted: true}
		}
		sessionDelegation := func(node, boot string) store.HubDelegation {
			d := template
			if node != d.NodeID {
				return store.HubDelegation{}
			}
			d.BootID = boot
			digest := sha256.Sum256([]byte(template.ID + "/" + boot))
			id := hex.EncodeToString(digest[:16])
			d.ID = id[:8] + "-" + id[8:12] + "-" + id[12:16] + "-" + id[16:20] + "-" + id[20:]
			return d
		}
		server.PrepareSession = func(ctx context.Context, node, boot string) error {
			return s.InstallHubDelegation(ctx, sessionDelegation(node, boot), clock())
		}
		issuer := &hub.Issuer{Store: s, HubID: *hubID, Clock: clock}
		if template.ShareReferences != nil {
			server.RenewalIssuer = issuer
			server.DropFirstRenewalReply = *dropRenewal
			server.HoldRenewalReplies = *holdRenewal
		}
		server.AuthorizationUpdate = func(node, boot string) (*controlv1.AuthorizationUpdate, error) {
			d := sessionDelegation(node, boot)
			old, err := issuer.Issue(ctx, d.ID, node, boot, d.ID, 8192, time.Minute)
			if err != nil {
				return nil, err
			}
			if *policyFixture != "" {
				return testPolicyDelivery(ctx, s, issuer, *policyFixture, node, boot, old)
			}
			return old, nil
		}
	}
	if *recoveryRequest != "" {
		if *recoveryBackend == "" || *recoveryBackendName == "" {
			panic("test recovery requires backend and exact TLS identity")
		}
		go runTestRecovery(ctx, s, *hubID, *recoveryRequest, *recoveryBackend, *recoveryBackendName, cert, pool)
	}
	if err := server.ServeGRPC(ctx, ln, config); err != nil && ctx.Err() == nil {
		panic(err)
	}
}
