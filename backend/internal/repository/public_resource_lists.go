package repository

import (
	"context"
	"encoding/json"
	"github.com/jackc/pgx/v5/pgtype"
	"time"
	"vortex/backend/db/generated"
	"vortex/backend/internal/policy"
)

type PublicResourceRow struct {
	Body      json.RawMessage
	CreatedAt time.Time
	ID        string
}

func PublicCollection(collection string) bool {
	switch collection {
	case "tenants", "nodes", "node-groups", "rules", "packages", "subscriptions", "port-allocations", "sources", "restrictions":
		return true
	}
	return false
}

// List applies authorization in SQL; registry membership never grants access.
// Global tenant enumeration is possible only from an authenticated platform scope.
func (r PublicResources) List(ctx context.Context, s TenantScope, collection string, after time.Time, id string, limit int, request string) ([]PublicResourceRow, error) {
	if !s.Allows(policy.Read) || !PublicCollection(collection) || limit < 1 || limit > 101 {
		return nil, policy.ErrForbidden
	}
	result := []PublicResourceRow{}
	read := func(q *generated.Queries) error {
		rows, e := q.ListPublicResources(ctx, generated.ListPublicResourcesParams{TenantID: s.TenantID(), Collection: collection, AllTenants: collection == "tenants" && s.Platform(), HasCursor: id != "", AfterCreatedAt: pgtype.Timestamptz{Time: after, Valid: !after.IsZero()}, AfterID: id, PageLimit: int32(limit)})
		if e != nil {
			return e
		}
		for _, v := range rows {
			if s.Platform() && collection == "tenants" { // audit each enumerated tenant before returning any result
				if e = q.InsertAudit(ctx, generated.InsertAuditParams{TenantID: v.TargetTenantID, ActorID: s.ActorID(), Action: "tenant.list.read", ResourceID: v.TargetTenantID, RequestID: request, Reason: "platform public tenant list", BeforeSummary: []byte(`{}`), AfterSummary: []byte(`{}`)}); e != nil {
					return e
				}
			}
			result = append(result, PublicResourceRow{Body: json.RawMessage(v.Body), CreatedAt: v.CreatedAt.Time.UTC(), ID: v.SortID})
		}
		return nil
	}
	if s.Platform() {
		e := (AuditWriter{Pool: r.Pool}).WithTransaction(ctx, s, policy.Read, &Audit{Action: collection + ".list", ResourceID: s.TenantID(), RequestID: request, Reason: "platform scoped public business list"}, read)
		return result, e
	}
	e := read(generated.New(r.Pool))
	return result, e
}
