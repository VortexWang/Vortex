package httpapi_test

import (
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"strings"
	"testing"
	"time"
	"vortex/backend/internal/auth"
	"vortex/backend/internal/policy"
	"vortex/backend/internal/repository"
	"vortex/backend/internal/service"
)

func (f *apiFixture) ownerScope(t *testing.T) repository.TenantScope {
	t.Helper()
	p, _ := auth.NewPrincipal(f.user, f.tenant, auth.Owner, "active")
	s, e := repository.NewTenantScope(p, f.tenant, policy.Mutate)
	if e != nil {
		t.Fatal(e)
	}
	return s
}
func (f *apiFixture) node(t *testing.T, kind string, s repository.TenantScope) string {
	t.Helper()
	id := uuid.NewString()
	_, e := (service.NodeService{Pool: f.pool}).CreateNode(context.Background(), s, service.NodeInput{ID: id, Name: "业务节点", Region: "test", HubID: uuid.NewString(), OwnerKind: kind, PublicAddress: "PRIVATE_INTERNAL_ADDRESS", ProgramVersion: "PRIVATE_PROGRAM", Capabilities: []string{"entry"}, PortMin: 20000, PortMax: 21000, LinkPort: 22000, ControlPort: 22001, MetricsPort: 22002, Capacity: 100}, "create-http-node")
	if e != nil {
		t.Fatal(e)
	}
	return id
}
func TestHTTPBusinessReadsOwnershipGrantExpiryAndSafeFields(t *testing.T) {
	f := newAPI(t)
	ctx := context.Background()
	s := f.ownerScope(t)
	node := f.node(t, "tenant", s)
	base := "/api/v1/tenants/" + f.tenant
	for _, path := range []string{base, base + "/nodes/" + node} {
		w := f.request(t, "GET", path, f.pair.AccessToken, "")
		if w.Code != 200 || strings.Contains(w.Body.String(), "PRIVATE_") || strings.Contains(w.Body.String(), "boot_id") {
			t.Fatal(path, w.Code, w.Body)
		}
	}
	if w := f.request(t, "GET", base+"/nodes/"+uuid.NewString(), f.pair.AccessToken, ""); w.Code != 404 {
		t.Fatal(w.Code, w.Body)
	}
	admin := uuid.NewString()
	if _, e := f.pool.Exec(ctx, `INSERT INTO platform_principals(id,role) VALUES($1,'platform_admin')`, admin); e != nil {
		t.Fatal(e)
	}
	p, _ := auth.NewPrincipal(admin, "", auth.PlatformAdmin, "active")
	platform, _ := repository.NewTenantScope(p, f.tenant, policy.Mutate)
	platformNode := f.node(t, "platform", platform)
	path := base + "/nodes/" + platformNode
	if w := f.request(t, "GET", path, f.pair.AccessToken, ""); w.Code != 404 {
		t.Fatal("ungranted platform node", w.Code, w.Body)
	}
	grant, _ := repository.NewTenantScope(p, f.tenant, policy.Grant)
	if e := (service.NodeService{Pool: f.pool}).SetGrant(ctx, grant, platformNode, 0, "active", time.Now().Add(-time.Minute), time.Now().Add(time.Hour), "grant"); e != nil {
		t.Fatal(e)
	}
	if w := f.request(t, "GET", path, f.pair.AccessToken, ""); w.Code != 200 {
		t.Fatal("active grant", w.Code, w.Body)
	}
	if e := (service.NodeService{Pool: f.pool}).SetGrant(ctx, grant, platformNode, 1, "revoked", time.Now().Add(-time.Minute), time.Now().Add(time.Hour), "revoke"); e != nil {
		t.Fatal(e)
	}
	if w := f.request(t, "GET", path, f.pair.AccessToken, ""); w.Code != 404 {
		t.Fatal("revoked grant", w.Code, w.Body)
	}
	if e := (service.NodeService{Pool: f.pool}).SetGrant(ctx, grant, platformNode, 2, "active", time.Now().Add(-2*time.Hour), time.Now().Add(-time.Hour), "expired"); e != nil {
		t.Fatal(e)
	}
	if w := f.request(t, "GET", path, f.pair.AccessToken, ""); w.Code != 404 {
		t.Fatal("expired grant", w.Code, w.Body)
	}
	other := newAPI(t)
	foreignNode := other.node(t, "tenant", other.ownerScope(t))
	if w := f.request(t, "GET", base+"/nodes/"+foreignNode, f.pair.AccessToken, ""); w.Code != 404 {
		t.Fatal("foreign node", w.Code, w.Body)
	}
	group := uuid.NewString()
	if _, e := (service.NodeService{Pool: f.pool}).CreateGroup(ctx, s, service.GroupInput{ID: group, Name: "入口组", OwnerKind: "tenant", Purpose: "entry", Scheduling: "dns", HealthIntervalSeconds: 10, HealthTimeoutSeconds: 2}, "group"); e != nil {
		t.Fatal(e)
	}
	if w := f.request(t, "GET", base+"/node-groups/"+group, f.pair.AccessToken, ""); w.Code != 200 || strings.Contains(w.Body.String(), "PRIVATE_") {
		t.Fatal(w.Code, w.Body)
	}
	if page := f.businessPage(t, base+"/node-groups", f.pair.AccessToken); len(page.Items) != 1 || page.Items[0]["id"] != group {
		t.Fatal(page)
	}
	port := uuid.NewString()
	if _, e := f.pool.Exec(ctx, `INSERT INTO port_allocations(id,tenant_id,rule_id,group_id,member_set_version,port,listen_address,protocols,purpose,state) VALUES($1,$2,$3,$4,repeat('0',64),20001,'PRIVATE_LISTEN_ADDRESS',ARRAY['tcp'],'service','unknown')`, port, f.tenant, uuid.NewString(), group); e != nil {
		t.Fatal(e)
	}
	if page := f.businessPage(t, base+"/port-allocations", f.pair.AccessToken); len(page.Items) != 1 || page.Items[0]["id"] != port || page.Items[0]["state"] != "unknown" || strings.Contains(string(mustJSON(t, page)), "PRIVATE_") {
		t.Fatal(page)
	}
}
func TestHTTPSubscriptionLedgerAndHeldLimitsUseAuthoritativeSnapshots(t *testing.T) {
	f := newAPI(t)
	ctx := context.Background()
	scope := f.ownerScope(t)
	node := f.node(t, "tenant", scope)
	admin := uuid.NewString()
	if _, e := f.pool.Exec(ctx, `INSERT INTO platform_principals(id,role) VALUES($1,'platform_admin')`, admin); e != nil {
		t.Fatal(e)
	}
	p, _ := auth.NewPrincipal(admin, "", auth.PlatformAdmin, "active")
	platform, _ := repository.NewTenantScope(p, f.tenant, policy.Mutate)
	packages := service.PackageService{Pool: f.pool}
	pkg := uuid.NewString()
	terms := service.PackageTerms{DurationDays: 30, TrafficBytes: 10000, RateLimit: 8000, ConnectionLimit: 12, RuleLimit: 2, Capabilities: []string{"tcp", "direct"}}
	if _, e := packages.PutCatalog(ctx, platform, pkg, "套餐", 0, terms, "catalog"); e != nil {
		t.Fatal(e)
	}
	grant, _ := repository.NewTenantScope(p, f.tenant, policy.Grant)
	sub, e := packages.Subscribe(ctx, grant, pkg, 1, uuid.NewString(), "subscribe")
	if e != nil {
		t.Fatal(e)
	}
	if e = (service.LedgerService{Pool: f.pool}).InitializeSource(ctx, scope, sub.SourceID, "ledger"); e != nil {
		t.Fatal(e)
	}
	for _, v := range []struct {
		kind   string
		amount int
	}{{"rate", 1000}, {"connections", 3}} {
		if _, e = f.pool.Exec(ctx, `INSERT INTO share_chains(tenant_id,node_id,kind,boot_id,held_amount) VALUES($1,$2,$3,$4,$5)`, f.tenant, node, v.kind, uuid.NewString(), v.amount); e != nil {
			t.Fatal(e)
		}
	}
	base := "/api/v1/tenants/" + f.tenant
	for _, path := range []string{base + "/subscription", base + "/packages/" + pkg, base + "/sources/" + sub.SourceID + "/ledger", base + "/limits"} {
		w := f.request(t, "GET", path, f.pair.AccessToken, "")
		if w.Code != 200 {
			t.Fatal(path, w.Code, w.Body)
		}
		var body map[string]any
		if e = json.Unmarshal(w.Body.Bytes(), &body); e != nil {
			t.Fatal(e)
		}
		if strings.HasSuffix(path, "/ledger") && body["balance_bytes"] != "10000" {
			t.Fatal("ledger precision", body)
		}
		if strings.HasSuffix(path, "/limits") && (body["rate_limit_bps"] != "8000" || body["connection_limit"] != "12" || body["reserved_rate_bps"] != "1000" || body["reserved_connections"] != "3" || body["execution_state"] != "submitted") {
			t.Fatal("held occupation or execution overclaimed", body)
		}
	}
	for collection, id := range map[string]string{"packages": pkg, "subscriptions": sub.SubscriptionID, "sources": sub.SourceID} {
		page := f.businessPage(t, base+"/"+collection, f.pair.AccessToken)
		found := false
		for _, item := range page.Items {
			if item["id"] == id {
				found = true
				if collection == "sources" && item["balance_bytes"] != "10000" {
					t.Fatal(item)
				}
			}
		}
		if !found {
			t.Fatal("missing public business list record", collection, id, page)
		}
	}
	terms.RateLimit = 99999
	if _, e = packages.PutCatalog(ctx, platform, pkg, "新套餐", 1, terms, "catalog2"); e != nil {
		t.Fatal(e)
	}
	w := f.request(t, "GET", base+"/limits", f.pair.AccessToken, "")
	var final map[string]any
	json.Unmarshal(w.Body.Bytes(), &final)
	if w.Code != 200 || final["rate_limit_bps"] != "8000" {
		t.Fatal("catalog rewrote subscription", w.Code, w.Body)
	}
	if w := f.request(t, "GET", base+"/sources/"+uuid.NewString()+"/ledger", f.pair.AccessToken, ""); w.Code != 404 {
		t.Fatal("foreign/missing ledger", w.Code, w.Body)
	}
}

