package agent

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"vortex/agent/store"
	controlv1 "vortex/contracts/protobuf/v1"
	"vortex/hub"
)

func TestDemandCompletesOnlyFromBoundTLSIssuer(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	open := func(role string) *store.Store {
		s, e := store.Open(filepath.Join(t.TempDir(), role+".db"), role)
		if e != nil {
			t.Fatal(e)
		}
		t.Cleanup(func() { s.Close() })
		return s
	}
	as, hs := open("agent"), open("hub")
	n, err := New(controlTestNodeID, as, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err = n.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer n.Stop(context.Background())
	now := time.Now().UTC()
	d := store.HubDelegation{ID: "550e8400-e29b-41d4-a716-446655440100", HubID: "550e8400-e29b-41d4-a716-446655440101", SourceID: "550e8400-e29b-41d4-a716-446655440102", RuntimeSourceID: "550e8400-e29b-41d4-a716-446655440103", ShareSourceID: "550e8400-e29b-41d4-a716-446655440104", NodeID: n.NodeID, BootID: n.BootID, BillingSnapshotID: "550e8400-e29b-41d4-a716-446655440107", RuleID: "rule", RuleRevision: 1, PathGeneration: 1, FencingFloor: 1, SignerEpoch: 1, RawBytes: 16 << 20, RateBPS: 1000000, Connections: 4, ValidFrom: now, ExpiresAt: now.Add(time.Hour), HardDeadline: now.Add(time.Hour), BackendHeartbeat: now, AutonomyDeadline: now.Add(2 * time.Hour)}
	clock := func() store.IssuerTime {
		at := time.Now().UTC()
		return store.IssuerTime{Earliest: at, Latest: at, Trusted: true}
	}
	if err = hs.InstallHubDelegation(ctx, d, clock()); err != nil {
		t.Fatal(err)
	}
	scope := store.DemandScope{HubID: d.HubID, DelegationID: d.ID, NodeID: d.NodeID, BootID: d.BootID, SourceID: d.SourceID, SnapshotID: d.BillingSnapshotID, RuleID: d.RuleID, SignerEpoch: 1, RuleRevision: 1, PathGeneration: 1}
	request, err := as.PrepareDemand(ctx, scope, 8<<20, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	issuer := hub.Issuer{Store: hs, HubID: d.HubID, Clock: clock}
	serve := func(identity string) error {
		st, ct, _ := controlTLSForHub(t, n.NodeID, identity)
		ln := startTestControlHub(t, ctx, &hub.TestServer{Store: hs, AuthorizationUpdate: func(id, boot string) (*controlv1.AuthorizationUpdate, error) {
			return issuer.Issue(ctx, d.ID, id, boot, request.ID, request.RawBytes, request.TTL)
		}}, st)
		return (&ControlClient{Node: n, TLS: ct}).Connect(ctx, ln.Addr().String())
	}
	// Valid CA and hostname verification are insufficient: a different
	// authenticated Hub must not satisfy the original pending demand.
	if err = serve("550e8400-e29b-41d4-a716-446655440199"); err == nil {
		t.Fatal("wrong TLS issuer installed response")
	}
	again, err := as.PrepareDemand(ctx, scope, 16<<20, 2*time.Minute)
	if err != nil || again != request {
		t.Fatalf("wrong issuer cleared pending: %+v %v", again, err)
	}
	if err = serve(d.HubID); err != nil {
		t.Fatal(err)
	}
	balance, err := as.AuditAuthorizationSource(ctx, n.NodeID, d.SourceID)
	if err != nil || balance.Initial != 8<<20 || balance.Remaining != 8<<20 {
		t.Fatalf("installed balance: %+v %v", balance, err)
	}
	// A repeated authenticated response does not complete a future demand.
	next, err := as.PrepareDemand(ctx, scope, 4<<20, time.Minute)
	if err != nil || next.ID == request.ID {
		t.Fatalf("response not atomically completed: %+v %v", next, err)
	}
	if err = serve(d.HubID); err != nil {
		t.Fatal(err)
	}
	again, err = as.PrepareDemand(ctx, scope, 8<<20, time.Minute)
	if err != nil || again != next {
		t.Fatalf("old response cleared new demand: %+v %v", again, err)
	}
	delegated, err := hs.AuditHubDelegation(ctx, d.ID)
	if err != nil || delegated.Issued != 8<<20 || delegated.Remaining != 8<<20 {
		t.Fatalf("response retry duplicated funding: %+v %v", delegated, err)
	}
}
