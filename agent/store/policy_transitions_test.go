package store

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"
)

func policyFixture(t *testing.T) (*Store, AuthorizationUse, PolicyTransition, AuthorizationUpdate, []AuthorizationRecord, string) {
	t.Helper()
	s, root, u, path := shareFixture(t)
	ctx := context.Background()
	now := root.NotBefore
	bundle := func(r ShareReference, snapshot string, start time.Time, offset int) []AuthorizationRecord {
		t.Helper()
		if err := s.InstallShareSourceAnchor(ctx, u.NodeID, shareID(90), r); err != nil {
			t.Fatal(err)
		}
		base := shareGrant(r, u)
		base.AuthorizationID = shareID(offset)
		base.SourceID = r.SourceID
		base.BillingSnapshotID = snapshot
		base.NotBefore = start.Format(time.RFC3339Nano)
		base.ExpiresAt = now.Add(20 * time.Minute).Format(time.RFC3339Nano)
		base.HardDeadline = root.HardDeadline.Format(time.RFC3339Nano)
		base.AmountBytes = 65536
		runtime := base
		runtime.AuthorizationID = shareID(offset + 1)
		runtime.Kind = "runtime_lease"
		runtime.AmountBytes = 0
		runtime.BillingSnapshotID = ""
		share := runtime
		share.AuthorizationID = shareID(offset + 2)
		share.Kind = "share_lease"
		share.RateBPS = r.RateBPS
		share.Connections = r.Connections
		return []AuthorizationRecord{base, runtime, share}
	}
	oldID, newID := shareID(200), shareID(201)
	boundary := now.Add(time.Minute)
	old := bundle(root, oldID, now, 300)
	if err := s.PersistAuthorizationUpdate(ctx, shareUpdate(u, 40, root), old, nil); err != nil {
		t.Fatal(err)
	}
	futureRoot := root
	futureRoot.ID = shareID(41)
	futureRoot.SourceID = shareID(42)
	future := bundle(futureRoot, newID, boundary, 400)
	p := PolicyTransition{ID: shareID(500), RuleID: root.RuleID, PathGeneration: 1, Old: PolicySnapshot{ID: oldID, RuleID: root.RuleID, NodeID: u.NodeID, PathGeneration: 1, Revision: 1, CoefficientMilli: 1000}, New: PolicySnapshot{ID: newID, RuleID: root.RuleID, NodeID: u.NodeID, PathGeneration: 1, Revision: 2, CoefficientMilli: 1500}, EffectiveAt: boundary, PreissueSeconds: 86400, GuardSeconds: 5, FutureSourceID: futureRoot.SourceID, FutureCreditID: future[0].AuthorizationID, FutureRuntimeID: future[1].AuthorizationID, FutureShareID: future[2].AuthorizationID}
	update := shareUpdate(u, 50, futureRoot)
	update.BackendHeartbeat = now
	update.AutonomyDeadline = now.Add(time.Hour)
	update.PolicyTransitions = []PolicyTransition{p}
	update.Payload, _ = json.Marshal(update.PolicyTransitions)
	scope := AuthorizationUse{NodeID: u.NodeID, BootID: u.BootID, RuleID: root.RuleID, RuleRevision: 1, PathGeneration: 1, Earliest: now.Add(time.Second), Latest: now.Add(time.Second), TrustedTime: true}
	return s, scope, p, update, future, path
}

