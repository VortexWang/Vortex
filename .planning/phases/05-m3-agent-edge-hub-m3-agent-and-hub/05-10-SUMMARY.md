---
phase: 05-m3-agent-edge-hub-m3-agent-and-hub
plan: '10'
subsystem: node-lifecycle
tags: [unix-socket, drain, systemd, Docker, SQLite, race]
requires:
  - phase: 05-09
    provides: Shared durable control and VLink runtime identity
provides:
  - Actual enroll/run/status/drain commands with private IPC and one writer
  - Engine stream admission drain, graceful half-close and bounded forced cleanup
  - Non-root deployment entries and real container signal/restart evidence
affects: [05-11, 05-12, 05-13, 05-14]
tech-stack:
  added: []
  patterns: [private-local-admin, single-writer-process-lock, drain-before-stop]
key-files:
  created: [agent/admin.go, agent/process_lock.go, cmd/vortex-node/commands.go, engine/runtime/relay/drain.go, deploy/systemd/vortex-node.service, deploy/node/Dockerfile, test/phase5/node_commands_test.go, test/phase5/node_container_test.go, tools/phase5/node_deployment_check.sh]
  modified: [cmd/vortex-node/main.go, agent/node.go, agent/engine_runtime.go, agent/scheduler.go, engine/runtime/runtime.go, engine/runtime/relay/bridge.go, tools/phase5/check.sh, .github/workflows/phase5.yml]
requirements-completed: [AGT-004, AGT-008, AGT-009, AGT-010, AGT-011, SUP-AGENT-001]
requirements-status-note: Scoped plan coverage only; sibling upgrade and phase acceptance remain pending.
key-decisions:
  - Use owned private Unix IPC; status/drain never open another DB writer.
  - Admission and drain share one linearization lock; grace retains original accounting.
  - Timeout closes residual links but success requires worker cleanup, never liability release.
  - Keep container supervision and actual business traffic drain evidence distinct.
coverage:
  - id: operational-commands
    description: Actual enroll/run/status/drain, private boundary and duplicate writer rejection
    verification:
      - kind: e2e
        ref: test/phase5/node_commands_test.go#TestNodeCommandsDrain
        status: pass
      - kind: integration
        ref: cmd/vortex-node/main_test.go#TestEnrollCommandUsesLocalCSRAndRejectsSecondWriter
        status: pass
      - kind: unit
        ref: agent/admin_test.go#TestAdminStrictBoundedRequests
        status: pass
    human_judgment: false
  - id: drain-and-liability
    description: Established mux rejects new OPEN, old TCP half-closes, residual TCP/UDP closes with debt retained
    verification:
      - kind: e2e
        ref: test/phase5/node_commands_test.go#TestNodeCommandsDrain
        status: pass
      - kind: unit
        ref: engine/runtime/relay/drain_test.go#TestDrainRejectsNewOpenOnEstablishedMux
        status: pass
      - kind: unit
        ref: engine/runtime/relay/drain_test.go#TestDrainWaitsForWorkerAndDoesNotInventCleanup
        status: pass
    human_judgment: false
  - id: deployment-supervision
    description: Non-root actual image handles signals/restart and retains authoritative debt
    verification:
      - kind: e2e
        ref: test/phase5/node_container_test.go#TestNodeContainerLifecycle
        status: pass
      - kind: other
        ref: tools/phase5/node_deployment_check.sh on061ffb5 Docker and Actions42186204143
        status: pass
    human_judgment: false
duration: multi-session
completed: 2026-11-06
status: complete
---

# Phase5 Plan10: Operational node lifecycle

Actual node commands now reach the running Agent and its Engine through private
Unix IPC. Drain rejects new streams while preserving funded existing traffic,
then joins forced cleanup at deadline. No lifecycle transition releases unknown debt.

## Completed tasks

1. Refactored testable CLI and local CSR enrollment; private bounded IPC and
exclusive run/enroll writer; formal Engine drain/stats; real TCP/UDP/half-close,
repeat drain, duplicate-writer and restart scenarios.
2. Non-root systemd and scratch container packaging, actual named-volume signal
and kill/restart test, Linux dual-architecture builds and Ubuntu unit verification.

Product commit: `061ffb5577a726b0431d2de9697850d86be061ef`.
Both clean Docker arm64 and Ubuntu amd64 Actions42186204143 passed full
phase5-check and deployment lifecycle. Raw files, independent audits, binary
hashes and artifact selection are linked by05-10-EVIDENCE.md and pair.json.

## Deviations and limits

[Rule1 - Bug] Detached run context initially left no-Hub SIGTERM waiting on the
internal context; it now observes the external shutdown signal. Long macOS Unix
paths use a short owned per-database default. These corrections precede the
verified source. [Rule2 - Missing critical] IPC duplicate/unknown fields, bounds,
lock-file type/ownership and live-socket replacement are checked explicitly.

No normative contract, dependency or financial rollback. Existing unrelated
Phase4/05-03/wave2 edits preserved. Systemd verification is configuration-level,
container signal/restart is real, and traffic drain is a separate actual node/mux
fixture. Cross compilation is not runtime proof. Production backend and performance
qualification remain outside this plan. G05-02 awaits05-11; Phase5 remains incomplete.
