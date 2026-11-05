# 05-03 Authorization Payload Contract Proposal

Status: **Owner decision required. No authorization payload contract is frozen.**

Prepared: 2026-11-05

## Decision Requested

Approve, request edits to, or defer a new independently versioned `vortex.authorization.v1` 1.0.0 JSON payload contract. If approved, `AuthorizationUpdate.payload` in `vortex.control.v1` 1.1.0 will carry this JSON document, and `AuthorizationUpdate.generation` must equal the payload's `path_generation`. `signer_epoch` remains a distinct authority epoch and is never substituted for Engine path generation.

No normative contract catalog, payload schema, protobuf, generated code, requirement mapping or changelog is changed until the exact proposal is approved.

## Existing Contract Boundary

`vortex.control.v1` 1.1.0 already defines `AuthorizationUpdate { generation, payload }`, but the frozen artifacts do not define the payload encoding or how its generation relates to signer authority. Reusing the opaque bytes for grants and leases without freezing those semantics would leave replay, renewal and deadline checks ambiguous. The existing `UsageBatch`/`UsageAcknowledgement` messages remain unchanged and carry durable usage handoff.

## Proposed Contract

- Contract ID: `vortex.authorization.v1`.
- Proposed version: `1.0.0`.
- Encoding: UTF-8 JSON, one object, validated against a separately frozen JSON Schema.
- Idempotency: `update_id` identifies exact canonical payload bytes; reuse with different bytes is a stable conflict.
- Identity: `node_id` and `boot_id` bind the update to the authenticated node process. Grants and leases from an earlier boot are settlement-only and cannot authorize new Engine work.
- Epoch separation: `signer_epoch` is the monotonic authority fencing epoch. `path_generation` and `fencing_generation_floor` apply only to Engine configuration; the control message `generation` equals `path_generation`.
- Delegation: each authorization must name an existing `parent_authorization_id` and `source_id`. The payload cannot create a rule, node, port, DNS entry, or cross-node share.
- Time: each authorization includes `issued_at`, `not_before`, `expires_at` and `hard_deadline`; runtime TTL is at most 300 seconds. `autonomy_deadline` is fixed to the last successful backend heartbeat plus at most 12 hours and is not recomputed from Hub reconnect or process restart. A runtime verifier must reject a deadline beyond that bound.
- Limits: at most 4096 authorization records and 4096 revocations per update; identifiers are UUIDs, rule IDs are 1..128 UTF-8 bytes, byte amounts are positive signed-64-bit integers, rate and connection limits are positive and bounded by the existing local share contract.
- Offline behavior: only renewal of the listed existing authorizations is permitted. New authorization, registration, port/DNS changes and cross-node rebalance are forbidden. Uncertain time or storage failure rejects new work and preserves outstanding upper bounds.

The exact proposed JSON Schema is:

```json
{
  "$schema": "https://json-schema.org/draft/2020-12/schema",
  "$id": "https://vortex.invalid/contracts/authorization/v1/payload.schema.json",
  "title": "VORTEX Authorization Update 1.0.0",
  "type": "object",
  "additionalProperties": false,
  "required": ["schema_version", "update_id", "node_id", "boot_id", "signer_epoch", "path_generation", "fencing_generation_floor", "issued_at", "backend_heartbeat_at", "autonomy_deadline", "authorizations", "revocations", "restrictions"],
  "properties": {
    "schema_version": {"const": "1.0.0"},
    "update_id": {"type": "string", "format": "uuid"},
    "node_id": {"type": "string", "format": "uuid"},
    "boot_id": {"type": "string", "format": "uuid"},
    "signer_epoch": {"type": "integer", "minimum": 1},
    "path_generation": {"type": "integer", "minimum": 1},
    "fencing_generation_floor": {"type": "integer", "minimum": 1},
    "issued_at": {"type": "string", "format": "date-time"},
    "backend_heartbeat_at": {"type": "string", "format": "date-time"},
    "autonomy_deadline": {"type": "string", "format": "date-time"},
    "authorizations": {
      "type": "array", "maxItems": 4096,
      "items": {
        "type": "object", "additionalProperties": false,
        "required": ["authorization_id", "parent_authorization_id", "source_id", "kind", "rule_id", "rule_revision", "path_generation", "issued_at", "not_before", "expires_at", "hard_deadline"],
        "properties": {
          "authorization_id": {"type": "string", "format": "uuid"},
          "parent_authorization_id": {"type": "string", "format": "uuid"},
          "source_id": {"type": "string", "format": "uuid"},
          "kind": {"enum": ["runtime_lease", "share_lease", "credit_grant"]},
          "rule_id": {"type": "string", "minLength": 1, "maxLength": 128},
          "rule_revision": {"type": "integer", "minimum": 1},
          "path_generation": {"type": "integer", "minimum": 1},
          "issued_at": {"type": "string", "format": "date-time"},
          "not_before": {"type": "string", "format": "date-time"},
          "expires_at": {"type": "string", "format": "date-time"},
          "hard_deadline": {"type": "string", "format": "date-time"},
          "billing_snapshot_id": {"type": "string", "format": "uuid"},
          "amount_bytes": {"type": "integer", "minimum": 1, "maximum": 9223372036854775807},
          "rate_bps": {"type": "integer", "minimum": 1, "maximum": 1000000000000},
          "connections": {"type": "integer", "minimum": 1, "maximum": 1000000}
        },
        "allOf": [
          {"if": {"properties": {"kind": {"const": "credit_grant"}}, "required": ["kind"]}, "then": {"required": ["billing_snapshot_id", "amount_bytes"]}},
          {"if": {"properties": {"kind": {"const": "share_lease"}}, "required": ["kind"]}, "then": {"required": ["rate_bps", "connections"]}}
        ]
      }
    },
    "revocations": {"type": "array", "maxItems": 4096, "uniqueItems": true, "items": {"type": "string", "format": "uuid"}},
    "restrictions": {
      "type": "object", "additionalProperties": false,
      "required": ["existing_rules_only", "existing_shares_only", "existing_delegated_balances_only", "new_rules", "new_ports", "dns_changes", "registrations", "cross_node_rebalance"],
      "properties": {
        "existing_rules_only": {"const": true},
        "existing_shares_only": {"const": true},
        "existing_delegated_balances_only": {"const": true},
        "new_rules": {"const": false},
        "new_ports": {"const": false},
        "dns_changes": {"const": false},
        "registrations": {"const": false},
        "cross_node_rebalance": {"const": false}
      }
    }
  }
}
```

## Compatibility and Affected Requirements

This adds a new payload contract without changing protobuf field numbers or the frozen `vortex.control.v1` 1.1.0 bytes. A node must reject an authorization update unless it supports `vortex.authorization.v1` 1.0.0; no fallback interpretation of opaque payload bytes is allowed.

The candidate maps to AGT-005, AGT-006, AGT-009, AGT-010, AGT-011, AGT-012 and HUB-001..018. Existing control fields also support AGT-002 and AGT-004, but this proposal does not revise their map entries.

## Unresolved Owner Choices

1. Approve the exact schema above as the independently versioned `vortex.authorization.v1` 1.0.0 contract, or request edits/defer authorization payload delivery.
2. Confirm that `AuthorizationUpdate.generation` equals `path_generation`, while `signer_epoch` remains separate.
3. Confirm that mTLS-authenticated Hub delivery is the trust boundary for this protocol-test slice; production issuer authority remains Phase 6.

## Approval Record

| Decision | Owner | Timestamp (UTC) | Proposal SHA-256 | Selected version | Notes |
| --- | --- | --- | --- | --- | --- |
| Pending | | | | | |
