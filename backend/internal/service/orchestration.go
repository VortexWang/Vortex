package service

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"google.golang.org/protobuf/proto"
	"vortex/backend/db/generated"
	"vortex/backend/internal/compiler"
	"vortex/backend/internal/policy"
	"vortex/backend/internal/repository"
	controlv1 "vortex/contracts/protobuf/v1"
)

// RuleOrchestrator owns fixed-member application facts. It performs no network
// I/O: every next phase and its audit/outbox commit before a worker sends it.
type RuleOrchestrator struct{ Pool *pgxpool.Pool }

// Serialize revision decisions before locking deployments/tasks. Mutation and
// compile already take this head lock first; a late receipt must not enqueue
// activation concurrently with the transaction superseding its pending work.
func lockRuleDeployment(ctx context.Context, q *generated.Queries, tenant, id string) (generated.RuleDeployment, generated.RuleHead, error) {
	d, e := q.GetRuleDeployment(ctx, generated.GetRuleDeploymentParams{TenantID: tenant, ID: id})
	if e != nil {
		return d, generated.RuleHead{}, e
	}
	h, e := q.LockRuleHead(ctx, generated.LockRuleHeadParams{TenantID: tenant, ID: d.RuleID})
	if e != nil {
		return d, h, e
	}
	d, e = q.LockRuleDeployment(ctx, generated.LockRuleDeploymentParams{TenantID: tenant, ID: id})
	return d, h, e
}

func (s RuleOrchestrator) Begin(ctx context.Context, scope TenantScope, rule string, revision int64, input, request string) (generated.RuleDeployment, error) {
	var result generated.RuleDeployment
	err := (repository.AuditWriter{Pool: s.Pool}).WithTransaction(ctx, scope, policy.Mutate, &repository.Audit{Action: "rule.prepare", ResourceID: rule, RequestID: request, Reason: "pin committed artifact member set"}, func(q *generated.Queries) error {
		var e error
		result, e = beginRuleDeployment(ctx, q, scope, rule, revision, input)
		return e
	})
	return result, err
}

