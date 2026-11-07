package repository

import (
	"context"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"time"
	"vortex/backend/db/generated"
	"vortex/backend/internal/policy"
)

type PortRepository struct{ Pool *pgxpool.Pool }

func (r PortRepository) Allocation(ctx context.Context, s TenantScope, id, request string) (generated.PortAllocation, error) {
	var result generated.PortAllocation
	err := (AuditWriter{Pool: r.Pool}).WithTransaction(ctx, s, policy.Read, &Audit{Action: "port.read", ResourceID: id, RequestID: request, Reason: "scoped allocation read"}, func(q *generated.Queries) error {
		var err error
		result, err = q.GetPortAllocation(ctx, generated.GetPortAllocationParams{TenantID: s.TenantID(), ID: id})
		return err
	})
	if err != nil {
		return generated.PortAllocation{}, err
	}
	return result, nil
}

// DNS consumers receive only eligible business addresses. Removing a DNS record
// never changes an allocation or acts as an execution/release confirmation.
func (r PortRepository) Serving(ctx context.Context, s TenantScope, group, request string) ([]generated.PortServingMembersRow, error) {
	var result []generated.PortServingMembersRow
	err := (AuditWriter{Pool: r.Pool}).WithTransaction(ctx, s, policy.Read, &Audit{Action: "group.serving", ResourceID: group, RequestID: request, Reason: "authorized confirmed serving set"}, func(q *generated.Queries) error {
		tenant, e := q.GetTenant(ctx, s.TenantID())
		if e != nil {
			return e
		}
		if tenant.AccountState != "active" {
			result = []generated.PortServingMembersRow{}
			return nil
		}
		active, e := q.ListCurrentRestrictions(ctx, generated.ListCurrentRestrictionsParams{TenantID: s.TenantID(), AtTime: pgtype.Timestamptz{Time: time.Now(), Valid: true}})
		if e != nil {
			return e
		}
		if len(active) > 0 {
			result = []generated.PortServingMembersRow{}
			return nil
		}
		if _, e = q.ScopedGroup(ctx, generated.ScopedGroupParams{GroupID: group, TenantID: s.TenantID(), PlatformRead: false}); e != nil {
			return e
		}
		result, e = q.PortServingMembers(ctx, generated.PortServingMembersParams{TenantID: s.TenantID(), GroupID: group})
		return e
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}
