# Phase 5 Plan 05-03 Evidence (Partial)

Status: **In progress. This evidence covers durable usage handoff and bounded autonomy timing only.**

## Implemented in `cad20a4cf8d5148a274dc25c784bf069d98c6daf`

- Agent SQLite persists deterministic serialized `UsageBatch` bytes, their SHA-256, billing snapshot, and an explicit unknown-liability upper bound before the control client sends them.
- Local autonomy acknowledgement is a separate state and does not remove a batch from the Hub handoff outbox. A Hub acknowledgement transitions it out of the send queue only after the callback and local durable update succeed.
- Hub protocol-test storage commits each batch before sending `UsageAcknowledgement`; duplicate `(node_id, batch_id)` with identical content is idempotent, while changed payload/snapshot/liability is rejected.
- Store tests cover WAL/FULL reopen recovery, local-ack retention, Hub completion, duplicate replay across boot IDs, identity/content conflict and injected SQLite write failure.
- Agent/Hub local store persists the last successful backend heartbeat and an explicit absolute hard deadline. After 30 seconds without a newer heartbeat, the durable mode enters `autonomous`, closes the signer-epoch gate, and records the transition; hard expiry closes existing renewal. Untrusted time denies authorization, and restart does not recompute the deadline.
- Authorization checks allow autonomy only when rule, share and delegated-balance ownership are all marked existing. A heartbeat arriving after autonomy began cannot reopen the signer gate without a future explicit handoff path.

## Verification

| Environment | Command / result |
| --- | --- |
| Local host | `go test -race ./agent/store ./agent ./hub` — PASS |
| Local host | `go test ./...` — PASS |
| Local host, Engine module | `(cd engine && go test ./...)` — PASS |
| Docker Go 1.27.3 | root module `go test ./...` — PASS |
| Docker Go 1.27.3 | Engine module `go test ./...` — PASS |
| GitHub Actions | run `42031661527`, SHA `cad20a4cf8d5148a274dc25c784bf069d98c6daf`, Wave 1 and Wave 2 — PASS |
| Local host | `go test -race ./agent/store ./agent ./hub`, `go test ./...`, `go vet ./agent/... ./hub ./test/phase5 ./cmd/vortex-node` — PASS at `3688dd02a85c5a259bdb30d6472a363fc8cd5086` |
| Local host, Engine module | `(cd engine && go test ./...)` — PASS at `3688dd02a85c5a259bdb30d6472a363fc8cd5086` |
| Docker Go 1.27.3 | root and Engine module `go test ./...` — PASS at `3688dd02a85c5a259bdb30d6472a363fc8cd5086` |
| GitHub Actions | run `42034048292`, SHA `3688dd02a85c5a259bdb30d6472a363fc8cd5086`, Wave 1 and Wave 2 — PASS |

## Remaining Plan Work and Limits

- `hub.TestServer` is a protocol fixture, not the production Hub authority or ledger service.
- The Engine does not yet automatically emit usage through a production Agent accounting adapter; `Node.PersistUsageBatch` is the durable integration boundary exposed for that follow-up.
- `unknown_upper_bound` remains durably stored on the Agent row after Hub ACK. The current approved `UsageBatch` wire message has no field that transfers this separate bound, so no code treats the Hub ACK as proof that the unknown liability is settled or releases it.
- `RecordBackendHeartbeat` is a persistence API but is not yet wired to an authenticated production backend heartbeat source. The caller must supply the fixed hard deadline; the maximum autonomy duration is not locked here.
- The `vortex.authorization.v1` payload proposal remains pending owner approval; no authorization payload schema or epoch semantics were frozen by the separate approval of `vortex.control.v1` 1.1.0.
- Grant/lease persistence, numeric signer-epoch fencing, atomic remaining-quota transfer, and signer-epoch/path-generation recovery separation remain unimplemented and unverified.
- No production Engine accounting adapter persists each measured/unknown write outcome before the Engine releases its reservation; that integration and full-disk stop behavior remain open.

## Incremental Reservation Durability (Uncommitted)

- Added an Engine `UsageSink` boundary with FULL SQLite reservation-before-write and atomic settlement plus outbox insertion. Engine receives no writable batch when reservation persistence fails; any uncertain persistence/settlement freezes the gate and retains the entire reservation as unknown.
- Agent startup scans unresolved reservations from previous boot IDs before Engine startup, converts each to an unknown upper-bound usage batch, and leaves it in the normal Hub handoff queue. Recovery is retryable after interruption.
- SQLite storage now binds settlement retries to the original accounting identity, payload bytes, digest and unknown upper bound. A failed outbox insert leaves the reservation in `reserved` state.
- Added tests for pre-write refusal, disk/outbox write failure, payload-bound idempotency, cross-boot unknown recovery, and Engine-to-Agent persistence.
- Local host: root `go test ./...` PASS; Engine module `go test ./...` PASS; root Agent race tests and Engine runtime accounting race tests PASS; root and Engine vet checks PASS after cleanup.
- This is an incremental implementation snapshot, not the complete 05-03 acceptance. Durable grant/lease authority, numeric signer-epoch ownership transfer, atomic remaining-quota transfer, authenticated heartbeat wiring, and Docker/Actions same-SHA evidence remain open.
- Owner approved the exact `vortex.authorization.v1` 1.0.0 proposal SHA `ac63e8301d6ccb05c6b55bd1675742a858fab1cab75993552ea00252017d6a07`; `AuthorizationUpdate.generation` equals `path_generation`, and `signer_epoch` remains independent. Approval is recorded in `docs/governance/contract-review.yaml`; contract implementation and validation are now unblocked.
- Frozen JSON Schema SHA-256: `eb83a6050ca15acd5f7dd78fece4816c59dc10846d8539480eff2d5152f8d8af`. Contract catalog, approval record, SemVer changelog, 24 affected requirement mappings and schema embed are present. Control-stream validation and SQLite persistence now bind exact update bytes, signer epoch, path generation, fencing floor, individual authorization records and revocations; same-ID changed-payload replay and stale epochs are rejected.

This file is incremental evidence, not a 05-03 SUMMARY and not a phase completion claim.
