package httpapi_test

import (
	"bufio"
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"
	"vortex/backend/internal/auth"
	"vortex/backend/internal/httpapi"
	"vortex/backend/internal/policy"
	"vortex/backend/internal/repository"
	"vortex/backend/internal/testutil"
	"vortex/backend/internal/worker"
)

type streamEvent struct{ ID, Type, Data string }

func readEvent(t *testing.T, reader *bufio.Reader) streamEvent {
	t.Helper()
	var event streamEvent
	for {
		line, e := reader.ReadString('\n')
		if e != nil {
			t.Fatal("event stream ended", e)
		}
		line = strings.TrimSuffix(line, "\n")
		if line == "" {
			if event.Type != "" {
				return event
			}
			continue
		}
		switch {
		case strings.HasPrefix(line, "id: "):
			event.ID = strings.TrimPrefix(line, "id: ")
		case strings.HasPrefix(line, "event: "):
			event.Type = strings.TrimPrefix(line, "event: ")
		case strings.HasPrefix(line, "data: "):
			event.Data = strings.TrimPrefix(line, "data: ")
		}
	}
}
func openEvents(t *testing.T, f *apiFixture, server *httptest.Server, token, last string) *http.Response {
	t.Helper()
	request, _ := http.NewRequest("GET", server.URL+"/api/v1/tenants/"+f.tenant+"/events", nil)
	request.Header.Set("Authorization", "Bearer "+token)
	if last != "" {
		request.Header.Set("Last-Event-ID", last)
	}
	client := &http.Client{Timeout: 4 * time.Second}
	response, e := client.Do(request)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { response.Body.Close() })
	return response
}
func TestSSECommittedEventsReplayScopeAndRESTConvergence(t *testing.T) {
	f := newAPI(t)
	ids := f.operations(t, 2)
	server := httptest.NewServer(f.handler)
	defer server.Close()
	response := openEvents(t, f, server, f.pair.AccessToken, "")
	if response.StatusCode != 200 || !strings.HasPrefix(response.Header.Get("Content-Type"), "text/event-stream") {
		t.Fatal(response.Status, response.Header)
	}
	reader := bufio.NewReader(response.Body)
	first := readEvent(t, reader)
	second := readEvent(t, reader)
	response.Body.Close()
	if first.ID == "" || second.ID == first.ID || first.Type != "operation_changed" || strings.Contains(first.Data, "PRIVATE_") {
		t.Fatal(first, second)
	}
	response = openEvents(t, f, server, f.pair.AccessToken, first.ID)
	replayed := readEvent(t, bufio.NewReader(response.Body))
	response.Body.Close()
	if replayed.Type != second.Type || replayed.Data != second.Data {
		t.Fatal("durable replay", replayed, second)
	}
	// The durable signed event position is independent of delivery time.
	p, _ := auth.NewPrincipal(f.user, f.tenant, auth.Owner, "active")
	scope, _ := repository.NewTenantScope(p, f.tenant, policy.Read)
	page, e := (repository.DomainEvents{Pool: f.pool}).Replay(context.Background(), scope, 0, 100, "replay")
	if e != nil || len(page.Events) != 2 || page.Events[0].Sequence != 1 || page.Events[1].Sequence != 2 {
		t.Fatal(page, e)
	}
	other := uuid.NewString()
	if _, e = f.pool.Exec(context.Background(), `INSERT INTO users(id,tenant_id,role) VALUES($1,$2,'member')`, other, f.tenant); e != nil {
		t.Fatal(e)
	}
	principal, _ := auth.NewPrincipal(other, f.tenant, auth.Member, "active")
	pair, e := f.tokens.Issue(context.Background(), principal, "other")
	if e != nil {
		t.Fatal(e)
	}
	if bad := openEvents(t, f, server, pair.AccessToken, first.ID); bad.StatusCode != 400 {
		t.Fatal("principal cursor reused", bad.Status)
	}
	if _, e = f.pool.Exec(context.Background(), `UPDATE operations SET state='unknown' WHERE tenant_id=$1 AND id=$2`, f.tenant, ids[0]); e != nil {
		t.Fatal(e)
	}
	response = openEvents(t, f, server, f.pair.AccessToken, second.ID)
	changed := readEvent(t, bufio.NewReader(response.Body))
	response.Body.Close()
	if !strings.Contains(changed.Data, `"state":"unknown"`) {
		t.Fatal(changed)
	}
	w := f.request(t, "GET", "/api/v1/tenants/"+f.tenant+"/operations/"+ids[0], f.pair.AccessToken, "")
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"state":"unknown"`) || !strings.Contains(w.Body.String(), `"occupation_held":true`) {
		t.Fatal("REST authoritative state", w.Code, w.Body)
	}
}
func TestSSERetentionGapResyncAndReauthentication(t *testing.T) {
	f := isolatedEventAPI(t)
	f.operations(t, 2)
	server := httptest.NewServer(f.handler)
	defer server.Close()
	response := openEvents(t, f, server, f.pair.AccessToken, "")
	first := readEvent(t, bufio.NewReader(response.Body))
	response.Body.Close()
	// Advance only the internal maintenance clock: this is not a24h soak test.
	maintenance := worker.EventRetentionWorker{Pool: f.pool, Clock: func() time.Time { return time.Now().Add(48 * time.Hour) }}
	if e := maintenance.Once(context.Background()); e != nil {
		t.Fatal("retention worker", e)
	}
	var floor, count int
	if e := f.pool.QueryRow(context.Background(), `SELECT retained_after FROM domain_event_streams WHERE tenant_id=$1`, f.tenant).Scan(&floor); e != nil || floor != 2 {
		t.Fatal("retention prefix", floor, e)
	}
	if e := f.pool.QueryRow(context.Background(), `SELECT count(*) FROM domain_events WHERE tenant_id=$1`, f.tenant).Scan(&count); e != nil || count != 0 {
		t.Fatal(count, e)
	}
	var e error
	response = openEvents(t, f, server, f.pair.AccessToken, first.ID)
	event := readEvent(t, bufio.NewReader(response.Body))
	if event.Type != "resync" || !strings.Contains(event.Data, "retention_gap") {
		t.Fatal(event)
	}
	if _, e = io.ReadAll(response.Body); e != nil {
		t.Fatal("resync did not close", e)
	}
	response.Body.Close()
	f.operations(t, 1)
	response = openEvents(t, f, server, f.pair.AccessToken, event.ID)
	next := readEvent(t, bufio.NewReader(response.Body))
	if next.Type != "operation_changed" {
		t.Fatal(next)
	}
	if e = f.tokens.Revoke(context.Background(), f.pair.AccessToken, "revoke-stream"); e != nil {
		t.Fatal(e)
	}
	if _, e = io.ReadAll(response.Body); e != nil {
		t.Fatal("revoked stream did not close", e)
	}
	response.Body.Close()
	if forbidden := openEvents(t, f, server, f.pair.AccessToken, ""); forbidden.StatusCode != 401 {
		t.Fatal(forbidden.Status)
	}
}
func TestSSERestrictionSummaryContainsNoSourceSecret(t *testing.T) {
	f := newAPI(t)
	id := uuid.NewString()
	if _, e := f.pool.Exec(context.Background(), `INSERT INTO restriction_events(id,tenant_id,cause,source_id,source_version,event_kind,effective_at) VALUES($1,$2,'manual','PRIVATE_SOURCE_SECRET',1,'applied',now())`, id, f.tenant); e != nil {
		t.Fatal(e)
	}
	server := httptest.NewServer(f.handler)
	defer server.Close()
	response := openEvents(t, f, server, f.pair.AccessToken, "")
	event := readEvent(t, bufio.NewReader(response.Body))
	response.Body.Close()
	var data struct {
		ResourceID string `json:"resource_id"`
		Revision   int64  `json:"revision"`
	}
	if e := json.Unmarshal([]byte(event.Data), &data); e != nil || data.ResourceID != f.tenant || data.Revision != 1 || event.Type != "restriction_changed" || strings.Contains(event.Data, "PRIVATE_") {
		t.Fatal(event, data, e)
	}
}

func TestSSEAccessExpiryAndMalformedHeaders(t *testing.T) {
	f := newAPI(t)
	f.tokens.AccessTTL = time.Second
	p, _ := auth.NewPrincipal(f.user, f.tenant, auth.Owner, "active")
	pair, e := f.tokens.Issue(context.Background(), p, "short-stream")
	if e != nil {
		t.Fatal(e)
	}
	server := httptest.NewServer(f.handler)
	defer server.Close()
	response := openEvents(t, f, server, pair.AccessToken, "")
	if _, e = io.ReadAll(response.Body); e != nil {
		t.Fatal("expired stream did not close", e)
	}
	response.Body.Close()
	for _, header := range [][]string{{"broken"}, {""}, {"a", "b"}, {strings.Repeat("a", 4097)}} {
		request, _ := http.NewRequest("GET", server.URL+"/api/v1/tenants/"+f.tenant+"/events", nil)
		request.Header.Set("Authorization", "Bearer "+f.pair.AccessToken)
		for _, value := range header {
			request.Header.Add("Last-Event-ID", value)
		}
		result, e := (&http.Client{Timeout: 3 * time.Second}).Do(request)
		if e != nil {
			t.Fatal(e)
		}
		result.Body.Close()
		if result.StatusCode != 400 {
			t.Fatal("invalid Last-Event-ID", result.Status)
		}
	}
}

func TestSSEEventIDStableAcrossReplaySecond(t *testing.T) {
	f := newAPI(t)
	f.operations(t, 1)
	server := httptest.NewServer(f.handler)
	defer server.Close()
	response := openEvents(t, f, server, f.pair.AccessToken, "")
	first := readEvent(t, bufio.NewReader(response.Body))
	response.Body.Close()
	time.Sleep(time.Until(time.Now().Truncate(time.Second).Add(time.Second)) + 10*time.Millisecond)
	response = openEvents(t, f, server, f.pair.AccessToken, "")
	replayed := readEvent(t, bufio.NewReader(response.Body))
	response.Body.Close()
	if first.ID != replayed.ID || first.Data != replayed.Data {
		t.Fatal("same durable event changed ID", first, replayed)
	}
}

// The maintenance worker intentionally processes at most32 tenants per turn.
// Use an isolated real schema so old fixtures cannot consume this test's batch.
func isolatedEventAPI(t *testing.T) *apiFixture {
	t.Helper()
	admin := testutil.Postgres(t)
	ctx := context.Background()
	schema := "api_events_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if _, e := admin.Exec(ctx, "CREATE SCHEMA "+schema); e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() {
		if _, e := admin.Exec(ctx, "DROP SCHEMA "+schema+" CASCADE"); e != nil {
			t.Error(e)
		}
	})
	parsed, e := url.Parse(os.Getenv("PHASE6_DATABASE_URL"))
	if e != nil {
		t.Fatal(e)
	}
	query := parsed.Query()
	query.Set("search_path", schema)
	parsed.RawQuery = query.Encode()
	t.Setenv("PHASE6_DATABASE_URL", parsed.String())
	return newAPI(t)
}

func TestSSEPlatformScopeFutureCursorAndAuditFailure(t *testing.T) {
	f := newAPI(t)
	f.operations(t, 1)
	ctx := context.Background()
	actor := uuid.NewString()
	if _, e := f.pool.Exec(ctx, `INSERT INTO platform_principals(id,role) VALUES($1,'auditor')`, actor); e != nil {
		t.Fatal(e)
	}
	p, _ := auth.NewPrincipal(actor, "", auth.Auditor, "active")
	pair, e := f.tokens.Issue(ctx, p, "platform-stream")
	if e != nil {
		t.Fatal(e)
	}
	server := httptest.NewServer(f.handler)
	defer server.Close()
	response := openEvents(t, f, server, pair.AccessToken, "")
	event := readEvent(t, bufio.NewReader(response.Body))
	response.Body.Close()
	other := newAPI(t)
	if denied := openEvents(t, other, server, pair.AccessToken, event.ID); denied.StatusCode != 400 {
		t.Fatal("cross-tenant event cursor", denied.Status)
	}
	codec, _ := httpapi.NewCursorCodec([]byte(strings.Repeat("cursor-key", 4)), time.Hour)
	future, e := codec.EncodeEventCursor(httpapi.CursorScope{PrincipalID: actor, TenantID: f.tenant, Collection: "events", Direction: "forward"}, 999)
	if e != nil {
		t.Fatal(e)
	}
	if denied := openEvents(t, f, server, pair.AccessToken, future); denied.StatusCode != 400 {
		t.Fatal("future cursor", denied.Status)
	}
	name := "sse_audit_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if _, e = f.pool.Exec(ctx, `CREATE FUNCTION `+name+`() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.actor_id='`+actor+`' AND NEW.action='events.replay' THEN RAISE EXCEPTION 'PRIVATE_SQL_SECRET'; END IF; RETURN NEW; END $$; CREATE TRIGGER `+name+` BEFORE INSERT ON audit_events FOR EACH ROW EXECUTE FUNCTION `+name+`() `); e != nil {
		t.Fatal(e)
	}
	defer f.pool.Exec(ctx, `DROP TRIGGER `+name+` ON audit_events; DROP FUNCTION `+name+`() `)
	denied := openEvents(t, f, server, pair.AccessToken, "")
	body, _ := io.ReadAll(denied.Body)
	denied.Body.Close()
	if denied.StatusCode != 503 || strings.Contains(string(body), "PRIVATE_") {
		t.Fatal("audit did not close stream admission", denied.Status, string(body))
	}
}
