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
	"vortex/backend/internal/service"
)

func (f *apiFixture) platformToken(t *testing.T, role auth.Role) string {
	t.Helper()
	id := uuid.NewString()
	if _, e := f.pool.Exec(context.Background(), `INSERT INTO platform_principals(id,role) VALUES($1,$2)`, id, string(role)); e != nil {
		t.Fatal(e)
	}
	p, _ := auth.NewPrincipal(id, "", role, "active")
	pair, e := f.tokens.Issue(context.Background(), p, "catalog-admin")
	if e != nil {
		t.Fatal(e)
	}
	return pair.AccessToken
}
func (f *apiFixture) putCatalog(t *testing.T, id, token, tag string, body any) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest("PUT", "/api/v1/tenants/"+f.tenant+"/packages/"+id, strings.NewReader(string(mustJSON(t, body))))
	r.Header.Set("Authorization", "Bearer "+token)
	r.Header.Set("Content-Type", "application/json")
	if tag != "" {
		r.Header.Set("If-Match", tag)
	}
	w := httptest.NewRecorder()
	f.handler.ServeHTTP(w, r)
	return w
}
func TestHTTPPackageCatalogAdminOnlyExactOperationAndCreationCAS(t *testing.T) {
	f := newAPI(t)
	id := uuid.NewString()
	token := f.platformToken(t, auth.PlatformAdmin)
	body := struct {
		Name  string               `json:"name"`
		Terms service.PackageTerms `json:"terms"`
	}{strings.Repeat("名", 128), service.PackageTerms{DurationDays: 30, TrafficBytes: 1000, Capabilities: []string{"tcp", "direct"}, RuleLimit: 2, RateLimit: 8000, ConnectionLimit: 12}}
	for _, denied := range []string{f.pair.AccessToken, f.platformToken(t, auth.Operator), f.platformToken(t, auth.Auditor)} {
		w := f.putCatalog(t, id, denied, `"0"`, body)
		if w.Code != 403 {
			t.Fatal(w.Code, w.Body)
		}
	}
	if w := f.putCatalog(t, id, token, "", body); w.Code != 428 {
		t.Fatal(w.Code, w.Body)
	}
	codes := make(chan int, 8)
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); codes <- f.putCatalog(t, id, token, `"0"`, body).Code }()
	}
	wg.Wait()
	close(codes)
	ok, stale := 0, 0
	for code := range codes {
		if code == 202 {
			ok++
		} else if code == 412 {
			stale++
		} else {
			t.Fatal("creation CAS", code)
		}
	}
	if ok != 1 || stale != 7 {
		t.Fatal(ok, stale)
	}
	w := f.putCatalog(t, id, token, `"1"`, body)
	if w.Code != 202 || w.Header().Get("ETag") != `"2"` {
		t.Fatal(w.Code, w.Body)
	}
	var result struct {
		ID        string `json:"package_id"`
		Operation string `json:"operation_id"`
		Version   int64  `json:"version"`
		State     string `json:"sync_status"`
	}
	if e := json.Unmarshal(w.Body.Bytes(), &result); e != nil || result.ID != id || result.Version != 2 || result.Operation == "" || result.State != "pending" {
		t.Fatal(result, e)
	}
	op := f.request(t, "GET", w.Header().Get("Location"), token, "")
	if op.Code != 200 || !strings.Contains(op.Body.String(), id) {
		t.Fatal(op.Code, op.Body)
	}
	if w = f.putCatalog(t, id, token, `"1"`, body); w.Code != 412 {
		t.Fatal(w.Code, w.Body)
	}
	body.Terms.DurationDays = 0
	if w = f.putCatalog(t, id, token, `"2"`, body); w.Code != 400 {
		t.Fatal(w.Code, w.Body)
	}
	body.Terms.DurationDays = 30
	body.Name = strings.Repeat("名", 129)
	if w = f.putCatalog(t, id, token, `"2"`, body); w.Code != 400 {
		t.Fatal("oversize Unicode name", w.Code, w.Body)
	}
	var versions int
	if e := f.pool.QueryRow(context.Background(), `SELECT count(*) FROM package_versions WHERE package_id=$1`, id).Scan(&versions); e != nil || versions != 2 {
		t.Fatal(versions, e)
	}
}
