package agent

import (
	"context"
	"encoding/json"
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
