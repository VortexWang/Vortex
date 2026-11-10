---
phase: 06-m4-m4-backend
plan: 09
subsystem: tenant-policy-and-evidence
tags: [postgres, restrictions, audit, openapi, same-sha, docker, github-actions]
requires:
  - phase: 06-08
    provides: Immutable restriction events, current projection, tenant scopes and audited PostgreSQL authority
provides:
  - Last-cause recovery issues only authorized desired revisions in one audit/task/Outbox transaction
  - One OpenAPI source, deterministic backend copy and executable structural/security lint
  - Explicit 96-owner register and fail-closed, checksum-bound same-source Docker/Actions evidence
  - Real normal/race backend, Agent and blocked-Hub evidence on Linux arm64 and amd64
affects: [06-02, 06-10, 06-03, 06-04, 06-05, 06-11, phase-07]
tech-stack:
  added: []
  patterns: [all-cause restriction projection, deterministic contract copy, exact-source paired evidence, host-independent reviewed dependency union]
key-files:
  created:
    - backend/openapi/v1/README.md
    - tools/phase6/openapi_validate.rb
    - tools/phase6/test_dependency_lock_check.py
    - artifacts/phase6/06-09/a9481584/run.json
  modified:
    - contracts/openapi/v1/openapi.yaml
    - backend/openapi/v1/openapi.yaml
    - backend/internal/service/tenant.go
    - backend/internal/service/tenant_test.go
    - tools/phase6/check.sh
    - tools/phase6/evidence_manifest.sh
    - tools/phase6/dependency_lock_check.py
    - tools/phase6/dependency_lock_check.sh
    - tools/phase6/container_tracer.sh
    - .github/workflows/phase6.yml
    - docs/coverage/phase6/COVERAGE.md
key-decisions:
  - OpenAPI has one source and no Phase 6 code generator; Phase 7 owns TypeScript generation.
  - A producer-only pass does not satisfy acceptance; Docker and actual Actions must pass on the same exact clean source commit.
  - Keep the reviewed Linux amd64/arm64 and Darwin arm64 dependency union identical on all producer hosts without changing module/license bytes or approved exception scope.
  - Trust only the current source directory inside the ephemeral build container, verify its expected SHA, and retain Go VCS stamping.
requirements-completed: [TEN-009]
coverage:
  - id: all-causes-recovery
    description: Clear every active cause before recovering authorized desired state; preserve pending and unknown occupation and account restrictions
    requirement: TEN-009
    verification:
      - kind: integration
        ref: backend/internal/service/tenant_test.go#TestRestrictionAllCauses
        status: pass
      - kind: integration
        ref: backend/internal/service/tenant_test.go#TestRestrictionAuditRollback
        status: pass
    human_judgment: false
  - id: contract-and-paired-gate
    description: Single contract ownership, executable lint, clean-source identity and complete paired producer evidence
    verification:
      - kind: integration
        ref: artifacts/phase6/06-09/a9481584/paired-check.log
        status: pass
      - kind: integration
        ref: artifacts/phase6/06-09/a9481584/paired-manifest.log
        status: pass
      - kind: unit
        ref: tools/phase6/test_dependency_lock_check.py#InventoryHostTests
        status: pass
    human_judgment: false
actuals:
  tokens: 128313
  tasks: 2
  commits: 14
plan_head_before: e2b56e51381b60dd8fb451ab7c79b9e729a7456a
plan_head_after: 32698c0de0552646ecaecfde9ee306e2f35005ff
source_commit: a948158446255e2eb2e2a975c435ddbb2f357ecf
actions_run: https://github.com/VortexWang/Vortex/actions/runs/42316152923
duration: at least 30min wall since first task commit, including authentication continuation
first_task_commit_at: 2026-11-06T23:42:56Z
completed: 2026-11-07
status: complete
---

# Phase 6 Plan 9: Restriction recovery and paired engineering gate Summary

**All-cause transactional recovery and deterministic OpenAPI ownership pass the complete Docker/Actions gate on source `a9481584`.**

