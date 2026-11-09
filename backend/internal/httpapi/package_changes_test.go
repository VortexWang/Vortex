package httpapi_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"vortex/backend/internal/auth"
	"vortex/backend/internal/policy"
	"vortex/backend/internal/repository"
	"vortex/backend/internal/service"
)

type packageHTTPFixture struct {
	*apiFixture
	packages         service.PackageService
	grant            repository.TenantScope
	original, target string
}

func newPackageHTTP(t *testing.T) *packageHTTPFixture {
	t.Helper()
	f := newAPI(t)
	token := f.platformToken(t, auth.PlatformAdmin)
	p, e := f.tokens.Authenticate(context.Background(), token)
	if e != nil {
		t.Fatal(e)
	}
	grant, e := repository.NewTenantScope(p, f.tenant, policy.Grant)
	if e != nil {
		t.Fatal(e)
	}
	catalog, e := repository.NewTenantScope(p, f.tenant, policy.Mutate)
	if e != nil {
		t.Fatal(e)
	}
	s := service.PackageService{Pool: f.pool}
	x := &packageHTTPFixture{f, s, grant, uuid.NewString(), uuid.NewString()}
	for _, id := range []string{x.original, x.target} {
		if _, e = s.PutCatalog(context.Background(), catalog, id, "业务套餐", 0, service.PackageTerms{DurationDays: 30, TrafficBytes: 10000, Capabilities: []string{"tcp", "direct"}, RuleLimit: 2, RateLimit: 8000, ConnectionLimit: 12}, uuid.NewString()); e != nil {
			t.Fatal(e)
		}
	}
	if _, e = s.Subscribe(context.Background(), grant, x.original, 1, uuid.NewString(), uuid.NewString()); e != nil {
		t.Fatal(e)
	}
	return x
}
func (f *packageHTTPFixture) code(t *testing.T, pkg, kind string, uses int64) string {
	t.Helper()
	code := "PRIVATE_CODE_" + uuid.NewString()
	if e := f.packages.CreateEntitlement(context.Background(), f.grant, code, kind, pkg, 1, 12, 321, uses, time.Now().Add(time.Hour), f.tenant, uuid.NewString()); e != nil {
		t.Fatal(e)
	}
	return code
}
func (f *packageHTTPFixture) post(t *testing.T, path, token, tag, key string, body any) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest("POST", "/api/v1/tenants/"+f.tenant+path, strings.NewReader(string(mustJSON(t, body))))
	r.Header.Set("Authorization", "Bearer "+token)
	r.Header.Set("Content-Type", "application/json")
	if tag != "" {
		r.Header.Set("If-Match", tag)
	}
	if key != "" {
		r.Header.Set("Idempotency-Key", key)
	}
	w := httptest.NewRecorder()
	f.handler.ServeHTTP(w, r)
	return w
}
func (f *packageHTTPFixture) preview(t *testing.T, path string, body map[string]any) map[string]any {
	t.Helper()
	w := f.post(t, path, f.pair.AccessToken, "", "", body)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body)
	}
	var p map[string]any
	if e := json.Unmarshal(w.Body.Bytes(), &p); e != nil {
		t.Fatal(e)
	}
	if len(p["digest"].(string)) != 64 || p["subscription_version"] != float64(1) || strings.Contains(w.Body.String(), "PRIVATE_CODE_") {
		t.Fatal(p)
	}
	return p
}
func TestHTTPPackagePreviewConfirmationCASAndExactReplay(t *testing.T) {
	f := newPackageHTTP(t)
	at := time.Now().UTC().Add(time.Hour).Truncate(time.Microsecond)
	body := map[string]any{"package_id": f.target, "package_version": 1, "keep_rules": []string{}, "effective_at": at}
	p := f.preview(t, "/subscription/preview", body)
	body["digest"] = p["digest"]
	if w := f.post(t, "/subscription/confirm", f.pair.AccessToken, "", uuid.NewString(), body); w.Code != 428 {
		t.Fatal(w.Code, w.Body)
	}
	if w := f.post(t, "/subscription/confirm", f.pair.AccessToken, `"2"`, uuid.NewString(), body); w.Code != 412 {
		t.Fatal(w.Code, w.Body)
	}
	key := uuid.NewString()
	w := f.post(t, "/subscription/confirm", f.pair.AccessToken, `"1"`, key, body)
	if w.Code != 202 || w.Header().Get("ETag") != `"2"` {
		t.Fatal(w.Code, w.Body)
	}
	var accepted service.PackageResult
	if e := json.Unmarshal(w.Body.Bytes(), &accepted); e != nil || accepted.OperationID == "" || accepted.Version != 2 {
		t.Fatal(accepted, e)
	}
	op := f.request(t, "GET", w.Header().Get("Location"), f.pair.AccessToken, "")
	if op.Code != 200 || !strings.Contains(op.Body.String(), accepted.OperationID) {
		t.Fatal(op.Code, op.Body)
	}
	var current int64
	if e := f.pool.QueryRow(context.Background(), `SELECT version FROM subscriptions WHERE tenant_id=$1`, f.tenant).Scan(&current); e != nil || current != 1 {
		t.Fatal("early activation", current, e)
	}
	code := f.code(t, f.original, "duration", 1)
	if r := f.post(t, "/entitlement-redemptions", f.pair.AccessToken, "", uuid.NewString(), map[string]string{"code": code}); r.Code != 202 {
		t.Fatal(r.Code, r.Body)
	}
	retry := f.post(t, "/subscription/confirm", f.pair.AccessToken, `"1"`, key, body)
	if retry.Code != 202 || retry.Body.String() != w.Body.String() {
		t.Fatal("changed current version blocked exact replay", retry.Code, retry.Body)
	}
	if r := f.post(t, "/subscription/confirm", f.pair.AccessToken, `"2"`, key, body); r.Code != 409 {
		t.Fatal(r.Code, r.Body)
	}
}
func TestHTTPEntitlementConfirmationConcurrentReplayAuditAndNoSecret(t *testing.T) {
	f := newPackageHTTP(t)
	code := f.code(t, f.target, "package", 1)
	body := map[string]any{"code": code, "keep_rules": []string{}, "effective_at": time.Now().UTC().Add(time.Hour).Truncate(time.Microsecond)}
	p := f.preview(t, "/entitlement-redemptions/preview", body)
	body["digest"] = p["digest"]
	// A real audit failure must roll back the code, immutable snapshot, pending
	// change and its operation before the identical request is retried.
	name := "package_http_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	ctx := context.Background()
	if _, e := f.pool.Exec(ctx, `CREATE FUNCTION `+name+`() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.tenant_id='`+f.tenant+`' AND NEW.action='package.change.confirm' THEN RAISE EXCEPTION 'injected package HTTP audit'; END IF; RETURN NEW; END $$; CREATE TRIGGER `+name+` BEFORE INSERT ON audit_events FOR EACH ROW EXECUTE FUNCTION `+name+`() `); e != nil {
		t.Fatal(e)
	}
	drop := func() {
		if _, e := f.pool.Exec(ctx, `DROP TRIGGER IF EXISTS `+name+` ON audit_events; DROP FUNCTION IF EXISTS `+name+`() `); e != nil {
			t.Error(e)
		}
	}
	t.Cleanup(drop)
	before := f.authorityCounts(t)
	key := uuid.NewString()
	failed := f.post(t, "/entitlement-redemptions/confirm", f.pair.AccessToken, `"1"`, key, body)
	if failed.Code != 503 || strings.Contains(failed.Body.String(), "injected") {
		t.Fatal(failed.Code, failed.Body)
	}
	var uses, changes int
	h := sha256.Sum256([]byte(code))
	if e := f.pool.QueryRow(ctx, `SELECT remaining_uses FROM entitlement_codes WHERE code_hash=$1`, h[:]).Scan(&uses); e != nil || uses != 1 {
		t.Fatal(uses, e)
	}
	if e := f.pool.QueryRow(ctx, `SELECT count(*) FROM package_changes WHERE tenant_id=$1`, f.tenant).Scan(&changes); e != nil || changes != 0 {
		t.Fatal(changes, e)
	}
	if after := f.authorityCounts(t); after != before {
		t.Fatal("audit failure changed authority", before, after)
	}
	drop()
	replies := make(chan *httptest.ResponseRecorder, 8)
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			replies <- f.post(t, "/entitlement-redemptions/confirm", f.pair.AccessToken, `"1"`, key, body)
		}()
	}
	wg.Wait()
	close(replies)
	result := ""
	for w := range replies {
		if w.Code != 202 {
			t.Fatal(w.Code, w.Body)
		}
		if result != "" && result != w.Body.String() {
			t.Fatal("non-identical replay")
		}
		result = w.Body.String()
	}
	if e := f.pool.QueryRow(ctx, `SELECT remaining_uses FROM entitlement_codes WHERE code_hash=$1`, h[:]).Scan(&uses); e != nil || uses != 0 {
		t.Fatal(uses, e)
	}
	if e := f.pool.QueryRow(ctx, `SELECT count(*) FROM package_changes WHERE tenant_id=$1`, f.tenant).Scan(&changes); e != nil || changes != 1 {
		t.Fatal(changes, e)
	}
	var leaks int
	if e := f.pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM audit_events WHERE tenant_id=$1 AND row_to_json(audit_events)::text LIKE $2)+(SELECT count(*) FROM tasks WHERE tenant_id=$1 AND row_to_json(tasks)::text LIKE $2)+(SELECT count(*) FROM outbox WHERE tenant_id=$1 AND row_to_json(outbox)::text LIKE $2)+(SELECT count(*) FROM operations WHERE tenant_id=$1 AND convert_from(payload,'UTF8') LIKE $2)`, f.tenant, "%"+code+"%").Scan(&leaks); e != nil || leaks != 0 {
		t.Fatal("raw code leak", leaks, e)
	}
	body["digest"] = hex.EncodeToString(make([]byte, 32))
	if w := f.post(t, "/entitlement-redemptions/confirm", f.pair.AccessToken, `"1"`, key, body); w.Code != 409 {
		t.Fatal(w.Code, w.Body)
	}
}
func TestHTTPPackageInputErrorsAndScope(t *testing.T) {
	f := newPackageHTTP(t)
	body := map[string]any{"package_id": f.target, "package_version": 1, "keep_rules": []string{}, "effective_at": time.Now().Add(time.Hour)}
	for _, token := range []string{f.platformToken(t, auth.PlatformAdmin), f.platformToken(t, auth.Operator), f.platformToken(t, auth.Auditor)} {
		if w := f.post(t, "/subscription/preview", token, "", "", body); w.Code != 403 {
			t.Fatal(w.Code, w.Body)
		}
	}
	for _, field := range []string{"package_id", "package_version", "keep_rules", "effective_at"} {
		bad := map[string]any{}
		for k, v := range body {
			if k != field {
				bad[k] = v
			}
		}
		if w := f.post(t, "/subscription/preview", f.pair.AccessToken, "", "", bad); w.Code != 400 {
			t.Fatal(field, w.Code, w.Body)
		}
	}
	p := f.preview(t, "/subscription/preview", body)
	body["digest"] = p["digest"]
	for _, tag := range []string{"1", `W/"1"`, `"0"`, `"01"`} {
		if w := f.post(t, "/subscription/confirm", f.pair.AccessToken, tag, uuid.NewString(), body); w.Code != 400 {
			t.Fatal(tag, w.Code, w.Body)
		}
	}
	for _, key := range []string{"", "has space", strings.Repeat("a", 129)} {
		if w := f.post(t, "/subscription/confirm", f.pair.AccessToken, `"1"`, key, body); w.Code != 400 {
			t.Fatal(key, w.Code, w.Body)
		}
	}
	if w := f.post(t, "/entitlement-redemptions", f.pair.AccessToken, "", uuid.NewString(), map[string]string{"code": "unknown_code_that_is_long_enough"}); w.Code != 409 || !strings.Contains(w.Body.String(), "entitlement_not_applicable") {
		t.Fatal(w.Code, w.Body)
	}
	code := f.code(t, f.target, "package", 1)
	if w := f.post(t, "/entitlement-redemptions", f.pair.AccessToken, "", uuid.NewString(), map[string]string{"code": code}); w.Code != 409 || !strings.Contains(w.Body.String(), "package_confirmation_required") {
		t.Fatal(w.Code, w.Body)
	}
}

func (f *packageHTTPFixture) authorityCounts(t *testing.T) string {
	t.Helper()
	var counts string
	if e := f.pool.QueryRow(context.Background(), `SELECT jsonb_build_array(
 (SELECT count(*) FROM package_snapshots WHERE tenant_id=$1),
 (SELECT count(*) FROM package_changes WHERE tenant_id=$1),
 (SELECT count(*) FROM package_requests WHERE tenant_id=$1),
 (SELECT count(*) FROM operations WHERE tenant_id=$1),
 (SELECT count(*) FROM tasks WHERE tenant_id=$1),
 (SELECT count(*) FROM outbox WHERE tenant_id=$1),
 (SELECT count(*) FROM audit_events WHERE tenant_id=$1),
 (SELECT count(*) FROM domain_events WHERE tenant_id=$1),
 (SELECT row_to_json(subscriptions) FROM subscriptions WHERE tenant_id=$1)
 )::text`, f.tenant).Scan(&counts); e != nil {
		t.Fatal(e)
	}
	return counts
}
func TestHTTPPackageLargeBodyStillValidatesDomainAndStrictHeaders(t *testing.T) {
	f := newPackageHTTP(t)
	ids := make([]string, 10000)
	for i := range ids {
		ids[i] = uuid.NewString()
	}
	body := map[string]any{"package_id": f.target, "package_version": 1, "keep_rules": ids, "effective_at": time.Now().Add(time.Hour)}
	// A full 10000-entry request reaches the real domain check (the rules do not
	// exist), rather than failing at the ordinary 16KiB parser limit.
	if w := f.post(t, "/subscription/preview", f.pair.AccessToken, "", "", body); w.Code != 409 {
		t.Fatal(w.Code, w.Body)
	}
	body["keep_rules"] = append(ids, uuid.NewString())
	if w := f.post(t, "/subscription/preview", f.pair.AccessToken, "", "", body); w.Code != 400 {
		t.Fatal(w.Code, w.Body)
	}
	body["keep_rules"] = []string{ids[0], ids[0]}
	if w := f.post(t, "/subscription/preview", f.pair.AccessToken, "", "", body); w.Code != 400 {
		t.Fatal(w.Code, w.Body)
	}
	body["keep_rules"] = []string{}
	foreign := newAPI(t)
	if w := f.post(t, "/subscription/preview", foreign.pair.AccessToken, "", "", body); w.Code != 403 {
		t.Fatal(w.Code, w.Body)
	}
	body["digest"] = strings.Repeat("a", 64)
	for _, header := range []string{"If-Match", "Idempotency-Key"} {
		r := httptest.NewRequest("POST", "/api/v1/tenants/"+f.tenant+"/subscription/confirm", strings.NewReader(string(mustJSON(t, body))))
		r.Header.Set("Authorization", "Bearer "+f.pair.AccessToken)
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("If-Match", `"1"`)
		r.Header.Set("Idempotency-Key", uuid.NewString())
		r.Header.Add(header, r.Header.Get(header))
		w := httptest.NewRecorder()
		f.handler.ServeHTTP(w, r)
		if w.Code != 400 {
			t.Fatal(header, w.Code, w.Body)
		}
	}
}

func TestHTTPPackageConfirmationExactReplayAfterEffectiveTime(t *testing.T) {
	f := newPackageHTTP(t)
	at := time.Now().UTC().Add(time.Second).Truncate(time.Microsecond)
	body := map[string]any{"package_id": f.target, "package_version": 1, "keep_rules": []string{}, "effective_at": at}
	p := f.preview(t, "/subscription/preview", body)
	body["digest"] = p["digest"]
	key := uuid.NewString()
	first := f.post(t, "/subscription/confirm", f.pair.AccessToken, `"1"`, key, body)
	if first.Code != 202 {
		t.Fatal(first.Code, first.Body)
	}
	if wait := time.Until(at); wait > 0 {
		time.Sleep(wait + time.Millisecond)
	}
	replay := f.post(t, "/subscription/confirm", f.pair.AccessToken, `"1"`, key, body)
	if replay.Code != 202 || replay.Body.String() != first.Body.String() {
		t.Fatal("past effective time blocked exact retry", replay.Code, replay.Body)
	}
	if fresh := f.post(t, "/subscription/confirm", f.pair.AccessToken, `"1"`, uuid.NewString(), body); fresh.Code != 409 {
		t.Fatal("fresh past request accepted", fresh.Code, fresh.Body)
	}
}
