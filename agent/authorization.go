package agent

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"time"
	"unicode/utf8"

	"encoding/hex"
	"vortex/agent/store"
	"vortex/contracts"
)

const authorizationSchemaVersion = "1.1.0"

type authorizationPayload struct {
	ShareReferences        []store.ShareReference    `json:"share_references,omitempty"`
	PolicyTransitions      []store.PolicyTransition  `json:"policy_transitions,omitempty"`
	RenewalRequestID       string                    `json:"renewal_request_id,omitempty"`
	RenewalRequestSHA256   string                    `json:"renewal_request_sha256,omitempty"`
	Handoffs               []store.EpochHandoff      `json:"epoch_handoffs,omitempty"`
	SchemaVersion          string                    `json:"schema_version"`
	UpdateID               string                    `json:"update_id"`
	NodeID                 string                    `json:"node_id"`
	BootID                 string                    `json:"boot_id"`
	SignerEpoch            uint64                    `json:"signer_epoch"`
	PathGeneration         uint64                    `json:"path_generation"`
	FencingGenerationFloor uint64                    `json:"fencing_generation_floor"`
	IssuedAt               time.Time                 `json:"issued_at"`
	BackendHeartbeatAt     time.Time                 `json:"backend_heartbeat_at"`
	AutonomyDeadline       time.Time                 `json:"autonomy_deadline"`
	Authorizations         []authorizationRecord     `json:"authorizations"`
	Revocations            []string                  `json:"revocations"`
	Restrictions           authorizationRestrictions `json:"restrictions"`
}

type authorizationRecord struct {
	ShareReferenceID      string    `json:"share_reference_id,omitempty"`
	DelegationID          string    `json:"delegation_id,omitempty"`
	AuthorizationID       string    `json:"authorization_id"`
	ParentAuthorizationID string    `json:"parent_authorization_id"`
	SourceID              string    `json:"source_id"`
	Kind                  string    `json:"kind"`
	RuleID                string    `json:"rule_id"`
	RuleRevision          uint64    `json:"rule_revision"`
	PathGeneration        uint64    `json:"path_generation"`
	IssuedAt              time.Time `json:"issued_at"`
	NotBefore             time.Time `json:"not_before"`
	ExpiresAt             time.Time `json:"expires_at"`
	HardDeadline          time.Time `json:"hard_deadline"`
	BillingSnapshotID     string    `json:"billing_snapshot_id,omitempty"`
	AmountBytes           *uint64   `json:"amount_bytes,omitempty"`
	RateBPS               *uint64   `json:"rate_bps,omitempty"`
	Connections           *uint64   `json:"connections,omitempty"`
}

type authorizationRestrictions struct {
	ExistingRulesOnly             *bool `json:"existing_rules_only"`
	ExistingSharesOnly            *bool `json:"existing_shares_only"`
	ExistingDelegatedBalancesOnly *bool `json:"existing_delegated_balances_only"`
	NewRules                      *bool `json:"new_rules"`
	NewPorts                      *bool `json:"new_ports"`
	DNSChanges                    *bool `json:"dns_changes"`
	Registrations                 *bool `json:"registrations"`
	CrossNodeRebalance            *bool `json:"cross_node_rebalance"`
}

type authorizationRequired struct {
	ShareReferences        *json.RawMessage `json:"share_references"`
	PolicyTransitions      *json.RawMessage `json:"policy_transitions"`
	RenewalRequestID       *string          `json:"renewal_request_id"`
	RenewalRequestSHA256   *string          `json:"renewal_request_sha256"`
	Handoffs               *json.RawMessage `json:"epoch_handoffs"`
	SchemaVersion          *string          `json:"schema_version"`
	UpdateID               *string          `json:"update_id"`
	NodeID                 *string          `json:"node_id"`
	BootID                 *string          `json:"boot_id"`
	SignerEpoch            *uint64          `json:"signer_epoch"`
	PathGeneration         *uint64          `json:"path_generation"`
	FencingGenerationFloor *uint64          `json:"fencing_generation_floor"`
	IssuedAt               *string          `json:"issued_at"`
	BackendHeartbeatAt     *string          `json:"backend_heartbeat_at"`
	AutonomyDeadline       *string          `json:"autonomy_deadline"`
	Authorizations         *json.RawMessage `json:"authorizations"`
	Revocations            *json.RawMessage `json:"revocations"`
	Restrictions           *json.RawMessage `json:"restrictions"`
}

