---
phase: 06-m4-m4-backend
plan: 03
subsystem: rules
tags: [postgresql, sqlc, compiler, immutable-revision, mtls, two-phase, engine]
requires:
  - phase: 06-10
    provides: durable serving/domain authority and current node/port snapshots
provides:
  - Immutable tenant-scoped rule revisions with optimistic concurrency and audited recompilation intent
  - Deterministic committed per-node artifacts and production consumption of pinned immutable inputs
  - Fixed-member Prepare/Commit/query/abort with conservative unknown recovery and authenticated socket release
affects: [06-04, 06-05, 06-06, 06-07, 06-11, phase-7]
tech-stack:
  added: []
  patterns: [canonical immutable input/artifact digests, revision-scoped durable delivery, rule-head-first locking, negotiated typed control commands]
key-files:
  created: [backend/db/migrations/00005_rules_compile.sql, backend/db/query/rules.sql, backend/internal/compiler/compiler.go, backend/internal/compiler/model.go, backend/internal/service/rule.go, backend/internal/service/rule_compile.go, backend/internal/service/orchestration.go, backend/internal/worker/rule_worker.go, agent/rule_command.go, engine/runtime/rule/manager.go, docs/coverage/phase6/RULES.md, docs/changes/CR-005-APPROVAL.json]
  modified: [backend/db/generated/rules.sql.go, backend/cmd/vortex-backend/main.go, backend/internal/control/adapter.go, agent/config_validate.go, contracts/database/schema.sql, contracts/openapi/v1/openapi.yaml, backend/openapi/v1/openapi.yaml, contracts/protobuf/v1/vortex.proto, contracts/runtime/config.schema.json, contracts/versions.yaml, docs/governance/contract-review.yaml, tools/phase6/check.sh, tools/phase6/evidence_manifest.sh]
key-decisions:
  - "Owner explicitly approved CR-005 proposal b02a73a9d3d4d8506cfd79904efa6a5ebc430cd4b06d729a37b518e19d582bb4; database/API 2.0.0 breaking and control 1.4.0/runtime 1.2.0 compatible are frozen with exact hashes."
  - "Typed RuleCommand is separately negotiated; legacy RuntimeConfigUpdate retains auto-apply semantics and validates the unchanged legacy schema branch."
  - "Missing immutable billing input leaves compilation pending and retains prior active authority; formal funded snapshot producers remain 06-04/05."
  - "Late obsolete observations retain evidence/holds and may queue query/abort, but cannot create fresh Commit work; locks follow head then deployment then task."
patterns-established:
  - "Persist revision/input/artifact and task/Outbox/audit authority before network delivery."
  - "Unknown execution retains physical occupation until authenticated current-incarnation query/stop proof."
requirements-completed: [RUL-001, RUL-002, RUL-003, RUL-004, RUL-005, RUL-006, RUL-007, RUL-008, RUL-009, RUL-010, RUL-011, RUL-012, RUL-013, RUL-014, RUL-015, RUL-016, BE-002, BE-003]
coverage:
  - id: D1
    description: "Immutable group-based rule revisions validate current authority and reject stale writes without overwriting history"
    requirement: RUL-003
    verification:
      - kind: integration
        ref: backend/internal/service/rule_test.go#TestRuleImmutableRevisionConflictAndTenantScope
        status: pass
      - kind: integration
        ref: backend/internal/service/rule_test.go#TestRuleValidationPathsTargetsAndDirections
        status: pass
    human_judgment: false
  - id: D2
    description: "Canonical pinned inputs reproduce immutable committed per-node artifacts; missing snapshots cannot dispatch"
    requirement: BE-002
    verification:
      - kind: integration
        ref: backend/internal/compiler/compiler_test.go#TestCompileCanonicalDigestReplay
        status: pass
      - kind: integration
        ref: backend/internal/service/rule_compile_test.go#TestCompileWorkerMissingSnapshotThenReadyRestart
        status: pass
    human_judgment: false
  - id: D3
    description: "Actual Agent/Engine sockets enforce fixed-member phased execution, unknown/query/restart, release and obsolete receipt fences"
    requirement: RUL-010
    verification:
      - kind: integration
        ref: backend/internal/service/rule_port_test.go#TestRulePortActualAgentSocketsUnknownAndQuery
        status: pass
      - kind: integration
        ref: backend/internal/service/orchestration_stale_test.go#TestApplySupersededLateObservationCannotQueueCommit
        status: pass
      - kind: integration
        ref: engine/runtime/rule/link_test.go#TestApplyMultihopForwardAndReverseRealTLS
        status: pass
    human_judgment: false
