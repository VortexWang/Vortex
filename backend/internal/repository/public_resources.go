package repository

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"vortex/backend/db/generated"
	"vortex/backend/internal/policy"
)

type PublicResources struct{ Pool *pgxpool.Pool }

// Get exposes only SQL-whitelisted business projections. It cannot return a
// generated model or issue a read without tenant scope and platform read audit.
func (r PublicResources) Get(ctx context.Context, s TenantScope, kind, id, request string) (json.RawMessage, error) {
	if !s.Allows(policy.Read) {
		return nil, policy.ErrForbidden
	}
	var body []byte
	read := func(q *generated.Queries) error {
		var e error
		switch kind {
		case "tenant":
			body, e = q.GetPublicTenant(ctx, s.TenantID())
		case "node":
			body, e = q.GetPublicNode(ctx, generated.GetPublicNodeParams{ResourceID: id, TenantID: pgtype.Text{String: s.TenantID(), Valid: true}})
		case "node_group":
			body, e = q.GetPublicNodeGroup(ctx, generated.GetPublicNodeGroupParams{ResourceID: id, TenantID: pgtype.Text{String: s.TenantID(), Valid: true}})
		case "subscription":
			body, e = q.GetPublicSubscription(ctx, s.TenantID())
		case "package":
			body, e = q.GetPublicPackage(ctx, id)
		case "rule":
			body, e = q.GetPublicRule(ctx, generated.GetPublicRuleParams{TenantID: s.TenantID(), ResourceID: id})
		case "source_ledger":
			body, e = q.GetPublicSourceLedger(ctx, generated.GetPublicSourceLedgerParams{TenantID: s.TenantID(), ResourceID: id})
		case "limits":
			body, e = q.GetPublicLimits(ctx, s.TenantID())
			if e == nil {
				var result struct {
					Operations []string `json:"operation_ids"`
				}
				if e = json.Unmarshal(body, &result); e == nil && len(result.Operations) > 1000 {
					e = errors.New("limit operation projection exceeds bound")
				}
			}
		default:
			return errors.New("unsupported public resource")
		}
		return e
	}
	if id == "" {
		id = s.TenantID()
	}
	if s.Platform() {
		e := (AuditWriter{Pool: r.Pool}).WithTransaction(ctx, s, policy.Read, &Audit{Action: kind + ".read", ResourceID: id, RequestID: request, Reason: "platform scoped public business read"}, read)
		if e != nil {
			return nil, e
		}
		return json.RawMessage(body), nil
	}
	if e := read(generated.New(r.Pool)); e != nil {
		return nil, e
	}
	return json.RawMessage(body), nil
}
