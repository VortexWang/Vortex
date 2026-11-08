package httpapi_test

import (
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"strings"
	"testing"
	"vortex/backend/db/generated"
	"vortex/backend/internal/policy"
	"vortex/backend/internal/repository"
	"vortex/backend/internal/service"
)

func TestHTTPPublicDomainOperationDNSUsesGroupAndRedactsEnvelope(t *testing.T) {
	f := newAPI(t)
	ctx := context.Background()
	group := uuid.NewString()
	var id string
	e := (repository.AuditWriter{Pool: f.pool}).WithTransaction(ctx, f.ownerScope(t), policy.Mutate, &repository.Audit{Action: "test.dns.enqueue", ResourceID: group, RequestID: "dns-public", Reason: "exercise actual DNS enqueue adapter"}, func(q *generated.Queries) error {
		op, e := repository.EnqueueDNS(ctx, q, f.ownerScope(t), uuid.NewString(), service.DNSDesired{Action: "dns.reconcile", GroupID: group, AccessID: "PRIVATE_ACCESS", ZoneID: "PRIVATE_ZONE", Name: "business.example.test", Addresses: []string{"PRIVATE_ADDRESS"}})
		id = op.ID
		return e
	})
	if e != nil {
		t.Fatal(e)
	}
	base := "/api/v1/tenants/" + f.tenant + "/operations"
	w := f.request(t, "GET", base+"/"+id, f.pair.AccessToken, "")
	if w.Code != 200 || strings.Contains(w.Body.String(), "PRIVATE_") {
		t.Fatal(w.Code, w.Body)
	}
	var result repository.PublicOperation
	if e = json.Unmarshal(w.Body.Bytes(), &result); e != nil || result.ResourceID != group || result.TargetRevision != 1 {
		t.Fatal(result, e)
	}
	page := f.businessPage(t, base, f.pair.AccessToken)
	if len(page.Items) != 1 || page.Items[0]["resource_id"] != group || strings.Contains(string(mustJSON(t, page)), "PRIVATE_") {
		t.Fatal(page)
	}
}