func TestPolicyTransitionPinsSessionAndSeparatesFutureFunding(t *testing.T) {
	s, scope, p, u, future, path := policyFixture(t)
	ctx := context.Background()
	if err := s.PersistAuthorizationUpdate(ctx, u, future, nil); err != nil {
		t.Fatal(err)
	}
	before, err := s.OpenRuntimeSession(ctx, scope, "old", "old-flow")
	if err != nil || before.Use.BillingSnapshotID != p.Old.ID {
		t.Fatalf("old %+v %v", before, err)
	}
	for _, at := range []time.Time{p.EffectiveAt.Add(-5 * time.Second), p.EffectiveAt.Add(-time.Nanosecond)} {
		test := scope
		test.Earliest = at
		test.Latest = at
		if _, err = s.OpenRuntimeSession(ctx, test, fmt.Sprint(at), "guard"); err == nil {
			t.Fatal("guard admitted new old session")
		}
	}
	test := scope
	test.Earliest = p.EffectiveAt.Add(-time.Second)
	test.Latest = p.EffectiveAt.Add(time.Second)
	if _, err = s.OpenRuntimeSession(ctx, test, "uncertain", "uncertain"); err == nil {
		t.Fatal("uncertain interval crossed period")
	}
	for i, at := range []time.Time{p.EffectiveAt, p.EffectiveAt.Add(5 * time.Second)} {
		test := scope
		test.Earliest = at
		test.Latest = at
		a, err := s.OpenRuntimeSession(ctx, test, fmt.Sprint(i), fmt.Sprint("new", i))
		if err != nil || a.Use.BillingSnapshotID != p.New.ID || a.Use.AuthorizationID != p.FutureCreditID || a.Use.RuntimeLeaseID != p.FutureRuntimeID || a.Use.ShareLeaseID != p.FutureShareID {
			t.Fatalf("cutover %+v %v", a, err)
		}
	}
	// The existing old session does not run policy selection again or move funds.
	before.Use.Earliest = p.EffectiveAt.Add(time.Second)
	before.Use.Latest = before.Use.Earliest
	r := UsageReservation{NodeID: scope.NodeID, BootID: scope.BootID, BatchID: "old-unknown", SnapshotID: p.Old.ID, FlowKey: "old-flow", RuleID: scope.RuleID, Direction: "egress", Reserved: 64, AuthorizationID: before.Use.AuthorizationID}
	if _, err = s.ReserveRuntimeUsage(ctx, r, before.Use); err != nil {
		t.Fatal(err)
	}
	if err = s.SettleUsageReservation(ctx, r, "unknown", 64, []byte("old debt"), 64); err != nil {
		t.Fatal(err)
	}
	s.Close()
	reopened, err := Open(path, "agent")
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	test = scope
	test.Earliest = p.EffectiveAt.Add(6 * time.Second)
	test.Latest = test.Earliest
	a, err := reopened.OpenRuntimeSession(ctx, test, "reopened", "new-reopen")
	if err != nil || a.Use.BillingSnapshotID != p.New.ID {
		t.Fatalf("restart cutover %+v %v", a, err)
	}
	b, err := reopened.AuditAuthorizationSource(ctx, scope.NodeID, shareID(2))
	if err != nil || b.Unknown != 64 {
		t.Fatalf("old debt %+v %v", b, err)
	}
}

func TestPolicyFutureRenewalKeepsFundedFamily(t *testing.T) {
	s, scope, p, u, future, _ := policyFixture(t)
	ctx := context.Background()
	if err := s.PersistAuthorizationUpdate(ctx, u, future, nil); err != nil {
		t.Fatal(err)
	}
	renewed := append([]AuthorizationRecord(nil), future...)
	for i := range renewed {
		renewed[i].AuthorizationID = shareID(800 + i)
		renewed[i].IssuedAt = p.EffectiveAt.Add(time.Second).Format(time.RFC3339Nano)
	}
	u.UpdateID = shareID(850)
	u.PolicyTransitions = nil
	u.Payload = []byte("funded-family-renewal")
	if err := s.PersistAuthorizationUpdate(ctx, u, renewed, nil); err != nil {
		t.Fatal(err)
	}
	scope.Earliest = p.EffectiveAt.Add(2 * time.Second)
	scope.Latest = scope.Earliest
	a, err := s.OpenRuntimeSession(ctx, scope, "renewed", "renewed-flow")
	if err != nil || a.Use.AuthorizationID != renewed[0].AuthorizationID || a.Use.RuntimeLeaseID != renewed[1].AuthorizationID || a.Use.ShareLeaseID != renewed[2].AuthorizationID || a.Use.BillingSnapshotID != p.New.ID {
		t.Fatalf("future family %+v %v", a, err)
	}
}

