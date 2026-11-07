package repository

import (
	"context"
	"github.com/jackc/pgx/v5/pgxpool"
	"vortex/backend/db/generated"
	"vortex/backend/internal/policy"
)

type UsageRepository struct{ Pool *pgxpool.Pool }

func (r UsageRepository) Reservation(ctx context.Context, scope TenantScope, id, request string) (generated.Reservation, error) {
	var out generated.Reservation
	read := func(q *generated.Queries) error {
		var e error
		out, e = q.GetUsageReservation(ctx, generated.GetUsageReservationParams{TenantID: scope.TenantID(), ID: id})
		return e
	}
	if !scope.Allows(policy.Read) {
		return out, policy.ErrForbidden
	}
	var e error
	if scope.Platform() {
		e = (AuditWriter{Pool: r.Pool}).WithTransaction(ctx, scope, policy.Read, &Audit{Action: "usage.read", ResourceID: id, RequestID: request, Reason: "platform reservation read"}, read)
	} else {
		e = read(generated.New(r.Pool))
	}
	if e != nil {
		return generated.Reservation{}, e
	}
	return out, nil
}
