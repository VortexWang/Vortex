package service_test

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"net"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"vortex/agent/store"
	"vortex/backend/db/generated"
	"vortex/backend/internal/auth"
	"vortex/backend/internal/control"
	"vortex/backend/internal/policy"
	controlv1 "vortex/contracts/protobuf/v1"
	"vortex/hub"
)

type loseRecoveryAdmission struct {
	*control.HubAdapter
	before, after atomic.Bool
}
type recoveryAdmissionStream struct {
	grpc.BidiStreamingServer[controlv1.HubMessage, controlv1.BackendMessage]
	owner *loseRecoveryAdmission
}

func (s *recoveryAdmissionStream) Send(m *controlv1.BackendMessage) error {
	if m.GetRecoveryAdmission() != nil && s.owner.before.Swap(false) {
		return errors.New("injected admission delivery loss before local FULL")
	}
	return s.BidiStreamingServer.Send(m)
}
func (s *recoveryAdmissionStream) Recv() (*controlv1.HubMessage, error) {
	m, e := s.BidiStreamingServer.Recv()
	if e == nil && m.GetStatus() != nil && s.owner.after.Swap(false) {
		return nil, errors.New("injected funded status loss after local FULL admission")
	}
	return m, e
}
func (s *loseRecoveryAdmission) Connect(stream grpc.BidiStreamingServer[controlv1.HubMessage, controlv1.BackendMessage]) error {
	return s.HubAdapter.Connect(&recoveryAdmissionStream{stream, s})
}

