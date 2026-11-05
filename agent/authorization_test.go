package agent

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"google.golang.org/protobuf/proto"
	"vortex/agent/store"
	controlv1 "vortex/contracts/protobuf/v1"
	engineaccounting "vortex/engine/accounting"
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

func TestAuthorizationGateAtomicallyConsumesAndReconcilesQuota(t *testing.T) {
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
	payload := authorizationPayloadForTest(t, node.NodeID, node.BootID, 1, 4)
	if err := (&ControlClient{Node: node}).applyAuthorizationUpdate(ctx, &controlv1.AuthorizationUpdate{Generation: 4, Payload: payload}); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 11, 5, 12, 1, 30, 0, time.UTC)
	gate, err := node.NewAuthorizationGate(AuthorizationGateConfig{
		AuthorizationID: "550e8400-e29b-41d4-a716-446655440021", RuleID: "rule-1", RuleRevision: 2,
		PathGeneration: 4, SnapshotID: "550e8400-e29b-41d4-a716-446655440024",
		Clock: func() (time.Time, time.Time, bool) { return now, now.Add(time.Second), true },
	})
	if err != nil {
		t.Fatal(err)
	}
	session, err := gate.Open(ctx, engineaccounting.Flow{Key: "opaque-flow", RuleID: "rule-1"})
	if err != nil {
		t.Fatal(err)
	}
	batch, err := session.Reserve(ctx, engineaccounting.LeftToRight, 3000)
	if err != nil {
		t.Fatal(err)
	}
	if remaining, err := s.RemainingAuthorizationBytes(ctx, node.NodeID, "550e8400-e29b-41d4-a716-446655440021"); err != nil || remaining != 1096 {
		t.Fatalf("remaining after pre-write reserve=%d err=%v", remaining, err)
	}
	if err := batch.Commit(ctx, 1200); err != nil {
		t.Fatal(err)
	}
	if balance, err := s.AuditAuthorizationBalance(ctx, node.NodeID, "550e8400-e29b-41d4-a716-446655440021"); err != nil || balance.Remaining != 2896 || balance.Committed != 1200 {
		t.Fatalf("committed source balance=%+v err=%v", balance, err)
	}
	var next map[string]any
	if err := json.Unmarshal(authorizationPayloadForTest(t, node.NodeID, node.BootID, 2, 5), &next); err != nil {
		t.Fatal(err)
	}
	next["update_id"] = "550e8400-e29b-41d4-a716-446655440040"
	next["authorizations"] = []any{}
	newPayload, err := json.Marshal(next)
	if err != nil {
		t.Fatal(err)
	}
	if err := (&ControlClient{Node: node}).applyAuthorizationUpdate(ctx, &controlv1.AuthorizationUpdate{Generation: 5, Payload: newPayload}); err != nil {
		t.Fatal(err)
	}
	if _, err := gate.Open(ctx, engineaccounting.Flow{Key: "new-flow", RuleID: "rule-1"}); err == nil {
		t.Fatal("old path generation admitted a new flow")
	}
	uncertain, err := session.Reserve(ctx, engineaccounting.RightToLeft, 2896)
	if err != nil {
		t.Fatal(err)
	}
	if err := uncertain.MarkUnknown(ctx); err != nil {
		t.Fatal(err)
	}
	if balance, err := s.AuditAuthorizationBalance(ctx, node.NodeID, "550e8400-e29b-41d4-a716-446655440021"); err != nil || balance.Remaining != 0 || balance.Committed != 1200 || balance.Unknown != 2896 {
		t.Fatalf("unknown liability balance=%+v err=%v", balance, err)
	}
	if _, err := session.Reserve(ctx, engineaccounting.LeftToRight, 1); err == nil {
		t.Fatal("exhausted grant authorized another write")
	}
	if err := session.Close(); err != nil {
		t.Fatal(err)
	}
	pending, err := s.PendingUsageOutbox(ctx, node.NodeID)
	if err != nil || len(pending) != 1 || pending[0].UnknownUpper != 0 {
		t.Fatalf("durable usage=%+v err=%v", pending, err)
	}
	unknown, err := s.PendingUnknownUsage(ctx, node.NodeID)
	if err != nil || len(unknown) != 1 || unknown[0].Upper != 2896 {
		t.Fatalf("unknown liability=%+v %v", unknown, err)
	}
}