func TestPolicyTransitionRejectsMissingFundingAndChangedSnapshot(t *testing.T) {
	for _, mode := range []string{"missing", "same-source", "wrong-node", "wrong-issuer", "early", "changed-snapshot", "revoked-future", "late-old", "duplicate"} {
		t.Run(mode, func(t *testing.T) {
			s, scope, p, u, future, _ := policyFixture(t)
			ctx := context.Background()
			switch mode {
			case "missing":
				future = nil
			case "same-source":
				future[0].SourceID = shareID(2)
				u.PolicyTransitions[0].FutureSourceID = shareID(2)
			case "wrong-node":
				u.PolicyTransitions[0].New.NodeID = shareID(999)
			case "wrong-issuer":
				u.IssuerID = shareID(999)
			case "early":
				u.BackendHeartbeat = p.EffectiveAt.Add(-25 * time.Hour)
				u.AutonomyDeadline = u.BackendHeartbeat.Add(time.Hour)
			case "changed-snapshot", "late-old", "duplicate":
				if err := s.PersistAuthorizationUpdate(ctx, u, future, nil); err != nil {
					t.Fatal(err)
				}
				future = nil
				u.UpdateID = shareID(700)
				if mode == "changed-snapshot" {
					u.PolicyTransitions[0].New.CoefficientMilli++
				} else if mode == "late-old" {
					u.PolicyTransitions[0].ID = shareID(701)
					u.PolicyTransitions[0].EffectiveAt = p.EffectiveAt.Add(-time.Second)
				} else {
					u.PolicyTransitions = append(u.PolicyTransitions, p)
				}
			case "revoked-future":
				if err := s.PersistAuthorizationUpdate(ctx, AuthorizationUpdate{NodeID: scope.NodeID, BootID: scope.BootID, UpdateID: shareID(750), Payload: []byte("revocation"), SignerEpoch: 1, PathGeneration: 1, FencingGenerationFloor: 1}, nil, []string{p.FutureCreditID}); err != nil {
					t.Fatal(err)
				}
			}
			u.Payload, _ = json.Marshal(u.PolicyTransitions)
			err := s.PersistAuthorizationUpdate(ctx, u, future, nil)
			if err == nil {
				t.Fatal("invalid policy accepted")
			}
		})
	}
}

func TestPolicyMissingFutureAndUntrustedClockNeverFallBackToOldPeriod(t *testing.T) {
	s, scope, p, u, future, _ := policyFixture(t)
	ctx := context.Background()
	if err := s.PersistAuthorizationUpdate(ctx, u, future, nil); err != nil {
		t.Fatal(err)
	}
	old, err := s.OpenRuntimeSession(ctx, scope, "old", "old")
	if err != nil {
		t.Fatal(err)
	}
	revoke := AuthorizationUpdate{IssuerID: u.IssuerID, NodeID: u.NodeID, BootID: u.BootID, UpdateID: shareID(960), Payload: []byte("future revoked"), SignerEpoch: 1, PathGeneration: 1, FencingGenerationFloor: 1}
	if err = s.PersistAuthorizationUpdate(ctx, revoke, nil, []string{p.FutureCreditID}); err != nil {
		t.Fatal(err)
	}
	scope.Earliest = p.EffectiveAt
	scope.Latest = p.EffectiveAt
	if _, err = s.OpenRuntimeSession(ctx, scope, "no-funds", "no-funds"); err == nil {
		t.Fatal("new period used old source")
	}
	scope.TrustedTime = false
	if _, err = s.OpenRuntimeSession(ctx, scope, "no-clock", "no-clock"); err == nil {
		t.Fatal("unknown clock admitted new period")
	}
	old.Use.Earliest = p.EffectiveAt.Add(time.Second)
	old.Use.Latest = old.Use.Earliest
	if _, err = s.CheckStoredAuthorization(ctx, old.Use); err != nil {
		t.Fatal("old funded session lost original boundary", err)
	}
}

func TestPolicyMigrationFailureKeepsAuthoritativeRowsAndCanReopen(t *testing.T) {
	s, scope, p, u, future, path := policyFixture(t)
	ctx := context.Background()
	if err := s.PersistAuthorizationUpdate(ctx, u, future, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec(`CREATE TRIGGER policy_write_failure BEFORE INSERT ON policy_transitions BEGIN SELECT RAISE(ABORT,'write failure'); END`); err != nil {
		t.Fatal(err)
	}
	next := u
	next.UpdateID = shareID(980)
	next.PolicyTransitions = append([]PolicyTransition(nil), u.PolicyTransitions...)
	next.PolicyTransitions[0].ID = shareID(981)
	next.PolicyTransitions[0].EffectiveAt = p.EffectiveAt.Add(time.Second)
	next.Payload = []byte("failed future transition")
	if err := s.PersistAuthorizationUpdate(ctx, next, nil, nil); err == nil {
		t.Fatal("failed policy write committed")
	}
	var count int
	if err := s.db.QueryRow(`SELECT count(*) FROM policy_transitions`).Scan(&count); err != nil || count != 1 {
		t.Fatal("partial transition", count, err)
	}
	if _, err := s.db.Exec(`DROP TRIGGER policy_write_failure`); err != nil {
		t.Fatal(err)
	}
	s.Close()
	reopened, err := Open(path, "agent")
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	scope.Earliest = p.EffectiveAt
	scope.Latest = p.EffectiveAt
	got, err := reopened.OpenRuntimeSession(ctx, scope, "post-failure", "post-failure")
	if err != nil || got.Use.BillingSnapshotID != p.New.ID {
		t.Fatalf("failure changed authoritative snapshot %+v %v", got, err)
	}
}
