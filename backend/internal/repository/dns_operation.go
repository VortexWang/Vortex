package repository

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"vortex/backend/db/generated"
)

var ErrDNSOperation = errors.New("DNS operation conflict")

// EnqueueDNS is used only inside the caller's AuditWriter transaction. Existing
// resource_change tasks keep this payload out of Agent RuntimeConfig delivery.
func EnqueueDNS(ctx context.Context, q *generated.Queries, s TenantScope, key string, payload any) (generated.Operation, error) {
	raw, e := json.Marshal(payload)
	if e != nil {
		return generated.Operation{}, e
	}
	digest := sha256.Sum256(raw)
	existing, e := q.GetOperationByKey(ctx, generated.GetOperationByKeyParams{TenantID: s.TenantID(), IdempotencyKey: "dns/" + key})
	if e == nil {
		if !bytes.Equal(existing.RequestSha256, digest[:]) {
			return generated.Operation{}, ErrDNSOperation
		}
		return existing, nil
	}
	if !errors.Is(e, pgx.ErrNoRows) {
		return generated.Operation{}, e
	}
	id := uuid.NewString()
	if _, e = q.PutDesiredState(ctx, generated.PutDesiredStateParams{TenantID: s.TenantID(), ResourceID: id, TargetKind: "node", Revision: 1, ExpectedRevision: 0, Payload: raw, PayloadSha256: digest[:]}); e != nil {
		return generated.Operation{}, e
	}
	op, e := q.InsertOperation(ctx, generated.InsertOperationParams{ID: id, TenantID: s.TenantID(), ResourceID: id, IdempotencyKey: "dns/" + key, RequestSha256: digest[:], TargetKind: "node", TargetRevision: 1, Generation: 1, Payload: raw})
	if e != nil {
		return generated.Operation{}, e
	}
	if e = q.InsertDomainTask(ctx, generated.InsertDomainTaskParams{ID: id, TenantID: s.TenantID(), OperationID: id, TaskKind: "resource_change"}); e != nil {
		return generated.Operation{}, e
	}
	e = q.InsertOutbox(ctx, generated.InsertOutboxParams{TenantID: s.TenantID(), OperationID: id})
	return op, e
}
