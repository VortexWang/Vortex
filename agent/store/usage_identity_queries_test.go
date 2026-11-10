package store

import (
	"context"
	"fmt"
	"testing"
)

func TestUsageIdentityLookupsKeepNodeScopeAndEveryShapeBoundary(t *testing.T) {
	for _, count := range []int{2, 3, 8, 9, 16, 17, 32} {
		t.Run(fmt.Sprint(count), func(t *testing.T) {
			s, batches := usageOriginSetFixture(t, count)
			ctx := context.Background()
			for i := range batches {
				// Adjacent identities share a batch ID across different nodes.
				// Quotes, JSON syntax and Unicode must remain literal values.
				batches[i] = knownGroupFixture(t, fmt.Sprintf("租户'\"[%d]", i%2), fmt.Sprintf("batch');--%d", i/2))
				batches[i].ReportBootID, batches[i].Sequence = "original", uint64(i+1)
			}
			persisted, err := s.PersistKnownUsageGroup(ctx, batches, true)
			if err != nil {
				t.Fatal(err)
			}
			pending, err := s.PendingKnownUsageGroup(ctx, persisted)
			if err != nil || len(pending) != count {
				t.Fatalf("pending=%d err=%v", len(pending), err)
			}
			if err := s.ConfirmBackendUsageGroup(ctx, pending); err != nil {
				t.Fatal(err)
			}
			for i := range batches {
				batches[i].ReportBootID, batches[i].Sequence = "retry", uint64(i+100)
			}
			replayed, err := s.PersistKnownUsageGroup(ctx, batches, true)
			if err != nil {
				t.Fatal(err)
			}
			for i, b := range replayed {
				if b.ReportBootID != "original" || b.Sequence != uint64(i+1) {
					t.Fatalf("cross-node origin: %+v", b)
				}
			}
			if pending, err := s.PendingKnownUsageGroup(ctx, replayed); err != nil || len(pending) != 0 {
				t.Fatalf("confirmed pending=%d err=%v", len(pending), err)
			}
			last := replayed[count-1]
			if _, err := s.db.Exec(`DELETE FROM usage_backend_receipts WHERE node_id=? AND batch_id=?`, last.NodeID, last.BatchID); err != nil {
				t.Fatal(err)
			}
			pending, err = s.PendingKnownUsageGroup(ctx, replayed)
			if err != nil || len(pending) != 1 || pending[0].NodeID != last.NodeID || pending[0].BatchID != last.BatchID {
				t.Fatalf("missing exact receipt: %+v %v", pending, err)
			}
			if err := s.ConfirmBackendUsageGroup(ctx, replayed); err != nil {
				t.Fatal(err)
			}
			if _, err := s.db.Exec(`UPDATE usage_backend_receipts SET payload_sha256=X'00' WHERE node_id=? AND batch_id=?`, last.NodeID, last.BatchID); err != nil {
				t.Fatal(err)
			}
			if _, err := s.PendingKnownUsageGroup(ctx, replayed); err == nil {
				t.Fatal("mismatched receipt suppressed redelivery")
			}
			if err := s.ConfirmBackendUsageGroup(ctx, replayed); err == nil {
				t.Fatal("mismatched receipt accepted")
			}
		})
	}
}
