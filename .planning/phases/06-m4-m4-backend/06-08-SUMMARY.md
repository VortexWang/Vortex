---
phase: 06-m4-m4-backend
plan: 08
subsystem: tenant-policy
tags: [tenant-scope, authorization, audit, postgres, goose, sqlc, valkey, license-gate]
requires:
  - phase: 06-01
    provides: PostgreSQL desired state, operations, tasks, Outbox, receipts and real mTLS tracer
provides:
  - Sealed tenant/action scopes, five fixed roles and explicit repository predicates
  - Append-only 00002 tenant policy migration with resource ownership/grants and immutable restriction history
  - Transaction AuditWriter and independent restriction apply/clear projections
  - Real nonempty PostgreSQL authority/ownership/task preservation under Valkey flush and established connection loss
  - Fail-closed generator license finding and exact-artifact CR-003 proposal awaiting owner decision
affects: [06-09, 06-02, 06-04, 06-05, 06-11]
tech-stack:
  added: []
  patterns: [action capabilities, tenant-filtered generated SQL, immutable event and current projection, repeatable-read cache rebuild, exact-artifact license gate]
key-files:
  created:
    - backend/internal/auth/principal.go
    - backend/internal/policy/policy.go
    - backend/internal/repository/tenant_scope.go
    - backend/internal/service/tenant.go
    - backend/db/migrations/00002_tenant_policy.sql
    - backend/db/query/tenants.sql
    - backend/db/generated/tenants.sql.go
    - backend/internal/testutil/valkey.go
    - backend/internal/service/authority_rebuild_test.go
    - tools/phase6/dependency_lock_check.sh
    - tools/phase6/backend-dependencies.json
    - docs/governance/proposals/CR-003-sqlc-generator-license-exception.md
  modified:
    - backend/internal/service/tracer.go
    - backend/internal/service/cache.go
    - backend/db/migrate.go
    - contracts/database/schema.sql
key-decisions:
  - Preserve 00001 unchanged; prove seeded authority survives 00002 with real PostgreSQL and immutable restriction history.
  - Ordinary tenant cache maintenance cannot accept platform principals and bypass their mandatory read audit.
  - A known MPL-2.0 component linked into the approved sqlc artifact blocks the complete dependency gate; backend-only license closure is insufficient.
requirements-completed: []
actuals:
  tokens: 43943
  tasks: 1
  commits: 4
plan_head_before: ec70d255dc9da7a1b7546dcfa4cd053c715c664a
plan_head_after: 82dd410fd3249b7a2e6a741091fde547801ce5b7
duration: 19min
started: 2026-11-06T19:16:32Z
completed: null
last_updated: 2026-11-06T19:35:05Z
status: blocked
checkpoint:
  task: 2
  type: decision
  gate: blocking-human
  reason: Official sqlc v1.31.1 links non-allowlisted MPL-2.0 mysql driver; CR-003 awaits owner decision
---

# Phase 6 Plan 8: Tenant policy, transactional restrictions and cache rebuild Summary

**Real PostgreSQL tenant/action isolation and immutable apply/clear restrictions pass, while the exact sqlc generator's linked MPL-2.0 dependency correctly blocks the remaining license gate.**

This is an incomplete-plan checkpoint record: **1/2 tasks complete**. Task 2 implementation and independent checks are committed for review, but its overall verification fails. No requirement or Phase 6 completion is inferred from this record.

## Task Results and Commits

| Task | Result | Commits |
| --- | --- | --- |
| 1: tenant scope, roles and transaction-scoped audit | Complete: real PG normal/race and seeded 00001 upgrade passed | `ec76aa6` RED; `60d85f8` GREEN |
| 2: sqlc/toolchain reproducibility and Valkey loss gate | Incomplete: rebuild and generator/checksum checks passed; generator license closure blocked | `6a66113` independent implementation/proposal; `82dd410` evidence |

`6a661137160000a9401920744170f07560353832` is the exact application/tooling source verified by Docker. The evidence commit adds records only. The final checkpoint documentation commit is outside the measured four commits above, matching the persisted plan ledger.

## Delivered Behavior

- Principal separates user and tenant identity; platform roles have no home tenant. Active/suspended/banned states and five fixed roles reject invalid or foreign capabilities. Service/repository checks cover platform_admin, operator, auditor, owner and member, and a read capability cannot mutate the tracer.
- Scoped generated queries filter tenant rows. Platform operation/tenant/restriction/resource reads require same-transaction audit context. Private resource ownership cannot be converted to a cross-tenant grant, even through the database foreign-key constraint. Expired grants grant no visibility.
- `AuditWriter.WithTransaction` commits callback authority writes and audit together. A PostgreSQL JSON audit failure rolls back seeded desired state, operation, task, Outbox and restriction event/projection changes together. Existing receipts remain identical. Later domain plans still prove their own concrete mutation rollback.
- The immutable `restriction_events` table and `current_restrictions` projection use tenant/cause/source/version identity. Clear replay and stale historical clear cannot remove a newer restriction. Clearing one cause leaves the other active; account state stays separate. History UPDATE/DELETE is rejected.
- Appended migration 00002 preserves the original applied 00001 bytes. Empty-schema and seeded-00001 tests preserve all prior desired_state/task/outbox/audit/receipt records. The schema contract now mirrors implemented ordered migrations rather than obsolete future draft tables.
- Rebuild reads all current projection producers in a PostgreSQL repeatable-read transaction. Nonempty users, private/platform resource ownership, grants, restrictions, confirmed operations/tasks and unknown occupation survive actual Valkey FLUSHALL and a cut established connection. PostgreSQL reconstruction restores the same cache bytes without authority writes.

