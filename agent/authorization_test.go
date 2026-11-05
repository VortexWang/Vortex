package agent

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"vortex/agent/store"
	controlv1 "vortex/contracts/protobuf/v1"
)

func authorizationPayloadForTest(t *testing.T, nodeID, bootID string, signerEpoch, generation uint64) []byte {
	t.Helper()
	heartbeat := time.Date(2026, 11, 5, 12, 0, 0, 0, time.UTC)
	issued := heartbeat.Add(time.Minute)
	body := map[string]any{
		"schema_version": "1.0.0", "update_id": "550e8400-e29b-41d4-a716-446655440020",
		"node_id": nodeID, "boot_id": bootID, "signer_epoch": signerEpoch,
		"path_generation": generation, "fencing_generation_floor": uint64(8),
		"issued_at": issued.Format(time.RFC3339Nano), "backend_heartbeat_at": heartbeat.Format(time.RFC3339Nano),
		"autonomy_deadline": heartbeat.Add(12 * time.Hour).Format(time.RFC3339Nano),
		"authorizations": []any{map[string]any{
			"authorization_id":        "550e8400-e29b-41d4-a716-446655440021",
			"parent_authorization_id": "550e8400-e29b-41d4-a716-446655440022",
			"source_id":               "550e8400-e29b-41d4-a716-446655440023", "kind": "credit_grant",
			"rule_id": "rule-1", "rule_revision": uint64(2), "path_generation": generation,
			"issued_at": issued.Format(time.RFC3339Nano), "not_before": issued.Format(time.RFC3339Nano),
			"expires_at":          issued.Add(2 * time.Minute).Format(time.RFC3339Nano),
			"hard_deadline":       heartbeat.Add(time.Hour).Format(time.RFC3339Nano),
			"billing_snapshot_id": "550e8400-e29b-41d4-a716-446655440024", "amount_bytes": uint64(4096),
		}},
		"revocations": []string{},
		"restrictions": map[string]any{
			"existing_rules_only": true, "existing_shares_only": true, "existing_delegated_balances_only": true,
			"new_rules": false, "new_ports": false, "dns_changes": false, "registrations": false, "cross_node_rebalance": false,
		},
	}
	payload, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	return payload
}

func TestAuthorizationUpdateValidationAndDurableApply(t *testing.T) {
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
	if !canonicalUUID(node.BootID) {
		t.Fatalf("boot ID is not canonical UUID: %q", node.BootID)
	}
	payload := authorizationPayloadForTest(t, node.NodeID, node.BootID, 1, 4)
	client := &ControlClient{Node: node}
	update := &controlv1.AuthorizationUpdate{Generation: 4, Payload: payload}
	if err := client.applyAuthorizationUpdate(ctx, update); err != nil {
		t.Fatal(err)
	}
	if err := client.applyAuthorizationUpdate(ctx, update); err != nil {
		t.Fatalf("identical update replay failed: %v", err)
	}
	changed := append([]byte(nil), payload...)
	changed = []byte(strings.Replace(string(changed), `"amount_bytes":4096`, `"amount_bytes":4097`, 1))
	if err := client.applyAuthorizationUpdate(ctx, &controlv1.AuthorizationUpdate{Generation: 4, Payload: changed}); err == nil {
		t.Fatal("same update ID with different payload was accepted")
	}
	if err := client.applyAuthorizationUpdate(ctx, &controlv1.AuthorizationUpdate{Generation: 5, Payload: payload}); err == nil {
		t.Fatal("mismatched control and payload path generations were accepted")
	}
}

func TestAuthorizationUpdateRejectsDuplicateKeysAndMissingRestrictions(t *testing.T) {
	valid := `{"schema_version":"1.0.0","schema_version":"1.0.0"}`
	if _, err := validateAuthorizationUpdate([]byte(valid), controlTestNodeID, "550e8400-e29b-41d4-a716-446655440000", 1); err == nil {
		t.Fatal("duplicate JSON key was accepted")
	}
	payload := authorizationPayloadForTest(t, controlTestNodeID, "550e8400-e29b-41d4-a716-446655440000", 1, 1)
	var object map[string]any
	if err := json.Unmarshal(payload, &object); err != nil {
		t.Fatal(err)
	}
	delete(object["restrictions"].(map[string]any), "new_rules")
	payload, err := json.Marshal(object)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := validateAuthorizationUpdate(payload, controlTestNodeID, "550e8400-e29b-41d4-a716-446655440000", 1); err == nil {
		t.Fatal("missing required false restriction was treated as valid")
	}
}

func TestAuthorizationRecordsRejectSchemaViolations(t *testing.T) {
	for _, test := range []struct {
		name string
		edit func(map[string]any)
	}{
		{"missing not_before", func(a map[string]any) { delete(a, "not_before") }},
		{"null not_before", func(a map[string]any) { a["not_before"] = nil }},
		{"null optional amount", func(a map[string]any) { a["amount_bytes"] = nil }},
		{"invalid optional rate", func(a map[string]any) { a["rate_bps"] = 0 }},
		{"invalid optional connections", func(a map[string]any) { a["connections"] = 1_000_001 }},
		{"invalid optional snapshot", func(a map[string]any) { a["kind"] = "runtime_lease"; a["billing_snapshot_id"] = "" }},
	} {
		t.Run(test.name, func(t *testing.T) {
			boot := "550e8400-e29b-41d4-a716-446655440000"
			var object map[string]any
			if err := json.Unmarshal(authorizationPayloadForTest(t, controlTestNodeID, boot, 1, 4), &object); err != nil {
				t.Fatal(err)
			}
			test.edit(object["authorizations"].([]any)[0].(map[string]any))
			payload, err := json.Marshal(object)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := validateAuthorizationUpdate(payload, controlTestNodeID, boot, 4); err == nil {
				t.Fatal("invalid record accepted")
			}
		})
	}
}

