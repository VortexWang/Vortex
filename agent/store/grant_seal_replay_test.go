package store

import (
	"bytes"
	"context"
	"errors"
	"testing"
)

func TestSealedGrantCannotBeResurrectedByAuthorizationReplay(t *testing.T) {
	s, use, path := runtimeStore(t)
	ctx := context.Background()
	use.AuthorizationID, use.Kind, use.BillingSnapshotID = "grant", "credit_grant", "snapshot"
	record, err := s.CheckStoredAuthorization(ctx, use)
	if err != nil {
		t.Fatal(err)
	}
	// A final proof must preserve both known consumption and unresolved liability.
	for _, entry := range []struct {
		id, state string
		amount    uint64
	}{{"known", "committed", 80}, {"uncertain", "unknown", 120}} {
		reservation := runtimeReservation(entry.id, entry.amount)
		if _, err := s.ReserveAuthorizedUsage(ctx, reservation, use); err != nil {
			t.Fatal(err)
		}
		upper := uint64(0)
		if entry.state == "unknown" {
			upper = entry.amount
		}
		if err := s.SettleUsageReservation(ctx, reservation, entry.state, entry.amount, []byte(entry.id), upper); err != nil {
			t.Fatal(err)
		}
	}
	scope := demandScopeFor("node", record)
	if err := s.SealIdleGrant(ctx, scope, "grant"); err != nil {
		t.Fatal(err)
	}
	original, hash, err := s.PrepareGrantSettlementProof(ctx, scope, "grant")
	if err != nil {
		t.Fatal(err)
	}
	if original.Used != 80 || original.Unknown != 120 || original.Unused != (1<<20)-200 {
		t.Fatalf("unexpected final proof: %+v", original)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(path, "agent")
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	update := AuthorizationUpdate{IssuerID: "hub-one", NodeID: "node", BootID: "boot", UpdateID: "initial", Payload: []byte("initial"), SignerEpoch: 1, PathGeneration: 1, FencingGenerationFloor: 1}
	verify := func(t *testing.T) {
		t.Helper()
		if _, err := reopened.CheckStoredAuthorization(ctx, use); !errors.Is(err, ErrAuthorizationDenied) {
			t.Fatalf("sealed grant admitted: %v", err)
		}
		if _, err := reopened.ReserveAuthorizedUsage(ctx, runtimeReservation("resurrection", 1), use); err == nil {
			t.Fatal("sealed grant spent credit")
		}
		proof, digest, err := reopened.PrepareGrantSettlementProof(ctx, scope, "grant")
		if err != nil || proof != original || !bytes.Equal(digest, hash) {
			t.Fatalf("final proof changed: %+v %v", proof, err)
		}
		balance, err := reopened.AuditAuthorizationSource(ctx, "node", "funding")
		if err != nil || balance.Initial != 1<<20 || balance.Remaining != original.Unused || balance.Committed != 80 || balance.Unknown != 120 || balance.Reserved != 0 {
			t.Fatalf("replay changed source liabilities: %+v %v", balance, err)
		}
		var generation string
		if err := reopened.db.QueryRow(`SELECT value FROM agent_state WHERE key='authorization_path_generation'`).Scan(&generation); err != nil || generation != "1" {
			t.Fatalf("rejected update partially advanced generation: %q %v", generation, err)
		}
	}
	if err := reopened.PersistAuthorizationUpdate(ctx, update, []AuthorizationRecord{record}, nil); err != nil {
		t.Fatalf("identical replay: %v", err)
	}
	verify(t)
	for _, test := range []struct {
		name string
		edit func(*AuthorizationUpdate, *AuthorizationRecord)
	}{
		{"new-envelope", func(u *AuthorizationUpdate, r *AuthorizationRecord) {}},
		{"larger-amount", func(u *AuthorizationUpdate, r *AuthorizationRecord) { r.AmountBytes++ }},
		{"new-boot", func(u *AuthorizationUpdate, r *AuthorizationRecord) { u.BootID = "new-boot"; r.BootID = u.BootID }},
		{"new-epoch", func(u *AuthorizationUpdate, r *AuthorizationRecord) { u.SignerEpoch++; r.SignerEpoch = u.SignerEpoch }},
		{"new-path", func(u *AuthorizationUpdate, r *AuthorizationRecord) {
			u.PathGeneration++
			r.PathGeneration = u.PathGeneration
		}},
		{"different-issuer", func(u *AuthorizationUpdate, r *AuthorizationRecord) { u.IssuerID = "another-hub" }},
	} {
		t.Run(test.name, func(t *testing.T) {
			changed, grant := update, record
			changed.UpdateID, changed.Payload = test.name, []byte(test.name)
			test.edit(&changed, &grant)
			if err := reopened.PersistAuthorizationUpdate(ctx, changed, []AuthorizationRecord{grant}, nil); !errors.Is(err, ErrAuthorizationUpdateConflict) {
				t.Fatalf("resurrection update: %v", err)
			}
			verify(t)
		})
	}
}
