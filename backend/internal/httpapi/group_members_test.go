package httpapi_test

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"vortex/backend/internal/policy"
	"vortex/backend/internal/repository"
	"vortex/backend/internal/service"
)

func (f *apiFixture) group(t *testing.T) string {
	t.Helper()
	id := uuid.NewString()
	if _, e := (service.NodeService{Pool: f.pool}).CreateGroup(context.Background(), f.ownerScope(t), service.GroupInput{ID: id, Name: "入口组", OwnerKind: "tenant", Purpose: "entry", Scheduling: "dns", HealthIntervalSeconds: 10, HealthTimeoutSeconds: 2}, uuid.NewString()); e != nil {
		t.Fatal(e)
	}
	return id
}
func (f *apiFixture) replaceMembers(t *testing.T, group, tag string, body any) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest("PUT", "/api/v1/tenants/"+f.tenant+"/node-groups/"+group+"/members", strings.NewReader(string(mustJSON(t, body))))
	r.Header.Set("Authorization", "Bearer "+f.pair.AccessToken)
	r.Header.Set("Content-Type", "application/json")
	if tag != "" {
		r.Header.Set("If-Match", tag)
	}
	w := httptest.NewRecorder()
	f.handler.ServeHTTP(w, r)
	return w
}
func TestHTTPGroupMembersExactOperationCASAndSafeRead(t *testing.T) {
	f := newAPI(t)
	g := f.group(t)
	node := f.node(t, "tenant", f.ownerScope(t))
	path := "/api/v1/tenants/" + f.tenant + "/node-groups/" + g + "/members"
	empty := f.request(t, "GET", path, f.pair.AccessToken, "")
	if empty.Code != 200 || !strings.Contains(empty.Body.String(), `"members": []`) && !strings.Contains(empty.Body.String(), `"members":[]`) {
		t.Fatal(empty.Code, empty.Body)
	}
	body := map[string]any{"members": []map[string]any{{"node_id": node, "weight": 2, "priority": 0}}}
	if w := f.replaceMembers(t, g, "", body); w.Code != 428 {
		t.Fatal(w.Code, w.Body)
	}
	responses := make(chan *httptest.ResponseRecorder, 8)
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); responses <- f.replaceMembers(t, g, `"1"`, body) }()
	}
	wg.Wait()
	close(responses)
	accepted, stale := 0, 0
	for w := range responses {
		switch w.Code {
		case 202:
			accepted++
			if w.Header().Get("ETag") != `"2"` {
				t.Fatal(w.Header())
			}
			var result struct {
				GroupID       string `json:"group_id"`
				Operation     string `json:"operation_id"`
				Version       int64  `json:"version"`
				MemberVersion int64  `json:"member_version"`
			}
			if e := json.Unmarshal(w.Body.Bytes(), &result); e != nil || result.GroupID != g || result.Operation == "" || result.Version != 2 || result.MemberVersion != 2 {
				t.Fatal(result, e)
			}
			op := f.request(t, "GET", w.Header().Get("Location"), f.pair.AccessToken, "")
			if op.Code != 200 || !strings.Contains(op.Body.String(), g) || !strings.Contains(op.Body.String(), `"target_revision":2`) {
				t.Fatal(op.Code, op.Body)
			}
		case 412:
			stale++
		default:
			t.Fatal(w.Code, w.Body)
		}
	}
	if accepted != 1 || stale != 7 {
		t.Fatal(accepted, stale)
	}
	read := f.request(t, "GET", path, f.pair.AccessToken, "")
	if read.Code != 200 || !strings.Contains(read.Body.String(), node) {
		t.Fatal(read.Code, read.Body)
	}
	for _, secret := range []string{"PRIVATE_", "boot_id", "generation", "public_address", "hub_id", "control_port"} {
		if strings.Contains(read.Body.String(), secret) {
			t.Fatal("member read exposed internal field", read.Body)
		}
	}
	if w := f.replaceMembers(t, g, `"2"`, map[string]any{"members": []any{}}); w.Code != 202 {
		t.Fatal(w.Code, w.Body)
	}
	read = f.request(t, "GET", path, f.pair.AccessToken, "")
	if read.Code != 200 || strings.Contains(read.Body.String(), node) {
		t.Fatal("explicit empty did not clear", read.Code, read.Body)
	}
}
func TestHTTPGroupMembersAuditRollbackAndPortRevalidation(t *testing.T) {
	f := newAPI(t)
	ctx := context.Background()
	g := f.group(t)
	node := f.node(t, "tenant", f.ownerScope(t))
	port := uuid.NewString()
	if _, e := f.pool.Exec(ctx, `INSERT INTO port_allocations(id,tenant_id,rule_id,group_id,member_set_version,port,listen_address,protocols,purpose,state) VALUES($1,$2,$3,$4,repeat('0',64),20001,'PRIVATE_LISTEN',ARRAY['tcp'],'service','unknown')`, port, f.tenant, uuid.NewString(), g); e != nil {
		t.Fatal(e)
	}
	name := "group_http_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if _, e := f.pool.Exec(ctx, `CREATE FUNCTION `+name+`() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.tenant_id='`+f.tenant+`' AND NEW.action='group.members' THEN RAISE EXCEPTION 'injected group HTTP audit'; END IF; RETURN NEW; END $$; CREATE TRIGGER `+name+` BEFORE INSERT ON audit_events FOR EACH ROW EXECUTE FUNCTION `+name+`() `); e != nil {
		t.Fatal(e)
	}
	drop := func() {
		if _, e := f.pool.Exec(ctx, `DROP TRIGGER IF EXISTS `+name+` ON audit_events; DROP FUNCTION IF EXISTS `+name+`() `); e != nil {
			t.Error(e)
		}
	}
	t.Cleanup(drop)
	body := map[string]any{"members": []map[string]any{{"node_id": node, "weight": 1, "priority": 0}}}
	var tasksBefore int
	if e := f.pool.QueryRow(ctx, `SELECT count(*) FROM tasks WHERE tenant_id=$1`, f.tenant).Scan(&tasksBefore); e != nil {
		t.Fatal(e)
	}
	if w := f.replaceMembers(t, g, `"1"`, body); w.Code != 503 || strings.Contains(w.Body.String(), "injected") {
		t.Fatal(w.Code, w.Body)
	}
	var members, tasks int
	var version int64
	var dirty bool
	if e := f.pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM node_group_members WHERE group_id=$1),(SELECT version FROM node_groups WHERE id=$1),(SELECT count(*) FROM tasks WHERE tenant_id=$2),(SELECT needs_revalidation FROM port_allocations WHERE id=$3)`, g, f.tenant, port).Scan(&members, &version, &tasks, &dirty); e != nil || members != 0 || version != 1 || tasks != tasksBefore || dirty {
		t.Fatal(members, version, tasks, dirty, e)
	}
	drop()
	if w := f.replaceMembers(t, g, `"1"`, body); w.Code != 202 {
		t.Fatal(w.Code, w.Body)
	}
	var state string
	if e := f.pool.QueryRow(ctx, `SELECT needs_revalidation,state FROM port_allocations WHERE id=$1`, port).Scan(&dirty, &state); e != nil || !dirty || state != "unknown" {
		t.Fatal("membership released uncertain occupation", dirty, state, e)
	}
}
func TestHTTPGroupMembersValidationAndOwnership(t *testing.T) {
	f := newAPI(t)
	g := f.group(t)
	node := f.node(t, "tenant", f.ownerScope(t))
	cases := []any{map[string]any{}, map[string]any{"members": nil}, map[string]any{"members": []map[string]any{{"node_id": node, "weight": 0, "priority": 0}}}, map[string]any{"members": []map[string]any{{"node_id": node, "weight": 1, "priority": -1}}}, map[string]any{"members": []map[string]any{{"node_id": node, "weight": 1, "priority": 0}, {"node_id": node, "weight": 1, "priority": 0}}}}
	for _, body := range cases {
		if w := f.replaceMembers(t, g, `"1"`, body); w.Code != 400 {
			t.Fatal(w.Code, w.Body)
		}
	}
	other := newAPI(t)
	foreign := other.node(t, "tenant", other.ownerScope(t))
	if w := f.replaceMembers(t, g, `"1"`, map[string]any{"members": []map[string]any{{"node_id": foreign, "weight": 1, "priority": 0}}}); w.Code != 404 {
		t.Fatal(w.Code, w.Body)
	}
	private := uuid.NewString()
	if _, e := (service.NodeService{Pool: f.pool}).CreateNode(context.Background(), f.ownerScope(t), service.NodeInput{ID: private, HubID: uuid.NewString(), Name: "relay only", Region: "test", OwnerKind: "tenant", Capabilities: []string{"relay"}, PortMin: 20000, PortMax: 21000, LinkPort: 22000, ControlPort: 22001, MetricsPort: 22002, Capacity: 100}, uuid.NewString()); e != nil {
		t.Fatal(e)
	}
	if w := f.replaceMembers(t, g, `"1"`, map[string]any{"members": []map[string]any{{"node_id": private, "weight": 1, "priority": 0}}}); w.Code != 409 {
		t.Fatal(w.Code, w.Body)
	}
	// A currently granted platform node is readable but ownership cannot be
	// transferred into this tenant-owned group by member replacement.
	platform := f.platformToken(t, "platform_admin")
	p, e := f.tokens.Authenticate(context.Background(), platform)
	if e != nil {
		t.Fatal(e)
	}
	mutate, e := repository.NewTenantScope(p, f.tenant, policy.Mutate)
	if e != nil {
		t.Fatal(e)
	}
	platformNode := f.node(t, "platform", mutate)
	grant, e := repository.NewTenantScope(p, f.tenant, policy.Grant)
	if e != nil {
		t.Fatal(e)
	}
	if e := (service.NodeService{Pool: f.pool}).SetGrant(context.Background(), grant, platformNode, 0, "active", time.Now().Add(-time.Minute), time.Now().Add(time.Hour), uuid.NewString()); e != nil {
		t.Fatal(e)
	}
	if w := f.replaceMembers(t, g, `"1"`, map[string]any{"members": []map[string]any{{"node_id": platformNode, "weight": 1, "priority": 0}}}); w.Code != 403 {
		t.Fatal(w.Code, w.Body)
	}
}

func TestHTTPGroupMembersGrantRevocationAndPlatformAudit(t *testing.T) {
	f := newAPI(t)
	ctx := context.Background()
	token := f.platformToken(t, "platform_admin")
	p, e := f.tokens.Authenticate(ctx, token)
	if e != nil {
		t.Fatal(e)
	}
	mutate, e := repository.NewTenantScope(p, f.tenant, policy.Mutate)
	if e != nil {
		t.Fatal(e)
	}
	grant, e := repository.NewTenantScope(p, f.tenant, policy.Grant)
	if e != nil {
		t.Fatal(e)
	}
	g := uuid.NewString()
	s := service.NodeService{Pool: f.pool}
	if _, e = s.CreateGroup(ctx, mutate, service.GroupInput{ID: g, Name: "平台组", OwnerKind: "platform", Purpose: "entry", Scheduling: "dns", HealthIntervalSeconds: 10, HealthTimeoutSeconds: 2, BillingMultiplierMilli: 1000}, uuid.NewString()); e != nil {
		t.Fatal(e)
	}
	path := "/api/v1/tenants/" + f.tenant + "/node-groups/" + g + "/members"
	if w := f.request(t, "GET", path, f.pair.AccessToken, ""); w.Code != 404 {
		t.Fatal(w.Code, w.Body)
	}
	if e = s.SetGrant(ctx, grant, g, 0, "active", time.Now().Add(-time.Minute), time.Now().Add(time.Hour), uuid.NewString()); e != nil {
		t.Fatal(e)
	}
	if w := f.request(t, "GET", path, f.pair.AccessToken, ""); w.Code != 200 {
		t.Fatal(w.Code, w.Body)
	}
	if e = s.SetGrant(ctx, grant, g, 1, "revoked", time.Now().Add(-time.Minute), time.Now().Add(time.Hour), uuid.NewString()); e != nil {
		t.Fatal(e)
	}
	if w := f.request(t, "GET", path, f.pair.AccessToken, ""); w.Code != 404 {
		t.Fatal(w.Code, w.Body)
	}
	owned := f.group(t)
	path = "/api/v1/tenants/" + f.tenant + "/node-groups/" + owned + "/members"
	name := "member_read_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if _, e = f.pool.Exec(ctx, `CREATE FUNCTION `+name+`() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.tenant_id='`+f.tenant+`' AND NEW.action='node_group_members.read' THEN RAISE EXCEPTION 'injected read audit'; END IF; RETURN NEW; END $$; CREATE TRIGGER `+name+` BEFORE INSERT ON audit_events FOR EACH ROW EXECUTE FUNCTION `+name+`() `); e != nil {
		t.Fatal(e)
	}
	defer f.pool.Exec(ctx, `DROP TRIGGER `+name+` ON audit_events; DROP FUNCTION `+name+`() `)
	if w := f.request(t, "GET", path, token, ""); w.Code != 503 || strings.Contains(w.Body.String(), owned) {
		t.Fatal(w.Code, w.Body)
	}
}

// The public value is exactly the port snapshot input, never the private Node
// fields from which the hash is derived. Health is deliberately not a version.
func TestHTTPGroupMembersOpaqueDigestTracksAuthorityNotHealth(t *testing.T) {
	f := newAPI(t)
	ctx := context.Background()
	g := f.group(t)
	n := f.node(t, "tenant", f.ownerScope(t))
	if w := f.replaceMembers(t, g, `"1"`, map[string]any{"members": []map[string]any{{"node_id": n, "weight": 1, "priority": 0}}}); w.Code != 202 {
		t.Fatal(w.Code, w.Body)
	}
	path := "/api/v1/tenants/" + f.tenant + "/node-groups/" + g + "/members"
	read := func() string {
		t.Helper()
		w := f.request(t, "GET", path, f.pair.AccessToken, "")
		if w.Code != 200 {
			t.Fatal(w.Code, w.Body)
		}
		var body struct {
			Digest string `json:"member_set_version"`
		}
		if e := json.Unmarshal(w.Body.Bytes(), &body); e != nil || len(body.Digest) != 64 {
			t.Fatal(body, e, w.Body)
		}
		snap, e := (service.NodeService{Pool: f.pool}).Snapshot(ctx, f.ownerScope(t), g, uuid.NewString())
		if e != nil || snap.MemberSetVersion != body.Digest {
			t.Fatal("public digest not usable by port service", snap.MemberSetVersion, body.Digest, e)
		}
		for _, key := range []string{"PRIVATE_", "boot_id", "generation", "public_address", "hub_id", "control_port", "program_version"} {
			if strings.Contains(w.Body.String(), key) {
				t.Fatal("private digest input leaked", w.Body)
			}
		}
		return body.Digest
	}
	before := read()
	if _, e := f.pool.Exec(ctx, `UPDATE nodes SET health='unhealthy' WHERE id=$1`, n); e != nil {
		t.Fatal(e)
	}
	if read() != before {
		t.Fatal("health changed configuration digest")
	}
	if _, e := f.pool.Exec(ctx, `UPDATE nodes SET boot_id=$2,generation=generation+1,version=version+1 WHERE id=$1`, n, uuid.NewString()); e != nil {
		t.Fatal(e)
	}
	if read() == before {
		t.Fatal("Node incarnation reused stale digest")
	}
}
