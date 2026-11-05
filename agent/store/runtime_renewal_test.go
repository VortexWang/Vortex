package store

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

func renewedRuntimeRecords(t *testing.T, s *Store, use AuthorizationUse) []AuthorizationRecord {
	t.Helper()
	var records []AuthorizationRecord
	for kind, id := range map[string]string{"credit_grant": "grant", "runtime_lease": "runtime", "share_lease": "share"} {
		u := use
		u.Kind, u.AuthorizationID = kind, id
		u.SessionID, u.RuntimeLeaseID, u.ShareLeaseID = "", "", ""
		r, err := s.CheckStoredAuthorization(context.Background(), u)
		if err != nil {
			t.Fatal(err)
		}
		r.AuthorizationID = "renewed-" + id
		r.IssuedAt = use.Latest.Add(time.Second).Format(time.RFC3339Nano)
		r.NotBefore = r.IssuedAt
		r.ExpiresAt = use.Latest.Add(10 * time.Minute).Format(time.RFC3339Nano)
		records = append(records, r)
	}
	return records
}

func installRuntimeRenewal(s *Store, records []AuthorizationRecord) error {
	return s.PersistAuthorizationUpdate(context.Background(), AuthorizationUpdate{IssuerID: records[0].IssuerID, NodeID: "node", BootID: "boot", UpdateID: "renewal", Payload: []byte("renewal"), SignerEpoch: 1, PathGeneration: 1, FencingGenerationFloor: 1}, records, nil)
}

func runtimeReservation(batch string, n uint64) UsageReservation {
	return UsageReservation{NodeID: "node", BootID: "boot", BatchID: batch, AuthorizationID: "grant", SnapshotID: "snapshot", FlowKey: "flow", RuleID: "rule", Direction: "egress", Reserved: n}
}

func runtimeSessionIDs(t *testing.T, s *Store) (string, string, string) {
	t.Helper()
	var g, r, sh string
	if err := s.db.QueryRow(`SELECT grant_id,runtime_lease_id,share_lease_id FROM runtime_sessions WHERE session_id='session'`).Scan(&g, &r, &sh); err != nil {
		t.Fatal(err)
	}
	return g, r, sh
}

func TestRuntimeRenewalKeepsOldUnknownAndOriginalBatchIdentity(t *testing.T) {
	s, scope, _ := runtimeStore(t)
	ctx := context.Background()
	a, err := s.OpenRuntimeSession(ctx, scope, "session", "flow")
	if err != nil {
		t.Fatal(err)
	}
	old := runtimeReservation("old", 100)
	if id, err := s.ReserveRuntimeUsage(ctx, old, a.Use); err != nil || id != "grant" {
		t.Fatalf("initial reserve %q: %v", id, err)
	}
	if err := installRuntimeRenewal(s, renewedRuntimeRecords(t, s, a.Use)); err != nil {
		t.Fatal(err)
	}
	// The other direction can select a new grant while the original batch is
	// still unresolved. Retrying that batch must not move or debit it again.
	use := a.Use
	use.Earliest, use.Latest = use.Earliest.Add(2*time.Second), use.Latest.Add(2*time.Second)
	other := runtimeReservation("other", 100)
	other.Direction = "ingress"
	if id, err := s.ReserveRuntimeUsage(ctx, other, use); err != nil || id != "renewed-grant" {
		t.Fatalf("new reserve %q: %v", id, err)
	}
	if id, err := s.ReserveRuntimeUsage(ctx, old, use); err != nil || id != "grant" {
		t.Fatalf("retry moved original grant %q: %v", id, err)
	}
	if err := s.SettleUsageReservation(ctx, old, "unknown", 100, []byte("unknown"), 100); err != nil {
		t.Fatal(err)
	}
	other.AuthorizationID = "renewed-grant"
	if err := s.SettleUsageReservation(ctx, other, "committed", 40, []byte("usage"), 0); err != nil {
		t.Fatal(err)
	}
	// Both old leases have now expired. A live session continues on its
	// original source and snapshot using the durable replacement leases.
	use.Earliest, use.Latest = use.Earliest.Add(6*time.Minute), use.Latest.Add(6*time.Minute)
	if id, err := s.ReserveRuntimeUsage(ctx, runtimeReservation("after-expiry", 10), use); err != nil || id != "renewed-grant" {
		t.Fatalf("renewal after expiry %q: %v", id, err)
	}
	g, r, sh := runtimeSessionIDs(t, s)
	if g != "renewed-grant" || r != "renewed-runtime" || sh != "renewed-share" {
		t.Fatalf("session did not rotate atomically: %s %s %s", g, r, sh)
	}
	b, err := s.AuditAuthorizationSource(ctx, "node", "funding")
	if err != nil || b.Initial != 2<<20 || b.Unknown != 100 || b.Committed != 40 || b.Reserved != 10 || b.Remaining != (2<<20)-150 {
		t.Fatalf("source conservation: %+v %v", b, err)
	}
}

