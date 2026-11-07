---
phase: 06-m4-m4-backend
plan: 02
subsystem: database
tags: [postgresql, sqlc, nodes, grants, ports, mtls, outbox]
requires:
  - phase: 06-09
    provides: shared audit transactions, tenant policy and exact-source producer
provides:
  - Scoped node/group lifecycle, stable member snapshots, immutable grant history and segment policy
  - Atomic fixed-member TCP/UDP reservation and authenticated proof-backed release
  - Restriction/port/task/Outbox atomicity and receipt-gated serving eligibility
affects: [06-10, 06-03, 06-07, 06-11]
tech-stack:
  added: []
  patterns: [SQL-first scoped repositories, typed domain tasks, incarnation-bound mTLS proofs]
key-files:
  created: [backend/db/migrations/00003_nodes_ports.sql, backend/db/query/nodes.sql, backend/db/query/node_groups.sql, backend/db/query/ports.sql, backend/internal/service/node.go, backend/internal/service/port.go, backend/internal/repository/node_repository.go, backend/internal/repository/port_repository.go, docs/coverage/phase6/NODES-PORTS.md]
  modified: [backend/internal/service/tenant.go, backend/db/query/authority.sql, contracts/database/schema.sql, contracts/openapi/v1/openapi.yaml, backend/openapi/v1/openapi.yaml, tools/phase6/check.sh, tools/phase6/evidence_manifest.sh]
key-decisions:
  - Platform grants reuse resource_grants through a view; ownership is never copied into tenant resources.
  - Domain tasks have distinct desired-state identities and task kinds, preserving real RuntimeConfig and preventing accidental control delivery.
  - Node CA, TLS identity, current boot/generation, task kind, lease and revision jointly authorize proofs; user scope alone cannot release ports.
  - Restriction clear resets retention clocks but serving requires a new prepare and all-member confirmations.
requirements-completed: [NOD-001, NOD-002, NOD-003, NOD-004, NOD-005, NOD-006, NOD-007, NOD-008, NOD-009, NOD-010, NOD-011, NOD-013, NOD-014, PRT-001, PRT-002, PRT-003, PRT-005, PRT-006, PRT-007, PRT-016, PRT-008, PRT-009, PRT-010]
requirements-pending: [NOD-012, PRT-004]
coverage:
  - id: D1
    description: Scoped nodes/groups, grant history, lifecycle dependencies and transaction rollback
    requirement: NOD-007
    verification:
      - kind: integration
        ref: backend/internal/service/node_test.go#TestNodeGroupOwnershipAndStableMembers
        status: pass
    human_judgment: false
  - id: D2
    description: Physical port conflicts and proof-backed release retain unknown occupation
    requirement: PRT-009
    verification:
      - kind: integration
        ref: backend/internal/service/port_test.go#TestPortProofAuthenticationAndGenerationFence
        status: pass
    human_judgment: false
actuals:
  tokens: 74064
  tasks: 2
  commits: 6
plan_head_before: 7af22e0349bb9fec6d7b6bd12cc576c10e5088ba
plan_head_after: f25c39aa2169d95684f4da3b787b0580228a7d54
duration: 32min
completed: 2026-11-07
status: complete
---

# Phase 6 Plan 02: Node and Port Authority Summary

**PostgreSQL ownership/grant constraints and fixed-member TCP/UDP reservations with incarnation-bound mTLS release proofs.**

## Accomplishments

- Added only migration 00003; 00001/00002 remain unchanged. Platform ownership, homogeneous group membership/capabilities, scheduling and immutable billing boundaries are constrained. Grants retain validity/version/history; personal groups retain zero multiplier.
- Scoped services enforce dependency/drain/delete checks, immutable grant history, segment permissions and stable sorted member versions. Sensitive reads and mutations use the shared audit transaction.
- Common-port prepare reserves every member/protocol atomically, including physical conflicts across tenants and wildcard/mapped-IP addresses. Commit requires all matching confirmations. Failed prepare/abort cannot remove another allocation.
- Restriction events automatically retain ports and enqueue stop tasks in their audit transaction. Repeated causes preserve the first clock; final clear resets it without claiming execution resumed. New prepare receipts gate serving. Unknown/reboot/disconnect/DNS changes cannot free occupation.
- Created scoped encrypted_credentials without plaintext for the 06-10 consumer; no Cloudflare behavior is claimed.

