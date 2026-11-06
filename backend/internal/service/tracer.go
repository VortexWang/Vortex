package service

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"vortex/backend/db/generated"
)

var (
	ErrScope        = errors.New("forbidden tenant scope")
	ErrConflict     = errors.New("desired revision or idempotency conflict")
	ErrStaleReceipt = errors.New("receipt does not match current target")
)

// TenantScope cannot be constructed with a caller-selected foreign tenant.
// Fixed user/platform role policy is layered on this boundary by plan 06-08.
type TenantScope struct{ tenant, actor string }

func NewTenantScope(actor, actorTenant, selectedTenant string) (TenantScope, error) {
	for _, id := range []string{actor, actorTenant, selectedTenant} {
		if _, err := uuid.Parse(id); err != nil {
			return TenantScope{}, ErrScope
		}
	}
	if actorTenant != selectedTenant {
		return TenantScope{}, ErrScope
	}
	return TenantScope{tenant: actorTenant, actor: actor}, nil
}
func (s TenantScope) TenantID() string { return s.tenant }
func (s TenantScope) Valid() bool      { return s.tenant != "" && s.actor != "" }

type Tracer struct{ Pool *pgxpool.Pool }
type Mutation struct {
	ResourceID                             string
	TargetKind                             string
	Revision, ExpectedRevision, Generation int64
	IdempotencyKey, RequestID              string
	Payload                                []byte
}

func (s *Tracer) Mutate(ctx context.Context, scope TenantScope, m Mutation) (generated.Operation, error) {
	var zero generated.Operation
	if !scope.Valid() {
		return zero, ErrScope
	}
	if _, err := uuid.Parse(m.ResourceID); err != nil {
		return zero, ErrConflict
	}
	if m.Revision <= 0 || m.Revision <= m.ExpectedRevision || m.ExpectedRevision < 0 || m.Generation <= 0 ||
		len(m.Payload) == 0 || len(m.Payload) > 524288 || len(m.IdempotencyKey) == 0 || len(m.IdempotencyKey) > 128 ||
		len(m.RequestID) == 0 || len(m.RequestID) > 128 || (m.TargetKind != "node" && m.TargetKind != "hub") {
		return zero, ErrConflict
	}
	identity := m
	identity.RequestID = "" // Request tracing is not business identity.
	encoded, err := json.Marshal(identity)
	if err != nil {
		return zero, err
	}
	requestDigest := sha256.Sum256(encoded)
	digest := sha256.Sum256(m.Payload)
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return zero, err
	}
	defer tx.Rollback(ctx)
	// Bound both concurrent retries and same-resource mutations before the first insert.
	if _, err = tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtextextended($1,0))", scope.tenant+"/key/"+m.IdempotencyKey); err != nil {
		return zero, err
	}
	q := generated.New(tx)
	previous, err := q.GetOperationByKey(ctx, generated.GetOperationByKeyParams{TenantID: scope.tenant, IdempotencyKey: m.IdempotencyKey})
	if err == nil {
		if !bytes.Equal(previous.RequestSha256, requestDigest[:]) {
			return zero, ErrConflict
		}
		return previous, tx.Commit(ctx)
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return zero, err
	}
	if _, err = tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtextextended($1,0))", scope.tenant+"/resource/"+m.ResourceID); err != nil {
		return zero, err
	}
	current, err := q.GetDesiredState(ctx, generated.GetDesiredStateParams{TenantID: scope.tenant, ResourceID: m.ResourceID})
	before := int64(0)
	if err == nil {
		before = current.Revision
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return zero, err
	}
	if before != m.ExpectedRevision {
		return zero, ErrConflict
	}
	if _, err = q.PutDesiredState(ctx, generated.PutDesiredStateParams{TenantID: scope.tenant, ResourceID: m.ResourceID, TargetKind: m.TargetKind, Revision: m.Revision, Payload: m.Payload, PayloadSha256: digest[:], ExpectedRevision: m.ExpectedRevision}); err != nil {
		return zero, err
	}
	id := uuid.NewString()
	op, err := q.InsertOperation(ctx, generated.InsertOperationParams{ID: id, TenantID: scope.tenant, ResourceID: m.ResourceID, IdempotencyKey: m.IdempotencyKey, RequestSha256: requestDigest[:], TargetKind: m.TargetKind, TargetRevision: m.Revision, ExpectedRevision: m.ExpectedRevision, Generation: m.Generation, Payload: m.Payload})
	if err != nil {
		return zero, err
	}
	if err = q.InsertTask(ctx, generated.InsertTaskParams{ID: id, TenantID: scope.tenant, OperationID: id}); err != nil {
		return zero, err
	}
	if err = q.InsertOutbox(ctx, generated.InsertOutboxParams{TenantID: scope.tenant, OperationID: id}); err != nil {
		return zero, err
	}
	beforeJSON := []byte(fmt.Sprintf(`{"revision":%d}`, before))
	afterJSON := []byte(fmt.Sprintf(`{"revision":%d}`, m.Revision))
	if err = q.InsertAudit(ctx, generated.InsertAuditParams{TenantID: scope.tenant, ActorID: scope.actor, Action: "desired_state.update", ResourceID: m.ResourceID, RequestID: m.RequestID, BeforeSummary: beforeJSON, AfterSummary: afterJSON, Reason: "desired state accepted"}); err != nil {
		return zero, err
	}
	return op, tx.Commit(ctx)
}

