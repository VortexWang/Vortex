package agent

import (
	"context"
	"testing"

	"vortex/agent/store"
	accounting "vortex/engine/accounting"
)

func TestFundingFragmentsConserveAcrossOutcomes(t *testing.T) {
	for _, mode := range []string{"short-close", "unknown", "canceled", "insufficient"} {
		t.Run(mode, func(t *testing.T) {
			n, session := fundingSession(t)
			s := session.(*fundingBlockSession)
			ctx := context.Background()
			low, high, trusted := s.inner.gate.config.Clock()
			use := s.inner.gate.use(low, high, trusted, true)
			use.SessionID = ""
			r, err := n.Store.CheckStoredAuthorization(ctx, use)
			if err != nil {
				t.Fatal(err)
			}
			r.AuthorizationID = "650e8400-e29b-41d4-a716-446655440099"
			r.AmountBytes = 64
			if err := n.Store.PersistAuthorizationUpdate(ctx, store.AuthorizationUpdate{IssuerID: r.IssuerID, NodeID: n.NodeID, BootID: n.BootID, UpdateID: "fragment-topup", Payload: []byte("fragment-topup"), SignerEpoch: r.SignerEpoch, PathGeneration: r.PathGeneration, FencingGenerationFloor: 8}, []store.AuthorizationRecord{r}, nil); err != nil {
				t.Fatal(err)
			}
			requested := int64(100)
			if mode == "insufficient" {
				requested = 5000
			}
			s.mu.Lock()
			batch, err := s.reserveFragments(ctx, accounting.LeftToRight, requested)
			s.mu.Unlock()
			if mode == "insufficient" {
				if err == nil || batch != nil {
					t.Fatal("partial collection admitted")
				}
				if b := fundingBalance(t, n); b.Reserved != 0 || b.Remaining != 4160 || b.Unknown != 0 {
					t.Fatalf("unused fragments leaked %+v", b)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			fresh, err := n.Store.AuditAuthorizationBalance(ctx, n.NodeID, r.AuthorizationID)
			if err != nil || fresh.Reserved != 64 {
				t.Fatalf("first fragment %+v %v", fresh, err)
			}
			if err := s.Close(); err != nil {
				t.Fatal(err)
			}
			switch mode {
			case "short-close":
				if err := batch.Commit(ctx, 70); err != nil {
					t.Fatal(err)
				}
				if b := fundingBalance(t, n); b.Committed != 70 || b.Reserved != 0 || b.Remaining != 4090 || b.Unknown != 0 {
					t.Fatalf("short write %+v", b)
				}
			case "unknown":
				if err := batch.MarkUnknown(ctx); err != nil {
					t.Fatal(err)
				}
				if b := fundingBalance(t, n); b.Unknown != 100 || b.Reserved != 0 || b.Remaining != 4060 {
					t.Fatalf("unknown %+v", b)
				}
			case "canceled":
				canceled, stop := context.WithCancel(ctx)
				stop()
				if err := batch.Commit(canceled, 70); err == nil {
					t.Fatal("canceled settlement acknowledged")
				}
				if b := fundingBalance(t, n); b.Committed != 0 || b.Reserved+b.Unknown != 100 || b.Remaining != 4060 {
					t.Fatalf("uncertain settlement refunded %+v", b)
				}
			}
		})
	}
}
