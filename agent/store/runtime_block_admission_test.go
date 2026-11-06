package store

import (
	"context"
	"testing"
	"time"
)

func TestRuntimeBlockAdmissionAttackBoundaries(t *testing.T) {
	for _, attack := range []string{"boot", "flow", "snapshot", "direction", "grant", "session", "revision", "path", "residual", "untrusted", "expired", "closed", "revoke-grant", "revoke-runtime", "revoke-share", "settled"} {
		t.Run(attack, func(t *testing.T) {
			s, scope, _ := runtimeStore(t)
			ctx := context.Background()
			a, err := s.OpenRuntimeSession(ctx, scope, "session", "flow")
			if err != nil {
				t.Fatal(err)
			}
			block := runtimeReservation("block", 256<<10)
			if _, err := s.ReserveRuntimeFundingBlock(ctx, block, a.Use); err != nil {
				t.Fatal(err)
			}
			use := a.Use
			switch attack {
			case "boot":
				block.BootID = "other"
			case "flow":
				block.FlowKey = "other"
			case "snapshot":
				block.SnapshotID = "other"
			case "direction":
				block.Direction = "ingress"
			case "grant":
				block.AuthorizationID = "other"
			case "session":
				use.SessionID = "other"
			case "revision":
				use.RuleRevision++
			case "path":
				use.PathGeneration++
			case "residual":
				block.Reserved--
			case "untrusted":
				use.TrustedTime = false
			case "expired":
				use.Earliest = use.Earliest.Add(time.Hour)
				use.Latest = use.Latest.Add(time.Hour)
			case "closed":
				if err := s.CloseRuntimeSession(ctx, "node", "boot", "session"); err != nil {
					t.Fatal(err)
				}
			case "settled":
				if err := s.SettleUsageReservation(ctx, block, "returned", 0, nil, 0); err != nil {
					t.Fatal(err)
				}
			default:
				id := attack[len("revoke-"):]
				if err := s.PersistAuthorizationUpdate(ctx, AuthorizationUpdate{IssuerID: "hub-one", NodeID: "node", BootID: "boot", UpdateID: "revoke", Payload: []byte("revoke"), SignerEpoch: 1, PathGeneration: 1, FencingGenerationFloor: 1}, nil, []string{id}); err != nil {
					t.Fatal(err)
				}
			}
			before, err := s.AuditAuthorizationSource(ctx, "node", "funding")
			if err != nil {
				t.Fatal(err)
			}
			if err := s.AdmitRuntimeFundingBlockWrite(ctx, block, use, 100); err == nil {
				t.Fatal("attack admitted")
			}
			after, err := s.AuditAuthorizationSource(ctx, "node", "funding")
			if err != nil || before != after {
				t.Fatalf("funding changed: %+v %+v %v", before, after, err)
			}
			var buckets int
			if err := s.db.QueryRow(`SELECT COUNT(*) FROM runtime_rate_buckets`).Scan(&buckets); err != nil || buckets != 0 {
				t.Fatalf("denial changed rate: %d %v", buckets, err)
			}
		})
	}
}

func TestRuntimeBlockAdmissionSharesRateWithoutSpendingFunding(t *testing.T) {
	s, scope, _ := runtimeStore(t)
	ctx := context.Background()
	a, err := s.OpenRuntimeSession(ctx, scope, "session", "flow")
	if err != nil {
		t.Fatal(err)
	}
	block := runtimeReservation("block", 256<<10)
	if _, err := s.ReserveRuntimeFundingBlock(ctx, block, a.Use); err != nil {
		t.Fatal(err)
	}
	if err := s.AdmitRuntimeFundingBlockWrite(ctx, block, a.Use, 32768); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ReserveRuntimeUsage(ctx, runtimeReservation("ordinary", 32768), a.Use); err != nil {
		t.Fatal(err)
	}
	if err := s.AdmitRuntimeFundingBlockWrite(ctx, block, a.Use, 1); err == nil {
		t.Fatal("shared rate bypass")
	}
	b, err := s.AuditAuthorizationSource(ctx, "node", "funding")
	if err != nil || b.Reserved != (256<<10)+32768 || b.Remaining != (1<<20)-(256<<10)-32768 || b.Committed != 0 {
		t.Fatalf("admission debited funding: %+v %v", b, err)
	}
}
