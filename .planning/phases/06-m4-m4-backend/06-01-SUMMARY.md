---
phase: 06-m4-m4-backend
plan: 01
subsystem: database
tags: [postgresql, pgx, goose, sqlc, grpc, mtls, outbox, valkey]
requires:
  - phase: 05-m3-agent-edge-hub-m3-agent-and-hub
    provides: control 1.3.0, durable Agent Reconciler and Hub SQLite authority primitives
provides:
  - Independent backend module and explicit PostgreSQL migration administration
  - Scoped desired-state transactions with immutable audit, operation, task and Outbox
  - Bounded SKIP LOCKED delivery and atomic receipt/task/operation/Outbox confirmation
  - Real backend, Agent and blocked Hub mTLS process tracer and clean committed Docker evidence
affects: [06-08, 06-09, 06-02, 06-03, 06-05, 06-06, 06-11]
tech-stack:
  added: [pgx v5.11.0, Goose v3.29.0, sqlc v1.31.1, PostgreSQL 18.6, Valkey 9.1.3]
  patterns: [tenant-scoped SQL, transaction audit, transactional outbox, conservative unknown, role-specific mTLS roots]
key-files:
  created:
    - backend/db/migrations/00001_authority.sql
    - backend/db/query/authority.sql
    - backend/db/generated/authority.sql.go
    - backend/internal/service/tracer.go
    - backend/internal/worker/outbox.go
    - backend/internal/control/adapter.go
    - hub/backend_client.go
    - tools/phase6/docker_check.sh
    - deploy/backend/compose.test.yml
  modified: []
key-decisions:
  - NodeControl sends RuntimeConfigUpdate directly to the real Agent; HubControl remains its existing liveness-only contract slice, with no invented allocation ACK or wire change.
  - The tracer Agent applies a configuration with no listeners/rules through its real ControlClient, SQLite Reconciler and Engine transaction manager; this is control evidence, not funded forwarding activation.
  - A fresh Hub persists backend heartbeat before publishing blocked status and retains no signer; full financial authority adapters remain 06-05/06-06.
  - Use a clean committed clone for exact-SHA Docker checks while preserving all unrelated dirty and untracked caller files.
requirements-completed: [BE-001, SUP-B2-001, SUP-B5-001]
coverage:
  - id: D1
    description: Scoped PostgreSQL mutation atomically commits desired state, operation, task, Outbox and audit; injected audit failure rolls all five back.
    requirement: BE-001
    verification:
      - kind: integration
        ref: backend/internal/service/tracer_test.go#TestTracerTransactionRollback
        status: pass
    human_judgment: false
  - id: D2
    description: Real mTLS Agent process confirms target revision and durable duplicate replay; timeout, late result and disconnect retain unknown occupation.
    requirement: SUP-B5-001
    verification:
      - kind: e2e
        ref: backend/internal/service/process_test.go#TestTracerRealProcesses
        status: pass
    human_judgment: false
  - id: D3
    description: PostgreSQL remains authoritative when Valkey is flushed or disconnected and reconstructs the implemented authority projection.
    requirement: SUP-B2-001
    verification:
      - kind: integration
        ref: backend/internal/service/tracer_test.go#TestValkeyLossRebuild
        status: pass
    human_judgment: false
actuals:
  tokens: 37782
  tasks: 1
  commits: 2
plan_head_before: 820af7ad7cc18bf04a08f0745b2d266b77d2b899
plan_head_after: dedcb798781433531987b98678b238710e4d43ad
duration: 29min
completed: 2026-11-07
status: complete
---

# Phase 6 Plan 1: PostgreSQL authority and real control tracer Summary

**PostgreSQL-backed desired operations now traverse durable task/Outbox leasing to a real Agent mTLS receipt, with unknown occupation retained through timeout, late results and disconnect.**

## Performance

- Started: 2026-11-06T18:40:34Z; completed: 2026-11-06T19:09:00Z.
- One task; 26 source/tooling files and 24 raw evidence files.
- Clean Docker normal tests: 6.325s; race tests with instrumented backend/Agent/Hub child processes: 8.123s. Focused native checks: 0.726s. Initial image/download/build provisioning is additional and is not included in those test durations.

## Accomplishments

- Added an independent Go 1.27.3 backend module, explicit Goose administration command, first authority migration, pgx/sqlc query consumers and two-independent-tree generation comparison.
- Enforced explicit tenant scope, expected revision, business idempotency, immutable audit and all-or-nothing desired/operation/task/Outbox/audit commits. Concurrent identical requests produce one operation; changed identity bytes fail.
- Workers commit leases before external I/O, bound delivery, then atomically persist receipt/task/operation/Outbox confirmation. Expired lease, timeout, disconnect and stale revision preserve unknown occupation. Explicit reconciliation converges status without releasing occupation.
- Separate backend, Agent and Hub processes use TLS 1.3 and distinct role roots. Duplicate delivery uses fresh transport message UUIDs and the same durable transaction UUID: two deliveries produce one Agent SQLite application. SIGSTOP/SIGCONT proves real-process timeout and late-result behavior; the wrong role certificate is rejected.
- Valkey flush and connection loss leave current PostgreSQL operation facts, ownership/occupation and confirmed tasks unchanged; the projection rebuilds solely from PostgreSQL.

## Task Commits

1. Task 1 — `a21b3cf6d728f82fb2d6827615179151d8cbb30d`: authority schema/service/worker, real mTLS adapters and process tests.
2. Validation evidence — `dedcb798781433531987b98678b238710e4d43ad`: exact-source clean Docker/race and native focused raw evidence.

