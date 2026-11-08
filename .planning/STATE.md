---
gsd_state_version: 1.0
milestone: V1
milestone_name: milestone
current_phase: 06
current_phase_name: M4 后端业务闭环 (M4 Backend)
current_plan: 06
status: executing
stopped_at: "06-06 Task 3: old-signer FULL closure transport verified; parent/children closure, sustained funding and boot recovery pending"
last_updated: "2026-11-08T06:13:26Z"
last_activity: 2026-11-08
last_activity_desc: "06-06 Task3: production old-signer closure barriers/pages, FULL tombstones and authenticated receipts verified. Native/Linux race, actual mTLS and dual-architecture builds passed. Phase6 stays8/11."
progress:
  total_phases: 6
  completed_phases: 4
  total_plans: 54
  completed_plans: 50
state_head: 704c213c
---

# Project State

## Project Reference

See: .planning/PROJECT.md (updated 2026-11-03)

**Core value:** 在租户隔离、额度与租约安全边界内可靠转发 TCP / UDP 业务流量，并在正常运行及故障恢复中保持计量可信、权利不重复签发、未知负债不被无资金释放。
**Current focus:** Phase 06 — M4 后端业务闭环 (M4 Backend)

## Current Position

Phase: 06 (M4 后端业务闭环 (M4 Backend)) — EXECUTING
Current Plan: 06
Total Plans in Phase: 11
Status: Executing — 06-01, 06-08, 06-09, 06-02, 06-10, 06-03, 06-04 and 06-05 complete (8/11). 06-06 Tasks 1 and 2 complete; Task 3 allocation/runtime authority remains. Final Phase6 acceptance pending.
Last activity: 2026-11-08 — CR-007 remains approved. In06-06 Task3, direct backend identity pin94561be1 and automatic share/funded bootstrap704c213c are committed. Full native/Linux race, actual mTLS and amd64/arm64 backend compilation passed; clean704c213c Docker passed with clean source before/after (existing through06-05 entry). Old signer renewal closure now has production FULL transport and paging with native/Linux race, actual mTLS loss/replay and builds passed. Parent/children closure, sustained funding and boot recovery remain; final Actions06-11.

Progress: Phase 6 [████████░░░] 8/11 plans executed. Order: 06-01 → 06-08 → 06-09 → 06-02 → 06-10 → 06-03 → 06-04 → 06-05 → 06-06 → 06-07 → 06-11.

Phase 5 remains closed within its runtime/protocol-adapter/static-destination scope on deb8b08 with same-SHA Docker/Actions 42224211933. Phase 4 qualification/D-04 and later-phase acceptance remain separate; Phase 6 planning does not mark those debts passed.

## Performance Metrics

**Velocity:**

- Total plans completed: 13
- Average duration: -
- Total execution time: -

**By Phase:**

| Phase | Plans | Total | Avg/Plan |
|-------|-------|-------|----------|
| 03 | 6 | - | - |
| 02 | 6 | - | - |

**Recent Trend:**

- Last 5 plans: -
- Trend: Not enough data

**Per-Plan Metrics:**

| Plan | Duration | Tasks | Files |
|------|----------|-------|-------|
| Phase 05 P03 | multi-session | 4 tasks | 61 files (source/governance; evidence excluded) |
| Phase 06 P01 | 29min | 1 task | 26 files (source/tooling; 24 evidence files excluded) |
| Phase 06 P08 | 254min wall including approval checkpoints | 2 tasks | 195 changed files including source/governance/planning and raw evidence |
| Phase 06 P09 | at least 30min wall since first task commit, including authentication continuation | 2 tasks | 157 changed files before summary/state metadata, including raw evidence |
| Phase 06 P02 | 32min | 2 tasks | 63 files before summary/state metadata, including raw evidence |
| Phase 06 P10 | 124min multi-session | 2 tasks | 107 files before summary/state metadata, including raw evidence |
| Phase 06 P03 | 378min multi-session | 3 tasks | 199 files before summary/state metadata, including raw evidence |
| Phase 06 P04 | multi-session including owner approval | 3 tasks | 232 files before summary/state metadata, including raw evidence |

