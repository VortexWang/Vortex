package service_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"os"
	"os/exec"
	"testing"
	"time"
	"vortex/backend/db/generated"
	"vortex/backend/internal/service"
)

// The child runs the real production mutation. Parent kills it only after PG
// observes its audit trigger, proving the ledger mutation is uncommitted.
func TestUsagePowerLossTransactionChild(t *testing.T) {
	if os.Getenv("VORTEX_LEDGER_CRASH_CHILD") != "1" {
		return
	}
	pool, e := pgxpool.New(context.Background(), os.Getenv("PHASE6_DATABASE_URL"))
	if e != nil {
		t.Fatal(e)
	}
	defer pool.Close()
	scope, e := service.NewTenantScope(os.Getenv("VORTEX_LEDGER_ACTOR"), os.Getenv("VORTEX_LEDGER_TENANT"), os.Getenv("VORTEX_LEDGER_TENANT"))
	if e != nil {
		t.Fatal(e)
	}
	proof := sha256.Sum256([]byte("powerloss-proof"))
	e = (service.MeteringService{Pool: pool}).CommitActualUsage(context.Background(), scope, os.Getenv("VORTEX_LEDGER_GRANT"), os.Getenv("VORTEX_LEDGER_SNAPSHOT"), "powerloss", 123, proof[:], os.Getenv("VORTEX_LEDGER_REQUEST"))
	if e != nil {
		t.Fatal(e)
	}
}
func TestUsageFaultPowerLossAuditTransactionRollback(t *testing.T) {
	f, source, snapshot := ledgerFixture(t)
	ctx := context.Background()
	r := reserveUsage(t, f, source, snapshot, 1000)
	request := uuid.NewString()
	function := "crash_ledger_" + uuid.NewString()[:8]
	sql := fmt.Sprintf(`CREATE FUNCTION %s() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.request_id='%s' THEN PERFORM pg_sleep(30); END IF; RETURN NEW; END $$; CREATE TRIGGER %s BEFORE INSERT ON audit_events FOR EACH ROW EXECUTE FUNCTION %s()`, function, request, function, function)
	if _, e := f.pool.Exec(ctx, sql); e != nil {
		t.Fatal(e)
	}
	defer f.pool.Exec(ctx, "DROP TRIGGER "+function+" ON audit_events;DROP FUNCTION "+function+"()")
	tables := []string{"settlements", "ledger_events", "ledger_projections", "audit_events"}
	before := authoritySnapshot(t, f.pool, f.tenant, tables)
	child := exec.Command(os.Args[0], "-test.run=^TestUsagePowerLossTransactionChild$", "-test.v")
	child.Env = append(os.Environ(), "VORTEX_LEDGER_CRASH_CHILD=1", "VORTEX_LEDGER_ACTOR="+f.scope.ActorID(), "VORTEX_LEDGER_TENANT="+f.tenant, "VORTEX_LEDGER_GRANT="+r.ID, "VORTEX_LEDGER_SNAPSHOT="+snapshot, "VORTEX_LEDGER_REQUEST="+request)
	var output bytes.Buffer
	child.Stdout = &output
	child.Stderr = &output
	if e := child.Start(); e != nil {
		t.Fatal(e)
	}
	reached := false
	end := time.Now().Add(10 * time.Second)
	for time.Now().Before(end) {
		var count int
		e := f.pool.QueryRow(ctx, "SELECT count(*) FROM pg_stat_activity WHERE datname=current_database() AND wait_event='PgSleep'").Scan(&count)
		if e != nil {
			child.Process.Kill()
			child.Wait()
			t.Fatal(e)
		}
		if count > 0 {
			reached = true
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if e := child.Process.Kill(); e != nil {
		child.Wait()
		t.Fatal(e)
	}
	child.Wait()
	if !reached {
		t.Fatal("child never reached actual audit transaction", output.String())
	}
	// Closing the client process leaves the server in the trigger until its next
	// cancellation point. Terminate that backend to model abrupt connection loss.
	if _, e := f.pool.Exec(ctx, "SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE datname=current_database() AND wait_event='PgSleep'"); e != nil {
		t.Fatal(e)
	}
	if !bytes.Equal(before, authoritySnapshot(t, f.pool, f.tenant, tables)) {
		t.Fatal("power loss split ledger from audit")
	}
	proof := sha256.Sum256([]byte("powerloss-proof"))
	s := service.MeteringService{Pool: f.pool}
	if e := s.RecordUnknown(ctx, f.scope, r.ID, proof[:], time.Now(), uuid.NewString()); e != nil {
		t.Fatal(e)
	}
	p, e := generated.New(f.pool).GetSourceProjection(ctx, generated.GetSourceProjectionParams{TenantID: f.tenant, SourceID: source})
	if e != nil || p.F != 1000 || p.B != 10000 {
		t.Fatal("crash minted funds or lost liability", e, p)
	}
}
