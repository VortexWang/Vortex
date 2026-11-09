package repository

import (
	"context"
	"encoding/json"
	"errors"

	"vortex/backend/db/generated"
	"vortex/backend/internal/policy"
)

func (r PublicResources) Recovery(ctx context.Context, s TenantScope, kind, id, request string) (json.RawMessage, error) {
	if !s.Platform() || !s.Allows(policy.Read) {
		return nil, policy.ErrForbidden
	}
	var body []byte
	e := (AuditWriter{Pool: r.Pool}).WithTransaction(ctx, s, policy.Read, &Audit{Action: "recovery.read", ResourceID: id, RequestID: request, Reason: "scoped platform business stage only; no control proofs or financial release"}, func(q *generated.Queries) error {
		var e error
		switch kind {
		case "node":
			body, e = q.GetPublicNodeRecovery(ctx, generated.GetPublicNodeRecoveryParams{TenantID: s.TenantID(), ID: id})
		case "hub-same-store":
			body, e = q.GetPublicHubSameStoreRecovery(ctx, generated.GetPublicHubSameStoreRecoveryParams{TenantID: s.TenantID(), ID: id})
		case "hub-cold":
			body, e = q.GetPublicHubColdRecovery(ctx, generated.GetPublicHubColdRecoveryParams{TenantID: s.TenantID(), ID: id})
		default:
			return errors.New("unsupported recovery kind")
		}
		return e
	})
	if e != nil {
		return nil, e
	}
	return json.RawMessage(body), nil
}
