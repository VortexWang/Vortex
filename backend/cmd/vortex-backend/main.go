package main

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"flag"
	"fmt"
	"github.com/gin-gonic/gin"
	"net"
	"os"
	"os/signal"
	"syscall"
	"time"
	"vortex/backend/internal/auth"
	"vortex/backend/internal/httpapi"
	"vortex/backend/internal/repository"

	"github.com/jackc/pgx/v5/pgxpool"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"vortex/backend/db"
	"vortex/backend/db/generated"
	"vortex/backend/internal/control"
	"vortex/backend/internal/service"
	"vortex/backend/internal/timeauthority"
	"vortex/backend/internal/worker"
	controlv1 "vortex/contracts/protobuf/v1"
)

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	if err := run(ctx, os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
func run(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return errors.New("command required: migrate, serve, serve-http, issue-session, mutate, operation")
	}
	url := os.Getenv("PHASE6_DATABASE_URL")
	if url == "" {
		return errors.New("PHASE6_DATABASE_URL required")
	}
	if args[0] == "migrate" {
		return db.Migrate(ctx, url)
	}
	config, err := pgxpool.ParseConfig(url)
	if err != nil {
		return err
	}
	config.MaxConns = 8
	config.ConnConfig.ConnectTimeout = 3 * time.Second
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		return err
	}
	defer pool.Close()
	if err = pool.Ping(ctx); err != nil {
		return err
	}
	switch args[0] {
	case "issue-session":
		return issueSession(ctx, pool, args[1:], os.Stdout)
	case "serve-http":
		return serveHTTP(ctx, pool, args[1:])
	case "serve":
		return serve(ctx, pool, args[1:])
	case "mutate", "operation":
		flags := flag.NewFlagSet(args[0], flag.ContinueOnError)
		actor := flags.String("actor", "", "authenticated local actor UUID")
		tenant := flags.String("tenant", "", "actor tenant UUID")
		selected := flags.String("scope", "", "selected tenant UUID")
		resource := flags.String("target", "", "target UUID")
		key := flags.String("key", "", "idempotency key")
		request := flags.String("request", "", "request ID")
		payload := flags.String("payload", "", "runtime artifact path")
		revision := flags.Int64("revision", 1, "target revision")
		expected := flags.Int64("expected", 0, "expected revision")
		generation := flags.Int64("generation", 1, "fencing generation")
		id := flags.String("operation", "", "operation UUID")
		if err = flags.Parse(args[1:]); err != nil {
			return err
		}
		scope, e := service.NewTenantScope(*actor, *tenant, *selected)
		if e != nil {
			return e
		}
		s := &service.Tracer{Pool: pool}
		if args[0] == "operation" {
			op, e := s.Operation(ctx, scope, *id)
			if e != nil {
				return e
			}
			return json.NewEncoder(os.Stdout).Encode(map[string]any{"operation_id": op.ID, "state": op.State, "revision": op.TargetRevision, "occupation_held": op.OccupationHeld})
		}
		raw, e := os.ReadFile(*payload)
		if e != nil {
			return e
		}
		op, e := s.Mutate(ctx, scope, service.Mutation{ResourceID: *resource, TargetKind: "node", Revision: *revision, ExpectedRevision: *expected, Generation: *generation, IdempotencyKey: *key, RequestID: *request, Payload: raw})
		if e != nil {
			return e
		}
		return json.NewEncoder(os.Stdout).Encode(map[string]any{"operation_id": op.ID, "state": op.State, "revision": op.TargetRevision})
	default:
		return errors.New("unknown command")
	}
}
func serve(parent context.Context, pool *pgxpool.Pool, args []string) error {
	f := flag.NewFlagSet("serve", flag.ContinueOnError)
	nodeListen := f.String("node-listen", "127.0.0.1:19091", "NodeControl address")
	hubListen := f.String("hub-listen", "127.0.0.1:19092", "HubControl address")
	cert := f.String("cert", "", "backend server certificate")
	key := f.String("key", "", "backend key")
	nodeCA := f.String("node-ca", "", "node-role client root")
	hubCA := f.String("hub-ca", "", "Hub-role client root")
	backendID := f.String("backend-id", "", "backend UUID")
	hubID := f.String("hub-id", "", "configured Hub UUID")
	if err := f.Parse(args); err != nil {
		return err
	}
	ctx, cancel := context.WithCancel(parent)
	defer cancel()
	observe := func(ctx context.Context, peer, session string, raw []byte) error {
		return generated.New(pool).InsertControlObservation(ctx, generated.InsertControlObservationParams{HubID: peer, SessionID: session, Payload: raw})
	}
	adapter := &control.Adapter{Observe: observe}
	nodeTLS, err := control.ServerTLS(*cert, *key, *nodeCA)
	if err != nil {
		return err
	}
	hubTLS, err := control.ServerTLS(*cert, *key, *hubCA)
	if err != nil {
		return err
	}
	// A shared client root would collapse the role boundary.
	nodeRoot, err := os.ReadFile(*nodeCA)
	if err != nil {
		return err
	}
	hubRoot, err := os.ReadFile(*hubCA)
	if err != nil {
		return err
	}
	if string(nodeRoot) == string(hubRoot) {
		return errors.New("node and Hub trust roots must differ")
	}
	rootBlock, rest := pem.Decode(nodeRoot)
	if rootBlock == nil || rootBlock.Type != "CERTIFICATE" || len(rest) != 0 {
		return errors.New("one node CA certificate required")
	}
	root, err := x509.ParseCertificate(rootBlock.Bytes)
	if err != nil {
		return err
	}
	adapter.Ports = &service.PortService{Pool: pool, NodeCA: root}
	adapter.Metering = &service.MeteringService{Pool: pool}
	hubRootBlock, hubRootRest := pem.Decode(hubRoot)
	if hubRootBlock == nil || hubRootBlock.Type != "CERTIFICATE" || len(hubRootRest) != 0 {
		return errors.New("one Hub CA certificate required")
	}
	signerRoot, err := x509.ParseCertificate(hubRootBlock.Bytes)
	if err != nil {
		return err
	}
	authorityClock := timeauthority.New()
	adapter.Shares = &service.ShareService{Pool: pool, NodeCA: root, SignerCA: signerRoot, Clock: authorityClock.Bounds}
	nodes, err := net.Listen("tcp", *nodeListen)
	if err != nil {
		return err
	}
	defer nodes.Close()
	hubs, err := net.Listen("tcp", *hubListen)
	if err != nil {
		return err
	}
	defer hubs.Close()
	server := grpc.NewServer(grpc.Creds(credentials.NewTLS(nodeTLS)), grpc.MaxRecvMsgSize(control.MaxMessage), grpc.MaxSendMsgSize(control.MaxMessage))
	hubServer := grpc.NewServer(grpc.Creds(credentials.NewTLS(hubTLS)), grpc.MaxRecvMsgSize(control.MaxMessage), grpc.MaxSendMsgSize(control.MaxMessage))
	defer server.Stop()
	defer hubServer.Stop()
	controlv1.RegisterNodeControlServer(server, adapter)
	controlv1.RegisterHubControlServer(hubServer, &control.HubAdapter{BackendID: *backendID, HubID: *hubID, Region: "phase6", Observe: observe, Metering: adapter.Metering, Shares: adapter.Shares})
	failures := make(chan error, 16)
	go func() {
		failures <- (worker.NodeSourceWorker{Service: *adapter.Shares, BackendID: *backendID, Adapter: adapter}).Run(ctx)
	}()
	go func() {
		failures <- (worker.CreditCloseWorker{Service: *adapter.Shares, BackendID: *backendID, Adapter: adapter}).Run(ctx)
	}()
	go func() {
		failures <- (worker.NodeRecoverySourceWorker{Service: *adapter.Shares, BackendID: *backendID, Adapter: adapter}).Run(ctx)
	}()
	go func() {
		failures <- (worker.NodeRecoveryCreditFenceWorker{Service: *adapter.Shares, BackendID: *backendID, Adapter: adapter}).Run(ctx)
	}()
	go func() {
		failures <- (worker.NodeBootRecoveryWorker{Service: *adapter.Shares, BackendID: *backendID, Adapter: adapter}).Run(ctx)
	}()
	go func() {
		failures <- (worker.HubCreditSettlementWorker{Service: *adapter.Shares}).Run(ctx)
	}()
	go func() { failures <- server.Serve(nodes) }()
	go func() { failures <- hubServer.Serve(hubs) }()
	go func() { failures <- (&worker.Worker{Pool: pool, Adapter: adapter}).Run(ctx) }()
	go func() {
		failures <- fmt.Errorf("rule delivery worker: %w", (worker.RuleWorker{Pool: pool, Adapter: adapter}).Run(ctx))
	}()
	go func() {
		failures <- fmt.Errorf("rule compile worker: %w", (worker.RuleCompileWorker{Pool: pool}).Run(ctx))
	}()
	go func() {
		failures <- fmt.Errorf("port release worker: %w", (worker.PortWorker{Pool: pool, Adapter: adapter}).Run(ctx))
	}()
	go func() {
		failures <- fmt.Errorf("group revalidation worker: %w", (worker.GroupRevalidationWorker{Pool: pool}).Run(ctx))
	}()
	go func() {
		failures <- fmt.Errorf("port operation worker: %w", (worker.PortOperationWorker{Pool: pool}).Run(ctx))
	}()
	go func() { failures <- fmt.Errorf("package due worker: %w", (worker.PackageWorker{Pool: pool}).Run(ctx)) }()
	go func() {
		failures <- fmt.Errorf("usage reconciliation worker: %w", (worker.UsageReconciliationWorker{Pool: pool}).Run(ctx))
	}()
	go func() {
		failures <- fmt.Errorf("share reconciliation worker: %w", (worker.ShareReconciliationWorker{Service: *adapter.Shares, Adapter: adapter}).Run(ctx))
	}()
	go func() {
		failures <- fmt.Errorf("share scheduling worker: %w", (worker.ShareSchedulingWorker{Service: *adapter.Shares}).Run(ctx))
	}()
	go func() {
		failures <- fmt.Errorf("runtime funding worker: %w", (worker.RuntimeFundingWorker{Service: *adapter.Shares, Region: "phase6"}).Run(ctx))
	}()
	go func() {
		failures <- fmt.Errorf("share renewal closure worker: %w", (worker.ShareRenewalClosureWorker{Service: *adapter.Shares, BackendID: *backendID}).Run(ctx))
	}()
	fmt.Printf("backend ready node=%s hub=%s\n", nodes.Addr(), hubs.Addr())
	select {
	case <-parent.Done():
		return nil
	case err := <-failures:
		return err
	}
}

