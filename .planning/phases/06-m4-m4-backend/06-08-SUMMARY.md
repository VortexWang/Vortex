---
phase: 06-m4-m4-backend
plan: 08
subsystem: tenant-policy
tags: [tenant-scope, audit, postgres, sqlc, valkey, license-gate]
requires:
  - phase: 06-01
    provides: PostgreSQL authority and real Agent/blocked-Hub tracer
provides:
  - Sealed tenant/action scopes, five fixed roles and transactional restrictions/audit
  - Seeded migration preservation and real PostgreSQL-only Valkey reconstruction
  - Owner-approved CR-003 exact artifact/usage/mysql exception and rejection tests
  - Complete linked-module review and reconstructed selected embedded runtime sources
  - Consolidated exact CR-004 proposal; overall license gate remains blocked
affects: [06-09, 06-02, 06-04, 06-05, 06-11]
tech-stack:
  added: []
  patterns: [tenant-filtered SQL, immutable restriction events, exact-artifact exception, object-level source reconstruction]
key-files:
  created:
    - backend/internal/auth/principal.go
    - backend/internal/policy/policy.go
    - backend/internal/repository/tenant_scope.go
    - backend/internal/service/tenant.go
    - backend/db/migrations/00002_tenant_policy.sql
    - backend/internal/service/authority_rebuild_test.go
    - tools/phase6/sqlc_license_check.py
    - tools/phase6/sqlc-dependencies.json
    - tools/phase6/compare_wasm_archive.py
    - docs/governance/tool-license-exceptions.json
    - docs/governance/proposals/CR-004-sqlc-embedded-license-review.md
  modified:
    - backend/internal/service/cache.go
    - backend/db/migrate.go
    - contracts/database/schema.sql
    - tools/phase6/dependency_lock_check.py
key-decisions:
  - Preserve applied 00001 and prove seeded authority survives additive 00002.
  - Owner selected CR-003 A with 按推荐; authorization is only the three unchanged official sqlc artifacts, development/CI usage, and exact mysql module/version/sum.
  - Reconstruct actual linked runtime sources rather than treating an unavailable historical run or SDK root MIT label as closure evidence.
  - CR-003 does not authorize MIT-0 or the Bison output-exception expression; CR-004 remains pending.
requirements-completed: []
actuals:
  tokens: 853354
  tasks: 1
  commits: 9
plan_head_before: ec70d255dc9da7a1b7546dcfa4cd053c715c664a
plan_head_after: 35f4a5cdc3a47f8cb0bf926c9e4cb7aaba773a43
duration: 63min active across initial execution and continuation
started: 2026-11-06T19:16:32Z
resumed: 2026-11-06T22:33:53Z
completed: null
last_updated: 2026-11-06T23:14:50Z
status: blocked
checkpoint:
  task: 2
  type: decision
  gate: blocking-human
  reason: CR-004 exact embedded MIT-0 and Bison output-exception decision pending
---

# Phase 6 Plan 8: Tenant authority and generator closure Summary

**Tenant isolation, immutable transactional restrictions and Valkey reconstruction pass; CR-003 is implemented, while the complete generator gate rejects only two precise CR-004 expressions.**

This is a checkpoint record: **1/2 tasks complete**. No requirement or Phase 6 completion is inferred. Phase 6 remains 1/11.

## Task Results and Commits

| Task | Result | Commits |
| --- | --- | --- |
| 1: tenant scope, roles and transaction audit | Complete; PostgreSQL normal/race and seeded upgrade pass | ec76aa6 RED; 60d85f8 GREEN |
| 2: reproducibility and Valkey loss gate | Application/reproducibility pass; overall license verification blocked | 6a66113 implementation; 82dd410 evidence; eabc8f6d CR-003 implementation; e6382860 recovery evidence; 838c7391 full source recovery/proposal; 35f4a5cd final evidence |

ee116058 is the earlier checkpoint metadata commit. The measured nine commits include it; this replacement checkpoint metadata commit is outside the measured range. Actual tokens use realized diff characters/4, including substantial raw source/build evidence, rather than harness usage.

## Delivered Behavior

Five fixed roles and sealed action scopes deny foreign tenant reads/mutations at service and repository layers. Platform sensitive reads require same-transaction audit; an ordinary tenant cache path cannot bypass platform read audit. Private ownership cannot become a cross-tenant grant, including through database constraints.

AuditWriter rolls authority, task, Outbox and restriction changes back together on audit failure. Immutable restriction history and current projections separate cause/source/version; replay or historical clears cannot remove a newer or different restriction. Account state stays separate.

Migration 00002 appends after byte-identical applied 00001. Empty-schema and seeded-upgrade tests preserve prior desired state, operations, tasks, Outbox, audit and receipts. Rebuild uses PostgreSQL repeatable-read snapshots. Actual Valkey FLUSHALL and an established connection cut leave nonempty authority, ownership, grants, restrictions, confirmed tasks and unknown occupation unchanged; PostgreSQL restores the same cache bytes.

## Final Verification and Source Identity

