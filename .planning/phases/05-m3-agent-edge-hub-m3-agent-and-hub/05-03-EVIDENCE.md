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

## Durable Hub issuance increment

- Added a Hub-only persisted delegation/issuance boundary (SQLite schema 6). A backend-installed immutable allocation pins the Hub, node boot, existing rule revision/path, runtime/share source, snapshot, rate/connection limits, raw-byte ceiling and absolute deadlines. Node requests can only name that allocation, amount and TTL; they cannot introduce new rules, ports, DNS, registrations or cross-node shares. The installation API is a trusted backend-adapter boundary, not proof that the Phase 6 backend exists.
- Each issue atomically subtracts remaining delegated bytes and stores the exact outgoing authorization JSON plus digest under a stable request ID before returning. Duplicate/lost-response retries return identical bytes across reopen without refilling; changed requests conflict. Epoch closure blocks fresh issuance and cannot be reopened by a larger epoch number or another installation. Existing issued grants retain their original boundaries and reservations.
- The legacy boolean-only autonomy check now refuses offline issuance; callers cannot claim ownership with three true values. The new issuer reads the immutable scope and remaining balance inside its issuance transaction.
- The real TCP/UDP runtime fixture now obtains its initial grant from this persisted Hub issuer and the mTLS control stream. The actual-full-disk fixture uses separate Agent/Hub SQLite files on the same isolated filesystem, proving that a full disk refuses a new Hub issuance as well as stopping Agent forwarding.
- Local root tests, Agent/Hub race, targeted vet and pinned Go 1.27.3 Docker full-disk TCP/UDP tests PASS. Same-SHA final Docker/Actions evidence will be recorded after the implementation commit.
- Remaining: the Agent still conservatively rejects a second credit grant on an already-owned source; repeated Hub top-up delivery therefore is not yet an end-to-end acceptance result. Parent raw-byte allocation/billing conversion and authentication use the explicit protocol backend boundary until Phase 6. Typed unknown-liability handoff is awaiting the existing 1.2.0 proposal decision. Complete 05-03 acceptance and 05-04 ownership-transfer/recovery are not claimed.

### Verified Hub issuer pair for `e702704e55ecc92091ed497f40cd7239547dceb9`