func TestHubRecoveryAdmissionActualMTLSExplicitProofFULLLossReconnectAndNewIssue(t *testing.T) {
	x, hs, a, r, _ := admissionFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()
	if e := hs.PersistUsageLedger(ctx, store.UsageBatch{NodeID: a.NodeID, BootID: a.BootID, BatchID: "post-prefix-tail", SnapshotID: a.SnapshotID, Payload: []byte("late unresolved usage remains pending")}); e != nil {
		t.Fatal(e)
	}
	start := time.Now()
	clock := func() store.IssuerTime {
		now := x.now.Add(time.Since(start))
		return store.IssuerTime{Earliest: now, Latest: now, Trusted: true}
	}
	x.s.Clock = func() (time.Time, time.Time, bool) { c := clock(); return c.Earliest, c.Latest, c.Trusted }
	backendCA, backendKey := portCA(t)
	hubCA, hubKey := portCA(t)
	backendRoots, hubRoots := x509.NewCertPool(), x509.NewCertPool()
	backendRoots.AddCert(backendCA)
	hubRoots.AddCert(hubCA)
	x.s.SignerCA = hubCA
	x.f.contexts[r.HubID] = portNodeContext(t, hubCA, hubKey, r.HubID)
	serverTLS := &tls.Config{Certificates: []tls.Certificate{portCertificate(t, backendCA, backendKey, shareBackendID, x509.ExtKeyUsageServerAuth)}, ClientCAs: hubRoots, ClientAuth: tls.RequireAndVerifyClientCert, MinVersion: tls.VersionTLS13, MaxVersion: tls.VersionTLS13}
	clientTLS := &tls.Config{Certificates: []tls.Certificate{portCertificate(t, hubCA, hubKey, r.HubID, x509.ExtKeyUsageClientAuth)}, RootCAs: backendRoots, ServerName: shareBackendID, MinVersion: tls.VersionTLS13, MaxVersion: tls.VersionTLS13}
	l, e := net.Listen("tcp", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	defer l.Close()
	server := grpc.NewServer(grpc.Creds(credentials.NewTLS(serverTLS)))
	defer server.Stop()
	adapter := &loseRecoveryAdmission{HubAdapter: &control.HubAdapter{HubID: r.HubID, BackendID: shareBackendID, Region: a.Region, Shares: &x.s, Observe: func(context.Context, string, string, []byte) error { return nil }}}
	controlv1.RegisterHubControlServer(server, adapter)
	go server.Serve(l)
	c := &hub.BackendClient{RecoveryPreflight: true, RecoveryAdmission: true, HubID: r.HubID, BootID: r.ToBootID, BackendID: shareBackendID, Region: a.Region, Address: l.Addr().String(), TLS: clientTLS, Store: hs, Clock: clock}
	if e = c.Run(ctx); e == nil || errors.Is(e, hub.ErrRecoveryEvidenceRetained) {
		t.Fatal("forensic retention became admission", e)
	}
	rows, e := hs.PendingLedger(ctx, 1000)
	if e != nil || len(rows) != 3 {
		t.Fatal("unapproved prefix was ACKed", rows, e)
	}
	if _, e = x.s.AdmitHubSameStore(ctx, policyScope(t, x.f.tenant, auth.PlatformAdmin, policy.Grant), r.ID, uuid.NewString()); e != nil {
		t.Fatal(e)
	}
	q := generated.New(x.f.pool)
	evidence, e := q.HubBootHasIssuerEvidence(ctx, generated.HubBootHasIssuerEvidenceParams{HubID: r.HubID, HubBootID: r.ToBootID})
	if e != nil || !evidence {
		t.Fatal("admitted boot lacks empty-state fence", evidence, e)
	}
	empty, e := store.Open(t.TempDir()+"/empty.db", "hub")
	if e != nil {
		t.Fatal(e)
	}
	defer empty.Close()
	for _, admission := range []bool{false, true} {
		missing := *c
		missing.Store, missing.RecoveryAdmission = empty, admission
		if e = missing.Run(ctx); e == nil {
			t.Fatal("empty DB adopted same-store admission", admission)
		}
	}
	wrong := *c
	wrong.TLS = clientTLS.Clone()
	wrong.TLS.Certificates = []tls.Certificate{portCertificate(t, x.s.NodeCA, x.f.caKey, r.HubID, x509.ExtKeyUsageClientAuth)}
	roleCtx, roleCancel := context.WithTimeout(ctx, 500*time.Millisecond)
	if e = wrong.Run(roleCtx); e == nil {
		t.Fatal("Node role impersonated Hub for admission")
	}
	roleCancel()
	financial := []string{"hub_same_store_admissions", "reservations", "ledger_events", "ledger_projections", "share_chains", "share_generations", "settlements"}
	before := authoritySnapshot(t, x.f.pool, x.f.tenant, financial)
	adapter.before.Store(true)
	if e = c.Run(ctx); e == nil || adapter.before.Load() {
		t.Fatal("admission delivery loss did not disconnect", e)
	}
	rows, e = hs.PendingLedger(ctx, 1000)
	if e != nil || len(rows) != 3 {
		t.Fatal("lost admission response ACKed local prefix", rows, e)
	}
	adapter.after.Store(true)
	if e = c.Run(ctx); e == nil || adapter.after.Load() {
		t.Fatal("post-FULL funded status loss did not disconnect", e)
	}
	rows, e = hs.PendingLedger(ctx, 1000)
	status, se := hs.HubControlSnapshot(ctx, r.HubID, clock().Latest)
	if e != nil || se != nil || len(rows) != 1 || status.Mode != store.AutonomyOnline || status.Confirmed != 2 || rows[0].Kind != "known" {
		t.Fatal("finite FULL admission lost or later usage ACKed", rows, status, e, se)
	}
	issue, e := hs.IssueHubAuthorization(ctx, store.IssueRequest{ID: uuid.NewString(), HubID: r.HubID, DelegationID: a.DelegationID, NodeID: a.NodeID, BootID: a.BootID, RawBytes: 10, TTL: time.Minute}, clock())
	if e != nil {
		t.Fatal("authenticated FULL admission did not permit original R", e)
	}
	run, stop := context.WithCancel(ctx)
	defer stop()
	done := make(chan error, 1)
	go func() { done <- c.Run(run) }()
	waitHub(t, ctx, func() bool {
		n, e := q.HubAllocationIssued(ctx, generated.HubAllocationIssuedParams{TenantID: x.f.tenant, AllocationID: a.AllocationID})
		rows, re := hs.PendingLedger(ctx, 1000)
		return e == nil && re == nil && n == 47 && len(rows) == 1 && rows[0].Kind == "known"
	})
	stop()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("admitted client did not stop")
	}
	if !bytes.Equal(before, authoritySnapshot(t, x.f.pool, x.f.tenant, financial)) {
		t.Fatal("FULL admission/reconnect/issue changed financial or capacity holds")
	}
	if len(issue.Payload) == 0 {
		t.Fatal("actual issue payload missing")
	}
}
