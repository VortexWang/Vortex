---
phase: 05-m3-agent-edge-hub-m3-agent-and-hub
plan: '08'
subsystem: agent-runtime-accounting
tags: [scheduler, renewal, SQLite, FULL, mTLS, retention, race]
requires:
  - phase: 05-07
    provides: Bounded regional runtime and current-session fencing
provides:
  - Bounded target probes and periodic current-session health/version reporting
  - Finite durable remote renewal over real node TCP/UDP traffic
  - Crash/revocation/session/time failure isolation with retained unknown debt
  - Conservative settled transport compaction with permanent receipt identity
  - Same-source Docker/Actions gates and independent raw SQLite audits
affects: [05-09, 05-10, 05-11, 05-12, 05-13, 05-14]
tech-stack:
  added: []
  patterns: [durable-before-send, immutable-request-hash, immediate-writer-snapshot, read-only-observers, conservative-retention]
key-files:
  created: [agent/scheduler.go, agent/renewal.go, agent/store/renewal_requests.go, agent/store/share_refs.go, agent/store/usage_retention.go, agent/store/reader.go, agent/renewal_safety_test.go, agent/renewal_session_test.go, test/phase5/scheduler_process_test.go, tools/phase5/audit_renewal_safety.py, tools/phase5/audit_usage_retention.py]
  modified: [agent/control.go, agent/authority_clock.go, agent/store/store.go, hub/testhub.go, hub/issuer.go, hub/recovery.go, tools/phase5/check.sh]
requirements-completed: [AGT-003, AGT-005, AGT-006, AGT-007, HUB-002, HUB-003, HUB-004, HUB-007, HUB-008, HUB-009, HUB-012, HUB-014, SUP-B1-001]
requirements-status-note: Plan coverage only; all IDs remain globally pending under the shared-plan gate.
key-decisions:
  - Received renewal success is not installed authority; unseen issuance remains reserved.
  - Revocation wins against late/duplicate success without releasing unknown debt.
  - Agent Hub ACK is not backend settlement and cannot authorize pruning.
  - Compact duplicate settled transport storage only; retain journal and hash identity.
coverage:
  - id: bounded-probes-reports
    description: Probe cadence, worker/target bounds and periodic current-session reports
    verification:
      - kind: integration
        ref: agent/scheduler_test.go#TestSchedulerReportsOverCurrentMutualTLSStreamWithoutAuthority
        status: pass
      - kind: integration
        ref: agent/scheduler_test.go#TestSchedulerBoundsConcurrencyAndJoinsCancellation
        status: pass
    human_judgment: false
  - id: live-durable-renewal
    description: Finite low-water TCP/UDP top-up and stable ID/hash crash replay
    verification:
      - kind: e2e
        ref: test/phase5/scheduler_process_test.go#TestSchedulerProcessRenewal
        status: pass
      - kind: other
        ref: make phase5-check on 0c104a6 Docker and Actions42175493658
        status: pass
    human_judgment: false
  - id: failure-and-conservation
    description: Revocation, old-session, disk, time and exhaustion cannot consume unfunded writes or release unknown debt
    verification:
      - kind: integration
        ref: agent/renewal_safety_test.go#TestRenewalSafetyRevocationDominatesDelayedAndConcurrentSuccess
        status: pass
      - kind: integration
        ref: agent/renewal_session_test.go#TestRenewalSafetyLateOldMutualTLSResultCannotInstall
        status: pass
      - kind: other
        ref: tools/phase5/audit_renewal_safety.py retained Docker/Actions snapshots
        status: pass
    human_judgment: false
  - id: safe-retention
    description: Trusted seven-day age and contiguous backend confirmation guard duplicate transport cleanup
    verification:
      - kind: integration
        ref: agent/store/usage_retention_test.go#TestSettledUsageRetentionConservativeAgeAndRestartReplay
        status: pass
      - kind: integration
        ref: hub/usage_retention_test.go#TestRecoveryRetainsReceiptIdentityAfterUsageCompaction
        status: pass
      - kind: other
        ref: tools/phase5/audit_usage_retention.py retained Docker/Actions snapshots
        status: pass
    human_judgment: false
