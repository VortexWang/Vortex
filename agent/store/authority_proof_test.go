package store

import (
	"context"
	"testing"
	"time"
)

func TestFundingProofReuseStillChecksTimeAndNextTransactionRevocation(t *testing.T) {
	s, use, _ := runtimeStore(t)
	ctx := context.Background()
	use.Kind, use.AuthorizationID, use.BillingSnapshotID = "credit_grant", "grant", "snapshot"
	use.ExistingSession = true
	tx, err := s.beginRuntimeTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	tx.authorityReads = make(map[[2]string]authorityRead)
	if _, err := checkAuthorizationTx(ctx, tx, use); err != nil {
		t.Fatal(err)
	}
	if len(tx.authorityLineage) != 1 {
		t.Fatal("relational proof was not retained")
	}
	expired := use
	expired.Latest = expired.Latest.Add(10 * time.Minute)
	if _, err := checkAuthorizationTx(ctx, tx, expired); err == nil {
		t.Fatal("relational proof bypassed time boundary")
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec("UPDATE authorization_records SET state='revoked' WHERE authorization_id='grant'"); err != nil {
		t.Fatal(err)
	}
	next, err := s.beginRuntimeTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer next.Rollback()
	next.authorityReads = make(map[[2]string]authorityRead)
	if _, err := checkAuthorizationTx(ctx, next, use); err == nil {
		t.Fatal("prior transaction proof bypassed revocation")
	}
}
