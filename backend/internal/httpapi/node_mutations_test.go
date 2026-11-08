package httpapi_test

import (
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"vortex/backend/internal/auth"
)

func (f *apiFixture) patchNode(t *testing.T, node, token, tag, body string) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest("PATCH", "/api/v1/tenants/"+f.tenant+"/nodes/"+node, strings.NewReader(body))
	r.Header.Set("Authorization", "Bearer "+token)
	r.Header.Set("Content-Type", "application/json")
	if tag != "" {
		r.Header.Set("If-Match", tag)
	}
	w := httptest.NewRecorder()
	f.handler.ServeHTTP(w, r)
	return w
}
func TestHTTPNodeTransitionIfMatchReturnsExactOperationAndKeepsExecutionPending(t *testing.T) {
	f := newAPI(t)
	node := f.node(t, "tenant", f.ownerScope(t))
	ctx := context.Background()
	for tag, status := range map[string]int{"": 428, `1`: 400, `W/"1"`: 400, `"01"`: 400, `"-1"`: 400} {
		w := f.patchNode(t, node, f.pair.AccessToken, tag, `{"lifecycle":"online"}`)
		if w.Code != status {
			t.Fatal(tag, w.Code, w.Body)
		}
	}
	w := f.patchNode(t, node, f.pair.AccessToken, `"1"`, `{"lifecycle":"online"}`)
	if w.Code != 202 || w.Header().Get("ETag") != `"2"` {
		t.Fatal(w.Code, w.Body, w.Header())
	}
	var body struct {
		Node      string `json:"node_id"`
		Operation string `json:"operation_id"`
		State     string `json:"sync_status"`
		Lifecycle string `json:"desired_lifecycle"`
		Version   int64  `json:"version"`
	}
	if e := json.Unmarshal(w.Body.Bytes(), &body); e != nil {
		t.Fatal(e)
	}
	if body.Node != node || body.Operation == "" || body.State != "pending" || body.Lifecycle != "online" || body.Version != 2 {
		t.Fatal(w.Body)
	}
	var linked int
	if e := f.pool.QueryRow(ctx, `SELECT count(*) FROM operations o JOIN tasks t ON t.tenant_id=o.tenant_id AND t.operation_id=o.id JOIN outbox b ON b.tenant_id=o.tenant_id AND b.operation_id=o.id WHERE o.tenant_id=$1 AND o.id=$2 AND o.state='pending' AND convert_from(o.payload,'UTF8')::jsonb->>'resource_id'=$3 AND convert_from(o.payload,'UTF8')::jsonb->>'action'='node.online'`, f.tenant, body.Operation, node).Scan(&linked); e != nil || linked != 1 {
		t.Fatal("wrong operation linkage", linked, e)
	}
	if r := f.request(t, "GET", w.Header().Get("Location"), f.pair.AccessToken, ""); r.Code != 200 || !strings.Contains(r.Body.String(), `"state":"pending"`) {
		t.Fatal(r.Code, r.Body)
	}
	r := f.request(t, "GET", w.Header().Get("Location"), f.pair.AccessToken, "")
	var operation struct {
		ResourceID string `json:"resource_id"`
		Revision   int64  `json:"target_revision"`
	}
	if e := json.Unmarshal(r.Body.Bytes(), &operation); e != nil {
		t.Fatal(e)
	}
	if operation.ResourceID != node || operation.Revision != 2 {
		t.Fatal("public operation exposed synthetic identity/version", r.Body)
	}
	list := f.businessPage(t, "/api/v1/tenants/"+f.tenant+"/operations", f.pair.AccessToken)
	if len(list.Items) != 1 || list.Items[0]["resource_id"] != node || list.Items[0]["target_revision"] != float64(2) {
		t.Fatal("operation list used internal identity", list)
	}
	for _, v := range []struct {
		tag, payload string
		status       int
	}{{`"1"`, `{"lifecycle":"offline"}`, 412}, {`"2"`, `{"lifecycle":"disabled"}`, 409}, {`"2"`, `{"lifecycle":"bogus"}`, 400}, {`"2"`, `{"lifecycle":"draining","drain_seconds":86401}`, 400}, {`"2"`, `{"lifecycle":"offline","expected_version":2}`, 400}} {
		w := f.patchNode(t, node, f.pair.AccessToken, v.tag, v.payload)
		if w.Code != v.status {
			t.Fatal(v, w.Code, w.Body)
		}
	}
}
func TestHTTPNodeTransitionScopeAuditRollbackAndConcurrentCAS(t *testing.T) {
	f := newAPI(t)
	ctx := context.Background()
	node := f.node(t, "tenant", f.ownerScope(t))
	other := newAPI(t)
	if w := other.patchNode(t, node, other.pair.AccessToken, `"1"`, `{"lifecycle":"online"}`); w.Code != 404 {
		t.Fatal("foreign resource", w.Code, w.Body)
	}
	actor := uuid.NewString()
	if _, e := f.pool.Exec(ctx, `INSERT INTO platform_principals(id,role) VALUES($1,'auditor')`, actor); e != nil {
		t.Fatal(e)
	}
	p, _ := auth.NewPrincipal(actor, "", auth.Auditor, "active")
	pair, e := f.tokens.Issue(ctx, p, "node-auditor")
	if e != nil {
		t.Fatal(e)
	}
	if w := f.patchNode(t, node, pair.AccessToken, `"1"`, `{"lifecycle":"online"}`); w.Code != 403 {
		t.Fatal("auditor mutated", w.Code, w.Body)
	}
	name := "node_http_audit_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if _, e = f.pool.Exec(ctx, `CREATE FUNCTION `+name+`() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.actor_id='`+f.user+`' AND NEW.action='node.lifecycle' THEN RAISE EXCEPTION 'PRIVATE_SQL_SECRET'; END IF; RETURN NEW; END $$; CREATE TRIGGER `+name+` BEFORE INSERT ON audit_events FOR EACH ROW EXECUTE FUNCTION `+name+`() `); e != nil {
		t.Fatal(e)
	}
	drop := func() {
		if _, e := f.pool.Exec(ctx, `DROP TRIGGER `+name+` ON audit_events; DROP FUNCTION `+name+`() `); e != nil {
			t.Error(e)
		}
	}
	defer func() {
		if drop != nil {
			drop()
		}
	}()
	if w := f.patchNode(t, node, f.pair.AccessToken, `"1"`, `{"lifecycle":"online"}`); w.Code != 503 || strings.Contains(w.Body.String(), "PRIVATE") {
		t.Fatal(w.Code, w.Body)
	}
	var unchanged bool
	if e = f.pool.QueryRow(ctx, `SELECT lifecycle='pending' AND version=1 AND NOT EXISTS(SELECT 1 FROM operations WHERE tenant_id=$2) AND NOT EXISTS(SELECT 1 FROM tasks WHERE tenant_id=$2) AND NOT EXISTS(SELECT 1 FROM outbox WHERE tenant_id=$2) AND NOT EXISTS(SELECT 1 FROM domain_events WHERE tenant_id=$2) FROM nodes WHERE id=$1`, node, f.tenant).Scan(&unchanged); e != nil || !unchanged {
		t.Fatal("HTTP audit failure escaped rollback", unchanged, e)
	}
	drop()
	drop = nil
	var wg sync.WaitGroup
	codes := make(chan int, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			codes <- f.patchNode(t, node, f.pair.AccessToken, `"1"`, `{"lifecycle":"online"}`).Code
		}()
	}
	wg.Wait()
	close(codes)
	accepted, stale := 0, 0
	for code := range codes {
		switch code {
		case 202:
			accepted++
		case 412:
			stale++
		default:
			t.Fatal(code)
		}
	}
	if accepted != 1 || stale != 7 {
		t.Fatal("CAS admitted duplicates", accepted, stale)
	}
	var count int
	if e = f.pool.QueryRow(ctx, `SELECT count(*) FROM operations WHERE tenant_id=$1`, f.tenant).Scan(&count); e != nil || count != 1 {
		t.Fatal(count, e)
	}
}
