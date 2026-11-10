package agent

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"vortex/agent/store"
	controlv1 "vortex/contracts/protobuf/v1"
	accounting "vortex/engine/accounting"
)

func TestAdaptiveDemandBridgesDatagramAboveWaterline(t *testing.T) {
	ctx := context.Background()
	db, err := store.Open(filepath.Join(t.TempDir(), "agent.db"), "agent")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	n, err := New(controlTestNodeID, db, nil)
	if err != nil {
		t.Fatal(err)
	}
	hub := "550e8400-e29b-41d4-a716-446655440101"
	if err := (&ControlClient{Node: n}).applyAuthorizationUpdateFrom(ctx, &controlv1.AuthorizationUpdate{Generation: 4, Payload: leasedPayload(t, n.NodeID, n.BootID)}, hub); err != nil {
		t.Fatal(err)
	}
	at := time.Date(2026, 11, 5, 12, 1, 30, 0, time.UTC)
	n.clock = &AuthorityClock{now: func() time.Time { return at }, received: at, lower: at, upper: at, valid: true, anchored: true}
	scope := RuntimeScope{RuleID: "rule-1", RuleRevision: 2, PathGeneration: 4}
	gate, err := n.NewExperimentalFundingBlockGate(scope, n.TrustedTime, 4096)
	if err != nil {
		t.Fatal(err)
	}
	session, err := gate.Open(ctx, accounting.Flow{Key: "flow", RuleID: scope.RuleID})
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	spent, err := session.Reserve(ctx, accounting.LeftToRight, 2856)
	if err != nil {
		t.Fatal(err)
	}
	if err := spent.Commit(ctx, 2856); err != nil {
		t.Fatal(err)
	}
	// Return the proven idle tail so all 1240 bytes are visible to planning.
	s := session.(*fundingBlockSession)
	s.mu.Lock()
	for direction, block := range s.blocks {
		if err := s.release(ctx, block); err != nil {
			t.Fatal(err)
		}
		delete(s.blocks, direction)
	}
	s.mu.Unlock()
	config := AdaptiveDemandConfig{Scope: store.DemandScope{HubID: hub, DelegationID: "550e8400-e29b-41d4-a716-446655440022", NodeID: n.NodeID, BootID: n.BootID, SourceID: "550e8400-e29b-41d4-a716-446655440023", SnapshotID: "550e8400-e29b-41d4-a716-446655440024", RuleID: scope.RuleID, RuleRevision: scope.RuleRevision, PathGeneration: scope.PathGeneration, SignerEpoch: 1}, Policy: DemandPolicyConfig{Minimum: 4096, MaxTarget: 4096, TargetSeconds: 30, RenewPercent: 30}, TTL: time.Minute}
	request, estimate, err := n.PrepareAdaptiveDemand(ctx, config)
	if err != nil || request.ID != "" || estimate.RequestBytes != 0 {
		t.Fatalf("above waterline without waiter: %+v %+v %v", request, estimate, err)
	}
	wake, release, err := n.subscribeDemands([]AdaptiveDemandConfig{config})
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	waitCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	done := make(chan error, 1)
	go func() {
		batch, err := session.Reserve(waitCtx, accounting.LeftToRight, 1500)
		if batch != nil {
			_ = batch.Return(ctx)
			err = errors.New("planning request became write permission")
		}
		done <- err
	}()
	select {
	case <-wake:
	case <-time.After(time.Second):
		t.Fatal("missing credit did not wake planner")
	}
	request, estimate, err = n.PrepareAdaptiveDemand(ctx, config)
	if err != nil || request.RawBytes != 2856 || estimate.Target != 4096 {
		t.Fatalf("datagram deficit: %+v %+v %v", request, estimate, err)
	}
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("unfunded write: %v", err)
	}
	_, estimate, err = n.PrepareAdaptiveDemand(ctx, config)
	if err != nil || estimate.RequestBytes != 0 {
		t.Fatalf("cancelled waiter retained hint: %+v %v", estimate, err)
	}
	pending, err := db.PendingDemands(ctx, n.NodeID, n.BootID, hub, "")
	if err != nil || len(pending) != 1 || pending[0].ID != request.ID {
		t.Fatalf("cancellation retired durable demand: %+v %v", pending, err)
	}
}