func validateAuthorizationUpdate(payload []byte, nodeID, bootID string, generation uint64) (authorizationPayload, error) {
	var result authorizationPayload
	if len(payload) == 0 || len(payload) > 1<<20 || !utf8.Valid(payload) {
		return result, fmt.Errorf("invalid_authorization: payload must be 1..1048576 bytes")
	}
	if len(contracts.AuthorizationPayloadSchema) == 0 {
		return result, fmt.Errorf("invalid_authorization: frozen schema is unavailable")
	}
	if err := rejectDuplicateJSONKeys(payload); err != nil {
		return result, fmt.Errorf("invalid_authorization: %w", err)
	}
	var required authorizationRequired
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&required); err != nil {
		return result, fmt.Errorf("invalid_authorization: %w", err)
	}
	if required.SchemaVersion == nil || required.UpdateID == nil || required.NodeID == nil || required.BootID == nil || required.SignerEpoch == nil || required.PathGeneration == nil || required.FencingGenerationFloor == nil || required.IssuedAt == nil || required.BackendHeartbeatAt == nil || required.AutonomyDeadline == nil || required.Authorizations == nil || required.Revocations == nil || required.Restrictions == nil {
		return result, fmt.Errorf("invalid_authorization: required field is missing")
	}
	decoder = json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&result); err != nil {
		return result, fmt.Errorf("invalid_authorization: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return result, fmt.Errorf("invalid_authorization: multiple JSON values")
	}
	if (result.SchemaVersion != authorizationSchemaVersion && result.SchemaVersion != "1.0.0" && result.SchemaVersion != "1.2.0") || result.NodeID != nodeID || result.BootID != bootID || result.PathGeneration != generation || generation == 0 {
		return result, fmt.Errorf("identity_mismatch: authorization schema, node, boot or path generation mismatch")
	}
	if !canonicalUUID(result.UpdateID) || !canonicalUUID(result.NodeID) || !canonicalUUID(result.BootID) || result.SignerEpoch == 0 || result.FencingGenerationFloor == 0 || result.Authorizations == nil || result.Revocations == nil || len(result.Authorizations) > 4096 || len(result.Revocations) > 4096 {
		return result, fmt.Errorf("invalid_authorization: identity, epoch or collection bound is invalid")
	}
	var envelopeFields map[string]json.RawMessage
	if err := json.Unmarshal(payload, &envelopeFields); err != nil {
		return result, err
	}
	if result.SchemaVersion == "1.2.0" {
		if required.ShareReferences == nil || required.PolicyTransitions == nil || result.ShareReferences == nil || result.PolicyTransitions == nil || len(result.ShareReferences) > 4096 || len(result.PolicyTransitions) > 64 || result.SignerEpoch > math.MaxInt64 || result.PathGeneration > math.MaxInt64 || result.FencingGenerationFloor > math.MaxInt64 {
			return result, fmt.Errorf("invalid_authorization: missing lifecycle collections or unsafe counters")
		}
		var policyFields []map[string]json.RawMessage
		if err := json.Unmarshal(*required.PolicyTransitions, &policyFields); err != nil {
			return result, err
		}
		for _, fields := range policyFields {
			for _, key := range []string{"transition_id", "rule_id", "path_generation", "old_snapshot", "new_snapshot", "effective_at", "preissue_seconds", "guard_seconds", "future_source_id", "future_credit_authorization_id", "future_runtime_authorization_id", "future_share_authorization_id"} {
				if v, ok := fields[key]; !ok || bytes.Equal(bytes.TrimSpace(v), []byte("null")) {
					return result, fmt.Errorf("invalid_authorization: missing policy field")
				}
			}
			for _, key := range []string{"old_snapshot", "new_snapshot"} {
				var binding map[string]json.RawMessage
				if err := json.Unmarshal(fields[key], &binding); err != nil {
					return result, err
				}
				for _, name := range []string{"snapshot_id", "rule_id", "execution_node_id", "path_generation", "policy_revision", "coefficient_milli"} {
					if v, ok := binding[name]; !ok || bytes.Equal(bytes.TrimSpace(v), []byte("null")) {
						return result, fmt.Errorf("invalid_authorization: missing snapshot field")
					}
				}
			}
		}
		seenPolicies := map[string]bool{}
		for _, p := range result.PolicyTransitions {
			if err := store.ValidatePolicyTransition(p); err != nil || seenPolicies[p.ID] {
				return result, fmt.Errorf("invalid_authorization: invalid or duplicate policy transition")
			}
			seenPolicies[p.ID] = true
		}
		var refs []map[string]json.RawMessage
		if err := json.Unmarshal(*required.ShareReferences, &refs); err != nil {
			return result, err
		}
		seen := map[string]bool{}
		for i, r := range result.ShareReferences {
			for _, key := range []string{"share_id", "parent_share_id", "parent_revision", "source_id", "allowed_issuer_hub_id", "subject_node_id", "signer_epoch", "share_generation", "revision", "rule_id", "rule_revision", "path_generation", "rate_bps", "connections", "not_before", "expires_at", "hard_deadline"} {
				v, ok := refs[i][key]
				if !ok || bytes.Equal(bytes.TrimSpace(v), []byte("null")) {
					return result, fmt.Errorf("invalid_authorization: missing share reference field")
				}
			}
			if err := store.ValidateShareReference(r); err != nil || seen[r.ID] {
				return result, fmt.Errorf("invalid_authorization: invalid or duplicate share reference")
			}
			seen[r.ID] = true
		}
		_, hasID := envelopeFields["renewal_request_id"]
		_, hasDigest := envelopeFields["renewal_request_sha256"]
		if hasID || hasDigest {
			hash, err := hex.DecodeString(result.RenewalRequestSHA256)
			if !hasID || !hasDigest || required.RenewalRequestID == nil || required.RenewalRequestSHA256 == nil || !canonicalUUID(result.RenewalRequestID) || err != nil || len(hash) != 32 || hex.EncodeToString(hash) != result.RenewalRequestSHA256 {
				return result, fmt.Errorf("invalid_authorization: incomplete renewal correlation")
			}
		}
	} else {
		for _, key := range []string{"share_references", "policy_transitions", "renewal_request_id", "renewal_request_sha256"} {
			if _, ok := envelopeFields[key]; ok {
				return result, fmt.Errorf("invalid_authorization: lifecycle field on legacy schema")
			}
		}
	}
	if raw, present := envelopeFields["epoch_handoffs"]; present && ((result.SchemaVersion != "1.1.0" && result.SchemaVersion != "1.2.0") || bytes.Equal(bytes.TrimSpace(raw), []byte("null"))) {
		return result, fmt.Errorf("invalid_authorization: unsupported or null handoffs")
	}
	if required.Handoffs != nil {
		if (result.SchemaVersion != "1.1.0" && result.SchemaVersion != "1.2.0") || result.Handoffs == nil || len(result.Handoffs) > 4096 {
			return result, fmt.Errorf("invalid_authorization: unsupported handoffs")
		}
		var raw []map[string]json.RawMessage
		if err := json.Unmarshal(*required.Handoffs, &raw); err != nil {
			return result, err
		}
		seen := map[string]bool{}
		for i, h := range result.Handoffs {
			for _, key := range []string{"transfer_id", "parent_authorization_id", "source_id", "from_epoch", "to_epoch", "remaining_bytes", "sealed_sequence", "record_sha256"} {
				v, ok := raw[i][key]
				if !ok || bytes.Equal(bytes.TrimSpace(v), []byte("null")) {
					return result, fmt.Errorf("invalid_authorization: handoff field missing")
				}
			}
			hash, e := hex.DecodeString(h.RecordSHA256)
			if !canonicalUUID(h.TransferID) || !canonicalUUID(h.ParentID) || !canonicalUUID(h.SourceID) || h.From == 0 || h.From == math.MaxUint64 || h.To != h.From+1 || h.To != result.SignerEpoch || h.Remaining > math.MaxInt64 || h.SealedSequence < 0 || e != nil || len(hash) != 32 || hex.EncodeToString(hash) != h.RecordSHA256 || seen[h.SourceID] {
				return result, fmt.Errorf("invalid_authorization: invalid handoff")
			}
			seen[h.SourceID] = true
		}
	}
	r := result.Restrictions
	if r.ExistingRulesOnly == nil || r.ExistingSharesOnly == nil || r.ExistingDelegatedBalancesOnly == nil || r.NewRules == nil || r.NewPorts == nil || r.DNSChanges == nil || r.Registrations == nil || r.CrossNodeRebalance == nil || !*r.ExistingRulesOnly || !*r.ExistingSharesOnly || !*r.ExistingDelegatedBalancesOnly || *r.NewRules || *r.NewPorts || *r.DNSChanges || *r.Registrations || *r.CrossNodeRebalance {
		return result, fmt.Errorf("invalid_authorization: restricted-autonomy flags violate the frozen contract")
	}
	if result.BackendHeartbeatAt.IsZero() || result.AutonomyDeadline.IsZero() || result.IssuedAt.IsZero() || !result.AutonomyDeadline.After(result.BackendHeartbeatAt) || result.AutonomyDeadline.After(result.BackendHeartbeatAt.Add(12*time.Hour)) || result.IssuedAt.Before(result.BackendHeartbeatAt) || result.IssuedAt.After(result.AutonomyDeadline) {
		return result, fmt.Errorf("invalid_authorization: heartbeat or autonomy time bounds are invalid")
	}
	ids := make(map[string]struct{}, len(result.Authorizations))
	var rawAuthorizations []map[string]json.RawMessage
	if err := json.Unmarshal(*required.Authorizations, &rawAuthorizations); err != nil {
		return result, fmt.Errorf("invalid_authorization: invalid authorization records")
	}
	for i, a := range result.Authorizations {
		if result.SchemaVersion == "1.2.0" {
			if !canonicalUUID(a.ShareReferenceID) || !canonicalUUID(a.DelegationID) || a.RuleRevision > math.MaxInt64 {
				return result, fmt.Errorf("invalid_authorization: missing share/delegation binding")
			}
		} else {
			for _, key := range []string{"share_reference_id", "delegation_id"} {
				if _, ok := rawAuthorizations[i][key]; ok {
					return result, fmt.Errorf("invalid_authorization: share binding on legacy schema")
				}
			}
		}
		for _, field := range []string{"authorization_id", "parent_authorization_id", "source_id", "kind", "rule_id", "rule_revision", "path_generation", "issued_at", "not_before", "expires_at", "hard_deadline"} {
			value, present := rawAuthorizations[i][field]
			if !present || bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
				return result, fmt.Errorf("invalid_authorization: authorization[%d] requires %s", i, field)
			}
		}
		for _, field := range []string{"billing_snapshot_id", "amount_bytes", "rate_bps", "connections"} {
			if value, present := rawAuthorizations[i][field]; present && bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
				return result, fmt.Errorf("invalid_authorization: authorization[%d] has null %s", i, field)
			}
		}
		if _, present := rawAuthorizations[i]["billing_snapshot_id"]; present && !canonicalUUID(a.BillingSnapshotID) ||
			a.AmountBytes != nil && (*a.AmountBytes == 0 || *a.AmountBytes > math.MaxInt64) ||
			a.RateBPS != nil && (*a.RateBPS == 0 || *a.RateBPS > 1_000_000_000_000) ||
			a.Connections != nil && (*a.Connections == 0 || *a.Connections > 1_000_000) {
			return result, fmt.Errorf("invalid_authorization: authorization[%d] optional limit or identity is invalid", i)
		}
		if !canonicalUUID(a.AuthorizationID) || !canonicalUUID(a.ParentAuthorizationID) || !canonicalUUID(a.SourceID) || a.RuleID == "" || len([]byte(a.RuleID)) > 128 || a.RuleRevision == 0 || a.PathGeneration != result.PathGeneration || a.IssuedAt.IsZero() || a.IssuedAt.Before(result.IssuedAt) || a.NotBefore.Before(a.IssuedAt) || !a.ExpiresAt.After(a.NotBefore) || a.ExpiresAt.After(a.HardDeadline) || a.HardDeadline.After(result.AutonomyDeadline) || a.ExpiresAt.Sub(a.IssuedAt) > 300*time.Second {
			return result, fmt.Errorf("invalid_authorization: authorization[%d] identity, scope or time bound is invalid", i)
		}
		if _, exists := ids[a.AuthorizationID]; exists {
			return result, fmt.Errorf("invalid_authorization: duplicate authorization_id")
		}
		ids[a.AuthorizationID] = struct{}{}
		switch a.Kind {
		case "runtime_lease":
		case "share_lease":
			if a.RateBPS == nil || *a.RateBPS == 0 || *a.RateBPS > 1_000_000_000_000 || a.Connections == nil || *a.Connections == 0 || *a.Connections > 1_000_000 {
				return result, fmt.Errorf("invalid_authorization: authorization[%d] share limits are invalid", i)
			}
		case "credit_grant":
			if !canonicalUUID(a.BillingSnapshotID) || a.AmountBytes == nil || *a.AmountBytes == 0 || *a.AmountBytes > math.MaxInt64 {
				return result, fmt.Errorf("invalid_authorization: authorization[%d] grant limits are invalid", i)
			}
		default:
			return result, fmt.Errorf("invalid_authorization: authorization[%d] kind is unsupported", i)
		}
	}
	seenRevocations := make(map[string]struct{}, len(result.Revocations))
	for _, id := range result.Revocations {
		if !canonicalUUID(id) {
			return result, fmt.Errorf("invalid_authorization: revocation id is invalid")
		}
		if _, exists := seenRevocations[id]; exists {
			return result, fmt.Errorf("invalid_authorization: duplicate revocation id")
		}
		seenRevocations[id] = struct{}{}
	}
	return result, nil
}