func TestRuntimeRenewalRollsBackSessionRateAndBalanceOnWriteFailure(t *testing.T) {
	s, scope, _ := runtimeStore(t)
	ctx := context.Background()
	a, err := s.OpenRuntimeSession(ctx, scope, "session", "flow")
	if err != nil {
		t.Fatal(err)
	}
	if err := installRuntimeRenewal(s, renewedRuntimeRecords(t, s, a.Use)); err != nil {
		t.Fatal(err)
	}
	use := a.Use
	use.Earliest, use.Latest = use.Earliest.Add(6*time.Minute), use.Latest.Add(6*time.Minute)
	if _, err := s.db.Exec(`CREATE TRIGGER reject_hold BEFORE INSERT ON authorization_usage_holds BEGIN SELECT RAISE(ABORT, 'injected durable write failure'); END`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ReserveRuntimeUsage(ctx, runtimeReservation("fail", 100), use); err == nil {
		t.Fatal("write failure returned authority")
	}
	g, r, sh := runtimeSessionIDs(t, s)
	if g != "grant" || r != "runtime" || sh != "share" {
		t.Fatalf("failed transaction rotated session: %s %s %s", g, r, sh)
	}
	var rateRows, reservations int
	_ = s.db.QueryRow(`SELECT COUNT(*) FROM runtime_rate_buckets`).Scan(&rateRows)
	_ = s.db.QueryRow(`SELECT COUNT(*) FROM usage_reservations`).Scan(&reservations)
	b, err := s.AuditAuthorizationSource(ctx, "node", "funding")
	if err != nil || rateRows != 0 || reservations != 0 || b.Remaining != b.Initial {
		t.Fatalf("failed transaction changed accounting: %+v rate=%d holds=%d %v", b, rateRows, reservations, err)
	}
}

func TestRuntimeRenewalCannotResetRateOrReviveClosedSession(t *testing.T) {
	s, scope, _ := runtimeStore(t)
	ctx := context.Background()
	a, err := s.OpenRuntimeSession(ctx, scope, "session", "flow")
	if err != nil {
		t.Fatal(err)
	}
	records := renewedRuntimeRecords(t, s, a.Use)
	for i := range records {
		records[i].IssuedAt = scope.Earliest.Format(time.RFC3339Nano)
		records[i].NotBefore = scope.Earliest.Format(time.RFC3339Nano)
	}
	if err := installRuntimeRenewal(s, records); err != nil {
		t.Fatal(err)
	}
	// Spend the original source bucket with the original grant; selecting a
	// new lease at the same clock time cannot replenish that bucket.
	if _, err := s.ReserveAuthorizedUsage(ctx, runtimeReservation("burst", 65536), a.Use); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ReserveRuntimeUsage(ctx, runtimeReservation("excess", 1), a.Use); err == nil {
		t.Fatal("renewal reset shared rate bucket")
	} else {
		var limited *RateLimited
		if !errors.As(err, &limited) {
			t.Fatal(err)
		}
	}
	if err := s.CloseRuntimeSession(ctx, "node", "boot", "session"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ReserveRuntimeUsage(ctx, runtimeReservation("closed", 1), a.Use); !errors.Is(err, ErrAuthorizationDenied) {
		t.Fatalf("closed session renewed: %v", err)
	}
}

func TestRuntimeRenewalHonorsRevocationTimeAndMigratedAnchors(t *testing.T) {
	for _, mode := range []string{"untrusted", "deadline", "revoked", "migrated"} {
		t.Run(mode, func(t *testing.T) {
			s, scope, path := runtimeStore(t)
			ctx := context.Background()
			a, err := s.OpenRuntimeSession(ctx, scope, "session", "flow")
			if err != nil {
				t.Fatal(err)
			}
			if err := installRuntimeRenewal(s, renewedRuntimeRecords(t, s, a.Use)); err != nil {
				t.Fatal(err)
			}
			use := a.Use
			use.Earliest, use.Latest = use.Earliest.Add(6*time.Minute), use.Latest.Add(6*time.Minute)
			switch mode {
			case "untrusted":
				use.TrustedTime = false
			case "deadline":
				use.Earliest, use.Latest = scope.Earliest.Add(time.Hour), scope.Latest.Add(time.Hour)
			case "revoked":
				if err := s.PersistAuthorizationUpdate(ctx, AuthorizationUpdate{IssuerID: "hub-one", NodeID: "node", BootID: "boot", UpdateID: "revoke", Payload: []byte("revoke"), SignerEpoch: 1, PathGeneration: 1, FencingGenerationFloor: 1}, nil, []string{"runtime"}); err != nil {
					t.Fatal(err)
				}
			case "migrated":
				// Simulate the pre-v8 session row. Startup must preserve its
				// original identities even with fresher authorizations present.
				if _, err := s.db.Exec(`UPDATE runtime_sessions SET initial_grant_id='',initial_runtime_lease_id='',initial_share_lease_id=''`); err != nil {
					t.Fatal(err)
				}
				reopened, err := Open(path, "agent")
				if err != nil {
					t.Fatal(err)
				}
				defer reopened.Close()
				s = reopened
			}
			id, err := s.ReserveRuntimeUsage(ctx, runtimeReservation("boundary", 1), use)
			if mode == "migrated" {
				if err != nil || id != "renewed-grant" {
					t.Fatalf("migration renewal %s: %v", id, err)
				}
			} else if err == nil {
				t.Fatal("renewal bypassed revocation or time boundary")
			}
		})
	}
}

func TestRuntimeRenewalRejectsChangedOwnershipAndAbsoluteLimits(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*AuthorizationRecord)
	}{
		{"issuer", func(r *AuthorizationRecord) { r.IssuerID = "other-hub" }},
		{"source", func(r *AuthorizationRecord) { r.SourceID += "-other" }},
		{"parent", func(r *AuthorizationRecord) { r.ParentAuthorizationID += "-other" }},
		{"snapshot", func(r *AuthorizationRecord) { r.BillingSnapshotID += "-other" }},
		{"epoch", func(r *AuthorizationRecord) { r.SignerEpoch++ }},
		{"boot", func(r *AuthorizationRecord) { r.BootID += "-other" }},
		{"path", func(r *AuthorizationRecord) { r.PathGeneration++ }},
		{"deadline", func(r *AuthorizationRecord) {
			r.HardDeadline = time.Date(2026, 11, 5, 14, 0, 0, 0, time.UTC).Format(time.RFC3339Nano)
		}},
		{"rate", func(r *AuthorizationRecord) {
			if r.Kind == "share_lease" {
				r.RateBPS++
			}
		}},
		{"connections", func(r *AuthorizationRecord) {
			if r.Kind == "share_lease" {
				r.Connections++
			}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, scope, _ := runtimeStore(t)
			ctx := context.Background()
			a, err := s.OpenRuntimeSession(ctx, scope, "session", "flow")
			if err != nil {
				t.Fatal(err)
			}
			records := renewedRuntimeRecords(t, s, a.Use)
			for i := range records {
				tc.change(&records[i])
			}
			// A rejected update is also a valid fail-closed result. In either
			// case it cannot fund an existing session after old leases expire.
			_ = installRuntimeRenewal(s, records)
			use := a.Use
			use.Earliest, use.Latest = use.Earliest.Add(6*time.Minute), use.Latest.Add(6*time.Minute)
			if _, err := s.ReserveRuntimeUsage(ctx, runtimeReservation("forbidden", 1), use); err == nil {
				t.Fatal("changed ownership or limit renewed a live session")
			}
			g, r, sh := runtimeSessionIDs(t, s)
			if g != "grant" || r != "runtime" || sh != "share" {
				t.Fatal("denial changed session")
			}
		})
	}
}

