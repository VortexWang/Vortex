package service

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"time"
	"vortex/backend/db/generated"
	"vortex/backend/internal/policy"
	"vortex/backend/internal/repository"
)

type TenantService struct{ Pool *pgxpool.Pool }

func (s TenantService) Tenant(ctx context.Context, scope TenantScope, requestID string) (generated.Tenant, error) {
	var tenant generated.Tenant
	if !scope.Allows(policy.Read) {
		return tenant, ErrScope
	}
	load := func(q *generated.Queries) error {
		var err error
		tenant, err = q.GetTenant(ctx, scope.TenantID())
		return err
	}
	if scope.Platform() {
		err := (repository.AuditWriter{Pool: s.Pool}).WithTransaction(ctx, scope, policy.Read, &repository.Audit{Action: "tenant.read", ResourceID: scope.TenantID(), RequestID: requestID, Reason: "platform tenant read"}, load)
		return tenant, err
	}
	err := load(generated.New(s.Pool))
	return tenant, err
}

func (s TenantService) SetAccountState(ctx context.Context, scope TenantScope, state string, expected int64, requestID string) error {
	// Tenant owners manage member access, not their platform account status.
	if !scope.Platform() || !scope.Allows(policy.ManageAccess) {
		return ErrScope
	}
	if (state != "active" && state != "suspended" && state != "banned") || expected < 1 {
		return ErrConflict
	}
	event := repository.Audit{Action: "tenant.account_state", ResourceID: scope.TenantID(), RequestID: requestID, Reason: "account status transition", After: []byte(`{"account_state":"` + state + `"}`)}
	return (repository.AuditWriter{Pool: s.Pool}).WithTransaction(ctx, scope, policy.ManageAccess, &event, func(q *generated.Queries) error {
		old, err := q.LockTenant(ctx, scope.TenantID())
		if err != nil {
			return err
		}
		event.Before = []byte(`{"account_state":"` + old.AccountState + `"}`)
		_, err = q.SetTenantState(ctx, generated.SetTenantStateParams{TenantID: scope.TenantID(), AccountState: state, ExpectedVersion: expected})
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrConflict
		}
		return err
	})
}

type Restriction struct {
	Cause, SourceID        string
	SourceVersion          int64
	EffectiveAt, ClearedAt time.Time
}

func timestamp(t time.Time) pgtype.Timestamptz {
	return pgtype.Timestamptz{Time: t.UTC().Truncate(time.Microsecond), Valid: !t.IsZero()}
}
func validRestriction(r Restriction) bool {
	if r.SourceID == "" || len(r.SourceID) > 128 || r.SourceVersion < 1 || r.EffectiveAt.IsZero() || (!r.ClearedAt.IsZero() && r.ClearedAt.Before(r.EffectiveAt)) {
		return false
	}
	switch r.Cause {
	case "account_suspended", "account_banned", "package_expired", "quota_exhausted", "quantity_exceeded", "manual":
		return true
	}
	return false
}