func TestConcurrentAuthorizedReservationsCannotOverdrawGrant(t *testing.T) {
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
	if err := (&ControlClient{Node: node}).applyAuthorizationUpdate(ctx, &controlv1.AuthorizationUpdate{Generation: 4, Payload: authorizationPayloadForTest(t, node.NodeID, node.BootID, 1, 4)}); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 11, 5, 12, 1, 30, 0, time.UTC)
	gate, err := node.NewAuthorizationGate(AuthorizationGateConfig{AuthorizationID: "550e8400-e29b-41d4-a716-446655440021", RuleID: "rule-1", RuleRevision: 2, PathGeneration: 4, SnapshotID: "550e8400-e29b-41d4-a716-446655440024", Clock: func() (time.Time, time.Time, bool) { return now, now, true }})
	if err != nil {
		t.Fatal(err)
	}
	sessions := make([]engineaccounting.Session, 2)
	for i := range sessions {
		sessions[i], err = gate.Open(ctx, engineaccounting.Flow{Key: fmt.Sprintf("parallel-%d", i), RuleID: "rule-1"})
		if err != nil {
			t.Fatal(err)
		}
	}
	start := make(chan struct{})
	errs := make(chan error, len(sessions))
	for i, session := range sessions {
		go func(i int, session engineaccounting.Session) {
			<-start
			_, err := session.Reserve(ctx, engineaccounting.LeftToRight, 3000)
			errs <- err
		}(i, session)
	}
	close(start)
	accepted, denied := 0, 0
	for range sessions {
		if err := <-errs; err == nil {
			accepted++
		} else {
			denied++
		}
	}
	if accepted != 1 || denied != 1 {
		t.Fatalf("accepted=%d denied=%d", accepted, denied)
	}
	balance, err := s.AuditAuthorizationBalance(ctx, node.NodeID, "550e8400-e29b-41d4-a716-446655440021")
	if err != nil || balance.Remaining != 1096 || balance.Reserved != 3000 {
		t.Fatalf("concurrent grant balance=%+v err=%v", balance, err)
	}
}

func TestSignerEpochCannotTransferSourceWithoutHandoffEvidence(t *testing.T) {
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
	oldID := "550e8400-e29b-41d4-a716-446655440021"
	sourceID := "550e8400-e29b-41d4-a716-446655440023"
	payload := authorizationPayloadForTest(t, node.NodeID, node.BootID, 1, 4)
	if err := (&ControlClient{Node: node}).applyAuthorizationUpdate(ctx, &controlv1.AuthorizationUpdate{Generation: 4, Payload: payload}); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 11, 5, 12, 1, 30, 0, time.UTC)
	gate, err := node.NewAuthorizationGate(AuthorizationGateConfig{AuthorizationID: oldID, RuleID: "rule-1", RuleRevision: 2, PathGeneration: 4, SnapshotID: "550e8400-e29b-41d4-a716-446655440024", Clock: func() (time.Time, time.Time, bool) { return now, now.Add(time.Second), true }})
	if err != nil {
		t.Fatal(err)
	}
	oldSession, err := gate.Open(ctx, engineaccounting.Flow{Key: "old-path-session", RuleID: "rule-1"})
	if err != nil {
		t.Fatal(err)
	}
	oldHold, err := oldSession.Reserve(ctx, engineaccounting.LeftToRight, 1000)
	if err != nil {
		t.Fatal(err)
	}
	var next map[string]any
	if err := json.Unmarshal(authorizationPayloadForTest(t, node.NodeID, node.BootID, 2, 4), &next); err != nil {
		t.Fatal(err)
	}
	next["update_id"] = "550e8400-e29b-41d4-a716-446655440041"
	auth := next["authorizations"].([]any)[0].(map[string]any)
	newID := "550e8400-e29b-41d4-a716-446655440042"
	auth["authorization_id"] = newID
	auth["parent_authorization_id"] = oldID
	auth["amount_bytes"] = uint64(3096)
	newPayload, err := json.Marshal(next)
	if err != nil {
		t.Fatal(err)
	}
	if err := (&ControlClient{Node: node}).applyAuthorizationUpdate(ctx, &controlv1.AuthorizationUpdate{Generation: 4, Payload: newPayload}); !errors.Is(err, store.ErrAuthorityHandoffRequired) {
		t.Fatalf("parent ID was accepted as handoff evidence: %v", err)
	}
	if err := oldHold.Commit(ctx, 500); err != nil {
		t.Fatal(err)
	}
	if _, err := s.RemainingAuthorizationBytes(ctx, node.NodeID, newID); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("unproven successor became spendable: %v", err)
	}
	if source, err := s.AuditAuthorizationSource(ctx, node.NodeID, sourceID); err != nil || source.Remaining != 3596 || source.Committed != 500 {
		t.Fatalf("old source changed by rejected transfer=%+v err=%v", source, err)
	}
}

