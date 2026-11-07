package repository

import (
	"context"
	"github.com/jackc/pgx/v5/pgxpool"
	"vortex/backend/db/generated"
	"vortex/backend/internal/policy"
)

type LedgerRepository struct{ Pool *pgxpool.Pool }

func (r LedgerRepository) Projection(ctx context.Context, scope TenantScope, source, request string) (generated.LedgerProjection, error) {
	var row generated.LedgerProjection
	read := func(q *generated.Queries) error {
		var e error
		row, e = q.GetSourceProjection(ctx, generated.GetSourceProjectionParams{TenantID: scope.TenantID(), SourceID: source})
		return e
	}
	if !scope.Allows(policy.Read) {
		return row, policy.ErrForbidden
	}
	var e error
	if scope.Platform() {
		e = (AuditWriter{Pool: r.Pool}).WithTransaction(ctx, scope, policy.Read, &Audit{Action: "ledger.read", ResourceID: source, RequestID: request, Reason: "platform source ledger read"}, read)
	} else {
		e = read(generated.New(r.Pool))
	}
	if e != nil {
		return generated.LedgerProjection{}, e
	}
	return row, nil
}