## Task Commits

1. Task 1 RED `6ae6d0ce`; GREEN `d1256447`.
2. Task 2 RED `5f63bc97`; GREEN `0f2ec944`.
3. Address alias correction `b71daf7d`; raw evidence `f25c39aa`.

## Verification

Accepted source is `b71daf7d43a0ddaecba1b68efb3c664b8b0362c1`. Its clean clone passed the full local Linux arm64 Docker producer through 06-02: 82 PASS lines across normal/race, zero skips/race warnings, real PostgreSQL/Valkey and existing real backend/Agent/blocked-Hub processes. OpenAPI Ruby lint/copy, reviewed dependency/license closure and pinned sqlc v1.31.1 two-clean-generation comparison passed. Host development full normal/race also passed. Raw evidence lives in `artifacts/phase6/06-02/` with checksum-bound Docker reports and retained initial upgrade failure.

Goose v3.29.0 library applied the final migration to disposable test PostgreSQL. The standalone Goose CLI is unavailable; the recorded real goose_db_version query confirms 0/1/2/3 applied, replacing the literal CLI status check without adding a CLI dependency closure. No new dependencies were installed.

Both task RED assertions executed against real PostgreSQL and received RED_EVIDENCE_OK before GREEN; reports include semantic assessments. Initial record formatting/path errors were corrected before implementation. Actuals use realized committed diff chars/4, including raw evidence, not harness tokens. The 6-commit count is measured from the persisted plan ledger; 63 files changed, including 29 source/governance files.

## Deviations from Plan

- **Rule 2:** Added typed task routing and distinct domain desired-state identities so resource/port events cannot overwrite RuntimeConfig or be delivered as ordinary control payloads. Committed in `d1256447`/`0f2ec944`.
- **Rule 1:** New task metadata broke the prior-schema upgrade fixture. Explicit compatible LockTask columns and comparison of original authority fields preserve upgrade coverage; the new default is independently asserted. Fixed in `0f2ec944`; retained failure log is clearly development evidence.
- **Rule 1:** Normalized IPv4-mapped IPv6 listener aliases before conflict checks; regression/race tests pass in `b71daf7d`.
- **Rule 3:** Used approved Goose library migration plus real version-table status in place of the unavailable standalone CLI. Recreated only disposable Compose tmpfs test services to verify final 00003 from a fresh database.
- **Rule 3:** SDK state handlers reject the historical Phase 5 passed_with_scope status. Preserved that verification and manually synchronized only current Phase 6 position, metric, decision and session; roadmap/requirement SDK handlers succeeded.

## Deferred Integration and Acceptance

NOD-012 remains pending for compiler consumption in 06-03. PRT-004 remains pending for the real Agent port-task consumer in 06-03/06-07. This plan completes its durable schema/service/task/proof boundary; it does not claim users can already apply these new port tasks through the production control adapter. Successful proof tests use a real mutual TLS handshake plus persisted worker lease state, not a claimed real Engine port execution.

06-10 owns real Cloudflare CRUD, TTL=60/DNS-only and credential scope/fail-closed acceptance. New-source Actions and paired acceptance were not run here and remain pending for 06-11. TEN-002's later domain ownership, source balances/Valkey debt, Hub signer authority, Phase 4 NFR/D-04 and final-system acceptance remain unchanged. No unimplemented source stubs or skipped tests were introduced.

## Next Plan Readiness

Next dependency plan is 06-10. Phase 6 is 4/11 plans executed; overall phase acceptance remains pending. Default local disposable PostgreSQL at 15436 now has final 00003; Valkey remains at 16386. Caller Phase 4/5 dirty and untracked files were preserved.

## Self-Check: PASSED

Created authority/service/repository/evidence files exist; all six measured commits are ancestors of HEAD. The copied Docker evidence passes checksum and required-test manifest validation for its exact source SHA.