// Public REST is a separate TLS listener/process from role-bound gRPC control.
func serveHTTP(ctx context.Context, pool *pgxpool.Pool, args []string) error {
	f := flag.NewFlagSet("serve-http", flag.ContinueOnError)
	address := f.String("listen", "127.0.0.1:19443", "public HTTPS address")
	cert := f.String("cert", "", "public HTTPS server certificate")
	key := f.String("key", "", "public HTTPS server private key")
	cursorKey := f.String("cursor-key-file", "", "private file containing 32..128 raw cursor signing key bytes")
	if err := f.Parse(args); err != nil {
		return err
	}
	if f.NArg() != 0 {
		return errors.New("unexpected public API arguments")
	}
	info, err := os.Stat(*cursorKey)
	if err != nil {
		return errors.New("cursor signing key file required")
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 || info.Size() < 32 || info.Size() > 128 {
		return errors.New("cursor signing key must be a private regular file with 32..128 bytes")
	}
	secret, err := os.ReadFile(*cursorKey)
	if err != nil {
		return errors.New("cannot read cursor signing key")
	}
	codec, err := httpapi.NewCursorCodec(secret, time.Hour)
	if err != nil {
		return err
	}
	certificate, err := tls.LoadX509KeyPair(*cert, *key)
	if err != nil {
		return errors.New("cannot load public HTTPS certificate")
	}
	gin.SetMode(gin.ReleaseMode)
	tokens := &auth.Tokens{Store: repository.TokenRepository{Pool: pool}, AccessTTL: 5 * time.Minute, RefreshTTL: 30 * 24 * time.Hour}
	handler, err := httpapi.NewRouter(httpapi.Config{Pool: pool, Tokens: tokens, Cursors: codec})
	if err != nil {
		return err
	}
	server, err := httpapi.NewServer(handler, certificate)
	if err != nil {
		return err
	}
	listener, err := net.Listen("tcp", *address)
	if err != nil {
		return err
	}
	defer listener.Close()
	fmt.Printf("backend public API ready https=%s\n", listener.Addr())
	runtime, cancel := context.WithCancel(ctx)
	defer cancel()
	failures := make(chan error, 4)
	go func() { failures <- httpapi.Serve(runtime, listener, server) }()
	go func() { failures <- (worker.EventRetentionWorker{Pool: pool}).Run(runtime) }()
	go func() { failures <- (worker.GroupRevalidationWorker{Pool: pool}).Run(runtime) }()
	go func() { failures <- (worker.PortOperationWorker{Pool: pool}).Run(runtime) }()
	first := <-failures
	cancel()
	<-failures
	<-failures
	<-failures
	if ctx.Err() != nil {
		return nil
	}
	return first
}
