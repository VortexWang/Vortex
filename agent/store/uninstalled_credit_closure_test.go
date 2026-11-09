package store

import (
	"context"
	"errors"
	"sync"
	"testing"
)

func TestUninstalledCreditClosurePermanentlyBlocksLateInstall(t *testing.T) {
	s, root, update, path := shareFixture(t)
	ctx := context.Background()
	expected := shareGrant(root, update)
	if err := s.InstallShareSourceAnchor(ctx, update.NodeID, shareID(90), root); err != nil {
		t.Fatal(err)
	}
	final, err := s.StopUninstalledCreditGrant(ctx, update.NodeID, update.BootID, expected)
	if err != nil || !final.NeverInstalled || final.Used != 0 || final.Unused != expected.AmountBytes {
		t.Fatalf("absent final=%+v %v", final, err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = Open(path, "agent")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	// The original valid grant can arrive after acknowledgement loss/restart.
	// Retain its history and source lineage, but never make it executable.
	if err := s.PersistAuthorizationUpdate(ctx, update, []AuthorizationRecord{expected}, nil); err != nil {
		t.Fatal(err)
	}
	use := AuthorizationUse{NodeID: update.NodeID, BootID: update.BootID, AuthorizationID: expected.AuthorizationID, Kind: expected.Kind, RuleID: expected.RuleID, RuleRevision: expected.RuleRevision, PathGeneration: expected.PathGeneration, BillingSnapshotID: expected.BillingSnapshotID, Earliest: root.NotBefore, Latest: root.NotBefore, TrustedTime: true}
	if _, err := s.CheckStoredAuthorization(ctx, use); !errors.Is(err, ErrAuthorizationDenied) {
		t.Fatalf("late install revived stopped grant: %v", err)
	}
	if err := s.PersistAuthorizationUpdate(ctx, update, []AuthorizationRecord{expected}, nil); err != nil {
		t.Fatalf("original duplicate update: %v", err)
	}
	replay, err := s.StopUninstalledCreditGrant(ctx, update.NodeID, update.BootID, expected)
	if err != nil || replay != final {
		t.Fatalf("late install changed zero-use witness: %+v %v", replay, err)
	}
	replay, err = s.FinalizeStoppedCreditGrant(ctx, update.NodeID, update.BootID, update.IssuerID, expected.AuthorizationID)
	if err != nil || replay != final {
		t.Fatalf("generic final changed absent witness: %+v %v", replay, err)
	}
	if _, err := s.FinalizeStoppedCreditGrant(ctx, update.NodeID, "wrong-boot", update.IssuerID, expected.AuthorizationID); !errors.Is(err, ErrAuthorizationDenied) {
		t.Fatalf("wrong boot replayed witness: %v", err)
	}
	if _, err := s.db.Exec(`DELETE FROM uninstalled_credit_grant_finals`); err == nil {
		t.Fatal("permanent absent witness deleted")
	}
}

func TestUninstalledCreditClosureCannotRewriteIssueScope(t *testing.T) {
	s, root, update, _ := shareFixture(t)
	ctx := context.Background()
	expected := shareGrant(root, update)
	if err := s.InstallShareSourceAnchor(ctx, update.NodeID, shareID(90), root); err != nil {
		t.Fatal(err)
	}
	if _, err := s.StopUninstalledCreditGrant(ctx, update.NodeID, update.BootID, expected); err != nil {
		t.Fatal(err)
	}
	changed := expected
	changed.AmountBytes++
	if _, err := s.StopUninstalledCreditGrant(ctx, update.NodeID, update.BootID, changed); !errors.Is(err, ErrAuthorizationUpdateConflict) {
		t.Fatalf("changed issue rewrote witness: %v", err)
	}
	if err := s.PersistAuthorizationUpdate(ctx, update, []AuthorizationRecord{changed}, nil); !errors.Is(err, ErrAuthorizationUpdateConflict) {
		t.Fatalf("changed late issue accepted: %v", err)
	}
	var count int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM authorization_records`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("changed issue left partial authority: %d %v", count, err)
	}
	if err := s.PersistAuthorizationUpdate(ctx, update, []AuthorizationRecord{expected}, nil); err != nil {
		t.Fatalf("failed mutation poisoned correct replay: %v", err)
	}
}

func TestUninstalledCreditClosureCannotEraseInstalledHistory(t *testing.T) {
	s, use := fundedStore(t)
	ctx := context.Background()
	r, err := s.CheckStoredAuthorization(ctx, use)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.StopUninstalledCreditGrant(ctx, use.NodeID, use.BootID, r); !errors.Is(err, ErrAuthorizationUpdateConflict) {
		t.Fatalf("installed grant reclassified as never installed: %v", err)
	}
	if _, err := s.ReserveAuthorizedUsage(ctx, closureReservation("valid", 1000), use); err != nil {
		t.Fatalf("rejected absence changed original authority: %v", err)
	}
}

func TestUninstalledCreditClosureWriteFailureIsAtomic(t *testing.T) {
	s, root, update, _ := shareFixture(t)
	ctx := context.Background()
	expected := shareGrant(root, update)
	if _, err := s.db.Exec(`CREATE TRIGGER fail_absent_final BEFORE INSERT ON uninstalled_credit_grant_finals BEGIN SELECT RAISE(ABORT,'disk failure'); END`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.StopUninstalledCreditGrant(ctx, update.NodeID, update.BootID, expected); err == nil {
		t.Fatal("failed absence transaction acknowledged")
	}
	for _, table := range []string{"credit_grant_stops", "authorization_revocations", "uninstalled_credit_grant_finals"} {
		var count int
		if err := s.db.QueryRow(`SELECT COUNT(*) FROM ` + table).Scan(&count); err != nil || count != 0 {
			t.Fatalf("partial %s escaped: %d %v", table, count, err)
		}
	}
	if _, err := s.db.Exec(`DROP TRIGGER fail_absent_final`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.StopUninstalledCreditGrant(ctx, update.NodeID, update.BootID, expected); err != nil {
		t.Fatalf("retry failed: %v", err)
	}
}

func TestUninstalledCreditClosureSerializesAnotherInstallerConnection(t *testing.T) {
	for range 8 {
		s, root, update, path := shareFixture(t)
		ctx := context.Background()
		expected := shareGrant(root, update)
		if err := s.InstallShareSourceAnchor(ctx, update.NodeID, shareID(90), root); err != nil {
			t.Fatal(err)
		}
		other, err := Open(path, "agent")
		if err != nil {
			t.Fatal(err)
		}
		start := make(chan struct{})
		var wg sync.WaitGroup
		var stopped CreditGrantFinal
		var stopErr, installErr error
		wg.Go(func() {
			<-start
			stopped, stopErr = s.StopUninstalledCreditGrant(ctx, update.NodeID, update.BootID, expected)
		})
		wg.Go(func() {
			<-start
			installErr = other.PersistAuthorizationUpdate(ctx, update, []AuthorizationRecord{expected}, nil)
		})
		close(start)
		wg.Wait()
		if installErr != nil {
			t.Fatal(installErr)
		}
		if stopErr != nil && !errors.Is(stopErr, ErrAuthorizationUpdateConflict) {
			t.Fatal(stopErr)
		}
		use := AuthorizationUse{NodeID: update.NodeID, BootID: update.BootID, AuthorizationID: expected.AuthorizationID, Kind: expected.Kind, RuleID: expected.RuleID, RuleRevision: expected.RuleRevision, PathGeneration: expected.PathGeneration, BillingSnapshotID: expected.BillingSnapshotID, Earliest: root.NotBefore, Latest: root.NotBefore, TrustedTime: true}
		_, authorityErr := other.CheckStoredAuthorization(ctx, use)
		if stopErr == nil {
			if !stopped.NeverInstalled || !errors.Is(authorityErr, ErrAuthorizationDenied) {
				t.Fatalf("absence proof coexists with executable credit: %+v %v", stopped, authorityErr)
			}
		} else if authorityErr != nil {
			t.Fatalf("installed-first conflict changed authority: %v", authorityErr)
		}
		if err := other.Close(); err != nil {
			t.Fatal(err)
		}
	}
}
