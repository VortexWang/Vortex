package store

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"sync"
	"testing"

	"vortex/contracts"
)

func closureReservation(batch string, amount uint64) UsageReservation {
	return UsageReservation{AuthorizationID: "grant", NodeID: "node", BootID: "boot", BatchID: batch, SnapshotID: "snapshot", FlowKey: "flow", RuleID: "rule", Direction: "egress", Reserved: amount}
}

func TestCreditGrantClosureWaitsForEveryWritableTicket(t *testing.T) {
	s, use := fundedStore(t)
	ctx := context.Background()
	if _, err := s.FinalizeStoppedCreditGrant(ctx, "node", "boot", "hub-one", "grant"); !errors.Is(err, ErrAuthorizationDenied) {
		t.Fatalf("live grant produced final evidence: %v", err)
	}
	a, b := closureReservation("write", 1000), closureReservation("cancel", 128)
	for _, r := range []UsageReservation{a, b} {
		if _, err := s.ReserveAuthorizedUsage(ctx, r, use); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.StopCreditGrant(ctx, "node", "boot", "hub-one", "grant"); err != nil {
		t.Fatal(err)
	}
	// Even replaying an already issued ticket cannot grant a second write.
	for _, r := range []UsageReservation{a, closureReservation("new", 1)} {
		if _, err := s.ReserveAuthorizedUsage(ctx, r, use); !errors.Is(err, ErrAuthorizationDenied) {
			t.Fatalf("stopped grant issued writable ticket: %v", err)
		}
	}
	if _, err := s.FinalizeStoppedCreditGrant(ctx, "node", "boot", "hub-one", "grant"); !errors.Is(err, ErrCreditGrantUnsettled) {
		t.Fatalf("pending writes finalized: %v", err)
	}
	if err := s.SettleUsageReservation(ctx, a, "committed", 300, []byte("original usage"), 0); err != nil {
		t.Fatal(err)
	}
	if _, err := s.FinalizeStoppedCreditGrant(ctx, "node", "boot", "hub-one", "grant"); !errors.Is(err, ErrCreditGrantUnsettled) {
		t.Fatalf("one remaining writable ticket finalized: %v", err)
	}
	if err := s.SettleUsageReservation(ctx, b, "returned", 0, nil, 0); err != nil {
		t.Fatal(err)
	}
	final, err := s.FinalizeStoppedCreditGrant(ctx, "node", "boot", "hub-one", "grant")
	if err != nil || final.Used != 300 || final.Unused != 3796 || final.BatchCount != 2 || len(final.UsageSHA256) != 64 || final.Grant.SourceID != "source" {
		t.Fatalf("final=%+v err=%v", final, err)
	}
	sum := sha256.Sum256([]byte("original usage"))
	want, err := contracts.BuildKnownUsageInventory([]contracts.KnownUsageBatch{{BatchID: "write", SHA256: hex.EncodeToString(sum[:])}})
	if err != nil || final.KnownUsage != want {
		t.Fatalf("wire inventory included returned ticket or missed committed bytes: %+v %v", final.KnownUsage, err)
	}
	balance, err := s.AuditAuthorizationBalance(ctx, "node", "grant")
	if err != nil || balance.Remaining != 3796 || balance.Committed != 300 || balance.Reserved != 0 {
		t.Fatalf("closure changed source balance: %+v %v", balance, err)
	}
	if err := s.SettleUsageReservation(ctx, a, "committed", 301, []byte("different"), 0); !errors.Is(err, ErrUsageBatchConflict) {
		t.Fatalf("late mutation accepted: %v", err)
	}
	// Final evidence survives reopen and repeated stop without relying on time.
	var path string
	if err := s.db.QueryRow(`SELECT file FROM pragma_database_list WHERE name='main'`).Scan(&path); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(path, "agent")
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	if err := reopened.StopCreditGrant(ctx, "node", "boot", "hub-one", "grant"); err != nil {
		t.Fatal(err)
	}
	replay, err := reopened.FinalizeStoppedCreditGrant(ctx, "node", "boot", "hub-one", "grant")
	if err != nil || replay != final {
		t.Fatalf("reopen changed final: %+v %v", replay, err)
	}
	if _, err := reopened.ReserveAuthorizedUsage(ctx, closureReservation("after-reopen", 1), use); !errors.Is(err, ErrAuthorizationDenied) {
		t.Fatalf("reopen revived grant: %v", err)
	}
	for _, table := range []string{"credit_grant_stops", "credit_grant_finals"} {
		for _, query := range []string{"DELETE FROM " + table, "UPDATE " + table + " SET node_id='changed'"} {
			if _, err := reopened.db.Exec(query); err == nil {
				t.Fatalf("immutable closure altered: %s", query)
			}
		}
	}
}

func TestCreditGrantClosureHistoricalFinalDoesNotInventKnownUsageProof(t *testing.T) {
	s, _ := fundedStore(t)
	ctx := context.Background()
	if err := s.StopCreditGrant(ctx, "node", "boot", "hub-one", "grant"); err != nil {
		t.Fatal(err)
	}
	r, _, err := readAuthorizationTxForTest(s)
	if err != nil {
		t.Fatal(err)
	}
	// Simulate the immutable payload produced by the preceding candidate.
	old := CreditGrantFinal{NodeID: "node", Grant: r, Unused: 4096}
	raw, err := json.Marshal(old)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.db.Exec(`INSERT INTO credit_grant_finals(node_id,authorization_id,payload) VALUES('node','grant',?)`, raw); err != nil {
		t.Fatal(err)
	}
	got, err := s.FinalizeStoppedCreditGrant(ctx, "node", "boot", "hub-one", "grant")
	if err != nil || got != old || got.KnownUsage.Validate() == nil {
		t.Fatal("historical immutable final was silently upgraded", got, err)
	}
}

func readAuthorizationTxForTest(s *Store) (AuthorizationRecord, string, error) {
	tx, err := s.db.BeginTx(context.Background(), nil)
	if err != nil {
		return AuthorizationRecord{}, "", err
	}
	defer tx.Rollback()
	return readAuthorizationTx(context.Background(), tx, "node", "grant")
}

func TestCreditGrantClosureRejectsWrongScopeAndHubStore(t *testing.T) {
	s, use := fundedStore(t)
	ctx := context.Background()
	for _, ids := range [][4]string{{"node", "other", "hub-one", "grant"}, {"node", "boot", "other", "grant"}, {"other", "boot", "hub-one", "grant"}} {
		if err := s.StopCreditGrant(ctx, ids[0], ids[1], ids[2], ids[3]); err == nil {
			t.Fatalf("wrong identity stopped credit: %v", ids)
		}
	}
	if _, err := s.CheckStoredAuthorization(ctx, use); err != nil {
		t.Fatalf("wrong identity changed authority: %v", err)
	}
	h, err := Open(filepath.Join(t.TempDir(), "hub.db"), "hub")
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close()
	if err := h.StopCreditGrant(ctx, "node", "boot", "hub-one", "grant"); !errors.Is(err, ErrAuthorizationDenied) {
		t.Fatalf("Hub manufactured Node stop: %v", err)
	}
}

func TestCreditGrantClosureWriteFailuresRemainConservative(t *testing.T) {
	s, use := fundedStore(t)
	ctx := context.Background()
	if _, err := s.db.Exec(`CREATE TRIGGER fail_stop BEFORE INSERT ON authorization_revocations BEGIN SELECT RAISE(ABORT,'disk failure'); END`); err != nil {
		t.Fatal(err)
	}
	if err := s.StopCreditGrant(ctx, "node", "boot", "hub-one", "grant"); err == nil {
		t.Fatal("failed stop acknowledged")
	}
	var count int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM credit_grant_stops`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("partial stop escaped rollback: %d %v", count, err)
	}
	if _, err := s.CheckStoredAuthorization(ctx, use); err != nil {
		t.Fatalf("rolled back stop changed authority: %v", err)
	}
	if _, err := s.db.Exec(`DROP TRIGGER fail_stop; CREATE TRIGGER fail_final BEFORE INSERT ON credit_grant_finals BEGIN SELECT RAISE(ABORT,'disk failure'); END`); err != nil {
		t.Fatal(err)
	}
	if err := s.StopCreditGrant(ctx, "node", "boot", "hub-one", "grant"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.FinalizeStoppedCreditGrant(ctx, "node", "boot", "hub-one", "grant"); err == nil {
		t.Fatal("failed final acknowledged")
	}
	if _, err := s.CheckStoredAuthorization(ctx, use); !errors.Is(err, ErrAuthorizationDenied) {
		t.Fatalf("failed final undid durable stop: %v", err)
	}
	if _, err := s.db.Exec(`DROP TRIGGER fail_final`); err != nil {
		t.Fatal(err)
	}
	final, err := s.FinalizeStoppedCreditGrant(ctx, "node", "boot", "hub-one", "grant")
	if err != nil || final.Used != 0 || final.Unused != 4096 || final.BatchCount != 0 {
		t.Fatalf("retry=%+v %v", final, err)
	}
}

func TestCreditGrantClosureRetainsUnknownUpperBound(t *testing.T) {
	s, use := fundedStore(t)
	ctx := context.Background()
	r := closureReservation("uncertain", 1000)
	if _, err := s.ReserveAuthorizedUsage(ctx, r, use); err != nil {
		t.Fatal(err)
	}
	if err := s.StopCreditGrant(ctx, "node", "boot", "hub-one", "grant"); err != nil {
		t.Fatal(err)
	}
	if err := s.SettleUsageReservation(ctx, r, "unknown", 1000, []byte("unknown"), 1000); err != nil {
		t.Fatal(err)
	}
	if _, err := s.FinalizeStoppedCreditGrant(ctx, "node", "boot", "hub-one", "grant"); !errors.Is(err, ErrCreditGrantUnsettled) {
		t.Fatalf("unknown debt finalized: %v", err)
	}
	balance, err := s.AuditAuthorizationBalance(ctx, "node", "grant")
	if err != nil || balance.Unknown != 1000 || balance.Remaining != 3096 {
		t.Fatalf("unknown debt released: %+v %v", balance, err)
	}
}

func TestCreditGrantClosureSerializesConcurrentReservations(t *testing.T) {
	s, use := fundedStore(t)
	ctx := context.Background()
	start := make(chan struct{})
	var wg sync.WaitGroup
	var mu sync.Mutex
	var accepted []UsageReservation
	for i := range 24 {
		wg.Go(func() {
			<-start
			r := closureReservation(fmt.Sprintf("race-%02d", i), 64)
			_, err := s.ReserveAuthorizedUsage(ctx, r, use)
			if err == nil {
				mu.Lock()
				accepted = append(accepted, r)
				mu.Unlock()
			} else if !errors.Is(err, ErrAuthorizationDenied) {
				t.Errorf("reservation: %v", err)
			}
		})
	}
	wg.Go(func() {
		<-start
		if err := s.StopCreditGrant(ctx, "node", "boot", "hub-one", "grant"); err != nil {
			t.Errorf("stop: %v", err)
		}
	})
	close(start)
	wg.Wait()
	if len(accepted) > 0 {
		if _, err := s.FinalizeStoppedCreditGrant(ctx, "node", "boot", "hub-one", "grant"); !errors.Is(err, ErrCreditGrantUnsettled) {
			t.Fatalf("race released live ticket: %v", err)
		}
	}
	for _, r := range accepted {
		if err := s.SettleUsageReservation(ctx, r, "committed", 37, []byte(r.BatchID), 0); err != nil {
			t.Fatal(err)
		}
	}
	final, err := s.FinalizeStoppedCreditGrant(ctx, "node", "boot", "hub-one", "grant")
	if err != nil || final.Used != uint64(len(accepted))*37 || final.Unused != 4096-final.Used || final.BatchCount != uint64(len(accepted)) {
		t.Fatalf("concurrent final=%+v %v", final, err)
	}
}

func TestCreditGrantClosureRejectsCorruptUsageEvidence(t *testing.T) {
	for _, corruption := range []string{
		`UPDATE usage_batches SET payload=x'00'`,
		`UPDATE authorization_usage_holds SET state='returned'`,
		`UPDATE authorization_balances SET remaining_bytes='4096'`,
		`DELETE FROM usage_reservations`,
	} {
		t.Run(corruption, func(t *testing.T) {
			s, use := fundedStore(t)
			ctx := context.Background()
			r := closureReservation("write", 1000)
			if _, err := s.ReserveAuthorizedUsage(ctx, r, use); err != nil {
				t.Fatal(err)
			}
			if err := s.SettleUsageReservation(ctx, r, "committed", 300, []byte("usage"), 0); err != nil {
				t.Fatal(err)
			}
			if err := s.StopCreditGrant(ctx, "node", "boot", "hub-one", "grant"); err != nil {
				t.Fatal(err)
			}
			if _, err := s.db.Exec(corruption); err != nil {
				t.Fatal(err)
			}
			if _, err := s.FinalizeStoppedCreditGrant(ctx, "node", "boot", "hub-one", "grant"); err == nil {
				t.Fatal("corrupt inventory finalized")
			}
		})
	}
}