Exact source **838c7391** is identified by the full SHA in artifacts/phase6/06-08/verification.json and source-identity.txt; use those authoritative full values. Both final Docker and full dependency gate ran from clean committed clones of **838c7391**.

- Native Linux arm64 OrbStack Docker, Go 1.27.3, PostgreSQL 18.6, Valkey 9.1.3: complete backend normal and race suites pass, including two passed real backend/Agent/blocked-Hub process runs. No skips; source clean before and after. Raw evidence: artifacts/phase6/06-08/838c7391/docker/.
- Full dependency gate exits **1**, correctly matching approved CR-003 and rejecting only MIT-0 and GPL-2.0-or-later WITH Bison-exception-2.2. No unresolved linked-source gaps. Raw clean-clone report/log and three-artifact review: artifacts/phase6/06-08/838c7391/license/.
- sqlc v1.31.1 two independent clean generations match each other and committed output. Exact Go, Goose module, sqlc archive/binary and protoc archive/version checks pass. Goose uses the existing pinned embedded migration command. The 24-module backend closure is separately verified; protoc coverage here is its pinned checksum/version, not a new full release-image audit.
- Eight focused tests prove exact artifact/origin/archive/binary/usage/dependency matching, rejection of other licenses, code/section drift rejection, and preservation of duplicate archive members. Unavailable PostgreSQL causes the cache integration test to fail rather than skip.
- GitHub Actions and native amd64 execution were not run. Three-platform artifact metadata/source checks are not native amd64 execution. Caller Phase 4/5 dirty work is preserved; caller cleanliness is not claimed.

## Full Generator Review and Remaining Decision

All three locked official sqlc binaries have the same 42 linked Go modules. Main/module sums, nested licenses and notices are pinned. Complete SQLite and libpg_query Wasm assets were located byte-for-byte in each binary.

The historical WASIX Actions run was unavailable even with authentication. Recovery continued through exact OCI manifest/config/labels and the verified sysroot layer. All 1086 non-builtins archive members match reconstructed source in every Wasm section except the compiler distribution's producers string; all 146 builtins members match exact bytes. Relinking the supplied parser recipe selects only 107 libc and 13 compiler-rt objects, all recovered; DWARF identifies the exact SDK/LLVM commit and those 13 runtime sources. The full optimized upstream Wasm was not independently reproduced.

Actual selected MIT/BSD/public-domain/Apache base-license sources and their notices are retained. Unselected gopher artwork, regex declarations, libc++ and unused math implementations are not presented as linked code. Source proofs and limitations are in CR-004 and artifacts/phase6/06-08/eabc8f6/license/source-recovery-final.json.

Owner response **“按推荐”** approved CR-003 A only. The gate binds that authorization to the three unchanged official artifacts, exact developer/CI usage and mysql v1.9.3 sum; it never broadens allowed_spdx or product linkage. The remaining CR-004 proposal consolidates the exact ncruces MIT-0 allocator and specific Bison output-exception applicability, with hashes, obligations and A/B/C options. Recommended A is still **pending**. No unknown-license waiver is requested.

## TDD Gate Compliance

ec76aa6 ran TestTenantRestrictionSchema against real PostgreSQL migrated through 00001. Six planned tenant-policy authority tables were absent; assertions failed after successful test execution, not compilation/fixture/discovery. Existing tdd-red.json passed RED_EVIDENCE_OK. GREEN 60d85f8 followed and passed real database tests; no refactor commit was needed. Task 2 was not marked tdd=true.

## Deviations and Deferred Boundaries

1. **Rule 4 — explicit policy decisions:** CR-003 A is approved and implemented; newly discovered embedded expressions are consolidated in CR-004 and remain rejected. WINDOWS 2 is fixed; WINDOWS 3 remains open.
2. **Existing scope:** real source balances do not exist until 06-05. Tests cover all currently produced nonempty authority facts, without presenting an empty balance set as conservation. WINDOWS 1 remains open for 06-05/06-11.
3. **State tooling limitation:** the SDK still rejects Phase 5's existing passed_with_scope verdict. Preserve that historical verdict and narrowly synchronize only Phase 6 checkpoint state/roadmap. No plan counter or requirements advance while this plan is incomplete.

No intentional implementation stubs or skipped required tests were added. No new network/auth endpoint was introduced; tenant schema/access changes implement T-06-23. Hub stays blocked with signer epoch zero; this slice does not grant financial signing authority.

## Resume

Obtain one explicit A/B/C decision on docs/governance/proposals/CR-004-sqlc-embedded-license-review.md. If A, implement only that exact supplemental boundary, rerun the complete gate and required checks, then replace this checkpoint with truthful completion metadata. Do not start dependent plans while Task 2 is blocked.

## Self-Check: PASSED

Critical implementation, approval, proposal and raw evidence files exist; recorded task/evidence commits are ancestors of HEAD. Final source identity, clean clones, two process passes and no skips were checked. Applied 00001 is unchanged. This verifies the checkpoint record, not a passing license gate.
