---
phase: "06"
slug: "m4-m4-backend"
status: draft
nyquist_compliant: false
wave_0_complete: false
created: "2026-11-07"
---

# Phase 06 — Validation Strategy

> Per-phase validation contract for feedback sampling during feedback and execution.

## Test Infrastructure

| Property | Value |
|----------|-------|
| **Framework** | Go `testing`, `go test -race`, PostgreSQL integration in Docker Compose |
| **Config file** | None yet — Wave 0 must add backend module, PostgreSQL fixture, migration runner and test configuration |
| **Quick run command** | `go test ./backend/...` |
| **Full suite command** | `docker compose -f deploy/backend/compose.test.yml run --rm backend-test` plus `go test -race ./backend/...` |
| **Estimated runtime** | ~120 seconds locally after Wave 0 |

## Sampling Rate

- **After every task commit:** Run the smallest affected `go test ./backend/... -run <focused-pattern>` command.
- **After every plan wave:** Run the Docker PostgreSQL integration suite and `go test -race ./backend/...`.
- **Before `$gsd-verify-work`:** Full suite must be green on the same source SHA in local Docker and GitHub Actions.
- **Max feedback latency:** 120 seconds for the focused suite; 10 minutes for the full Docker/Actions gate.

## Per-Task Verification Map

| Task ID | Plan | Wave | Requirement | Threat Ref | Secure Behavior | Test Type | Automated Command | File Exists | Status |
|---------|------|------|-------------|------------|-----------------|-----------|-------------------|-------------|--------|
| 06-01-01 | 01 | 1 | BE-001, BE-004, TEN-001..009 | T-06-01 / T-06-02 | PostgreSQL authority and tenant scope cannot be bypassed; cross-tenant access is denied and audited | PostgreSQL integration | `go test ./backend/... -run 'Tenant|Scope|Audit'` | ❌ W0 | ⬜ pending |
| 06-02-01 | 02 | 2 | NOD-001..014, PRT-001..016 | T-06-03 | Ownership, lifecycle, port conflicts and common-port reservation remain transactional | PostgreSQL integration | `go test ./backend/... -run 'Node|Port|Group'` | ❌ W0 | ⬜ pending |
| 06-03-01 | 03 | 3 | RUL-001..016, BE-002/003 | T-06-04 / T-06-05 | Compiler digest is deterministic; stale revisions and partial application cannot update desired state | Unit + integration | `go test ./backend/... -run 'Rule|Compile|Apply|Task'` | ❌ W0 | ⬜ pending |
| 06-04-01 | 04 | 4 | PLN-001..008 | T-06-06 | Package snapshots and idempotent entitlement redemption preserve source and period history | PostgreSQL property/integration | `go test ./backend/... -run 'Package|Entitlement|Subscription'` | ❌ W0 | ⬜ pending |
| 06-05-01 | 05 | 5 | MTR-001..016, SUP-B2-001, SUP-B5-001 | T-06-07 / T-06-08 | Per-source B/F/L invariants, immutable snapshots and unknown liability survive duplicate/faulted usage | PostgreSQL concurrency + fault injection | `go test -race ./backend/... -run 'Source|Usage|Unknown|Dispute'` | ❌ W0 | ⬜ pending |
| 06-06-01 | 06 | 6 | LIM-001..010 | T-06-09 | Share CAS, boot fencing and activation-unknown do not release ownership without proof | PostgreSQL + Agent/Hub integration | `go test -race ./backend/... -run 'CAS|Boot|Activation|Share'` | ❌ W0 | ⬜ pending |
| 06-07-01 | 07 | 7 | BE-005/006 | T-06-10 | REST errors/scopes, cursor ordering and SSE replay/gap recovery expose only authorized business state | HTTP integration | `go test ./backend/... -run 'OpenAPI|Cursor|SSE|Error'` | ❌ W0 | ⬜ pending |

*Status: ⬜ pending · ✅ green · ❌ red · ⚠️ flaky*

## Wave 0 Requirements

- [ ] `backend/go.mod` and package layout with the approved Go baseline.
- [ ] `backend/migrations/` Goose migrations for the authoritative schema and constraints.
- [ ] `backend/db/queries/` sqlc input plus generated-code reproducibility check.
- [ ] `backend/internal/testutil/postgres.go` Docker PostgreSQL fixture and cleanup helpers.
- [ ] `deploy/backend/compose.test.yml` PostgreSQL + backend test entry point.
- [ ] `.github/workflows/phase6.yml` same-SHA Docker/Actions gate with raw evidence retention.
- [ ] OpenAPI validation/generation command and an explicitly reviewed generator version, or a documented no-generator boundary until Phase 7.

## Manual-Only Verifications

| Behavior | Requirement | Why Manual | Test Instructions |
|----------|-------------|------------|-------------------|
| PostgreSQL physical backup/WAL restore and external-service recovery rehearsal | BE-001, BE-006 | Requires an isolated database service and operator-controlled storage lifecycle | Run the documented `pg_dump`/restore and WAL recovery procedure against the pinned PostgreSQL image; compare ledger and desired-state digests before/after. |
| Real Agent/Hub mTLS deployment and operation receipt convergence | SUP-B2-001, SUP-B5-001 | Requires the Phase 5 node/hub binaries and certificates in a Linux process topology | Launch the pinned Docker topology, apply a rule revision, lose/reconnect the control stream, and confirm `unknown` converges without releasing authority. |

## Validation Sign-Off

- [ ] All tasks have `<automated>` verify or Wave 0 dependencies
- [ ] Sampling continuity: no 3 consecutive tasks without automated verify
- [ ] Wave 0 covers all missing references
- [ ] No watch-mode flags
- [ ] Feedback latency < 120 seconds for focused checks
- [ ] `nyquist_compliant: true` set in frontmatter after execution evidence passes

**Approval:** pending
