package service_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"github.com/google/uuid"
	"strings"
	"testing"
	"time"
	"vortex/backend/db/generated"
	"vortex/backend/internal/auth"
	"vortex/backend/internal/control"
	"vortex/backend/internal/policy"
	"vortex/backend/internal/repository"
	"vortex/backend/internal/service"
)

func TestServingRejectsDeletedMember(t *testing.T) {
	f := newPortFixture(t)
	a := f.prepare(t, 20000)
	f.commit(t, a)
	ctx := context.Background()
	if _, err := f.pool.Exec(ctx, "UPDATE nodes SET deleted_at=now() WHERE id=$1", f.nodes[0]); err != nil {
		t.Fatal(err)
	}
	members, err := (repository.PortRepository{Pool: f.pool}).Serving(ctx, f.scope, f.group, uuid.NewString())
	if err != nil {
		t.Fatal(err)
	}
	for _, member := range members {
		if member.ID == f.nodes[0] {
			t.Fatal("deleted member must never enter serving despite retained confirmed ports")
		}
	}
}

func TestServingRejectsUnhealthyMember(t *testing.T) {
	f := newPortFixture(t)
	a := f.prepare(t, 20000)
	f.commit(t, a)
	ctx := context.Background()
	if _, e := f.pool.Exec(ctx, "UPDATE nodes SET health='unhealthy' WHERE id=$1", f.nodes[0]); e != nil {
		t.Fatal(e)
	}
	members, e := (repository.PortRepository{Pool: f.pool}).Serving(ctx, f.scope, f.group, uuid.NewString())
	if e != nil || len(members) != 1 || members[0].ID == f.nodes[0] {
		t.Fatal("unhealthy member retained in DNS serving set", e, len(members))
	}
}

type servingProvider struct {
	calls     int
	fail      bool
	addresses []string
}