actuals:
  tokens: 411500
  tasks: 3
  commits: 22
  plan_head_before: 0a2da58e15a233d6a4c527514343bac5e90b4749
  plan_head_after: 6b74cc5b1a5f26a2546fed780ab8d5dad1d51920
duration: 378min multi-session
completed: 2026-11-07
status: complete
---

# Phase 6 Plan 3: Immutable Rules and Phased Runtime Summary

**不可变规则修订经确定性编译成为已提交的逐节点 artifact，由真实 Agent/Engine 执行固定成员 Prepare/Commit 与保守 unknown/query/release。**

## Accomplishments and Verification

- Final implementation/governance source is `fc63d57406f9f0e3eebe99ad276f95b004add710`. A clean clone ran `check.sh --through-plan 06-03 --produce docker --expected-sha fc63d57406f9f0e3eebe99ad276f95b004add710`: exit **0**, result=0, source_clean=true, local acceptance_complete=true. This field describes the local producer, not final phase acceptance. Evidence commit: `6b74cc5b`.
- Primary run: `artifacts/phase6/06-03/fc63d57406f9f0e3eebe99ad276f95b004add710/reverification/docker/`. A previously persisted sibling `docker/` has the same exact source and successful result with different process/run logs; both were retained without overwrite. Both sets have checksum-bound source identity and results; the copied reverification manifest passed.
- Pinned Go **1.27.3**, PostgreSQL **18.6**, Valkey **9.1.3**, sqlc **1.31.1**: full backend normal/race produced **212 PASS lines**, contract/Agent/Engine normal/race **1,282 PASS lines**, zero skip/fail/race warnings. Tests include real multiprocess backend/Agent/Hub mTLS, direct and multi-hop TCP/UDP, prepared sockets not forwarding, old sessions on hot updates, current-incarnation query, unknown/restart, fixed/auto port ownership, explicit closed-socket release, and three obsolete observation cases.
- Fresh database applied unchanged migrations **00001..00004** then new **00005**; stored Goose status is retained in `migration-status.log`. No existing applied migration 00001..00004 was rewritten. sqlc and protobuf were each generated twice in clean temporary directories and compared with committed output; OpenAPI canonical/copy lint passed. Four CR-005 frozen checks and exact approved proposal/wire/schema checks passed.
- Linux **amd64/arm64** backend/node build metadata plus Engine package compilation passed. Native execution/race evidence is Linux arm64 Docker only; cross-compilation does not prove native amd64 execution, performance or dedicated-host qualification.
- Exact backend production/test dependency closure and sqlc linked/embedded license closure passed with reviewed CR-003/004 exceptions. This is not a full release-image license/SBOM attestation.
- Real Cloudflare create/read/update/delete passed (**5.20s**): TTL60, DNS-only, identity-bound exact-zone creation-policy attestation, encrypted persistence and exact-name cleanup. Tokens/private policy contents were excluded from evidence and checked for literal leakage. An initial local attempt selected an example policy file and failed before network; final complete rerun used the existing private configured policy by absolute path.
- `docs/coverage/phase6/RULES.md` audits all 18 plan requirements and the three must-haves. Compilation is a pure transform with immutable grant/member/permit/billing bindings. Missing real snapshot inputs remain pending, producing no authority and retaining prior active artifacts. Formal snapshot/source producers remain 06-04/05; public HTTP adapters/impact presentation remain 06-07/Phase 7. No fabricated production funding is claimed.
- Final same-SHA Docker/Actions pairing, 1,000-rule timing and remaining Phase 6 requirements are **pending 06-11**. No new Actions result is claimed. Phase 5 historical acceptance and Phase 4 qualification debt remain unchanged.

## Task Commits

1. **Task 1 — immutable revisions and validation:** RED `da7b5fcc`, GREEN `db552436`.
2. **Task 2 — deterministic committed artifacts:** RED `f84b1e13`, GREEN `1d51ba0c`; binding fix `5cd8fc9b`.
3. **Task 3 — phased runtime and recovery:** `9b75da5a`, `3e6098f0`, `c28a625e`, `983a3a21`, `797b8de4`, `af8d7bad`, `f12a1c51`, `428356aa`; candidate bounds/governance `5c77224b`, `43f88647`; legacy decoder fix `f285137b`; evidence/checkpoint `7e5e9619`, `e3abceef`; obsolete-observation RED `c21acb0e`, GREEN `4e902c49`; actual approval/freeze `fc63d574`; final evidence/audit `6b74cc5b`.

