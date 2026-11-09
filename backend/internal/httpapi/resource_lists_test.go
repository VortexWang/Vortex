package httpapi_test

import (
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"
	"vortex/backend/db"
	"vortex/backend/internal/auth"
	"vortex/backend/internal/policy"
	"vortex/backend/internal/repository"
	"vortex/backend/internal/service"
	"vortex/backend/internal/testutil"
)

type businessPage struct {
	Items []map[string]any `json:"items"`
	Next  string           `json:"next_cursor"`
}

func (f *apiFixture) businessPage(t *testing.T, path, token string) businessPage {
	t.Helper()
	w := f.request(t, "GET", path, token, "")
	if w.Code != 200 {
		t.Fatalf("%s status=%d body=%s", path, w.Code, w.Body.String())
	}
	var p businessPage
	if e := json.Unmarshal(w.Body.Bytes(), &p); e != nil {
		t.Fatal(e)
	}
	if p.Items == nil {
		t.Fatal("null items")
	}
	return p
}
func TestHTTPResourceListCursorScopeOrderingAndMutableBusinessState(t *testing.T) {
	f := newAPI(t)
	ctx := context.Background()
	base := "/api/v1/tenants/" + f.tenant
	ids := []string{f.node(t, "tenant", f.ownerScope(t)), f.node(t, "tenant", f.ownerScope(t)), f.node(t, "tenant", f.ownerScope(t))}
	other := newAPI(t)
	_ = other.node(t, "tenant", other.ownerScope(t))
	first := f.businessPage(t, base+"/nodes?limit=1", f.pair.AccessToken)
	if len(first.Items) != 1 || first.Next == "" || strings.Contains(string(mustJSON(t, first)), "PRIVATE_") {
		t.Fatal(first)
	}
	seen := map[string]bool{first.Items[0]["id"].(string): true}
	if _, e := f.pool.Exec(ctx, `UPDATE nodes SET name='changed',version=version+1 WHERE id=ANY($1::text[])`, ids); e != nil {
		t.Fatal(e)
	}
	cursor := first.Next
	for cursor != "" {
		p := f.businessPage(t, base+"/nodes?limit=1&cursor="+url.QueryEscape(cursor), f.pair.AccessToken)
		for _, v := range p.Items {
			id := v["id"].(string)
			if seen[id] {
				t.Fatal("repeated resource", id)
			}
			seen[id] = true
		}
		cursor = p.Next
	}
	if len(seen) != 3 {
		t.Fatal("skipped or foreign resources", seen)
	}
	for _, path := range []string{base + "/rules?cursor=" + url.QueryEscape(first.Next), base + "/nodes?limit=01", base + "/nodes?cursor=bad", base + "/nodes?limit=1&limit=2", base + "/nodes?unknown=x"} {
		w := f.request(t, "GET", path, f.pair.AccessToken, "")
		if w.Code != 400 {
			t.Fatal(path, w.Code, w.Body)
		}
	}
	p, _ := auth.NewPrincipal(uuid.NewString(), f.tenant, auth.Member, "active")
	if _, e := f.pool.Exec(ctx, `INSERT INTO users(id,tenant_id,role) VALUES($1,$2,'member')`, p.ID(), f.tenant); e != nil {
		t.Fatal(e)
	}
	pair, e := f.tokens.Issue(ctx, p, "second-member")
	if e != nil {
		t.Fatal(e)
	}
	if w := f.request(t, "GET", base+"/nodes?cursor="+url.QueryEscape(first.Next), pair.AccessToken, ""); w.Code != 400 {
		t.Fatal("foreign principal cursor", w.Code, w.Body)
	}
	if w := f.request(t, "GET", "/api/v1/tenants/"+other.tenant+"/nodes", f.pair.AccessToken, ""); w.Code != 403 {
		t.Fatal(w.Code, w.Body)
	}
	for _, collection := range []string{"node-groups", "rules", "packages", "subscriptions", "port-allocations", "sources", "restrictions"} {
		f.businessPage(t, base+"/"+collection+"?limit=1", f.pair.AccessToken)
	}
	me := f.request(t, "GET", "/api/v1/me", f.pair.AccessToken, "")
	if me.Code != 200 || !strings.Contains(me.Body.String(), f.user) || !strings.Contains(me.Body.String(), f.tenant) {
		t.Fatal(me.Code, me.Body)
	}
	tenants := f.businessPage(t, "/api/v1/tenants?limit=1", f.pair.AccessToken)
	if len(tenants.Items) != 1 || tenants.Items[0]["id"] != f.tenant || tenants.Next != "" {
		t.Fatal(tenants)
	}
}
func mustJSON(t *testing.T, v any) []byte {
	t.Helper()
	b, e := json.Marshal(v)
	if e != nil {
		t.Fatal(e)
	}
	return b
}

