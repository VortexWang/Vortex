package service_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"vortex/agent/store"
	"vortex/backend/db/generated"
	"vortex/backend/internal/auth"
	"vortex/backend/internal/httpapi"
	"vortex/backend/internal/policy"
	"vortex/backend/internal/repository"
	"vortex/backend/internal/service"
	"vortex/backend/internal/worker"
)

func recoveryHTTP(t *testing.T, f *portFixture, role auth.Role) (http.Handler, string, service.TenantScope) {
	t.Helper()
	actor := uuid.NewString()
	if _, e := f.pool.Exec(context.Background(), `INSERT INTO platform_principals(id,role) VALUES($1,$2)`, actor, string(role)); e != nil {
		t.Fatal(e)
	}
	p, e := auth.NewPrincipal(actor, "", role, "active")
	if e != nil {
		t.Fatal(e)
	}
	tokens := &auth.Tokens{Store: repository.TokenRepository{Pool: f.pool}, AccessTTL: time.Minute, RefreshTTL: time.Hour}
	pair, e := tokens.Issue(context.Background(), p, uuid.NewString())
	if e != nil {
		t.Fatal(e)
	}
	codec, e := httpapi.NewCursorCodec([]byte(strings.Repeat("recovery-api-test", 3)), time.Hour)
	if e != nil {
		t.Fatal(e)
	}
	h, e := httpapi.NewRouter(httpapi.Config{Pool: f.pool, Tokens: tokens, Cursors: codec})
	if e != nil {
		t.Fatal(e)
	}
	action := policy.Read
	if role == auth.PlatformAdmin {
		action = policy.Grant
	}
	scope, e := repository.NewTenantScope(p, f.tenant, action)
	if e != nil {
		t.Fatal(e)
	}
	return h, pair.AccessToken, scope
}

func recoverySafe(t *testing.T, raw string, id, resource, stage string, withOperation bool) {
	t.Helper()
	var body map[string]any
	if e := json.Unmarshal([]byte(raw), &body); e != nil {
		t.Fatal(e, raw)
	}
	want := 4
	if withOperation {
		want = 5
	}
	if len(body) != want || body["id"] != id || body["resource_id"] != resource || body["stage"] != stage {
		t.Fatal("unsafe or inaccurate recovery projection", raw)
	}
	for k := range body {
		switch k {
		case "id", "resource_id", "mode", "stage", "operation_id":
		default:
			t.Fatal("internal field", k)
		}
	}
}

