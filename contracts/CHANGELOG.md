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
{"changes":[{"contract_id":"vortex.control.v1","from":"1.3.0","version":"1.4.0","kind":"compatible","proposal_sha256":"b02a73a9d3d4d8506cfd79904efa6a5ebc430cd4b06d729a37b518e19d582bb4","status":"approved","summary":"Owner-approved additive negotiated rule transactions and immutable per-node runtime artifacts; legacy behavior retained.","approved_at":"2026-11-07T09:33:05Z"},{"contract_id":"vortex.authorization.v1","from":"1.1.0","version":"1.2.0","kind":"compatible","proposal_sha256":"fc67a1ec0b2036530d836f0d4e4b59694f21498230d45128b1f4a96dcce36e3a","approved_at":"2026-11-06T02:21:16.718444+00:00","summary":"Owner-approved lifecycle capability negotiation, durable renewal/recovery, identity and share/policy boundaries; legacy input behavior retained."},{"contract_id":"vortex.runtime-config","from":"1.1.0","version":"1.2.0","kind":"compatible","proposal_sha256":"b02a73a9d3d4d8506cfd79904efa6a5ebc430cd4b06d729a37b518e19d582bb4","status":"approved","summary":"Owner-approved additive negotiated rule transactions and immutable per-node runtime artifacts; legacy behavior retained.","approved_at":"2026-11-07T09:33:05Z"},{"contract_id":"vortex.database","from":"1.0.0","version":"2.0.0","kind":"breaking","proposal_sha256":"b02a73a9d3d4d8506cfd79904efa6a5ebc430cd4b06d729a37b518e19d582bb4","status":"approved","summary":"Breaking relative to frozen 1.0 conceptual schema: table/column/type and constraint changes. No in-place migration from that conceptual schema is claimed. Fresh backend databases apply unchanged existing migrations 00001..00004 then additive 00005; existing Phase 6 00004 databases upgrade via 00005. Old 1.0 data needs a separately reviewed migration before use.","approved_at":"2026-11-07T09:33:05Z"},{"contract_id":"vortex.public-api","from":"1.0.0","version":"2.0.0","kind":"breaking","proposal_sha256":"b02a73a9d3d4d8506cfd79904efa6a5ebc430cd4b06d729a37b518e19d582bb4","status":"approved","summary":"Breaking relative to frozen 1.0 shared types: Restriction id/kind/active replaced by cause/source/version/effective timestamps; ApiError requires request_id; objects reject undeclared fields. Business routes/If-Match/async operations are specified; HTTP implementation remains 06-07. /api/v1 is the route namespace, distinct from contract SemVer 2.0.0.","approved_at":"2026-11-07T09:33:05Z"},{"contract_id":"vortex.database","from":"2.0.0","version":"2.1.0","kind":"compatible","status":"pending-owner-approval","proposal_path":"docs/changes/CR-006-packages-entitlements.json","summary":"Additive immutable package entitlement and restriction lifecycle contracts; HTTP adapters in 06-07."},{"contract_id":"vortex.public-api","from":"2.0.0","version":"2.1.0","kind":"compatible","status":"pending-owner-approval","proposal_path":"docs/changes/CR-006-packages-entitlements.json","summary":"Additive immutable package entitlement and restriction lifecycle contracts; HTTP adapters in 06-07."}]}
```

## vortex.control.v1 1.2.0 (2026-11-05)

- Owner-approved exact unknown-usage proposal SHA-256 `4f44708c18bd4086c49ee1fb9af611e23a98501ff9e52679e0a51995f8e38d67`.
- Adds UnknownUsageBatch and UnknownUsageAcknowledgement. Unknown upper bounds remain unresolved liabilities, separate from actual usage; transport ACK requires durable bytes and matching SHA-256 and never releases quota.
- Existing fields and ordinary usage semantics are unchanged. Legacy migration retains original before-images and never re-emits previously acknowledged unknown rows as new financial events.

## vortex.authorization.v1 1.1.0

Owner approved exact proposal `185541ee09e8dfbb0333a6249e0571c805b182220c77e7c2a79976d45efa4b6e`. Adds optional epoch_handoffs with durable S/R attestation and local R budget. Control remains 1.2.0; old 1.0.0 payloads remain supported without the new property.

## Phase 5 lifecycle contracts (05-06)

Owner approved exact proposal `fc67a1ec0b2036530d836f0d4e4b59694f21498230d45128b1f4a96dcce36e3a` in-session. Control 1.3.0 adds negotiated lifecycle messages, typed backend restrictions and handoff requests, and hash-locked normative lifecycle/identity companions. Authorization 1.2.0 preserves the 1.1.0 input branch and adds complete share references, renewal correlation and immutable policy transitions. Runtime 1.1.0 retains old inputs and adds opt-in lifecycle/probe settings. New capability consumers remain pending in 05-07..05-13; schema publication does not claim runtime completion.

## Phase 6 rule transactions (06-03 approved)

The owner explicitly approved CR-005 with “批准 CR-005”. The exact review unit remains
`docs/changes/CR-005-rule-two-phase.json`, SHA-256
`b02a73a9d3d4d8506cfd79904efa6a5ebc430cd4b06d729a37b518e19d582bb4`.
The immutable proposal retains its original pending text; the actual subsequent
approval is recorded in `docs/changes/CR-005-APPROVAL.json` and the four appended
`docs/governance/contract-review.yaml` change approvals.

Control 1.4.0 / runtime 1.2.0 are compatible extensions from 1.3.0 / 1.1.0.
Legacy RuntimeConfigUpdate and the 1.3 Hub lifecycle retain their meanings.
Database / public API 2.0.0 are breaking changes relative to their actual frozen
1.0.0 schemas, included in the same approved review. Fresh/00004 backend migration
evidence remains separate from any upgrade of a historical 1.0 deployment; no such
historical upgrade is implemented or claimed. Catalog status changes to frozen are
approval metadata, with both candidate and frozen catalog hashes in the approval
record. All normative candidate source bytes remain unchanged.

## Phase 6 packages and entitlements (06-04 candidate CR-006)

Database and public API 2.1.0 are compatible extensions of the exact CR-005
2.0.0 review at `fc63d57406f9f0e3eebe99ad276f95b004add710`. The catalog marks
only these two contracts as candidate. CR-005 proposal, approval and historical
review records remain immutable; this entry is not owner approval.

Migration 00006 adds immutable package/catalog/subscription/source/billing facts,
code redemption and persisted due changes. Migrations 00001..00005 remain byte
identical. Code duration and amount become stored grant terms; capabilities and
reset policy remain bound to the code's package version. Different-package code
confirmation consumes the code, redemption, snapshot, change and audit atomically.
Future execution cannot apply early; stale input or revoked actor fails without
partial authority changes. Consumed grants are not silently refunded on later
failure. Unknown execution and funding liabilities remain held.

OpenAPI specifies catalog editing, immutable subscription terms, impact preview,
ordinary confirmation and code-backed preview/confirmation under `/api/v1`.
HTTP implementations remain 06-07. Online payment/auto-renewal, ledger funding,
signer/lease production and native dual-architecture acceptance are not claimed.
Monthly issuance/expiry and disabled revisions use durable workers; subsequent
funded deployment consumers remain 06-05/06-06.
