# Phase 5 Plan 05-03 Evidence (Partial)

Status: **In progress. This evidence covers durable usage handoff, bounded autonomy timing, the approved authorization payload, and stored authorization validation. It does not establish complete runtime authority enforcement.**

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
- `Node.NewCreditBook` now provides an Engine-to-Agent reservation and settlement adapter. The production `RuntimeEngine` still needs to use durable, source-bound grant/lease authority rather than treating a configured in-memory budget as authority.
- `unknown_upper_bound` remains durably stored on the Agent row after Hub ACK. The current approved `UsageBatch` wire message has no field that transfers this separate bound, so no code treats the Hub ACK as proof that the unknown liability is settled or releases it.
- `RecordBackendHeartbeat` is a persistence API but is not yet wired to an authenticated production backend heartbeat source. The caller must supply the fixed hard deadline; the maximum autonomy duration is not locked here.
- The owner approved `vortex.authorization.v1` 1.0.0 at proposal SHA `ac63e8301d6ccb05c6b55bd1675742a858fab1cab75993552ea00252017d6a07`. The exact proposal is retained as an immutable historical snapshot; its original pending wording is superseded by `docs/governance/contract-review.yaml`. `AuthorizationUpdate.generation = path_generation`; signer epoch remains independent.
- Grant/lease records and independent numeric signer/path fencing are persisted. Source-bound quota allocation, atomic remaining-quota transfer, and full recovery orchestration remain open. A stored authorization check does not prove parent delegation ownership or remaining spendable quota.
- The process-internal UsageSink persists reservation-before-write and settlement-before-release, with injected failure tests. Wiring this into the production runtime authority gate and exercising a real full-disk topology remain open.

## Incremental Reservation Durability (`c8c0dc6`)

- Added an Engine `UsageSink` boundary with FULL SQLite reservation-before-write and atomic settlement plus outbox insertion. Engine receives no writable batch when reservation persistence fails; any uncertain persistence/settlement freezes the gate and retains the entire reservation as unknown.
- Agent startup scans unresolved reservations from previous boot IDs before Engine startup, converts each to an unknown upper-bound usage batch, and leaves it in the normal Hub handoff queue. Recovery is retryable after interruption.
- SQLite storage now binds settlement retries to the original accounting identity, payload bytes, digest and unknown upper bound. A failed outbox insert leaves the reservation in `reserved` state.
- Added tests for pre-write refusal, disk/outbox write failure, payload-bound idempotency, cross-boot unknown recovery, and Engine-to-Agent persistence.
- Local host: root `go test ./...` PASS; Engine module `go test ./...` PASS; root Agent race tests and Engine runtime accounting race tests PASS; root and Engine vet checks PASS after cleanup.
- This is an incremental implementation snapshot, not the complete 05-03 acceptance. End-to-end grant/lease authority, signer-epoch ownership transfer, atomic remaining-quota transfer, and authenticated backend heartbeat wiring remain open. Docker/Actions results are listed separately below.
- Owner approved the exact `vortex.authorization.v1` 1.0.0 proposal SHA `ac63e8301d6ccb05c6b55bd1675742a858fab1cab75993552ea00252017d6a07`; `AuthorizationUpdate.generation` equals `path_generation`, and `signer_epoch` remains independent. Approval is recorded in `docs/governance/contract-review.yaml`; contract implementation and validation are now unblocked.
- Frozen JSON Schema SHA-256: `eb83a6050ca15acd5f7dd78fece4816c59dc10846d8539480eff2d5152f8d8af`. Contract catalog, approval record, SemVer changelog, 24 affected requirement mappings and schema embed are present. Control-stream validation and SQLite persistence now bind exact update bytes, signer epoch, path generation, fencing floor, individual authorization records and revocations; same-ID changed-payload replay and stale epochs are rejected.

## Approved Contract Follow-up (`130dcae`)

