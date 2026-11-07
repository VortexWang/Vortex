package repository

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
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
		var e error
		result, e = ServingMembers(ctx, q, s, group)
		return e
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

func ServingMembers(ctx context.Context, q *generated.Queries, s TenantScope, group string) ([]generated.PortServingMembersRow, error) {
	tenant, e := q.LockTenant(ctx, s.TenantID())
	if e != nil {
		return nil, e
	}
	if tenant.AccountState != "active" {
		return []generated.PortServingMembersRow{}, nil
	}
	active, e := q.ListCurrentRestrictions(ctx, generated.ListCurrentRestrictionsParams{TenantID: s.TenantID(), AtTime: pgtype.Timestamptz{Time: time.Now(), Valid: true}})
	if e != nil {
		return nil, e
	}
	if len(active) > 0 {
		return []generated.PortServingMembersRow{}, nil
	}
	g, e := q.ScopedGroup(ctx, generated.ScopedGroupParams{GroupID: group, TenantID: s.TenantID(), PlatformRead: false})
	if e != nil {
		return nil, e
	}
	if g.Purpose != "entry" {
		return nil, policy.ErrForbidden
	}
	members, e := q.GroupMembers(ctx, group)
	if e != nil {
		return nil, e
	}
	raw, e := json.Marshal(struct {
		Version int64
		Members []generated.GroupMembersRow
	}{g.MemberVersion, members})
	if e != nil {
		return nil, e
	}
	sum := sha256.Sum256(raw)
	return q.PortServingMembers(ctx, generated.PortServingMembersParams{TenantID: s.TenantID(), GroupID: group, MemberSetVersion: hex.EncodeToString(sum[:])})
}
