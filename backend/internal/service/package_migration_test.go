package service_test

import (
	"bytes"
	"context"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"net/url"
	"os"
	"testing"
	"vortex/backend/db"
	"vortex/backend/internal/service"
	"vortex/backend/internal/testutil"
)

// Upgrade an actual 00005 authority store, preserving every row of every
// pre-existing tenant table. A private schema cannot disturb other executions.
func TestPackageMigrationUpgradeSeeded00005(t *testing.T) {
	admin := testutil.Postgres(t)
	ctx := context.Background()
	schema := "package_upgrade_" + uuid.NewString()[:8]
	if _, e := admin.Exec(ctx, "CREATE SCHEMA "+schema); e != nil {
		t.Fatal(e)
	}
	defer admin.Exec(ctx, "DROP SCHEMA "+schema+" CASCADE")
	u, e := url.Parse(os.Getenv("PHASE6_DATABASE_URL"))
	if e != nil {
		t.Fatal(e)
	}
	args := u.Query()
	args.Set("search_path", schema)
	u.RawQuery = args.Encode()
	if e = db.MigrateTo(ctx, u.String(), 5); e != nil {
		t.Fatal(e)
	}
	pool, e := pgxpool.New(ctx, u.String())
	if e != nil {
		t.Fatal(e)
	}
	defer pool.Close()
	own := scope(t)
	if _, e = pool.Exec(ctx, "INSERT INTO tenants(id) VALUES($1)", own.TenantID()); e != nil {
		t.Fatal(e)
	}
	tracer := service.Tracer{Pool: pool}
	op, e := tracer.Mutate(ctx, own, mutation())
	if e != nil {
		t.Fatal(e)
	}
	if e = tracer.Reconcile(ctx, own.TenantID(), op.ID, service.Receipt{Revision: 1, Generation: 1, State: "unknown", Payload: []byte("uncertain authority must survive upgrade")}); e != nil {
		t.Fatal(e)
	}
	rule := uuid.NewString()
	if _, e = pool.Exec(ctx, "INSERT INTO rule_heads(tenant_id,id,desired_revision,sync_status) VALUES($1,$2,1,'unknown')", own.TenantID(), rule); e != nil {
		t.Fatal(e)
	}
	if _, e = pool.Exec(ctx, "INSERT INTO rule_revisions(tenant_id,rule_id,revision,payload,payload_sha256,created_by,change_class) VALUES($1,$2,1,$3,$4,$5,'stop')", own.TenantID(), rule, []byte(`{"enabled":false}`), make([]byte, 32), own.ActorID()); e != nil {
		t.Fatal(e)
	}
	if _, e = pool.Exec(ctx, "INSERT INTO rule_targets(tenant_id,rule_id,revision,address,weight,priority) VALUES($1,$2,1,'192.0.2.1:443',1,0)", own.TenantID(), rule); e != nil {
		t.Fatal(e)
	}
	tables := []string{"desired_state", "operations", "tasks", "outbox", "audit_events", "control_receipts", "rule_heads", "rule_revisions", "rule_targets", "rule_segments", "rule_dependencies", "rule_compile_requests", "rule_compile_inputs", "rule_artifacts", "rule_artifact_commits", "port_allocations", "port_reservations", "release_proofs"}
	before := authoritySnapshot(t, pool, own.TenantID(), tables)
	if e = db.Migrate(ctx, u.String()); e != nil {
		t.Fatal(e)
	}
	if !bytes.Equal(before, authoritySnapshot(t, pool, own.TenantID(), tables)) {
		t.Fatal("00006 changed pre-existing authority or unknown observations")
	}
	var version int
	if e = pool.QueryRow(ctx, "SELECT max(version_id) FROM goose_db_version WHERE is_applied").Scan(&version); e != nil || version != 7 {
		t.Fatal(version, e)
	}
}