func TestRecoveryHTTPDurableAdvanceWaitsForFullProofAndRetainsDebt(t *testing.T) {
	isolatePackageAuthority(t)
	x, _, hs, a, r := sameStoreFixture(t)
	ctx := context.Background()
	q := generated.New(x.f.pool)
	h, token, admin := recoveryHTTP(t, x.f, auth.PlatformAdmin)
	approval, e := x.s.PrepareHubSameStoreRecovery(ctx, admin, r, uuid.NewString())
	if e != nil {
		t.Fatal(e)
	}
	base := "/api/v1/tenants/" + x.f.tenant + "/hub-recoveries/same-store/" + r.ID
	w := servingCall(h, token, "GET", base, "", "")
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body)
	}
	recoverySafe(t, w.Body.String(), r.ID, r.HubID, "awaiting_manifest", true)
	var read map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &read)
	if read["operation_id"] != approval.OperationID {
		t.Fatal("non-exact approval", w.Body)
	}
	financial := []string{"reservations", "ledger_projections", "ledger_events", "hub_credit_grants", "settlements", "share_chains", "share_generations", "hub_funded_allocations"}
	held := authoritySnapshot(t, x.f.pool, x.f.tenant, financial)
	key := uuid.NewString()
	ids := make(chan string, 8)
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			w := servingCall(h, token, "POST", base+"/advances", key, `{}`)
			if w.Code != 202 {
				t.Error(w.Code, w.Body)
				return
			}
			var b struct {
				OperationID string `json:"operation_id"`
			}
			if json.Unmarshal(w.Body.Bytes(), &b) != nil {
				t.Error(w.Body)
				return
			}
			if w.Header().Get("Location") != "/api/v1/tenants/"+x.f.tenant+"/operations/"+b.OperationID {
				t.Error("location", w.Header())
			}
			ids <- b.OperationID
		})
	}
	wg.Wait()
	close(ids)
	id := ""
	for got := range ids {
		if id != "" && id != got {
			t.Fatal("duplicate operation")
		}
		id = got
	}
	if id == "" {
		t.Fatal("no job")
	}
	for _, table := range []string{"tasks", "outbox"} {
		var n int
		if e = x.f.pool.QueryRow(ctx, `SELECT count(*) FROM `+table+` WHERE tenant_id=$1 AND operation_id=$2`, x.f.tenant, id).Scan(&n); e != nil || n != 1 {
			t.Fatal(table, n, e)
		}
	}
	consumer := worker.RecoveryAdvanceWorker{Service: x.s}
	run := func() {
		t.Helper()
		worked, e := consumer.Once(ctx)
		if e != nil || !worked {
			t.Fatal("consumer", worked, e)
		}
	}
	due := func() {
		t.Helper()
		if _, e = x.f.pool.Exec(ctx, `UPDATE tasks SET lease_until=now()-interval '1 second' WHERE tenant_id=$1 AND operation_id=$2`, x.f.tenant, id); e != nil {
			t.Fatal(e)
		}
	}
	run()
	op, e := q.GetOperation(ctx, generated.GetOperationParams{TenantID: x.f.tenant, ID: id})
	if e != nil || op.State != "pending" || !op.OccupationHeld {
		t.Fatal("waiting fabricated completion", op, e)
	}
	if worked, e := consumer.Once(ctx); e != nil || worked {
		t.Fatal("ignored bounded retry delay", worked, e)
	}
	_, raw, records := sameStoreManifest(t, x, hs, r)
	retainSameStore(t, x, r, records)
	if _, e = x.s.VerifyHubSameStorePrefix(x.f.contexts[r.HubID], r.HubID, r.ToBootID, r.ID, raw, uuid.NewString()); e != nil {
		t.Fatal(e)
	}
	due()
	run()
	fence, e := q.GetNodeRecoveryCreditFenceByAllocation(ctx, generated.GetNodeRecoveryCreditFenceByAllocationParams{TenantID: x.f.tenant, RecoveryID: r.ID, AllocationID: a.AllocationID})
	if e != nil || fence.CreatedBy != admin.ActorID() {
		t.Fatal(fence, e)
	}
	if _, e = q.GetNodeRecoverySourcesByAllocation(ctx, generated.GetNodeRecoverySourcesByAllocationParams{TenantID: x.f.tenant, RecoveryID: r.ID, AllocationID: a.AllocationID}); e == nil {
		t.Fatal("root before real fence")
	}
	ns, e := store.Open(t.TempDir()+"/node.db", "agent")
	if e != nil {
		t.Fatal(e)
	}
	defer ns.Close()
	original, e := q.GetNodeSourceProvision(ctx, generated.GetNodeSourceProvisionParams{TenantID: x.f.tenant, AllocationID: a.AllocationID})
	if e != nil {
		t.Fatal(e)
	}
	p, e := store.DecodeSourceProvision(original.Payload)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = ns.ApplySourceProvision(ctx, a.NodeID, a.BootID, shareBackendID, original.Payload, store.IssuerTime{Earliest: p.IssuedAt, Latest: p.IssuedAt, Trusted: true}); e != nil {
		t.Fatal(e)
	}
	recordRecoveryFence(t, x, fence, ns)
	due()
	run()
	roots, e := q.GetNodeRecoverySourcesByAllocation(ctx, generated.GetNodeRecoverySourcesByAllocationParams{TenantID: x.f.tenant, RecoveryID: r.ID, AllocationID: a.AllocationID})
	if e != nil {
		t.Fatal(e)
	}
	receipt, e := ns.ApplySourceProvision(ctx, a.NodeID, a.BootID, shareBackendID, roots.Payload, store.IssuerTime{Earliest: x.now, Latest: x.now, Trusted: true})
	if e != nil {
		t.Fatal(e)
	}
	if e = x.s.RecordNodeRecoverySources(x.f.contexts[a.NodeID], x.f.scope, receipt, uuid.NewString()); e != nil {
		t.Fatal(e)
	}
	due()
	run()
	op, e = q.GetOperation(ctx, generated.GetOperationParams{TenantID: x.f.tenant, ID: id})
	if e != nil || op.State != "succeeded" || !op.OccupationHeld {
		t.Fatal("admission is not financial release", op, e)
	}
	w = servingCall(h, token, "GET", base, "", "")
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body)
	}
	recoverySafe(t, w.Body.String(), r.ID, r.HubID, "admission_recorded", true)
	if !bytes.Equal(held, authoritySnapshot(t, x.f.pool, x.f.tenant, financial)) {
		t.Fatal("recovery changed original financial authority")
	}
	tables := []string{"operations", "tasks", "outbox", "audit_events", "domain_events"}
	before := authoritySnapshot(t, x.f.pool, x.f.tenant, tables)
	if worked, e := x.s.ProcessRecoveryAdvance(ctx, x.f.tenant, id, uuid.NewString()); e != nil || worked {
		t.Fatal("terminal replay", worked, e)
	}
	if !bytes.Equal(before, authoritySnapshot(t, x.f.pool, x.f.tenant, tables)) {
		t.Fatal("terminal replay wrote audit/task")
	}
}