- Clean pinned Go 1.27.3 Docker and [Actions run 42056223206](https://github.com/VortexWang/Vortex/actions/runs/42056223206) PASS. Raw results are retained in `artifacts/phase5/authorization/e702704/`. Both reports record real full-disk coverage and `plan_complete: false`.
- This SHA verifies initial durable Hub issuance and its source reserve, not the subsequent same-source top-up support described below.

## Same-source grant top-up increment

- Additional grants are accepted only within the original source's parent allocation, node boot, signer epoch, rule revision, path and immutable billing snapshot; the original hard deadline cannot grow. Different parents/epochs/boots/scopes still require explicit handoff and are rejected. Authority to allocate the parent's total remains at the authenticated Hub, whose preceding issuance transaction reserves each grant.
- Every grant keeps its own balance and usage holds. Proven unused bytes return to that same grant; old unknown liabilities are not transferred, refunded or erased by a new grant. Source audit sums all granted slices and verifies remaining + known + unknown + held conservation without overflow.
- The real mTLS/TCP/UDP fixture now sends two grants from the same persisted delegation, asserts the runtime selects the second grant, and keeps both reservations on the Hub. Unit tests cover replay, unknown old usage, correct per-grant refunds and refusal of changed ownership/boundaries.
- A compile error in the new assertion (grant ID is on UsageDelta, not UsageBatch) was corrected before acceptance testing. No production protocol changed.
- In-session grant/lease renewal and complete runtime lifecycle verification remain to be completed; this increment does not declare 05-03 finished.

## Current 05-03 acceptance audit

| Plan obligation | Current evidence | Remaining work |
| --- | --- | --- |
| Durable grants/leases, conservative time and 30-second loss transition | Persisted authorization and issuer transactions; clock/deadline/epoch unit and race tests | Renew authority for an already-open session without changing its billing snapshot or freeing old holds |
| Engine-to-Agent-to-Hub durable usage | Real TCP/UDP, FULL reservation/settlement/outbox and Hub-before-ACK tests | Separate unknown-liability wire handoff; proposal 1.2.0 still awaits approval |
| Restricted autonomy | Durable preinstalled node/rule/share/delegation scope; no new allocation during loss; source conservation under concurrent issuance | Complete the ongoing renewal loop and review all runtime roles against the plan |
| Signer-epoch and ownership safety | Durable irreversible closure; cross-epoch/source/boot top-ups rejected; old unknown liabilities retained | Full prefix/remainder transfer and recovery belong to dependent 05-04; 05-03 must still verify the integrated issuance boundary |
| Docker/Actions mixed fixture | `cebe5ab` and `e702704` paired evidence; latest `e04384d` Docker passes | Retain latest Actions evidence when the run completes |

No 05-03 SUMMARY or completion status has been created.

### Verified top-up pair and issuer identity follow-up

- Top-up SHA `e04384d40cffbc4d0cb53984bb5c7ae2b8aebf00` passed pinned Docker and [Actions run 42056885512](https://github.com/VortexWang/Vortex/actions/runs/42056885512). Raw snapshots retain two grants totaling 8192 bytes against a 12288-byte parent allocation; the Hub retains 4096 bytes unissued. Actual traffic is recorded against the second grant.
- Follow-up review identified the need to bind same-source continuation to the authenticated issuing Hub as well as the numeric epoch. The control TLS verifier now requires an unambiguous CA-verified Hub certificate identity and persists it with each authorization update/record (local SQLite schema 7). Cross-Hub or missing-identity top-ups fail closed even when parent/epoch match.
- Existing pre-upgrade records are not assigned fabricated issuer evidence: they retain an empty issuer and cannot accept source top-ups until reconciled. Existing authorization remains bounded by its original lifetime. The frozen wire/JSON contracts are unchanged.
- Added tests for missing/foreign issuers; the live mTLS fixture checks the persisted issuer identity. Local Agent/Hub race and vet passed. Final same-source verification for this follow-up is recorded after the next commit.

Next implementation boundary: a continuing session must renew runtime/share/grant authority in one durable transaction, preserving its fixed billing snapshot and source ownership. Selecting a fresh grant must not move or release outstanding holds from an older grant. The current session pins its admitted IDs and therefore fails closed when that authority expires/exhausts; new sessions can select a top-up, but seamless in-session renewal is not yet proven.

### Final verified pair for issuer/top-up/identity increment

- Code SHA `04bd2bf5647dc0eb280e519f5281105e78b154f3`: clean pinned Go 1.27.3 Docker and [GitHub Actions run 42057556887](https://github.com/VortexWang/Vortex/actions/runs/42057556887) both PASS. Actions includes frozen contracts, Wave 1/2 checks and the same incremental authorization/full-disk command.
- Raw host records, race/vet logs, verdicts and compressed SQLite snapshots are retained under `artifacts/phase5/authorization/04bd2bf/{docker,actions}/`, with Actions metadata and `ledger-audit.json` beside them.
- Independently verified all eight snapshot hashes, SQLite integrity and payload digests. Each Agent has two independent 4096-byte grants bound to the authenticated `hub` identity; the runtime's two directional batches spend the new grant. Each Hub has exactly two persisted issues (8192 bytes total), leaving 4096 of its 12288-byte delegation unissued. No unknown liability was released by top-up or expiry. Unknown-liability-specific behavior remains covered by the store tests and awaits typed wire handoff.
- The real full-disk fixture also refuses new Hub issuance while stopping Agent traffic before target delivery. Both reports retain `plan_complete: false`. This remains partial 05-03 evidence, with in-session renewal and the pending unknown-usage contract checkpoint still open.

## Durable in-session renewal increment

- Runtime session records now retain immutable initial grant/runtime/share IDs (SQLite schema 8). Existing rows are anchored to their previously pinned IDs during migration; no issuer or transferred rights are fabricated.
- Each pre-write reservation can atomically select valid replacement authority, update the current session IDs, debit the existing share-source rate bucket, reserve grant bytes and persist the usage hold in one FULL transaction. Candidates must preserve the initial issuer, source, parent, boot, rule/revision, path, signer epoch and billing snapshot. They cannot extend the initial hard deadline or enlarge its share limits.
- Outstanding batches retain their original grant identity even if the other direction renews the live session. Unknown old holds remain frozen. New authority cannot reopen a closed/revoked session or reset its rate bucket; failed writes roll back both the session update and accounting changes.
- The real mTLS/VLink TCP and UDP fixture now opens one stream, exchanges payload under the first grant, receives a second grant/lease set, waits beyond the first lease expiry, then exchanges payload over the same stream. Each generation produces two directional batches, with separate grant balances. Known usage is durably acknowledged by the Hub, and the existing real-full-disk checks remain enabled.
- Local root/Engine tests, Agent/Hub race and targeted vet PASS. Added tests cover unresolved-batch replay, old unknown liability, expired-lease renewal, write-failure rollback, shared-rate exhaustion, changed ownership/limits, clock/deadline/revocation, migrated session anchors and concurrent duplex retries.
- `tools/phase5/audit_runtime_renewal.py` independently audits closed SQLite artifacts, hashes, grant balances, immutable/current session IDs, pre/post-expiry settlement timestamps and Hub issue conservation. Same-SHA Docker/Actions results will be appended after the implementation commit.

05-03 remains executing. Typed unknown-liability handoff still awaits the existing control 1.2.0 proposal decision, and complete runtime-role/issuance acceptance remains to be reviewed. Full signer ownership transfer and restart recovery remain in 05-04. No SUMMARY or phase completion is claimed.

### Verified renewal pair and retained slots

- Renewal code SHA `86640e7c1bfefe420ad3eb9952bd0078c1ebaff9` passed clean pinned Docker and [Actions run 42060467701](https://github.com/VortexWang/Vortex/actions/runs/42060467701). Both executed real full-disk TCP/UDP coverage and retained `plan_complete: false`. Raw artifacts are under `artifacts/phase5/authorization/86640e7/`.
- Independently audited all eight raw SQLite snapshots. Each Agent records one continuing session, two 4096-byte grants with 4050 bytes left in each, and four committed 23-byte directional batches: two before and two after the original grant's expiry. Hub issuance remains 8192 of 12288 bytes, with 4096 unissued.
- UDP session-slot closure attempted while the filesystem is full can fail to persist. Those slots remain `open` in the evidence instead of being assumed released. The runtime is stopped, the fault latch rejects new streams, and recovery must later prove closure before reclaiming the slot. The audit reports this retained state explicitly; it does not certify 05-04 recovery or claim every slot was released.
- Phase 1 run `42060467716` failed only `contract-check`: existing VLink content differs from its approved hash. This drift is already documented by the 05-05 proposal; no VLink approval/version/hash was changed during renewal work. Remaining Phase 1 checks passed.

### Interim protection for unknown usage pending typed wire approval

- The old ordinary usage sender could serialize the local unknown upper bound as actual consumed bytes, losing its distinction at the Hub. It now withholds every durable record with `unknown_upper_bound > 0` from the ordinary wire path. Original payload/digest and unresolved reservation remain retained without migration or deletion.
- Ordinary Hub ACKs reject unknown rows transactionally, including mixed known/unknown ACK lists; notification hooks run only after durable validation. Known usage still reaches the Hub and is acknowledged independently. Tests cover send/retry suppression, mixed-ACK rollback and retained liability.
- This is a fail-closed interim fix within the existing 1.1.0 protocol. It does not implement or approve the proposed typed 1.2.0 handoff; that exact proposal remains pending owner approval.


## Approved control 1.2.0 and complete usage handoff implementation

- Owner approved exact proposal SHA-256 `4f44708c18bd4086c49ee1fb9af611e23a98501ff9e52679e0a51995f8e38d67` as `vortex.control.v1` 1.2.0. The proposal remains immutable; approval and source digest are recorded in governance. Pinned protoc 36.2 generation, frozen contract and requirement-map checks pass. Authorization JSON remains 1.0.0.
- Typed unknown batches now retain their original boot, grant, immutable deterministic bytes and hash, with separate unresolved liability and transport acknowledgement. FULL settlement/outbox insertion is atomic. A normal usage ACK cannot acknowledge them; typed hash-bound ACK never refunds or converts their upper bound into consumption.
- Added immutable legacy before-images, same-identity/type conflict protection and retained conflicting payloads. Tests cover wrong hashes, wrong ACK types, old peers, lost ACK, restart replay and atomic write failure.
- The online/autonomous review found that the protocol Hub previously ACKed after local persistence in both modes. `UsageRelay` now requires a backend-adapter durable hash confirmation online, records that receipt locally before Agent ACK, and retains batches after local autonomous ACK. A SQLite-backed protocol backend fixture verifies lost backend ACK, bad digest, Hub reopen, exact replay and unchanged hard deadline/signing closure. This is explicitly the Phase 5 backend boundary, not the Phase 6 production backend.
- The long-lived Agent control stream now checks its durable outbox every 10 seconds (test override available), serializing sends with configuration responses. Newly generated known and unknown usage no longer waits for reconnection.
- The TCP/UDP runtime fixture now verifies all four directional renewal batches against the Agent, Hub and protocol-backend SQLite files; the real full-disk fixture remains enabled. Final clean-SHA Docker/Actions results and plan audit follow after the code commit. No completion is claimed by this implementation note.

The preceding interim 1.1.0 safety guard was verified at SHA `1dbc3159acf478d775b4a2da51756a933438564f`: clean Docker and [Actions 42118509060](https://github.com/VortexWang/Vortex/actions/runs/42118509060) passed; all eight renewal snapshots passed independent audit. Artifacts are retained under `artifacts/phase5/authorization/1dbc315/`. This is historical evidence, not evidence for the new 1.2.0 implementation.

### Failed first full regression, retained for audit

- SHA `d8d0ac86517c6f77ac3da387d55ffdde9fc0d80b` failed clean Docker at `TestControlReportsNewUsageWithoutReconnecting` with send-side EOF. Its report/logs and available ledgers are retained in `artifacts/phase5/authorization/d8d0ac8/docker/`; Engine checks did not execute after this failure.
- Root cause: the finite protocol fixture counted duplicate batch messages toward its early exit, and the periodic sender returned immediately on EOF before draining final received ACKs. The fixture now counts unique batch identities, while the client stops sending and drains received messages after send EOF. No failed result is promoted to pass.
- The post-processing renewal auditor also narrows its filename match to the original two manifests so it does not confuse newly added three-party handoff manifests with the two-party renewal format.
