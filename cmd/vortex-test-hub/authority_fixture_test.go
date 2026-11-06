package main

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"vortex/agent/store"
	controlv1 "vortex/contracts/protobuf/v1"
)

func TestAuthorityFixtureBindsBootAndReplaysDurably(t *testing.T) {
	ctx := context.Background()
	id := func(n int) string { return fmt.Sprintf("550e8400-e29b-41d4-a716-%012d", n) }
	now := time.Now().UTC().Add(-time.Second)
	c := authorityFixture{Delegation: store.HubDelegation{ID: id(1), HubID: id(2), SourceID: id(3), RuntimeSourceID: id(4), ShareSourceID: id(5), NodeID: id(6), BillingSnapshotID: id(7), RuleID: "rule", SignerEpoch: 1, RuleRevision: 1, PathGeneration: 1, FencingFloor: 1, RawBytes: 8192, RateBPS: 1000000, Connections: 16, ValidFrom: now, ExpiresAt: now.Add(time.Hour), HardDeadline: now.Add(time.Hour), BackendHeartbeat: now, AutonomyDeadline: now.Add(2 * time.Hour)}, InitialRequestID: id(8), InitialBytes: 4096, TTLSeconds: 60}
	path := filepath.Join(t.TempDir(), "hub.db")
	s, err := store.Open(path, "hub")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { s.Close() }()
	backend, err := store.Open(filepath.Join(t.TempDir(), "backend.db"), "hub")
	if err != nil {
		t.Fatal(err)
	}
	defer backend.Close()
	fixture := configureAuthorityFixture(ctx, s, backend, c)
	if _, err := fixture.AuthorizationUpdate(id(99), id(9)); err == nil {
		t.Fatal("wrong node admitted")
	}
	initial, err := fixture.AuthorizationUpdate(id(6), id(9))
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = store.Open(path, "hub")
	if err != nil {
		t.Fatal(err)
	}
	fixture = configureAuthorityFixture(ctx, s, backend, c)
	repeated, err := fixture.AuthorizationUpdate(id(6), id(9))
	if err != nil {
		t.Fatal(err)
	}
	if string(initial.Payload) != string(repeated.Payload) {
		t.Fatal("reconnect changed initial issue")
	}
	if _, err := fixture.AuthorizationUpdate(id(6), id(10)); err == nil {
		t.Fatal("new boot reused delegation")
	}
	balance, err := s.AuditHubDelegation(ctx, id(1))
	if err != nil || balance.Issued != 4096 || balance.Remaining != 4096 {
		t.Fatalf("issuance %+v %v", balance, err)
	}
	var payload struct {
		Authorizations []struct {
			ID   string `json:"authorization_id"`
			Kind string `json:"kind"`
		} `json:"authorizations"`
	}
	if err := json.Unmarshal(initial.Payload, &payload); err != nil {
		t.Fatal(err)
	}
	grant := ""
	for _, r := range payload.Authorizations {
		if r.Kind == "credit_grant" {
			grant = r.ID
		}
	}
	if grant == "" {
		t.Fatal("missing issued grant")
	}
	usage := &controlv1.UsageBatch{BatchId: id(11), BillingSnapshotId: id(7), Deltas: []*controlv1.UsageDelta{{RuleId: "rule", Role: "source", Direction: "egress", Bytes: 100, GrantId: grant}}}
	for i := 0; i < 2; i++ {
		if err := fixture.Usage.AcceptKnown(ctx, id(6), id(9), usage); err != nil {
			t.Fatal(err)
		}
	}
}
