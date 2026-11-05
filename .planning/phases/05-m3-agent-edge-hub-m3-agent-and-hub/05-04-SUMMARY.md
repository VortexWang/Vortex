---
phase: 05-m3-agent-edge-hub-m3-agent-and-hub
plan: 04
subsystem: durable-recovery
tags: [sqlite, activation, signer-epoch, handoff, mtls, recovery]
requires:
  - phase: 05-03
    provides: Durable source-bound authority and typed unknown usage
provides:
  - Durable activation holds and verified execution/process recovery
  - Real fresh-boot re-Prepare, authority and Commit tracer
  - Atomic S/R ownership transfer and hash-bound prefix/tail replay
  - Approved authorization 1.1.0 successor attestation and revocation outbox
  - Same-SHA Docker/Actions fail-closed recovery evidence gate
affects: [phase-06, accounting, activation, phase-verification]
tech-stack:
  added: []
  patterns: [proof-before-release, atomic-signer-succession, immutable-handoff-outbox]
key-files:
  created:
    - agent/recovery.go
    - agent/runtime_activation.go
    - agent/runtime_restart_test.go
    - agent/store/activation.go
    - agent/store/boot_recovery.go
    - agent/store/handoff.go
    - agent/store/epoch_handoff.go
    - agent/store/recovery_authorization.go
    - hub/recovery.go
    - tools/phase5/audit_recovery.py
    - tools/phase5/docker_check.sh
  modified:
    - agent/reconcile.go
    - agent/control.go
    - agent/node.go
    - cmd/vortex-node/main.go
    - engine/apply/apply.go
    - contracts/authorization/v1/payload.schema.json
    - .github/workflows/phase5.yml
requirements-completed: []
key-decisions:
  - "Owner approved authorization 1.1.0 exact proposal 185541ee09e8dfbb0333a6249e0571c805b182220c77e7c2a79976d45efa4b6e; control stays 1.2.0."
  - "Signer epoch changes do not change path generation or extend original grants and deadlines."
  - "A new boot, timeout or ACK never releases unknown financial liability."
  - "Same-kernel and PID-namespace process death evidence can release configuration occupancy, never financial debt."
coverage:
  - id: D1
    description: Durable activation and proof before resource release
    verification:
      - kind: integration
        ref: agent/recovery_test.go#TestRecoveryQueriesExecutionBeforeReleasingResources
        status: pass
      - kind: integration
        ref: agent/recovery_test.go#TestNewExecutorCannotForgeOldAbortProof
        status: pass
    human_judgment: false
  - id: D2
    description: Fresh boot resumes a real configured Engine while retaining old debt
    verification:
      - kind: integration
        ref: agent/runtime_restart_test.go#TestRuntimeSIGKILLFreshBootReprepareAndRecoverDebt
        status: pass
    human_judgment: false
  - id: D3
    description: Unique S/R transfer and continued durable tail replay
    verification:
      - kind: integration
        ref: agent/store/handoff_test.go#TestHandoffPreservesIssuedRightsAndTransfersOnlyR
        status: pass
      - kind: integration
        ref: hub/recovery_test.go#TestRecoveryReplaysDurablePrefixAndTailAfterLostACK
        status: pass
    human_judgment: false
  - id: D4
    description: Authenticated successor epoch and revocations retain original authority bounds
    verification:
      - kind: integration
        ref: agent/epoch_handoff_test.go#TestAuthenticatedHubEpochHandoffKeepsOldGrantAndBoundsSuccessor
        status: pass
    human_judgment: false
  - id: D5
    description: Stale config admission and competing Commit/Abort cannot gain new rights
    verification:
      - kind: unit
        ref: agent/store/runtime_authority_test.go#TestRuntimeFencingAdvanceBlocksNewAdmissionButRetainsSession
        status: pass
      - kind: unit
        ref: engine/apply/apply_test.go#TestConcurrentCommitAbortHasOneOutcome
        status: pass
    human_judgment: false
  - id: D6
    description: Same-SHA dual-host recovery and missing-evidence rejection
    verification:
      - kind: other
        ref: artifacts/phase5/check/79a1c0b148f77e765190ee51075adf845e5ad115/pair.json
        status: pass
    human_judgment: false
duration: multi-session
completed: 2026-11-05
status: complete
---

# 05-04: Ledger handoff and activation recovery

Durable configuration recovery now reaches the real Engine boundary; signer recovery moves only unissued R once and retains unknown liabilities through authenticated replay and restart.

## Accomplishments

- Persisted immutable requests, fences, execution IDs, process witnesses, activation holds and observations before exposing transitions. Unknown execution stays occupied; stale or recreated executors cannot fabricate release proof.
- Added fresh-boot reporting, old-boot settlement-only fencing, typed unknown recovery and actual mTLS re-Prepare/current authority/Commit. SIGKILL testing resumes real TCP while retaining the original unfinished write liability.
- Added revoke-first atomic S/R handoff, single successor ownership, immutable recovery outbox and hash-bound contiguous confirmations for prefix and ongoing tail. Approved successor attestations keep original parent/source/deadline/path boundaries and cap new credit at R.
- Added real bounded full-disk, commit/abort competition, duplicate/lost/late receipts and new-admission fencing tests, plus a clean-source same-command Docker/Actions gate with independently audited SQLite snapshots.

## Implementation commits

`3e0fb03`, `56efdfe`, `fef53ed`, `614b4f6`, `6448c7d`, `a802ab3`, `79a1c0b`.

## Validation

Implementation SHA `79a1c0b148f77e765190ee51075adf845e5ad115`: clean Docker Linux arm64 and [Actions Ubuntu 24.04 amd64 42132354600](https://github.com/VortexWang/Vortex/actions/runs/42132354600) pass `make phase5-check`. Both retain root/Engine tests, races, vet, contract checks, ten required recovery fault cases, five independently audited snapshots and the separate competing Commit/Abort test. Four negative gate cases reject absent or modified evidence. Raw evidence, source/run linkage and SHA-256 manifest are under `artifacts/phase5/check/79a1c0b148f77e765190ee51075adf845e5ad115/`.

## Deviations and issues

The plan required an approved authorization-contract extension to carry S/R evidence. The owner approved the exact 1.1.0 proposal; schema, catalog, governance and CI were updated together. Intermediate Actions preflight failures exposed generated Python bytecode; the final pair fixes that classification while keeping untracked source blocked. Review also found missing actual-runtime activation, revocation-only recovery and stale-floor enforcement; all were fixed before acceptance.

## Boundaries and next step

This completes plan 05-04, not phase-wide acceptance. The configured runtime slice is a static destination; unsupported hot configuration is rejected. Hub/backend remains protocol-test-only, and Phase 6 supplies production backend authority. Finite process/disk tests do not certify physical power loss or performance. Prior-phase VLink approval-hash and benchmark-report failures remain visible in EVIDENCE. Phase 5 now has five plan summaries; continue its review, validation/security hooks and UAT before marking the phase complete.

## Self-Check: PASSED

All four plan tasks are linked to implemented source and passing evidence. The source commit is pushed, the Actions run succeeded on the same SHA, downloaded ledgers re-audit successfully, and no unrelated user changes were included.
