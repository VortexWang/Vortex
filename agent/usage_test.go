package agent

import (
	"context"
	"path/filepath"
	"testing"

	"google.golang.org/protobuf/proto"
	"vortex/agent/store"
	controlv1 "vortex/contracts/protobuf/v1"
	engineaccounting "vortex/engine/accounting"
	runtimeaccounting "vortex/engine/runtime/accounting"
)

func TestNodeUsageSinkPersistsCommittedAndUnknownBounds(t *testing.T) {
	ctx := context.Background()
	s, err := store.Open(filepath.Join(t.TempDir(), "agent.db"), "agent")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	node, err := New(controlTestNodeID, s, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, record := range []engineaccounting.UsageRecord{
		{BatchID: "batch-committed", RuleID: "rule-1", SnapshotID: "snapshot-1", Direction: engineaccounting.LeftToRight, Resolution: engineaccounting.Committed, Reserved: 64, Used: 20},
		{BatchID: "batch-unknown", RuleID: "rule-1", SnapshotID: "snapshot-1", Direction: engineaccounting.RightToLeft, Resolution: engineaccounting.Unknown, Reserved: 32, Used: 32},
	} {
		if err := node.PersistUsage(ctx, record); err != nil {
			t.Fatal(err)
		}
	}
	pending, err := s.PendingUsageOutbox(ctx, controlTestNodeID)
	if err != nil || len(pending) != 2 {
		t.Fatalf("pending usage=%+v err=%v", pending, err)
	}
	for _, record := range pending {
		var batch controlv1.UsageBatch
		if err := proto.Unmarshal(record.Payload, &batch); err != nil {
			t.Fatal(err)
		}
		if batch.GetBatchId() != record.BatchID || batch.GetDeltas()[0].GetRuleId() != "rule-1" {
			t.Fatalf("usage payload mismatch: %+v / %s", record, batch.String())
		}
		if record.BatchID == "batch-committed" && (record.UnknownUpper != 0 || batch.GetDeltas()[0].GetBytes() != 20) {
			t.Fatalf("committed record=%+v delta=%+v", record, batch.GetDeltas()[0])
		}
		if record.BatchID == "batch-unknown" && (record.UnknownUpper != 32 || batch.GetDeltas()[0].GetBytes() != 32 || batch.GetDeltas()[0].GetDirection() != "ingress") {
			t.Fatalf("unknown record=%+v delta=%+v", record, batch.GetDeltas()[0])
		}
	}
}

func TestNodeCreditBookPersistsEngineUsageToSQLite(t *testing.T) {
	ctx := context.Background()
	s, err := store.Open(filepath.Join(t.TempDir(), "agent.db"), "agent")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	node, err := New(controlTestNodeID, s, nil)
	if err != nil {
		t.Fatal(err)
	}
	credit := runtimeaccounting.CreditConfig{SnapshotID: "snapshot-1", Budget: 256, InitialBlock: 64, MaxBlock: 64, GrantTarget: 64, RenewPercent: 30}
	book, err := node.NewCreditBook(runtimeaccounting.BookConfig{
		FundingBytes: 256, Active: "snapshot-1", SelectionRevision: 1,
		Snapshots: map[string]runtimeaccounting.CreditVersion{"snapshot-1": {
			Binding: runtimeaccounting.Binding{RuleID: "rule-1", ExecutionNode: controlTestNodeID, PathGeneration: 1, PolicyRevision: 1, CoefficientMilli: 1000},
			Credit:  credit,
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	session, err := book.Open(ctx, engineaccounting.Flow{Key: "opaque-stream-key"})
	if err != nil {
		t.Fatal(err)
	}
	reserved, err := session.Reserve(ctx, engineaccounting.LeftToRight, 32)
	if err != nil {
		t.Fatal(err)
	}
	if err := reserved.Commit(ctx, 24); err != nil {
		t.Fatal(err)
	}
	pending, err := s.PendingUsageOutbox(ctx, controlTestNodeID)
	if err != nil || len(pending) != 1 {
		t.Fatalf("Agent usage outbox=%+v err=%v", pending, err)
	}
	var batch controlv1.UsageBatch
	if err := proto.Unmarshal(pending[0].Payload, &batch); err != nil {
		t.Fatal(err)
	}
	if batch.GetBillingSnapshotId() != "snapshot-1" || len(batch.GetDeltas()) != 1 || batch.GetDeltas()[0].GetRuleId() != "rule-1" || batch.GetDeltas()[0].GetBytes() != 24 {
		t.Fatalf("Engine accounting record did not retain pinned billing identity: %s", batch.String())
	}
}

func TestNodeRestartFreezesUnresolvedEngineReservation(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "agent.db")
	s, err := store.Open(path, "agent")
	if err != nil {
		t.Fatal(err)
	}
	r := store.UsageReservation{NodeID: controlTestNodeID, BootID: "previous-boot", BatchID: "inflight-1", SnapshotID: "snapshot-1", FlowKey: "flow-1", RuleID: "rule-1", Direction: "ingress", Reserved: 77}
	if err := s.ReserveUsage(ctx, r); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = store.Open(path, "agent")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if _, err := New(controlTestNodeID, s, nil); err != nil {
		t.Fatal(err)
	}
	pending, err := s.PendingUsageOutbox(ctx, controlTestNodeID)
	if err != nil || len(pending) != 1 || pending[0].UnknownUpper != 77 {
		t.Fatalf("recovered unknown liability=%+v err=%v", pending, err)
	}
}
