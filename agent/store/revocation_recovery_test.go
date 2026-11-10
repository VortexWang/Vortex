package store

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

func olderRevocationFixture(t *testing.T) (*Store, AuthorizationUse, AuthorizationRecord) {
	t.Helper()
	s, use := fundedStore(t)
	root, err := s.CheckStoredAuthorization(context.Background(), use)
	if err != nil {
		t.Fatal(err)
	}
	next := root
	next.AuthorizationID = "current-credit"
	next.ParentAuthorizationID = "current-parent"
	next.SourceID = "current-source"
	next.PathGeneration = 2
	next.SignerEpoch = 2
	if err = s.PersistAuthorizationUpdate(context.Background(), AuthorizationUpdate{IssuerID: "hub-one", NodeID: use.NodeID, BootID: use.BootID, UpdateID: "current-path", Payload: []byte("current-path"), SignerEpoch: 2, PathGeneration: 2, FencingGenerationFloor: 3}, []AuthorizationRecord{next}, nil); err != nil {
		t.Fatal(err)
	}
	return s, use, root
}
func oldRevocationUpdate(use AuthorizationUse) AuthorizationUpdate {
	return AuthorizationUpdate{IssuerID: "hub-one", NodeID: use.NodeID, BootID: use.BootID, UpdateID: "old-revoke", Payload: []byte("old-revoke"), SignerEpoch: 2, PathGeneration: 1, FencingGenerationFloor: 1}
}

func TestPureOlderPathRevocationKeepsFloorsFundsAndUnknownScope(t *testing.T) {
	s, use, _ := olderRevocationFixture(t)
	ctx := context.Background()

	held := UsageReservation{NodeID: use.NodeID, BootID: use.BootID, AuthorizationID: use.AuthorizationID, BatchID: "old-unknown", SnapshotID: use.BillingSnapshotID, FlowKey: "old", RuleID: use.RuleID, Direction: "egress", Reserved: 200}
	if _, err := s.ReserveAuthorizedUsage(ctx, held, use); err != nil {
		t.Fatal(err)
	}
	if err := s.SettleUsageReservation(ctx, held, "unknown", 200, []byte("unknown"), 200); err != nil {
		t.Fatal(err)
	}
	before, err := s.AuditAuthorizationBalance(ctx, use.NodeID, use.AuthorizationID)
	if err != nil {
		t.Fatal(err)
	}
	u := oldRevocationUpdate(use)
	if err = s.PersistAuthorizationUpdate(ctx, u, nil, []string{use.AuthorizationID, "not-installed"}); err != nil {
		t.Fatal("pure older-path revoke rejected", err)
	}
	for key, want := range map[string]string{"authorization_path_generation": "2", "authorization_fencing_floor": "3", "authorization_signer_epoch": "2"} {
		got, err := s.AgentState(ctx, key)
		if err != nil || got != want {
			t.Fatal(key, got, err)
		}
	}
	if _, err = s.CheckStoredAuthorization(ctx, use); !errors.Is(err, ErrAuthorizationDenied) {
		t.Fatal("old credit still executable", err)
	}
	after, err := s.AuditAuthorizationBalance(ctx, use.NodeID, use.AuthorizationID)
	if err != nil || before != after {
		t.Fatal("revocation released old credit", before, after, err)
	}
	// A never-installed identifier is retained only for this issuer/boot/path.
	// It cannot poison an independent current-path family from another Hub.
	root, _, err := readAuthorizationForTest(s, use.NodeID, "current-credit")
	if err != nil {
		t.Fatal(err)
	}
	root.AuthorizationID = "not-installed"
	root.SourceID = "other-hub-source"
	root.ParentAuthorizationID = "other-parent"
	fresh := AuthorizationUpdate{IssuerID: "other-hub", NodeID: use.NodeID, BootID: use.BootID, UpdateID: "other-hub", Payload: []byte("other-hub"), SignerEpoch: 2, PathGeneration: 2, FencingGenerationFloor: 3}
	if err = s.PersistAuthorizationUpdate(ctx, fresh, []AuthorizationRecord{root}, nil); err != nil {
		t.Fatal("unknown tombstone escaped its scope", err)
	}
	other := use
	other.AuthorizationID = root.AuthorizationID
	other.PathGeneration = 2
	if _, err = s.CheckStoredAuthorization(ctx, other); err != nil {
		t.Fatal(err)
	}
	var global int
	if err = s.db.QueryRow(`SELECT count(*) FROM authorization_revocations WHERE authorization_id='not-installed'`).Scan(&global); err != nil || global != 0 {
		t.Fatal("unscoped unknown tombstone", global, err)
	}
}

