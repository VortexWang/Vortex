package service_test

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"testing"
	"vortex/backend/internal/service"
)

// Historical migration fixtures use the actual historical SQL surface rather
// than current generated RETURNING columns against an older schema.
func seedHistoricalAuthority(t *testing.T, pool *pgxpool.Pool, s service.TenantScope, state string) {
	t.Helper()
	ctx := context.Background()
	m := mutation()
	id := uuid.NewString()
	digest := sha256.Sum256(m.Payload)
	tx, e := pool.Begin(ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer tx.Rollback(ctx)
	exec := func(sql string, args ...any) {
		if _, e = tx.Exec(ctx, sql, args...); e != nil {
			t.Fatal(e)
		}
	}
	exec(`INSERT INTO desired_state(tenant_id,resource_id,target_kind,revision,payload,payload_sha256) VALUES($1,$2,'node',1,$3,$4)`, s.TenantID(), m.ResourceID, m.Payload, digest[:])
	exec(`INSERT INTO operations(id,tenant_id,resource_id,idempotency_key,request_sha256,target_kind,target_revision,expected_revision,generation,payload,state) VALUES($1,$2,$3,$1,$4,'node',1,0,1,$5,$6)`, id, s.TenantID(), m.ResourceID, digest[:], m.Payload, state)
	exec(`INSERT INTO tasks(id,tenant_id,operation_id,state) VALUES($1,$2,$1,$3)`, id, s.TenantID(), state)
	exec(`INSERT INTO outbox(tenant_id,operation_id,delivered_at) VALUES($1,$2,CASE WHEN $3::text='succeeded' THEN now() ELSE NULL END)`, s.TenantID(), id, state)
	exec(`INSERT INTO audit_events(tenant_id,actor_id,action,resource_id,request_id,before_summary,after_summary,reason) VALUES($1,$2,'desired_state.update',$3,$4,'{}','{"revision":1}','historical seeded authority')`, s.TenantID(), s.ActorID(), m.ResourceID, m.RequestID)
	exec(`INSERT INTO control_receipts(tenant_id,operation_id,target_revision,generation,receipt_sha256,state) VALUES($1,$2,1,1,$3,$4)`, s.TenantID(), id, digest[:], state)
	if e = tx.Commit(ctx); e != nil {
		t.Fatal(e)
	}
}
func prePublicOrderSnapshot(t *testing.T, pool *pgxpool.Pool, tenant string, tables []string) []byte {
	t.Helper()
	var snapshot map[string][]map[string]any
	if e := json.Unmarshal(authoritySnapshot(t, pool, tenant, tables), &snapshot); e != nil {
		t.Fatal(e)
	}
	for _, op := range snapshot["operations"] {
		if added, ok := op["public_created_at"]; ok {
			if added != op["created_at"] {
				t.Fatal("public order did not preserve historical creation", op)
			}
			delete(op, "public_created_at")
		}
	}
	raw, e := json.Marshal(snapshot)
	if e != nil {
		t.Fatal(e)
	}
	return raw
}
