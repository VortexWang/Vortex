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
  - Seeded migration preservation and PostgreSQL-only Valkey reconstruction
  - Exact CR-003 and CR-004 approved generator exceptions with fail-closed source/artifact/use matching
  - Passing full generator/toolchain gate and same-source Docker normal/race regression evidence
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
    - tools/phase6/test_sqlc_license_check.py
    - artifacts/phase6/06-08/verification.json
key-decisions:
  - Preserve applied 00001 and prove seeded authority survives additive 00002.
  - CR-003 A owner response 按推荐 authorizes only the exact mysql module/version/sum in three unchanged official development/CI sqlc artifacts.
  - CR-004 A owner response a separately authorizes the exact MIT-0 allocator and Bison output-exception applicability in those same artifacts, with source/asset hashes; no global SPDX expansion or runtime/distribution authorization.
  - Reconstruct actual linked runtime sources instead of assuming SDK root licensing or inventing unavailable historical Actions metadata.
requirements-completed: [TEN-001, TEN-003, TEN-004, TEN-005, TEN-006, TEN-007, TEN-008, BE-004]
coverage:
  - id: tenant-policy
    description: Fixed-role scoped repository access, ownership grants and same-transaction audit
    requirement: BE-004
    verification:
      - kind: integration
        ref: backend/internal/service/tenant_test.go#TestTenantRepositoryScopeAndPlatformAudit
        status: pass
      - kind: unit
        ref: backend/internal/policy/policy_test.go#TestTenantRoleScopeMatrix
        status: pass
    human_judgment: false
  - id: migration-restrictions
    description: Additive upgrade preserves authority and independent immutable restrictions
    verification:
      - kind: integration
        ref: backend/internal/service/tenant_test.go#TestMigrationUpgradeTenantAuthority
        status: pass
      - kind: integration
        ref: backend/internal/service/tenant_test.go#TestTenantRestrictionsIndependentAndImmutable
        status: pass
    human_judgment: false
  - id: cache-authority
    description: PostgreSQL reconstructs all currently produced authority after real Valkey loss
    verification:
      - kind: integration
        ref: backend/internal/service/authority_rebuild_test.go#TestValkeyLossRebuildAllProducedAuthority
        status: pass
    human_judgment: false
  - id: generator-gate
    description: Reproducible sqlc output and exact approved module/embedded-source closure
    verification:
      - kind: integration
        ref: artifacts/phase6/06-08/7d43e1d2/license/result.json
        status: pass
      - kind: integration
        ref: artifacts/phase6/06-08/7d43e1d2/license/negative-closure-checks.json
        status: pass
    human_judgment: false
actuals:
  tokens: 875834
  tasks: 2
  commits: 12
plan_head_before: ec70d255dc9da7a1b7546dcfa4cd053c715c664a
plan_head_after: b1a92d725a634cbb00a2ebfe06bf83bf227d792f
duration: 254min wall time including approval checkpoints
started: 2026-11-06T19:16:32Z
final_resumed: 2026-11-06T23:24:32Z
completed: 2026-11-06
last_updated: 2026-11-06T23:30:00Z
status: complete
---

# Phase 6 Plan 8: Tenant authority and generator closure Summary

**Tenant scopes, immutable transactional restrictions, additive migration and Valkey reconstruction pass with a checksum-bound, owner-approved sqlc license gate.**

Both tasks are complete. Phase 6 advances to **2/11 plans executed**, with 06-09 next; final Phase 6 acceptance remains pending.

## Accomplishments

Five fixed roles and sealed action scopes deny foreign tenant reads/mutations at service and repository layers. Platform sensitive reads require same-transaction audit; the cache path cannot bypass that audit. Private ownership cannot become a cross-tenant grant, including through database constraints.

AuditWriter rolls authority, task, Outbox and restriction writes back together on audit failure. Immutable restriction history and current projections distinguish cause/source/version; replay and historical clears cannot remove newer or different restrictions. Account state remains active/suspended/banned and separate from business restrictions.

Migration 00002 appends after byte-identical applied 00001. Empty-schema and seeded-upgrade tests preserve earlier desired state, operations, tasks, Outbox, audit and receipts. Cache reconstruction uses a PostgreSQL repeatable-read snapshot. Real Valkey FLUSHALL and an established connection cut preserve nonempty authority, ownership, grants, restrictions, confirmed tasks and unknown occupation, then reconstruct identical cache bytes.

