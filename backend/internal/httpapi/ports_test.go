package httpapi_test

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"
	"vortex/backend/internal/service"
)

func (f *apiFixture) portInput(t *testing.T) map[string]any {
	t.Helper()
	ctx := context.Background()
	g := f.group(t)
	members := []service.GroupMember{}
	for i := 0; i < 2; i++ {
		n := f.node(t, "tenant", f.ownerScope(t))
		// Preparation fixtures persist observed facts; actual Node mTLS proofs are
		// tested separately by the service suite, never fabricated by this HTTP test.
		if _, e := f.pool.Exec(ctx, `UPDATE nodes SET lifecycle='online',boot_id=$2,generation=1,version=version+1 WHERE id=$1`, n, uuid.NewString()); e != nil {
			t.Fatal(e)
		}
		members = append(members, service.GroupMember{NodeID: n, Weight: 1})
	}
	if e := (service.NodeService{Pool: f.pool}).SetMembers(ctx, f.ownerScope(t), g, 1, members, uuid.NewString()); e != nil {
		t.Fatal(e)
	}
	w := f.request(t, "GET", "/api/v1/tenants/"+f.tenant+"/node-groups/"+g+"/members", f.pair.AccessToken, "")
	var body struct {
		Digest string `json:"member_set_version"`
	}
	if e := json.Unmarshal(w.Body.Bytes(), &body); w.Code != 200 || e != nil || len(body.Digest) != 64 {
		t.Fatal(w.Code, w.Body, e)
	}
	return map[string]any{"rule_id": uuid.NewString(), "group_id": g, "member_set_version": body.Digest, "listen_address": "*", "port": 20000, "protocols": []string{"udp", "tcp"}}
}
func (f *apiFixture) preparePortHTTP(t *testing.T, in any) *httptest.ResponseRecorder {
	t.Helper()
	return f.request(t, "POST", "/api/v1/tenants/"+f.tenant+"/port-reservations", f.pair.AccessToken, string(mustJSON(t, in)))
}
func TestHTTPPortPreparePublicDigestExactAggregateAndConcurrentConflict(t *testing.T) {
	f := newAPI(t)
	in := f.portInput(t)
	responses := make(chan *httptest.ResponseRecorder, 8)
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); responses <- f.preparePortHTTP(t, in) }()
	}
	wg.Wait()
	close(responses)
	accepted, conflicts := 0, 0
	var allocation, operation string
	for w := range responses {
		if w.Code == 409 {
			conflicts++
			continue
		}
		if w.Code != 202 {
			t.Fatal(w.Code, w.Body)
		}
		accepted++
		var b struct {
			ID        string `json:"id"`
			Operation string `json:"operation_id"`
			State     string `json:"state"`
			Version   int64  `json:"version"`
			Status    string `json:"sync_status"`
		}
		if e := json.Unmarshal(w.Body.Bytes(), &b); e != nil || b.ID == "" || b.Operation == "" || b.State != "preparing" || b.Version != 1 || b.Status != "pending" {
			t.Fatal(b, e, w.Body)
		}
		allocation, operation = b.ID, b.Operation
		if w.Header().Get("ETag") != `"1"` || !strings.HasSuffix(w.Header().Get("Location"), "/"+b.Operation) {
			t.Fatal(w.Header())
		}
		read := f.request(t, "GET", w.Header().Get("Location"), f.pair.AccessToken, "")
		if read.Code != 200 || !strings.Contains(read.Body.String(), allocation) || !strings.Contains(read.Body.String(), `"state":"pending"`) {
			t.Fatal(read.Code, read.Body)
		}
		for _, v := range []*httptest.ResponseRecorder{w, read} {
			for _, secret := range []string{"PRIVATE_", "boot_id", "generation", "listen_address", "payload", "prepare_task_id", "runtime"} {
				if strings.Contains(v.Body.String(), secret) {
					t.Fatal("private field exposed", v.Body)
				}
			}
		}
	}
	if accepted != 1 || conflicts != 7 {
		t.Fatal(accepted, conflicts)
	}
	var bindings, held, proofs int
	if e := f.pool.QueryRow(context.Background(), `SELECT (SELECT count(*) FROM port_operation_bindings WHERE tenant_id=$1 AND allocation_id=$2 AND operation_id=$3),(SELECT count(*) FROM port_reservations WHERE tenant_id=$1 AND allocation_id=$2 AND state<>'released'),(SELECT count(*) FROM release_proofs WHERE tenant_id=$1 AND allocation_id=$2)`, f.tenant, allocation, operation).Scan(&bindings, &held, &proofs); e != nil || bindings != 1 || held != 4 || proofs != 0 {
		t.Fatal(bindings, held, proofs, e)
	}
	path := "/api/v1/tenants/" + f.tenant + "/port-allocations/" + allocation
	w := f.request(t, "GET", path, f.pair.AccessToken, "")
	if w.Code != 200 || w.Header().Get("ETag") != `"1"` || strings.Contains(w.Body.String(), "PRIVATE_") || strings.Contains(w.Body.String(), "listen_address") {
		t.Fatal(w.Code, w.Body, w.Header())
	}
	other := newAPI(t)
	if w := other.request(t, "GET", "/api/v1/tenants/"+other.tenant+"/port-allocations/"+allocation, other.pair.AccessToken, ""); w.Code != 404 {
		t.Fatal(w.Code, w.Body)
	}
	if w := f.request(t, "POST", path+"/proofs", f.pair.AccessToken, `{"result":"released"}`); w.Code != 404 {
		t.Fatal(w.Code, w.Body)
	}
}
func TestHTTPPortPrepareAuditRollbackValidationAndSafeAlternative(t *testing.T) {
	f := newAPI(t)
	in := f.portInput(t)
	ctx := context.Background()
	name := "http_port_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if _, e := f.pool.Exec(ctx, `CREATE FUNCTION `+name+`() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.tenant_id='`+f.tenant+`' AND NEW.action='port.prepare' THEN RAISE EXCEPTION 'PRIVATE injected port audit'; END IF; RETURN NEW; END $$; CREATE TRIGGER `+name+` BEFORE INSERT ON audit_events FOR EACH ROW EXECUTE FUNCTION `+name+`() `); e != nil {
		t.Fatal(e)
	}
	drop := func() {
		if _, e := f.pool.Exec(ctx, `DROP TRIGGER IF EXISTS `+name+` ON audit_events; DROP FUNCTION IF EXISTS `+name+`() `); e != nil {
			t.Error(e)
		}
	}
	t.Cleanup(drop)
	snapshot := func() string {
		var raw string
		if e := f.pool.QueryRow(ctx, `SELECT jsonb_build_array((SELECT count(*) FROM port_allocations WHERE tenant_id=$1),(SELECT count(*) FROM port_reservations WHERE tenant_id=$1),(SELECT count(*) FROM operations WHERE tenant_id=$1),(SELECT count(*) FROM tasks WHERE tenant_id=$1),(SELECT count(*) FROM outbox WHERE tenant_id=$1),(SELECT count(*) FROM domain_events WHERE tenant_id=$1),(SELECT count(*) FROM port_operation_bindings WHERE tenant_id=$1))::text`, f.tenant).Scan(&raw); e != nil {
			t.Fatal(e)
		}
		return raw
	}
	before := snapshot()
	if w := f.preparePortHTTP(t, in); w.Code != 503 || strings.Contains(w.Body.String(), "PRIVATE") {
		t.Fatal(w.Code, w.Body)
	}
	if after := snapshot(); after != before {
		t.Fatal("rollback leaked authority", before, after)
	}
	drop()
	for key, value := range map[string]any{"member_set_version": strings.Repeat("0", 64), "group_id": uuid.NewString()} {
		copy := map[string]any{}
		for k, v := range in {
			copy[k] = v
		}
		copy[key] = value
		want := 409
		if key == "group_id" {
			want = 404
		}
		if w := f.preparePortHTTP(t, copy); w.Code != want {
			t.Fatal(key, w.Code, w.Body)
		}
	}
	for _, patch := range []map[string]any{{"member_set_version": "bad"}, {"protocols": []string{"tcp", "tcp"}}, {"listen_address": "fe80::1%eth0"}, {"boot_id": uuid.NewString()}, {"port": -1}, {"rule_id": "bad"}} {
		copy := map[string]any{}
		for k, v := range in {
			copy[k] = v
		}
		for k, v := range patch {
			copy[k] = v
		}
		if w := f.preparePortHTTP(t, copy); w.Code != 400 {
			t.Fatal(patch, w.Code, w.Body)
		}
	}
	if w := f.preparePortHTTP(t, in); w.Code != 202 {
		t.Fatal(w.Code, w.Body)
	}
	in["rule_id"] = uuid.NewString()
	w := f.preparePortHTTP(t, in)
	if w.Code != 409 || !strings.Contains(w.Body.String(), `"suggested_port":"20001"`) || strings.Contains(w.Body.String(), "PRIVATE") {
		t.Fatal(w.Code, w.Body)
	}
	in["port"] = 0
	w = f.preparePortHTTP(t, in)
	if w.Code != 202 || !strings.Contains(w.Body.String(), `"port":20001`) {
		t.Fatal(w.Code, w.Body)
	}
}

func TestHTTPPortPrepareRejectsChangedIncarnationAndAuditor(t *testing.T) {
	f := newAPI(t)
	in := f.portInput(t)
	ctx := context.Background()
	if _, e := f.pool.Exec(ctx, `UPDATE nodes SET boot_id=$2,generation=generation+1,version=version+1 WHERE id=(SELECT node_id FROM node_group_members WHERE group_id=$1 ORDER BY node_id LIMIT 1)`, in["group_id"], uuid.NewString()); e != nil {
		t.Fatal(e)
	}
	if w := f.preparePortHTTP(t, in); w.Code != 409 {
		t.Fatal("stale public digest accepted", w.Code, w.Body)
	}
	w := f.request(t, "POST", "/api/v1/tenants/"+f.tenant+"/port-reservations", f.platformToken(t, "auditor"), string(mustJSON(t, in)))
	if w.Code != 403 {
		t.Fatal(w.Code, w.Body)
	}
	var count int
	if e := f.pool.QueryRow(ctx, `SELECT count(*) FROM port_allocations WHERE tenant_id=$1`, f.tenant).Scan(&count); e != nil || count != 0 {
		t.Fatal(count, e)
	}
}