func rejectDuplicateJSONKeys(payload []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.UseNumber()
	if err := consumeJSONValue(decoder); err != nil {
		return err
	}
	if _, err := decoder.Token(); err != io.EOF {
		return fmt.Errorf("multiple JSON values")
	}
	return nil
}

func consumeJSONValue(decoder *json.Decoder) error {
	token, err := decoder.Token()
	if err != nil {
		return err
	}
	delimiter, ok := token.(json.Delim)
	if !ok {
		return nil
	}
	switch delimiter {
	case '{':
		seen := make(map[string]struct{})
		for decoder.More() {
			keyToken, err := decoder.Token()
			if err != nil {
				return err
			}
			key, ok := keyToken.(string)
			if !ok {
				return fmt.Errorf("object key is not a string")
			}
			if _, exists := seen[key]; exists {
				return fmt.Errorf("duplicate object key %q", key)
			}
			seen[key] = struct{}{}
			if err := consumeJSONValue(decoder); err != nil {
				return err
			}
		}
		_, err = decoder.Token()
		return err
	case '[':
		for decoder.More() {
			if err := consumeJSONValue(decoder); err != nil {
				return err
			}
		}
		_, err = decoder.Token()
		return err
	default:
		return fmt.Errorf("unexpected JSON delimiter %q", delimiter)
	}
}
