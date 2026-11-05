# 05-05 Control Contract Change Proposal

Status: **Owner decision required. No normative contract change is approved by this proposal.**

Prepared: 2026-11-05

## Decision Requested

Approve, request edits to, or defer the proposed `vortex.control.v1` 1.1.0 additive extension described below. Approval must identify this document's SHA-256 and the selected SemVer version. Until then, `vortex.proto`, generated code, contract version records, the requirements map, and the contract changelog remain unchanged.

## Baselines and Observed Drift

| Artifact | Approved baseline | Current working tree | Finding |
| --- | --- | --- | --- |
| `contracts/protobuf/v1/vortex.proto` | Version 1.0.0; SHA-256 `4aedc57c1433102d36ab861ea4deee6d063707010f69e70dadf3d433abb4117a` (verified from approved freeze commit `0f870da`) | SHA-256 `484824b260a27afe249604a049a6af5b2b2dce41cdae11b6870f7311c97b052f`; self-labelled DRAFT 1.1.0 | Unapproved additive `RegisterRequest` and `ControlError` messages/oneof arms, plus source-list/comment changes. The self-declared draft version is not approval. |
| `contracts/vlink/v1/encoding.md` | Version 1.0.0; SHA-256 `dbf1607d306720a73053848fff1ca92e1d66716957a0410f83c7c1552a112b52` | SHA-256 `bf118a61789d37854f7c18dc5676d62bac4b845040944812418419cdacb17a9e` | Commit `6310aef` added the OPEN total-payload range 42..310 bytes. This is a normative validation bound and therefore requires owner review, a SemVer/change record and matching approval hash; it is not covered by this control proposal's approval request. |

The approved control source hash was recalculated directly from `0f870da:contracts/protobuf/v1/vortex.proto`; it matches `docs/governance/contract-review.yaml`. The control working hash and current VLink hash were calculated from the checked-out files. No generated control Go code is present in the current contract directory.

`python3 tools/phase1/contracts.py check-frozen` currently fails with three findings: control is not frozen 1.0.0, the control approved hash is stale, and the VLink approved hash is stale. `python3 tools/phase1/contracts.py check-map` passes. The checker also hard-codes every frozen contract to version 1.0.0, conflicting with the already recorded SemVer policy; a future compatible 1.1.0 contract would fail even with an approved hash. Proposed checker correction: compare each frozen version with its approved record and validate content hashes, while relying on `check-change` to enforce SemVer/change records for subsequent edits. This checker correction is governance tooling, not a change to any normative wire contract.

## Proposed Control Extension

The proposal starts from the approved 1.0.0 source, incorporates the existing draft's registration and generic-error additions, and adds a transaction-safe runtime configuration update. The wire-level change is additive: existing field numbers and types remain unchanged; new messages and fields use previously unused numbers. A 1.0.0 peer can parse the protobuf wire format but will not implement the new lifecycle semantics. Runtime config delivery therefore requires the peer to advertise/support control contract 1.1.0; it must never be sent to a 1.0.0 peer.

Proposed schema delta (fields marked `NEW` are relative to the approved 1.0.0 source):

```proto
message NodeMessage {
  string node_id = 1;
  string boot_id = 2;
  uint64 sequence = 3;
  oneof body {
    Heartbeat heartbeat = 10;
    UsageBatch usage_batch = 11;
    ApplyResult apply_result = 12;
    NodeEvent event = 13;
    RegisterRequest register = 14; // NEW, existing unapproved draft addition
  }
}

message ControlMessage {
  string message_id = 1;
  uint64 sequence = 2;
  oneof body {
    RuntimeConfigUpdate config_update = 10;
    UsageAcknowledgement usage_ack = 11;
    AuthorizationUpdate authorization_update = 12;
    ControlError error = 13; // NEW, existing unapproved draft addition
  }
}

message RuntimeConfigUpdate {
  uint64 version = 1;
  bytes payload = 2;
  bytes transaction_id = 3;       // NEW
  uint64 fencing_generation = 4;  // NEW
  bytes payload_sha256 = 5;       // NEW
  uint64 expected_version = 6;    // NEW; zero means no active config is expected
}

message ApplyResult {
  uint64 version = 1;
  string state = 2;
  repeated ObjectResult objects = 3;
  bytes transaction_id = 4;       // NEW
  uint64 fencing_generation = 5;  // NEW
  string error_code = 6;           // NEW; stable machine code, empty on success
}

message RegisterRequest { string enrollment_token = 1; bytes csr_der = 2; string requested_node_id = 3; }
message ControlError { string code = 1; string message_id = 2; bool retryable = 3; }
```