func beginRuleDeployment(ctx context.Context, q *generated.Queries, scope TenantScope, rule string, revision int64, input string) (generated.RuleDeployment, error) {
	if e := activeTenant(ctx, q, scope); e != nil {
		return generated.RuleDeployment{}, e
	}
	head, e := q.LockRuleHead(ctx, generated.LockRuleHeadParams{TenantID: scope.TenantID(), ID: rule})
	if e != nil {
		return generated.RuleDeployment{}, e
	}
	if head.DesiredRevision != revision {
		return generated.RuleDeployment{}, ErrRuleRevisionConflict
	}
	old, e := q.RuleDeploymentByInput(ctx, generated.RuleDeploymentByInputParams{TenantID: scope.TenantID(), RuleID: rule, RuleRevision: revision, InputDigest: input})
	if e == nil {
		return old, nil
	}
	if !errors.Is(e, pgx.ErrNoRows) {
		return generated.RuleDeployment{}, e
	}
	rows, e := q.ListCommittedRuleArtifacts(ctx, generated.ListCommittedRuleArtifactsParams{TenantID: scope.TenantID(), RuleID: rule, RuleRevision: revision, InputDigest: input})
	if e != nil {
		return generated.RuleDeployment{}, e
	}
	if len(rows) == 0 {
		return generated.RuleDeployment{}, ErrConflict
	}
	// Revalidate current rights and incarnation, but never silently substitute a
	// different member set or regenerate the pinned artifact during delivery.
	rev, e := q.GetRuleRevision(ctx, generated.GetRuleRevisionParams{TenantID: scope.TenantID(), RuleID: rule, Revision: revision})
	if e != nil {
		return generated.RuleDeployment{}, e
	}
	var model RuleInput
	if e = json.Unmarshal(rev.Payload, &model); e != nil {
		return generated.RuleDeployment{}, e
	}
	resolved, e := resolveRule(ctx, q, scope, model)
	if e != nil {
		return generated.RuleDeployment{}, e
	}
	boots := map[string]string{}
	for _, group := range resolved.members {
		for _, n := range group.Members {
			boots[n.ID] = n.BootID
		}
	}
	if len(boots) != len(rows) {
		return generated.RuleDeployment{}, ErrConflict
	}
	result, e := q.InsertRuleDeployment(ctx, generated.InsertRuleDeploymentParams{TenantID: scope.TenantID(), ID: uuid.NewString(), RuleID: rule, RuleRevision: revision, InputDigest: input, CreatedBy: scope.ActorID()})
	if e != nil {
		return generated.RuleDeployment{}, e
	}
	for _, a := range rows {
		env, e := repository.CommittedArtifact(ctx, q, scope.TenantID(), repository.ArtifactBinding{RuleID: rule, NodeID: a.NodeID, Digest: a.Digest, Revision: revision, FencingGeneration: a.FencingGeneration})
		if e != nil {
			return generated.RuleDeployment{}, e
		}
		if boots[a.NodeID] != env.BootID {
			return generated.RuleDeployment{}, ErrConflict
		}
		m := generated.RuleDeploymentMember{TenantID: scope.TenantID(), DeploymentID: result.ID, NodeID: a.NodeID, BootID: env.BootID, TransactionID: uuid.NewString(), ArtifactDigest: a.Digest, FencingGeneration: a.FencingGeneration, State: "pending"}
		if e = q.InsertRuleDeploymentMember(ctx, generated.InsertRuleDeploymentMemberParams{TenantID: m.TenantID, DeploymentID: m.DeploymentID, NodeID: m.NodeID, BootID: m.BootID, TransactionID: m.TransactionID, ArtifactDigest: m.ArtifactDigest, FencingGeneration: m.FencingGeneration}); e != nil {
			return generated.RuleDeployment{}, e
		}
		if e = queueRulePhase(ctx, q, scope, result, m, "prepare"); e != nil {
			return generated.RuleDeployment{}, e
		}
	}
	if e = bindRulePortPrepare(ctx, q, scope, result); e != nil {
		return generated.RuleDeployment{}, e
	}
	return result, q.SetRuleSync(ctx, generated.SetRuleSyncParams{TenantID: scope.TenantID(), ID: rule, ActiveRevision: revision, SyncStatus: "preparing"})
}

func queueRulePhase(ctx context.Context, q *generated.Queries, scope TenantScope, d generated.RuleDeployment, m generated.RuleDeploymentMember, phase string) error {
	exists, e := q.PendingRuleDelivery(ctx, generated.PendingRuleDeliveryParams{TenantID: d.TenantID, DeploymentID: d.ID, NodeID: m.NodeID, Phase: phase})
	if e != nil {
		return e
	}
	if exists {
		return nil
	}
	op, e := newDomainTask(ctx, q, scope, m.NodeID, "rule."+phase, "rule_"+phase, d.RuleRevision)
	if e != nil {
		return e
	}
	return q.BindRuleDelivery(ctx, generated.BindRuleDeliveryParams{TenantID: d.TenantID, TaskID: op.ID, DeploymentID: d.ID, NodeID: m.NodeID, Phase: phase})
}

// Command reloads the exact committed artifact after the task has been leased.
// The caller must authenticate the node and match BootID on the live stream.
func RuleCommand(ctx context.Context, q *generated.Queries, tenant, task string) (*controlv1.RuleCommand, string, string, error) {
	return ruleCommand(ctx, q, tenant, task, true)
}

// RuleObservationCommand reconstructs identity for recovery of a command which
// may already have executed. Current restrictions cannot erase that evidence.
// This function does not authorize sending activation commands.
func RuleObservationCommand(ctx context.Context, q *generated.Queries, tenant, task string) (*controlv1.RuleCommand, string, string, error) {
	return ruleCommand(ctx, q, tenant, task, false)
}

