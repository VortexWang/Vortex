package store

import (
	"context"
	"fmt"
	"math"
	"path/filepath"
	"testing"
	"time"

	"google.golang.org/protobuf/proto"
	controlv1 "vortex/contracts/protobuf/v1"
)

// This benchmarks only the durable per-write reservation/settlement boundary.
// It does not transmit bytes, include Hub issuance or implement session blocks.
func BenchmarkRuntimeDurableAccounting(b *testing.B) {
	for _, size := range []uint64{1200, 16 << 10, 64 << 10} {
		b.Run(fmt.Sprintf("bytes-%d", size), func(b *testing.B) {
			ctx := context.Background()
			s, err := Open(filepath.Join(b.TempDir(), "agent.db"), "agent")
			if err != nil {
				b.Fatal(err)
			}
			defer s.Close()
			info, err := s.Info(ctx)
			if err != nil || info.JournalMode != "wal" || info.Synchronous != "2" {
				b.Fatalf("requires WAL/FULL: %+v %v", info, err)
			}
			if uint64(b.N) > math.MaxInt64/size {
				b.Fatal("benchmark budget overflow")
			}
			total := uint64(b.N) * size
			at := time.Now().UTC()
			grant := AuthorizationRecord{AuthorizationID: "grant", ParentAuthorizationID: "parent", SourceID: "funding", Kind: "credit_grant", RuleID: "rule", RuleRevision: 1, PathGeneration: 1, SignerEpoch: 1, BootID: "boot", IssuedAt: at.Format(time.RFC3339Nano), NotBefore: at.Format(time.RFC3339Nano), ExpiresAt: at.Add(time.Hour).Format(time.RFC3339Nano), HardDeadline: at.Add(time.Hour).Format(time.RFC3339Nano), BillingSnapshotID: "snapshot", AmountBytes: total}
			runtime := grant
			runtime.AuthorizationID, runtime.Kind, runtime.AmountBytes, runtime.BillingSnapshotID = "runtime", "runtime_lease", 0, ""
			share := runtime
			share.AuthorizationID, share.Kind, share.SourceID = "share", "share_lease", "share-source"
			share.RateBPS, share.Connections = 1_000_000_000_000, 1
			if err := s.PersistAuthorizationUpdate(ctx, AuthorizationUpdate{IssuerID: "hub", NodeID: "node", BootID: "boot", UpdateID: "initial", Payload: []byte("benchmark-fixture"), SignerEpoch: 1, PathGeneration: 1, FencingGenerationFloor: 1}, []AuthorizationRecord{grant, runtime, share}, nil); err != nil {
				b.Fatal(err)
			}
			scope := AuthorizationUse{NodeID: "node", BootID: "boot", RuleID: "rule", RuleRevision: 1, PathGeneration: 1, Earliest: at, Latest: at, TrustedTime: true}
			session, err := s.OpenRuntimeSession(ctx, scope, "session", "flow")
			if err != nil {
				b.Fatal(err)
			}
			b.SetBytes(int64(size))
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				now := time.Now().UTC()
				use := session.Use
				use.Earliest, use.Latest = now, now
				batch := fmt.Sprintf("batch-%d", i)
				reservation := UsageReservation{NodeID: "node", BootID: "boot", BatchID: batch, SnapshotID: "snapshot", FlowKey: "flow", RuleID: "rule", Direction: "egress", Reserved: size}
				id, err := s.ReserveRuntimeUsage(ctx, reservation, use)
				if err != nil {
					b.Fatal(err)
				}
				reservation.AuthorizationID = id
				payload, err := (proto.MarshalOptions{Deterministic: true}).Marshal(&controlv1.UsageBatch{BatchId: batch, BillingSnapshotId: "snapshot", Deltas: []*controlv1.UsageDelta{{RuleId: "rule", Role: "execution", Direction: "egress", Bytes: size, GrantId: id}}})
				if err != nil {
					b.Fatal(err)
				}
				if err := s.SettleUsageReservation(ctx, reservation, "committed", size, payload, 0); err != nil {
					b.Fatal(err)
				}
			}
			b.StopTimer()
			if err := s.CloseRuntimeSession(ctx, "node", "boot", "session"); err != nil {
				b.Fatal(err)
			}
			balance, err := s.AuditAuthorizationSource(ctx, "node", "funding")
			if err != nil || balance.Initial != total || balance.Committed != total || balance.Remaining != 0 || balance.Reserved != 0 || balance.Unknown != 0 {
				b.Fatalf("failed conservation: %+v %v", balance, err)
			}
			var outbox int
			if err := s.db.QueryRow(`SELECT COUNT(*) FROM usage_batches WHERE state='pending'`).Scan(&outbox); err != nil || outbox != b.N {
				b.Fatalf("outbox count=%d want=%d err=%v", outbox, b.N, err)
			}
		})
	}
}