The complete generator gate now passes after two independent, narrowly recorded owner approvals. No global license policy, ordinary GPL, modified/rebuilt generator, product runtime or external distribution exception was introduced.

## Task Results and Commits

| Task | Result | Commits |
| --- | --- | --- |
| 1: tenant scope, roles and transaction audit | Complete; real PostgreSQL normal/race and seeded upgrade pass | ec76aa60 RED; 60d85f8b GREEN |
| 2: reproducibility and Valkey loss gate | Complete; reproducibility, exact license closure and cache reconstruction pass | 6a661137 implementation; 82dd410f evidence; eabc8f6d CR-003 implementation; e6382860 recovery evidence; 838c7391 source recovery; 35f4a5cd prior evidence; 7d43e1d2 CR-004 implementation; b1a92d72 final evidence |

ee116058 and 06e309bc preserve the earlier blocked checkpoints. The **12 commits** are measured from the persisted plan ledger through b1a92d72, including those historical checkpoint commits. This completion SUMMARY and subsequent state metadata commits are outside that measured range. Actual tokens are realized diff characters/4 over that same range, including substantial raw source/build evidence, rather than harness usage.

## Final Verification and Source Identity

Final tested source: **7d43e1d26420db23aaa97121d7ba28bdfc8a1beb**, tree **482019f1a5d49ea7243e0f9c500a853a648a4a41**. Evidence commit: **b1a92d725a634cbb00a2ebfe06bf83bf227d792f**. Metadata commits do not become the tested source. The machine-readable index is [verification.json](../../../artifacts/phase6/06-08/verification.json); authoritative paths below are repository-relative.

- **Full dependency gate exit 0** from a clean committed clone: tools/phase6/dependency_lock_check.sh verifies Go 1.27.3, Goose v3.29.0 module sum, official sqlc archive/binary bytes, protoc 36.2 archive checksum/version, 24 backend compiled modules and 42 sqlc linked modules. Two independent clean sqlc generations match each other and committed output byte-for-byte. Evidence: artifacts/phase6/06-08/7d43e1d2/license/dependency-lock.log and dependency-lock.json.
- **All three official generator artifacts pass** exact module/embedded-source closure with CR-003 and both CR-004 matches, no license findings and no provenance gaps. Darwin arm64, Linux amd64 and Linux arm64 bytes/metadata were verified; this is not native amd64 execution. Evidence: license/three-artifacts.json.
- **15 matcher/source comparison tests pass.** Seven additional experiments run the actual closure verifier and reject missing CR-004, pending approval, module-sum drift, source-file hash drift, removal of Bison-exception-2.2, product-runtime use and modified generator bytes. Evidence: license/negative-closure-checks.json; exact runner retained as license/verify-runner.py.
- **Native Linux arm64 OrbStack Docker normal and race backend suites pass**, with 40 passing test/subtest entries and no skips. They include role/scope/transaction-audit tests, independent restrictions, migration upgrade from seeded 00001 and empty schema, real Valkey loss/rebuild and two real backend/Agent/blocked-Hub process runs. Go 1.27.3, PostgreSQL 18.6 and Valkey 9.1.3 versions/image identities are captured. Evidence: artifacts/phase6/06-08/7d43e1d2/docker/.
- Both final clean clones are clean before and after verification. The caller's pre-existing Phase 4/5 dirty/untracked files were preserved. GitHub Actions and native amd64 execution were **not run**; they remain separate later evidence obligations.

The plan's Goose/sqlc prerequisites execute through the already pinned embedded Goose migration command and checksum-verified sqlc installer used by the Docker entrypoint. All plan-level verification scenarios run in the full suites; no missing host CLI is treated as a pass.

## Full Generator Source Review and Exact Approval

All three checksum-locked official sqlc binaries contain the same 42 linked Go modules. Main/module sums, nested licenses and notices are pinned. Complete supplied SQLite and libpg_query Wasm assets were located byte-for-byte in each binary.

