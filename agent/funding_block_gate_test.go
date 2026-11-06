package agent

import (
	"context"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"google.golang.org/protobuf/proto"
	"vortex/agent/store"
	controlv1 "vortex/contracts/protobuf/v1"
	accounting "vortex/engine/accounting"
)

func fundingSession(t *testing.T) (*Node, accounting.Session) {
	t.Helper()
	s, err := store.Open(filepath.Join(t.TempDir(), "agent.db"), "agent")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	n, err := New(controlTestNodeID, s, nil)
	if err != nil {
		t.Fatal(err)
	}
	client := &ControlClient{Node: n}
	if err := client.applyAuthorizationUpdate(context.Background(), &controlv1.AuthorizationUpdate{Generation: 4, Payload: leasedPayload(t, n.NodeID, n.BootID)}); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 11, 5, 12, 1, 30, 0, time.UTC)
	gate, err := n.NewExperimentalFundingBlockGate(RuntimeScope{RuleID: "rule-1", RuleRevision: 2, PathGeneration: 4}, func() (time.Time, time.Time, bool) { return now, now, true }, 128)
	if err != nil {
		t.Fatal(err)
	}
	session, err := gate.Open(context.Background(), accounting.Flow{Key: "flow", RuleID: "rule-1"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { session.Close() })
	return n, session
}
func fundingBalance(t *testing.T, n *Node) store.AuthorizationBalance {
	t.Helper()
	b, err := n.Store.AuditAuthorizationSource(context.Background(), n.NodeID, "550e8400-e29b-41d4-a716-446655440023")
	if err != nil {
		t.Fatal(err)
	}
	return b
}
func TestFundingBlockGateConcurrentShortWritesAndClose(t *testing.T) {
	n, s := fundingSession(t)
	ctx := context.Background()
	var wg sync.WaitGroup
	errs := make(chan error, 16)
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			b, err := s.Reserve(ctx, accounting.LeftToRight, 32)
			if err == nil {
				err = b.Commit(ctx, 17)
			}
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	b := fundingBalance(t, n)
	if b.Committed != 16*17 || b.Remaining != 4096-16*17 || b.Reserved != 0 || b.Unknown != 0 {
		t.Fatalf("conservation: %+v", b)
	}
	batches, err := n.Store.PendingUsageOutbox(ctx, n.NodeID)
	if err != nil {
		t.Fatal(err)
	}
	var total uint64
	for _, entry := range batches {
		var usage controlv1.UsageBatch
		if err := proto.Unmarshal(entry.Payload, &usage); err != nil {
			t.Fatal(err)
		}
		for _, d := range usage.Deltas {
			total += d.Bytes
		}
	}
	if total != 16*17 {
		t.Fatalf("wire usage %d", total)
	}
}
func TestFundingBlockGateCloseJoinsOutstandingResults(t *testing.T) {
	n, s := fundingSession(t)
	ctx := context.Background()
	left, err := s.Reserve(ctx, accounting.LeftToRight, 60)
	if err != nil {
		t.Fatal(err)
	}
	right, err := s.Reserve(ctx, accounting.RightToLeft, 60)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	if b := fundingBalance(t, n); b.Reserved != 256 {
		t.Fatalf("early refund: %+v", b)
	}
	if _, err := s.Reserve(ctx, accounting.LeftToRight, 1); err == nil {
		t.Fatal("closed session admitted")
	}
	if err := left.Commit(ctx, 23); err != nil {
		t.Fatal(err)
	}
	if err := right.Return(ctx); err != nil {
		t.Fatal(err)
	}
	if b := fundingBalance(t, n); b.Reserved != 0 || b.Committed != 23 || b.Remaining != 4096-23 {
		t.Fatalf("close: %+v", b)
	}
}
func TestFundingBlockGateUnknownFreezesResidualAndStopsWrites(t *testing.T) {
	n, s := fundingSession(t)
	ctx := context.Background()
	first, err := s.Reserve(ctx, accounting.LeftToRight, 40)
	if err != nil {
		t.Fatal(err)
	}
	second, err := s.Reserve(ctx, accounting.LeftToRight, 40)
	if err != nil {
		t.Fatal(err)
	}
	if err := first.Commit(ctx, 20); err != nil {
		t.Fatal(err)
	}
	if err := second.MarkUnknown(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Reserve(ctx, accounting.RightToLeft, 1); err == nil {
		t.Fatal("faulted session admitted")
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	if b := fundingBalance(t, n); b.Committed != 20 || b.Unknown != 108 || b.Reserved != 0 || b.Remaining != 4096-128 {
		t.Fatalf("unknown: %+v", b)
	}
}
func TestFundingBlockGateWaitingReservationCancellation(t *testing.T) {
	n, s := fundingSession(t)
	ctx := context.Background()
	b, err := s.Reserve(ctx, accounting.LeftToRight, 128)
	if err != nil {
		t.Fatal(err)
	}
	wait, cancel := context.WithTimeout(ctx, 20*time.Millisecond)
	defer cancel()
	if _, err := s.Reserve(wait, accounting.LeftToRight, 1); err == nil {
		t.Fatal("overallocated")
	}
	if err := b.Return(ctx); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	if balance := fundingBalance(t, n); balance.Remaining != 4096 || balance.Reserved != 0 {
		t.Fatalf("cancel: %+v", balance)
	}
}
