package repository

import (
	"context"
	"github.com/jackc/pgx/v5/pgxpool"
	"vortex/backend/db/generated"
	"vortex/backend/internal/policy"
)

type PackageRepository struct{ Pool *pgxpool.Pool }

// Snapshot always includes the tenant predicate, including platform reads.
func (r PackageRepository) Snapshot(ctx context.Context, s TenantScope, id, request string) (generated.PackageSnapshot, error) {
	var out generated.PackageSnapshot
	if !s.Allows(policy.Read) {
		return out, policy.ErrForbidden
	}
	read := func(q *generated.Queries) error {
		var e error
		out, e = q.GetPackageSnapshot(ctx, generated.GetPackageSnapshotParams{TenantID: s.TenantID(), ID: id})
		return e
	}
	var e error
	if s.Platform() {
		e = (AuditWriter{Pool: r.Pool}).WithTransaction(ctx, s, policy.Read, &Audit{Action: "package.snapshot.read", ResourceID: id, RequestID: request, Reason: "platform snapshot read"}, read)
	} else {
		e = read(generated.New(r.Pool))
	}
	if e != nil {
		return generated.PackageSnapshot{}, e
	}
	return out, nil
}
