package store

import (
	"context"
	"testing"
	"time"
)

func TestRuntimeBlockBatchCoalescesAndBoundsWrites(t *testing.T) {
	s, scope, _ := runtimeStore(t)
	ctx := context.Background()
	a, err := s.OpenRuntimeSession(ctx, scope, "session", "flow")
	if err != nil {
		t.Fatal(err)
	}
	block := runtimeReservation("block", 65536)
	if _, err := s.ReserveRuntimeFundingBlock(ctx, block, a.Use); err != nil {
		t.Fatal(err)
	}
	writes := make([]RuntimeBlockAdmission, 16)
	for i := range writes {
		writes[i] = RuntimeBlockAdmission{Block: block, Scope: a.Use, Bytes: 4096}
	}
	if err := s.AdmitRuntimeFundingBlockBatch(ctx, writes); err != nil {
		t.Fatal(err)
	}
	if err := s.AdmitRuntimeFundingBlockBatch(ctx, writes[:1]); err == nil {
		t.Fatal("batch bypassed shared rate")
	}
	var tokens uint64
	if err := s.db.QueryRow(`SELECT tokens FROM runtime_rate_buckets`).Scan(&tokens); err != nil || tokens != 0 {
		t.Fatalf("tokens %d %v", tokens, err)
	}
	b, err := s.AuditAuthorizationSource(ctx, "node", "funding")
	if err != nil || b.Reserved != 65536 || b.Committed != 0 || b.Remaining != (1<<20)-65536 {
		t.Fatalf("funding changed %+v %v", b, err)
	}
}

func TestRuntimeBlockBatchRollsBackEarlierRateOnLaterDenial(t *testing.T) {
	s, scope, _ := runtimeStore(t)
	ctx := context.Background()
	a, err := s.OpenRuntimeSession(ctx, scope, "session", "flow")
	if err != nil {
		t.Fatal(err)
	}
	left := runtimeReservation("left", 65536)
	right := runtimeReservation("right", 65536)
	right.Direction = "ingress"
	for _, b := range []UsageReservation{left, right} {
		if _, err := s.ReserveRuntimeFundingBlock(ctx, b, a.Use); err != nil {
			t.Fatal(err)
		}
	}
	writes := []RuntimeBlockAdmission{{Block: left, Scope: a.Use, Bytes: 40000}, {Block: right, Scope: a.Use, Bytes: 40000}}
	if err := s.AdmitRuntimeFundingBlockBatch(ctx, writes); err == nil {
		t.Fatal("aggregate rate exceeded")
	}
	var buckets int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM runtime_rate_buckets`).Scan(&buckets); err != nil || buckets != 0 {
		t.Fatalf("partial rate debit %d %v", buckets, err)
	}
	writes[1].Bytes = 1000
	if err := s.AdmitRuntimeFundingBlockBatch(ctx, writes); err != nil {
		t.Fatal(err)
	}
	var tokens uint64
	if err := s.db.QueryRow(`SELECT tokens FROM runtime_rate_buckets`).Scan(&tokens); err != nil || tokens != 65536-41000 {
		t.Fatalf("tokens %d %v", tokens, err)
	}
}

func TestRuntimeBlockBatchRejectsMutationAndConservativeTime(t *testing.T) {
	for _, variant := range []string{"capacity", "scope", "identity", "expired", "uncertain", "too-many", "canceled"} {
		t.Run(variant, func(t *testing.T) {
			s, scope, _ := runtimeStore(t)
			ctx := context.Background()
			a, err := s.OpenRuntimeSession(ctx, scope, "session", "flow")
			if err != nil {
				t.Fatal(err)
			}
			block := runtimeReservation("block", 100)
			if _, err := s.ReserveRuntimeFundingBlock(ctx, block, a.Use); err != nil {
				t.Fatal(err)
			}
			writes := []RuntimeBlockAdmission{{Block: block, Scope: a.Use, Bytes: 40}, {Block: block, Scope: a.Use, Bytes: 40}}
			switch variant {
			case "capacity":
				writes[1].Bytes = 61
			case "scope":
				writes[1].Scope.SessionID = "other"
			case "identity":
				writes[1].Block.FlowKey = "other"
			case "expired":
				writes[1].Scope.Latest = writes[1].Scope.Latest.Add(time.Hour)
			case "uncertain":
				writes[1].Scope.TrustedTime = false
			case "too-many":
				for len(writes) < 65 {
					writes = append(writes, writes[0])
				}
			case "canceled":
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			}
			if err := s.AdmitRuntimeFundingBlockBatch(ctx, writes); err == nil {
				t.Fatal("invalid batch admitted")
			}
			var buckets int
			if err := s.db.QueryRow(`SELECT COUNT(*) FROM runtime_rate_buckets`).Scan(&buckets); err != nil || buckets != 0 {
				t.Fatalf("partial rate debit %d %v", buckets, err)
			}
		})
	}
}