func TestRecoveryHTTPStrictScopeReplayAndAuditRollback(t *testing.T) {
	isolatePackageAuthority(t)
	x, _, _, _, r := sameStoreFixture(t)
	ctx := context.Background()
	h, token, admin := recoveryHTTP(t, x.f, auth.PlatformAdmin)
	if _, e := x.s.PrepareHubSameStoreRecovery(ctx, admin, r, uuid.NewString()); e != nil {
		t.Fatal(e)
	}
	base := "/api/v1/tenants/" + x.f.tenant + "/hub-recoveries/same-store/" + r.ID
	for _, body := range []string{`null`, `{"backend_id":"` + shareBackendID + `"}`, `{"epoch":8}`, `{} {}`, `[]`} {
		w := servingCall(h, token, "POST", base+"/advances", uuid.NewString(), body)
		if w.Code != 400 {
			t.Fatal(body, w.Code, w.Body)
		}
	}
	for _, role := range []auth.Role{auth.Operator, auth.Auditor} {
		rh, rt, _ := recoveryHTTP(t, x.f, role)
		w := servingCall(rh, rt, "GET", base, "", "")
		if w.Code != 200 {
			t.Fatal(role, w.Code, w.Body)
		}
		w = servingCall(rh, rt, "POST", base+"/advances", uuid.NewString(), `{}`)
		if w.Code != 403 {
			t.Fatal(role, w.Code, w.Body)
		}
	}
	if _, e := x.f.pool.Exec(ctx, `INSERT INTO users(id,tenant_id,role) VALUES($1,$2,'owner') ON CONFLICT(id) DO NOTHING`, x.f.scope.ActorID(), x.f.tenant); e != nil {
		t.Fatal(e)
	}
	owner, ot := servingHTTP(t, x.f)
	for _, method := range []string{"GET", "POST"} {
		path := base
		body := ""
		if method == "POST" {
			path += "/advances"
			body = `{}`
		}
		w := servingCall(owner, ot, method, path, uuid.NewString(), body)
		if w.Code != 403 {
			t.Fatal(method, w.Code, w.Body)
		}
	}
	other := seedTenant(t, x.f.pool)
	w := servingCall(h, token, "GET", strings.Replace(base, x.f.tenant, other, 1), "", "")
	if w.Code != 404 {
		t.Fatal("cross tenant", w.Code, w.Body)
	}
	tables := []string{"operations", "tasks", "outbox", "audit_events", "domain_events"}
	request := uuid.NewString()
	failAudit(t, x.f.pool, request)
	before := authoritySnapshot(t, x.f.pool, x.f.tenant, tables)
	if _, e := x.s.RequestRecoveryAdvance(ctx, admin, r.ID, uuid.NewString(), request); e == nil {
		t.Fatal("queue bypassed audit")
	}
	if !bytes.Equal(before, authoritySnapshot(t, x.f.pool, x.f.tenant, tables)) {
		t.Fatal("partial queue")
	}
	request = uuid.NewString()
	failAudit(t, x.f.pool, request)
	if _, e := (repository.PublicResources{Pool: x.f.pool}).Recovery(ctx, admin, "hub-same-store", r.ID, request); e == nil {
		t.Fatal("platform read bypassed audit")
	}
	key := uuid.NewString()
	op, e := x.s.RequestRecoveryAdvance(ctx, admin, r.ID, key, uuid.NewString())
	if e != nil {
		t.Fatal(e)
	}
	_, _, second := recoveryHTTP(t, x.f, auth.PlatformAdmin)
	if _, e = x.s.RequestRecoveryAdvance(ctx, second, r.ID, key, uuid.NewString()); !errors.Is(e, service.ErrConflict) {
		t.Fatal("key rebound actor", e)
	}
	request = uuid.NewString()
	failAudit(t, x.f.pool, request)
	before = authoritySnapshot(t, x.f.pool, x.f.tenant, tables)
	if worked, e := x.s.ProcessRecoveryAdvance(ctx, x.f.tenant, op.ID, request); e == nil || worked {
		t.Fatal("lease bypassed audit", worked, e)
	}
	if !bytes.Equal(before, authoritySnapshot(t, x.f.pool, x.f.tenant, tables)) {
		t.Fatal("partial lease")
	}
	// Coordinator errors are recorded as unknown, and never polled again.
	x.s.Clock = nil
	if worked, e := x.s.ProcessRecoveryAdvance(ctx, x.f.tenant, op.ID, uuid.NewString()); e != nil || !worked {
		t.Fatal("unknown projection", worked, e)
	}
	current, e := generated.New(x.f.pool).GetOperation(ctx, generated.GetOperationParams{TenantID: x.f.tenant, ID: op.ID})
	if e != nil || current.State != "unknown" || !current.OccupationHeld {
		t.Fatal(current, e)
	}
	if worked, e := (worker.RecoveryAdvanceWorker{Service: x.s}).Once(ctx); e != nil || worked {
		t.Fatal("unknown was repolled", worked, e)
	}
	if worked, e := x.s.ProcessRecoveryAdvance(ctx, x.f.tenant, op.ID, uuid.NewString()); !errors.Is(e, service.ErrConflict) || worked {
		t.Fatal("explicit unknown retry", worked, e)
	}
	op, e = x.s.RequestRecoveryAdvance(ctx, admin, r.ID, uuid.NewString(), uuid.NewString())
	if e != nil {
		t.Fatal(e)
	}
	if _, e = x.f.pool.Exec(ctx, `UPDATE platform_principals SET state='suspended' WHERE id=$1`, admin.ActorID()); e != nil {
		t.Fatal(e)
	}
	if worked, e := (worker.RecoveryAdvanceWorker{Service: x.s}).Once(ctx); e != nil || worked {
		t.Fatal("revoked actor consumed", worked, e)
	}
	if worked, e := x.s.ProcessRecoveryAdvance(ctx, x.f.tenant, op.ID, uuid.NewString()); !errors.Is(e, service.ErrScope) || worked {
		t.Fatal("revoked actor direct", worked, e)
	}
}

