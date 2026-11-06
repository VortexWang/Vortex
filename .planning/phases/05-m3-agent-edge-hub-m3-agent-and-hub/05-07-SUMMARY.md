---
phase: 05-m3-agent-edge-hub-m3-agent-and-hub
plan: '07'
subsystem: agent-control
tags: [grpc, mTLS, regional-failover, race, process]
requires:
  - phase: 05-06
    provides: Exact owner-approved lifecycle and identity contracts
provides:
  - Actual node bounded same-region control loop with readiness/status watchdog
  - Stream ownership fencing and joined cancellation
  - Real process TCP continuity and unknown-debt regression evidence
affects: [05-08, 05-09, 05-12]
tech-stack:
  added: []
  patterns: [single-send-loop, authenticated-session-status, cancel-before-mutation-lock]
key-files:
  created: [agent/regional_control.go, agent/regional_control_test.go, agent/store/control_status.go, agent/store/control_status_test.go, hub/status.go, test/phase5/regional_process_test.go, tools/phase5/REGIONAL-CONTROL.md]
  modified: [agent/control.go, agent/store/store.go, hub/testhub.go, cmd/vortex-node/main.go, cmd/vortex-test-hub/main.go, tools/phase5/check.sh]
requirements-completed: []
key-decisions:
  - Hub status is observational and cannot refresh funds or trusted authority time.
  - Regional component closed; scheduler and other shared requirements remain pending.
coverage:
  - id: regional-runtime
    description: Actual node selects authorized online same-region peer under stalled/restricted peers
    requirement: AGT-002
    verification:
      - kind: e2e
        ref: test/phase5/regional_process_test.go#TestRegionalProcessFailover
        status: pass
    human_judgment: false
  - id: session-fencing
    description: Old stream cannot mutate rights; TCP/unknown liabilities preserve original bounds
    verification:
      - kind: integration
        ref: agent/regional_control_test.go#TestRegionalLateAuthorizationCannotAdvanceClockOrConfig
        status: pass
      - kind: e2e
        ref: test/phase5/regional_process_test.go#tcp-original-authorization
        status: pass
    human_judgment: false
completed: 2026-11-06
status: complete
---

# 05-07 bounded regional runtime

The shipped vortex-node now runs a bounded region-aware TLS/control loop. Authenticated current-session status selects only configured peers; timeout, watchdog and joined ownership cancellation isolate stalled/old streams. Existing TCP and funded unknown holds survive transport switch without extending authority.

## Task commits

- Tracer/runtime: `578e8d2`.
- Required process/repeated race gates and runbook: `0535c6e`.
- Fixture timing and bounded aggregate diagnostics/backoff fix: `dd109e5`.
- Healthy lifetime/sweep budget correction and regression: `0182318`, `ec155ad`.

## Verification

Final source `ec155ad92d5a6d940fc0f22a051872fb0d982c60`: Docker Linux arm64 and [Actions42154985414](https://github.com/VortexWang/Vortex/actions/runs/42154985414) passed full make phase5-check, five actual-process cases and ten repetitions of all required regional race cases. Both raw recovery sets independently audited; five snapshots/ten faults each. See05-07-EVIDENCE.md for failed first attempt, correction, exact source references and raw evidence.

## Boundaries and deviations

Added read-only Store status/exhaustion tests and required named-test gate/runbook beyond the proposed file list. No dependency or normative contract changes. Original qualification-only Engine certificate fixture retained pending05-09 shared identity integration. Production backend remains Phase6. G05-01 scheduler/renewal part awaits05-08; other gaps remain open and shared requirement IDs are not marked complete. Historical UAT is unchanged.

## Self-check: PASSED

Actual process tracer, mandatory tests, negative/race/cancel/unknown-hold checks and same-source paired gates passed. Neither finite fixtures nor successful control failover are represented as full phase/performance acceptance.
