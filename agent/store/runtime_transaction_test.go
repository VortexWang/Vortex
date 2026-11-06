package store

import (
	"context"
	"errors"
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
