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
{"changes": [{"contract_id":"vortex.control.v1","from":"1.0.0","version":"1.1.0","kind":"compatible","proposal_sha256":"d166443ec3b6e92180592f7ef6354306169f69fd930c459e7ac869cdac3d9e7218","approved_at":"2026-11-05T11:26:21Z","summary":"Add config transaction identity, fencing, digest, expected version, apply result correlation, registration and stable errors."},{"contract_id":"vortex.authorization.v1","from":"none","version":"1.0.0","kind":"new","proposal_sha256":"ac63e8301d6ccb05c6b55bd1675742a858fab1cab75993552ea00252017d6a07","approved_at":"2026-11-05T13:41:37Z","summary":"Freeze the independent JSON authorization payload schema with distinct signer epoch and path generation semantics."}]}
```