func ruleCommand(ctx context.Context, q *generated.Queries, tenant, task string, authorizeSend bool) (*controlv1.RuleCommand, string, string, error) {
	b, e := q.RuleDelivery(ctx, generated.RuleDeliveryParams{TenantID: tenant, TaskID: task})
	if e != nil {
		return nil, "", "", e
	}
	a, e := q.GetCommittedRuleArtifact(ctx, generated.GetCommittedRuleArtifactParams{TenantID: tenant, Digest: b.ArtifactDigest, RuleID: b.RuleID, RuleRevision: b.RuleRevision, NodeID: b.NodeID, FencingGeneration: b.FencingGeneration})
	if e != nil {
		return nil, "", "", e
	}
	artifact, e := compiler.DecodeArtifact(a.Canonical, a.Digest)
	if e != nil {
		return nil, "", "", e
	}
	if authorizeSend && artifact.Intent.Rule.Enabled && (b.Phase == "prepare" || b.Phase == "commit") {
		scope, err := NewTenantScope(uuid.NewString(), tenant, tenant)
		if err != nil {
			return nil, "", "", err
		}
		if err = activeRule(ctx, q, scope, artifact.RuleID); err != nil {
			return nil, "", "", err
		}
	}
	id, e := uuid.Parse(b.TransactionID)
	if e != nil {
		return nil, "", "", e
	}
	digest, e := hex.DecodeString(b.ArtifactDigest)
	if e != nil {
		return nil, "", "", e
	}
	return &controlv1.RuleCommand{Phase: b.Phase, TransactionId: id[:], TaskKey: task, TargetRevision: uint64(b.RuleRevision), IdempotencyKey: b.IdempotencyKey, ArtifactSha256: digest, FencingGeneration: uint64(b.FencingGeneration), Payload: a.Canonical}, b.NodeID, b.BootID, nil
}

func (s RuleOrchestrator) Receipt(ctx context.Context, scope TenantScope, task string, r *controlv1.RuleReceipt, request string) error {
	return s.ReceiptWithLease(ctx, scope, task, "", r, request)
}

// ReceiptWithLease binds socket observations to the worker's durable attempt.
func (s RuleOrchestrator) ReceiptWithLease(ctx context.Context, scope TenantScope, task, lease string, r *controlv1.RuleReceipt, request string) error {
	return (repository.AuditWriter{Pool: s.Pool}).WithTransaction(ctx, scope, policy.Mutate, &repository.Audit{Action: "rule.receipt", ResourceID: task, RequestID: request, Reason: "correlate fixed member phase receipt"}, func(q *generated.Queries) error {
		b, e := q.RuleDelivery(ctx, generated.RuleDeliveryParams{TenantID: scope.TenantID(), TaskID: task})
		if e != nil {
			return e
		}
		d, head, e := lockRuleDeployment(ctx, q, scope.TenantID(), b.DeploymentID)
		if e != nil {
			return e
		}
		// Member state is mutable. Refresh it after serializing on the head so
		// another receipt cannot make the pre-lock observation stale.
		b, e = q.RuleDelivery(ctx, generated.RuleDeliveryParams{TenantID: scope.TenantID(), TaskID: task})
		if e != nil {
			return e
		}
		expected, _, _, e := RuleObservationCommand(ctx, q, scope.TenantID(), task)
		if e != nil {
			return e
		}
		if r == nil || r.Phase != expected.Phase || r.TaskKey != expected.TaskKey || r.IdempotencyKey != expected.IdempotencyKey || r.TargetRevision != expected.TargetRevision || r.FencingGeneration != expected.FencingGeneration || !bytes.Equal(r.TransactionId, expected.TransactionId) || !bytes.Equal(r.ArtifactSha256, expected.ArtifactSha256) {
			return ErrStaleReceipt
		}
		raw, e := proto.MarshalOptions{Deterministic: true}.Marshal(r)
		if e != nil {
			return e
		}
		if len(raw) > 1<<20 {
			return ErrStaleReceipt
		}
		digest := sha256.Sum256(raw)
		prior, e := q.GetRuleReceipt(ctx, generated.GetRuleReceiptParams{TenantID: scope.TenantID(), TaskID: task})
		if e == nil {
			if !bytes.Equal(prior.ReceiptSha256, digest[:]) {
				return ErrConflict
			}
			return nil
		}
		if !errors.Is(e, pgx.ErrNoRows) {
			return e
		}
		state := r.State
		switch b.Phase {
		case "prepare":
			if state != "prepared" && state != "unknown" && state != "failed" {
				return ErrStaleReceipt
			}
		case "commit":
			if state != "active" && state != "unknown" && state != "failed" {
				return ErrStaleReceipt
			}
		case "abort":
			if state != "aborted" && state != "unknown" {
				return ErrStaleReceipt
			}
		case "query":
			if state != "prepared" && state != "active" && state != "aborted" && state != "unknown" {
				return ErrStaleReceipt
			}
		}
		// Observations may refine unknown to a proof. They never regress known
		// active/prepared state because an earlier command's response arrived late.
		if b.MemberState == "active" && state != "active" {
			return ErrStaleReceipt
		}
		if e = recordRulePortReceipt(ctx, q, scope, d, b, task, lease, state); e != nil {
			return e
		}
		if e = q.InsertRuleReceipt(ctx, generated.InsertRuleReceiptParams{TenantID: scope.TenantID(), TaskID: task, ReceiptSha256: digest[:], Payload: raw}); e != nil {
			return e
		}
		if e = q.SetRuleMemberState(ctx, generated.SetRuleMemberStateParams{TenantID: scope.TenantID(), DeploymentID: d.ID, NodeID: b.NodeID, State: state}); e != nil {
			return e
		}
		taskState := "succeeded"
		if state == "unknown" {
			taskState = "unknown"
		}
		if state == "failed" {
			taskState = "failed"
		}
		if e = q.ReconcileTask(ctx, generated.ReconcileTaskParams{TenantID: scope.TenantID(), OperationID: task, State: taskState}); e != nil {
			return e
		}
		if e = q.UpdateOperationState(ctx, generated.UpdateOperationStateParams{TenantID: scope.TenantID(), ID: task, State: taskState}); e != nil {
			return e
		}
		if taskState != "unknown" {
			if e = q.MarkDelivered(ctx, generated.MarkDeliveredParams{TenantID: scope.TenantID(), OperationID: task}); e != nil {
				return e
			}
		}
		return reconcileRuleDeployment(ctx, q, scope, d, head)
	})
}