The 1.1.0 version is proposed because this is a compatible additive extension of the approved 1.0.0 wire schema. It does not assert that the current draft was approved. If review determines any existing 1.0.0 field semantics must change, that part is breaking and must be split into a major-version proposal rather than folded into 1.1.0.

## Field Contract

| Field | Wire type / presence | Bound and validation | Idempotency / fencing | Failure response |
| --- | --- | --- | --- | --- |
| `RuntimeConfigUpdate.version` | `uint64`, required semantic value | `1..2^64-1`; must equal the validated payload's `version` and exceed the currently active config version unless this is an exact replay | Persist as part of transaction identity/result; lower/equal version with a new transaction is rejected | `stale_version` |
| `RuntimeConfigUpdate.payload` | `bytes`, required | UTF-8 JSON, 1..1,048,576 bytes; reject invalid UTF-8/JSON, unknown schema fields, unsupported runtime roles, wrong node ID, or schema violation before durable Prepare | Bytes must hash to `payload_sha256`; exact bytes are replay identity | `invalid_config` or `unsupported_config` |
| `RuntimeConfigUpdate.transaction_id` | `bytes`, required, exactly 16 bytes (UUID) | Reject zero value | Unique per logical update; same ID and same payload/version/generation replays persisted state/result; same ID with different request is `tx_conflict` | `invalid_transaction_id` / `tx_conflict` |
| `RuntimeConfigUpdate.fencing_generation` | `uint64`, required and nonzero | Must exceed durable lower bound for a new transaction; equal value is allowed only for the same transaction replay | Compare-and-set against durable generation before Prepare; an older generation cannot overwrite a newer one | `stale_generation` |
| `RuntimeConfigUpdate.payload_sha256` | `bytes`, required, exactly 32 bytes | SHA-256 of exact payload bytes; compute before JSON parsing and durable write | Included in replay equality and durable transaction row | `payload_digest_mismatch` |
| `RuntimeConfigUpdate.expected_version` | `uint64`, required; zero is an explicit empty-state sentinel | Must match current durable active config version (`0` if none) for a new transaction | Compare-and-set prevents concurrent writers based on stale state | `stale_version` |
| `ApplyResult.version` | `uint64`, required semantic value | Echo accepted config version | Persist and replay with transaction | Stable result state/error |
| `ApplyResult.state` | `string`, required stable token | Allowlist: `prepared`, `active`, `activation_unknown`, `rejected`, `aborted`; no free-form values | Durable Reconciler state; `activation_unknown` is never converted to `active` or released by timeout | `error_code` carries the stable reason |
| `ApplyResult.objects` | repeated `ObjectResult`, optional when no per-object result exists | Maximum 10,000 entries; object IDs 1..128 UTF-8 bytes, state/reason use bounded allowlists/codes; no secret or target detail | Persist exact ordered result and replay it | Invalid/unsupported object result rejects the update before ACK |
| `ApplyResult.transaction_id` | `bytes`, exactly 16 bytes | Must match request | Correlates durable result after reconnect | `invalid_transaction_id` |
| `ApplyResult.fencing_generation` | `uint64`, nonzero for an accepted transaction | Must match request generation | Allows sender to reject stale acknowledgements | `stale_generation` |
| `ApplyResult.error_code` | `string`, empty on success | Maximum 64 ASCII chars; fixed codes only: `invalid_config`, `unsupported_config`, `identity_mismatch`, `stale_version`, `stale_generation`, `invalid_transaction_id`, `payload_digest_mismatch`, `tx_conflict`, `activation_unknown`, `prepare_failed`, `commit_failed`, `aborted` | Persist with result; retrying same transaction returns same code/state | No internal target, path, secret or filesystem detail |
| `RegisterRequest.enrollment_token` | `string`, required | 1..256 bytes; single-use and time-limited by enrollment service | Existing draft addition; token redemption is idempotent only for the same CSR and node identity | Existing registration error policy; never disclose token state to another identity |
| `RegisterRequest.csr_der` | `bytes`, required | 1..16,384 bytes; strict DER parse and key-strength policy | Existing draft addition; bind redemption to CSR digest | `invalid_csr` |
| `RegisterRequest.requested_node_id` | `string`, required | Canonical UUID, 36 ASCII chars; must match enrollment authorization | Existing draft addition; unique node identity | `identity_mismatch` |
| `ControlError.code` | `string`, required | 1..64 ASCII chars, stable error-code allowlist | Existing draft addition; retries reuse message identity | Unknown code closes/rejects the control operation; `retryable` is advisory, never authority to release holds |
| `ControlError.message_id` | `string`, required | Canonical UUID, 36 ASCII chars | Correlates the control request; must match its `ControlMessage.message_id` | `message_id_mismatch` |
| `ControlError.retryable` | `bool`, explicit | `true` only for transient transport/storage conditions | Retry must preserve transaction/message identity | False for validation, identity, version and fencing failures |