func TestStoredAuthorizationTimeRevocationBootAndEpochBoundaries(t *testing.T) {
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
	client := &ControlClient{Node: node}
	payload := authorizationPayloadForTest(t, node.NodeID, node.BootID, 1, 4)
	if err := client.applyAuthorizationUpdate(ctx, &controlv1.AuthorizationUpdate{Generation: 4, Payload: payload}); err != nil {
		t.Fatal(err)
	}
	at := time.Date(2026, 11, 5, 12, 1, 30, 0, time.UTC)
	use := store.AuthorizationUse{NodeID: node.NodeID, BootID: node.BootID, AuthorizationID: "550e8400-e29b-41d4-a716-446655440021", Kind: "credit_grant", RuleID: "rule-1", RuleRevision: 2, PathGeneration: 4, BillingSnapshotID: "550e8400-e29b-41d4-a716-446655440024", Earliest: at, Latest: at.Add(time.Second), TrustedTime: true}
	if record, err := s.CheckStoredAuthorization(ctx, use); err != nil || record.AmountBytes != 4096 {
		t.Fatalf("record=%+v err=%v", record, err)
	}
	for _, test := range []struct {
		name string
		edit func(*store.AuthorizationUse)
	}{
		{"untrusted time", func(u *store.AuthorizationUse) { u.TrustedTime = false }},
		{"crosses not-before", func(u *store.AuthorizationUse) { u.Earliest = at.Add(-time.Minute) }},
		{"crosses expiry", func(u *store.AuthorizationUse) { u.Latest = at.Add(90 * time.Second) }},
		{"wrong boot", func(u *store.AuthorizationUse) { u.BootID = "different-boot" }},
		{"wrong snapshot", func(u *store.AuthorizationUse) { u.BillingSnapshotID = "different-snapshot" }},
		{"wrong rule revision", func(u *store.AuthorizationUse) { u.RuleRevision++ }},
	} {
		t.Run(test.name, func(t *testing.T) {
			changed := use
			test.edit(&changed)
			if _, err := s.CheckStoredAuthorization(ctx, changed); err == nil {
				t.Fatal("invalid use accepted")
			}
		})
	}
	// A signer handoff with the same path preserves existing authorization.
	var object map[string]any
	if err := json.Unmarshal(payload, &object); err != nil {
		t.Fatal(err)
	}
	object["update_id"] = "550e8400-e29b-41d4-a716-446655440030"
	object["signer_epoch"] = 2
	object["authorizations"] = []any{}
	apply := func(generation uint64) {
		t.Helper()
		object["path_generation"] = generation
		data, err := json.Marshal(object)
		if err != nil {
			t.Fatal(err)
		}
		if err := client.applyAuthorizationUpdate(ctx, &controlv1.AuthorizationUpdate{Generation: generation, Payload: data}); err != nil {
			t.Fatal(err)
		}
	}
	apply(4)
	if _, err := s.CheckStoredAuthorization(ctx, use); err != nil {
		t.Fatalf("signer handoff invalidated outstanding grant: %v", err)
	}
	object["update_id"] = "550e8400-e29b-41d4-a716-446655440031"
	apply(5)
	if _, err := s.CheckStoredAuthorization(ctx, use); !errors.Is(err, store.ErrAuthorizationDenied) {
		t.Fatalf("old path accepted new work: %v", err)
	}
	use.ExistingSession = true
	if _, err := s.CheckStoredAuthorization(ctx, use); err != nil {
		t.Fatalf("old path could not drain: %v", err)
	}
	object["update_id"] = "550e8400-e29b-41d4-a716-446655440032"
	object["revocations"] = []string{use.AuthorizationID}
	apply(5)
	if _, err := s.CheckStoredAuthorization(ctx, use); !errors.Is(err, store.ErrAuthorizationDenied) {
		t.Fatalf("revoked authorization permitted drain: %v", err)
	}
}

func TestRestartDoesNotReviveStoredAuthorization(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "agent.db")
	s, err := store.Open(path, "agent")
	if err != nil {
		t.Fatal(err)
	}
	node, err := New(controlTestNodeID, s, nil)
	if err != nil {
		t.Fatal(err)
	}
	payload := authorizationPayloadForTest(t, node.NodeID, node.BootID, 1, 4)
	if err := (&ControlClient{Node: node}).applyAuthorizationUpdate(ctx, &controlv1.AuthorizationUpdate{Generation: 4, Payload: payload}); err != nil {
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
	at := time.Date(2026, 11, 5, 12, 1, 30, 0, time.UTC)
	// Even a caller supplying the old boot cannot revive settlement-only rows.
	_, err = s.CheckStoredAuthorization(ctx, store.AuthorizationUse{
		NodeID: node.NodeID, BootID: node.BootID, AuthorizationID: "550e8400-e29b-41d4-a716-446655440021",
		Kind: "credit_grant", RuleID: "rule-1", RuleRevision: 2, PathGeneration: 4,
		BillingSnapshotID: "550e8400-e29b-41d4-a716-446655440024", Earliest: at, Latest: at,
		TrustedTime: true, ExistingSession: true,
	})
	if !errors.Is(err, store.ErrAuthorizationDenied) {
		t.Fatalf("restart revived old boot authorization: %v", err)
	}
}
