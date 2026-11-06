package phase5_test

import (
	"bytes"
	"context"
	"database/sql"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"vortex/agent/store"
	"vortex/engine/vlink"
)

// Observe live WAL files with the same bounded writer wait as Store. The
// pragma is connection-local, so apply it in the DSN rather than a pooled Exec.
func openProcessLedger(path string) (*sql.DB, error) {
	db, err := sql.Open("sqlite", path+"?_pragma=busy_timeout(5000)")
	if err == nil {
		db.SetMaxOpenConns(1)
	}
	return db, err
}

func killProcess(t *testing.T, child *exec.Cmd) {
	t.Helper()
	if err := child.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	if err := child.Wait(); err == nil {
		t.Fatal("SIGKILL unexpectedly exited successfully")
	}
}

func awaitRenewalFault(t *testing.T, ctx context.Context, ready func() bool, label string) {
	t.Helper()
	for !ready() {
		select {
		case <-ctx.Done():
			t.Fatalf("renewal fault never reached %s: %v", label, ctx.Err())
		case <-time.After(5 * time.Millisecond):
		}
	}
}

// All mutations below are explicit test fixtures at a local adapter/storage
// boundary. Neither a remote peer nor the production CLI can install triggers.
// Returning true means this negative scenario has completed its assertions.
func schedulerRenewalFault(t *testing.T, ctx context.Context, db *sql.DB, dir, mode string, stream *vlink.MuxStream, exchange func(string), restart func()) bool {
	t.Helper()
	if mode == "low-water-tcp-udp" || mode == "lost-result" {
		return false
	}
	ns, err := store.Open(filepath.Join(dir, "agent.db"), "agent")
	if err != nil {
		t.Fatal(err)
	}
	defer ns.Close()
	hs, err := store.Open(filepath.Join(dir, "hub0.db"), "hub")
	if err != nil {
		t.Fatal(err)
	}
	defer hs.Close()
	awaitRenewalFault(t, ctx, func() bool {
		b, err := ns.AuditAuthorizationSource(ctx, regionalNode, regionalFirst)
		return err == nil && b.Committed == 6144 && b.Reserved == 0
	}, "settled initial traffic")
	var delegation string
	if err := db.QueryRowContext(ctx, `SELECT delegation_id FROM authorization_records WHERE kind='credit_grant' LIMIT 1`).Scan(&delegation); err != nil {
		t.Fatal(err)
	}

	if mode == "hub-restart" || mode == "node-restart" {
		// The Hub commits the terminal outcome but holds every reply. This is
		// a deterministic crash point, not a race against the one-second retry.
		hdb, err := openProcessLedger(filepath.Join(dir, "hub0.db"))
		if err != nil {
			t.Fatal(err)
		}
		defer hdb.Close()
		var requestID string
		awaitRenewalFault(t, ctx, func() bool {
			return hdb.QueryRowContext(ctx, `SELECT request_id FROM issuer_renewals LIMIT 1`).Scan(&requestID) == nil
		}, "committed result before SIGKILL")
		before, err := ns.RenewalIntent(ctx, requestID)
		if err != nil || before.State != "pending" || before.Result != nil {
			t.Fatalf("crash barrier lost pending request: %+v %v", before, err)
		}
		issued, err := hs.AuditHubDelegation(ctx, delegation)
		if err != nil || issued.Initial != 65536 || issued.Issued != 16384 || issued.Remaining != 49152 {
			t.Fatalf("pre-crash issuer conservation: %+v %v", issued, err)
		}
		if mode == "node-restart" {
			// Model a write whose reservation committed before SIGKILL but whose
			// outcome never reached the durable settlement boundary.
			var grant string
			if err := db.QueryRowContext(ctx, `SELECT authorization_id FROM authorization_records WHERE kind='credit_grant' LIMIT 1`).Scan(&grant); err != nil {
				t.Fatal(err)
			}
			r := before.Request
			stamp := time.Now().UTC()
			_, err := ns.ReserveAuthorizedUsage(ctx, store.UsageReservation{NodeID: r.NodeId, BootID: r.BootId, BatchID: "renewal-crash-write", AuthorizationID: grant, SnapshotID: r.BillingSnapshotId, FlowKey: "crash-fixture", RuleID: r.RuleId, Direction: "egress", Reserved: 512}, store.AuthorizationUse{NodeID: r.NodeId, BootID: r.BootId, AuthorizationID: grant, Kind: "credit_grant", RuleID: r.RuleId, RuleRevision: r.RuleRevision, PathGeneration: r.PathGeneration, BillingSnapshotID: r.BillingSnapshotId, Earliest: stamp, Latest: stamp, TrustedTime: true, ExistingSession: true})
			if err != nil {
				t.Fatal(err)
			}
		}
		restart()
		if mode == "hub-restart" {
			awaitRenewalFault(t, ctx, func() bool {
				after, err := ns.RenewalIntent(ctx, requestID)
				return err == nil && after.State == "installed" && bytes.Equal(after.Request.RequestSha256, before.Request.RequestSha256)
			}, "same request installed after Hub restart")
			t.Logf("SIGKILL Hub: original request %s replayed without another debit", requestID)
			return false // Continue the same TCP stream and UDP conservation test.
		}
		awaitRenewalFault(t, ctx, func() bool {
			var boot string
			return db.QueryRowContext(ctx, `SELECT event_id FROM durable_events WHERE kind='boot' AND event_id<>? LIMIT 1`, "boot-"+before.Request.BootId).Scan(&boot) == nil
		}, "fresh Agent boot while issuer remains offline")
		after, err := ns.RenewalIntent(ctx, requestID)
		if err != nil || after.State != "pending" || after.Request.BootId != before.Request.BootId || !bytes.Equal(after.Request.RequestSha256, before.Request.RequestSha256) {
			t.Fatalf("new boot changed unresolved renewal: %+v %v", after, err)
		}
		var active int
		if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM authorization_records WHERE state='active'`).Scan(&active); err != nil || active != 0 {
			t.Fatalf("new boot reused old authority: %d %v", active, err)
		}
		balance, err := ns.AuditAuthorizationSource(ctx, regionalNode, regionalFirst)
		if err != nil || balance.Initial != 8192 || balance.Committed != 6144 || balance.Remaining != 1536 || balance.Unknown != 512 || balance.Reserved != 0 {
			t.Fatalf("restart released or invented Agent funding: %+v %v", balance, err)
		}
		var upper, liability, origin string
		if err := db.QueryRowContext(ctx, `SELECT unknown_upper_bound,liability_state,origin_boot_id FROM unknown_usage_batches WHERE batch_id='renewal-crash-write'`).Scan(&upper, &liability, &origin); err != nil || upper != "512" || liability != "unresolved" || origin != before.Request.BootId {
			t.Fatalf("restart lost original write debt: %s %s %s %v", upper, liability, origin, err)
		}
		afterIssue, err := hs.AuditHubDelegation(ctx, delegation)
		if err != nil || afterIssue != issued {
			t.Fatalf("restart released unacknowledged issuer debit: %+v %v", afterIssue, err)
		}
		t.Log("SIGKILL Agent: new boot fenced old authority; original pending request and issuer debit retained")
		return true
	}

	if mode == "quota-exhausted" {
		var requestID string
		awaitRenewalFault(t, ctx, func() bool {
			return db.QueryRowContext(ctx, `SELECT request_id FROM renewal_requests WHERE state='rejected' LIMIT 1`).Scan(&requestID) == nil
		}, "durable quota refusal")
		intent, err := ns.RenewalIntent(ctx, requestID)
		if err != nil || intent.Result == nil || intent.Result.Code != "quota_exhausted" {
			t.Fatalf("wrong exhausted result: %+v %v", intent, err)
		}
		for range 2 { // Only the remaining 2048 bytes can be consumed.
			exchange(strings.Repeat("f", 512))
		}
		awaitRenewalFault(t, ctx, func() bool {
			b, err := ns.AuditAuthorizationSource(ctx, regionalNode, regionalFirst)
			return err == nil && b.Committed == 8192 && b.Reserved == 0
		}, "fully consumed original credit")
	} else if mode == "write-failure" {
		if _, err := db.ExecContext(ctx, `CREATE TRIGGER test_reservation_write_failure BEFORE INSERT ON usage_reservations BEGIN SELECT RAISE(ABORT,'injected durable write failure'); END`); err != nil {
			t.Fatal(err)
		}
	} else {
		t.Fatalf("unhandled renewal fault %s", mode)
	}
	if err := stream.WriteData(ctx, []byte("must-not-forward")); err != nil {
		t.Fatal(err)
	}
	frame, err := stream.Receive(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer frame.Release()
	if frame.Type != vlink.FrameRST {
		t.Fatalf("unfunded/unrecordable write was forwarded: type=%v payload=%q", frame.Type, frame.Payload)
	}
	balance, err := ns.AuditAuthorizationSource(ctx, regionalNode, regionalFirst)
	if err != nil || balance.Initial != balance.Remaining+balance.Committed+balance.Unknown+balance.Reserved {
		t.Fatalf("fault lost source conservation: %+v %v", balance, err)
	}
	if mode == "quota-exhausted" && (balance.Initial != 8192 || balance.Committed != 8192 || balance.Remaining != 0) {
		t.Fatalf("refusal minted or released credit: %+v", balance)
	}
	if mode == "write-failure" && (balance.Committed != 6144 || balance.Remaining != balance.Initial-6144 || balance.Unknown != 0 || balance.Reserved != 0) {
		t.Fatalf("failed pre-write consumed or refunded funding: %+v", balance)
	}
	t.Logf("%s stopped live TCP writes; source remains conserved: %+v", mode, balance)
	return true
}
