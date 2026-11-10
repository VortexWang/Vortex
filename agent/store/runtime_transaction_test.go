package store

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestPreparedAuthorizationReadsTransactionState(t *testing.T) {
	s, use, _ := runtimeStore(t)
	ctx := context.Background()
	use.AuthorizationID, use.Kind, use.BillingSnapshotID = "grant", "credit_grant", "snapshot"
	if _, err := s.CheckStoredAuthorization(ctx, use); err != nil {
		t.Fatal(err)
	}
	for _, commit := range []bool{false, true} {
		tx, err := s.beginRuntimeTx(ctx, nil)
		if err != nil {
			t.Fatal(err)
		}
		defer tx.Rollback()
		if _, err := tx.ExecContext(ctx, `UPDATE authorization_records SET state='revoked' WHERE node_id=? AND authorization_id=?`, "node", "grant"); err != nil {
			t.Fatal(err)
		}
		if _, err := checkAuthorizationTx(ctx, tx, use); !errors.Is(err, ErrAuthorizationDenied) {
			t.Fatalf("prepared read missed own transaction revocation: %v", err)
		}
		if commit {
			err = tx.Commit()
		} else {
			err = tx.Rollback()
		}
		if err != nil {
			t.Fatal(err)
		}
		_, err = s.CheckStoredAuthorization(ctx, use)
		if commit && !errors.Is(err, ErrAuthorizationDenied) || !commit && err != nil {
			t.Fatalf("commit=%v, next transaction saw incorrect state: %v", commit, err)
		}
	}
}

func TestRuntimeSessionIndexUpgradePreservesOccupiedSlots(t *testing.T) {
	s, scope, path := runtimeStore(t)
	ctx := context.Background()
	if _, err := s.OpenRuntimeSession(ctx, scope, "first", "first-flow"); err != nil {
		t.Fatal(err)
	}
	// Emulate the prior schema with a live occupied slot, then reopen it.
	if _, err := s.db.ExecContext(ctx, "DROP INDEX runtime_sessions_open_source"); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	upgraded, err := Open(path, "agent")
	if err != nil {
		t.Fatal(err)
	}
	defer upgraded.Close()
	var id, parent, unused int
	var plan string
	err = upgraded.db.QueryRowContext(ctx, `EXPLAIN QUERY PLAN SELECT COUNT(*) FROM runtime_sessions WHERE node_id=? AND source_id=? AND state='open'`, "node", "share-source").Scan(&id, &parent, &unused, &plan)
	if err != nil || !strings.Contains(plan, "runtime_sessions_open_source") {
		t.Fatalf("active source count still scans session history: %q, %v", plan, err)
	}
	if _, err := upgraded.OpenRuntimeSession(ctx, scope, "second", "second-flow"); !errors.Is(err, ErrAuthorizationDenied) {
		t.Fatalf("upgrade lost occupied slot: %v", err)
	}
	if err := upgraded.CloseRuntimeSession(ctx, "node", "boot", "first"); err != nil {
		t.Fatal(err)
	}
	if _, err := upgraded.OpenRuntimeSession(ctx, scope, "second", "second-flow"); err != nil {
		t.Fatalf("closed slot not reusable: %v", err)
	}
}

func TestPreparedAuthorizationCancellationDoesNotPoisonReuse(t *testing.T) {
	s, use, _ := runtimeStore(t)
	ctx := context.Background()
	use.AuthorizationID, use.Kind, use.BillingSnapshotID = "grant", "credit_grant", "snapshot"
	tx, err := s.beginRuntimeTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if _, _, err := readAuthorizationTx(cancelled, tx, "node", "grant"); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled read returned %v", err)
	}
	if _, err := checkAuthorizationTx(ctx, tx, use); err != nil {
		t.Fatalf("cancelled binding poisoned current transaction: %v", err)
	}
	if err := tx.Rollback(); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CheckStoredAuthorization(ctx, use); err != nil {
		t.Fatalf("statement unusable in fresh transaction: %v", err)
	}
}

func TestPreparedNodeShareBindingSeesSavepointChanges(t *testing.T) {
	s, use, _ := runtimeStore(t)
	ctx := context.Background()
	tx, err := s.beginRuntimeTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	for i := 0; i < 2; i++ {
		if _, bound, err := nodeShareTx(ctx, tx, use, "rate"); err != nil || bound {
			t.Fatalf("rolled-back binding retained: bound=%v err=%v", bound, err)
		}
		if _, err := tx.ExecContext(ctx, "SAVEPOINT funding_member"); err != nil {
			t.Fatal(err)
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO node_share_bindings(node_id,rule_id,tenant_id) VALUES(?,?,?)`, use.NodeID, use.RuleID, "tenant"); err != nil {
			t.Fatal(err)
		}
		// A newly bound platform rule without its capacity head must fail closed,
		// even when the same prepared query previously found no binding.
		if _, bound, err := nodeShareTx(ctx, tx, use, "rate"); !bound || !errors.Is(err, ErrAuthorizationDenied) {
			t.Fatalf("new binding bypassed: bound=%v err=%v", bound, err)
		}
		if _, err := tx.ExecContext(ctx, "ROLLBACK TO funding_member"); err != nil {
			t.Fatal(err)
		}
		if _, err := tx.ExecContext(ctx, "RELEASE funding_member"); err != nil {
			t.Fatal(err)
		}
	}
}