All semantic validation must occur before `Reconciler` persists Prepare intent. After Prepare, Commit/Abort results are persisted before a successful response is sent. A Commit timeout or lost response is `activation_unknown`; the sender queries/retries the same transaction ID and generation. It must retain old and new resource holds until durable engine/store evidence proves a terminal safe state.

## Response Correlation, Retry, and Session Identity

- A control stream is bound to the mTLS certificate's canonical `node_id`; the Agent also requires it to equal its durable local identity. Every `NodeMessage.node_id` and current `boot_id` must match the bound session before processing.
- `ControlMessage.message_id` is a canonical UUID unique per logical control message; `sequence` is strictly increasing for a sender session. Reconnection may replay the same logical transaction with the same transaction ID; sequence is not used as proof of transaction uniqueness.
- `ApplyResult.transaction_id` plus `fencing_generation` correlate the durable operation. A response to a duplicate transaction replays the stored result. Same transaction ID with different version, expected version, generation, digest, or bytes is a stable `tx_conflict`.
- Transient transport failure retries the identical message/transaction identity with bounded exponential backoff and jitter. Validation, identity, stale-version, stale-generation, and transaction-conflict errors are non-retryable until a new valid request exists.
- Never ACK config activation before the local durable transaction and Engine result have been recorded. A transport ACK is not proof of Engine activation. `activation_unknown` retains resource holds and requires status reconciliation.

## Requirement Mapping Review

The existing requirements map already references `vortex.control.v1` broadly; the proposed change should narrow/control the rows below as part of its implementation review. V1.11 is source context only; these IDs do not independently freeze acceptance.

| Requirement ID | Relationship to this proposal | Contract mapping after approval |
| --- | --- | --- |
| AGT-001 | Registration request token/CSR/node binding | `vortex.control.v1` |
| AGT-002 | mTLS bidirectional control session and reconnect | `vortex.control.v1` |
| AGT-003 | Durable active config and transaction outcome | `vortex.control.v1`, `vortex.runtime-config` |
| AGT-004 | Prepare/Commit/Abort reconciliation and per-object result | `vortex.control.v1`, `vortex.runtime-config` |
| AGT-005 | Authorization delivery envelope; authority semantics remain separate | `vortex.control.v1`, `vortex.runtime-config` |
| AGT-006 | Usage reporting and idempotent acknowledgements | `vortex.control.v1` |
| AGT-007 | Probe behavior is not defined by this control config update proposal | `vortex.runtime-config` (existing mapping); control mapping retained only if review confirms delivery is required |
| AGT-008 | Upgrade operation is not defined by this control protobuf proposal | `vortex.control.v1` only if an upgrade command is added later; otherwise no new mapping |
| AGT-009 | Boot identity and recovery sequencing envelope | `vortex.control.v1`, `vortex.runtime-config` |
| AGT-010 | Durable config activation sequence | `vortex.control.v1`, `vortex.runtime-config` |
| AGT-011 | Control-disconnected authorization boundary | `vortex.control.v1`, `vortex.runtime-config` |
| AGT-012 | Certificate renewal command/CSR transport | `vortex.control.v1` |
| HUB-001 | Direct: mTLS control stream identity and revocation gate | `vortex.control.v1`; certificate/revocation authority remains outside this message extension |
| HUB-002 | Related only: message transport may carry config/authorization, but signing and quota authority are not defined here | No new authority mapping; retain the existing relevant database/runtime mappings |
| HUB-003 | Direct: usage batch acknowledgement and delivery correlation | `vortex.control.v1`; durable Hub SQLite semantics are out of scope |
| HUB-004 | Direct: status/health updates use the control channel | `vortex.control.v1` for transport; status semantics remain separately specified |
| HUB-005 | Direct: backend control stream connection and reconnect transport | `vortex.control.v1`; production Hub failover authority is out of scope |
| HUB-006 | Related only: delegated quota values may arrive through control, but quota calculation/ownership is not defined here | No new mapping; retain database/runtime authority mapping |
| HUB-007 | Related only: autonomy transition may be reported over control; the mode and restrictions are not defined here | No new mapping; retain database/runtime authority mapping |
| HUB-008 | Related only: hard-deadline state may be transported; enforcement and trusted time are not defined here | No new mapping; retain database/runtime authority mapping |
| HUB-009 | Related only: policy/expiry updates may use control delivery; source accounting is not defined here | No new mapping; retain database/runtime authority mapping |
| HUB-010 | Direct: revocation/restriction delivery needs a control message | `vortex.control.v1` transport only; ordering/authority semantics remain separately governed |
| HUB-011 | Related only: signer/allocation state is not established by control transport | No new mapping; retain database/runtime authority mapping |
| HUB-012 | Related only: ledger rows may be transported, but durable Hub ledger semantics are not defined here | No new mapping; retain database authority mapping |
| HUB-013 | Related only: recovery handoff status may be transported, but atomic ledger rotation is not defined here | No new mapping; retain database/control mappings only as separately specified |
| HUB-014 | Direct: hub-switch/report envelope can use control transport | `vortex.control.v1` transport only; stale-response and resource-hold policy are outside this change |
| HUB-015 | Related only: frozen liability/ownership is not defined by message transport | No new mapping; retain database authority mapping |
| HUB-016 | Related only: per-source quota and share ownership are not defined here | No new mapping; retain database/runtime authority mapping |
| HUB-017 | Related only: restart recovery status may use control, but idempotent ledger recovery is not defined here | No new mapping; retain database/control mappings only as separately specified |
| HUB-018 | Related only: policy synchronization may be transported; effective-time and lease enforcement are not defined here | No new mapping; retain database/runtime authority mapping |
| SUP-AGENT-001 | Node enroll/control/status command surface | `vortex.control.v1`, `vortex.runtime-config` |

