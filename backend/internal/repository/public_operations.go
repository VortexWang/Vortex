package repository

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"vortex/backend/db/generated"
	"vortex/backend/internal/policy"
)

type PublicOperation struct {
	ID             string    `json:"id"`
	ResourceID     string    `json:"resource_id"`
	TargetRevision int64     `json:"target_revision"`
	State          string    `json:"state"`
	OccupationHeld bool      `json:"occupation_held"`
	CreatedAt      time.Time `json:"created_at"`
}
type PublicOperations struct{ Pool *pgxpool.Pool }

func (r PublicOperations) List(ctx context.Context, s TenantScope, after time.Time, id string, limit int, request string) ([]PublicOperation, error) {
	if !s.Allows(policy.Read) || limit < 1 || limit > 101 {
		return nil, policy.ErrForbidden
	}
	result := []PublicOperation{}
	read := func(q *generated.Queries) error {
		rows, e := q.ListPublicOperations(ctx, generated.ListPublicOperationsParams{TenantID: s.TenantID(), HasCursor: id != "", AfterCreatedAt: pgtype.Timestamptz{Time: after, Valid: !after.IsZero()}, AfterID: id, PageLimit: int32(limit)})
		if e != nil {
			return e
		}
		for _, v := range rows {
			result = append(result, PublicOperation{ID: v.ID, ResourceID: v.ResourceID, TargetRevision: v.TargetRevision, State: v.State, OccupationHeld: v.OccupationHeld, CreatedAt: v.CreatedAt.Time.UTC()})
		}
		return nil
	}
	if s.Platform() {
		e := (AuditWriter{Pool: r.Pool}).WithTransaction(ctx, s, policy.Read, &Audit{Action: "operation.list", ResourceID: s.TenantID(), RequestID: request, Reason: "platform tenant operation list"}, read)
		return result, e
	}
	e := read(generated.New(r.Pool))
	return result, e
}
func (r PublicOperations) Get(ctx context.Context, s TenantScope, id, request string) (PublicOperation, error) {
	var result PublicOperation
	if !s.Allows(policy.Read) {
		return result, policy.ErrForbidden
	}
	read := func(q *generated.Queries) error {
		v, e := q.GetPublicOperation(ctx, generated.GetPublicOperationParams{TenantID: s.TenantID(), ID: id})
		if e != nil {
			return e
		}
		result = PublicOperation{ID: v.ID, ResourceID: v.ResourceID, TargetRevision: v.TargetRevision, State: v.State, OccupationHeld: v.OccupationHeld, CreatedAt: v.CreatedAt.Time.UTC()}
		return nil
	}
	if s.Platform() {
		e := (AuditWriter{Pool: r.Pool}).WithTransaction(ctx, s, policy.Read, &Audit{Action: "operation.read", ResourceID: id, RequestID: request, Reason: "platform tenant operation read"}, read)
		return result, e
	}
	e := read(generated.New(r.Pool))
	return result, e
}