| Phase 06 P05 | multi-session including exact approval | 3 tasks | 241 files before summary/state metadata, including raw evidence |

## Accumulated Context

### Decisions

- 06-05: user explicitly approved exact CR-0073ddd1ab7. DB/API2.2 and control1.5 frozen with prior approvals immutable. Unknown funding/compensation and first-report provenance survive restart/lost receipts. Clean source84821a04 Docker passed; next06-06, final Actions06-11.

- 06-04: user explicitly approved CR-006 exact proposal 60487b773b60f9b1bfee80811e49c17d43b910b7ff19c68025a33c9c5e83b28d; DB/API2.1 frozen with CR005 history unchanged. Confirmation consumes code atomically; future failure does not refund. Offline/revoked expiry persists authoritative limits/source expiry while actual stop stays pending and unknown occupation is retained. Source9db5da0e local producer passed; next06-05, final Actions06-11.

- 06-03: owner-approved CR-005 exact proposal b02a73a9d3d4d8506cfd79904efa6a5ebc430cd4b06d729a37b518e19d582bb4 freezes database/API2.0, control1.4 and runtime1.2. Separate negotiated phased commands preserve legacy auto-apply. Missing immutable billing input stays pending; obsolete observations cannot create fresh Commit work. Source fc63d574 passed local Docker; final pairing remains 06-11.

- 06-10 binds one immutable normalized entry domain to its owner/group and verified zone. Platform DNS aggregates every active tenant's committed rules; live per-member health is outside the configuration digest. Appended 00004 entry domains; future rules/packages/ledger/shares/events references are 00005..00009. Real Cloudflare CRUD passed on clean source de6f14d6; reviewed policy metadata remains an explicit deployment attestation, not independent live-policy introspection. No token was uploaded to GitHub; final Actions pairing remains 06-11. SDK state handler still rejects historical passed_with_scope, so Phase 6 state alone was synchronized manually.

- 06-02 preserves platform ownership and uses typed domain tasks plus node-CA/boot/generation/task/lease/revision-bound proofs. NOD-012 and PRT-004 remain pending for compiler and real Agent task consumers in 06-03/07. DNS CRUD remains 06-10; final same-SHA Actions pairing remains 06-11. SDK state writes rejected historical Phase 5 passed_with_scope, so only current Phase 6 position/metrics/session were synchronized manually without changing that historical verification.

- 06-09 uses one OpenAPI source and a deterministic backend copy with dependency-free Ruby lint; Phase 7 owns TypeScript generation. All 96 requirements have primary plan owners, but only implemented producers are required through 06-09. Docker producer success is separate from the mandatory paired gate. Last-cause recovery reissues only authorized persisted desired state and preserves pending/unknown occupation in the same audit/task/Outbox transaction.

- 06-09 accepted source a9481584 with actual Actions 42316152923 and checksum-bound same-SHA Docker pairing. The reviewed dependency union stays identical across Linux and Darwin producers without widening licenses; scoped container Git trust retains exact VCS stamping. Next dependency is 06-02.

- 06-08 preserves applied 00001 and adds immutable tenant restrictions in 00002. Owner approved CR-003 A with 按推荐 and independently approved CR-004 A with a. Exact official artifact/usage/module/source/asset matching now passes all three sqlc artifacts without widening allowed_spdx or runtime/distribution rights. Full 42-module and embedded-source review reconstructs all 120 selected libc/runtime objects. TEN-009 completed in 06-09; TEN-002 and future source-balance validation remain pending for later domain plans/06-11.

- 06-01 uses real Agent NodeControl receipts; HubControl remains authenticated persistent liveness, blocked with signer epoch zero. Financial authority remains 06-05/06-06. Clean committed-clone Docker evidence remains separate from the dirty caller checkout; 06-09 now adds actual same-source Actions for the implemented scope.

- 05-04 owner approved authorization 1.1.0 exact proposal 185541ee09e8dfbb0333a6249e0571c805b182220c77e7c2a79976d45efa4b6e. Signer S/R transfer preserves path generation and prior grant/debt boundaries; production backend remains Phase 6.

