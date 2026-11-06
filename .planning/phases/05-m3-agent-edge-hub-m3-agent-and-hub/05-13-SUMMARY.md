---
phase: 05-m3-agent-edge-hub-m3-agent-and-hub
plan: '13'
subsystem: share-policy-runtime
requires:
  - phase: 05-12
    provides: Durable finite recovery barrier
provides:
  - Complete share lineage and configuration revision occupancy before Prepare/Commit/OPEN
  - Durable immutable policy cutover and separately funded future-period admission
  - Actual TCP/UDP positive and hostile-share process evidence
requirements-completed: [AGT-004, AGT-005, AGT-010, AGT-011, HUB-002, HUB-009, HUB-011, HUB-016, HUB-018, SUP-B1-001, SUP-B3-001]
requirements-status-note: Scoped coverage only; consolidated sibling plan05-14 remains incomplete.
tech-stack:
  added: []
  patterns: [immutable-session-snapshot, funded-family-renewal, proof-backed-share-release]
coverage:
  - id: complete-share-runtime
    description: Complete installed chain gates actual TCP/UDP and Commit; unknown activation retains historical occupancy
    verification:
      - kind: e2e
        ref: test/phase5/share_policy_process_test.go#TestSharePolicyProcess
        status: pass
      - kind: integration
        ref: agent/store/share_occupancy_test.go#TestConfigShareOccupancyRetainsUnknownAndRequiresProvenRelease
        status: pass
    human_judgment: false
  - id: immutable-policy-boundary
    description: Guard and conservative time select old/new immutable funded snapshots without moving existing debt
    verification:
      - kind: e2e
        ref: test/phase5/share_policy_process_test.go#TestSharePolicyProcess
        status: pass
      - kind: integration
        ref: agent/store/policy_transitions_test.go#TestPolicyTransitionPinsSessionAndSeparatesFutureFunding
        status: pass
      - kind: integration
        ref: agent/store/policy_transitions_test.go#TestPolicyFutureRenewalKeepsFundedFamily
        status: pass
      - kind: integration
        ref: agent/store/policy_transitions_test.go#TestPolicyMissingFutureAndUntrustedClockNeverFallBackToOldPeriod
        status: pass
    human_judgment: false
  - id: paired-ledger-evidence
    description: Same source clean Docker and Actions, six policy snapshots and independent conservation audits
    verification:
      - kind: other
        ref: artifacts/phase5/check/bc1c262602e03a8da544d72d071ae27874fb3701/pair.json
        status: pass
    human_judgment: false
completed: 2026-11-06
status: complete
---

# 05-13 share chains and immutable policy cutover

Agent now durably validates full source/parent/issuer/revision chains before
configuration Prepare/Commit and each new Engine OPEN. Historical configuration
holds survive unknown activation until proven release; current CAS and retained
session occupancy prevent capacity reuse. Engine receives validated sessions
through its existing process-internal Gate and never accesses SQL.

Policy transitions persist approved immutable snapshots and independently funded
future authorization families. New TCP/UDP use conservative effective time and
guard checks; existing sessions retain their source/snapshot and original limits.
Missing funds/time, policy revocation, late replay and malformed share chains fail
closed. Funded same-family renewal remains usable. Hub restart does not issue
additional funds or release the original512-byte unknown liability.

Sourcebc1c262 passed full clean Linux arm64 Docker and Ubuntu amd64
Actions42204137919, plus named native process/race/vet checks. Eight actual-process
cases, scoped Store tests, six policy snapshot audits and existing recovery,
renewal, retention, disk and Engine regressions passed. Evidence/checksums and
precise limitations are retained in05-13-EVIDENCE.md.

Deviation: existing per-session Gate already supports immutable snapshot identity,
so no Engine contract/source change was needed. Trusted offline provisioning and
backend policy allocation remain explicitly protocol-test fixtures for Phase6.
VACUUM snapshots are consistent standalone backups; source WAL mode is separately
recorded. SQL abort tests do not prove physical power-loss behavior. This closes
only05-13;05-14 consolidated verifier/security/review/UAT remains outstanding.