func TestHTTPBusinessProjectionPlatformAuditFailureIsClosed(t *testing.T) {
	f := newAPI(t)
	ctx := context.Background()
	node := f.node(t, "tenant", f.ownerScope(t))
	actor := uuid.NewString()
	if _, e := f.pool.Exec(ctx, `INSERT INTO platform_principals(id,role) VALUES($1,'auditor')`, actor); e != nil {
		t.Fatal(e)
	}
	p, _ := auth.NewPrincipal(actor, "", auth.Auditor, "active")
	pair, e := f.tokens.Issue(ctx, p, "auditor-login")
	if e != nil {
		t.Fatal(e)
	}
	path := "/api/v1/tenants/" + f.tenant + "/nodes/" + node
	if w := f.request(t, "GET", path, pair.AccessToken, ""); w.Code != 200 {
		t.Fatal(w.Code, w.Body)
	}
	var count int
	if e := f.pool.QueryRow(ctx, `SELECT count(*) FROM audit_events WHERE actor_id=$1 AND action='node.read'`, actor).Scan(&count); e != nil || count != 1 {
		t.Fatal(count, e)
	}
	name := "resource_audit_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if _, e := f.pool.Exec(ctx, `CREATE FUNCTION `+name+`() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.actor_id='`+actor+`' AND NEW.action='node.read' THEN RAISE EXCEPTION 'PRIVATE_SQL_SECRET'; END IF; RETURN NEW; END $$; CREATE TRIGGER `+name+` BEFORE INSERT ON audit_events FOR EACH ROW EXECUTE FUNCTION `+name+`() `); e != nil {
		t.Fatal(e)
	}
	defer f.pool.Exec(ctx, `DROP TRIGGER `+name+` ON audit_events; DROP FUNCTION `+name+`() `)
	if w := f.request(t, "GET", path, pair.AccessToken, ""); w.Code != 503 || strings.Contains(w.Body.String(), "PRIVATE") {
		t.Fatal(w.Code, w.Body)
	}
}
