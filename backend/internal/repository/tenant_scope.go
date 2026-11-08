package repository

import (
	"context"
	"errors"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"vortex/backend/db/generated"
	"vortex/backend/internal/auth"
	"vortex/backend/internal/policy"
)

type TenantScope struct {
	principal auth.Principal
	tenant    string
	action    policy.Action
}

func NewTenantScope(p auth.Principal, tenant string, action policy.Action) (TenantScope, error) {
	if err := policy.Authorize(p, tenant, action); err != nil {
		return TenantScope{}, err
	}
	return TenantScope{principal: p, tenant: tenant, action: action}, nil
}
func (s TenantScope) TenantID() string { return s.tenant }
func (s TenantScope) ActorID() string  { return s.principal.ID() }
func (s TenantScope) Platform() bool   { return s.principal.Platform() }
func (s TenantScope) Valid() bool      { return policy.Authorize(s.principal, s.tenant, s.action) == nil }
func (s TenantScope) Allows(a policy.Action) bool {
	return s.Valid() && (a == policy.Read || s.action == a) && policy.Authorize(s.principal, s.tenant, a) == nil
}

// ForTenant preserves the authenticated principal and its existing action. It
// cannot promote a stored actor ID or widen a read/mutation scope into a grant.
// The new tenant is independently authorized; platform reads/writes still use
// the target operation's AuditWriter transaction.
func (s TenantScope) ForTenant(tenant string, action policy.Action) (TenantScope, error) {
	if !s.Allows(action) {
		return TenantScope{}, policy.ErrForbidden
	}
	return NewTenantScope(s.principal, tenant, action)
}

type Audit struct {
	Action, ResourceID, RequestID, Reason string
	Before, After                         []byte
}
type AuditWriter struct{ Pool *pgxpool.Pool }

// WithTransaction commits authority, audit, tasks and outbox together. Callers
// return any write error to abort all changes; no cache or external I/O belongs
// inside the callback. Summary JSON must contain safe domain fields, no secrets.
func (w AuditWriter) WithTransaction(ctx context.Context, scope TenantScope, action policy.Action, event *Audit, fn func(*generated.Queries) error) error {
	if !scope.Allows(action) {
		return policy.ErrForbidden
	}
	if event == nil {
		return errors.New("invalid audit context")
	}
	if event.Action == "" || event.ResourceID == "" || event.RequestID == "" || len(event.RequestID) > 128 || event.Reason == "" || len(event.Reason) > 512 {
		return errors.New("invalid audit context")
	}
	return pgx.BeginTxFunc(ctx, w.Pool, pgx.TxOptions{}, func(tx pgx.Tx) error {
		q := generated.New(tx)
		if err := fn(q); err != nil {
			return err
		}
		before, after := event.Before, event.After
		if before == nil {
			before = []byte(`{}`)
		}
		if after == nil {
			after = []byte(`{}`)
		}
		return q.InsertAudit(ctx, generated.InsertAuditParams{TenantID: scope.TenantID(), ActorID: scope.ActorID(), Action: event.Action, ResourceID: event.ResourceID, RequestID: event.RequestID, Reason: event.Reason, BeforeSummary: before, AfterSummary: after})
	})
}

type TenantRepository struct{ Pool *pgxpool.Pool }

// Operation keeps the predicate and platform read audit below the service layer.
func (r TenantRepository) Operation(ctx context.Context, s TenantScope, id, requestID string) (generated.Operation, error) {
	var result generated.Operation
	if !s.Allows(policy.Read) {
		return result, policy.ErrForbidden
	}
	read := func(q *generated.Queries) error {
		var err error
		result, err = q.GetOperation(ctx, generated.GetOperationParams{TenantID: s.TenantID(), ID: id})
		return err
	}
	if s.Platform() {
		err := (AuditWriter{Pool: r.Pool}).WithTransaction(ctx, s, policy.Read, &Audit{Action: "operation.read", ResourceID: id, RequestID: requestID, Reason: "platform tenant read"}, read)
		if err != nil {
			return generated.Operation{}, err
		}
		return result, err
	}
	err := read(generated.New(r.Pool))
	return result, err
}
