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
{"changes": [{"contract_id": "vortex.authorization.v1", "from": "1.1.0", "version": "1.2.0", "kind": "compatible", "proposal_sha256": "fc67a1ec0b2036530d836f0d4e4b59694f21498230d45128b1f4a96dcce36e3a", "approved_at": "2026-11-06T02:21:16.718444+00:00", "summary": "Owner-approved lifecycle capability negotiation, durable renewal/recovery, identity and share/policy boundaries; legacy input behavior retained."}, {"contract_id": "vortex.runtime-config", "from": "1.1.0", "version": "1.2.0", "kind": "compatible", "proposal_sha256": "b02a73a9d3d4d8506cfd79904efa6a5ebc430cd4b06d729a37b518e19d582bb4", "status": "approved", "summary": "Owner-approved additive negotiated rule transactions and immutable per-node runtime artifacts; legacy behavior retained.", "approved_at": "2026-11-07T09:33:05Z"}, {"contract_id": "vortex.database", "from": "1.0.0", "version": "2.4.0", "kind": "breaking", "status": "pending-owner-approval", "summary": "Integrate already-approved Phase6 database2.4.0, including CR005 breaking schema2.0 and CR006..009 additive changes; no historical1.0 deployment upgrade is claimed."}, {"contract_id": "vortex.public-api", "from": "2.3.0", "version": "3.0.0", "kind": "breaking", "status": "pending-owner-approval", "summary": "CR-009 explicit tenant paths, strong quoted CAS, actual status/DTO/page semantics and durable REST/SSE boundary."}, {"contract_id": "vortex.control.v1", "from": "1.7.0", "version": "1.8.0", "kind": "compatible", "status": "candidate", "change_request": "CR-012", "summary": "Union of owner-approved Phase6 control1.6 and Phase4 migrated control1.7; preserves every existing field and enum; exact combined bytes await separate approval."}, {"contract_id": "vortex.vlink.v1", "from": "1.0.0", "version": "1.1.0", "kind": "compatible", "status": "approved", "change_request": "CR-011", "proposal_sha256": "d53f94e7ca46df4522890a64fc1d5bb6c4f85642dc847a1313115353a7820d95", "proposal_json_sha256": "a29cc7103a2f195ea41741487b47f3a8f9d41792258de8916221eaa85617dd4f", "approved_at": "2026-11-09T11:04:51.435005+00:00", "summary": "Exact approval reconciles the historical draft and existing detailed VLink v1 specification; on-wire protocol 0x0100 and implementation bytes unchanged."}]}
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

## Phase 6 packages and entitlements (06-04 approved)

The owner explicitly approved CR-006 with “批准 CR-006”, including consumption
on successful confirmation and no automatic refund or use restoration on later
application failure. The immutable proposal SHA-256 remains
`60487b773b60f9b1bfee80811e49c17d43b910b7ff19c68025a33c9c5e83b28d`.
Approval is recorded in `docs/changes/CR-006-APPROVAL.json` and two appended
`docs/governance/contract-review.yaml` records. Database/API 2.1.0 are frozen;
only approval metadata and this appended notice differ from candidate bytes.
The earlier candidate section remains an immutable historical statement.

## Phase 6 source ledger and usage (06-05 candidate)

CR-007 proposes database/API 2.2.0 and control 1.5.0 as compatible extensions.
Only a newly negotiated usage-ledger-v1 Hub LedgerRecord carries the versioned
original node/report-boot/sequence/digest envelope. Protobuf tags and legacy
wire encodings are unchanged. Existing sequence-less SQLite payloads are retained
without invented provenance. Full signer and funded runtime producers remain
plan 06-06; HTTP adapters remain 06-07; final paired acceptance remains 06-11.
Unknown upper bounds remain frozen, distinct from measured bytes; 72 hours
is never an automatic release. Compensation requires approved actual platform
backing and source-local atomic accounting. No owner approval is implied.

## Phase 6 source ledger and usage (06-05 approved)

The owner explicitly approved CR-007 with “批准 CR-007”. The immutable proposal
SHA-256 remains `3ddd1ab79c656b3684e64a3854f32785ff2fb8998743ac73353cdc90b086b0c5`.
Approval is recorded in `docs/changes/CR-007-APPROVAL.json` and three appended
`docs/governance/contract-review.yaml` records. Database/API 2.2.0 and control
1.5.0 are frozen, including the exact control lifecycle companions. Only approval
metadata and this appended notice differ from candidate bytes. The earlier
candidate section remains an immutable historical statement. CR-005/006 approval
history is unchanged. Unknown liabilities require funded proof to close;
06-06 signer/lease production, 06-07 HTTP and 06-11 paired acceptance remain due.

## Phase 6 limits and funded runtime (06-06 candidate)