// ChangeRestriction serializes history, projection and recovery on the tenant
// row. Recovery only reissues persisted desired state of currently authorized
// resources. It never treats missing execution confirmation as stopped.
func (s TenantService) ChangeRestriction(ctx context.Context, scope TenantScope, r Restriction, requestID string) error {
	if !scope.Allows(policy.Restrict) {
		return ErrScope
	}
	if !validRestriction(r) || (!r.ClearedAt.IsZero() && r.ClearedAt.After(time.Now())) {
		return ErrConflict
	}
	kind := "applied"
	if !r.ClearedAt.IsZero() {
		kind = "cleared"
	}
	event := repository.Audit{Action: "restriction." + kind, ResourceID: r.SourceID, RequestID: requestID, Reason: "business restriction transition", After: []byte(`{"cause":"` + r.Cause + `","event":"` + kind + `"}`)}
	return (repository.AuditWriter{Pool: s.Pool}).WithTransaction(ctx, scope, policy.Restrict, &event, func(q *generated.Queries) error {
		tenant, err := q.LockTenant(ctx, scope.TenantID())
		if err != nil {
			return err
		}
		previous, err := q.GetRestrictionEvent(ctx, generated.GetRestrictionEventParams{TenantID: scope.TenantID(), Cause: r.Cause, SourceID: r.SourceID, SourceVersion: r.SourceVersion, EventKind: kind})
		effective, cleared := timestamp(r.EffectiveAt), timestamp(r.ClearedAt)
		if err == nil {
			if !previous.EffectiveAt.Time.Equal(effective.Time) || previous.ClearedAt.Valid != cleared.Valid || (cleared.Valid && !previous.ClearedAt.Time.Equal(cleared.Time)) {
				return ErrConflict
			}
			return nil
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		if kind == "cleared" {
			current, e := q.GetCurrentRestriction(ctx, generated.GetCurrentRestrictionParams{TenantID: scope.TenantID(), Cause: r.Cause, SourceID: r.SourceID})
			if e != nil {
				if errors.Is(e, pgx.ErrNoRows) {
					return ErrConflict
				}
				return e
			}
			if current.SourceVersion != r.SourceVersion || !current.EffectiveAt.Time.Equal(effective.Time) || current.ClearedAt.Valid {
				return ErrConflict
			}
		}
		id := uuid.NewString()
		if _, err = q.AppendRestrictionEvent(ctx, generated.AppendRestrictionEventParams{ID: id, TenantID: scope.TenantID(), Cause: r.Cause, SourceID: r.SourceID, SourceVersion: r.SourceVersion, EventKind: kind, EffectiveAt: effective, ClearedAt: cleared}); err != nil {
			return err
		}
		var count int64
		if kind == "applied" {
			count, err = q.ApplyCurrentRestriction(ctx, generated.ApplyCurrentRestrictionParams{TenantID: scope.TenantID(), Cause: r.Cause, SourceID: r.SourceID, SourceVersion: r.SourceVersion, EventID: id, EffectiveAt: effective})
		} else {
			count, err = q.ClearCurrentRestriction(ctx, generated.ClearCurrentRestrictionParams{TenantID: scope.TenantID(), Cause: r.Cause, SourceID: r.SourceID, SourceVersion: r.SourceVersion, ClearedAt: cleared, EventID: id})
		}
		if err != nil {
			return err
		}
		if count != 1 {
			return ErrConflict
		}
		if kind == "cleared" && tenant.AccountState == "active" {
			return resumeAuthorizedResources(ctx, q, scope.TenantID(), id)
		}
		return nil
	})
}

// Every recovery operation/task/outbox is committed with the final clear event
// and its audit by AuditWriter. A replay returns before this function, so it
// cannot reissue rights. No balances, shares or old occupations are released.
func resumeAuthorizedResources(ctx context.Context, q *generated.Queries, tenantID, eventID string) error {
	at := timestamp(time.Now())
	active, err := q.ListCurrentRestrictions(ctx, generated.ListCurrentRestrictionsParams{TenantID: tenantID, AtTime: at})
	if err != nil || len(active) != 0 {
		return err
	}
	resources, err := q.ListTenantResources(ctx, generated.ListTenantResourcesParams{TenantID: pgtype.Text{String: tenantID, Valid: true}, AtTime: at})
	if err != nil {
		return err
	}
	// Lock operation rows in the generated query's stable ID order, so concurrent
	// receipts cannot turn an unknown observation into inferred free capacity.
	rows, err := q.ListAuthority(ctx, tenantID)
	if err != nil {
		return err
	}
	blocked := map[string]bool{}
	generation := map[string]int64{}
	for _, row := range rows {
		op, e := q.LockOperation(ctx, generated.LockOperationParams{TenantID: tenantID, ID: row.ID})
		if e != nil {
			return e
		}
		if op.Generation > generation[op.ResourceID] {
			generation[op.ResourceID] = op.Generation
		}
		if op.State != "succeeded" && op.State != "failed" {
			blocked[op.ResourceID] = true
		}
	}
	for _, resource := range resources {
		if blocked[resource.ResourceID] {
			continue
		}
		desired, e := q.GetDesiredState(ctx, generated.GetDesiredStateParams{TenantID: tenantID, ResourceID: resource.ResourceID})
		if errors.Is(e, pgx.ErrNoRows) {
			continue // Registry ownership alone never invents a desired configuration.
		}
		if e != nil {
			return e
		}
		if desired.Revision == int64(^uint64(0)>>1) || generation[resource.ResourceID] == int64(^uint64(0)>>1) {
			return ErrConflict
		}
		if _, e = q.PutDesiredState(ctx, generated.PutDesiredStateParams{TenantID: tenantID, ResourceID: resource.ResourceID, TargetKind: desired.TargetKind, Revision: desired.Revision + 1, ExpectedRevision: desired.Revision, Payload: desired.Payload, PayloadSha256: desired.PayloadSha256}); e != nil {
			return e
		}
		id := uuid.NewString()
		key := "restriction-resume/" + eventID + "/" + resource.ResourceID
		digest := sha256.Sum256([]byte(fmt.Sprintf("%s/%d/%x", key, desired.Revision+1, desired.PayloadSha256)))
		if _, e = q.InsertOperation(ctx, generated.InsertOperationParams{ID: id, TenantID: tenantID, ResourceID: resource.ResourceID, IdempotencyKey: key, RequestSha256: digest[:], TargetKind: desired.TargetKind, TargetRevision: desired.Revision + 1, ExpectedRevision: desired.Revision, Generation: generation[resource.ResourceID] + 1, Payload: desired.Payload}); e != nil {
			return e
		}
		if e = q.InsertTask(ctx, generated.InsertTaskParams{ID: id, TenantID: tenantID, OperationID: id}); e != nil {
			return e
		}
		if e = q.InsertOutbox(ctx, generated.InsertOutboxParams{TenantID: tenantID, OperationID: id}); e != nil {
			return e
		}
	}
	return nil
}

func (s TenantService) Restrictions(ctx context.Context, scope TenantScope, at time.Time, requestID string) ([]generated.CurrentRestriction, error) {
	if !scope.Allows(policy.Read) {
		return nil, ErrScope
	}
	var rows []generated.CurrentRestriction
	load := func(q *generated.Queries) error {
		var err error
		rows, err = q.ListCurrentRestrictions(ctx, generated.ListCurrentRestrictionsParams{TenantID: scope.TenantID(), AtTime: timestamp(at)})
		return err
	}
	if scope.Platform() {
		err := (repository.AuditWriter{Pool: s.Pool}).WithTransaction(ctx, scope, policy.Read, &repository.Audit{Action: "restriction.read", ResourceID: scope.TenantID(), RequestID: requestID, Reason: "platform restriction read"}, load)
		return rows, err
	}
	err := load(generated.New(s.Pool))
	return rows, err
}

func (s TenantService) Resources(ctx context.Context, scope TenantScope, at time.Time, requestID string) ([]generated.ResourceOwnership, error) {
	if !scope.Allows(policy.Read) {
		return nil, ErrScope
	}
	var rows []generated.ResourceOwnership
	load := func(q *generated.Queries) error {
		var err error
		rows, err = q.ListTenantResources(ctx, generated.ListTenantResourcesParams{TenantID: pgtype.Text{String: scope.TenantID(), Valid: true}, AtTime: timestamp(at)})
		return err
	}
	if scope.Platform() {
		err := (repository.AuditWriter{Pool: s.Pool}).WithTransaction(ctx, scope, policy.Read, &repository.Audit{Action: "resource.read", ResourceID: scope.TenantID(), RequestID: requestID, Reason: "platform resource read"}, load)
		return rows, err
	}
	err := load(generated.New(s.Pool))
	return rows, err
}

func (s TenantService) GrantResource(ctx context.Context, scope TenantScope, resourceID string, version int64, from, until time.Time, requestID string) error {
	if !scope.Allows(policy.Grant) {
		return ErrScope
	}
	if _, err := uuid.Parse(resourceID); err != nil {
		return ErrConflict
	}
	if version < 1 || from.IsZero() || !until.After(from) {
		return ErrConflict
	}
	return (repository.AuditWriter{Pool: s.Pool}).WithTransaction(ctx, scope, policy.Grant, &repository.Audit{Action: "resource.grant", ResourceID: resourceID, RequestID: requestID, Reason: "platform resource entitlement grant"}, func(q *generated.Queries) error {
		if _, err := q.GetPlatformResource(ctx, resourceID); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return ErrScope
			}
			return err
		}
		return q.InsertResourceGrant(ctx, generated.InsertResourceGrantParams{ID: uuid.NewString(), TenantID: scope.TenantID(), ResourceID: resourceID, Version: version, Status: "active", ValidFrom: timestamp(from), ValidUntil: timestamp(until)})
	})
}
