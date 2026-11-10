package repository

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"time"
	"vortex/backend/db/generated"
	"vortex/backend/internal/policy"
)

type DomainEvent struct {
	Sequence   int64           `json:"sequence"`
	Type       string          `json:"type"`
	ResourceID string          `json:"resource_id"`
	Revision   int64           `json:"revision"`
	Summary    json.RawMessage `json:"summary"`
}
type DomainEventPage struct {
	LastSequence, RetainedAfter int64
	Events                      []DomainEvent
}
type DomainEvents struct {
	Pool  *pgxpool.Pool
	Clock func() time.Time
}

func (r DomainEvents) Replay(ctx context.Context, s TenantScope, after int64, limit int, request string) (DomainEventPage, error) {
	result := DomainEventPage{Events: []DomainEvent{}}
	if !s.Allows(policy.Read) || after < 0 || limit < 1 || limit > 100 {
		return result, policy.ErrForbidden
	}
	read := func(q *generated.Queries) error {
		// Bounds and rows share a single MVCC snapshot, including concurrent pruning.
		page, e := q.ReadDomainEventPage(ctx, generated.ReadDomainEventPageParams{TenantID: s.TenantID(), AfterSequence: after, PageLimit: int32(limit)})
		if e != nil {
			return e
		}
		result.LastSequence, result.RetainedAfter = page.LastSequence, page.RetainedAfter
		return json.Unmarshal(page.Events, &result.Events)
	}
	if s.Platform() {
		e := (AuditWriter{Pool: r.Pool}).WithTransaction(ctx, s, policy.Read, &Audit{Action: "events.replay", ResourceID: s.TenantID(), RequestID: request, Reason: "platform tenant domain event replay"}, read)
		return result, e
	}
	e := read(generated.New(r.Pool))
	return result, e
}

// Prune keeps a contiguous prefix floor in the same transaction as deletion.
// It is an internal maintenance entrypoint, never a request-selected cursor.
func (r DomainEvents) Prune(ctx context.Context, tenant string, cutoff time.Time) (int64, error) {
	now := time.Now()
	if r.Clock != nil {
		now = r.Clock()
	}
	if tenant == "" || cutoff.IsZero() || now.IsZero() || cutoff.After(now.Add(-24*time.Hour)) {
		return 0, errors.New("at least24h domain event retention required")
	}
	var deleted int64
	e := pgx.BeginTxFunc(ctx, r.Pool, pgx.TxOptions{}, func(tx pgx.Tx) error {
		q := generated.New(tx)
		stream, e := q.LockDomainEventStream(ctx, tenant)
		if errors.Is(e, pgx.ErrNoRows) {
			return nil
		}
		if e != nil {
			return e
		}
		floor, e := q.RetentionEventFloor(ctx, generated.RetentionEventFloorParams{TenantID: tenant, Cutoff: pgtype.Timestamptz{Time: cutoff, Valid: true}, LastSequence: stream.LastSequence})
		if e != nil {
			return e
		}
		if e = q.AdvanceEventRetention(ctx, generated.AdvanceEventRetentionParams{TenantID: tenant, RetainedAfter: floor}); e != nil {
			return e
		}
		deleted, e = q.DeleteRetainedDomainEvents(ctx, tenant)
		return e
	})
	return deleted, e
}
