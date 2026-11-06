---
phase: 05-m3-agent-edge-hub-m3-agent-and-hub
plan: '11'
subsystem: signed-service-upgrade
tags: [Ed25519, systemd, drain, rollback, fencing, SQLite, race]
requires:
  - phase: 05-10
    provides: Actual private node commands, drain and deployment entries
provides:
  - Separate signed upgrade command with read-only rollback/schema preflight
  - Durable exclusive service stop/select/start/health and same-transaction recovery
  - Actual node replacement/failed-candidate rollback with financial monotonicity
affects: [05-12, 05-13, 05-14]
tech-stack:
  added: []
  patterns: [stop-witness-before-selection, nonfinancial-only-rollback, same-transaction-reconciliation]
key-files:
  created: [agent/upgrade/service.go, agent/upgrade/service_test.go, agent/upgrade/systemd.go, agent/upgrade/probe.go, cmd/vortex-node/upgrade.go, deploy/systemd/vortex-node-upgrade.service, test/phase5/upgrade_process_test.go]
  modified: [agent/upgrade/installer.go, agent/admin.go, agent/store/reader.go, cmd/vortex-node/main.go, cmd/vortex-node/commands.go, tools/phase5/check.sh, tools/phase5/wave1_check.sh, .github/workflows/phase5.yml]
requirements-completed: [AGT-008, AGT-009, AGT-012, SUP-AGENT-001]
requirements-status-note: Scoped plan coverage; phase-wide requirement gates remain pending.
key-decisions:
  - External upgrader proves whole-service termination before selection or rollback.
  - Only unchanged schema16 supports automatic rollback; no financial or authority snapshots.
  - Health requires actual current build/boot/control/config/FULL-WAL and authorized TCP response.
  - Reconcile interruptions under the same transaction with digest and fresh termination witness.
coverage:
  - id: actual-service-upgrade
    description: Distinct built candidate replaces drained real node; unhealthy candidate restores old forwarding service
    verification:
      - kind: e2e
        ref: test/phase5/upgrade_process_test.go#TestUpgradeProcessRollback
        status: pass
      - kind: unit
        ref: agent/upgrade/service_test.go#TestServiceRollbackStopsCandidateBeforeRestoringBinary
        status: pass
    human_judgment: false
  - id: durable-failure-reconciliation
    description: Stop uncertainty, nine crash stages, lock contention, changed digest and full disk cannot invent activation
    verification:
      - kind: integration
        ref: agent/upgrade/service_test.go#TestServiceStopUnknownCannotActivateAndRecoveryUsesSameTransaction
        status: pass
      - kind: integration
        ref: agent/upgrade/service_test.go#TestServiceCrashBoundariesRetainOneTransaction
        status: pass
      - kind: integration
        ref: agent/upgrade/service_test.go#TestServiceFullDiskCannotStopOrSelectCandidate
        status: pass
      - kind: unit
        ref: agent/upgrade/service_test.go#TestServiceRecoveredCandidateDigestCannotBeChanged
        status: pass
    human_judgment: false
  - id: rollback-financial-monotonicity
    description: Confirmed usage hashes and unknown debt persist, old boot authority stays closed and fencing never decreases
    verification:
      - kind: e2e
        ref: test/phase5/upgrade_process_test.go#TestUpgradeProcessRollback
        status: pass
      - kind: integration
        ref: cmd/vortex-node/main_test.go#TestUpgradePreflightReadsExistingSchemaWithoutMigrating
        status: pass
      - kind: other
        ref: make phase5-check on5ca72e0 Docker and Actions42193600373, raw ledger audits
        status: pass
    human_judgment: false
duration: multi-session
completed: 2026-11-06
status: complete
---

# Phase5 Plan11: Signed external service upgrade

Actual upgrade now drains the live node, proves termination, atomically selects
a signed staged binary, starts the service and requires current-authority TCP
forwarding health. Failed candidates stop before old binary restart; financial
state is never restored from a snapshot. Uncertain lifecycle outcomes remain
under the same durable transaction.

## Completed tasks

1. External command/systemd controller, independent installer lock, current build/
boot/FULL-WAL/control/config and authenticated existing-rule TCP health; actual
OS-process node candidate/rollback tracer.
2. Read-only same-schema preflight, no-settings-changed compatibility marker,
confirmed consumption/debt/fencing/old boot assertions, nine crash stages,
actual isolated full disk, stale selection and digest/lock/input refusal.

Commits:3710071,8566b1e, final product5ca72e06898f20d105c099e60f83defa1ebf5c5f.
Clean Linux arm64 Docker and Ubuntu amd64 Actions42193600373 passed full
phase5-check and deployment lifecycle. Evidence pair, raw logs/snapshots,
independent audits and checksums are in05-11-EVIDENCE.md. Earlier8566b1e pair
failed ActionsWave1 and remains failed; no substitution with its Docker pass.

## Deviations and boundaries

[Rule1 - Bug] Allowed bounded cleanup margin for operational drain RPC;
short client deadline previously cancelled before forced cleanup ACK. The real
restart tracer now supplies fresh backend-authorized fenced configuration and
separately funded roots; closed transactions/occupied shares are never reused.
Darwin cannot prove Linux process death and is not used for that acceptance.

[Rule2 - Missing critical] Added same-transaction post-selection reconciliation,
recovered digest check, installer OS lock, nested symlink refusal and explicit
failure log retention. Existing Wave1 redirected failures previously lacked
retained diagnostics; new source prints and archives them.

Systemd unit syntax/commands are verified on Ubuntu; actual lifecycle acceptance
uses real node OS processes and the formal controller, not an actual systemd
boot/upgrade claim. Backend is protocol-test-only; Engine runtime is the supported
static destination. Cross builds do not establish runtime/performance results.
No normative contract or dependency change. G05-02 resolved within this scope;
G05-05/05-12 and policy/phase acceptance remain pending. Phase5 is incomplete.
