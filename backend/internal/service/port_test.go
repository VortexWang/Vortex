package service_test

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"errors"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/peer"
	"math/big"
	"net"
	"sync"
	"testing"
	"time"
	"vortex/backend/db/generated"
	"vortex/backend/internal/auth"
	"vortex/backend/internal/policy"
	"vortex/backend/internal/repository"
	"vortex/backend/internal/service"
	"vortex/backend/internal/testutil"
)

func TestPortReservationProofSchema(t *testing.T) {
	pool := testutil.Postgres(t)
	for _, table := range []string{"port_allocations", "port_reservations", "release_proofs"} {
		var exists bool
		if e := pool.QueryRow(context.Background(), "SELECT to_regclass($1) IS NOT NULL", table).Scan(&exists); e != nil {
			t.Fatal(e)
		}
		if !exists {
			t.Errorf("port allocation authority table %s missing", table)
		}
	}
}

func portCA(t *testing.T) (*x509.Certificate, ed25519.PrivateKey) {
	t.Helper()
	pub, key, e := ed25519.GenerateKey(rand.Reader)
	if e != nil {
		t.Fatal(e)
	}
	template := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "test node trust root"}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour), IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign}
	der, e := x509.CreateCertificate(rand.Reader, template, template, pub, key)
	if e != nil {
		t.Fatal(e)
	}
	ca, e := x509.ParseCertificate(der)
	if e != nil {
		t.Fatal(e)
	}
	return ca, key
}
func portCertificate(t *testing.T, ca *x509.Certificate, key ed25519.PrivateKey, name string, usage x509.ExtKeyUsage) tls.Certificate {
	t.Helper()
	pub, priv, e := ed25519.GenerateKey(rand.Reader)
	if e != nil {
		t.Fatal(e)
	}
	serial, e := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 120))
	if e != nil {
		t.Fatal(e)
	}
	leaf := &x509.Certificate{SerialNumber: serial, Subject: pkix.Name{CommonName: name}, DNSNames: []string{name}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour), KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{usage}}
	der, e := x509.CreateCertificate(rand.Reader, leaf, ca, pub, key)
	if e != nil {
		t.Fatal(e)
	}
	return tls.Certificate{Certificate: [][]byte{der, ca.Raw}, PrivateKey: priv}
}