func (*servingProvider) VerifyZone(context.Context, []byte, string) (control.DNSZone, error) {
	return control.DNSZone{ID: strings.Repeat("a", 32), Name: "example.test"}, nil
}
func (p *servingProvider) Reconcile(_ context.Context, _ []byte, _, _ string, addresses []string, _ string) error {
	p.calls++
	p.addresses = append([]string(nil), addresses...)
	if p.fail {
		return control.ErrDNS
	}
	return nil
}
func dnsFixture(t *testing.T) (*portFixture, control.CredentialStore, string, *servingProvider) {
	t.Helper()
	f := newPortFixture(t)
	ctx := context.Background()
	if e := generated.New(f.pool).InsertTenantUser(ctx, generated.InsertTenantUserParams{ID: f.scope.ActorID(), TenantID: f.tenant, Role: "owner", State: "active"}); e != nil {
		t.Fatal(e)
	}
	p, e := auth.NewPrincipal(f.scope.ActorID(), f.tenant, auth.Owner, "active")
	if e != nil {
		t.Fatal(e)
	}
	scope, e := repository.NewTenantScope(p, f.tenant, policy.ManageAccess)
	if e != nil {
		t.Fatal(e)
	}
	crypto, e := control.NewCredentialCrypto(1, map[int64][]byte{1: bytes.Repeat([]byte{4}, 32)})
	if e != nil {
		t.Fatal(e)
	}
	provider := &servingProvider{}
	store := control.CredentialStore{Pool: f.pool, Crypto: crypto, Provider: provider}
	id := uuid.NewString()
	if _, e = store.Store(ctx, scope, id, strings.Repeat("a", 32), []byte("dedicated-test-token-only"), uuid.NewString()); e != nil {
		t.Fatal(e)
	}
	return f, store, id, provider
}
func TestDNSDurableServingRecomputeAndUnknownReplay(t *testing.T) {
	f, store, id, provider := dnsFixture(t)
	ctx := context.Background()
	a := f.prepare(t, 20000)
	f.commit(t, a)
	svc := service.ServingService{Pool: f.pool}
	key := uuid.NewString()
	op, e := svc.RecomputeDNS(ctx, f.scope, f.group, id, f.group+".example.test", key, uuid.NewString())
	if e != nil {
		t.Fatal(e)
	}
	replay, e := svc.RecomputeDNS(ctx, f.scope, f.group, id, f.group+".example.test", key, uuid.NewString())
	if e != nil || replay.ID != op.ID {
		t.Fatal("idempotent queue", e)
	}
	if _, e = svc.RecomputeDNS(ctx, f.scope, f.group, id, "other.example.test", key, uuid.NewString()); e == nil {
		t.Fatal("conflicting idempotency accepted")
	}
	var desired service.DNSDesired
	if e = json.Unmarshal(op.Payload, &desired); e != nil || len(desired.Addresses) != 1 {
		t.Fatal("deduplicated serving", e, desired)
	}
	provider.fail = true
	if e = svc.ApplyDNS(ctx, f.scope, op.ID, uuid.NewString(), store); !errors.Is(e, control.ErrDNS) {
		t.Fatal("unknown result", e)
	}
	saved, e := generated.New(f.pool).GetOperation(ctx, generated.GetOperationParams{TenantID: f.tenant, ID: op.ID})
	if e != nil || saved.State != "unknown" || !saved.OccupationHeld {
		t.Fatal("DNS failure released unknown occupation", e, saved.State)
	}
	provider.fail = false
	if e = svc.ApplyDNS(ctx, f.scope, op.ID, uuid.NewString(), store); e != nil {
		t.Fatal(e)
	}
	if e = svc.ApplyDNS(ctx, f.scope, op.ID, uuid.NewString(), store); e != nil || provider.calls != 2 {
		t.Fatal("succeeded operation repeated effects", e, provider.calls)
	}
	if _, e = f.pool.Exec(ctx, "UPDATE port_allocations SET state='retained' WHERE tenant_id=$1 AND id=$2", f.tenant, a.ID); e != nil {
		t.Fatal(e)
	}
	removal, e := svc.RecomputeDNS(ctx, f.scope, f.group, id, f.group+".example.test", uuid.NewString(), uuid.NewString())
	if e != nil {
		t.Fatal(e)
	}
	if e = svc.ApplyDNS(ctx, f.scope, removal.ID, uuid.NewString(), store); e != nil || len(provider.addresses) != 0 {
		t.Fatal("retained ports entered DNS", e)
	}
	var held int
	if e = f.pool.QueryRow(ctx, "SELECT count(*) FROM port_reservations WHERE tenant_id=$1 AND state<>'released'", f.tenant).Scan(&held); e != nil || held != 4 {
		t.Fatal("DNS cleanup released retained sockets", e, held)
	}
}
func TestDNSAuditRollbackAndCurrentRole(t *testing.T) {
	f, _, id, _ := dnsFixture(t)
	ctx := context.Background()
	svc := service.ServingService{Pool: f.pool}
	request := uuid.NewString()
	failAudit(t, f.pool, request)
	tables := []string{"operations", "tasks", "outbox", "audit_events"}
	before := authoritySnapshot(t, f.pool, f.tenant, tables)
	if _, e := svc.RecomputeDNS(ctx, f.scope, f.group, id, f.group+".example.test", uuid.NewString(), request); e == nil {
		t.Fatal("audit failure committed DNS task")
	}
	if !bytes.Equal(before, authoritySnapshot(t, f.pool, f.tenant, tables)) {
		t.Fatal("DNS task/outbox/audit partial commit")
	}
	if _, e := f.pool.Exec(ctx, "UPDATE users SET state='suspended' WHERE tenant_id=$1 AND id=$2", f.tenant, f.scope.ActorID()); e != nil {
		t.Fatal(e)
	}
	if _, e := svc.RecomputeDNS(ctx, f.scope, f.group, id, f.group+".example.test", uuid.NewString(), uuid.NewString()); e == nil {
		t.Fatal("stale principal queued DNS")
	}
}

func TestDNSEntryGroupDomainBinding(t *testing.T) {
	f, _, id, _ := dnsFixture(t)
	ctx := context.Background()
	svc := service.ServingService{Pool: f.pool}
	if _, e := svc.RecomputeDNS(ctx, f.scope, f.group, id, "vortex.outside.test", uuid.NewString(), uuid.NewString()); e == nil {
		t.Fatal("domain outside enrolled zone accepted")
	}
	if _, e := svc.RecomputeDNS(ctx, f.scope, f.group, id, f.group+".example.test", uuid.NewString(), uuid.NewString()); e != nil {
		t.Fatal(e)
	}
	binding, e := generated.New(f.pool).GetEntryGroupDomain(ctx, generated.GetEntryGroupDomainParams{TenantID: f.tenant, GroupID: f.group})
	if e != nil || binding.Name != f.group+".example.test" || binding.CredentialID != id {
		t.Fatal("entry group binding not persisted", e)
	}
	if _, e = svc.RecomputeDNS(ctx, f.scope, f.group, id, "other.example.test", uuid.NewString(), uuid.NewString()); e == nil {
		t.Fatal("entry group accepted second domain")
	}
	if _, e = f.pool.Exec(ctx, "UPDATE entry_group_domains SET name='other.example.test' WHERE group_id=$1", f.group); e == nil {
		t.Fatal("entry group binding mutable outside audited service")
	}
}
func TestServingCurrentSnapshotAndUnknown(t *testing.T) {
	f := newPortFixture(t)
	ctx := context.Background()
	a := f.prepare(t, 20000)
	f.commit(t, a)
	repo := repository.PortRepository{Pool: f.pool}
	if _, e := f.pool.Exec(ctx, "UPDATE port_reservations SET state='unknown' WHERE allocation_id=$1 AND node_id=$2", a.ID, f.nodes[0]); e != nil {
		t.Fatal(e)
	}
	rows, e := repo.Serving(ctx, f.scope, f.group, uuid.NewString())
	if e != nil || len(rows) != 1 {
		t.Fatal("unknown member entered serving", e, len(rows))
	}
	if _, e = f.pool.Exec(ctx, "UPDATE nodes SET program_version='changed',version=version+1 WHERE id=$1", f.nodes[1]); e != nil {
		t.Fatal(e)
	}
	rows, e = repo.Serving(ctx, f.scope, f.group, uuid.NewString())
	if e != nil || len(rows) != 0 {
		t.Fatal("stale member snapshot entered serving", e, len(rows))
	}
}

