package service

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"sort"

	"vortex/backend/db/generated"
	"vortex/backend/internal/auth"
	"vortex/backend/internal/policy"
	"vortex/backend/internal/repository"
)

type groupRevalidationEnvelope struct {
	Action        string `json:"action"`
	GroupID       string `json:"resource_id"`
	Version       int64  `json:"version"`
	ActorID       string `json:"actor_id"`
	PlatformActor bool   `json:"platform_actor"`
	Schema        int    `json:"revalidation_schema"`
}

func newGroupRevalidationTask(ctx context.Context, q *generated.Queries, scope TenantScope, group string, version int64) (generated.Operation, error) {
	body := auditJSON(groupRevalidationEnvelope{"group.revalidate_all_rules", group, version, scope.ActorID(), scope.Platform(), 1})
	return appendDomainTask(ctx, q, scope, "resource_change", version, body)
}

var errGroupRevalidationProcessed = errors.New("group revalidation already processed")

// ProcessGroupRevalidation consumes only an immutable actor-bound platform job.
// It enqueues every tenant's current dependent revision; success does not assert
// that any artifact has compiled, applied or stopped, or release old occupation.
func (s NodeService) ProcessGroupRevalidation(ctx context.Context, tenant, operation, request string) (bool, error) {
	q := generated.New(s.Pool)
	op, e := q.GetOperation(ctx, generated.GetOperationParams{TenantID: tenant, ID: operation})
	if e != nil {
		return false, e
	}
	var job groupRevalidationEnvelope
	digest := sha256.Sum256(op.Payload)
	if json.Unmarshal(op.Payload, &job) != nil || job.Action != "group.revalidate_all_rules" || job.Schema != 1 || !job.PlatformActor || !validID(job.ActorID) || !validID(job.GroupID) || job.Version < 1 || job.Version != op.Generation || !bytes.Equal(digest[:], op.RequestSha256) {
		return false, ErrConflict
	}
	actor, e := q.GetPlatformPrincipal(ctx, job.ActorID)
	if e != nil {
		return false, ErrScope
	}
	principal, e := auth.NewPrincipal(actor.ID, "", auth.Role(actor.Role), actor.State)
	if e != nil {
		return false, ErrScope
	}
	scope, e := repository.NewTenantScope(principal, tenant, policy.Mutate)
	if e != nil {
		return false, e
	}
	worked := false
	e = (repository.AuditWriter{Pool: s.Pool}).WithTransaction(ctx, scope, policy.Mutate, &repository.Audit{Action: "group.revalidate.complete", ResourceID: operation, RequestID: request, Reason: "enqueue all current tenant dependents; no execution or release claim"}, func(q *generated.Queries) error {
		if e := packageActor(ctx, q, scope, policy.Mutate); e != nil {
			return e
		}
		tenants, e := q.GroupDependentTenants(ctx, job.GroupID)
		if e != nil {
			return e
		}
		tenants = append(tenants, tenant)
		sort.Strings(tenants)
		unique := tenants[:0]
		for _, id := range tenants {
			if len(unique) == 0 || unique[len(unique)-1] != id {
				unique = append(unique, id)
			}
		}
		// Take every tenant before any rule head or task. This transaction never
		// waits for the source group held by an editor in another tenant.
		for _, id := range unique {
			if _, e := scope.ForTenant(id, policy.Mutate); e != nil {
				return e
			}
			if _, e := q.LockTenant(ctx, id); e != nil {
				return e
			}
		}
		task, e := q.LockGroupRevalidationTask(ctx, generated.LockGroupRevalidationTaskParams{TenantID: tenant, OperationID: operation})
		if e != nil {
			return e
		}
		if task.State == "succeeded" {
			return errGroupRevalidationProcessed
		}
		if task.State != "pending" {
			return ErrConflict
		}
		group, e := q.GetGroupRevalidationAuthority(ctx, job.GroupID)
		if e != nil {
			return e
		}
		if group.Version < job.Version {
			return ErrConflict
		}
		for _, id := range unique {
			target, e := scope.ForTenant(id, policy.Mutate)
			if e != nil {
				return e
			}
			if e = invalidateRules(ctx, q, target, "group", job.GroupID); e != nil {
				return e
			}
			if e = q.InsertAudit(ctx, generated.InsertAuditParams{TenantID: id, ActorID: scope.ActorID(), Action: "group.revalidate.rules", ResourceID: job.GroupID, RequestID: request, Reason: "platform group membership dependency invalidation", BeforeSummary: []byte(`{}`), AfterSummary: auditJSON(struct {
				Version   int64  `json:"version"`
				Operation string `json:"operation_id"`
			}{job.Version, operation})}); e != nil {
				return e
			}
		}
		if e = q.ReconcileTask(ctx, generated.ReconcileTaskParams{TenantID: tenant, OperationID: operation, State: "succeeded"}); e != nil {
			return e
		}
		if e = q.UpdateOperationState(ctx, generated.UpdateOperationStateParams{TenantID: tenant, ID: operation, State: "succeeded"}); e != nil {
			return e
		}
		if e = q.MarkDelivered(ctx, generated.MarkDeliveredParams{TenantID: tenant, OperationID: operation}); e != nil {
			return e
		}
		worked = true
		return nil
	})
	if errors.Is(e, errGroupRevalidationProcessed) {
		return false, nil
	}
	if e != nil {
		return false, e
	}
	return worked, nil
}
