package service_test

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"github.com/google/uuid"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"net"
	"sync/atomic"
	"testing"
	"time"
	"vortex/agent/store"
	"vortex/backend/db/generated"
	"vortex/backend/internal/auth"
	"vortex/backend/internal/control"
	"vortex/backend/internal/policy"
	"vortex/backend/internal/service"
	controlv1 "vortex/contracts/protobuf/v1"
	"vortex/hub"
)

type loseRecoveryRetention struct {
	*control.HubAdapter
	stage atomic.Int32
}
type recoveryRetentionStream struct {
	grpc.BidiStreamingServer[controlv1.HubMessage, controlv1.BackendMessage]
	owner *loseRecoveryRetention
}

func (s *recoveryRetentionStream) Send(m *controlv1.BackendMessage) error {
	if r := m.GetRecoveryRetention(); r != nil {
		if r.Kind != "manifest" && s.owner.stage.CompareAndSwap(0, 1) {
			return errors.New("injected PG retained-row receipt loss")
		}
		if r.Kind == "manifest" && s.owner.stage.CompareAndSwap(1, 2) {
			return errors.New("injected PG verified-manifest receipt loss")
		}
	}
	return s.BidiStreamingServer.Send(m)
}
func (s *loseRecoveryRetention) Connect(stream grpc.BidiStreamingServer[controlv1.HubMessage, controlv1.BackendMessage]) error {
	return s.HubAdapter.Connect(&recoveryRetentionStream{stream, s})
}

