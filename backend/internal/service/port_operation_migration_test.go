package service_test

import (
	"context"
	"net/url"
	"os"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"vortex/backend/db"
	"vortex/backend/internal/testutil"
)

func TestPortOperationMigrationPreservesHistoricalUnknownAndDoesNotInventAggregate(t *testing.T) {
	admin := testutil.Postgres(t)
	ctx := context.Background()
	schema := "port_upgrade_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if _, e := admin.Exec(ctx, "CREATE SCHEMA "+schema); e != nil {
		t.Fatal(e)
	}
	defer admin.Exec(ctx, "DROP SCHEMA "+schema+" CASCADE")
	u, e := url.Parse(os.Getenv("PHASE6_DATABASE_URL"))
	if e != nil {
		t.Fatal(e)
	}
	query := u.Query()
	query.Set("search_path", schema)
	u.RawQuery = query.Encode()
	if e = db.MigrateTo(ctx, u.String(), 8); e != nil {
		t.Fatal(e)
	}
	pool, e := pgxpool.New(ctx, u.String())
	if e != nil {
		t.Fatal(e)
	}
	defer pool.Close()
	tenant, group, allocation := uuid.NewString(), uuid.NewString(), uuid.NewString()
	for _, row := range []struct {
		sql  string
		args []any
	}{
		{`INSERT INTO tenants(id) VALUES($1)`, []any{tenant}},
		{`INSERT INTO resource_ownership(resource_id,resource_kind,owner_kind,owner_tenant_id) VALUES($1,'node_group','tenant',$2)`, []any{group, tenant}},
		{`INSERT INTO node_groups(id,name,purpose,scheduling,health_interval_seconds,health_timeout_seconds,billing_multiplier_milli) VALUES($1,'legacy group','entry','dns',10,2,0)`, []any{group}},
		{`INSERT INTO port_allocations(id,tenant_id,rule_id,group_id,member_set_version,port,listen_address,protocols,purpose,state) VALUES($1,$2,$3,$4,repeat('0',64),20000,'0.0.0.0',ARRAY['tcp'],'service','unknown')`, []any{allocation, tenant, uuid.NewString(), group}},
	} {
		if _, e = pool.Exec(ctx, row.sql, row.args...); e != nil {
			t.Fatal(e)
		}
	}
	snapshot := func() string {
		t.Helper()
		var raw string
		if e := pool.QueryRow(ctx, `SELECT jsonb_build_array((SELECT row_to_json(t) FROM tenants t WHERE id=$1),(SELECT row_to_json(g) FROM node_groups g WHERE id=$2),(SELECT row_to_json(a) FROM port_allocations a WHERE id=$3))::text`, tenant, group, allocation).Scan(&raw); e != nil {
			t.Fatal(e)
		}
		return raw
	}
	before := snapshot()
	if e = db.Migrate(ctx, u.String()); e != nil {
		t.Fatal(e)
	}
	if after := snapshot(); before != after {
		t.Fatal("upgrade rewrote historical unknown authority", before, after)
	}
	var bindings, projections int
	if e = pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM port_operation_bindings),(SELECT count(*) FROM port_operation_projection)`).Scan(&bindings, &projections); e != nil || bindings != 0 || projections != 0 {
		t.Fatal("upgrade invented accepted aggregate/proofs", bindings, projections, e)
	}
}
