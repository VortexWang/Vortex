package store

import (
	"context"
	"crypto/sha256"
	"fmt"
	"math"
	"path/filepath"
	"testing"

	"google.golang.org/protobuf/proto"
	controlv1 "vortex/contracts/protobuf/v1"
)

func usageOriginSetFixture(t *testing.T, count int) (*Store, []HubUsage) {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "hub.db"), "hub")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	batches := make([]HubUsage, count)
	for i := range batches {
		id := fmt.Sprintf("batch-%02d", i)
		payload, err := proto.Marshal(&controlv1.UsageBatch{BatchId: id, BillingSnapshotId: "snapshot"})
		if err != nil {
			t.Fatal(err)
		}
		digest := sha256.Sum256(payload)
		batches[i] = HubUsage{NodeID: "node", BootID: "boot", BatchID: id, Kind: "known", Payload: payload, Digest: digest[:], ReportBootID: "first", Sequence: uint64(i + 1)}
	}
	return s, batches
}

func TestUsageOriginSetPreservesFirstEnvelopeAndDuplicateIdentity(t *testing.T) {
	for _, count := range []int{2, 9, 32} {
		t.Run(fmt.Sprint(count), func(t *testing.T) {
			s, batches := usageOriginSetFixture(t, count)
			ctx := context.Background()
			duplicate := batches[0]
			duplicate.ReportBootID = "later"
			duplicate.Sequence = 1000
			batches[len(batches)-1] = duplicate
			for round := 0; round < 2; round++ {
				got, err := s.PersistKnownUsageGroup(ctx, batches, true)
				if err != nil {
					t.Fatal(err)
				}
				for i, b := range got {
					seq := uint64(i + 1)
					if i == len(got)-1 {
						seq = 1
					}
					if b.ReportBootID != "first" || b.Sequence != seq {
						t.Fatalf("changed origin: %+v", b)
					}
				}
				for i := range batches {
					batches[i].ReportBootID = "retry"
					batches[i].Sequence += 2000
				}
			}
			var n int
			if err := s.db.QueryRow(`SELECT COUNT(*) FROM usage_origin_envelopes`).Scan(&n); err != nil || n != count-1 {
				t.Fatalf("origin count=%d %v", n, err)
			}
		})
	}
}

func TestUsageOriginSetRejectsInvalidEnvelopeAndPartialInsert(t *testing.T) {
	for _, variant := range []string{"missing-boot", "zero", "overflow", "sequence-collision", "abort", "ignore"} {
		t.Run(variant, func(t *testing.T) {
			s, batches := usageOriginSetFixture(t, 9)
			ctx := context.Background()
			switch variant {
			case "missing-boot":
				batches[8].ReportBootID = ""
			case "zero":
				batches[8].Sequence = 0
			case "overflow":
				batches[8].Sequence = uint64(math.MaxInt64) + 1
			case "sequence-collision":
				batches[8].Sequence = batches[0].Sequence
			default:
				failure := `RAISE(ABORT,'FULL failed')`
				if variant == "ignore" {
					failure = `RAISE(IGNORE)`
				}
				if _, err := s.db.Exec(`CREATE TRIGGER fail_origin BEFORE INSERT ON usage_origin_envelopes WHEN NEW.batch_id='batch-08' BEGIN SELECT ` + failure + `; END`); err != nil {
					t.Fatal(err)
				}
			}
			if got, err := s.PersistKnownUsageGroup(ctx, batches, true); err == nil || got != nil {
				t.Fatal("invalid/partial provenance acknowledged", err)
			}
			for _, table := range []string{"usage_batches", "usage_origin_envelopes", "hub_ledger"} {
				var n int
				if err := s.db.QueryRow(`SELECT COUNT(*) FROM ` + table).Scan(&n); err != nil || n != 0 {
					t.Fatalf("partial %s=%d %v", table, n, err)
				}
			}
		})
	}
}

func TestUsageOriginSetMissingCompactedPayloadCannotInventProvenance(t *testing.T) {
	s, batches := usageOriginSetFixture(t, 2)
	ctx := context.Background()
	if _, err := s.PersistKnownUsageGroup(ctx, batches, true); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec(`INSERT INTO settled_usage_tombstones(node_id,batch_id,snapshot_id,payload_sha256,ledger_sequence) SELECT node_id,batch_id,snapshot_id,payload_sha256,1 FROM usage_batches WHERE batch_id='batch-00'; DELETE FROM usage_batches WHERE batch_id='batch-00'; DELETE FROM usage_origin_envelopes WHERE batch_id='batch-00'`); err != nil {
		t.Fatal(err)
	}
	if got, err := s.PersistKnownUsageGroup(ctx, batches, true); err == nil || got != nil {
		t.Fatal("invented provenance for absent payload", err)
	}
	var n int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM usage_origin_envelopes WHERE batch_id='batch-00'`).Scan(&n); err != nil || n != 0 {
		t.Fatalf("invented origin=%d %v", n, err)
	}
}