- Required authorization record fields cannot be omitted or null. Optional limits and snapshot IDs are validated whenever present, including on runtime leases. Zero/invalid heartbeat bounds are rejected.
- `Store.CheckStoredAuthorization` reads a consistent SQLite snapshot and checks node/boot, rule revision, path generation, billing snapshot, persisted active/revoked/settlement-only state and conservative time bounds. The entire trusted time interval must fit inside the authorization interval; exact expiry denies use.
- Signer epoch changes preserve outstanding authorization at its original boundary. Old paths reject new admission while permitting already established sessions to drain. Reopening the store and starting a fresh Agent boot cannot revive old settlement-only authorization, even if a caller supplies the old boot ID.
- This API returns a validation snapshot, not a write capability. It does not reserve quota, establish parent/source ownership, make a check-then-write sequence atomic, or prove that a caller's `ExistingSession` assertion is authentic. The runtime adapter must own those boundaries before complete acceptance.
- Local host: `go test -race ./agent/... ./hub ./test/phase5` PASS; targeted vet PASS.
- Full `make phase5-contract-check` PASS using the pinned protoc 36.2 archive, verified against locked darwin-arm64 SHA-256 `9cd98a532c5c5e0c4161314de0225de27e4c8a323917b6ea7b1b714d3ae23466`, and pinned Go generators. No global protoc replacement was made.
- GitHub Actions run [42042555033](https://github.com/VortexWang/Vortex/actions/runs/42042555033), SHA `c8c0dc62c3a20b25f2cbe138c47f0bc2a4d84b11`: contract gate, Wave 1 and Wave 2 PASS. This run predates the additional stored-use checks in `130dcae`.
- Docker Go 1.27.3, clean `git archive 130dcae77c45bc93032b9d5361966474d30e2108` mounted read-only: `go test -race ./agent/... ./hub ./test/phase5` PASS (Agent, identity, store, upgrade, Hub and tracer packages). The retained user edits are excluded from this snapshot.
- GitHub Actions run [42044110569](https://github.com/VortexWang/Vortex/actions/runs/42044110569), same SHA `130dcae77c45bc93032b9d5361966474d30e2108`: contract gate, Wave 1 and Wave 2 PASS. This establishes the current incremental regression result, not complete 05-03 acceptance.
- At `c8c0dc6`, Phase 1 remains failed on the existing VLink approved-review hash mismatch; Phase 3 fails during its benchmark; Phase 4 fails the 64 MiB binary integrity/half-close check. These results are not green and are not silently changed by this authorization work. Phase 4 remains on its separately agreed workstream.

This file is incremental evidence, not a 05-03 SUMMARY and not a phase completion claim.

## Durable credit-gate increment (`84dd896`)

- Added an Agent-owned credit-grant Gate and a pre-start Engine injection interface. Each writable batch atomically checks persisted node/boot/rule/revision/path/snapshot/time and subtracts a full byte upper bound under SQLite FULL before returning to Engine. Concurrent reservations cannot overdraw the grant.
- Settlement binds the original grant identity, returns only proven unused bytes, and commits usage/outbox with the reservation state. An unknown outcome retains its entire bound. Restart recovery retains the grant ID in the unknown outbox record.
- A persistence failure latches the Gate against later reservations and new sessions. Tests cover other already-open sessions attempting subsequent reservations after the fault. This does not establish interruption of writes for batches already issued before the fault.
- Source ownership is single-owner and conservative in this increment. A parent authorization ID or higher signer epoch alone cannot transfer the remainder: a second grant on the same source returns `ErrAuthorityHandoffRequired`, with the complete update rolled back. No implicit signer-epoch ownership transfer is implemented.
- Balance audits read a consistent SQLite transaction and reject arithmetic overflow. Added tests cover concurrent exhaustion, replay after revocation, changed grant identity on settlement, restart unknown recovery and SQL-trigger write failures.
- SQLite schema version is 4. Existing authorization rows are not assigned synthetic balances during upgrade; without a durable balance they remain unable to reserve new bytes. Recovery/migration of previously issued grants requires explicit reconciliation.
- Added `make phase5-authorization-check` to run Agent/Hub race, targeted vet and Engine accounting/runtime race checks. It retains host, SHA, raw test logs and a report that explicitly sets `plan_complete: false`; the same command runs in Docker and Actions.
- Local root `go test ./...`, Agent/Hub race and targeted vet PASS; Engine `go test ./...` PASS.

Remaining: runtime lease admission, production configuration selection/wiring, authenticated conservative time/backend heartbeat, full-disk topology, durable signer closure/ownership handoff and complete Hub authority. `NewAuthorizationGate` is a credit adapter, not the complete production authorization policy. The `vortex-node` CLI is not yet automatically wired to it. The approved authorization and control contracts are unchanged.

### Same-source verification for `84dd89655adc1ca6296da43b7265d197c0ae143f`

- Local Docker: a clean `git archive` of the exact SHA mounted read-only; pinned `golang:1.27.3` digest `sha256:c292963ad0e5f51e843c726edc204a4e4c26a859ba1e4d736d91028aa3633048`; `make phase5-authorization-check` equivalent script invocation PASS. Raw host/race/vet logs and report: `artifacts/phase5/authorization/84dd896/docker/`.
- GitHub Actions: [run 42051210228](https://github.com/VortexWang/Vortex/actions/runs/42051210228), same SHA, PASS. Contract checks, Wave 1, Wave 2 and `make phase5-authorization-check` completed. Downloaded raw authorization artifact: `artifacts/phase5/authorization/84dd896/actions/`; run metadata retained beside it.
- Both authorization reports deliberately record `plan_complete: false`. These are incremental correctness regressions, not full-disk or production runtime/Hub authority acceptance.

## Lease and authenticated heartbeat increment (`1d900cd`)

- `AuthorizationUpdate` received through the authenticated gRPC control stream now stores its explicit backend heartbeat and fixed autonomy deadline atomically with the authorization records. A plain Hub receipt still cannot become backend heartbeat evidence. Equal-heartbeat redelivery must preserve the original deadline and cannot reopen a closed signer; late/new heartbeat after autonomy requires handoff.
- `NewLeasedAuthorizationGate` requires named runtime and share leases as well as the credit grant. New admission and each pre-write reservation validate all three against the same SQLite snapshot, including boot, local rule/revision/path and conservative expiry. Tests revoke/expire leases while a session exists and confirm no further byte allocation occurs.
- Added a boot-local authority clock for authenticated update delivery. It starts untrusted, uses the random boot ID's creation time to conservatively bound an update's age, intersects later observations, includes a 1000 ppm drift budget and 10 ms margin, and rejects detected clock discontinuity. Bootstrap must complete within 300 seconds; an unrefreshed anchor becomes untrusted after 300 seconds. Replays cannot move time backwards or extend the persisted autonomy deadline. These clock budgets are explicit implementation bounds, not measurements of host oscillator accuracy.
- Protocol-test Hub now supports generating a node/boot-bound authorization update after authenticating the client. The real mTLS test proves heartbeat persistence and that a following ordinary reconnect leaves the original deadline intact.
- Local root tests, Agent/Hub race tests and targeted vet passed. The first Docker invocation did not run tests because the OrbStack socket disappeared; the service was restarted before retrying the same clean source archive. Final Docker/Actions evidence follows separately.

Remaining 05-03 acceptance work:

1. Select and compose the leased Gate from the actual Agent/Engine runtime configuration and enforce share connection/rate limits. The current CLI does not automatically install it; the Engine lifecycle remains a static destination tracer.
2. Connect persisted existing-rule/share/delegation ownership to offline renewal decisions instead of caller-supplied booleans; complete the bounded protocol-test Hub issuer/ledger behavior.
3. Transfer unknown-liability bounds to Hub with approved wire semantics. Current wire `UsageBatch` lacks the separate bound; the Agent conservatively retains it, so no release is implied by ACK.
4. Exercise real full-disk failure and mixed Agent/Hub/Engine traffic under the final composed runtime, then retain same-SHA Docker/Actions evidence. SQL-trigger failure tests alone do not satisfy this gate.
5. Prove signer closure/ownership guards at the issuance boundary; full prefix S/remainder R transfer and activation recovery belong to dependent 05-04.

05-03 remains executing; no SUMMARY or completion marker is produced by this increment.

### Verified pair for `1d900cdc97e8695aec25dbfeb45e385163338404`

- Local Docker PASS after OrbStack recovery, using a clean read-only source archive and the previously pinned Go 1.27.3 image digest. Raw logs/report are under `artifacts/phase5/authorization/1d900cd/docker/`.
- [GitHub Actions run 42052706718](https://github.com/VortexWang/Vortex/actions/runs/42052706718) PASS at the identical SHA: frozen contracts, Wave 1, Wave 2 and incremental authorization race regression. Downloaded logs/report are under `artifacts/phase5/authorization/1d900cd/actions/`.
- These runs verify the heartbeat/clock and lease-bound credit increment, not the remaining runtime composition, share enforcement, offline issuance or full-disk acceptance items listed above. Both reports preserve `plan_complete: false`.

## Automatic runtime authority composition (`d64fe72`)

- `Node.New` now installs a dynamic durable Gate into `RuntimeEngine`, using the local Engine configuration's rule, revision and path generation. The static destination runtime accepts non-qualification configurations but refuses to start without an Agent Gate. No remote OPEN can alter the locally configured target or rule.
- Target admission moved before dialing: local topology checks run first, then durable runtime/share/grant admission. The same admitted session is used for transfer; a second post-dial admission is not created.
- Added persistent runtime session slots and a per-share-source rate bucket (SQLite schema version 5). All Gate instances count the same held connection slots. Unknown previous-boot slots continue to count; a different boot cannot release them. Each write revalidates the pinned runtime/share/grant and open session in the reservation transaction. Rate debits are atomic with quota holds; returning unused quota does not refund rate tokens. A prior-boot rate bucket requires handoff rather than being silently refilled.
- New TCP and UDP tests start the real `RuntimeEngine` and VLink TLS/HELLO, demonstrate no target contact before authorization, obtain boot-bound authorization from the mTLS Hub fixture, reject a second stream at the one-connection share limit, transfer payload in both directions, and reconcile exact byte totals against the Agent source ledger.
- The same fixture sends resulting usage over the real control stream, waits for durable Hub acknowledgements, and verifies the Hub ledger has both directional batches. With the evidence environment enabled it retains compressed, closed SQLite Agent/Hub files and their uncompressed SHA-256 values. The archive contains test authority and ledger state, not runtime credential private keys.
- Local Agent/Hub race and targeted vet PASS; Engine accounting/runtime race PASS. Docker/Actions evidence follows below after completion.

Current remaining 05-03 boundaries: restricted offline issuance must be derived from durable parent/share/delegation ownership rather than booleans; unknown upper bounds still need explicit wire treatment; real full-disk regression and issuance-boundary fencing guards remain open. The Agent lifecycle supports a static destination configuration; source/relay topology process composition is not supplied by this increment and still needs assessment against full runtime acceptance. No full 05-03 completion is claimed.

## Actual full-disk runtime regression (`cebe5ab`)

- The TCP/UDP runtime fixture can place the Agent database on an explicitly supplied isolated 4 MiB tmpfs, fill it to operating-system ENOSPC, and assert SQLite error code 13. The default test never fills the host filesystem; a misconfigured fixture fails after a bounded 16 MiB write.
- After successful authorized traffic, the fixture exhausts the Agent filesystem and sends additional traffic over the existing stream. The Engine stops the stream before the target receives those bytes. Deleting the filler does not automatically reopen the fault-latched Gate. Previously confirmed usage still reaches the separate Hub ledger after storage recovery.
- The fixture retains compressed closed Agent and Hub SQLite files plus SHA-256 values. Independent read-only inspection of the Docker snapshots returned `PRAGMA quick_check = ok`; both TCP and UDP Agent/Hub stores contain two usage records, covering the known ingress and egress batches.
- This supplies actual full-disk evidence for the composed static-destination path. It does not claim a power-loss test, unknown-outcome wire handoff, all source/relay roles, or 05-04 recovery completion.
- Docker clean SHA `cebe5ab0bf181ea6e06f36d27b84ee60f60d8b5c`, pinned Go 1.27.3 image, isolated 4 MiB tmpfs: PASS. `full_disk_requested: true` is recorded. GitHub Actions verification is recorded below when terminal.

## Next contract checkpoint

The exact unknown-liability wire proposal is retained in `05-03-UNKNOWN-USAGE-PROPOSAL.md`, SHA-256 `4f44708c18bd4086c49ee1fb9af611e23a98501ff9e52679e0a51995f8e38d67`, requesting additive `vortex.control.v1` 1.2.0. Owner approval is pending. No normative contract was changed. Approval is required by `docs/governance/contract-review.yaml` before changing frozen wire semantics. Offline issuance/ownership work can proceed independently while this decision is pending.

### Verified full-disk pair for `cebe5ab0bf181ea6e06f36d27b84ee60f60d8b5c`

- Local Docker and [GitHub Actions run 42054542619](https://github.com/VortexWang/Vortex/actions/runs/42054542619) both PASS at the identical source SHA. Both reports record `full_disk_requested: true` and `plan_complete: false`. Actions also passed the contract gate and preceding Wave 1/2 regressions.
- Raw artifacts and run metadata are retained under `artifacts/phase5/authorization/cebe5ab/{docker,actions}/` and `actions-run.json`.
- Independently verified all eight closed SQLite snapshots (Agent/Hub × TCP/UDP × Docker/Actions): decompressed SHA-256 matches each fixture manifest, `PRAGMA quick_check` returns `ok`, and each snapshot contains two known directional usage batches. Machine-readable results: `artifacts/phase5/authorization/cebe5ab/ledger-audit.json`.
- This closes the actual-full-disk mixed-runtime regression item for the static destination path. Offline issuance/ownership, typed unknown-liability handoff, and broader runtime lifecycle acceptance remain open.
