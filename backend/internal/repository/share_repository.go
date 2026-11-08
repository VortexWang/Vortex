package repository

import (
	"context"
	"github.com/jackc/pgx/v5/pgxpool"
	"vortex/backend/db/generated"
	"vortex/backend/internal/policy"
)

type ShareRepository struct{ Pool *pgxpool.Pool }

func (r ShareRepository) Chain(ctx context.Context, scope TenantScope, node, kind, request string) (generated.ShareChain, error) {
	var out generated.ShareChain
	if !scope.Allows(policy.Read) {
		return out, policy.ErrForbidden
	}
	read := func(q *generated.Queries) error {
		var e error
		out, e = q.ReadShareChain(ctx, generated.ReadShareChainParams{TenantID: scope.TenantID(), NodeID: node, Kind: kind})
		return e
	}
	var e error
	if scope.Platform() {
		e = (AuditWriter{Pool: r.Pool}).WithTransaction(ctx, scope, policy.Read, &Audit{Action: "share.read", ResourceID: node, RequestID: request, Reason: "platform tenant share authority read"}, read)
	} else {
		e = read(generated.New(r.Pool))
	}
	if e != nil {
		return generated.ShareChain{}, e
	}
	return out, nil
}