func TestServingAllCommittedRulesAndFailedNewRule(t *testing.T) {
	f := newPortFixture(t)
	ctx := context.Background()
	first := f.prepare(t, 20000)
	f.commit(t, first)
	repo := repository.PortRepository{Pool: f.pool}
	// A failed/uncommitted new rule must not remove members serving an old rule.
	pending := f.prepare(t, 20001)
	rows, e := repo.Serving(ctx, f.scope, f.group, uuid.NewString())
	if e != nil || len(rows) != 2 {
		t.Fatal("uncommitted new rule removed existing serving member", e, len(rows))
	}
	f.commit(t, pending)
	if _, e = f.pool.Exec(ctx, "UPDATE port_reservations SET state='unknown' WHERE allocation_id=$1 AND node_id=$2", pending.ID, f.nodes[0]); e != nil {
		t.Fatal(e)
	}
	rows, e = repo.Serving(ctx, f.scope, f.group, uuid.NewString())
	if e != nil {
		t.Fatal(e)
	}
	for _, row := range rows {
		if row.ID == f.nodes[0] {
			t.Fatal("one confirmed rule hid another committed rule's unknown ports")
		}
	}
	if len(rows) != 1 {
		t.Fatal("unaffected member removed", len(rows))
	}
}

func TestServingPlatformAggregatesAllCommittedTenantRules(t *testing.T) {
	f := newPortFixtureKind(t, true)
	ctx := context.Background()
	first := f.prepare(t, 20000)
	f.commit(t, first)
	other := *f
	other.tenant = seedTenant(t, f.pool)
	other.scope = policyScope(t, other.tenant, auth.Owner, policy.Mutate)
	admin := policyScope(t, other.tenant, auth.PlatformAdmin, policy.Grant)
	if e := f.nodeService.SetGrant(ctx, admin, f.group, 0, "active", time.Now().Add(-time.Hour), time.Now().Add(time.Hour), uuid.NewString()); e != nil {
		t.Fatal(e)
	}
	second := other.prepare(t, 20001)
	other.commit(t, second)
	if _, e := f.pool.Exec(ctx, "UPDATE port_reservations SET state='unknown' WHERE tenant_id=$1 AND allocation_id=$2 AND node_id=$3", other.tenant, second.ID, f.nodes[0]); e != nil {
		t.Fatal(e)
	}
	repo := repository.PortRepository{Pool: f.pool}
	rows, e := repo.Serving(ctx, policyScope(t, f.tenant, auth.PlatformAdmin, policy.Read), f.group, uuid.NewString())
	if e != nil || len(rows) != 1 || rows[0].ID == f.nodes[0] {
		t.Fatal("platform DNS ignored another tenant's unknown committed rule", e, len(rows))
	}
	rows, e = repo.Serving(ctx, f.scope, f.group, uuid.NewString())
	if e != nil || len(rows) != 2 {
		t.Fatal("tenant scoped view changed to foreign tenant data", e, len(rows))
	}
	if e = generated.New(f.pool).InsertTenantUser(ctx, generated.InsertTenantUserParams{ID: f.scope.ActorID(), TenantID: f.tenant, Role: "owner", State: "active"}); e != nil {
		t.Fatal(e)
	}
	if _, e = (service.ServingService{Pool: f.pool}).RecomputeDNS(ctx, f.scope, f.group, uuid.NewString(), "tenant.example.test", uuid.NewString(), uuid.NewString()); !errors.Is(e, service.ErrConflict) {
		t.Fatal("granted tenant changed platform group DNS")
	}
}