The historical WASIX Actions run remained unavailable, including authenticated lookup. Exact OCI manifest/config/labels and the verified sysroot layer enabled object-level recovery: all 1086 non-builtins archive members match reconstructed source in every Wasm section except the compiler distribution's producers string; all 146 builtins members match exact bytes. Relinking the supplied parser recipe selects only 107 libc and 13 compiler-rt objects, all recovered. DWARF identifies the exact SDK/LLVM revision and those runtime sources. No independent byte-for-byte reproduction of the original optimized Wasm or invented historical checkout is claimed.

Selected MIT/BSD/public-domain/Apache base-license source notices remain retained. Unselected artwork, regex declarations, libc++ and unused math implementations are not described as linked code. The original reconstruction is preserved at artifacts/phase6/06-08/eabc8f6/license/source-recovery-final.json and linked evidence.

Owner response **“按推荐”** approved CR-003 A, limited to the exact mysql v1.9.3 MPL-2.0 module/sum and three unchanged official sqlc internal development/CI artifacts. Owner's independent response **“a”** approved **CR-004 A**: the exact ncruces v0.32.0 MIT-0 allocator and exact wasilibs parser expression **GPL-2.0-or-later WITH Bison-exception-2.2** under its reviewed output-exception applicability. Both approvals are recorded in docs/governance/tool-license-exceptions.json, with artifact origin, archive/binary hashes, module/version/sum, source/build/asset file hashes and obligations. Parser-generator skeleton use, deleted/different exceptions, ordinary GPL, unknown licenses, source drift, modified builds, runtime linkage and distribution remain rejected.

## Requirements and Deferred Scope

Completed requirement markers cover the delivered shared authority boundary: TEN-001, TEN-003..008 and BE-004. **TEN-002 remains pending** despite appearing in this plan's frontmatter: rule, usage and consumption domain tables and all of their tenant ownership are introduced by later plans, so this shared scope foundation does not establish their complete coverage. TEN-009 is absent from this plan's requirements list; independent restriction state is tested here, while complete business resume remains later domain/API validation.

Source balances do not yet exist until 06-05. Cache-loss tests cover every currently produced nonempty authority fact and do not present an empty balance set as conservation evidence. **WINDOWS 1 remains open for 06-05/06-11.** Hub remains blocked with signer epoch zero; real process tests cover empty Agent runtime control and Hub liveness, not financially authorized forwarding.

No intentional implementation stubs, skipped required tests or new network/auth endpoints were introduced by the final license change. Tenant schema/access changes implement T-06-23.

## TDD Gate Compliance

ec76aa60 ran TestTenantRestrictionSchema against real PostgreSQL migrated through 00001. Six planned tenant-policy authority tables were absent; the test assertions failed after successful execution, not compilation, fixture setup or discovery. Existing tdd-red.json passed RED_EVIDENCE_OK. GREEN 60d85f8b followed with passing real database tests; no refactor commit was needed. Task 2 was not tdd=true.

## Deviations from Plan

1. **Rule 4 — explicit policy decisions:** full generator inspection discovered MPL and embedded MIT-0/Bison expressions outside the allowlist. Execution paused for CR-003 and CR-004; both exact A proposals were separately approved and implemented. No unknown-license waiver or global policy widening. WINDOWS 2 and 3 are resolved by their respective implemented gates.
2. **Rule 2 — correctness of acceptance records:** keep TEN-002 pending until its later domain producers exist, and preserve the source-balance debt instead of marking unproduced facts tested.
3. **State tooling limitation:** GSD state handlers reject Phase 5's existing passed_with_scope status. Preserve that historical scoped verdict and narrowly synchronize only Phase 6 position, metrics, decisions, session and roadmap after SDK failure.

No new authentication gate occurred in this continuation.

## Next Plan Readiness

06-09 may proceed after this committed completion record and state synchronization. OpenAPI/REST/SSE, financial source authority, signing, native amd64/Actions and final phase/system acceptance remain outside this plan's completion claim.

## Self-Check: PASSED

All 11 key created files exist; all 12 measured commits are ancestors of HEAD. Evaluation-scope resolved this branch's 06-08 commits. Final source SHA/tree match both evidence sets, clone status is clean before/after, real process/cache/upgrade checks passed twice without skips, applied 00001 is unchanged, and the global license policy is unchanged.