func TestRestartFreezesAuthorizedReservationAndRecoversUnknownBound(t *testing.T) {
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
	now := time.Date(2026, 11, 5, 12, 1, 30, 0, time.UTC)
	gate, err := node.NewAuthorizationGate(AuthorizationGateConfig{
		AuthorizationID: "550e8400-e29b-41d4-a716-446655440021", RuleID: "rule-1", RuleRevision: 2,
		PathGeneration: 4, SnapshotID: "550e8400-e29b-41d4-a716-446655440024",
		Clock: func() (time.Time, time.Time, bool) { return now, now.Add(time.Second), true },
	})
	if err != nil {
		t.Fatal(err)
	}
	session, err := gate.Open(ctx, engineaccounting.Flow{Key: "restart-flow", RuleID: "rule-1"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := session.Reserve(ctx, engineaccounting.LeftToRight, 1000); err != nil {
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
	balance, err := s.AuditAuthorizationBalance(ctx, controlTestNodeID, "550e8400-e29b-41d4-a716-446655440021")
	if err != nil || balance.Remaining != 3096 || balance.Unknown != 1000 {
		t.Fatalf("recovered grant balance=%+v err=%v", balance, err)
	}
	pending, err := s.PendingUnknownUsage(ctx, controlTestNodeID)
	if err != nil || len(pending) != 1 || pending[0].Upper != 1000 {
		t.Fatalf("recovered durable liability=%+v err=%v", pending, err)
	}
	var batch controlv1.UnknownUsageBatch
	if err := proto.Unmarshal(pending[0].Payload, &batch); err != nil {
		t.Fatal(err)
	}
	if batch.GetGrantId() != "550e8400-e29b-41d4-a716-446655440021" || batch.GetOriginBootId() != node.BootID {
		t.Fatalf("recovered grant/origin attribution=%+v", &batch)
	}
}

func TestAuthorizationGateLatchesPersistenceFailureAcrossSessions(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "agent.db")
	s, err := store.Open(path, "agent")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	node, err := New(controlTestNodeID, s, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := (&ControlClient{Node: node}).applyAuthorizationUpdate(ctx, &controlv1.AuthorizationUpdate{Generation: 4, Payload: authorizationPayloadForTest(t, node.NodeID, node.BootID, 1, 4)}); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 11, 5, 12, 1, 30, 0, time.UTC)
	gate, err := node.NewAuthorizationGate(AuthorizationGateConfig{AuthorizationID: "550e8400-e29b-41d4-a716-446655440021", RuleID: "rule-1", RuleRevision: 2, PathGeneration: 4, SnapshotID: "550e8400-e29b-41d4-a716-446655440024", Clock: func() (time.Time, time.Time, bool) { return now, now, true }})
	if err != nil {
		t.Fatal(err)
	}
	first, err := gate.Open(ctx, engineaccounting.Flow{Key: "first", RuleID: "rule-1"})
	if err != nil {
		t.Fatal(err)
	}
	second, err := gate.Open(ctx, engineaccounting.Flow{Key: "second", RuleID: "rule-1"})
	if err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`CREATE TRIGGER fail_authority_hold BEFORE INSERT ON authorization_usage_holds BEGIN SELECT RAISE(ABORT, 'injected write failure'); END`); err != nil {
		t.Fatal(err)
	}
	if _, err := first.Reserve(ctx, engineaccounting.LeftToRight, 100); err == nil {
		t.Fatal("failed persistence returned writable batch")
	}
	if _, err := db.Exec(`DROP TRIGGER fail_authority_hold`); err != nil {
		t.Fatal(err)
	}
	if _, err := second.Reserve(ctx, engineaccounting.LeftToRight, 100); err == nil {
		t.Fatal("other session bypassed latched disk failure")
	}
	if _, err := gate.Open(ctx, engineaccounting.Flow{Key: "third", RuleID: "rule-1"}); err == nil {
		t.Fatal("new session bypassed latched disk failure")
	}
	b, err := s.AuditAuthorizationSource(ctx, node.NodeID, "550e8400-e29b-41d4-a716-446655440023")
	if err != nil || b.Remaining != 4096 || b.Reserved != 0 {
		t.Fatalf("failed transaction leaked debit: %+v %v", b, err)
	}
}

func TestLeasedGateRequiresLiveRuntimeShareAndCreditForEveryReservation(t *testing.T) {
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
	var body map[string]any
	if err := json.Unmarshal(authorizationPayloadForTest(t, node.NodeID, node.BootID, 1, 4), &body); err != nil {
		t.Fatal(err)
	}
	credit := body["authorizations"].([]any)[0].(map[string]any)
	runtimeID, shareID := "550e8400-e29b-41d4-a716-446655440031", "550e8400-e29b-41d4-a716-446655440032"
	leases := []any{credit}
	for i, id := range []string{runtimeID, shareID} {
		lease := map[string]any{}
		for k, v := range credit {
			lease[k] = v
		}
		delete(lease, "billing_snapshot_id")
		delete(lease, "amount_bytes")
		lease["authorization_id"] = id
		lease["kind"] = "runtime_lease"
		lease["expires_at"] = "2026-11-05T12:01:45Z"
		if i == 1 {
			lease["kind"] = "share_lease"
			lease["rate_bps"] = 1000000
			lease["connections"] = 10
		}
		leases = append(leases, lease)
	}
	body["authorizations"] = leases
	payload, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	client := &ControlClient{Node: node}
	if err := client.applyAuthorizationUpdate(ctx, &controlv1.AuthorizationUpdate{Generation: 4, Payload: payload}); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 11, 5, 12, 1, 30, 0, time.UTC)
	config := AuthorizationGateConfig{AuthorizationID: credit["authorization_id"].(string), RuntimeLeaseID: runtimeID, ShareLeaseID: shareID, RuleID: "rule-1", RuleRevision: 2, PathGeneration: 4, SnapshotID: credit["billing_snapshot_id"].(string), Clock: func() (time.Time, time.Time, bool) { return now, now, true }}
	invalid := config
	invalid.ShareLeaseID = ""
	if _, err := node.NewLeasedAuthorizationGate(invalid); err == nil {
		t.Fatal("missing share lease accepted")
	}
	gate, err := node.NewLeasedAuthorizationGate(config)
	if err != nil {
		t.Fatal(err)
	}
	session, err := gate.Open(ctx, engineaccounting.Flow{Key: "leased", RuleID: "rule-1"})
	if err != nil {
		t.Fatal(err)
	}
	batch, err := session.Reserve(ctx, engineaccounting.LeftToRight, 100)
	if err != nil {
		t.Fatal(err)
	}
	if err := batch.Commit(ctx, 100); err != nil {
		t.Fatal(err)
	}
	now = now.Add(15 * time.Second)
	if _, err := session.Reserve(ctx, engineaccounting.LeftToRight, 100); err == nil {
		t.Fatal("expired runtime/share lease authorized an existing session write")
	}
	if _, err := gate.Open(ctx, engineaccounting.Flow{Key: "expired"}); err == nil {
		t.Fatal("expired lease admitted a new session")
	}
	balance, err := s.AuditAuthorizationBalance(ctx, node.NodeID, config.AuthorizationID)
	if err != nil || balance.Committed != 100 || balance.Remaining != 3996 {
		t.Fatalf("lease refusal changed credit: %+v %v", balance, err)
	}
	now = now.Add(-15 * time.Second)
	body["update_id"] = "550e8400-e29b-41d4-a716-446655440033"
	body["authorizations"] = []any{}
	body["revocations"] = []string{runtimeID}
	payload, err = json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	if err := client.applyAuthorizationUpdate(ctx, &controlv1.AuthorizationUpdate{Generation: 4, Payload: payload}); err != nil {
		t.Fatal(err)
	}
	if _, err := session.Reserve(ctx, engineaccounting.LeftToRight, 100); err == nil {
		t.Fatal("revoked runtime lease authorized existing session write")
	}
	if err := session.Close(); err != nil {
		t.Fatal(err)
	}
}
