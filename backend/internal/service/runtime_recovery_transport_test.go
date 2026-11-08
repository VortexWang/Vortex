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
	"google.golang.org/protobuf/proto"
	"vortex/agent"
	"vortex/agent/store"
	"vortex/backend/internal/control"
	"vortex/backend/internal/service"
	"vortex/contracts"
	controlv1 "vortex/contracts/protobuf/v1"
	"vortex/hub"
)

func TestRuntimeRecoveryUpdateActualMTLSAgentFULLBeforeLeaseWithoutRefill(t *testing.T) {
	testRuntimeRecoveryUpdateActualMTLS(t, "open")
}
func TestRuntimeRecoveryUpdateActualMTLSAfterDirectCreditStopRetainsDebt(t *testing.T) {
	testRuntimeRecoveryUpdateActualMTLS(t, "stopped")
}
func TestRuntimeRecoveryUpdateActualMTLSAfterSealAndStopRetainsDebt(t *testing.T) {
	for _, ordering := range []string{"sealed-before-materialization", "sealed-after-materialization"} {
		t.Run(ordering, func(t *testing.T) { testRuntimeRecoveryUpdateActualMTLS(t, ordering) })
	}
}
func testRuntimeRecoveryUpdateActualMTLS(t *testing.T, ordering string) {
	stopped := ordering != "open"
	testEffectiveRuntimeFundingWithHook(t, true, func(x *shareFixture, hs, ns *store.Store, a hub.Allocation, r service.HubSameStoreRecoveryInput, node *agent.Node) {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		var frozenRecovery []byte
		if ordering == "sealed-after-materialization" {
			page, err := hs.RuntimeRecoveryUpdates(ctx, a.HubID, a.NodeID, a.BootID, "", store.IssuerTime{Earliest: x.now, Latest: x.now, Trusted: true})
			if err != nil || len(page.Updates) != 1 {
				t.Fatal("pre-seal recovery materialization", page, err)
			}
			frozenRecovery = append([]byte(nil), page.Updates[0].Payload...)
		}
		if ordering == "sealed-before-materialization" || ordering == "sealed-after-materialization" {
			if _, err := hs.SealHubDelegation(ctx, a.HubID, r.ToBootID, a.DelegationID); err != nil {
				t.Fatal("seal before first recovery delivery", err)
			}
		}
		var stoppedGrant string
		if stopped {
			pending, err := ns.PendingUsageOutbox(ctx, a.NodeID)
			if err != nil || len(pending) != 1 {
				t.Fatal(pending, err)
			}
			batch := new(controlv1.UsageBatch)
			if err = proto.Unmarshal(pending[0].Payload, batch); err != nil {
				t.Fatal(err)
			}
			stoppedGrant = batch.Deltas[0].GrantId
			if err = ns.StopCreditGrant(ctx, a.NodeID, a.BootID, a.HubID, stoppedGrant); err != nil {
				t.Fatal(err)
			}
		}
		// The helper advances its deterministic recovery clock by five seconds.
		// Regional advertisements use wall time, so wait for that instant rather
		// than accepting a future backend heartbeat or creating a new Node boot.
		if delay := time.Until(x.now); delay > 0 {
			time.Sleep(delay)
		}
		ca, key := portCA(t)
		roots := x509.NewCertPool()
		roots.AddCert(ca)
		nodeRoots := x509.NewCertPool()
		nodeRoots.AddCert(x.s.NodeCA)
		serverTLS := &tls.Config{Certificates: []tls.Certificate{portCertificate(t, ca, key, a.HubID, x509.ExtKeyUsageServerAuth)}, ClientCAs: nodeRoots, ClientAuth: tls.RequireAndVerifyClientCert, MinVersion: tls.VersionTLS13, MaxVersion: tls.VersionTLS13}
		clientTLS := &tls.Config{Certificates: []tls.Certificate{portCertificate(t, x.s.NodeCA, x.f.caKey, a.NodeID, x509.ExtKeyUsageClientAuth)}, RootCAs: roots, ServerName: a.HubID, MinVersion: tls.VersionTLS13, MaxVersion: tls.VersionTLS13}
		started, base := time.Now(), x.now
		clock := func() store.IssuerTime {
			now := base.Add(time.Since(started))
			return store.IssuerTime{Earliest: now, Latest: now, Trusted: true}
		}
		originalClock := x.s.Clock
		x.s.Clock = func() (time.Time, time.Time, bool) { n := clock(); return n.Earliest, n.Latest, n.Trusted }
		defer func() { x.now = clock().Latest; x.s.Clock = originalClock }()
		server := grpc.NewServer(grpc.Creds(credentials.NewTLS(serverTLS)))
		defer server.Stop()
		// Online usage requires a real independently authenticated PG acceptance
		// before Hub ACK. Omitting this backend would hide a reconnect behind
		// RunRegion even after the recovery envelope had already persisted.
		backendCA, backendKey := portCA(t)
		backendRoots, hubRoots := x509.NewCertPool(), x509.NewCertPool()
		backendRoots.AddCert(backendCA)
		hubRoots.AddCert(x.s.SignerCA)
		backendListener, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		defer backendListener.Close()
		backendServer := grpc.NewServer(grpc.Creds(credentials.NewTLS(&tls.Config{Certificates: []tls.Certificate{portCertificate(t, backendCA, backendKey, shareBackendID, x509.ExtKeyUsageServerAuth)}, ClientCAs: hubRoots, ClientAuth: tls.RequireAndVerifyClientCert, MinVersion: tls.VersionTLS13, MaxVersion: tls.VersionTLS13})))
		defer backendServer.Stop()
		controlv1.RegisterHubControlServer(backendServer, &control.HubAdapter{BackendID: shareBackendID, HubID: a.HubID, Region: a.Region, Shares: &x.s, Metering: &service.MeteringService{Pool: x.f.pool}, Observe: func(context.Context, string, string, []byte) error { return nil }})
		go backendServer.Serve(backendListener)
		usageBackend := hub.BackendUsageClient{Config: &hub.BackendClient{HubID: a.HubID, BootID: r.ToBootID, BackendID: shareBackendID, Region: a.Region, Address: backendListener.Addr().String(), Store: hs, Clock: clock, TLS: &tls.Config{Certificates: []tls.Certificate{portCertificate(t, x.s.SignerCA, x.f.caKey, a.HubID, x509.ExtKeyUsageClientAuth)}, RootCAs: backendRoots, ServerName: shareBackendID, MinVersion: tls.VersionTLS13, MaxVersion: tls.VersionTLS13}}}
		runtime := &hub.RuntimeServer{HubID: a.HubID, Region: a.Region, Issuer: &hub.Issuer{Store: hs, HubID: a.HubID, Clock: clock}, Usage: &hub.UsageRelay{Store: hs, Clock: clock, Backend: usageBackend}}
		controlv1.RegisterNodeControlServer(server, runtime)
		listener, e := net.Listen("tcp", "127.0.0.1:0")
		if e != nil {
			t.Fatal(e)
		}
		defer listener.Close()
		go server.Serve(listener)
		balance, e := hs.AuditHubDelegation(ctx, a.DelegationID)
		if e != nil {
			t.Fatal(e)
		}
		page, e := hs.RuntimeRecoveryUpdates(ctx, a.HubID, a.NodeID, a.BootID, "", clock())
		if e != nil || len(page.Updates) != 1 {
			t.Fatal(page, e)
		}
		if frozenRecovery != nil && string(page.Updates[0].Payload) != string(frozenRecovery) {
			t.Fatal("seal rewrote already committed recovery bytes")
		}
		for attempt := 0; attempt < 2; attempt++ {
			run, stop := context.WithCancel(ctx)
			done := make(chan error, 1)
			client := &agent.ControlClient{Node: node, TLS: clientTLS}
			go func() {
				done <- client.RunRegion(run, agent.RegionalConfig{Region: a.Region, Watchdog: 3 * time.Second, LeaseRefresh: true, Endpoints: []agent.HubEndpoint{{Region: a.Region, HubID: a.HubID, ServerName: a.HubID, Address: listener.Addr().String()}}})
			}()
			tick := time.NewTicker(10 * time.Millisecond)
			for {
				epoch, e := ns.AgentState(ctx, "authorization_signer_epoch")
				pending, pendingErr := ns.PendingUsageOutbox(ctx, a.NodeID)
				if e == nil && epoch == "8" && pendingErr == nil && len(pending) == 0 && client.CurrentHubStatus() != nil {
					break
				}
				select {
				case e := <-done:
					stop()
					tick.Stop()
					t.Fatal("actual runtime recovery stopped", e)
				case <-ctx.Done():
					stop()
					tick.Stop()
					t.Fatal(ctx.Err())
				case <-tick.C:
				}
			}
			tick.Stop()
			// Let the following bootstrap/lease pass execute. No transferred original
			// partition can be reissued after receiving the recovery update.
			select {
			case e := <-done:
				stop()
				t.Fatal("post recovery stream rejected", e)
			case <-time.After(150 * time.Millisecond):
			}
			if status := client.CurrentHubStatus(); status == nil || status.SignerEpoch != 8 || client.RegionalCounters().Attempts != 1 {
				stop()
				t.Fatal("recovery replay caused reconnect/rejection", status, client.RegionalCounters())
			}
			stop()
			select {
			case <-done:
			case <-ctx.Done():
				t.Fatal("runtime recovery did not join")
			}
		}
		// Recovery must make a live transferred family observable to the
		// scheduler without making its old share revision a new-session right.
		due, err := ns.DueRenewals(ctx, store.AuthorizationUse{NodeID: a.NodeID, BootID: a.BootID, RuleID: a.RuleID, RuleRevision: a.RuleRevision, PathGeneration: a.PathGeneration, FencingGeneration: a.FencingFloor, Earliest: clock().Earliest, Latest: clock().Latest, TrustedTime: true}, a.HubID, 30)
		if err != nil {
			t.Fatal(err)
		}
		if stopped {
			if len(due) != 0 {
				t.Fatal("stopped source produced automatic renewal", due)
			}
		} else {
			if len(due) != 1 || due[0].Request.SignerEpoch != 8 {
				t.Fatal("actual recovery lost effective renewal trigger", due)
			}
			request := due[0].Request
			request.RequestId = uuid.NewString()
			request.RequestSha256, err = contracts.RenewalDigest(request)
			if err != nil {
				t.Fatal(err)
			}
			if err = ns.PersistTriggeredRenewal(ctx, a.HubID, request, due[0].GrantID); err != nil {
				t.Fatal(err)
			}
		}
		if stopped {
			_, err := ns.CheckStoredAuthorization(ctx, store.AuthorizationUse{NodeID: a.NodeID, BootID: a.BootID, AuthorizationID: stoppedGrant, Kind: "credit_grant", RuleID: a.RuleID, RuleRevision: a.RuleRevision, PathGeneration: a.PathGeneration, BillingSnapshotID: a.SnapshotID, Earliest: clock().Earliest, Latest: clock().Latest, TrustedTime: true, ExistingSession: true})
			if !errors.Is(err, store.ErrAuthorizationDenied) {
				t.Fatal("recovery revived stopped credit", err)
			}
			b, err := ns.AuditAuthorizationBalance(ctx, a.NodeID, stoppedGrant)
			if err != nil || b.Committed != 37 {
				t.Fatal("recovery changed original known debt", b, err)
			}
		}
		after, e := hs.AuditHubDelegation(ctx, a.DelegationID)
		if e != nil || after != balance {
			t.Fatal("runtime recovery changed old credit partition", balance, after, e)
		}
		replay, e := hs.RuntimeRecoveryUpdates(ctx, a.HubID, a.NodeID, a.BootID, "", clock())
		if e != nil || len(replay.Updates) != 1 || string(replay.Updates[0].Payload) != string(page.Updates[0].Payload) {
			t.Fatal("reconnect retimed runtime recovery", replay, e)
		}
	})
}