The final `requirements-map.yaml` update must list each ID individually, preserve unrelated mappings and add `not_applicable` with a reason only where an ID truly has no contract representation. The current broad mapping is insufficiently specific for this change but passes structural `check-map`.

## SemVer and Compatibility Impact

- Proposed control contract version: **1.1.0**, a compatible additive extension from the last approved 1.0.0 source. No existing field number, type, or approved field meaning changes.
- The existing draft additions (`RegisterRequest` and `ControlError`) and proposed config transaction/result fields are all new wire fields. Deployments gate config updates on explicit 1.1.0 support; 1.0.0 peers remain unable to apply them.
- Any change to an approved field's meaning, reuse of a field number, or incompatible required behavior is outside this minor proposal and requires a major-version proposal.
- Approval requires one coordinated change to `vortex.proto`, generated code, `versions.yaml`, per-requirement mappings, `CHANGELOG.md`, approved hash records, and the frozen-contract checker. CI must verify reproducible generation, semantic version increment, current hashes, and complete requirement mapping.
- The frozen checker should accept a later approved SemVer version instead of hard-coding `1.0.0`; `check-change` must remain responsible for requiring a valid SemVer increment and machine-readable change record on content changes.

## Unresolved Owner Choices

1. Approve this exact schema and validation bounds, request edits, or defer runtime configuration delivery.
2. Confirm that additive control changes may be deployed only after explicit 1.1.0 peer support, with no 1.0.0 fallback for config updates.
3. Confirm the 1 MiB payload and 10,000 object-result limits, or provide alternatives.
4. Confirm `expected_version=0` as the no-active-config sentinel and the fixed error-code set.
5. Confirm that the registration and generic error additions from the existing unapproved draft are included in the same 1.1.0 approval, or exclude them into a separate proposal.
6. VLink 1.0.0 hash drift is reported but not changed or implicitly approved here; it requires a separate owner decision before any hash/version record update.

## Review Record

| Decision | Owner | Timestamp (UTC) | Proposal SHA-256 | Selected version | Notes |
| --- | --- | --- | --- | --- | --- |
| Approved | User, in-session | 2026-11-05T11:26:21Z | `d166443ec3b6e92180592f7ef6354306169f69fd930c459e7ac869cdac3d9e7218` | `1.1.0` | Approved exact proposal. This decision does not approve the separate VLink 1.0.0 hash drift. |
