package service

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"vortex/backend/db/generated"
	"vortex/backend/internal/auth"
	"vortex/backend/internal/policy"
	"vortex/backend/internal/repository"
)

type recoveryAdvanceIntent struct {
	Action     string `json:"action"`
	RecoveryID string `json:"resource_id"`
	Version    int64  `json:"version"`
	ActorID    string `json:"actor_id"`
	Schema     int    `json:"advance_schema"`
}

func (s ShareService) RequestRecoveryAdvance(ctx context.Context, scope TenantScope, recovery, key, request string) (generated.Operation, error) {
	var out generated.Operation
	if !scope.Platform() || !scope.Allows(policy.Grant) {
		return out, ErrScope
	}
	if !validID(recovery) || len(key) < 1 || len(key) > 128 {
		return out, ErrConflict
	}
	body := auditJSON(recoveryAdvanceIntent{"hub.advance.requested", recovery, 1, scope.ActorID(), 1})
	e := (repository.AuditWriter{Pool: s.Pool}).WithTransaction(ctx, scope, policy.Grant, &repository.Audit{Action: "hub.request_recovery_advance", ResourceID: recovery, RequestID: request, Reason: "queue existing immutable approval only; no new epoch/boot authority or release"}, func(q *generated.Queries) error {
		if e := packageActor(ctx, q, scope, policy.Grant); e != nil {
			return e
		}
		if _, e := q.LockTenant(ctx, scope.TenantID()); e != nil {
			return e
		}
		if _, e := q.ScopedHubRecoveryContext(ctx, generated.ScopedHubRecoveryContextParams{TenantID: scope.TenantID(), ID: recovery}); e != nil {
			return e
		}
		var e error
		out, e = appendKeyedDomainTask(ctx, q, scope, "resource_change", 1, "hub.advance/"+key, body)
		return e
	})
	if e != nil {
		return generated.Operation{}, e
	}
	return out, nil
}

var errRecoveryAdvanceIdle = errors.New("recovery advance idle")

func (s ShareService) ProcessRecoveryAdvance(ctx context.Context, tenant, id, request string) (bool, error) {
	if !validID(tenant) || !validID(id) || request == "" || len(request) > 100 {
		return false, ErrConflict
	}
	q := generated.New(s.Pool)
	op, e := q.GetOperation(ctx, generated.GetOperationParams{TenantID: tenant, ID: id})
	if e != nil {
		return false, e
	}
	var job recoveryAdvanceIntent
	digest := sha256.Sum256(op.Payload)
	if json.Unmarshal(op.Payload, &job) != nil || job.Action != "hub.advance.requested" || job.Schema != 1 || job.Version != 1 || !validID(job.ActorID) || !validID(job.RecoveryID) || !bytes.Equal(digest[:], op.RequestSha256) {
		return false, ErrConflict
	}
	if op.State == "succeeded" {
		return false, nil
	}
	if op.State != "pending" && op.State != "leased" {
		return false, ErrConflict
	}
	p, e := (repository.TokenRepository{Pool: s.Pool}).LocalSessionPrincipal(ctx, job.ActorID, "")
	if e != nil || p.Role() != auth.PlatformAdmin {
		return false, ErrScope
	}
	scope, e := repository.NewTenantScope(p, tenant, policy.Grant)
	if e != nil {
		return false, e
	}
	lease := uuid.NewString()
	backend := ""
	e = (repository.AuditWriter{Pool: s.Pool}).WithTransaction(ctx, scope, policy.Grant, &repository.Audit{Action: "hub.recovery_advance.lease", ResourceID: job.RecoveryID, RequestID: request, Reason: "current platform role and original approval; durable bounded coordinator lease"}, func(q *generated.Queries) error {
		if e := packageActor(ctx, q, scope, policy.Grant); e != nil {
			return e
		}
		if _, e := q.LockTenant(ctx, tenant); e != nil {
			return e
		}
		if _, e := q.LockRecoveryAdvanceJob(ctx, generated.LockRecoveryAdvanceJobParams{TenantID: tenant, OperationID: id}); e != nil {
			return e
		}
		binding, e := q.ScopedHubRecoveryContext(ctx, generated.ScopedHubRecoveryContextParams{TenantID: tenant, ID: job.RecoveryID})
		if e != nil {
			return e
		}
		backend = binding.BackendID
		n, e := q.LeaseRecoveryAdvanceJob(ctx, generated.LeaseRecoveryAdvanceJobParams{TenantID: tenant, OperationID: id, LeaseToken: pgtype.Text{String: lease, Valid: true}})
		if e != nil {
			return e
		}
		if n != 1 {
			return errRecoveryAdvanceIdle
		}
		return q.UpdateOperationState(ctx, generated.UpdateOperationStateParams{TenantID: tenant, ID: id, State: "leased"})
	})
	if errors.Is(e, errRecoveryAdvanceIdle) {
		return false, nil
	}
	if e != nil {
		return false, e
	}
	progress, advanceErr := s.AdvanceHubSameStoreRecovery(ctx, scope, job.RecoveryID, backend, request+"/steps")
	state := "pending"
	if advanceErr != nil {
		state = "unknown"
	} else if progress.Stage == "admitted" {
		state = "succeeded"
	}
	e = (repository.AuditWriter{Pool: s.Pool}).WithTransaction(ctx, scope, policy.Grant, &repository.Audit{Action: "hub.recovery_advance.result", ResourceID: job.RecoveryID, RequestID: request + "/result", Reason: "record only persisted recovery stage; unknown retains all occupation", After: auditJSON(struct {
		State string `json:"state"`
		Stage string `json:"stage"`
	}{state, progress.Stage})}, func(q *generated.Queries) error {
		// Audit/domain-event append also serializes on the tenant. Match the
		// lease path's tenant -> task -> operation order under concurrent retries.
		if _, e := q.LockTenant(ctx, tenant); e != nil {
			return e
		}
		n, e := q.FinishRecoveryAdvanceJob(ctx, generated.FinishRecoveryAdvanceJobParams{TenantID: tenant, OperationID: id, LeaseToken: pgtype.Text{String: lease, Valid: true}, State: state})
		if e != nil {
			return e
		}
		if n != 1 {
			return ErrConflict
		}
		if e = q.UpdateOperationState(ctx, generated.UpdateOperationStateParams{TenantID: tenant, ID: id, State: state}); e != nil {
			return e
		}
		if state == "succeeded" {
			return q.MarkDelivered(ctx, generated.MarkDeliveredParams{TenantID: tenant, OperationID: id})
		}
		return nil
	})
	if e != nil {
		return false, e
	}
	return true, nil
}