Both tasks are complete. Phase 6 has **3/11 plans executed**; the next dependency-ordered plan is **06-02**. This summary completes 06-09's implemented scope, while final Phase 6 acceptance remains pending.

## Accomplishments

Clearing one active restriction cannot resume service. The final clear appends its immutable fact and creates recovery desired revisions, operations, tasks and Outbox entries in the same AuditWriter transaction. Current ownership and valid grants are required. Suspended/banned accounts remain restricted, future clear times are rejected, replay cannot issue duplicate recovery, and pending/unknown execution keeps its occupation. Audit failure rolls the clear and all recovery side effects back together. Applied migrations and generated APIs were reused.

`contracts/openapi/v1/openapi.yaml` is the sole public contract. Its backend copy is byte-identical, Ruby lint checks public structure/security/errors/cursors/SSE and excludes runtime/secret surfaces, and Phase 7 owns TypeScript generation. The full 96-requirement register assigns primary owners; the manifest requires only implemented producers through 06-09 and refuses future scope.

Clean HEAD, expected SHA, external evidence placement, explicit filenames, checksums, separate normal/race process reports and Docker/Actions source identity all fail closed. Complete evidence includes actual backend/Agent/blocked-Hub processes, PostgreSQL/Valkey versions, image/host/build metadata, immutable source identity and raw test logs.

## Verification Evidence

