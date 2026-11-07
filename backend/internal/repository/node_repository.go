package repository

import (
	"context"
	"github.com/jackc/pgx/v5/pgxpool"
	"vortex/backend/db/generated"
	"vortex/backend/internal/policy"
)

type NodeRepository struct{ Pool *pgxpool.Pool }

func (r NodeRepository) Node(ctx context.Context, s TenantScope, id, requestID string) (generated.ScopedNodeRow, error) {
	var result generated.ScopedNodeRow
	err := (AuditWriter{Pool: r.Pool}).WithTransaction(ctx, s, policy.Read, &Audit{Action: "node.read", ResourceID: id, RequestID: requestID, Reason: "scoped sensitive node read"}, func(q *generated.Queries) error {
		var err error
		result, err = q.ScopedNode(ctx, generated.ScopedNodeParams{NodeID: id, TenantID: s.TenantID(), PlatformRead: s.Platform()})
		return err
	})
	if err != nil {
		return generated.ScopedNodeRow{}, err
	}
	return result, nil
}

func (r NodeRepository) Group(ctx context.Context, s TenantScope, id, requestID string) (generated.ScopedGroupRow, error) {
	var result generated.ScopedGroupRow
	err := (AuditWriter{Pool: r.Pool}).WithTransaction(ctx, s, policy.Read, &Audit{Action: "group.read", ResourceID: id, RequestID: requestID, Reason: "scoped sensitive group read"}, func(q *generated.Queries) error {
		var err error
		result, err = q.ScopedGroup(ctx, generated.ScopedGroupParams{GroupID: id, TenantID: s.TenantID(), PlatformRead: s.Platform()})
		return err
	})
	if err != nil {
		return generated.ScopedGroupRow{}, err
	}
	return result, nil
}