func (s *Tracer) Operation(ctx context.Context, scope TenantScope, id string) (generated.Operation, error) {
	if !scope.Valid() {
		return generated.Operation{}, ErrScope
	}
	return generated.New(s.Pool).GetOperation(ctx, generated.GetOperationParams{TenantID: scope.tenant, ID: id})
}

type Receipt struct {
	Revision, Generation int64
	State                string
	Payload              []byte
}

// Reconcile is called only after the adapter has authenticated and correlated
// the target. An unknown outcome never frees resource occupation.
func (s *Tracer) Reconcile(ctx context.Context, tenant, id string, r Receipt) error {
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	q := generated.New(tx)
	if _, err = q.LockTask(ctx, generated.LockTaskParams{TenantID: tenant, OperationID: id}); err != nil {
		return err
	}
	if err = s.ReconcileInTransaction(ctx, q, tenant, id, r); err != nil {
		return err
	}
	if err = q.ReconcileTask(ctx, generated.ReconcileTaskParams{TenantID: tenant, OperationID: id, State: r.State}); err != nil {
		return err
	}
	if r.State == "succeeded" || r.State == "failed" {
		if err = q.MarkDelivered(ctx, generated.MarkDeliveredParams{TenantID: tenant, OperationID: id}); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

// ReconcileInTransaction lets the lease worker commit its receipt, operation,
// task and Outbox outcome in one transaction after locking the leased task.
func (s *Tracer) ReconcileInTransaction(ctx context.Context, q *generated.Queries, tenant, id string, r Receipt) error {
	if r.State != "succeeded" && r.State != "failed" && r.State != "unknown" {
		return ErrStaleReceipt
	}
	if len(r.Payload) == 0 || len(r.Payload) > 1<<20 {
		return ErrStaleReceipt
	}
	op, err := q.LockOperation(ctx, generated.LockOperationParams{TenantID: tenant, ID: id})
	if err != nil {
		return err
	}
	if op.TargetRevision != r.Revision || op.Generation != r.Generation {
		return ErrStaleReceipt
	}
	digest := sha256.Sum256(r.Payload)
	old, err := q.GetReceipt(ctx, generated.GetReceiptParams{TenantID: tenant, OperationID: id})
	if err == nil {
		if old.State != r.State || !bytes.Equal(old.ReceiptSha256, digest[:]) {
			return ErrConflict
		}
		return nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	current, err := q.GetDesiredState(ctx, generated.GetDesiredStateParams{TenantID: tenant, ResourceID: op.ResourceID})
	if err != nil {
		return err
	}
	if current.Revision != r.Revision {
		return ErrStaleReceipt
	}
	if r.State == "unknown" {
		return q.UpdateOperationState(ctx, generated.UpdateOperationStateParams{TenantID: tenant, ID: id, State: "unknown"})
	}
	if err = q.InsertReceipt(ctx, generated.InsertReceiptParams{TenantID: tenant, OperationID: id, TargetRevision: r.Revision, Generation: r.Generation, ReceiptSha256: digest[:], State: r.State}); err != nil {
		return err
	}
	if err = q.UpdateOperationState(ctx, generated.UpdateOperationStateParams{TenantID: tenant, ID: id, State: r.State}); err != nil {
		return err
	}
	return nil
}