func readAuthorizationForTest(s *Store, node, id string) (AuthorizationRecord, string, error) {
	tx, err := s.db.BeginTx(context.Background(), nil)
	if err != nil {
		return AuthorizationRecord{}, "", err
	}
	defer tx.Rollback()
	return readAuthorizationTx(context.Background(), tx, node, id)
}

func TestOlderPathRevocationRejectsForeignKnownScopeAndMixedRights(t *testing.T) {
	for _, name := range []string{"issuer", "boot", "current-path-id", "lower-epoch", "grant", "handoff", "policy", "references", "heartbeat"} {
		t.Run(name, func(t *testing.T) {
			s, use, root := olderRevocationFixture(t)
			ctx := context.Background()
			u := oldRevocationUpdate(use)
			revocations := []string{use.AuthorizationID}
			var records []AuthorizationRecord
			switch name {
			case "issuer":
				u.IssuerID = "another-hub"
			case "boot":
				u.BootID = "another-boot"
			case "current-path-id":
				revocations = []string{"current-credit"}
			case "lower-epoch":
				u.SignerEpoch = 1
			case "grant":
				root.AuthorizationID = "mixed-new"
				root.SignerEpoch = 2
				records = []AuthorizationRecord{root}
			case "handoff":
				u.Handoffs = []EpochHandoff{{SourceID: "unproven"}}
			case "policy":
				u.PolicyTransitions = []PolicyTransition{{}}
			case "references":
				u.ShareReferences = []ShareReference{{}}
			case "heartbeat":
				revocations = nil
				u.BackendHeartbeat = use.Latest
				u.AutonomyDeadline = use.Latest.Add(time.Hour)
			}
			before, err := s.AuditAuthorizationBalance(ctx, use.NodeID, use.AuthorizationID)
			if err != nil {
				t.Fatal(err)
			}
			if err = s.PersistAuthorizationUpdate(ctx, u, records, revocations); err == nil {
				t.Fatal("invalid older-path revoke accepted", name)
			}
			if _, err = s.CheckStoredAuthorization(ctx, use); err != nil {
				t.Fatal("failed update changed old draining credit", err)
			}
			after, err := s.AuditAuthorizationBalance(ctx, use.NodeID, use.AuthorizationID)
			if err != nil || before != after {
				t.Fatal(after, err)
			}
		})
	}
}

func TestOlderPathRevocationFULLRollbackAndConcurrentReplay(t *testing.T) {
	s, use, _ := olderRevocationFixture(t)
	ctx := context.Background()
	u := oldRevocationUpdate(use)
	if _, err := s.db.Exec(`CREATE TRIGGER fail_scoped_revoke BEFORE INSERT ON authorization_scoped_revocations WHEN NEW.authorization_id='not-installed' BEGIN SELECT RAISE(ABORT,'injected FULL failure'); END`); err != nil {
		t.Fatal(err)
	}
	if err := s.PersistAuthorizationUpdate(ctx, u, nil, []string{use.AuthorizationID, "not-installed"}); err == nil {
		t.Fatal("write failure accepted")
	}
	if _, err := s.CheckStoredAuthorization(ctx, use); err != nil {
		t.Fatal("partial revocation escaped rollback", err)
	}
	if _, err := s.db.Exec(`DROP TRIGGER fail_scoped_revoke`); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			if err := s.PersistAuthorizationUpdate(ctx, u, nil, []string{use.AuthorizationID, "not-installed"}); err != nil {
				t.Error(err)
			}
		})
	}
	wg.Wait()
	var count int
	if err := s.db.QueryRow(`SELECT count(*) FROM authorization_scoped_revocations`).Scan(&count); err != nil || count != 2 {
		t.Fatal(count, err)
	}

	var path string
	if err := s.db.QueryRow(`SELECT file FROM pragma_database_list WHERE name='main'`).Scan(&path); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	var err error
	s, err = Open(path, "agent")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err = s.PersistAuthorizationUpdate(ctx, u, nil, []string{use.AuthorizationID, "not-installed"}); err != nil {
		t.Fatal("reopened revoke changed replay", err)
	}
	if _, err := s.db.Exec(`DELETE FROM authorization_scoped_revocations`); err == nil {
		t.Fatal("scoped tombstones mutable")
	}
	u.UpdateID = "future-replay"
	u.Payload = []byte("future-replay")
	u.SignerEpoch = 9
	if err := s.PersistAuthorizationUpdate(ctx, u, nil, []string{use.AuthorizationID}); err != nil {
		t.Fatal(err)
	}
	if value, err := s.AgentState(ctx, "authorization_signer_epoch"); err != nil || value != "2" {
		t.Fatal("pure revoke promoted global epoch", value, err)
	}
}
