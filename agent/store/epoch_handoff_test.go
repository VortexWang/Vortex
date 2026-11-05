package store

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestSuccessorGrantsConsumeOnlyTransferredRAndKeepOldRights(t *testing.T) {
	s, use := fundedStore(t)
	ctx := context.Background()
	root, err := s.CheckStoredAuthorization(ctx, use)
	if err != nil {
		t.Fatal(err)
	}
	grant := topupRecord(t, s, use)
	grant.SignerEpoch = 2
	grant.AmountBytes = 600
	h := EpochHandoff{TransferID: "550e8400-e29b-41d4-a716-446655440800", ParentID: root.ParentAuthorizationID, SourceID: root.SourceID, From: 1, To: 2, Remaining: 1000, SealedSequence: 4, RecordSHA256: strings.Repeat("a", 64)}
	u := AuthorizationUpdate{IssuerID: "hub-one", NodeID: use.NodeID, BootID: use.BootID, UpdateID: "handoff", Payload: []byte("handoff-1000"), SignerEpoch: 2, PathGeneration: 1, FencingGenerationFloor: 1, Handoffs: []EpochHandoff{h}}
	if err = s.PersistAuthorizationUpdate(ctx, u, []AuthorizationRecord{grant}, nil); err != nil {
		t.Fatal(err)
	}
	if err = s.PersistAuthorizationUpdate(ctx, u, []AuthorizationRecord{grant}, nil); err != nil {
		t.Fatal("replay", err)
	}
	if _, err = s.CheckStoredAuthorization(ctx, use); err != nil {
		t.Fatal("old rights invalidated", err)
	}
	use.AuthorizationID = grant.AuthorizationID
	if _, err = s.CheckStoredAuthorization(ctx, use); err != nil {
		t.Fatal("successor denied", err)
	}
	u.UpdateID = "excess"
	u.Payload = []byte("excess")
	grant.AuthorizationID = "excess"
	grant.AmountBytes = 401
	if err = s.PersistAuthorizationUpdate(ctx, u, []AuthorizationRecord{grant}, nil); !errors.Is(err, ErrAuthorizationDenied) {
		t.Fatalf("excess accepted %v", err)
	}
	grant.AmountBytes = 400
	u.UpdateID = "last"
	u.Payload = []byte("last")
	if err = s.PersistAuthorizationUpdate(ctx, u, []AuthorizationRecord{grant}, nil); err != nil {
		t.Fatal(err)
	}
	grant.AuthorizationID = "after-empty"
	grant.AmountBytes = 1
	u.UpdateID = "empty"
	u.Payload = []byte("empty")
	if err = s.PersistAuthorizationUpdate(ctx, u, []AuthorizationRecord{grant}, nil); !errors.Is(err, ErrAuthorizationDenied) {
		t.Fatalf("budget refilled %v", err)
	}
	balance, err := s.AuditAuthorizationSource(ctx, use.NodeID, root.SourceID)
	if err != nil || balance.Initial != 5096 || balance.Remaining != 5096 {
		t.Fatalf("balance %+v %v", balance, err)
	}
	var n string
	if err = s.db.QueryRow(`SELECT remaining_bytes FROM authorization_handoffs`).Scan(&n); err != nil || n != "0" {
		t.Fatal(n, err)
	}
}

func TestEpochHandoffRejectsMutationAndRevocationBeforeTransfer(t *testing.T) {
	for _, mode := range []string{"wrong-issuer", "wrong-parent", "wrong-boot", "gap", "revoked-root", "changed-replay", "fork"} {
		t.Run(mode, func(t *testing.T) {
			s, use := fundedStore(t)
			ctx := context.Background()
			root, err := s.CheckStoredAuthorization(ctx, use)
			if err != nil {
				t.Fatal(err)
			}
			h := EpochHandoff{TransferID: "550e8400-e29b-41d4-a716-446655440800", ParentID: root.ParentAuthorizationID, SourceID: root.SourceID, From: 1, To: 2, Remaining: 1000, RecordSHA256: strings.Repeat("a", 64)}
			u := AuthorizationUpdate{IssuerID: "hub-one", NodeID: use.NodeID, BootID: use.BootID, UpdateID: "handoff", Payload: []byte("handoff"), SignerEpoch: 2, PathGeneration: 1, FencingGenerationFloor: 1}
			var revocations []string
			switch mode {
			case "wrong-issuer":
				u.IssuerID = "different"
			case "wrong-parent":
				h.ParentID = "other"
			case "wrong-boot":
				u.BootID = "other"
			case "gap":
				h.From = 2
				h.To = 3
				u.SignerEpoch = 3
			case "revoked-root":
				revocations = []string{root.AuthorizationID}
			case "changed-replay", "fork":
				u.Handoffs = []EpochHandoff{h}
				if err = s.PersistAuthorizationUpdate(ctx, u, nil, nil); err != nil {
					t.Fatal(err)
				}
				u.UpdateID = "changed"
				u.Payload = []byte("changed")
				if mode == "changed-replay" {
					h.Remaining++
				} else {
					h.TransferID = "550e8400-e29b-41d4-a716-446655440801"
				}
			}
			u.Handoffs = []EpochHandoff{h}
			if err = s.PersistAuthorizationUpdate(ctx, u, nil, revocations); err == nil {
				t.Fatal("invalid handoff accepted")
			}
		})
	}
}
