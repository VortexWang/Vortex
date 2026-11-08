package httpapi_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"vortex/backend/internal/auth"
	"vortex/backend/internal/compiler"
	"vortex/backend/internal/httpapi"
	"vortex/backend/internal/repository"
	"vortex/backend/internal/service"
	"vortex/backend/internal/testutil"
)

type apiFixture struct {
	pool         *pgxpool.Pool
	tokens       *auth.Tokens
	tenant, user string
	pair         auth.TokenPair
	handler      http.Handler
}

func newAPI(t *testing.T) *apiFixture {
	t.Helper()
	pool := testutil.Postgres(t)
	ctx := context.Background()
	f := &apiFixture{pool: pool, tenant: uuid.NewString(), user: uuid.NewString()}
	if _, e := pool.Exec(ctx, `INSERT INTO tenants(id) VALUES($1)`, f.tenant); e != nil {
		t.Fatal(e)
	}
	if _, e := pool.Exec(ctx, `INSERT INTO users(id,tenant_id,role) VALUES($1,$2,'owner')`, f.user, f.tenant); e != nil {
		t.Fatal(e)
	}
	f.tokens = &auth.Tokens{Store: repository.TokenRepository{Pool: pool}, AccessTTL: time.Minute, RefreshTTL: time.Hour}
	p, _ := auth.NewPrincipal(f.user, f.tenant, auth.Owner, "active")
	var e error
	f.pair, e = f.tokens.Issue(ctx, p, "fixture-auth")
	if e != nil {
		t.Fatal(e)
	}
	codec, e := httpapi.NewCursorCodec([]byte(strings.Repeat("cursor-key", 4)), time.Hour)
	if e != nil {
		t.Fatal(e)
	}
	f.handler, e = httpapi.NewRouter(httpapi.Config{Pool: pool, Tokens: f.tokens, Cursors: codec})
	if e != nil {
		t.Fatal(e)
	}
	return f
}
func (f *apiFixture) request(t *testing.T, method, path, token, body string) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	if token != "" {
		r.Header.Set("Authorization", "Bearer "+token)
	}
	if body != "" {
		r.Header.Set("Content-Type", "application/json")
	}
	w := httptest.NewRecorder()
	f.handler.ServeHTTP(w, r)
	if w.Header().Get("X-Request-ID") == "" || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("missing safe headers", w.Header())
	}
	return w
}
func (f *apiFixture) operations(t *testing.T, n int) []string {
	t.Helper()
	ctx := context.Background()
	at := time.Now().UTC().Truncate(time.Microsecond)
	resource := uuid.NewString()
	payload := []byte("PRIVATE_RUNTIME_TARGET_SQL_SECRET")
	digest := bytes.Repeat([]byte{1}, 32)
	if _, e := f.pool.Exec(ctx, `INSERT INTO desired_state(tenant_id,resource_id,target_kind,revision,payload,payload_sha256) VALUES($1,$2,'node',1,$3,$4)`, f.tenant, resource, payload, digest); e != nil {
		t.Fatal(e)
	}
	ids := []string{}
	for i := 0; i < n; i++ {
		id := uuid.NewString()
		if _, e := f.pool.Exec(ctx, `INSERT INTO operations(id,tenant_id,resource_id,idempotency_key,request_sha256,target_kind,target_revision,expected_revision,generation,payload,created_at) VALUES($1,$2,$3,$1,$4,'node',1,0,1,$5,$6)`, id, f.tenant, resource, digest, payload, at); e != nil {
			t.Fatal(e)
		}
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}
func TestHTTPAuthScopeSafeOperationProjectionAndCursorPagination(t *testing.T) {
	f := newAPI(t)
	ids := f.operations(t, 5)
	base := "/api/v1/tenants/" + f.tenant + "/operations"
	if w := f.request(t, "GET", base, "", ""); w.Code != 401 {
		t.Fatal(w.Code, w.Body)
	}
	foreign := "/api/v1/tenants/" + uuid.NewString() + "/operations"
	if w := f.request(t, "GET", foreign, f.pair.AccessToken, ""); w.Code != 403 {
		t.Fatal(w.Code, w.Body)
	}
	got := []string{}
	cursor := ""
	firstCursor := ""
	for i := 0; i < 3; i++ {
		url := base + "?limit=2"
		if cursor != "" {
			url += "&cursor=" + cursor
		}
		w := f.request(t, "GET", url, f.pair.AccessToken, "")
		if w.Code != 200 {
			t.Fatal(w.Code, w.Body)
		}
		if strings.Contains(w.Body.String(), "PRIVATE_") || strings.Contains(w.Body.String(), "payload") || strings.Contains(w.Body.String(), "tenant_id") {
			t.Fatal("internal data exposed", w.Body)
		}
		var page struct {
			Items []repository.PublicOperation `json:"items"`
			Next  string                       `json:"next_cursor"`
		}
		if e := json.Unmarshal(w.Body.Bytes(), &page); e != nil {
			t.Fatal(e)
		}
		for _, v := range page.Items {
			got = append(got, v.ID)
		}
		cursor = page.Next
		if i == 0 {
			firstCursor = cursor
			// Worker timing updates must not move the public pagination tuple.
			if _, e := f.pool.Exec(context.Background(), `UPDATE operations SET created_at=created_at-interval '1 day' WHERE tenant_id=$1`, f.tenant); e != nil {
				t.Fatal(e)
			}
		}
	}
	if strings.Join(got, ",") != strings.Join(ids, ",") || cursor != "" {
		t.Fatal(got, ids, cursor)
	}
	another := uuid.NewString()
	if _, e := f.pool.Exec(context.Background(), `INSERT INTO users(id,tenant_id,role) VALUES($1,$2,'member')`, another, f.tenant); e != nil {
		t.Fatal(e)
	}
	p, _ := auth.NewPrincipal(another, f.tenant, auth.Member, "active")
	pair, e := f.tokens.Issue(context.Background(), p, "another")
	if e != nil {
		t.Fatal(e)
	}
	if w := f.request(t, "GET", base+"?cursor="+firstCursor, pair.AccessToken, ""); w.Code != 400 {
		t.Fatal("foreign principal cursor", w.Code, w.Body)
	}
	for _, query := range []string{"?limit=0", "?limit=101", "?limit=02", "?limit=2&limit=3", "?cursor=", "?state=pending", "?cursor=broken"} {
		w := f.request(t, "GET", base+query, f.pair.AccessToken, "")
		if w.Code != 400 {
			t.Fatal(query, w.Code, w.Body)
		}
		var problem map[string]any
		json.Unmarshal(w.Body.Bytes(), &problem)
		if problem["request_id"] == "" || problem["code"] == nil || problem["message"] == nil {
			t.Fatal(problem)
		}
	}
	w := f.request(t, "GET", base+"/"+ids[0], f.pair.AccessToken, "")
	if w.Code != 200 || strings.Contains(w.Body.String(), "PRIVATE") {
		t.Fatal(w.Code, w.Body)
	}
	if w = f.request(t, "GET", base+"/"+uuid.NewString(), f.pair.AccessToken, ""); w.Code != 404 {
		t.Fatal(w.Code, w.Body)
	}
}
func TestHTTPRefreshRotationReuseAndLogout(t *testing.T) {
	f := newAPI(t)
	body, _ := json.Marshal(map[string]string{"refresh_token": f.pair.RefreshToken})
	w := f.request(t, "POST", "/api/v1/auth/refresh", "", string(body))
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body)
	}
	var next auth.TokenPair
	json.Unmarshal(w.Body.Bytes(), &next)
	if next.AccessToken == "" || strings.Contains(w.Body.String(), "FamilyID") {
		t.Fatal("unsafe token reply")
	}
	w = f.request(t, "POST", "/api/v1/auth/refresh", "", string(body))
	if w.Code != 401 {
		t.Fatal("reuse", w.Code, w.Body)
	}
	base := "/api/v1/tenants/" + f.tenant + "/operations"
	if w = f.request(t, "GET", base, next.AccessToken, ""); w.Code != 401 {
		t.Fatal("family still live", w.Code, w.Body)
	}
	p, _ := auth.NewPrincipal(f.user, f.tenant, auth.Owner, "active")
	pair, e := f.tokens.Issue(context.Background(), p, "second-login")
	if e != nil {
		t.Fatal(e)
	}
	if w = f.request(t, "POST", "/api/v1/auth/logout", pair.AccessToken, ""); w.Code != 204 {
		t.Fatal(w.Code, w.Body)
	}
	if w = f.request(t, "GET", base, pair.AccessToken, ""); w.Code != 401 {
		t.Fatal("logout", w.Code, w.Body)
	}
	for _, bad := range []string{`{"refresh_token":"bad","role":"platform_admin"}`, `{}`, `{"refresh_token":`, strings.Repeat("x", 17000)} {
		if w = f.request(t, "POST", "/api/v1/auth/refresh", "", bad); w.Code != 400 && w.Code != 401 {
			t.Fatal(w.Code)
		}
	}
}
func TestHTTPBearerDoesNotAcceptMTLSRefreshOrAmbiguousHeaders(t *testing.T) {
	f := newAPI(t)
	base := "/api/v1/tenants/" + f.tenant + "/operations"
	for _, values := range [][]string{{}, {"Bearer " + f.pair.RefreshToken}, {"bearer " + f.pair.AccessToken}, {"Bearer " + f.pair.AccessToken, "Bearer " + f.pair.AccessToken}, {"Bearer " + f.pair.AccessToken + ",evil"}} {
		r := httptest.NewRequest("GET", base, nil)
		r.TLS = &tls.ConnectionState{PeerCertificates: []*x509.Certificate{{}}, VerifiedChains: [][]*x509.Certificate{{{}}}}
		for _, v := range values {
			r.Header.Add("Authorization", v)
		}
		w := httptest.NewRecorder()
		f.handler.ServeHTTP(w, r)
		if w.Code != 401 {
			t.Fatal("non-bearer accepted", w.Code)
		}
	}
	if _, e := f.pool.Exec(context.Background(), `UPDATE users SET state='suspended' WHERE id=$1`, f.user); e != nil {
		t.Fatal(e)
	}
	if w := f.request(t, "GET", base, f.pair.AccessToken, ""); w.Code != 401 {
		t.Fatal("suspended", w.Code, w.Body)
	}
}
func TestHTTPPlatformReadAuditFailureReturnsSafeUnavailable(t *testing.T) {
	f := newAPI(t)
	ctx := context.Background()
	id := uuid.NewString()
	if _, e := f.pool.Exec(ctx, `INSERT INTO platform_principals(id,role) VALUES($1,'auditor')`, id); e != nil {
		t.Fatal(e)
	}
	p, _ := auth.NewPrincipal(id, "", auth.Auditor, "active")
	pair, e := f.tokens.Issue(ctx, p, "platform-login")
	if e != nil {
		t.Fatal(e)
	}
	path := "/api/v1/tenants/" + f.tenant + "/operations"
	w := f.request(t, "GET", path, pair.AccessToken, "")
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body)
	}
	var count int
	if e = f.pool.QueryRow(ctx, `SELECT count(*) FROM audit_events WHERE actor_id=$1 AND tenant_id=$2 AND action='operation.list'`, id, f.tenant).Scan(&count); e != nil || count != 1 {
		t.Fatal(count, e)
	}
	trigger := "api_audit_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	_, e = f.pool.Exec(ctx, `CREATE FUNCTION `+trigger+`() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.actor_id='`+id+`' AND NEW.action='operation.list' THEN RAISE EXCEPTION 'PRIVATE_SQL_SECRET'; END IF; RETURN NEW; END $$; CREATE TRIGGER `+trigger+` BEFORE INSERT ON audit_events FOR EACH ROW EXECUTE FUNCTION `+trigger+`()`)
	if e != nil {
		t.Fatal(e)
	}
	defer f.pool.Exec(ctx, `DROP TRIGGER `+trigger+` ON audit_events; DROP FUNCTION `+trigger+`()`)
	w = f.request(t, "GET", path, pair.AccessToken, "")
	if w.Code != 503 || strings.Contains(w.Body.String(), "PRIVATE") {
		t.Fatal(w.Code, w.Body)
	}
}
func TestHTTPRealServerRequestIDAndContentType(t *testing.T) {
	f := newAPI(t)
	server := httptest.NewServer(f.handler)
	defer server.Close()
	req, _ := http.NewRequest("POST", server.URL+"/api/v1/auth/refresh", strings.NewReader(`{}`))
	req.Header.Set("X-Request-ID", "injected-secret")
	resp, e := server.Client().Do(req)
	if e != nil {
		t.Fatal(e)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 415 || resp.Header.Get("X-Request-ID") == "injected-secret" || bytes.Contains(raw, []byte("injected-secret")) {
		t.Fatal(resp.StatusCode, string(raw))
	}
}

func TestHTTPRuleMutationExplicitIfMatchAcceptedOperationAndStaleConflict(t *testing.T) {
	f := newAPI(t)
	ctx := context.Background()
	rule := service.RuleInput{ID: uuid.NewString(), Name: "rule", Protocol: "tcp", Mode: "direct", Port: 20000, Path: []string{uuid.NewString()}, Targets: []service.RuleTarget{{Address: "192.0.2.80:443", Weight: 1}}, Scheduling: "weighted_round_robin", ProxyProtocol: "off", Enabled: true}
	rule, e := compiler.NormalizeRule(rule)
	if e != nil {
		t.Fatal(e)
	}
	raw, _ := json.Marshal(rule)
	sum := sha256.Sum256(raw)
	if _, e = f.pool.Exec(ctx, `INSERT INTO rule_heads(tenant_id,id,desired_revision) VALUES($1,$2,1)`, f.tenant, rule.ID); e != nil {
		t.Fatal(e)
	}
	if _, e = f.pool.Exec(ctx, `INSERT INTO rule_revisions(tenant_id,rule_id,revision,payload,payload_sha256,created_by,change_class) VALUES($1,$2,1,$3,$4,$5,'rebuild')`, f.tenant, rule.ID, raw, sum[:], f.user); e != nil {
		t.Fatal(e)
	}
	rule.Enabled = false
	raw, _ = json.Marshal(rule)
	path := "/api/v1/tenants/" + f.tenant + "/rules/" + rule.ID
	call := func(tag string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("PUT", path, bytes.NewReader(raw))
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Authorization", "Bearer "+f.pair.AccessToken)
		if tag != "" {
			r.Header.Set("If-Match", tag)
		}
		w := httptest.NewRecorder()
		f.handler.ServeHTTP(w, r)
		return w
	}
	for _, bad := range []string{"", `W/"1"`, `1`, `"01"`, `"1", "2"`, `"-1"`} {
		w := call(bad)
		if w.Code != 400 && w.Code != 428 {
			t.Fatal("invalid If-Match accepted", bad, w.Code, w.Body)
		}
	}
	w := call(`"1"`)
	if w.Code != 202 || w.Header().Get("ETag") != `"2"` {
		t.Fatal(w.Code, w.Body, w.Header())
	}
	var summary struct {
		RuleID      string `json:"rule_id"`
		OperationID string `json:"operation_id"`
		Revision    int64  `json:"revision"`
		SyncStatus  string `json:"sync_status"`
	}
	json.Unmarshal(w.Body.Bytes(), &summary)
	if summary.RuleID != rule.ID || summary.OperationID == "" || summary.Revision != 2 || summary.SyncStatus != "pending" {
		t.Fatal("accepted desired state missing", w.Body)
	}
	if w = call(`"1"`); w.Code != 412 {
		t.Fatal("stale revision", w.Code, w.Body)
	}
	read := f.request(t, "GET", path, f.pair.AccessToken, "")
	if read.Code != 200 || !strings.Contains(read.Body.String(), `"desired_revision": 2`) || !strings.Contains(read.Body.String(), `"active_revision": 0`) || !strings.Contains(read.Body.String(), `"sync_status": "pending"`) {
		t.Fatal("accepted desired state confused with applied", read.Code, read.Body)
	}
	var revisions, work int
	if e = f.pool.QueryRow(ctx, `SELECT count(*) FROM rule_revisions WHERE tenant_id=$1 AND rule_id=$2`, f.tenant, rule.ID).Scan(&revisions); e != nil {
		t.Fatal(e)
	}
	if e = f.pool.QueryRow(ctx, `SELECT count(*) FROM rule_compile_requests WHERE tenant_id=$1 AND rule_id=$2`, f.tenant, rule.ID).Scan(&work); e != nil || revisions != 2 || work != 1 {
		t.Fatal("stale/invalid writes", revisions, work, e)
	}
}

func TestHTTPOperationPublicOrderCannotBeRewritten(t *testing.T) {
	f := newAPI(t)
	ids := f.operations(t, 1)
	for _, statement := range []string{
		`UPDATE operations SET public_created_at=public_created_at+interval '1 second' WHERE id=$1`,
		`UPDATE operations SET id='rewritten' WHERE id=$1`,
		`UPDATE operations SET tenant_id='rewritten' WHERE id=$1`,
	} {
		if _, e := f.pool.Exec(context.Background(), statement, ids[0]); e == nil {
			t.Fatal("public order changed", statement)
		}
	}
}
func TestHTTPRuleMutationScopeAndBusinessBodyBoundary(t *testing.T) {
	f := newAPI(t)
	id := uuid.NewString()
	path := "/api/v1/tenants/" + f.tenant + "/rules/" + id
	call := func(token, body string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("PUT", path, strings.NewReader(body))
		r.Header.Set("Authorization", "Bearer "+token)
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("If-Match", `"0"`)
		w := httptest.NewRecorder()
		f.handler.ServeHTTP(w, r)
		return w
	}
	for _, body := range []string{`{"id":"` + uuid.NewString() + `"}`, `{"id":"` + id + `","runtime_config":{}}`, `{"id":"` + id + `"} {}`, `{"id":"` + id + `","credential":"SECRET"}`} {
		w := call(f.pair.AccessToken, body)
		if w.Code != 400 || strings.Contains(w.Body.String(), "SECRET") {
			t.Fatal(w.Code, w.Body)
		}
	}
	actor := uuid.NewString()
	if _, e := f.pool.Exec(context.Background(), `INSERT INTO platform_principals(id,role) VALUES($1,'auditor')`, actor); e != nil {
		t.Fatal(e)
	}
	p, _ := auth.NewPrincipal(actor, "", auth.Auditor, "active")
	pair, e := f.tokens.Issue(context.Background(), p, "auditor")
	if e != nil {
		t.Fatal(e)
	}
	if w := call(pair.AccessToken, `{"id":"`+id+`"}`); w.Code != 403 {
		t.Fatal("auditor mutated", w.Code, w.Body)
	}
	path = "/api/v1/tenants/" + uuid.NewString() + "/rules/" + id
	if w := call(f.pair.AccessToken, `{"id":"`+id+`"}`); w.Code != 403 {
		t.Fatal("foreign scope mutated", w.Code, w.Body)
	}
}