func TestHTTPResourceListsPlatformGrantAndAuditBoundaries(t *testing.T) {
	f := newAPI(t)
	ctx := context.Background()
	base := "/api/v1/tenants/" + f.tenant
	admin := uuid.NewString()
	if _, e := f.pool.Exec(ctx, `INSERT INTO platform_principals(id,role) VALUES($1,'platform_admin')`, admin); e != nil {
		t.Fatal(e)
	}
	p, _ := auth.NewPrincipal(admin, "", auth.PlatformAdmin, "active")
	s, _ := repository.NewTenantScope(p, f.tenant, policy.Mutate)
	g, _ := repository.NewTenantScope(p, f.tenant, policy.Grant)
	node := f.node(t, "platform", s)
	if page := f.businessPage(t, base+"/nodes", f.pair.AccessToken); len(page.Items) != 0 {
		t.Fatal("ungranted platform resource", page)
	}
	serviceNodes := service.NodeService{Pool: f.pool}
	if e := serviceNodes.SetGrant(ctx, g, node, 0, "active", time.Now().Add(-time.Minute), time.Now().Add(time.Hour), "list-grant"); e != nil {
		t.Fatal(e)
	}
	if page := f.businessPage(t, base+"/nodes", f.pair.AccessToken); len(page.Items) != 1 || page.Items[0]["id"] != node {
		t.Fatal(page)
	}
	if e := serviceNodes.SetGrant(ctx, g, node, 1, "revoked", time.Now().Add(-time.Minute), time.Now().Add(time.Hour), "list-revoke"); e != nil {
		t.Fatal(e)
	}
	if page := f.businessPage(t, base+"/nodes", f.pair.AccessToken); len(page.Items) != 0 {
		t.Fatal("registry granted a revoked resource", page)
	}
	if e := serviceNodes.SetGrant(ctx, g, node, 2, "active", time.Now().Add(-2*time.Hour), time.Now().Add(-time.Hour), "list-expired"); e != nil {
		t.Fatal(e)
	}
	if page := f.businessPage(t, base+"/nodes", f.pair.AccessToken); len(page.Items) != 0 {
		t.Fatal("expired grant visible", page)
	}
	pair, e := f.tokens.Issue(ctx, p, "platform-list-auth")
	if e != nil {
		t.Fatal(e)
	}
	own := f.node(t, "tenant", f.ownerScope(t))
	_ = own
	f.businessPage(t, base+"/nodes", pair.AccessToken)
	page := f.businessPage(t, "/api/v1/tenants?limit=1", pair.AccessToken)
	if len(page.Items) != 1 {
		t.Fatal(page)
	}
	var audits int
	if e = f.pool.QueryRow(ctx, `SELECT count(*) FROM audit_events WHERE actor_id=$1 AND tenant_id=$2 AND action='tenant.list.read'`, admin, page.Items[0]["id"]).Scan(&audits); e != nil || audits != 1 {
		t.Fatal("missing per-tenant audit", audits, e)
	}
	name := "lists_audit_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if _, e = f.pool.Exec(ctx, `CREATE FUNCTION `+name+`() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.actor_id='`+admin+`' AND NEW.action IN ('nodes.list','tenant.list.read') THEN RAISE EXCEPTION 'PRIVATE_SQL_SECRET'; END IF; RETURN NEW; END $$; CREATE TRIGGER `+name+` BEFORE INSERT ON audit_events FOR EACH ROW EXECUTE FUNCTION `+name+`() `); e != nil {
		t.Fatal(e)
	}
	defer f.pool.Exec(ctx, `DROP TRIGGER `+name+` ON audit_events; DROP FUNCTION `+name+`() `)
	for _, path := range []string{base + "/nodes", "/api/v1/tenants?limit=1"} {
		w := f.request(t, "GET", path, pair.AccessToken, "")
		if w.Code != 503 || strings.Contains(w.Body.String(), "PRIVATE") || strings.Contains(w.Body.String(), own) {
			t.Fatal("failed audit exposed list", w.Code, w.Body)
		}
	}
}
func TestHTTPRestrictionListHasStableIdentityAcrossProjectionVersions(t *testing.T) {
	f := newAPI(t)
	ctx := context.Background()
	base := "/api/v1/tenants/" + f.tenant + "/restrictions"
	source := "source/with:delimiters"
	event := uuid.NewString()
	if _, e := f.pool.Exec(ctx, `INSERT INTO restriction_events(id,tenant_id,cause,source_id,source_version,event_kind,effective_at) VALUES($1,$2,'manual',$3,1,'applied',now())`, event, f.tenant, source); e != nil {
		t.Fatal(e)
	}
	if _, e := f.pool.Exec(ctx, `INSERT INTO current_restrictions(tenant_id,cause,source_id,source_version,event_id,effective_at) VALUES($1,'manual',$2,1,$3,now())`, f.tenant, source, event); e != nil {
		t.Fatal(e)
	}
	first := f.businessPage(t, base, f.pair.AccessToken)
	if len(first.Items) != 1 {
		t.Fatal(first)
	}
	if _, e := f.pool.Exec(ctx, `UPDATE current_restrictions SET source_version=2,cleared_at=now() WHERE tenant_id=$1`, f.tenant); e != nil {
		t.Fatal(e)
	}
	second := f.businessPage(t, base, f.pair.AccessToken)
	if len(second.Items) != 1 || second.Items[0]["id"] != first.Items[0]["id"] || second.Items[0]["created_at"] != first.Items[0]["created_at"] || second.Items[0]["cleared_at"] == nil {
		t.Fatal("mutable projection changed list identity", first, second)
	}
	for _, sql := range []string{`UPDATE public_resource_order SET created_at=created_at+interval '1 second' WHERE tenant_key=$1`, `DELETE FROM public_resource_order WHERE tenant_key=$1`, `UPDATE current_restrictions SET source_id='changed' WHERE tenant_id=$1`} {
		if _, e := f.pool.Exec(ctx, sql, f.tenant); e == nil {
			t.Fatal("immutable ordering/identity changed", sql)
		}
	}
}

