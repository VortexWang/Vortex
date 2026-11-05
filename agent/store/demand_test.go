package store

import (
	"bytes"
	"context"
	"encoding/json"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func TestDemandCoalescesAcrossRestartAndLostIssuerResponse(t *testing.T) {
	hub, d, clock, _ := issuerFixture(t)
	path := filepath.Join(t.TempDir(), "agent.db")
	agent, err := Open(path, "agent")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { agent.Close() }()
	scope := DemandScope{HubID: d.HubID, DelegationID: d.ID, NodeID: d.NodeID, BootID: d.BootID, SourceID: d.SourceID, SnapshotID: d.BillingSnapshotID, RuleID: d.RuleID, SignerEpoch: d.SignerEpoch, RuleRevision: d.RuleRevision, PathGeneration: d.PathGeneration}
	ctx := context.Background()
	first, err := agent.PrepareDemand(ctx, scope, 256, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r, e := agent.PrepareDemand(ctx, scope, 512, 2*time.Minute)
			if e != nil || r != first {
				t.Errorf("concurrent demand changed: %+v %v", r, e)
			}
		}()
	}
	wg.Wait()
	issued, err := hub.IssueHubAuthorization(ctx, first, clock)
	if err != nil {
		t.Fatal(err)
	}
	// Simulate a lost response and actual Agent close/reopen. Larger demand
	// cannot allocate a new issue identity or cause a second Hub debit.
	if err := agent.Close(); err != nil {
		t.Fatal(err)
	}
	agent, err = Open(path, "agent")
	if err != nil {
		t.Fatal(err)
	}
	retry, err := agent.PrepareDemand(ctx, scope, 1024, 3*time.Minute)
	if err != nil || retry != first {
		t.Fatalf("restart replay: %+v %v", retry, err)
	}
	replay, err := hub.IssueHubAuthorization(ctx, retry, clock)
	if err != nil || !bytes.Equal(issued.Payload, replay.Payload) {
		t.Fatalf("issuer replay: %v", err)
	}
	balance, err := hub.AuditHubDelegation(ctx, d.ID)
	if err != nil || balance.Issued != 256 || balance.Remaining != 768 {
		t.Fatalf("duplicate debit: %+v %v", balance, err)
	}
	var envelope struct {
		Authorizations []struct {
			ID   string `json:"authorization_id"`
			Kind string `json:"kind"`
		} `json:"authorizations"`
	}
	if err := json.Unmarshal(issued.Payload, &envelope); err != nil {
		t.Fatal(err)
	}
	var grant string
	for _, a := range envelope.Authorizations {
		if a.Kind == "credit_grant" {
			grant = a.ID
		}
	}
	if err := agent.CompleteDemand(ctx, first.ID, grant); err == nil {
		t.Fatal("uninstalled response cleared pending demand")
	}
	record := AuthorizationRecord{AuthorizationID: grant, ParentAuthorizationID: d.ID, SourceID: d.SourceID, Kind: "credit_grant", RuleID: d.RuleID, RuleRevision: d.RuleRevision, PathGeneration: d.PathGeneration, SignerEpoch: d.SignerEpoch, BootID: d.BootID, IssuedAt: clock.Latest.Format(time.RFC3339Nano), NotBefore: clock.Latest.Format(time.RFC3339Nano), ExpiresAt: clock.Latest.Add(time.Minute).Format(time.RFC3339Nano), HardDeadline: d.HardDeadline.Format(time.RFC3339Nano), BillingSnapshotID: d.BillingSnapshotID, AmountBytes: 256}
	if err := agent.PersistAuthorizationUpdate(ctx, AuthorizationUpdate{IssuerID: d.HubID, NodeID: d.NodeID, BootID: d.BootID, UpdateID: first.ID, Payload: issued.Payload, SignerEpoch: d.SignerEpoch, PathGeneration: d.PathGeneration, FencingGenerationFloor: d.FencingFloor}, []AuthorizationRecord{record}, nil); err != nil {
		t.Fatal(err)
	}
	// A response installed under the same update ID still cannot resolve a
	// different immutable scope or impersonate another issuer.
	for _, attack := range []struct{ column, original, forged string }{
		{"issuer_id", d.HubID, d.NodeID},
		{"parent_authorization_id", d.ID, d.NodeID},
		{"source_id", d.SourceID, d.NodeID},
		{"billing_snapshot_id", d.BillingSnapshotID, d.NodeID},
		{"rule_id", d.RuleID, "different-rule"},
		{"rule_revision", "2", "3"},
		{"path_generation", "4", "5"},
		{"signer_epoch", "1", "2"},
		{"boot_id", d.BootID, d.NodeID},
		{"amount_bytes", "256", "512"},
	} {
		t.Run("mismatched-"+attack.column, func(t *testing.T) {
			query := "UPDATE authorization_records SET " + attack.column + "=? WHERE node_id=? AND authorization_id=?"
			if _, err := agent.db.Exec(query, attack.forged, d.NodeID, grant); err != nil {
				t.Fatal(err)
			}
			if err := agent.CompleteDemand(ctx, first.ID, grant); err == nil {
				t.Fatal("mismatched response cleared pending demand")
			}
			if _, err := agent.db.Exec(query, attack.original, d.NodeID, grant); err != nil {
				t.Fatal(err)
			}
			retry, err := agent.PrepareDemand(ctx, scope, 512, time.Minute)
			if err != nil || retry != first {
				t.Fatalf("rejected response replaced pending request: %+v %v", retry, err)
			}
		})
	}
	// Completion must also survive a crash after installation but before ack.
	if err := agent.Close(); err != nil {
		t.Fatal(err)
	}
	agent, err = Open(path, "agent")
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if err := agent.CompleteDemand(ctx, first.ID, grant); err != nil {
			t.Fatal(err)
		}
	}
	if err := agent.CompleteDemand(ctx, first.ID, d.ID); err == nil {
		t.Fatal("changed completion accepted")
	}
	next, err := agent.PrepareDemand(ctx, scope, 512, 2*time.Minute)
	if err != nil || next.ID == first.ID || next.RawBytes != 512 {
		t.Fatalf("next demand: %+v %v", next, err)
	}
	funded, err := agent.AuditAuthorizationSource(ctx, d.NodeID, d.SourceID)
	if err != nil || funded.Initial != 256 || funded.Remaining != 256 {
		t.Fatalf("completion changed installed funds: %+v %v", funded, err)
	}
}

func TestDemandPersistenceFailureReturnsNoRequest(t *testing.T) {
	_, d, _, _ := issuerFixture(t)
	agent, err := Open(filepath.Join(t.TempDir(), "agent.db"), "agent")
	if err != nil {
		t.Fatal(err)
	}
	defer agent.Close()
	scope := DemandScope{HubID: d.HubID, DelegationID: d.ID, NodeID: d.NodeID, BootID: d.BootID, SourceID: d.SourceID, SnapshotID: d.BillingSnapshotID, RuleID: d.RuleID, SignerEpoch: d.SignerEpoch, RuleRevision: d.RuleRevision, PathGeneration: d.PathGeneration}
	if _, err := agent.db.Exec(`CREATE TRIGGER fail_demand BEFORE INSERT ON authorization_demands BEGIN SELECT RAISE(ABORT,'disk failure'); END`); err != nil {
		t.Fatal(err)
	}
	r, err := agent.PrepareDemand(context.Background(), scope, 256, time.Minute)
	if err == nil || r.ID != "" {
		t.Fatalf("failed commit returned dispatchable request: %+v %v", r, err)
	}
}