- 2026-11-05 owner decision: Phase 4 qualification and optimization continue on gsd/phase-4-optimization-closure; main proceeds with Phase 5. Historical Phase 4 gaps do not block main progression or automatically reopen Phase 4. Final acceptance findings remain truthful.

- V1.11 is a product reference, not a frozen acceptance baseline; current owner decisions govern.
- The user approved Vegeta in place of k6; CR-001 records the tooling and acceptance impact without altering the frozen source.
- The roadmap follows engineering prerequisites, then M0 through M6 in order; dependencies are reviewed just before the phase that introduces them.
- NFR-038 historical recovery is out of scope after project restart. The eleven original register rows remain `missing`; this is not a claim of recovery or verification.
- PostgreSQL and Valkey are external data services. Their image package licenses are excluded from the product dependency gate; versions, provenance, and image digests remain tracked.
- OrbStack is running; local Compose and Phase 4 Docker protocol checks passed on b71a9d0. `gh auth status` confirms VortexWang is the active GitHub account with repo/workflow scopes. Prior same-commit Actions evidence exists for Phase 1; current governance changes still need CI evidence.

### Pending Todos

- Phase 4 implementation gap plans are executed. Keep NFR-003/005/006/007/039/040/044 and D-04 open until eligible native qualification and complete authority evidence exist; durable production authorization/accounting remains a later Agent/Hub integration concern. This is carried forward while Phase 5 proceeds.

### Blockers/Concerns

- CR-007 approval gate is resolved: exact owner response, three frozen contracts and approved clean source84821a04 full local evidence. Phase6 is8/11; signer/share/lease production06-06, HTTP06-07 and final same-SHA Actions06-11 remain.
- SDK advance/metric/session still rejects historical Phase5 passed_with_scope. Preserve that verdict; after actual SDK attempts, only current Phase6 metadata was synchronized narrowly. SDK roadmap/requirements succeeded; 50 summaries of54 plans now exist.
- Prior CR005/006, license and authentication gates are resolved; immutable proposals, failed attempts and exact evidence remain in each plan SUMMARY/CHECKPOINT. Separate historical VLink frozen-hash mismatch remains deferred.
- WINDOWS #1 source-balance/Valkey rebuild now has06-05 implementation evidence; final cross-domain and paired acceptance remain06-11. TEN-002 needs remaining domain coverage. Phase4 dedicated-host performance/D-04 and formal NFRs remain open; no qualification is implied by finite regressions.

## Deferred Items

| Category | Item | Status | Deferred At | Milestone |
|----------|------|--------|-------------|-----------|
| Product scope | Online payment, team invitations, complete VTP camouflage, QUIC and other deferred items | Deferred by project scope decisions | Initialization | V1 |

## Quick Tasks Completed

| # | Description | Date | Commit | Status | Directory |
|---|-------------|------|--------|--------|-----------|
| 261103-l7e | Project restart governance clarified; Phase 1 complete for M0 entry | 2026-11-03 | 62f4687 | complete | [261103-l7e-project-restart-governance](./quick/261103-l7e-project-restart-governance/) |
| 261103-lt9 | Restart governance aligned; M0 TCP relay tracer implemented | 2026-11-03 | a9b79f1 | complete | [261103-lt9-align-restart-governance-and-enter-m0-im](./quick/261103-lt9-align-restart-governance-and-enter-m0-im/) |
| 261105-9j7 | Native host preflight and formal qualification runbook | 2026-11-05 | pending | complete | [20261105-native-host-preflight](./quick/20261105-native-host-preflight/) |

## Session Continuity

Last session: 2026-11-08T06:13:26Z
Stopped at: 06-06 Task 3: old-signer FULL closure transport verified; parent/children closure, sustained funding and boot recovery pending
Resume file: .planning/phases/06-m4-m4-backend/06-06-CONTINUATION.md

### Historical qualification boundaries

Phase4 D-04 remains candidate; see04-VERIFICATION.md. Earlier credit6a1faf2 was TCP-only; clean d2f21f8 later verified static-snapshot TCP/UDP with bounded grant-result injection and same-SHA Actions41856742608. These increments do not complete hot routes, persistent issuer or formal performance/24h qualification. Phase4 and Phase5 historical verdicts are unchanged.