// Upgrade an actual schema8 store: order backfill cannot alter prior authority.
func TestPublicResourceOrderMigrationBackfillsAndPreservesHistoricalRows(t *testing.T) {
	admin := testutil.Postgres(t)
	ctx := context.Background()
	schema := "public_order_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if _, e := admin.Exec(ctx, "CREATE SCHEMA "+schema); e != nil {
		t.Fatal(e)
	}
	defer admin.Exec(ctx, "DROP SCHEMA "+schema+" CASCADE")
	u, e := url.Parse(os.Getenv("PHASE6_DATABASE_URL"))
	if e != nil {
		t.Fatal(e)
	}
	q := u.Query()
	q.Set("search_path", schema)
	u.RawQuery = q.Encode()
	if e = db.MigrateTo(ctx, u.String(), 8); e != nil {
		t.Fatal(e)
	}
	pool, e := pgxpool.New(ctx, u.String())
	if e != nil {
		t.Fatal(e)
	}
	defer pool.Close()
	tenant, node, event := uuid.NewString(), uuid.NewString(), uuid.NewString()
	for _, v := range []struct {
		sql  string
		args []any
	}{
		{`INSERT INTO tenants(id) VALUES($1)`, []any{tenant}},
		{`INSERT INTO resource_ownership(resource_id,resource_kind,owner_kind) VALUES($1,'node','platform')`, []any{node}},
		{`INSERT INTO nodes(id,name,region,hub_id,capabilities,port_min,port_max,link_port,control_port,metrics_port,capacity) VALUES($1,'old-node','old-region','old-hub',ARRAY['entry'],20000,21000,22000,22001,22002,10)`, []any{node}},
		{`INSERT INTO restriction_events(id,tenant_id,cause,source_id,source_version,event_kind,effective_at) VALUES($1,$2,'manual','old/source',1,'applied',now())`, []any{event, tenant}},
		{`INSERT INTO current_restrictions(tenant_id,cause,source_id,source_version,event_id,effective_at) VALUES($1,'manual','old/source',1,$2,now())`, []any{tenant, event}},
	} {
		if _, e = pool.Exec(ctx, v.sql, v.args...); e != nil {
			t.Fatal(e)
		}
	}
	snapshot := func() string {
		var result string
		if e := pool.QueryRow(ctx, `SELECT jsonb_build_array((SELECT to_jsonb(t) FROM tenants t WHERE id=$1),(SELECT to_jsonb(n) FROM nodes n WHERE id=$2),(SELECT to_jsonb(r) FROM current_restrictions r WHERE tenant_id=$1))::text`, tenant, node).Scan(&result); e != nil {
			t.Fatal(e)
		}
		return result
	}
	before := snapshot()
	if e = db.Migrate(ctx, u.String()); e != nil {
		t.Fatal(e)
	}
	if after := snapshot(); after != before {
		t.Fatal("backfill rewrote old authority", before, after)
	}
	var count int
	if e = pool.QueryRow(ctx, `SELECT count(*) FROM public_resource_order`).Scan(&count); e != nil || count != 3 {
		t.Fatal(count, e)
	}
	var body string
	if e = pool.QueryRow(ctx, `SELECT jsonb_agg(jsonb_build_array(collection,tenant_key,resource_key,sort_id) ORDER BY collection)::text FROM public_resource_order`).Scan(&body); e != nil {
		t.Fatal(e)
	}
	if !strings.Contains(body, node) || !strings.Contains(body, tenant) || !strings.Contains(body, "restrictions") {
		t.Fatal(body)
	}
	// Registry rows are written in the same transaction as the originating row.
	tx, e := pool.Begin(ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer tx.Rollback(ctx)
	rolled := uuid.NewString()
	if _, e = tx.Exec(ctx, `INSERT INTO tenants(id) VALUES($1)`, rolled); e != nil {
		t.Fatal(e)
	}
	if e = tx.Rollback(ctx); e != nil {
		t.Fatal(e)
	}
	if e = pool.QueryRow(ctx, `SELECT count(*) FROM public_resource_order WHERE resource_key=$1`, rolled).Scan(&count); e != nil || count != 0 {
		t.Fatal("ordering escaped rollback", count, e)
	}
}