CR-008 proposes database/API 2.3.0 and control 1.6.0 as compatible extensions.
Shares, runtime leases and source-funded credit remain independent rights.
Renewal/closure, complete inventory, original-source finalization and explicit
Node/Hub boot recovery require their own durable authenticated proofs. Unknown
liabilities remain held. Negotiated capabilities isolate new commands/journals;
existing field numbers and old wire encodings remain unchanged. Runtime,
authorization and VLink frozen schemas retain their approved bytes/versions.
No owner approval is implied; HTTP adapters remain06-07, paired acceptance06-11.

## Phase 6 limits and funded runtime (06-06 approved)

The owner explicitly approved CR-008 with “批准 CR-008”. The immutable proposal
SHA-256 remains `4e29497f82808b9a8693030ef400dae3a2bc42cc27cf8a39f24bdcf8605074bd`.
Database/API2.3.0 and control1.6.0 are frozen with the exact normative companions
and codecs inventoried by the proposal. CR-005/006/007 proposal and approval
bytes remain unchanged. Approval is recorded in docs/changes/CR-008-APPROVAL.json
and three appended governance records. HTTP adapters remain06-07; final same-SHA
Docker/Actions pairing and broader phase acceptance remain06-11.

## CR-009 public REST / durable events candidate (06-07, unapproved)

Public API2.3.0 ->3.0.0 is a breaking candidate. Implemented routes consistently
require an explicit tenant path; old unscoped limits, ledger, billing and catalog
paths are removed. CAS uses one strong quoted decimal ETag; invalid input is400,
missing If-Match428, stale412, conflicts409. Code redemption returns202 with its
exact operation; DNS idempotency is a header. Cursor pages omit next_cursor on
last page; safe response DTOs and persisted stages now match actual adapters.
The contract SemVer changes independently of the retained /api/v1 URL prefix.

Additions include opaque rotating token families with one narrowly documented
refresh-body authentication exception, trusted local initial session issuance,
current-principal reads, authorized resource/operation pages, versioned business
mutations, proof-bound aggregate port operations, DNS intent/consumer and safe
existing recovery status/advancement. SSE uses durable per-tenant PostgreSQL
sequence with signed principal-bound resume IDs, bounded streams and resync.

Database2.3.0 ->2.4.0 is an additive candidate: migration9 introduces hash-only
token families/revocations, transactional event sequencing/retention, immutable
public ordering and exact aggregate port operation bindings. Existing migrations
1..8 are unchanged. Other frozen contracts and prior approvals remain unchanged.
No approval or freeze is implied; precise CR-009 review and clean-source evidence
must precede owner approval. Final same-SHA Actions pairing remains06-11.

## Phase 6 REST and durable events (06-07 approved)

The owner explicitly approved CR-009 with “批准 CR-009”. The immutable proposal
SHA-256 remains `ed7ca2f0cf68f46d29984b7ddafbc13fc8de4ad171cd3378b0b9dda8e45bdfe0`.
Approval is recorded in `docs/changes/CR-009-APPROVAL.json` and two appended
`docs/governance/contract-review.yaml` records. Public API 3.0.0 is a breaking
release from 2.3.0; database 2.4.0 is an additive release from 2.3.0.
All 99 candidate artifacts are bound to the clean tested source 97966a3a;
only catalog freeze metadata and this appended notice differ from candidate bytes.
The proposal, historical CR approvals, migrations 1–8 and other frozen contracts
remain unchanged. Existing pending text is historical candidate metadata.
Final same-SHA Docker/Actions and cross-domain acceptance remain 06-11.

## vortex.control.v1 1.7.0 approved merge migration (2026-11-09)

Owner approved CR-010 in session (approval and exact hashes in governance). Keeps main lifecycle fields unchanged;
adds opt-in experimental demand/denial at field 100. Old experimental 16/15
peers require coordinated replacement; no automatic compatibility fallback.
Phase 6 1.4..1.6 features remain on their original lineage. Historical approval
records retain their original hashes; the new CR-010 record approves this migration.

## vortex.vlink.v1 1.1.0 (2026-11-09)

CR-011 was explicitly approved in session. It records the exact existing detailed frame, HELLO/exporter identity, OPEN bounds, window and failure-scope specification after the old approval remained bound to a draft. The original draft approval is retained in change_approvals; it is not relabeled as approval of the later detailed bytes. The catalog becomes 1.1.0 while the implemented on-wire version stays 0x0100; no new representation, negotiation or authority relaxation is authorized.

## Phase 4 / Phase 6 integration (CR-012 candidate)

Control 1.8.0 combines the exact Phase6 1.6.0 production messages and the
CR-010 1.7.0 field100 experimental demand messages. Published fields, enum
values, capabilities, source/boot/fencing/deadline and accounting semantics
remain intact. The combined Protobuf and lifecycle narrative are new exact
bytes; prior approvals remain historical and do not approve this candidate.
VLink1.1.0 remains the exact CR-011 specification, wire0x0100 unchanged.
Database2.4.0, API3.0.0, runtime1.2.0 and authorization1.2.0 are retained.