func TestRecoveryHTTPNodeAndColdStagesAreOnlyPersistedBusinessFacts(t *testing.T) {
	t.Run("node", func(t *testing.T) {
		x, in, _ := nodeColdFixture(t)
		ctx := context.Background()
		h, token, admin := recoveryHTTP(t, x.f, auth.PlatformAdmin)
		x.now = x.now.Add(time.Hour)
		p, e := x.s.PrepareNodeBootRecovery(ctx, admin, in, uuid.NewString())
		if e != nil {
			t.Fatal(e)
		}
		path := "/api/v1/tenants/" + x.f.tenant + "/node-recoveries/" + p.ID
		w := servingCall(h, token, "GET", path, "", "")
		if w.Code != 200 {
			t.Fatal(w.Code, w.Body)
		}
		recoverySafe(t, w.Body.String(), in.ID, in.NodeID, "awaiting_retirement_receipt", true)
	})
	t.Run("cold_hub", func(t *testing.T) {
		x, in, _ := fundedHubFixture(t)
		ctx := context.Background()
		h, token, admin := recoveryHTTP(t, x.f, auth.PlatformAdmin)
		r := coldRecoveryInput(t, x, in.HubID, in.HubBootID)
		x.now = x.now.Add(time.Hour)
		if _, e := x.s.RecoverColdHubBoot(ctx, admin, r, uuid.NewString()); e != nil {
			t.Fatal(e)
		}
		path := "/api/v1/tenants/" + x.f.tenant + "/hub-recoveries/cold/" + r.ID
		w := servingCall(h, token, "GET", path, "", "")
		if w.Code != 200 {
			t.Fatal(w.Code, w.Body)
		}
		recoverySafe(t, w.Body.String(), r.ID, r.HubID, "expiry_approved", false)
	})
}

