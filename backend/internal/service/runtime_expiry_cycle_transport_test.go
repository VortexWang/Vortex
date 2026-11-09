package service_test

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"net"
	"testing"
	"time"

	"github.com/google/uuid"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"vortex/agent"
	"vortex/agent/store"
	"vortex/backend/db/generated"
	"vortex/backend/internal/control"
	"vortex/backend/internal/service"
	"vortex/backend/internal/worker"
	controlv1 "vortex/contracts/protobuf/v1"
	"vortex/hub"
)

// The short original child TTL exercises the production expiry path without
// jumping over heartbeat recovery boundaries. Bootstrap's real300s TTL and
// real60s lease refresh have their separate tests. No boot/epoch is reopened.
func TestRuntimeCreditExpiryActualMTLSClosureSettlementRefillAndAgentReady(t *testing.T) {
	x, in, hs := fundedHubFixtureMultiplier(t, 1200)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	wait := func(check func() bool) {
		t.Helper()
		tick := time.NewTicker(25 * time.Millisecond)
		defer tick.Stop()
		for !check() {
			select {
			case <-ctx.Done():
				t.Fatal("expiry cycle did not converge", ctx.Err())
			case <-tick.C:
			}
		}
	}
	a, err := x.s.PrepareHubAllocation(ctx, x.f.scope, in, uuid.NewString())
	if err != nil {
		t.Fatal(err)
	}
	installAllocationFixture(t, hs, a, x.now)
	issued, err := hs.IssueHubAuthorization(ctx, store.IssueRequest{ID: uuid.NewString(), HubID: in.HubID, DelegationID: a.DelegationID, NodeID: in.NodeID, BootID: in.BootID, RawBytes: a.RawBytes, TTL: 2 * time.Second}, store.IssuerTime{Earliest: x.now.Add(time.Second), Latest: x.now.Add(time.Second), Trusted: true})
	if err != nil {
		t.Fatal(err)
	}
	rows, err := hs.PendingLedger(ctx, 100)
	if err != nil || len(rows) != 1 {
		t.Fatal(rows, err)
	}
	if err = x.s.RecordHubIssue(x.f.contexts[in.HubID], in.HubID, in.HubBootID, rows[0].Sequence, issued.Payload, uuid.NewString()); err != nil {
		t.Fatal(err)
	}
	facts, err := agent.InspectAuthorizationUpdate(issued.Payload, in.NodeID, in.BootID, a.PathGeneration)
	if err != nil {
		t.Fatal(err)
	}
	var grant string
	for _, r := range facts.Records {
		if r.Kind == "credit_grant" {
			grant = r.AuthorizationID
		}
	}
	st, err := store.Open(t.TempDir()+"/node.db", "agent")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	node, err := agent.New(in.NodeID, st, nil)
	if err != nil {
		t.Fatal(err)
	}
	node.BootID = in.BootID
	// Seed the already-confirmed bootstrap CAS commands into this Node store,
	// so successor readiness also checks both actual node aggregate caps.
	for _, kind := range []string{"rate", "connections"} {
		h, e := generated.New(x.f.pool).ShareHandoffByGeneration(ctx, generated.ShareHandoffByGenerationParams{TenantID: x.f.tenant, NodeID: in.NodeID, Kind: kind, Generation: 1})
		if e != nil {
			t.Fatal(e)
		}
		if _, e = st.ApplyShareHandoff(ctx, in.NodeID, in.BootID, shareBackendID, "cas", h.Command, store.IssuerTime{Earliest: x.now.Add(time.Second), Latest: x.now.Add(time.Second), Trusted: true}); e != nil {
			t.Fatal(e)
		}
	}
	f := &creditCloseFixture{x: x, in: in, a: a, hs: hs, st: st, node: node, issued: issued, grant: grant}
	batch := f.commitLocalUsage(t, 37)
	started, base := time.Now(), time.Now().UTC()
	clock := func() store.IssuerTime {
		now := base.Add(time.Since(started))
		return store.IssuerTime{Earliest: now, Latest: now, Trusted: true}
	}
	x.s.Clock = func() (time.Time, time.Time, bool) { now := clock(); return now.Earliest, now.Latest, now.Trusted }
	backendCA, backendKey := portCA(t)
	hubCA, hubKey := portCA(t)
	backendRoots, hubRoots, nodeRoots := x509.NewCertPool(), x509.NewCertPool(), x509.NewCertPool()
	backendRoots.AddCert(backendCA)
	hubRoots.AddCert(hubCA)
	nodeRoots.AddCert(x.s.NodeCA)
	x.s.SignerCA = hubCA
	tlsServer := func(cert tls.Certificate, roots *x509.CertPool) *tls.Config {
		return &tls.Config{Certificates: []tls.Certificate{cert}, ClientCAs: roots, ClientAuth: tls.RequireAndVerifyClientCert, MinVersion: tls.VersionTLS13, MaxVersion: tls.VersionTLS13}
	}
	tlsClient := func(cert tls.Certificate, roots *x509.CertPool, name string) *tls.Config {
		return &tls.Config{Certificates: []tls.Certificate{cert}, RootCAs: roots, ServerName: name, MinVersion: tls.VersionTLS13, MaxVersion: tls.VersionTLS13}
	}
	serve := func(config *tls.Config, register func(*grpc.Server)) string {
		t.Helper()
		l, e := net.Listen("tcp", "127.0.0.1:0")
		if e != nil {
			t.Fatal(e)
		}
		s := grpc.NewServer(grpc.Creds(credentials.NewTLS(config)))
		register(s)
		t.Cleanup(func() { s.Stop(); l.Close() })
		go s.Serve(l)
		return l.Addr().String()
	}
	backendCert := portCertificate(t, backendCA, backendKey, shareBackendID, x509.ExtKeyUsageServerAuth)
	hubAddress := serve(tlsServer(backendCert, hubRoots), func(s *grpc.Server) {
		controlv1.RegisterHubControlServer(s, &control.HubAdapter{BackendID: shareBackendID, HubID: in.HubID, Region: in.Region, Shares: &x.s, Metering: &service.MeteringService{Pool: x.f.pool}, Observe: func(context.Context, string, string, []byte) error { return nil }})
	})
	ready := make(chan struct{}, 4)
	direct := &control.Adapter{Shares: &x.s, Observe: func(context.Context, string, string, []byte) error { ready <- struct{}{}; return nil }}
	nodeAddress := serve(tlsServer(backendCert, nodeRoots), func(s *grpc.Server) { controlv1.RegisterNodeControlServer(s, direct) })

	nodeCert := portCertificate(t, x.s.NodeCA, x.f.caKey, in.NodeID, x509.ExtKeyUsageClientAuth)
	directTLS := tlsClient(nodeCert, backendRoots, shareBackendID)
	regionalTLS := tlsClient(nodeCert, hubRoots, in.HubID)
	hubTLS := tlsClient(portCertificate(t, hubCA, hubKey, in.HubID, x509.ExtKeyUsageClientAuth), backendRoots, shareBackendID)
	backendClient := &hub.BackendClient{HubID: in.HubID, BootID: in.HubBootID, BackendID: shareBackendID, Region: in.Region, Address: hubAddress, TLS: hubTLS, Store: hs, Clock: clock}
	regionalAddress := serve(tlsServer(portCertificate(t, hubCA, hubKey, in.HubID, x509.ExtKeyUsageServerAuth), nodeRoots), func(s *grpc.Server) {
		controlv1.RegisterNodeControlServer(s, &hub.RuntimeServer{HubID: in.HubID, Region: in.Region, Issuer: &hub.Issuer{Store: hs, HubID: in.HubID, Clock: clock}, Usage: &hub.UsageRelay{Store: hs, Clock: clock, Backend: hub.BackendUsageClient{Config: backendClient}}})
	})
	run, stop := context.WithCancel(ctx)
	defer stop()
	var joined []<-chan error
	launch := func(fn func() error) {
		done := make(chan error, 1)
		joined = append(joined, done)
		go func() {
			e := fn()
			if run.Err() == nil {
				t.Log("cycle transport ended", e)
			}
			done <- e
		}()
	}
	defer func() {
		stop()
		for _, done := range joined {
			select {
			case <-done:
			case <-ctx.Done():
				t.Error("runtime cycle transport did not join")
			}
		}
	}()
	launch(func() error {
		return backendClient.Run(run)
	})
	launch(func() error {
		return (&agent.ControlClient{Node: node, TLS: directTLS, SourceBackendID: shareBackendID, ControlOnly: true}).Connect(run, nodeAddress)
	})
	select {
	case <-ready:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	q := generated.New(x.f.pool)
	wait(func() bool {
		var n int
		return x.f.pool.QueryRow(ctx, `SELECT count(*) FROM hub_delegation_seals WHERE tenant_id=$1 AND allocation_id=$2`, x.f.tenant, a.AllocationID).Scan(&n) == nil && n == 1
	})
	if !clock().Latest.Before(a.ExpiresAt) {
		t.Fatal("test did not seal before parent expiry")
	}
	closeWorker := worker.CreditCloseWorker{Service: x.s, BackendID: shareBackendID, Adapter: direct}
	if _, err = closeWorker.Once(ctx); !errors.Is(err, service.ErrShareUnknown) {
		t.Fatal("missing known inventory released credit", err)
	}
	if _, err = (worker.RuntimeFundingWorker{Service: x.s, Region: in.Region}).Once(ctx); err != nil && !errors.Is(err, service.ErrShareUnknown) {
		t.Fatal(err)
	}
	var count int
	if err = x.f.pool.QueryRow(ctx, `SELECT count(*) FROM hub_funded_allocations WHERE tenant_id=$1 AND node_id=$2`, x.f.tenant, in.NodeID).Scan(&count); err != nil || count != 1 {
		t.Fatal("expiry alone refilled", count, err)
	}
	if _, err = (service.MeteringService{Pool: x.f.pool}).IngestWireUsage(ctx, in.NodeID, in.BootID, 1, batch); err != nil {
		t.Fatal(err)
	}
	if _, err = closeWorker.Once(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err = (worker.HubCreditSettlementWorker{Service: x.s}).Once(ctx); err != nil {
		t.Fatal(err)
	}
	projection, err := q.GetSourceProjection(ctx, generated.GetSourceProjectionParams{TenantID: x.f.tenant, SourceID: in.SourceID})
	if err != nil || projection.F != 0 || projection.Charged != 44 || projection.RemainderMilli != 400 {
		t.Fatal("wrong original parent settlement", projection, err)
	}
	if _, err = (worker.RuntimeFundingWorker{Service: x.s, Region: in.Region}).Once(ctx); err != nil && !errors.Is(err, service.ErrShareUnknown) {
		t.Fatal(err)
	}
	history, err := q.RuntimeFundingHistory(ctx, generated.RuntimeFundingHistoryParams{TenantID: x.f.tenant, NodeID: in.NodeID, BootID: in.BootID, SnapshotID: a.SnapshotID, PathGeneration: "1", FencingFloor: "3"})
	if err != nil || len(history) != 2 {
		t.Fatal(history, err)
	}
	var successor hub.Allocation
	for _, h := range history {
		if h.AllocationID != a.AllocationID {
			row, e := q.GetHubAllocation(ctx, generated.GetHubAllocationParams{TenantID: x.f.tenant, AllocationID: h.AllocationID})
			if e != nil {
				t.Fatal(e)
			}
			successor, err = hub.DecodeAllocation(row.Payload)
			if err != nil {
				t.Fatal(err)
			}
		}
	}
	if successor.SourceID == a.SourceID || successor.SourceID == "" {
		t.Fatal("successor reused financial parent")
	}
	defer func() {
		if t.Failed() {
			diagnosticCtx, cancelDiagnostic := context.WithTimeout(context.Background(), time.Second)
			defer cancelDiagnostic()
			b, e := hs.AuditHubDelegation(diagnosticCtx, successor.DelegationID)
			t.Log("successor Hub delegation", b, e)
			pending, e := x.s.PendingHubAllocations(x.f.contexts[in.HubID], in.HubID, in.HubBootID)
			t.Log("pending allocations", len(pending), e)
			snapshot, e := hs.HubControlSnapshot(diagnosticCtx, in.HubID, clock().Latest)
			t.Log("Hub snapshot", snapshot, e)
			ledger, e := hs.PendingLedger(diagnosticCtx, 100)
			t.Log("pending journal", len(ledger), e)
		}
	}()
	if _, err = (worker.NodeSourceWorker{Service: x.s, BackendID: shareBackendID, Adapter: direct}).Once(ctx); err != nil {
		t.Fatal(err)
	}
	launch(func() error {
		return (&agent.ControlClient{Node: node, TLS: regionalTLS}).RunRegion(run, agent.RegionalConfig{Region: in.Region, Watchdog: 3 * time.Second, LeaseRefresh: true, Endpoints: []agent.HubEndpoint{{Region: in.Region, HubID: in.HubID, ServerName: in.HubID, Address: regionalAddress}}})
	})
	var latest []byte
	wait(func() bool {
		return x.f.pool.QueryRow(ctx, `SELECT payload FROM hub_issue_receipts WHERE tenant_id=$1 AND allocation_id=$2`, x.f.tenant, successor.AllocationID).Scan(&latest) == nil
	})
	newFacts, e := agent.InspectAuthorizationUpdate(latest, in.NodeID, in.BootID, successor.PathGeneration)
	if e != nil {
		t.Fatal(e)
	}
	diagnosed := false
	wait(func() bool {
		lo, hi, trusted := node.TrustedTime()
		e := st.CheckRuntimeReady(ctx, store.AuthorizationUse{NodeID: in.NodeID, BootID: in.BootID, RuleID: successor.RuleID, RuleRevision: successor.RuleRevision, PathGeneration: successor.PathGeneration, FencingGeneration: successor.FencingFloor, BillingSnapshotID: successor.SnapshotID, Earliest: lo, Latest: hi, TrustedTime: trusted})
		if e != nil && !diagnosed {
			diagnosed = true
			t.Log("runtime readiness pending", trusted, lo, hi, e)
			for _, r := range newFacts.Records {
				_, recordErr := st.CheckStoredAuthorization(ctx, store.AuthorizationUse{NodeID: in.NodeID, BootID: in.BootID, AuthorizationID: r.AuthorizationID, Kind: r.Kind, RuleID: successor.RuleID, RuleRevision: successor.RuleRevision, PathGeneration: successor.PathGeneration, BillingSnapshotID: r.BillingSnapshotID, Earliest: lo, Latest: hi, TrustedTime: trusted})
				t.Log("record", r.Kind, recordErr)
			}
		}
		return e == nil
	})
	wait(func() bool {
		issued, e := q.HubAllocationIssued(ctx, generated.HubAllocationIssuedParams{TenantID: x.f.tenant, AllocationID: successor.AllocationID})
		return e == nil && issued == int64(successor.RawBytes)
	})
	wait(func() bool {
		pending, e := st.PendingUsageOutbox(ctx, in.NodeID)
		return e == nil && len(pending) == 0
	})

	if remaining, e := st.RemainingAuthorizationBytes(ctx, in.NodeID, grant); e != nil || remaining != a.RawBytes-37 {
		t.Fatal("old credit changed on successor", remaining, e)
	}
	t.Log("actual role mTLS: child expiry before parent, automatic seal, missing-inventory hold, authenticated child final, original-source settlement, funded successor, direct source provisioning and Agent runtime ready; short child TTL fixture")
}
