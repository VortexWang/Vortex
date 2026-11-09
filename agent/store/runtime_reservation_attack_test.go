package store

import (
	"context"
	"testing"
)

// These are attacks on the local durable Store seam, not wire OPEN coverage.
func TestRuntimeReservationScopeAndReplayAttacksAreAtomic(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*UsageReservation, *AuthorizationUse)
	}{
		{"node", func(r *UsageReservation, u *AuthorizationUse) { r.NodeID = "other"; u.NodeID = "other" }},
		{"boot", func(r *UsageReservation, u *AuthorizationUse) { r.BootID = "other"; u.BootID = "other" }},
		{"session", func(r *UsageReservation, u *AuthorizationUse) { u.SessionID = "missing" }},
		{"flow", func(r *UsageReservation, u *AuthorizationUse) { r.FlowKey = "other" }},
		{"snapshot", func(r *UsageReservation, u *AuthorizationUse) { r.SnapshotID = "other"; u.BillingSnapshotID = "other" }},
		{"rule", func(r *UsageReservation, u *AuthorizationUse) { r.RuleID = "other"; u.RuleID = "other" }},
		{"revision", func(r *UsageReservation, u *AuthorizationUse) { u.RuleRevision++ }},
		{"generation", func(r *UsageReservation, u *AuthorizationUse) { u.PathGeneration++ }},
		{"direction", func(r *UsageReservation, u *AuthorizationUse) { r.Direction = "invalid" }},
		{"zero", func(r *UsageReservation, u *AuthorizationUse) { r.Reserved = 0 }},
		{"amount-replay", func(r *UsageReservation, u *AuthorizationUse) { r.Reserved++ }},
		{"direction-replay", func(r *UsageReservation, u *AuthorizationUse) { r.Direction = "ingress" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, scope, _ := runtimeStore(t)
			ctx := context.Background()
			a, err := s.OpenRuntimeSession(ctx, scope, "session", "flow")
			if err != nil {
				t.Fatal(err)
			}
			original := runtimeReservation("original", 100)
			if _, err := s.ReserveRuntimeUsage(ctx, original, a.Use); err != nil {
				t.Fatal(err)
			}
			before, err := s.AuditAuthorizationSource(ctx, "node", "funding")
			if err != nil {
				t.Fatal(err)
			}
			var tokens string
			var tick int64
			if err := s.db.QueryRow(`SELECT tokens,last_tick FROM runtime_rate_buckets WHERE node_id='node'`).Scan(&tokens, &tick); err != nil {
				t.Fatal(err)
			}
			attacked, use := original, a.Use
			tc.change(&attacked, &use)
			if _, err := s.ReserveRuntimeUsage(ctx, attacked, use); err == nil {
				t.Fatal("altered reservation accepted")
			}
			after, err := s.AuditAuthorizationSource(ctx, "node", "funding")
			if err != nil || after != before {
				t.Fatalf("denial changed balance: before=%+v after=%+v err=%v", before, after, err)
			}
			var newTokens string
			var newTick int64
			if err := s.db.QueryRow(`SELECT tokens,last_tick FROM runtime_rate_buckets WHERE node_id='node'`).Scan(&newTokens, &newTick); err != nil || tokens != newTokens || tick != newTick {
				t.Fatalf("denial changed rate state: %v", err)
			}
			for _, table := range []string{"authorization_usage_holds", "usage_reservations"} {
				var count int
				if err := s.db.QueryRow("SELECT COUNT(*) FROM " + table).Scan(&count); err != nil || count != 1 {
					t.Fatalf("%s rows=%d err=%v", table, count, err)
				}
			}
			g, r, sh := runtimeSessionIDs(t, s)
			if g != "grant" || r != "runtime" || sh != "share" {
				t.Fatal("denial changed selected authority")
			}
			if id, err := s.ReserveRuntimeUsage(ctx, original, a.Use); err != nil || id != "grant" {
				t.Fatalf("original retry poisoned: %q %v", id, err)
			}
			if err := s.CloseRuntimeSession(ctx, "node", "boot", "session"); err != nil {
				t.Fatal(err)
			}
			if _, err := s.ReserveRuntimeUsage(ctx, original, a.Use); err == nil {
				t.Fatal("closed session replay accepted")
			}
			after, err = s.AuditAuthorizationSource(ctx, "node", "funding")
			if err != nil || after != before {
				t.Fatalf("retry/close released hold: %+v %v", after, err)
			}
		})
	}
}