// Capture the server side of a real mutual TLS handshake. No forged context
// or self-attested verified chain is used for the successful proof path.
func portNodeContext(t *testing.T, ca *x509.Certificate, key ed25519.PrivateKey, node string) context.Context {
	t.Helper()
	roots := x509.NewCertPool()
	roots.AddCert(ca)
	serverName := uuid.NewString()
	serverCert := portCertificate(t, ca, key, serverName, x509.ExtKeyUsageServerAuth)
	clientCert := portCertificate(t, ca, key, node, x509.ExtKeyUsageClientAuth)
	listener, e := net.Listen("tcp", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	defer listener.Close()
	states := make(chan tls.ConnectionState, 1)
	failures := make(chan error, 1)
	go func() {
		raw, e := listener.Accept()
		if e != nil {
			failures <- e
			return
		}
		defer raw.Close()
		_ = raw.SetDeadline(time.Now().Add(5 * time.Second))
		conn := tls.Server(raw, &tls.Config{Certificates: []tls.Certificate{serverCert}, ClientCAs: roots, ClientAuth: tls.RequireAndVerifyClientCert, MinVersion: tls.VersionTLS13, MaxVersion: tls.VersionTLS13})
		if e = conn.Handshake(); e != nil {
			failures <- e
			return
		}
		states <- conn.ConnectionState()
	}()
	client, e := tls.Dial("tcp", listener.Addr().String(), &tls.Config{RootCAs: roots, ServerName: serverName, Certificates: []tls.Certificate{clientCert}, MinVersion: tls.VersionTLS13, MaxVersion: tls.VersionTLS13})
	if e != nil {
		t.Fatal(e)
	}
	defer client.Close()
	select {
	case state := <-states:
		return peer.NewContext(context.Background(), &peer.Peer{AuthInfo: credentials.TLSInfo{State: state}})
	case e := <-failures:
		t.Fatal(e)
	case <-time.After(5 * time.Second):
		t.Fatal("TLS handshake timeout")
	}
	return nil
}

type portFixture struct {
	caKey         ed25519.PrivateKey
	pool          *pgxpool.Pool
	tenant, group string
	scope         repository.TenantScope
	nodes         []string
	contexts      map[string]context.Context
	ports         service.PortService
	nodeService   service.NodeService
}

func newPortFixture(t *testing.T) *portFixture {
	return newPortFixtureKind(t, false)
}
func newPortFixtureKind(t *testing.T, platform bool) *portFixture {
	t.Helper()
	pool := testutil.Postgres(t)
	tenant := seedTenant(t, pool)
	scope := policyScope(t, tenant, auth.Owner, policy.Mutate)
	ca, key := portCA(t)
	f := &portFixture{caKey: key, pool: pool, tenant: tenant, scope: scope, contexts: map[string]context.Context{}, ports: service.PortService{Pool: pool, NodeCA: ca}, nodeService: service.NodeService{Pool: pool}}
	creator := scope
	if platform {
		creator = policyScope(t, tenant, auth.PlatformAdmin, policy.Mutate)
	}
	for i := 0; i < 2; i++ {
		in := nodeInput()
		if platform {
			in.OwnerKind = "platform"
		}
		n := mustNode(t, f.nodeService, creator, in)
		ctx := portNodeContext(t, ca, key, n)
		f.contexts[n] = ctx
		f.nodes = append(f.nodes, n)
		if e := f.ports.RegisterNodeSession(ctx, creator, n, uuid.NewString(), 1, uuid.NewString()); e != nil {
			t.Fatal(e)
		}
		if e := f.nodeService.Transition(context.Background(), creator, n, "online", 2, 0, uuid.NewString()); e != nil {
			t.Fatal(e)
		}
	}
	in := groupInput()
	if platform {
		in.OwnerKind = "platform"
		in.BillingMultiplierMilli = 1000
	}
	f.group = mustGroup(t, f.nodeService, creator, in)
	members := []service.GroupMember{}
	for _, n := range f.nodes {
		members = append(members, service.GroupMember{NodeID: n, Weight: 1})
	}
	if e := f.nodeService.SetMembers(context.Background(), creator, f.group, 1, members, uuid.NewString()); e != nil {
		t.Fatal(e)
	}
	if platform {
		if e := f.nodeService.SetGrant(context.Background(), policyScope(t, tenant, auth.PlatformAdmin, policy.Grant), f.group, 0, "active", time.Now().Add(-time.Hour), time.Now().Add(time.Hour), uuid.NewString()); e != nil {
			t.Fatal(e)
		}
	}
	return f
}

func TestPortPlatformCrossTenantPhysicalConflict(t *testing.T) {
	f := newPortFixtureKind(t, true)
	ctx := context.Background()
	f.prepare(t, 20000)
	other := seedTenant(t, f.pool)
	scope := policyScope(t, other, auth.Owner, policy.Mutate)
	grant := policyScope(t, other, auth.PlatformAdmin, policy.Grant)
	if e := f.nodeService.SetGrant(ctx, grant, f.group, 0, "active", time.Now().Add(-time.Hour), time.Now().Add(time.Hour), uuid.NewString()); e != nil {
		t.Fatal(e)
	}
	snap, e := f.nodeService.Snapshot(ctx, scope, f.group, uuid.NewString())
	if e != nil {
		t.Fatal(e)
	}
	req := service.PortRequest{RuleID: uuid.NewString(), GroupID: f.group, MemberSetVersion: snap.MemberSetVersion, ListenAddress: "192.0.2.1", Port: 20000, Purpose: "service", Protocols: []string{"tcp", "udp"}}
	if _, e = f.ports.PreparePortReservation(ctx, scope, req, uuid.NewString()); e == nil {
		t.Fatal("tenant key hid same physical socket conflict")
	}
	req.Port = 0
	prepared, e := f.ports.PreparePortReservation(ctx, scope, req, uuid.NewString())
	if e != nil || prepared.Port != 20001 {
		t.Fatal("cross-tenant common alternative", e, prepared.Port)
	}
}

func TestReleaseOldBootAndDisconnectRetainOccupation(t *testing.T) {
	f := newPortFixture(t)
	ctx := context.Background()
	a := f.prepare(t, 20000)
	f.commit(t, a)
	if e := f.ports.RequestRelease(ctx, f.scope, a.ID, "explicit", uuid.NewString()); e != nil {
		t.Fatal(e)
	}
	p := f.proof(t, a, f.nodes[0], "release")
	if e := f.ports.RecordReleaseProof(ctx, f.scope, p, uuid.NewString()); !errors.Is(e, service.ErrScope) {
		t.Fatal("disconnect or DNS removal became proof", e)
	}
	if e := f.ports.RegisterNodeSession(f.contexts[f.nodes[0]], f.scope, f.nodes[0], uuid.NewString(), 2, uuid.NewString()); e != nil {
		t.Fatal(e)
	}
	if e := f.ports.RecordReleaseProof(f.contexts[f.nodes[0]], f.scope, p, uuid.NewString()); !errors.Is(e, service.ErrStaleReceipt) {
		t.Fatal("old boot proof freed socket", e)
	}
	var held int
	if e := f.pool.QueryRow(ctx, "SELECT count(*) FROM port_reservations WHERE tenant_id=$1 AND state<>'released'", f.tenant).Scan(&held); e != nil || held != 4 {
		t.Fatal("old boot/disconnect released occupation", e, held)
	}
}
func (f *portFixture) request(t *testing.T, port int32) service.PortRequest {
	t.Helper()
	snap, e := f.nodeService.Snapshot(context.Background(), f.scope, f.group, uuid.NewString())
	if e != nil {
		t.Fatal(e)
	}
	return service.PortRequest{RuleID: uuid.NewString(), GroupID: f.group, MemberSetVersion: snap.MemberSetVersion, Port: port, ListenAddress: "0.0.0.0", Purpose: "service", Protocols: []string{"udp", "tcp"}}
}
func (f *portFixture) prepare(t *testing.T, port int32) generated.PortAllocation {
	t.Helper()
	a, e := f.ports.PreparePortReservation(context.Background(), f.scope, f.request(t, port), uuid.NewString())
	if e != nil {
		t.Fatal(e)
	}
	return a.PortAllocation
}
func (f *portFixture) proof(t *testing.T, a generated.PortAllocation, node, kind string) service.PortProof {
	t.Helper()
	rows, e := generated.New(f.pool).PortReservations(context.Background(), generated.PortReservationsParams{TenantID: f.tenant, AllocationID: a.ID})
	if e != nil {
		t.Fatal(e)
	}
	p := service.PortProof{AllocationID: a.ID, NodeID: node, Kind: kind, Result: "prepared", LeaseToken: uuid.NewString()}
	if kind == "release" {
		p.Result = "released"
	}
	for _, r := range rows {
		if r.NodeID == node {
			p.TaskID = r.PrepareTaskID
			p.BootID = r.BootID
			p.Generation = r.Generation
			if kind == "release" {
				p.TaskID = r.ReleaseTaskID.String
			}
			break
		}
	}
	if e = f.pool.QueryRow(context.Background(), "SELECT target_revision FROM operations WHERE tenant_id=$1 AND id=$2", f.tenant, p.TaskID).Scan(&p.Revision); e != nil {
		t.Fatal(e)
	}
	if _, e = f.pool.Exec(context.Background(), "UPDATE tasks SET state='running',lease_token=$2,lease_until=now()+interval '30 seconds',attempts=attempts+1 WHERE id=$1", p.TaskID, p.LeaseToken); e != nil {
		t.Fatal(e)
	}
	return p
}
func (f *portFixture) commit(t *testing.T, a generated.PortAllocation) {
	t.Helper()
	for _, n := range f.nodes {
		p := f.proof(t, a, n, "prepare")
		if e := f.ports.RecordPrepareProof(f.contexts[n], f.scope, p, uuid.NewString()); e != nil {
			t.Fatal(e)
		}
	}
	if e := f.ports.CommitPortReservation(context.Background(), f.scope, a.ID, uuid.NewString()); e != nil {
		t.Fatal(e)
	}
}

func TestPortAtomicFixedMembersAndServing(t *testing.T) {
	f := newPortFixture(t)
	ctx := context.Background()
	a := f.prepare(t, 20000)
	rows, e := generated.New(f.pool).PortReservations(ctx, generated.PortReservationsParams{TenantID: f.tenant, AllocationID: a.ID})
	if e != nil || len(rows) != 4 {
		t.Fatal("partial TCP+UDP set", e, len(rows))
	}
	for _, r := range rows {
		if r.Port != 20000 {
			t.Fatal("port mismatch")
		}
	}
	if e = f.ports.CommitPortReservation(ctx, f.scope, a.ID, uuid.NewString()); !errors.Is(e, service.ErrConflict) {
		t.Fatal("commit before all members prepared", e)
	}
	repo := repository.PortRepository{Pool: f.pool}
	serving, e := repo.Serving(ctx, f.scope, f.group, uuid.NewString())
	if e != nil || len(serving) != 0 {
		t.Fatal("unconfirmed serving", e)
	}
	f.commit(t, a)
	serving, e = repo.Serving(ctx, f.scope, f.group, uuid.NewString())
	if e != nil || len(serving) != 2 {
		t.Fatal("confirmed serving missing", e, len(serving))
	}
	bad := f.request(t, 20000)
	before := authoritySnapshot(t, f.pool, f.tenant, []string{"port_allocations", "port_reservations", "operations", "tasks", "outbox", "audit_events"})
	if _, e = f.ports.PreparePortReservation(ctx, f.scope, bad, uuid.NewString()); e == nil {
		t.Fatal("conflicting prepare succeeded")
	}
	if string(before) != string(authoritySnapshot(t, f.pool, f.tenant, []string{"port_allocations", "port_reservations", "operations", "tasks", "outbox", "audit_events"})) {
		t.Fatal("failed prepare damaged existing allocation")
	}
	next := f.prepare(t, 0)
	if next.Port != 20001 {
		t.Fatal("auto port did not compute common availability", next.Port)
	}
	if e = f.ports.AbortPortReservation(ctx, f.scope, next.ID, uuid.NewString()); e != nil {
		t.Fatal(e)
	}
	state, e := repo.Allocation(ctx, f.scope, a.ID, uuid.NewString())
	if e != nil || state.State != "committed" {
		t.Fatal("abort changed previous allocation", e)
	}
	if e = f.ports.CommitPortReservation(ctx, f.scope, next.ID, uuid.NewString()); !errors.Is(e, service.ErrConflict) {
		t.Fatal("aborted prepare committed", e)
	}
}

func TestPortNoCommonPortAndMemberChanges(t *testing.T) {
	f := newPortFixture(t)
	ctx := context.Background()
	a := f.prepare(t, 20000)
	f.commit(t, a)
	if _, e := f.pool.Exec(ctx, "UPDATE nodes SET port_min=20050,port_max=20060,version=version+1 WHERE id=$1", f.nodes[0]); e != nil {
		t.Fatal(e)
	}
	if _, e := f.pool.Exec(ctx, "UPDATE nodes SET port_min=20070,port_max=20080,version=version+1 WHERE id=$1", f.nodes[1]); e != nil {
		t.Fatal(e)
	}
	request := f.request(t, 0)
	if _, e := f.ports.PreparePortReservation(ctx, f.scope, request, uuid.NewString()); !errors.Is(e, service.ErrConflict) {
		t.Fatal("member silently excluded", e)
	}
	if _, e := f.pool.Exec(ctx, "UPDATE nodes SET port_min=20000,port_max=20100,version=version+1 WHERE id=ANY($1::text[])", f.nodes); e != nil {
		t.Fatal(e)
	}
	b := f.prepare(t, 20001)
	if e := f.nodeService.SetMembers(ctx, f.scope, f.group, 2, []service.GroupMember{{NodeID: f.nodes[0], Weight: 2}}, uuid.NewString()); e != nil {
		t.Fatal(e)
	}
	if e := f.ports.CommitPortReservation(ctx, f.scope, b.ID, uuid.NewString()); !errors.Is(e, service.ErrConflict) {
		t.Fatal("stale member set committed", e)
	}
	for _, id := range []string{a.ID, b.ID} {
		got, e := (repository.PortRepository{Pool: f.pool}).Allocation(ctx, f.scope, id, uuid.NewString())
		if e != nil || !got.NeedsRevalidation {
			t.Fatal("all affected rules not marked", e)
		}
	}
	var occupied int
	if e := f.pool.QueryRow(ctx, "SELECT count(*) FROM port_reservations WHERE tenant_id=$1 AND state<>'released'", f.tenant).Scan(&occupied); e != nil || occupied != 8 {
		t.Fatal("membership change freed occupation", e, occupied)
	}
}

func TestPortConcurrentWildcardConflict(t *testing.T) {
	f := newPortFixture(t)
	requests := []service.PortRequest{f.request(t, 20002), f.request(t, 20002)}
	requests[1].ListenAddress = "192.0.2.1"
	results := make(chan error, 2)
	var wg sync.WaitGroup
	for _, r := range requests {
		wg.Add(1)
		go func(r service.PortRequest) {
			defer wg.Done()
			_, e := f.ports.PreparePortReservation(context.Background(), f.scope, r, uuid.NewString())
			results <- e
		}(r)
	}
	wg.Wait()
	close(results)
	succeeded := 0
	for e := range results {
		if e == nil {
			succeeded++
		}
	}
	if succeeded != 1 {
		t.Fatal("physical wildcard reservation not serialized", succeeded)
	}
	mapped := f.request(t, 20003)
	mapped.ListenAddress = "::ffff:192.0.2.1"
	if _, e := f.ports.PreparePortReservation(context.Background(), f.scope, mapped, uuid.NewString()); e != nil {
		t.Fatal(e)
	}
	specific := f.request(t, 20003)
	specific.ListenAddress = "192.0.2.1"
	if _, e := f.ports.PreparePortReservation(context.Background(), f.scope, specific, uuid.NewString()); e == nil {
		t.Fatal("IPv4-mapped IPv6 alias bypassed conflict")
	}
}

func TestPortProofAuthenticationAndGenerationFence(t *testing.T) {
	f := newPortFixture(t)
	ctx := context.Background()
	a := f.prepare(t, 20000)
	node := f.nodes[0]
	p := f.proof(t, a, node, "prepare")
	if e := f.ports.RecordPrepareProof(ctx, f.scope, p, uuid.NewString()); !errors.Is(e, service.ErrScope) {
		t.Fatal("unauthenticated service caller minted proof", e)
	}
	if e := f.ports.RecordPrepareProof(f.contexts[f.nodes[1]], f.scope, p, uuid.NewString()); !errors.Is(e, service.ErrScope) {
		t.Fatal("wrong node certificate", e)
	}
	ca, key := portCA(t)
	wrongRoot := portNodeContext(t, ca, key, node)
	if e := f.ports.RecordPrepareProof(wrongRoot, f.scope, p, uuid.NewString()); !errors.Is(e, service.ErrScope) {
		t.Fatal("Hub/foreign CA accepted", e)
	}
	for _, field := range []string{"boot", "generation", "revision", "task", "lease"} {
		bad := p
		switch field {
		case "boot":
			bad.BootID = uuid.NewString()
		case "generation":
			bad.Generation++
		case "revision":
			bad.Revision++
		case "task":
			bad.TaskID = uuid.NewString()
		case "lease":
			bad.LeaseToken = uuid.NewString()
		}
		if e := f.ports.RecordPrepareProof(f.contexts[node], f.scope, bad, uuid.NewString()); e == nil {
			t.Fatal("mismatched proof accepted", field)
		}
	}
	request := uuid.NewString()
	failAudit(t, f.pool, request)
	before := authoritySnapshot(t, f.pool, f.tenant, []string{"release_proofs", "port_reservations", "tasks", "operations", "outbox", "audit_events"})
	if e := f.ports.RecordPrepareProof(f.contexts[node], f.scope, p, request); e == nil {
		t.Fatal("proof audit failure committed")
	}
	if string(before) != string(authoritySnapshot(t, f.pool, f.tenant, []string{"release_proofs", "port_reservations", "tasks", "operations", "outbox", "audit_events"})) {
		t.Fatal("proof and task partial commit")
	}
	if e := f.ports.RecordPrepareProof(f.contexts[node], f.scope, p, uuid.NewString()); e != nil {
		t.Fatal(e)
	}
	if e := f.ports.RecordPrepareProof(f.contexts[node], f.scope, p, uuid.NewString()); e != nil {
		t.Fatal("duplicate receipt", e)
	}
	p.Result = "unknown"
	if e := f.ports.RecordPrepareProof(f.contexts[node], f.scope, p, uuid.NewString()); !errors.Is(e, service.ErrConflict) {
		t.Fatal("conflicting replay", e)
	}
}

func TestPortRestrictionClockAndProofBackedRelease(t *testing.T) {
	f := newPortFixture(t)
	ctx := context.Background()
	a := f.prepare(t, 20000)
	f.commit(t, a)
	restricted := policyScope(t, f.tenant, auth.PlatformAdmin, policy.Restrict)
	tenants := service.TenantService{Pool: f.pool}
	first := time.Now().Add(-8 * 24 * time.Hour).UTC().Truncate(time.Microsecond)
	one := service.Restriction{Cause: "package_expired", SourceID: uuid.NewString(), SourceVersion: 1, EffectiveAt: first}
	if e := tenants.ChangeRestriction(ctx, restricted, one, uuid.NewString()); e != nil {
		t.Fatal(e)
	}
	if e := f.ports.MarkRestricted(ctx, restricted, a.ID, uuid.NewString()); e != nil {
		t.Fatal(e)
	}
	two := service.Restriction{Cause: "quota_exhausted", SourceID: uuid.NewString(), SourceVersion: 1, EffectiveAt: time.Now().Add(-time.Hour)}
	if e := tenants.ChangeRestriction(ctx, restricted, two, uuid.NewString()); e != nil {
		t.Fatal(e)
	}
	if e := f.ports.MarkRestricted(ctx, restricted, a.ID, uuid.NewString()); e != nil {
		t.Fatal(e)
	}
	repo := repository.PortRepository{Pool: f.pool}
	got, e := repo.Allocation(ctx, f.scope, a.ID, uuid.NewString())
	if e != nil || !got.FirstEffectiveAt.Time.Equal(first) || !got.RetainedUntil.Time.Equal(first.Add(7*24*time.Hour)) {
		t.Fatal("seven-day clock reset", e, got.FirstEffectiveAt.Time)
	}
	serving, e := repo.Serving(ctx, f.scope, f.group, uuid.NewString())
	if e != nil || len(serving) != 0 {
		t.Fatal("restricted node serving", e)
	}
	if e = f.ports.RequestRelease(ctx, f.scope, a.ID, "retention", uuid.NewString()); e != nil {
		t.Fatal(e)
	}
	p := f.proof(t, a, f.nodes[0], "release")
	p.Result = "unknown"
	if e = f.ports.RecordReleaseProof(f.contexts[f.nodes[0]], f.scope, p, uuid.NewString()); e != nil {
		t.Fatal(e)
	}
	var held int
	if e = f.pool.QueryRow(ctx, "SELECT count(*) FROM port_reservations WHERE tenant_id=$1 AND state<>'released'", f.tenant).Scan(&held); e != nil || held != 4 {
		t.Fatal("unknown freed occupation", e, held)
	}
	p.Result = "released"
	if e = f.ports.RecordReleaseProof(f.contexts[f.nodes[0]], f.scope, p, uuid.NewString()); e != nil {
		t.Fatal(e)
	}
	got, e = repo.Allocation(ctx, f.scope, a.ID, uuid.NewString())
	if e != nil || got.State != "releasing" {
		t.Fatal("partial release freed group", e)
	}
	p = f.proof(t, a, f.nodes[1], "release")
	if e = f.ports.RecordReleaseProof(f.contexts[f.nodes[1]], f.scope, p, uuid.NewString()); e != nil {
		t.Fatal(e)
	}
	got, e = repo.Allocation(ctx, f.scope, a.ID, uuid.NewString())
	if e != nil || got.State != "released" {
		t.Fatal("release not converged", e)
	}
	for _, r := range []service.Restriction{one, two} {
		r.ClearedAt = time.Now()
		if e = tenants.ChangeRestriction(ctx, restricted, r, uuid.NewString()); e != nil {
			t.Fatal(e)
		}
	}
	restored := f.request(t, 20000)
	restored.RuleID = a.RuleID
	b, e := f.ports.PreparePortReservation(ctx, f.scope, restored, uuid.NewString())
	if e != nil || b.ID == a.ID || !b.EntryAddressMayChange {
		t.Fatal("released rule did not reallocate", e)
	}
}

func TestPortRetentionClearReapplyAndManualHold(t *testing.T) {
	f := newPortFixture(t)
	ctx := context.Background()
	a := f.prepare(t, 20000)
	f.commit(t, a)
	scope := policyScope(t, f.tenant, auth.PlatformAdmin, policy.Restrict)
	tenants := service.TenantService{Pool: f.pool}
	r := service.Restriction{Cause: "account_suspended", SourceID: uuid.NewString(), SourceVersion: 1, EffectiveAt: time.Now().Add(-time.Hour)}
	if e := tenants.ChangeRestriction(ctx, scope, r, uuid.NewString()); e != nil {
		t.Fatal(e)
	}
	if e := f.ports.MarkRestricted(ctx, scope, a.ID, uuid.NewString()); e != nil {
		t.Fatal(e)
	}
	if e := f.ports.RequestRelease(ctx, f.scope, a.ID, "retention", uuid.NewString()); !errors.Is(e, service.ErrConflict) {
		t.Fatal("early seven-day release", e)
	}
	r.ClearedAt = time.Now()
	if e := tenants.ChangeRestriction(ctx, scope, r, uuid.NewString()); e != nil {
		t.Fatal(e)
	}
	if e := f.ports.ResumeRetainedPort(ctx, f.scope, a.ID, uuid.NewString()); e != nil {
		t.Fatal(e)
	}
	f.commit(t, a)
	r.SourceVersion++
	r.EffectiveAt = time.Now().Add(-time.Minute)
	r.ClearedAt = time.Time{}
	if e := tenants.ChangeRestriction(ctx, scope, r, uuid.NewString()); e != nil {
		t.Fatal(e)
	}
	if e := f.ports.MarkRestricted(ctx, scope, a.ID, uuid.NewString()); e != nil {
		t.Fatal(e)
	}
	got, e := (repository.PortRepository{Pool: f.pool}).Allocation(ctx, f.scope, a.ID, uuid.NewString())
	if e != nil || !got.FirstEffectiveAt.Time.Equal(r.EffectiveAt.UTC().Truncate(time.Microsecond)) {
		t.Fatal("reapply did not start a new clock", e)
	}
	r.ClearedAt = time.Now()
	if e = tenants.ChangeRestriction(ctx, scope, r, uuid.NewString()); e != nil {
		t.Fatal(e)
	}
	if e = f.ports.ResumeRetainedPort(ctx, f.scope, a.ID, uuid.NewString()); e != nil {
		t.Fatal(e)
	}
	f.commit(t, a)
	manual := service.Restriction{Cause: "manual", SourceID: uuid.NewString(), SourceVersion: 1, EffectiveAt: time.Now().Add(-8 * 24 * time.Hour)}
	if e = tenants.ChangeRestriction(ctx, scope, manual, uuid.NewString()); e != nil {
		t.Fatal(e)
	}
	if e = f.ports.MarkRestricted(ctx, scope, a.ID, uuid.NewString()); e != nil {
		t.Fatal(e)
	}
	got, e = (repository.PortRepository{Pool: f.pool}).Allocation(ctx, f.scope, a.ID, uuid.NewString())
	if e != nil || got.RetainedUntil.Valid {
		t.Fatal("manual stop starts timed release", e)
	}
}

func TestPortAuditRollbackAndForeignAllocation(t *testing.T) {
	f := newPortFixture(t)
	request := uuid.NewString()
	failAudit(t, f.pool, request)
	tables := []string{"port_allocations", "port_reservations", "operations", "tasks", "outbox", "audit_events"}
	before := authoritySnapshot(t, f.pool, f.tenant, tables)
	in := f.request(t, 20000)
	before = authoritySnapshot(t, f.pool, f.tenant, tables)
	if _, e := f.ports.PreparePortReservation(context.Background(), f.scope, in, request); e == nil {
		t.Fatal("prepare audit failure committed")
	}
	if string(before) != string(authoritySnapshot(t, f.pool, f.tenant, tables)) {
		t.Fatal("failed prepare left partial audit/task/outbox")
	}
	a := f.prepare(t, 20000)
	other := seedTenant(t, f.pool)
	foreign := policyScope(t, other, auth.Owner, policy.Mutate)
	if e := f.ports.RequestRelease(context.Background(), foreign, a.ID, "explicit", uuid.NewString()); e == nil {
		t.Fatal("cross-tenant release")
	}
}

func TestPortRestrictionAuditRollbackSameTransaction(t *testing.T) {
	f := newPortFixture(t)
	ctx := context.Background()
	a := f.prepare(t, 20000)
	f.commit(t, a)
	request := uuid.NewString()
	failAudit(t, f.pool, request)
	tables := []string{"restriction_events", "current_restrictions", "port_allocations", "port_reservations", "operations", "tasks", "outbox", "audit_events"}
	before := authoritySnapshot(t, f.pool, f.tenant, tables)
	scope := policyScope(t, f.tenant, auth.PlatformAdmin, policy.Restrict)
	r := service.Restriction{Cause: "quota_exhausted", SourceID: uuid.NewString(), SourceVersion: 1, EffectiveAt: time.Now().Add(-time.Minute)}
	tenants := service.TenantService{Pool: f.pool}
	if e := tenants.ChangeRestriction(ctx, scope, r, request); e == nil {
		t.Fatal("restriction audit fault committed")
	}
	if string(before) != string(authoritySnapshot(t, f.pool, f.tenant, tables)) {
		t.Fatal("restriction, ports and stop tasks did not roll back together")
	}
	if e := tenants.ChangeRestriction(ctx, scope, r, uuid.NewString()); e != nil {
		t.Fatal(e)
	}
	got, e := (repository.PortRepository{Pool: f.pool}).Allocation(ctx, f.scope, a.ID, uuid.NewString())
	if e != nil || got.State != "retained" {
		t.Fatal("restriction event did not automatically retain port", e)
	}
	r.ClearedAt = time.Now()
	if e = tenants.ChangeRestriction(ctx, scope, r, uuid.NewString()); e != nil {
		t.Fatal(e)
	}
	got, e = (repository.PortRepository{Pool: f.pool}).Allocation(ctx, f.scope, a.ID, uuid.NewString())
	if e != nil || got.FirstEffectiveAt.Valid || got.State != "retained" {
		t.Fatal("clear reset clock or execution incorrectly", e)
	}
	if e = f.ports.ResumeRetainedPort(ctx, f.scope, a.ID, uuid.NewString()); e != nil {
		t.Fatal(e)
	}
	serving, e := (repository.PortRepository{Pool: f.pool}).Serving(ctx, f.scope, f.group, uuid.NewString())
	if e != nil || len(serving) != 0 {
		t.Fatal("resume reused pre-stop config receipt", e)
	}
	f.commit(t, a)
}