func TestRuntimeRenewalDuplexConcurrencyReservesOncePerBatch(t *testing.T) {
	s, scope, _ := runtimeStore(t)
	ctx := context.Background()
	a, err := s.OpenRuntimeSession(ctx, scope, "session", "flow")
	if err != nil {
		t.Fatal(err)
	}
	if err := installRuntimeRenewal(s, renewedRuntimeRecords(t, s, a.Use)); err != nil {
		t.Fatal(err)
	}
	use := a.Use
	use.Earliest, use.Latest = use.Earliest.Add(6*time.Minute), use.Latest.Add(6*time.Minute)
	var wg sync.WaitGroup
	for _, direction := range []string{"ingress", "egress"} {
		wg.Add(1)
		go func(direction string) {
			defer wg.Done()
			r := runtimeReservation(direction, 100)
			r.Direction = direction
			for range 2 {
				if id, err := s.ReserveRuntimeUsage(ctx, r, use); err != nil || id != "renewed-grant" {
					t.Errorf("duplex %s %s: %v", direction, id, err)
				}
			}
		}(direction)
	}
	wg.Wait()
	b, err := s.AuditAuthorizationSource(ctx, "node", "funding")
	if err != nil || b.Reserved != 200 || b.Remaining != (2<<20)-200 {
		t.Fatalf("duplicate or lost debit: %+v %v", b, err)
	}
}