func reconcileRuleDeployment(ctx context.Context, q *generated.Queries, scope TenantScope, d generated.RuleDeployment, head generated.RuleHead) error {
	members, e := q.RuleDeploymentMembers(ctx, generated.RuleDeploymentMembersParams{TenantID: d.TenantID, DeploymentID: d.ID})
	if e != nil {
		return e
	}
	allPrepared, allActive, unknown, failed, anyActive := true, true, false, false, false
	for _, m := range members {
		allPrepared = allPrepared && m.State == "prepared"
		allActive = allActive && m.State == "active"
		unknown = unknown || m.State == "unknown"
		failed = failed || m.State == "failed"
		anyActive = anyActive || m.State == "active"
	}
	obsolete := head.DesiredRevision != d.RuleRevision || d.State == "superseded"
	// A matching receipt remains evidence after restriction, but cannot create
	// fresh activation work. Safe query/abort continues to reconcile occupation.
	activationBlocked := false
	input, e := deploymentPortInput(ctx, q, scope, d)
	if e != nil {
		return e
	}
	if input.Rule.Enabled {
		if e = activeRule(ctx, q, scope, d.RuleID); e != nil {
			if !errors.Is(e, ErrConflict) && !errors.Is(e, ErrScope) {
				return e
			}
			activationBlocked = true
		}
	}
	state := d.State
	switch {
	case allActive:
		state = "running"
	case allPrepared && !obsolete && !activationBlocked && d.State != "failed":
		state = "committing"
		for _, m := range members {
			if e = queueRulePhase(ctx, q, scope, d, m, "commit"); e != nil {
				return e
			}
		}
	case anyActive:
		state = "partial"
	case unknown:
		state = "unknown"
	case failed:
		state = "failed"
	case obsolete:
		state = "superseded"
	}
	for _, m := range members {
		if m.State == "unknown" {
			if e = queueRulePhase(ctx, q, scope, d, m, "query"); e != nil {
				return e
			}
		}
		if m.State == "prepared" && (obsolete || activationBlocked || failed || d.State == "failed") {
			if e = queueRulePhase(ctx, q, scope, d, m, "abort"); e != nil {
				return e
			}
		} else if m.State == "prepared" && !obsolete && !activationBlocked && (d.State == "committing" || d.State == "partial") {
			if e = queueRulePhase(ctx, q, scope, d, m, "commit"); e != nil {
				return e
			}
		}
	}
	if e = q.SetRuleDeploymentState(ctx, generated.SetRuleDeploymentStateParams{TenantID: d.TenantID, ID: d.ID, State: state}); e != nil {
		return e
	}
	return q.SetRuleSync(ctx, generated.SetRuleSyncParams{TenantID: d.TenantID, ID: d.RuleID, ActiveRevision: d.RuleRevision, SyncStatus: state})
}

