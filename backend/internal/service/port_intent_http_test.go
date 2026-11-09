package service_test

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"vortex/backend/internal/auth"
	"vortex/backend/internal/httpapi"
	"vortex/backend/internal/repository"
)

func TestPortIntentHTTPCommitUsesActualAuthenticatedStoredProofs(t *testing.T) {
	f := newPortFixture(t)
	ctx := context.Background()
	a := f.prepare(t, 20000)
	for _, n := range f.nodes {
		p := f.proof(t, a, n, "prepare")
		if e := f.ports.RecordPrepareProof(f.contexts[n], f.scope, p, uuid.NewString()); e != nil {
			t.Fatal(e)
		}
	}
	// The service fixture carries a scoped principal without a users row.
	// Public token issuance correctly requires the current persisted identity.
	if _, e := f.pool.Exec(ctx, `INSERT INTO users(id,tenant_id,role) VALUES($1,$2,'owner')`, f.scope.ActorID(), f.tenant); e != nil {
		t.Fatal(e)
	}
	tokens := &auth.Tokens{Store: repository.TokenRepository{Pool: f.pool}, AccessTTL: time.Minute, RefreshTTL: time.Hour}
	principal, e := auth.NewPrincipal(f.scope.ActorID(), f.tenant, auth.Owner, "active")
	if e != nil {
		t.Fatal(e)
	}
	pair, e := tokens.Issue(ctx, principal, uuid.NewString())
	if e != nil {
		t.Fatal(e)
	}
	cursors, e := httpapi.NewCursorCodec([]byte(strings.Repeat("port-http-test", 3)), time.Hour)
	if e != nil {
		t.Fatal(e)
	}
	handler, e := httpapi.NewRouter(httpapi.Config{Pool: f.pool, Tokens: tokens, Cursors: cursors})
	if e != nil {
		t.Fatal(e)
	}
	call := func(tag string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("POST", "/api/v1/tenants/"+f.tenant+"/port-allocations/"+a.ID+"/commit", strings.NewReader(`{}`))
		r.Header.Set("Authorization", "Bearer "+pair.AccessToken)
		r.Header.Set("If-Match", tag)
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w
	}
	w := call(`"1"`)
	var body struct {
		ID        string `json:"id"`
		Operation string `json:"operation_id"`
		Version   int64  `json:"version"`
		State     string `json:"state"`
		Status    string `json:"sync_status"`
	}
	if e = json.Unmarshal(w.Body.Bytes(), &body); e != nil || w.Code != 202 || body.ID != a.ID || body.Operation == "" || body.Version != 2 || body.State != "committed" || body.Status != "succeeded" {
		t.Fatal(w.Code, w.Body, e)
	}
	if w.Header().Get("ETag") != `"2"` || !strings.HasSuffix(w.Header().Get("Location"), "/"+body.Operation) {
		t.Fatal(w.Header())
	}
	for _, key := range []string{"boot_id", "generation", "listen_address", "payload", "lease_token", "NodeID"} {
		if strings.Contains(w.Body.String(), key) {
			t.Fatal("internal proof field exposed", w.Body)
		}
	}
	var held int
	var operation string
	if e = f.pool.QueryRow(ctx, `SELECT (SELECT operation_id FROM port_operation_bindings WHERE tenant_id=$1 AND allocation_id=$2 AND action='commit'),(SELECT count(*) FROM port_reservations WHERE tenant_id=$1 AND allocation_id=$2 AND state<>'released')`, f.tenant, a.ID).Scan(&operation, &held); e != nil || operation != body.Operation || held != 4 {
		t.Fatal(operation, held, e)
	}
	if w = call(`"1"`); w.Code != 412 {
		t.Fatal(w.Code, w.Body)
	}
	if w = call(`"2"`); w.Code != 409 {
		t.Fatal(w.Code, w.Body)
	}
}
