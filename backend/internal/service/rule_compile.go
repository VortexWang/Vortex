package service

import (
	"bytes"
	"context"
	"encoding/json"

	"github.com/jackc/pgx/v5/pgtype"
	"vortex/backend/db/generated"
	"vortex/backend/internal/compiler"
	"vortex/backend/internal/policy"
	"vortex/backend/internal/repository"
)

// BindCompileInput is the production producer boundary for the billing snapshot
// authority. Callers supply an already validated immutable snapshot, never a
// default or latest lookup. Missing snapshots leave compile requests pending.
// Binding validates all rights and pins their versions once, without artifacts.
func (s RuleService) BindCompileInput(ctx context.Context, scope TenantScope, task string, billing compiler.BillingSnapshot, signer, path, fence int64, request string) (string, error) {
	var digest string
	err := (repository.AuditWriter{Pool: s.Pool}).WithTransaction(ctx, scope, policy.Mutate, &repository.Audit{Action: "rule.compile.bind", ResourceID: task, RequestID: request, Reason: "pin validated immutable billing and compiler inputs"}, func(q *generated.Queries) error {
		b, e := lockCompileTask(ctx, q, scope, task)
		if e != nil {
			return e
		}
		if b.InputDigest.Valid {
			in, e := readCompileInput(ctx, q, b)
			if e != nil {
				return e
			}
			if in.Billing != billing || in.SignerEpoch != signer || in.PathGeneration != path || in.FencingGeneration != fence {
				return ErrConflict
			}
			digest = b.InputDigest.String
			return nil
		}
		if b.State != "pending" {
			return ErrRuleRevisionConflict
		}
		in, e := lockedCompileInput(ctx, q, scope, b.RuleID, b.RuleRevision, billing, signer, path, fence)
		if e != nil {
			return e
		}
		r, e := repository.PersistCompileInput(ctx, q, in)
		if e != nil {
			return e
		}
		digest = r.InputDigest
		n, e := q.BindRuleCompileInput(ctx, generated.BindRuleCompileInputParams{TenantID: scope.TenantID(), TaskID: task, InputDigest: pgtype.Text{String: digest, Valid: true}})
		if e != nil {
			return e
		}
		if n != 1 {
			return ErrConflict
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	return digest, nil
}

func lockCompileTask(ctx context.Context, q *generated.Queries, scope TenantScope, task string) (generated.LockRuleCompileRequestRow, error) {
	b, e := q.GetRuleCompileRequest(ctx, generated.GetRuleCompileRequestParams{TenantID: scope.TenantID(), TaskID: task})
	if e != nil {
		return generated.LockRuleCompileRequestRow{}, e
	}
	h, e := q.LockRuleHead(ctx, generated.LockRuleHeadParams{TenantID: scope.TenantID(), ID: b.RuleID})
	if e != nil {
		return generated.LockRuleCompileRequestRow{}, e
	}
	if h.DesiredRevision != b.RuleRevision {
		return generated.LockRuleCompileRequestRow{}, ErrRuleRevisionConflict
	}
	r, e := q.LockRuleCompileRequest(ctx, generated.LockRuleCompileRequestParams{TenantID: scope.TenantID(), TaskID: task})
	return r, e
}

func readCompileInput(ctx context.Context, q *generated.Queries, b generated.LockRuleCompileRequestRow) (compiler.CompileInput, error) {
	var in compiler.CompileInput
	if !b.InputDigest.Valid {
		return in, compiler.Unauthorized
	}
	p, e := q.GetRuleCompileInput(ctx, generated.GetRuleCompileInputParams{TenantID: b.TenantID, RuleID: b.RuleID, RuleRevision: b.RuleRevision, InputDigest: b.InputDigest.String})
	if e != nil {
		return in, e
	}
	if e = json.Unmarshal(p.Canonical, &in); e != nil {
		return in, e
	}
	r := compiler.Compile(ctx, in)
	if len(r.Diagnostics) != 0 {
		return in, r.Diagnostics[0]
	}
	if r.InputDigest != b.InputDigest.String || !bytes.Equal(r.InputCanonical, p.Canonical) || in.TenantID != b.TenantID || in.Rule.ID != b.RuleID || in.Revision != b.RuleRevision || in.Billing.ID != p.BillingSnapshotID || in.SignerEpoch != p.SignerEpoch || in.PathGeneration != p.PathGeneration || in.FencingGeneration != p.FencingGeneration {
		return in, ErrConflict
	}
	return in, nil
}

// ConsumeCompile atomically consumes only the task's pinned input, commits the
// immutable artifacts and fixed-member Prepare tasks, and acknowledges outbox.
// There is no network I/O and no durable lease crash window in this transaction.
func (s RuleService) ConsumeCompile(ctx context.Context, scope TenantScope, task, request string) error {
	return (repository.AuditWriter{Pool: s.Pool}).WithTransaction(ctx, scope, policy.Mutate, &repository.Audit{Action: "rule.compile.consume", ResourceID: task, RequestID: request, Reason: "compile pinned input and enqueue fixed-member prepare atomically"}, func(q *generated.Queries) error {
		b, e := lockCompileTask(ctx, q, scope, task)
		if e != nil {
			return e
		}
		if b.State == "succeeded" {
			return nil
		}
		if b.State != "pending" {
			return ErrRuleRevisionConflict
		}
		in, e := readCompileInput(ctx, q, b)
		if e != nil {
			return e
		}
		r, e := repository.PersistCompilation(ctx, q, in)
		if e != nil {
			return e
		}
		if _, e = beginRuleDeployment(ctx, q, scope, b.RuleID, b.RuleRevision, r.InputDigest); e != nil {
			return e
		}
		if e = q.ReconcileTask(ctx, generated.ReconcileTaskParams{TenantID: scope.TenantID(), OperationID: task, State: "succeeded"}); e != nil {
			return e
		}
		if e = q.UpdateOperationState(ctx, generated.UpdateOperationStateParams{TenantID: scope.TenantID(), ID: task, State: "succeeded"}); e != nil {
			return e
		}
		return q.MarkDelivered(ctx, generated.MarkDeliveredParams{TenantID: scope.TenantID(), OperationID: task})
	})
}

// RejectCompile records a known pre-delivery conflict. It never releases the
// prior active artifact or resource holds and never handles uncertain execution.
func (s RuleService) RejectCompile(ctx context.Context, scope TenantScope, task, request string) error {
	return (repository.AuditWriter{Pool: s.Pool}).WithTransaction(ctx, scope, policy.Mutate, &repository.Audit{Action: "rule.compile.rejected", ResourceID: task, RequestID: request, Reason: "pinned compilation no longer matches current resources"}, func(q *generated.Queries) error {
		b, err := lockCompileTask(ctx, q, scope, task)
		if err != nil {
			return err
		}
		if b.State == "failed" {
			return nil
		}
		if b.State != "pending" || !b.InputDigest.Valid {
			return ErrRuleRevisionConflict
		}
		if err = q.ReconcileTask(ctx, generated.ReconcileTaskParams{TenantID: scope.TenantID(), OperationID: task, State: "failed"}); err != nil {
			return err
		}
		if err = q.UpdateOperationState(ctx, generated.UpdateOperationStateParams{TenantID: scope.TenantID(), ID: task, State: "failed"}); err != nil {
			return err
		}
		if err = q.SetRuleSync(ctx, generated.SetRuleSyncParams{TenantID: scope.TenantID(), ID: b.RuleID, ActiveRevision: b.RuleRevision, SyncStatus: "failed"}); err != nil {
			return err
		}
		return q.MarkDelivered(ctx, generated.MarkDeliveredParams{TenantID: scope.TenantID(), OperationID: task})
	})
}
