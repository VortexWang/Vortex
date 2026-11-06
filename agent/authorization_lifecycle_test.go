package agent

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"vortex/agent/store"
	"vortex/contracts"
	controlv1 "vortex/contracts/protobuf/v1"
)

func lifecycleAuthorizationFixture(t *testing.T) map[string]any {
	t.Helper()
	var body map[string]any
	if err := json.Unmarshal(authorizationPayloadForTest(t, controlTestNodeID, "550e8400-e29b-41d4-a716-446655440000", 1, 4), &body); err != nil {
		t.Fatal(err)
	}
	body["schema_version"] = "1.2.0"
	a := body["authorizations"].([]any)[0].(map[string]any)
	a["share_reference_id"] = "550e8400-e29b-41d4-a716-446655440050"
	a["delegation_id"] = a["parent_authorization_id"]
	start := time.Date(2026, 11, 5, 12, 0, 0, 0, time.UTC)
	r := store.ShareReference{ID: a["share_reference_id"].(string), SourceID: a["source_id"].(string), IssuerID: "550e8400-e29b-41d4-a716-446655440051", NodeID: controlTestNodeID, SignerEpoch: 1, Generation: 1, Revision: 1, RuleID: "rule-1", RuleRevision: 2, PathGeneration: 4, RateBPS: 1000000, Connections: 1, NotBefore: start, ExpiresAt: start.Add(time.Hour), HardDeadline: start.Add(time.Hour)}
	raw, _ := json.Marshal(r)
	var ref map[string]any
	if err := json.Unmarshal(raw, &ref); err != nil {
		t.Fatal(err)
	}
	body["share_references"] = []any{ref}
	body["policy_transitions"] = []any{}
	return body
}

func TestAuthorizationLifecycleRenewalNeedsDurableMatchingScope(t *testing.T) {
	for _, scenario := range []string{"valid", "unjournaled", "unreceived", "wrong-issuer", "expanded-rule", "expanded-amount", "expanded-ttl"} {
		t.Run(scenario, func(t *testing.T) {
			ctx := context.Background()
			s, err := store.Open(filepath.Join(t.TempDir(), "agent.db"), "agent")
			if err != nil {
				t.Fatal(err)
			}
			defer s.Close()
			n, err := New(controlTestNodeID, s, nil)
			if err != nil {
				t.Fatal(err)
			}
			body := lifecycleAuthorizationFixture(t)
			body["boot_id"] = n.BootID
			a := body["authorizations"].([]any)[0].(map[string]any)
			issuer := "550e8400-e29b-41d4-a716-446655440051"
			r := &controlv1.RenewalRequest{RequestId: "550e8400-e29b-41d4-a716-446655440052", NodeId: n.NodeID, BootId: n.BootID, DelegationId: a["delegation_id"].(string), SourceId: a["source_id"].(string), RuleId: "rule-1", RuleRevision: 2, PathGeneration: 4, FencingGeneration: 8, SignerEpoch: 1, BillingSnapshotId: a["billing_snapshot_id"].(string), RequestedRawBytes: 4096, RequestedTtlSeconds: 120}
			r.RequestSha256, err = contracts.RenewalDigest(r)
			if err != nil {
				t.Fatal(err)
			}
			body["renewal_request_id"], body["renewal_request_sha256"] = r.RequestId, hex.EncodeToString(r.RequestSha256)
			switch scenario {
			case "expanded-rule":
				a["rule_id"] = "other-rule"
			case "expanded-amount":
				a["amount_bytes"] = 4097
			case "expanded-ttl":
				r.RequestedTtlSeconds = 60
				r.RequestSha256, err = contracts.RenewalDigest(r)
				if err != nil {
					t.Fatal(err)
				}
				body["renewal_request_sha256"] = hex.EncodeToString(r.RequestSha256)
			}
			raw, err := json.Marshal(body)
			if err != nil {
				t.Fatal(err)
			}
			update := &controlv1.AuthorizationUpdate{Generation: 4, Payload: raw}
			if scenario != "unjournaled" {
				if err := s.PersistRenewalIntent(ctx, issuer, r); err != nil {
					t.Fatal(err)
				}
				if scenario != "unreceived" {
					if err := s.PersistRenewalResult(ctx, issuer, &controlv1.RenewalResult{RequestId: r.RequestId, RequestSha256: r.RequestSha256, Code: "ok", AuthorizationUpdate: update}); err != nil {
						t.Fatal(err)
					}
				}
			}
			refs, _ := json.Marshal(body["share_references"])
			var roots []store.ShareReference
			if err := json.Unmarshal(refs, &roots); err != nil {
				t.Fatal(err)
			}
			for _, root := range roots {
				if err := s.InstallShareSourceAnchor(ctx, n.NodeID, "550e8400-e29b-41d4-a716-446655440059", root); err != nil {
					t.Fatal(err)
				}
			}
			if scenario == "wrong-issuer" {
				issuer = "550e8400-e29b-41d4-a716-446655440060"
			}
			c := &ControlClient{Node: n}
			err = c.applyAuthorizationUpdateFrom(ctx, update, issuer)
			if scenario == "valid" && err != nil {
				t.Fatal(err)
			}
			if scenario != "valid" && err == nil {
				t.Fatal("unmatched or expanded renewal installed")
			}
		})
	}
}

func TestAuthorizationLifecycleStrictFieldsAndLegacyIsolation(t *testing.T) {
	for _, name := range []string{"valid", "missing-refs", "null-refs", "missing-policy", "null-policy", "unsupported-policy", "missing-leaf", "null-parent", "unknown-ref-key", "unsafe-revision", "duplicate-ref", "half-correlation", "null-correlation", "bad-hash", "legacy-lifecycle"} {
		t.Run(name, func(t *testing.T) {
			body := lifecycleAuthorizationFixture(t)
			ref := body["share_references"].([]any)[0].(map[string]any)
			switch name {
			case "missing-refs":
				delete(body, "share_references")
			case "null-refs":
				body["share_references"] = nil
			case "missing-policy":
				delete(body, "policy_transitions")
			case "null-policy":
				body["policy_transitions"] = nil
			case "unsupported-policy":
				body["policy_transitions"] = []any{map[string]any{}}
			case "missing-leaf":
				delete(body["authorizations"].([]any)[0].(map[string]any), "share_reference_id")
			case "null-parent":
				ref["parent_share_id"] = nil
			case "unknown-ref-key":
				ref["future_authority"] = true
			case "unsafe-revision":
				ref["revision"] = uint64(1) << 63
			case "duplicate-ref":
				body["share_references"] = []any{ref, ref}
			case "half-correlation":
				body["renewal_request_id"] = "550e8400-e29b-41d4-a716-446655440052"
			case "null-correlation":
				body["renewal_request_id"], body["renewal_request_sha256"] = nil, nil
			case "bad-hash":
				body["renewal_request_id"], body["renewal_request_sha256"] = "550e8400-e29b-41d4-a716-446655440052", strings.Repeat("Z", 64)
			case "legacy-lifecycle":
				body["schema_version"] = "1.0.0"
			}
			raw, err := json.Marshal(body)
			if err != nil {
				t.Fatal(err)
			}
			_, err = validateAuthorizationUpdate(raw, controlTestNodeID, "550e8400-e29b-41d4-a716-446655440000", 4)
			if name == "valid" && err != nil {
				t.Fatal(err)
			}
			if name != "valid" && err == nil {
				t.Fatal("unsafe lifecycle payload accepted")
			}
		})
	}
}