// FailRollback records a rejected current-rights/resource check. No execution
// or occupation fact is removed; unknown members remain queryable.
func (s RuleOrchestrator) FailRollback(ctx context.Context, scope TenantScope, id, request string) error {
	return (repository.AuditWriter{Pool: s.Pool}).WithTransaction(ctx, scope, policy.Mutate, &repository.Audit{Action: "rule.rollback.denied", ResourceID: id, RequestID: request, Reason: "current authorization/resource recheck failed; retain occupation"}, func(q *generated.Queries) error {
		d, head, e := lockRuleDeployment(ctx, q, scope.TenantID(), id)
		if e != nil {
			return e
		}
		if d.RollbackRevision != 0 || d.State == "running" {
			return nil
		}
		if e = q.SetRuleRollbackRevision(ctx, generated.SetRuleRollbackRevisionParams{TenantID: scope.TenantID(), ID: id, RollbackRevision: -1}); e != nil {
			return e
		}
		d.State = "failed"
		if e = q.SetRuleDeploymentState(ctx, generated.SetRuleDeploymentStateParams{TenantID: scope.TenantID(), ID: id, State: "failed"}); e != nil {
			return e
		}
		return reconcileRuleDeployment(ctx, q, scope, d, head)
	})
}

// Timeout never fabricates a stop receipt. Rollback appends a new revision of
// prior content through the same current-rights validation as a normal edit.
// A rollback revision cannot recursively roll back itself.
func (s RuleOrchestrator) Rollback(ctx context.Context, scope TenantScope, id, request string) (RuleMutation, error) {
	var result RuleMutation
	var d generated.RuleDeployment
	var model RuleInput
	e := (repository.AuditWriter{Pool: s.Pool}).WithTransaction(ctx, scope, policy.Mutate, &repository.Audit{Action: "rule.rollback.request", ResourceID: id, RequestID: request, Reason: "deadline exceeded; retain unknown occupation"}, func(q *generated.Queries) error {
		var e error
		var h generated.RuleHead
		d, h, e = lockRuleDeployment(ctx, q, scope.TenantID(), id)
		if e != nil {
			return e
		}
		if d.State == "running" || d.RollbackRevision != 0 {
			return ErrConflict
		}
		if h.DesiredRevision != d.RuleRevision {
			return ErrRuleRevisionConflict
		}
		current, e := q.GetRuleRevision(ctx, generated.GetRuleRevisionParams{TenantID: scope.TenantID(), RuleID: d.RuleID, Revision: d.RuleRevision})
		if e != nil {
			return e
		}
		if current.RollbackOf != 0 || h.ActiveRevision == 0 {
			return fmt.Errorf("rollback has no authorized predecessor")
		}
		prior, e := q.GetRuleRevision(ctx, generated.GetRuleRevisionParams{TenantID: scope.TenantID(), RuleID: d.RuleID, Revision: h.ActiveRevision})
		if e != nil {
			return e
		}
		if e = json.Unmarshal(prior.Payload, &model); e != nil {
			return e
		}
		result, e = appendRuleRevision(ctx, q, scope, model, h.DesiredRevision, d.RuleRevision)
		if e != nil {
			return e
		}
		return q.SetRuleRollbackRevision(ctx, generated.SetRuleRollbackRevisionParams{TenantID: scope.TenantID(), ID: id, RollbackRevision: result.Revision.Revision})
	})
	return result, e
}