## Verification Evidence

`artifacts/phase6/06-08/verification.json` records the overall **blocked** result. Clean committed clone evidence is under `artifacts/phase6/06-08/6a66113/docker/`:

- Native Linux arm64 OrbStack Docker, Go 1.27.3, real PostgreSQL 18.6 and Valkey 9.1.3: `go test ./...` and `go test -race ./...` passed, including all tenant, restriction, rollback, migration and cache-loss cases.
- The existing real backend/Agent/blocked-Hub process test passed in normal and process-race runs. Duplicate delivery retains one SQLite effect; disconnect remains unknown. Hub is still blocked with signer epoch zero; financial authority is outside this slice.
- sqlc v1.31.1 generated twice from independent clean input directories; `cmp -s` matched both results and committed generated Go. sqlc archive, protoc 36.2 archive and Goose v3.29.0 module checksums passed. Goose runs through the existing explicit backend migration command and embedded pinned library; no unrelated CLI dependency installation was introduced.
- The exact 24-module backend compiled production/test closure was reviewed with license-file hashes and checked for host plus Linux amd64/arm64 dependency selection. Module checksum verification passed. This is not native amd64 execution or a full generator/release-image license attestation.
- `license/dependency-gate.log` and `.json` show **exit 1** on the exact committed relevant source. The full generator closure has not passed. Actions was not run; it remains for 06-09/06-11.
- Caller checkout remains dirty due preserved Phase 4/5 work. Source-status records prove the temporary committed clone was clean before and after execution; caller cleanliness is not claimed.

## TDD Gate Compliance

RED `ec76aa6` ran `TestTenantRestrictionSchema` against real PostgreSQL migrated through 00001. All six required tenant-policy authority tables were absent; these planned assertions failed after the Go test executed successfully. The unittest reporter wraps the real Go command and preserves its complete failure output. `artifacts/phase6/06-08/tdd-red.json` passed the GSD classifier with `RED_EVIDENCE_OK`.

Semantic assessment: the target executed and failed the intended absent-authority assertions, with no compilation, fixture, discovery or unrelated failures. GREEN `60d85f8` followed RED and passed the expanded real database tests. No refactor commit was necessary.

## Blocking Decision: CR-003

The official locked sqlc v1.31.1 binary metadata includes `github.com/go-sql-driver/mysql v1.9.3`, sum `h1:U/N249h2WzJ3Ukj8SowVFjdtZKfu9vlLZxjPXV1aweo=`. The exact driver LICENSE is MPL-2.0. `docs/governance/license-policy.yaml:5` does not allow MPL-2.0, and line 9 explicitly includes generators and transitive dependencies. The sqlc command package directly blank-imports this driver, so it is not merely an uncompiled graph dependency.

`docs/governance/proposals/CR-003-sqlc-generator-license-exception.md` gives archive/binary checksums, official LICENSE/source links, notices/source-availability obligations and three concrete options. Recommended: owner-approved exception confined to the three already locked official sqlc development/CI artifacts and this exact driver, without widening product dependency licenses. Alternative: keep the policy and replan a separately approved generator artifact. No existing option of the unchanged official binary can remove an already linked dependency. No tool was patched, replaced, or silently exempted.

## Deviations from Plan

1. **Rule 4 — explicit decision required:** approved top-level sqlc MIT metadata does not cover linked MPL-2.0 code. The complete license gate fails closed. No user/policy approval was inferred. WINDOWS entry 2 records the defect; Task 2 and dependent waves remain blocked.
2. **Existing scope boundary:** source ledgers and balances do not exist until 06-05. Tests assert all currently produced nonempty authority facts; no empty balance set is presented as balance conservation. Existing WINDOWS entry 1 remains open for 06-05/06-11.
3. **State tooling limitation:** the session state handler rejects Phase 5's pre-existing `passed_with_scope` verdict. Preserve that historical verdict and update only the Phase 6 checkpoint position. The roadmap SDK ran before this partial summary and correctly retained 1/11 complete; the incomplete summary must not be counted as plan completion.

## Deferred Issues and Resume

Owner selects A/B/C in CR-003. After an approved decision, finish the full generator closure review, implement the exact allowed exception/artifact boundary if selected, rerun the dependency gate and required application checks, then replace this blocked summary with truthful completion metadata. Keep 06-08 unchecked and the Phase 6 progress at 1/11 until that occurs.

No intentional implementation stubs or skipped required tests were added. No new network listener or external auth endpoint was introduced; new schema/access surfaces implement threat T-06-23. Native amd64, Actions, real source balances and broader Phase 6 acceptance remain unproven by this slice.

## Self-Check: PASSED

All eleven checked implementation/proposal/summary files exist, and all four recorded commits are ancestors of HEAD. Docker source identity matches the application commit; both normal and race runs contain the required passed tests with no skips. This self-check verifies the checkpoint record, not completion of the blocked license gate.