The measured actuals use `ceil(chars/4)` over the realized diff from the persisted pre-plan ledger to `plan_head_after`; commit count is `git rev-list --count` over those exact bounds. Planning metadata commits follow that measurement.

## Verification

The canonical evidence directory is `artifacts/phase6/06-01/a21b3cf6d728f82fb2d6827615179151d8cbb30d/docker/`; `manifest.json` binds raw evidence by SHA-256.

- `PHASE6_EXPECTED_SHA=a21b3cf6d728f82fb2d6827615179151d8cbb30d ./tools/phase6/docker_check.sh`: passed using a clean committed clone before and after testing. Both binaries report the same revision and `vcs.modified=false`.
- Container checks: exact Go 1.27.3 / local toolchain, checksum-verified sqlc v1.31.1, independent double generation, committed generated-source comparison, external-module verification, explicit Goose migration, `go test ./... -count=1 -v`, then real child binaries rebuilt with `-race` and `go test -race ./... -count=1 -v` under `GORACE=halt_on_error=1`: passed.
- Native task command with explicit Go 1.27.3 GOROOT/PATH, Compose database URL/Valkey endpoint, sqlc generation and `go test ./... -run 'TestTracerDesiredStateReceipt|TestValkeyLossRebuild' -count=1`: passed.
- Pinned service runtime queries report PostgreSQL 18.6 and Valkey 9.1.3. Docker host: OrbStack aarch64, 10 CPUs, 8,392,671,232 bytes memory; these results do not establish dedicated-host performance or amd64 execution.
- All 24 compiled external modules match the pre-existing dependency closure review. Three additional graph-only, uncompiled dependencies are present in the full module graph at other versions; full engineering/license graph enforcement remains 06-08/06-09.
- Caller checkout cleanliness: **failed**, because the existing Phase 4/5 modifications and untracked files were preserved. Caller status is retained separately; it was not represented as the clean tested source.
- GitHub Actions: **not run**; pairing and final phase evidence remain 06-09/06-11. No same-SHA Docker/Actions acceptance is claimed.

## Decisions Made

The orchestrator explicitly approved preserving control 1.3.0: NodeControl carries the Agent RuntimeConfigUpdate, while HubControl proves only authenticated persistent heartbeat/status. Its frozen ledger kinds do not include allocation ACKs; no extra wire kind or runtime payload hidden in an allocation was introduced. Hub remains blocked with signer epoch zero. Financial allocations, restrictions and handoff remain the later authority plans' responsibility.

## Deviations from Plan

1. **[Rule 3 — Missing integration primitive]** Added `hub/backend_client.go`, a separate peer command and explicit migration/clean-export/generator helpers because the repository had no production HubControl client or backend module. The Hub slice is intentionally liveness-only and rejects unsupported bodies; it does not impersonate the Phase 5 test Hub as production financial authority. Approved by the orchestrator during execution; committed with Task 1.
2. **[Rule 1 — Transport/business identity]** Initial process duplicate delivery exposed Agent's strict per-stream message-ID uniqueness. The adapter now generates a new transport message UUID per delivery while retaining the operation UUID as transaction ID. Normal and process-race tests confirm durable effect count remains one; committed with Task 1.
3. **[Rule 3 — Toolchain environment]** Host PATH selected a downloaded Go 1.27.3 binary while inherited GOROOT still pointed to Go 1.26.4. Native verification explicitly set both GOROOT and PATH; Docker uses its fixed Go image and `GOTOOLCHAIN=local`. Local replacement modules have no downloadable ziphash, so external `go mod verify` runs on a temporary graph without those two local requires; exact committed-source checks cover the local modules.
4. **[Scope clarification]** Source balances are introduced by 06-05, so current cache-loss evidence covers only actually implemented PostgreSQL authority. It does not claim balance invariants over nonexistent balances. The extension is recorded in the cross-phase windows ledger and must be completed when real source-ledger producers exist.

## Deferred Issues and Evidence Boundaries

- Extend cache loss/rebuild checks to real source balances/ledger ownership after 06-05 introduces them; keep the ledger entry open until that evidence exists.
- The Agent tracer's empty listeners/rules configuration proves durable control application using the real Engine transaction manager. It does not prove funded runtime activation, payload forwarding, the future rule compiler, or 1,000-rule reconciliation.
- Hub allocation/restriction/handoff and complete financial authority policy remain 06-05/06-06. No LedgerRecord encoding or Phase 5 contract was changed.
- Existing Phase 5 verification status `passed_with_scope` is rejected by the SDK's closed status enum. Advance/metric/decision/session handlers failed for that reason; progress skipped the unscoped phase. The original Phase 5 result was preserved and only Phase 6 position/progress/metric/decision/session were synchronized narrowly. Roadmap and requirement handlers succeeded.
- User-role policy and public REST/SSE are later plans. The administration CLI does not claim to replace Bearer authentication or fixed-role authorization.

## Known Stubs

None preventing this scoped tracer. Empty listeners/rules are the intentional no-business-traffic test input, not a mock adapter or unwired product UI.

## Next Plan Readiness

06-08 can append migration 00002 to the real seeded 00001 database and extend tenant policy/AuditWriter/cache validation. Do not rewrite the applied 00001 migration. Phase 6 is 1/11 executed; final phase acceptance remains pending.

## Self-Check: PASSED

The summary, authority migration/service, Hub client and Docker verification entry point exist. Both recorded source/evidence commits are ancestors of the current HEAD; no task-owned source changes remain uncommitted.
