package agent

import (
	"context"
	"encoding/json"
	"fmt"
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
	if err := client.applyAuthorizationUpdateFrom(context.Background(), &controlv1.AuthorizationUpdate{Generation: 4, Payload: leasedPayload(t, n.NodeID, n.BootID)}, "hub-one"); err != nil {
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

func TestFundingBlockGateIdleReturnDoesNotRefundInFlight(t *testing.T) {
	n, session := fundingSession(t)
	s := session.(*fundingBlockSession)
	s.idleTimeout = 15 * time.Millisecond
	ctx := context.Background()
	b, err := s.Reserve(ctx, accounting.LeftToRight, 60)
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(40 * time.Millisecond)
	if balance := fundingBalance(t, n); balance.Reserved != 128 {
		t.Fatalf("inflight refunded: %+v", balance)
	}
	if err := b.Commit(ctx, 23); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(time.Second)
	for fundingBalance(t, n).Reserved != 0 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if balance := fundingBalance(t, n); balance.Reserved != 0 || balance.Committed != 23 || balance.Remaining != 4096-23 {
		t.Fatalf("idle return: %+v", balance)
	}
	b, err = s.Reserve(ctx, accounting.LeftToRight, 60)
	if err != nil {
		t.Fatal(err)
	}
	if err := b.Return(ctx); err != nil {
		t.Fatal(err)
	}
}
func TestFundingBlockGateUsesRemainingScopedCreditBelowTarget(t *testing.T) {
	n, session := fundingSession(t)
	s := session.(*fundingBlockSession)
	s.size = 4000
	ctx := context.Background()
	first, err := s.Reserve(ctx, accounting.LeftToRight, 4000)
	if err != nil {
		t.Fatal(err)
	}
	if err := first.Commit(ctx, 4000); err != nil {
		t.Fatal(err)
	}
	last, err := s.Reserve(ctx, accounting.LeftToRight, 96)
	if err != nil {
		t.Fatal(err)
	}
	if err := last.Commit(ctx, 96); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Reserve(ctx, accounting.LeftToRight, 1); err == nil {
		t.Fatal("unfunded write accepted")
	}
	if balance := fundingBalance(t, n); balance.Committed != 4096 || balance.Reserved != 0 || balance.Remaining != 0 {
		t.Fatalf("remaining: %+v", balance)
	}
}
func TestFundingBlockGateRefreshesExpiredIdleAuthority(t *testing.T) {
	n, session := fundingSession(t)
	s := session.(*fundingBlockSession)
	ctx := context.Background()
	b, err := s.Reserve(ctx, accounting.LeftToRight, 60)
	if err != nil {
		t.Fatal(err)
	}
	if err := b.Commit(ctx, 20); err != nil {
		t.Fatal(err)
	}
	var body map[string]any
	if err := json.Unmarshal(leasedPayload(t, n.NodeID, n.BootID), &body); err != nil {
		t.Fatal(err)
	}
	body["update_id"] = "550e8400-e29b-41d4-a716-446655440120"
	for i, entry := range body["authorizations"].([]any) {
		r := entry.(map[string]any)
		r["authorization_id"] = fmt.Sprintf("550e8400-e29b-41d4-a716-44665544012%d", i+1)
		r["issued_at"] = "2026-11-05T12:02:00Z"
		r["not_before"] = "2026-11-05T12:02:00Z"
		r["expires_at"] = "2026-11-05T12:06:00Z"
	}
	payload, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	if err := (&ControlClient{Node: n}).applyAuthorizationUpdateFrom(ctx, &controlv1.AuthorizationUpdate{Generation: 4, Payload: payload}, "hub-one"); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 11, 5, 12, 4, 0, 0, time.UTC)
	s.mu.Lock()
	s.inner.gate.config.Clock = func() (time.Time, time.Time, bool) { return now, now, true }
	s.mu.Unlock()
	b, err = s.Reserve(ctx, accounting.LeftToRight, 60)
	if err != nil {
		t.Fatal(err)
	}
	if err := b.Commit(ctx, 30); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	if balance := fundingBalance(t, n); balance.Initial != 8192 || balance.Committed != 50 || balance.Reserved != 0 || balance.Remaining != 8192-50 {
		t.Fatalf("renewal: %+v", balance)
	}
	batches, err := n.Store.PendingUsageOutbox(ctx, n.NodeID)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, entry := range batches {
		var batch controlv1.UsageBatch
		if err := proto.Unmarshal(entry.Payload, &batch); err != nil {
			t.Fatal(err)
		}
		for _, d := range batch.Deltas {
			if d.Bytes == 30 && d.GrantId == "550e8400-e29b-41d4-a716-446655440121" {
				found = true
			}
		}
	}
	if !found {
		t.Fatal("new usage did not bind renewed grant")
	}
}

func TestFundingBlockGateReclaimsOnlyIdleOppositeDirection(t *testing.T) {
	n, session := fundingSession(t)
	s := session.(*fundingBlockSession)
	s.size = 4096
	ctx := context.Background()
	first, err := s.Reserve(ctx, accounting.LeftToRight, 1500)
	if err != nil {
		t.Fatal(err)
	}
	waitCtx, cancel := context.WithTimeout(ctx, 20*time.Millisecond)
	defer cancel()
	if _, err := s.Reserve(waitCtx, accounting.RightToLeft, 1500); err == nil {
		t.Fatal("reclaimed in-flight funding")
	}
	if b := fundingBalance(t, n); b.Reserved != 4096 || b.Remaining != 0 {
		t.Fatalf("inflight changed: %+v", b)
	}
	if err := first.Commit(ctx, 1500); err != nil {
		t.Fatal(err)
	}
	second, err := s.Reserve(ctx, accounting.RightToLeft, 1500)
	if err != nil {
		t.Fatal(err)
	}
	if err := second.Commit(ctx, 1500); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	if b := fundingBalance(t, n); b.Committed != 3000 || b.Remaining != 1096 || b.Reserved != 0 {
		t.Fatalf("reclaim conservation: %+v", b)
	}
}

func TestFundingBlockGateResponseWaitsForOutboundCheckpoint(t *testing.T) {
	n, session := fundingSession(t)
	s := session.(*fundingBlockSession)
	s.size = 4096
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	outbound, err := s.Reserve(ctx, accounting.LeftToRight, 1500)
	if err != nil {
		t.Fatal(err)
	}
	result := make(chan error, 1)
	go func() {
		response, err := s.Reserve(ctx, accounting.RightToLeft, 1500)
		if err == nil {
			err = response.Commit(ctx, 1500)
		}
		result <- err
	}()
	select {
	case err := <-result:
		t.Fatalf("response did not wait: %v", err)
	case <-time.After(30 * time.Millisecond):
	}
	if err := outbound.Commit(ctx, 1500); err != nil {
		t.Fatal(err)
	}
	if err := <-result; err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	if b := fundingBalance(t, n); b.Committed != 3000 || b.Reserved != 0 || b.Remaining != 1096 {
		t.Fatalf("response conservation: %+v", b)
	}
}

func TestFundingBlockGateAdmissionDeadlineWhileAllocatorBusy(t *testing.T) {
	n, session := fundingSession(t)
	s := session.(*fundingBlockSession)
	s.mu.Lock()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	done := make(chan error, 1)
	go func() { _, err := s.Reserve(ctx, accounting.LeftToRight, 1); done <- err }()
	select {
	case err := <-done:
		if err != context.DeadlineExceeded {
			t.Fatalf("deadline: %v", err)
		}
	case <-time.After(time.Second):
		s.mu.Unlock()
		t.Fatal("allocator lock ignored admission cancellation")
	}
	s.mu.Unlock()
	b, err := s.Reserve(context.Background(), accounting.LeftToRight, 1)
	if err != nil {
		t.Fatal(err)
	}
	if err := b.Return(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	if balance := fundingBalance(t, n); balance.Remaining != 4096 || balance.Reserved != 0 {
		t.Fatalf("canceled queue changed funds: %+v", balance)
	}
}