func TestRecoveryHTTPResultAuditFailureRetainsLeaseAndResumesExactly(t *testing.T) {
	isolatePackageAuthority(t)
	x, _, _, _, r := sameStoreFixture(t)
	ctx := context.Background()
	_, _, admin := recoveryHTTP(t, x.f, auth.PlatformAdmin)
	if _, e := x.s.PrepareHubSameStoreRecovery(ctx, admin, r, uuid.NewString()); e != nil {
		t.Fatal(e)
	}
	op, e := x.s.RequestRecoveryAdvance(ctx, admin, r.ID, uuid.NewString(), uuid.NewString())
	if e != nil {
		t.Fatal(e)
	}
	request := uuid.NewString()
	failAudit(t, x.f.pool, request+"/result")
	financial := []string{"reservations", "ledger_projections", "ledger_events", "hub_credit_grants", "settlements", "share_chains", "share_generations", "hub_funded_allocations"}
	held := authoritySnapshot(t, x.f.pool, x.f.tenant, financial)
	if worked, e := x.s.ProcessRecoveryAdvance(ctx, x.f.tenant, op.ID, request); e == nil || worked {
		t.Fatal("result bypassed audit", worked, e)
	}
	var state string
	var heldFlag bool
	var delivered bool
	if e = x.f.pool.QueryRow(ctx, `SELECT o.state,o.occupation_held,b.delivered_at IS NOT NULL FROM operations o JOIN outbox b ON b.tenant_id=o.tenant_id AND b.operation_id=o.id WHERE o.tenant_id=$1 AND o.id=$2`, x.f.tenant, op.ID).Scan(&state, &heldFlag, &delivered); e != nil || state != "leased" || !heldFlag || delivered {
		t.Fatal("partial result", state, heldFlag, delivered, e)
	}
	var count int
	if e = x.f.pool.QueryRow(ctx, `SELECT count(*) FROM audit_events WHERE request_id=$1`, request+"/result").Scan(&count); e != nil || count != 0 {
		t.Fatal("partial result audit", count, e)
	}
	if _, e = x.f.pool.Exec(ctx, `UPDATE tasks SET lease_until=now()-interval '1 second' WHERE tenant_id=$1 AND operation_id=$2`, x.f.tenant, op.ID); e != nil {
		t.Fatal(e)
	}
	var wg sync.WaitGroup
	done := make(chan bool, 8)
	for range 8 {
		wg.Go(func() {
			worked, e := x.s.ProcessRecoveryAdvance(ctx, x.f.tenant, op.ID, uuid.NewString())
			if e != nil {
				t.Error(e)
			}
			done <- worked
		})
	}
	wg.Wait()
	close(done)
	count = 0
	for worked := range done {
		if worked {
			count++
		}
	}
	if count != 1 {
		t.Fatal("concurrent lease effects", count)
	}
	if !bytes.Equal(held, authoritySnapshot(t, x.f.pool, x.f.tenant, financial)) {
		t.Fatal("result failure released debt")
	}
}
