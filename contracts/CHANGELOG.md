# Contract Changelog

Contract source baseline: VORTEX V1.11, SHA-256 `95c73f356841da9a41f9d8bd4ada008497bb1503904792d500701de1898653e5`.

## Draft 1.0.0 (2026-11-03)

- Created initial draft contracts for database, public OpenAPI, control-plane Protobuf, runtime configuration and VLink framing.
- Source: V1.11 normative text and Appendix B. Draft status is not approval or freeze.
- Draft content hashes are recorded for review; reviewer decisions remain pending in `docs/governance/contract-review.yaml`.
- Unspecified wire and schema details are listed in each artifact and must be resolved before approval.

## vortex.control.v1 1.1.0 (2026-11-05)

- Approved additive control messages for registration and stable errors, plus transaction identity, expected version, fencing generation, payload digest and durable apply result fields.
- Runtime configuration payloads are UTF-8 JSON validated against the runtime schema before Prepare; retries use the same transaction ID and uncertain Commit remains `activation_unknown`.
- Approval: exact proposal SHA-256 `d166443ec3b6e92180592f7ef6354306169f69fd930c459e7ac869cdac3d9e7218`; approved by the project owner in-session at 2026-11-05T11:26:21Z.
- Compatibility: additive minor release from the approved 1.0.0 baseline. No existing field number or meaning changed.

Machine-readable change records (CI authority):

```json
{"changes":[{"contract_id":"vortex.control.v1","from":"1.3.0","version":"1.4.0","kind":"compatible","proposal_sha256":"b301223dee73e37cc9544c18542ae9c7100cd67bd5b721e07b85f4a18c0d90bd","status":"pending-owner-approval","summary":"Candidate additive negotiated rule transactions and immutable per-node runtime artifacts; legacy behavior retained. No separate owner freeze approval yet."},{"contract_id":"vortex.authorization.v1","from":"1.1.0","version":"1.2.0","kind":"compatible","proposal_sha256":"fc67a1ec0b2036530d836f0d4e4b59694f21498230d45128b1f4a96dcce36e3a","approved_at":"2026-11-06T02:21:16.718444+00:00","summary":"Owner-approved lifecycle capability negotiation, durable renewal/recovery, identity and share/policy boundaries; legacy input behavior retained."},{"contract_id":"vortex.runtime-config","from":"1.1.0","version":"1.2.0","kind":"compatible","proposal_sha256":"b301223dee73e37cc9544c18542ae9c7100cd67bd5b721e07b85f4a18c0d90bd","status":"pending-owner-approval","summary":"Candidate additive negotiated rule transactions and immutable per-node runtime artifacts; legacy behavior retained. No separate owner freeze approval yet."}]}
```

## vortex.control.v1 1.2.0 (2026-11-05)

- Owner-approved exact unknown-usage proposal SHA-256 `4f44708c18bd4086c49ee1fb9af611e23a98501ff9e52679e0a51995f8e38d67`.
- Adds UnknownUsageBatch and UnknownUsageAcknowledgement. Unknown upper bounds remain unresolved liabilities, separate from actual usage; transport ACK requires durable bytes and matching SHA-256 and never releases quota.
- Existing fields and ordinary usage semantics are unchanged. Legacy migration retains original before-images and never re-emits previously acknowledged unknown rows as new financial events.

## vortex.authorization.v1 1.1.0

Owner approved exact proposal `185541ee09e8dfbb0333a6249e0571c805b182220c77e7c2a79976d45efa4b6e`. Adds optional epoch_handoffs with durable S/R attestation and local R budget. Control remains 1.2.0; old 1.0.0 payloads remain supported without the new property.

## Phase 5 lifecycle contracts (05-06)

Owner approved exact proposal `fc67a1ec0b2036530d836f0d4e4b59694f21498230d45128b1f4a96dcce36e3a` in-session. Control 1.3.0 adds negotiated lifecycle messages, typed backend restrictions and handoff requests, and hash-locked normative lifecycle/identity companions. Authorization 1.2.0 preserves the 1.1.0 input branch and adds complete share references, renewal correlation and immutable policy transitions. Runtime 1.1.0 retains old inputs and adds opt-in lifecycle/probe settings. New capability consumers remain pending in 05-07..05-13; schema publication does not claim runtime completion.

## Phase 6 rule transactions (06-03 candidate)

Control 1.4.0 / runtime 1.2.0 are candidate compatible extensions from 1.3.0 / 1.1.0. Exact review unit: `docs/changes/CR-005-rule-two-phase.json`, SHA-256 `b301223dee73e37cc9544c18542ae9c7100cd67bd5b721e07b85f4a18c0d90bd`. The implementation is necessary to the already authorized plan; no separate owner approval of this contract proposal has occurred. Legacy RuntimeConfigUpdate and the 1.3 Hub lifecycle retain their meanings. Historical `docs/governance/contract-review.yaml` remains unchanged; its freeze check intentionally fails until the owner approves the exact candidate. Generated/wire/schema/runtime validation is recorded separately.