- **Accepted source:** `a948158446255e2eb2e2a975c435ddbb2f357ecf`. Later evidence/summary commits are metadata descendants and are not relabeled as this source.
- **Local producer:** native Linux arm64 containers under OrbStack, run by the pinned Darwin Go 1.27.3 producer; `artifacts/phase6/06-09/a9481584/docker/result.json` reports result 0 and source_clean true.
- **Actual Actions:** [42316152923](https://github.com/VortexWang/Vortex/actions/runs/42316152923), ubuntu-24.04/native Linux amd64, completed success. Downloaded artifact `phase6-a948158446255e2eb2e2a975c435ddbb2f357ecf`, raw workflow log and API run/job metadata are retained under `artifacts/phase6/06-09/a9481584/`.
- Both producers passed the exact dependency/license closure, two independent sqlc generations and complete backend normal/race suites. Each tracer log has 50 PASS records and no skipped test, failure or data race. Both have two real-process reports with blocked Hub signer epoch 0, wrong-role rejection and unknown occupation held.
- Both normal binaries retain VCS revision equal to the accepted source and `vcs.modified=false` in `build-metadata.txt`.
- In the clean accepted-source clone, both `check.sh --through-plan 06-09 --expected-sha <source> --evidence-dir <external-pair>` and `evidence_manifest.sh` with the same arguments returned 0 and `manifest passed: 96 owners; paired Docker/Actions`.
- Twelve earlier negative experiments are preserved at `artifacts/phase6/06-09/c1e6b766/negative-gates.json`: wrong SHA, dirty source, unsupported future producer, output inside source, six malformed OpenAPI variants, log tampering and missing actual Actions. Their source remains explicitly `c1e6b766`; the final two producer runs and paired acceptance use `a9481584`.

## TDD Gate Compliance

Task 2 RED commit `42679d2f` ran `TestRestrictionAllCauses` against real PostgreSQL and failed on the intended recovery assertion: operations count 5, expected 7 after the last clear. `artifacts/phase6/06-09/tdd/red.json` records actual execution and the SDK accepted RED_EVIDENCE_OK. GREEN commit `20acf557` implemented the recovery; final normal/race suites pass the target and audit rollback tests. No compile, discovery or fixture error was treated as RED. No separate refactor was needed.

The host-union regression also failed first on Linux missing the reviewed Darwin target, then passed for Linux amd64, Linux arm64 and Darwin arm64. Both final dependency producers ran all 16 license/inventory unit tests successfully.

## Task Commits

| Commit | Task / purpose |
|---|---|
| c065bf69 | Task 1: OpenAPI ownership, lint, 96-owner register and engineering gate |
| 42679d2f | Task 2: genuine PostgreSQL TDD RED |
| 20acf557 | Task 2: audited all-cause recovery GREEN |
| affb9c04 | Task 1: checksum-bound normal/race process evidence |
| 1882e91f | Tasks 1/2: suspended/banned negatives and public-contract hardening |
| c1e6b766 | Task 1: reject source HEAD drift |
| bbbac76d | Retain original clean Docker and failed missing-Actions pairing evidence |
| 14c7383c | Preserve authentication checkpoint and incomplete state |
| fe055a45 | Retain actual first Actions dependency-union failure |
| 3c877d4d | Fix host-dependent reviewed union, with regression test |
| ecc427ff | Retain actual second Actions VCS failure and local success |
| a9481584 | Scope container Git trust and preserve exact VCS stamping |
| d6784e55 | Retain final-source successful local producer |
| 32698c0d | Task 1 acceptance: actual successful Actions artifact and paired checks |

The measured ledger covers these 14 commits before this summary and subsequent state metadata commits. Actual tokens are realized UTF-8 diff characters/4, rounded up, across 157 changed files including raw evidence; they are not harness tokens.

## Deviations from Plan

1. **[Rule 2 - Missing critical verification]** Tightened process filename/checksum indexing, separate normal/race assertions, public secret/runtime exclusions, account-state negatives and before/after source identity checks. Commits `affb9c04`, `1882e91f`, `c1e6b766` make the requested fail-closed gate enforce its actual claims.
2. **[Rule 1 - Bug]** Real Actions `42315479232` on `c1e6b766` failed because the executing-host union omitted two already-reviewed Darwin-only modules on Linux. `3c877d4d` includes the fixed reviewed Darwin target on every host. Exact 24-module/license bytes, 42-module generator review and CR-003/004 exception scope remain unchanged. Failure evidence is retained in `actions-failed-42315479232`.
3. **[Rule 3 - Blocking issue]** Real Actions `42315790129` on `3c877d4d` passed dependency licensing but Go build failed obtaining VCS status across runner/container ownership. `a9481584` trusts only the ephemeral container's current source directory and checks expected SHA. A foreign-owner container experiment reproduced Git exit 128, then verified clean exact VCS identity after scoped trust. VCS stamping remains enabled. Raw failed evidence is retained in `actions-failed-42315790129`.

No architecture, dependency version, license allowance, applied migration or later domain was changed by these fixes.

## Authentication Gate

The original account `nodeox` had pull but no push permission; the actual authorized branch push dry-run returned 403. Work stopped at a recorded authentication checkpoint. The user completed browser login, the continuation verified current account `VortexWang` and repository push/admin permission, and exact source commits were pushed normally to `codex/phase6-backend`. Real on-push Actions then produced the above failures and final successful evidence. No additional authorization remained pending.

## Threat Flags

| Flag | File | Description |
|---|---|---|
| threat_flag: tooling-trust | tools/phase6/container_tracer.sh | Explicitly trust the current source mount inside an ephemeral container for VCS metadata; trust is restricted to that path, source SHA is checked, and final producer/manifest verify clean identity. |

## Remaining Boundaries

There are no goal-blocking stubs or skipped/unrun 06-09 verification steps. Hub remains blocked with signer epoch zero until later financial authority. TEN-002 domain ownership, source-balance/cache-loss debt in WINDOWS item 1 and remaining Phase 6 plans stay pending. Native functional/race evidence does not satisfy dedicated performance, full model, stability, full system or Phase 4 qualification acceptance. Phase 4/5 working changes were left intact.

The SDK's pre-existing rejection of Phase 5 `passed_with_scope` must not be resolved by rewriting that verdict. Phase 6 bookkeeping uses the SDK where supported and narrowly synchronizes its own position/session if rejected.

## Self-Check: PASSED

All seven key output/evidence paths exist, and all 14 recorded commits are ancestors of HEAD. The clean-source paired check and standalone manifest both passed against the downloaded actual Actions artifact and final local Docker result.
