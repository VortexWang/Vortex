package service

import (
	"context"
	"vortex/backend/db/generated"
	"vortex/backend/internal/compiler"
)

// Only the first platform group meters the path. A wholly self-hosted path
// uses its existing independent runtime controls and never reserves platform
// capacity. Configuration preparation alone cannot create a Commit permit.
func platformBootstrapReserved(ctx context.Context, q *generated.Queries, tenant string, in compiler.CompileInput) (bool, error) {
	if !in.Rule.Enabled {
		return true, nil
	}
	execution := ""
	for _, id := range in.Rule.Path {
		for _, g := range in.Groups {
			if g.ID == id && g.OwnerKind == "platform" {
				execution = id
				break
			}
		}
		if execution != "" {
			break
		}
	}
	if execution == "" {
		return true, nil
	}
	for _, g := range in.Groups {
		if g.ID != execution {
			continue
		}
		for _, n := range g.Members {
			reserved, e := q.ShareBootstrapReserved(ctx, generated.ShareBootstrapReservedParams{TenantID: tenant, NodeID: n.ID, BootID: n.BootID})
			if e != nil {
				return false, e
			}
			if !reserved {
				return false, nil
			}
		}
	}
	return true, nil
}

// Resume only current, prepared deployments after capacity is durably reserved.
// This runs inside the caller's AuditWriter transaction; network follows commit.
func resumePreparedNodeDeployments(ctx context.Context, q *generated.Queries, scope TenantScope, node string) error {
	rows, e := q.PreparedNodeDeployments(ctx, generated.PreparedNodeDeploymentsParams{TenantID: scope.TenantID(), NodeID: node})
	if e != nil {
		return e
	}
	for _, row := range rows {
		d, h, e := lockRuleDeployment(ctx, q, scope.TenantID(), row.ID)
		if e != nil {
			return e
		}
		if e = reconcileRuleDeployment(ctx, q, scope, d, h); e != nil {
			return e
		}
	}
	return nil
}