Actuals use chars/4 over the realized committed diff from the on-disk plan ledger, including retained raw evidence; **22 commits** were measured before this summary. This substantially exceeded the initial estimate because proving the real compiler-to-Agent socket boundary required typed negotiated control, process-local runtime integration and recovery/port proof paths.

## TDD Gate Compliance

- Task 1 RED executed on real PostgreSQL and failed the five missing authority-table assertions; Task 2 RED failed the missing committed artifact table assertion. Both compiled/connected successfully, without collection/setup failures. Their original unchanged unittest reports and semantic assessments are under `artifacts/phase6/06-03/red/`; both classifier reruns return **RED_EVIDENCE_OK**, followed by GREEN commits in order.
- Obsolete scheduling RED executed prepare/query-prepared/partial-commit cases and observed 2/2/1 recreated Commit tasks. GREEN `4e902c49` serializes on the current head and disallows fresh obsolete activation work. Latest real normal/race assertions all pass. Its unchanged RED report also returns **RED_EVIDENCE_OK**.
- No plan-level TDD mode was enabled; no separate REFACTOR commit was needed.

## Decisions and Deviations

1. **Owner-approved architectural extension:** Legacy automatic config updates cannot encode prepare-only holds. CR-005 adds separately negotiated RuleCommand/RuleReceipt and a rule-artifact schema branch, with actual owner response **“批准 CR-005”** recorded in `docs/changes/CR-005-APPROVAL.json`. The immutable proposal hash remains `b02a73a9d3d4d8506cfd79904efa6a5ebc430cd4b06d729a37b518e19d582bb4`; no further approval was inferred. Database/API 2.0.0 are breaking relative to historical conceptual 1.0; no historical 1.0 deployment migration is supplied. Existing Phase 6 databases upgrade by additive migration 00005.
2. **[Rule 1 — Bug] Executable legacy schema compatibility:** The new schema wrapper initially caused the old hand-written decoder to reject valid legacy config updates. `f285137b` selects the exact unchanged legacy branch and rejects unsupported branches. Existing mTLS update/idempotency tests and new direct decoder regressions pass normal/race.
3. **[Rule 1 — Bug] Obsolete phase scheduling:** Late observations could create Commit work after supersession. `4e902c49` fixes lock order/current-head refresh and queues only safe query/abort for obsolete work; held occupation and receipt history remain intact.
4. **[Rule 1/2 — Correctness] Port and runtime closure:** Committed artifacts bind real fixed/auto allocations, typed authenticated physical receipts, boot/execution identities, reuse protection and proof-backed socket release. Required actual direct/multi-hop runtime and compile-worker consumers were added instead of treating schema or fixtures as execution evidence.

## Deferred Issues and Known Limits

- All-contract frozen validation still reports the pre-existing **VLink approved-hash mismatch**. Four selected CR-005 checks pass; that historical issue is recorded in `deferred-items.md` and `all-contract-freeze.log`. No VLink record or historical approval was changed.
- No blocking stub remains in the implemented plan boundary. Billing/source funding producers, paid entitlement issuance, final Actions pairing and UI/HTTP presentation retain their explicit later-plan ownership. These boundaries do not authorize empty/mock-funded production execution.
- Existing WINDOWS 6/7 architecture/unrun-verification entries can now be closed with the actual approval and source-bound tests; source balance debt 1 remains open.

## Threat Flags

| Flag | File | Description |
|---|---|---|
| threat_flag: control-extension | contracts/protobuf/v1/vortex.proto | New negotiated phased control crosses the compiler/Agent boundary. CR-005 explicitly approves the exact wire/schema bytes; capability, role, revision, artifact, lease, boot and generation correlation tests enforce the plan's T-06-05/06 mitigations. |

## Self-Check: PASSED

Created source/audit/evidence files exist; all listed task/evidence/approval commits are ancestors of HEAD. Final source clone stayed clean; stored evidence checksums and named normal/race tests passed the copied manifest. Immutable approved proposal bytes and the measured commit ledger were verified before close-out.
