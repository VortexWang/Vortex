package repository

import (
	"context"
	"github.com/jackc/pgx/v5/pgxpool"
	"vortex/backend/db/generated"
	"vortex/backend/internal/policy"
)

type RuleRepository struct{ Pool *pgxpool.Pool }

func (r RuleRepository) Revision(ctx context.Context, s TenantScope, id string, revision int64, request string) (generated.RuleRevision, error) {
	var result generated.RuleRevision
	err := (AuditWriter{Pool: r.Pool}).WithTransaction(ctx, s, policy.Read, &Audit{Action: "rule.read", ResourceID: id, RequestID: request, Reason: "scoped immutable rule revision"}, func(q *generated.Queries) error {
		var e error
		result, e = q.GetRuleRevision(ctx, generated.GetRuleRevisionParams{TenantID: s.TenantID(), RuleID: id, Revision: revision})
		return e
	})
	if err != nil {
		return generated.RuleRevision{}, err
	}
	return result, nil
}
