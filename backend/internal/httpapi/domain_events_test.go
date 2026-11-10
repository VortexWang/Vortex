package httpapi_test

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"strings"
	"testing"
	"time"
	"vortex/backend/db/generated"
	"vortex/backend/internal/policy"
	"vortex/backend/internal/repository"
)

func TestDomainEventSameTransactionAuditRollbackAndDeferredVisibility(t *testing.T) {
	f := newAPI(t)
	ids := f.operations(t, 1)
	ctx := context.Background()
	scope := f.ownerScope(t)
	name := "events_audit_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if _, e := f.pool.Exec(ctx, `CREATE FUNCTION `+name+`() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.tenant_id='`+f.tenant+`' AND NEW.action='test.events.rollback' THEN RAISE EXCEPTION 'injected audit failure'; END IF; RETURN NEW; END $$; CREATE TRIGGER `+name+` BEFORE INSERT ON audit_events FOR EACH ROW EXECUTE FUNCTION `+name+`() `); e != nil {
		t.Fatal(e)
	}
	defer f.pool.Exec(ctx, `DROP TRIGGER `+name+` ON audit_events; DROP FUNCTION `+name+`() `)
	e := (repository.AuditWriter{Pool: f.pool}).WithTransaction(ctx, scope, policy.Mutate, &repository.Audit{Action: "test.events.rollback", ResourceID: ids[0], RequestID: "rollback", Reason: "exercise originating transaction"}, func(q *generated.Queries) error {
		if e := q.UpdateOperationState(ctx, generated.UpdateOperationStateParams{TenantID: f.tenant, ID: ids[0], State: "unknown"}); e != nil {
			return e
		}
		return q.InsertOutbox(ctx, generated.InsertOutboxParams{TenantID: f.tenant, OperationID: ids[0]})
	})
	if e == nil {
		t.Fatal("audit failure committed")
	}
	events := repository.DomainEvents{Pool: f.pool}
	page, e := events.Replay(ctx, scope, 0, 100, "after-rollback")
	if e != nil || page.LastSequence != 1 || len(page.Events) != 1 {
		t.Fatal("event escaped rollback", page, e)
	}
	var outbox, pending int
	var state string
	if e = f.pool.QueryRow(ctx, `SELECT count(*) FROM outbox WHERE tenant_id=$1`, f.tenant).Scan(&outbox); e != nil {
		t.Fatal(e)
	}
	if e = f.pool.QueryRow(ctx, `SELECT state FROM operations WHERE tenant_id=$1 AND id=$2`, f.tenant, ids[0]).Scan(&state); e != nil {
		t.Fatal(e)
	}
	if outbox != 0 || state != "pending" {
		t.Fatal("business/outbox escaped rollback", outbox, state)
	}
	tx, e := f.pool.Begin(ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer tx.Rollback(ctx)
	if _, e = tx.Exec(ctx, `UPDATE operations SET state='running' WHERE id=$1`, ids[0]); e != nil {
		t.Fatal(e)
	}
	if e = tx.QueryRow(ctx, `SELECT count(*) FROM domain_event_pending WHERE transaction_id=txid_current()`).Scan(&pending); e != nil || pending != 1 {
		t.Fatal(pending, e)
	}
	page, e = events.Replay(ctx, scope, 0, 100, "before-commit")
	if e != nil || page.LastSequence != 1 {
		t.Fatal("uncommitted event visible", page, e)
	}
	if e = tx.Commit(ctx); e != nil {
		t.Fatal(e)
	}
	page, e = (repository.DomainEvents{Pool: f.pool}).Replay(ctx, scope, 1, 100, "reopened")
	if e != nil || page.LastSequence != 2 || len(page.Events) != 1 || page.Events[0].Sequence != 2 {
		t.Fatal(page, e)
	}
	if e = f.pool.QueryRow(ctx, `SELECT count(*) FROM domain_event_pending WHERE tenant_id=$1`, f.tenant).Scan(&pending); e != nil || pending != 0 {
		t.Fatal("committed queue left behind", pending, e)
	}
	if _, e = f.pool.Exec(ctx, `UPDATE domain_events SET summary='{}' WHERE tenant_id=$1`, f.tenant); e == nil {
		t.Fatal("immutable event rewritten")
	}
	if _, e = f.pool.Exec(ctx, `DELETE FROM domain_events WHERE tenant_id=$1`, f.tenant); e == nil {
		t.Fatal("event removed without retained floor")
	}
}
func TestDomainEventLateCommitAndCrossTenantFlushOrder(t *testing.T) {
	f := newAPI(t)
	ids := f.operations(t, 2)
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	a, e := f.pool.Begin(ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer a.Rollback(context.Background())
	b, e := f.pool.Begin(ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer b.Rollback(context.Background())
	if _, e = a.Exec(ctx, `UPDATE operations SET state='unknown' WHERE id=$1`, ids[0]); e != nil {
		t.Fatal(e)
	}
	// No stream-head lock may block this unrelated business row before commit.
	if _, e = b.Exec(ctx, `UPDATE operations SET state='running' WHERE id=$1`, ids[1]); e != nil {
		t.Fatal("early stream lock", e)
	}
	if e = b.Commit(ctx); e != nil {
		t.Fatal(e)
	}
	if e = a.Commit(ctx); e != nil {
		t.Fatal(e)
	}
	page, e := (repository.DomainEvents{Pool: f.pool}).Replay(ctx, f.ownerScope(t), 2, 100, "commit-order")
	if e != nil || len(page.Events) != 2 || page.Events[0].ResourceID != ids[1] || page.Events[1].ResourceID != ids[0] || page.Events[0].Sequence != 3 || page.Events[1].Sequence != 4 {
		t.Fatal("late commit skipped/reordered", page, e)
	}
	other := newAPI(t)
	more := other.operations(t, 2)
	a, e = f.pool.Begin(ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer a.Rollback(context.Background())
	b, e = f.pool.Begin(ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer b.Rollback(context.Background())
	for _, id := range []string{ids[0], more[0]} {
		if _, e = a.Exec(ctx, `UPDATE operations SET state='failed' WHERE id=$1`, id); e != nil {
			t.Fatal(e)
		}
	}
	for _, id := range []string{more[1], ids[1]} {
		if _, e = b.Exec(ctx, `UPDATE operations SET state='failed' WHERE id=$1`, id); e != nil {
			t.Fatal(e)
		}
	}
	results := make(chan error, 2)
	go func() { results <- a.Commit(ctx) }()
	go func() { results <- b.Commit(ctx) }()
	for i := 0; i < 2; i++ {
		if e = <-results; e != nil {
			t.Fatal("cross-tenant commit lock cycle", e)
		}
	}
	page, e = (repository.DomainEvents{Pool: f.pool}).Replay(ctx, f.ownerScope(t), 0, 100, "contiguous")
	if e != nil || len(page.Events) != 6 {
		t.Fatal(page, e)
	}
	for i, event := range page.Events {
		if event.Sequence != int64(i+1) {
			t.Fatal("sequence hole", page)
		}
	}
	if _, e = (repository.DomainEvents{Pool: f.pool}).Prune(ctx, f.tenant, time.Now()); e == nil || errors.Is(e, context.Canceled) {
		t.Fatal("unsafe retention accepted", e)
	}
}

func TestDomainEventRetentionDeleteFailureRollsBackFloor(t *testing.T) {
	f := newAPI(t)
	f.operations(t, 2)
	ctx := context.Background()
	name := "retention_failure_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if _, e := f.pool.Exec(ctx, `CREATE FUNCTION `+name+`() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF OLD.tenant_id='`+f.tenant+`' THEN RAISE EXCEPTION 'injected retention failure'; END IF; RETURN OLD; END $$; CREATE TRIGGER `+name+` BEFORE DELETE ON domain_events FOR EACH ROW EXECUTE FUNCTION `+name+`() `); e != nil {
		t.Fatal(e)
	}
	defer f.pool.Exec(ctx, `DROP TRIGGER `+name+` ON domain_events; DROP FUNCTION `+name+`() `)
	events := repository.DomainEvents{Pool: f.pool, Clock: func() time.Time { return time.Now().Add(48 * time.Hour) }}
	if _, e := events.Prune(ctx, f.tenant, time.Now().Add(24*time.Hour)); e == nil {
		t.Fatal("delete failure ignored")
	}
	page, e := events.Replay(ctx, f.ownerScope(t), 0, 100, "retention-rollback")
	if e != nil || page.RetainedAfter != 0 || len(page.Events) != 2 {
		t.Fatal("retention floor escaped rollback", page, e)
	}
}
