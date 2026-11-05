# 05-03 Unknown Usage Wire Proposal

Status: Owner review required. No normative contract or generated code changed.

## Problem and approval requested

Approve an additive `vortex.control.v1` **1.2.0** extension from the current 1.1.0 source SHA-256 `0b1ecdb1480fd28632571a14850fff96907dc2ba32a06ea92486667705328da0`.

The current `UsageBatch` carries actual byte deltas but has no separate unknown-liability bound. The Agent preserves that bound in SQLite; a normal usage ACK cannot prove that the Hub stored the bound. Encoding an uncertain reservation as actual consumption would destroy the distinction required by 05-03 and the project's accounting invariant.

`vortex.authorization.v1` 1.0.0, VLink and runtime configuration contracts are unchanged by this proposal.

## Exact additive protobuf delta

Keep every existing message, field number, type and meaning unchanged. Change the contract version comment to 1.2.0 and add only the following members/messages:

```proto
// Add to NodeMessage.body, using the next unused field number.
UnknownUsageBatch unknown_usage_batch = 15;

// Add to ControlMessage.body, using the next unused field number.
UnknownUsageAcknowledgement unknown_usage_ack = 14;

message UnknownUsageBatch {
  string batch_id = 1;
  string origin_boot_id = 2;
  string billing_snapshot_id = 3;
  string rule_id = 4;
  string grant_id = 5;
  string role = 6;
  string direction = 7;
  uint64 unknown_upper_bound = 8;
}

message UnknownUsageAcknowledgement {
  string batch_id = 1;
  bytes payload_sha256 = 2;
}
```

## Normative behavior

1. `unknown_upper_bound` is an outstanding liability reservation, **not actual consumed bytes**. It is positive and at most 9223372036854775807. No component may infer its consumption, refund, expiry release or settlement from this message or its ACK.
2. `batch_id` is 1..128 UTF-8 bytes and is the immutable original reservation identity. `origin_boot_id`, `billing_snapshot_id` and `grant_id` are canonical UUID strings. `rule_id` is 1..128 UTF-8 bytes. `role` is `source`; `direction` is `ingress` or `egress`. A malformed or partially missing message receives no success ACK.
3. The authenticated `NodeMessage.node_id` identifies the owner. Its envelope `boot_id` identifies the current sender; `origin_boot_id` identifies the process that made the reservation and may differ after restart. A reconnect does not invent a new batch identity.
4. Before transmission, the Agent durably records the deterministic protobuf serialization of `UnknownUsageBatch` and its SHA-256, along with the original reservation and upper bound. Crash/retry sends identical bytes. No lossy conversion discards the original persisted evidence.
5. The Hub validates identity, limits and payload, then atomically stores the raw unknown-batch bytes, original boot, snapshot, grant, upper bound, digest and an explicitly unresolved state under unique `(node_id, batch_id)`. A matching retry is idempotent; changed bytes under the same identity are rejected and retained for investigation. The same identity cannot also be accepted as an ordinary consumed-usage batch.
6. The Hub sends `UnknownUsageAcknowledgement` only after its FULL SQLite commit succeeds. `payload_sha256` is exactly 32 bytes and hashes the deterministic serialized `UnknownUsageBatch` that was durably accepted. The Agent marks transport handoff acknowledged only when both ID and digest match its durable wire record.
7. An ordinary `UsageAcknowledgement` can acknowledge only ordinary known-usage records. It cannot acknowledge an unknown batch. An unknown ACK cannot acknowledge an ordinary batch. Missing, mismatched and out-of-order ACKs never release the unknown liability.
8. Hub ACK permits transport retry suppression, **not removal of the Agent's unresolved reservation or source liability**. Authoritative settlement/return still requires later reconciliation proof. Unknown rows and their evidence remain retained until that proof.
9. Existing ordinary `UsageBatch` behavior is unchanged. Senders emit only `UnknownUsageBatch` for newly handed-off uncertain outcomes. Historical local unknown rows can be migrated to immutable typed wire records while preserving their original before-image; any row previously acknowledged through the old ordinary path remains frozen for explicit reconciliation, not silently retransmitted as a second financial event.

## Compatibility and validation

The extension uses new oneof arms, so a 1.1.0 implementation cannot mistake an unknown batch for ordinary consumption. An older peer will reject/ignore an unsupported body and cannot produce the typed hash-bound ACK; the sender retains its outbox and liability. No fallback to the old consumed-usage encoding is allowed.

Implementation must update the version catalog, approval hash, CHANGELOG, generated Go code and mappings for AGT-005/006/009/010/011/012 and applicable HUB accounting/handoff requirements. Existing contract checks must remain enabled.

Required tests: deterministic bytes/digest; unknown versus actual accounting distinction; durable Hub-before-ACK; wrong-hash and wrong-type ACK refusal; duplicate/conflicting batch IDs; lost ACK and restart replay; old-peer refusal; legacy unknown migration without duplicate release; same-SHA Docker and Actions with raw Agent/Hub SQLite evidence.

## Approval scope

Approve the exact delta and normative rules above as `vortex.control.v1` 1.2.0. This does not declare 05-03 complete and does not approve changes to the already frozen authorization JSON schema.