duration: multi-session
started: 2026-11-06T03:36:41Z
completed: 2026-11-06
status: complete
---

# Phase 5 Plan 8: Bounded probes, durable renewal and conservative retention

The actual node now probes installed targets, periodically reports bounded
health/version snapshots and renews finite existing allocations over its current
authenticated stream. Real TCP/UDP continues after top-up; crash, stale-session,
revocation, exhaustion, disk failure and expiry preserve durable funding bounds.

## Accomplishments

- Task 1: bounded scheduler/report cache and runtime remote-renewal tracer,
  including explicit source/share anchors and correlated durable results.
- Task 2: seven required binary-process renewal cases, repeated safety/journal/
  race tests, WAL writer/reader isolation, and seven-day conservative Hub cleanup
  retaining permanent identity plus original authoritative journal.
- Independent auditors verify raw SQLite payload hashes, request correlation,
  source balances, unknown holds, contiguous confirmations and retained disputes.

## Task commits

- Journal and scheduler: `464a0c6`, `29b3085`.
- Source anchors and full result correlation: `adaa388`.
- Live renewal: `36d244b`.
- Crash replay/WAL fixes: `a983ab9`, `e9f2f3d`, `e4aa4c4`.
- Safe settled transport compaction: `b0ab02a`.
- Final renewal session/revocation/time proof: `0c104a6`.

## Verification

Final product source `0c104a600300b9f5923778a893013604bbb03a80` passed
`make phase5-check` in clean pinned Linux arm64 Docker and GitHub Actions
Ubuntu amd64 run42175493658. Both required seven actual-process scenarios,
18 named safety-race passes, 18 retention-race passes, scheduler/share/journal
regressions, Engine race, real SQLITE_FULL and recovery checks. Twelve safety
snapshots plus retention/recovery snapshots were independently audited per host.
Native root/race/vet also passed. See05-08-EVIDENCE.md and retained pair.json /
files.sha256 for exact output, failed earlier attempts and acceptance table.

## Deviations from Plan

**[Rule 2 - Missing critical] Source/share anchors and receipt installation
separation.** Runtime acceptance required explicit local roots and full result
correlation rather than journal-only success. Added Store/Hub helpers under the
already approved schema; no normative contract or dependency changes.

**[Rule 1 - Bug] Replay and WAL concurrency.** Live Hub restart exposed exact
config-receipt replay conflicts; concurrent observer/startup writes exposed stale
WAL snapshot promotion. Exact same event replay is idempotent, changed bytes
conflict, observers are read-only and writers reserve before reading authority.
Earlier local failed SHA attempts remain recorded as failed, not paired success.

**[Rule 2 - Missing critical] Cleanup proof and identity tombstones.** Added
FULL local migration, mature/unaged bounded scan, conflict evidence, permanent
hash tombstones and independent auditors. Only duplicate known transport payloads
and receipts compact; authoritative journal/unknown/disputed evidence survives.

Total: three implementation deviation groups, plus supporting test/auditor files
outside the proposed file list. Impact: fulfills existing safety acceptance;
no new authority source, product dependency or normative protocol change.

## Issues Encountered and boundaries

Earlier SQLITE_BUSY failures and native macOS Python SQLite snapshot-open failure
remain explicitly not passed; final pinned Linux pairs and independent re-audits
passed. Deterministic exact-time and seven-day tests prove conservative bounds,
not seven days of production observation. Protocol backend fixtures are not Phase6
production authority; original qualification-only Engine identity remains pending
05-09. Product-wide requirements remain pending where declared by later plans.
No Phase4 performance or full Phase5/UAT completion is claimed.

## Self-check: PASSED

Both task acceptance criteria, prescribed tests, finite threat mitigations and
same-source paired gates are evidenced. Ready for05-09; Phase5 remains executing.