func TestHubRecoveryPreflightActualMTLSLostRetentionReplayNeverACKsOrAdmits(t *testing.T) {
	x, in, hs, a, r := sameStoreFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	// More than one32-row page, with every issue independently registered in PG.
	for i := 0; i < 40; i++ {
		issued, e := hs.IssueHubAuthorization(ctx, store.IssueRequest{ID: uuid.NewString(), HubID: r.HubID, DelegationID: a.DelegationID, NodeID: a.NodeID, BootID: a.BootID, RawBytes: 1, TTL: time.Minute}, store.IssuerTime{Earliest: x.now, Latest: x.now, Trusted: true})
		if e != nil {
			t.Fatal(e)
		}
		if e = x.s.RecordHubIssue(x.f.contexts[r.HubID], r.HubID, in.HubBootID, int64(i+2), issued.Payload, uuid.NewString()); e != nil {
			t.Fatal(e)
		}
	}
	x.now = x.now.Add(time.Second)
	approval, e := x.s.PrepareHubSameStoreRecovery(ctx, policyScope(t, x.f.tenant, auth.PlatformAdmin, policy.Grant), r, uuid.NewString())
	if e != nil {
		t.Fatal(e)
	}
	held := authoritySnapshot(t, x.f.pool, x.f.tenant, []string{"reservations", "ledger_projections", "ledger_events", "hub_credit_grants", "share_chains", "share_generations", "settlements"})
	backendCA, backendKey := portCA(t)
	hubCA, hubKey := portCA(t)
	x.s.SignerCA = hubCA
	x.f.contexts[r.HubID] = portNodeContext(t, hubCA, hubKey, r.HubID)
	backendRoots, hubRoots := x509.NewCertPool(), x509.NewCertPool()
	backendRoots.AddCert(backendCA)
	hubRoots.AddCert(hubCA)
	serverTLS := &tls.Config{Certificates: []tls.Certificate{portCertificate(t, backendCA, backendKey, shareBackendID, x509.ExtKeyUsageServerAuth)}, ClientCAs: hubRoots, ClientAuth: tls.RequireAndVerifyClientCert, MinVersion: tls.VersionTLS13, MaxVersion: tls.VersionTLS13}
	clientTLS := &tls.Config{Certificates: []tls.Certificate{portCertificate(t, hubCA, hubKey, r.HubID, x509.ExtKeyUsageClientAuth)}, RootCAs: backendRoots, ServerName: shareBackendID, MinVersion: tls.VersionTLS13, MaxVersion: tls.VersionTLS13}
	adapter := &loseRecoveryRetention{HubAdapter: &control.HubAdapter{HubID: r.HubID, BackendID: shareBackendID, Region: "phase6", Shares: &x.s, Observe: func(context.Context, string, string, []byte) error { return nil }}}
	server := grpc.NewServer(grpc.Creds(credentials.NewTLS(serverTLS)))
	defer server.Stop()
	controlv1.RegisterHubControlServer(server, adapter)
	listener, e := net.Listen("tcp", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	defer listener.Close()
	go server.Serve(listener)
	c := &hub.BackendClient{HubID: r.HubID, BootID: r.ToBootID, BackendID: shareBackendID, Region: "phase6", Address: listener.Addr().String(), TLS: clientTLS, Store: hs, Clock: func() store.IssuerTime { return store.IssuerTime{Earliest: x.now, Latest: x.now, Trusted: true} }, RecoveryPreflight: true}
	// Even a Node-role certificate claiming the Hub UUID cannot cross the Hub CA.
	wrongRole := *c
	wrongRole.TLS = clientTLS.Clone()
	wrongRole.TLS.Certificates = []tls.Certificate{portCertificate(t, x.s.NodeCA, x.f.caKey, r.HubID, x509.ExtKeyUsageClientAuth)}
	roleCtx, roleCancel := context.WithTimeout(ctx, 500*time.Millisecond)
	if e = wrongRole.Run(roleCtx); e == nil {
		t.Fatal("Node role certificate impersonated Hub")
	}
	roleCancel()
	q := generated.New(x.f.pool)
	// Existing funded capability cannot consume the recovery approval.
	c.RecoveryPreflight = false
	if e = c.Run(ctx); e == nil {
		t.Fatal("legacy funded stream accepted target boot")
	}
	c.RecoveryPreflight = true
	wrong := *c
	wrong.BootID = uuid.NewString()
	if e = wrong.Run(ctx); e == nil || errors.Is(e, hub.ErrRecoveryEvidenceRetained) {
		t.Fatal("wrong boot received recovery approval", e)
	}
	empty, e := store.Open(t.TempDir()+"/empty-hub.db", "hub")
	if e != nil {
		t.Fatal(e)
	}
	defer empty.Close()
	missing := *c
	missing.Store = empty
	if e = missing.Run(ctx); e == nil || errors.Is(e, hub.ErrRecoveryEvidenceRetained) {
		t.Fatal("empty Hub claimed same-store continuity", e)
	}
	if adapter.stage.Load() != 0 {
		t.Fatal("empty Hub retained recovery evidence")
	}
	if e = c.Run(ctx); e == nil || errors.Is(e, hub.ErrRecoveryEvidenceRetained) {
		t.Fatal("lost first retention passed", e)
	}
	if adapter.stage.Load() != 1 {
		t.Fatal("first retention did not commit", adapter.stage.Load())
	}
	first, e := q.GetHubSameStoreJournal(ctx, generated.GetHubSameStoreJournalParams{RecoveryID: r.ID, Sequence: 1})
	if e != nil || first.Kind != "issue" {
		t.Fatal(first, e)
	}
	if _, e = q.GetHubSameStoreManifest(ctx, r.ID); e == nil {
		t.Fatal("partial prefix became witness")
	}
	if e = c.Run(ctx); e == nil || errors.Is(e, hub.ErrRecoveryEvidenceRetained) {
		t.Fatal("lost manifest retention passed", e)
	}
	if adapter.stage.Load() != 2 {
		t.Fatal("manifest did not commit", adapter.stage.Load())
	}
	witness, e := q.GetHubSameStoreManifest(ctx, r.ID)
	if e != nil {
		t.Fatal(e)
	}
	if e = c.Run(ctx); !errors.Is(e, hub.ErrRecoveryEvidenceRetained) {
		t.Fatal("exact prefix replay", e)
	}
	again, e := q.GetHubSameStoreManifest(ctx, r.ID)
	if e != nil || !bytes.Equal(witness.Payload, again.Payload) {
		t.Fatal("manifest replay changed", e)
	}
	snapshot, e := hs.HubControlSnapshot(ctx, r.HubID, x.now)
	if e != nil || snapshot.Mode != "recovering" || snapshot.Confirmed != 0 || snapshot.Pending != 42 {
		t.Fatal("retention became local ACK/online", snapshot, e)
	}
	if _, e = hs.IssueHubAuthorization(ctx, store.IssueRequest{ID: uuid.NewString(), HubID: a.HubID, DelegationID: a.DelegationID, NodeID: a.NodeID, BootID: a.BootID, RawBytes: 1, TTL: time.Minute}, store.IssuerTime{Earliest: x.now, Latest: x.now, Trusted: true}); !errors.Is(e, store.ErrAuthorityHandoffRequired) {
		t.Fatal("retained prefix issued fresh R or failed for another reason", e)
	}
	bound, e := q.LatestHubIncarnation(ctx, r.HubID)
	if e != nil || bound.BootID != in.HubBootID || bound.SignerEpoch != r.FromEpoch {
		t.Fatal("preflight bound successor", bound, e)
	}
	if _, e = x.s.BindHubSession(x.f.contexts[r.HubID], r.HubID, r.ToBootID, uuid.NewString()); !errors.Is(e, service.ErrShareUnknown) {
		t.Fatal("preflight admitted successor", e)
	}
	if !bytes.Equal(held, authoritySnapshot(t, x.f.pool, x.f.tenant, []string{"reservations", "ledger_projections", "ledger_events", "hub_credit_grants", "share_chains", "share_generations", "settlements"})) {
		t.Fatal("preflight changed money/share authority")
	}
	original, e := q.GetHubSameStoreRecovery(ctx, generated.GetHubSameStoreRecoveryParams{HubID: r.HubID, ToEpoch: r.ToEpoch})
	if e != nil || !bytes.Equal(approval.PayloadDigest, original.PayloadDigest) {
		t.Fatal("preflight rewrote approval", e)
	}
}
