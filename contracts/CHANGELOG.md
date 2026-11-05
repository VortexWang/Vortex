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
{"changes": [{"contract_id": "vortex.authorization.v1", "from": "none", "version": "1.0.0", "kind": "new", "proposal_sha256": "ac63e8301d6ccb05c6b55bd1675742a858fab1cab75993552ea00252017d6a07", "approved_at": "2026-11-05T13:41:37Z", "summary": "Freeze the independent JSON authorization payload schema with distinct signer epoch and path generation semantics."}, {"contract_id": "vortex.control.v1", "from": "1.1.0", "version": "1.2.0", "kind": "compatible", "proposal_sha256": "4f44708c18bd4086c49ee1fb9af611e23a98501ff9e52679e0a51995f8e38d67", "approved_at": "2026-11-05T22:07:56Z", "summary": "Add independent unknown usage batches and hash-bound acknowledgements without releasing liabilities."}, {"contract_id": "vortex.control.v1", "from": "1.2.0", "version": "1.3.0", "kind": "compatible", "proposal_sha256": "1dfd8ae92fb93217011118ccbd605067f94b8944ef9e257fcadeba8cfcbb2064", "approved_at": "2026-11-05T23:42:15Z", "summary": "Add bounded credit demand and durable definitive denial; success retains AuthorizationUpdate and unknown issuance remains pending."}]}
```

## vortex.control.v1 1.2.0 (2026-11-05)

- Owner-approved exact unknown-usage proposal SHA-256 `4f44708c18bd4086c49ee1fb9af611e23a98501ff9e52679e0a51995f8e38d67`.
- Adds UnknownUsageBatch and UnknownUsageAcknowledgement. Unknown upper bounds remain unresolved liabilities, separate from actual usage; transport ACK requires durable bytes and matching SHA-256 and never releases quota.
- Existing fields and ordinary usage semantics are unchanged. Legacy migration retains original before-images and never re-emits previously acknowledged unknown rows as new financial events.

## vortex.control.v1 1.3.0 (2026-11-05)

- Owner approved the exact 04-13 demand proposal in chat: “批准 1.3.0 提案”.
- Adds CreditDemand (NodeMessage field 16) and CreditDemandDenied (ControlMessage field 15), with explicit denial enum.
- Only committed outcomes may resolve requests. Timeouts and transport failures retain pending identity; existing/unknown liabilities are never released by a denial.
- Additive fields only; deployment is opt-in for capable peers. Parameter lock and qualification remain separate.
