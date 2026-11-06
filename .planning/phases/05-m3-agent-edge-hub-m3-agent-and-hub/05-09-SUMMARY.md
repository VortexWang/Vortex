---
phase: 05-m3-agent-edge-hub-m3-agent-and-hub
plan: '09'
subsystem: runtime-identity
tags: [mTLS, VLink, certificate-rotation, revocation, SQLite, race]
requires:
  - phase: 05-08
    provides: Durable runtime accounting and same-host regression gates
provides:
  - Durable Agent identity publication to TLS and exporter HELLO
  - Live certificate rotation retaining existing funded TCP and boot/path identity
  - Durable additive authenticated revocation and current stream admission checks
  - Same-source Docker and Actions runtime/fault evidence
affects: [05-10, 05-11, 05-12, 05-13, 05-14]
tech-stack:
  added: []
  patterns: [immutable-per-handshake-snapshot, durable-before-publication, additive-revocation]
key-files:
  created: [agent/identity_runtime.go, agent/credential_revocations.go, agent/store/credential_revocations.go, contracts/credential_revocations.go, agent/identity_runtime_test.go, agent/identity_control_test.go, engine/runtime/link/identity_test.go, engine/vlink/admission_identity_test.go, test/phase5/identity_process_test.go]
  modified: [agent/credentials.go, agent/registration.go, agent/control.go, agent/identity/identity.go, agent/engine_runtime.go, cmd/vortex-node/main.go, engine/runtime/runtime.go, engine/runtime/link/supervisor.go, engine/runtime/link/transport.go, engine/vlink/dispatch.go, tools/phase5/check.sh]
requirements-completed: [AGT-001, AGT-012, HUB-001]
requirements-status-note: Scoped plan coverage; production issuer and sibling-plan requirements remain pending.
key-decisions:
  - Use the already approved 05-06 profile; no normative contract/dependency change.
  - Capture one TLS/HELLO pair, disable late key selection and session resumption.
  - FULL revocation commit precedes cumulative in-memory publication; Engine never queries SQL.
  - Reject new OPEN on affected existing links while preserving existing financial bounds.
coverage:
  - id: coherent-live-identity
    description: Actual node rotates both control and VLink identity without changing boot/path or old TCP
    verification:
      - kind: e2e
        ref: test/phase5/identity_process_test.go#TestIdentityProcessRotation
        status: pass
      - kind: unit
        ref: engine/runtime/link/identity_test.go#TestRuntimeIdentitySnapshotCapturedOnceForTLSAndHello
        status: pass
    human_judgment: false
  - id: durable-revocation-and-expiry
    description: Authenticated cumulative revocation and current link admission reject invalid identities
    verification:
      - kind: integration
        ref: agent/identity_control_test.go#TestRuntimeIdentityAuthenticatedControlRevocationStopsReconnectAndRenew
        status: pass
      - kind: integration
        ref: engine/vlink/admission_identity_test.go#TestIdentityAdmissionRejectsNewOpenWithoutClosingExistingStreams
        status: pass
      - kind: unit
        ref: agent/identity_runtime_test.go#TestRuntimeIdentityCapturedControlRejectsExpiredPeerAndLocal
        status: pass
    human_judgment: false
  - id: durable-selection-failure-boundaries
    description: Failed commit, crash, concurrent publication, overlap and stale aliases cannot replace authority
    verification:
      - kind: integration
        ref: agent/identity_runtime_test.go#TestRuntimeIdentityCrashPublicationAndStaleAliases
        status: pass
      - kind: integration
        ref: agent/identity_runtime_test.go#TestRuntimeIdentityRenewalCommitFailureKeepsPriorSelection
        status: pass
      - kind: other
        ref: make phase5-check on a9aea9d Docker and Actions42181891508
        status: pass
    human_judgment: false
duration: multi-session
completed: 2026-11-06
status: complete
---

# Phase 5 Plan 9: Shared runtime identity and revocation

Actual vortex-node now publishes its durably selected Agent identity to Engine.
A new TLS/exporter HELLO handshake captures one certificate/key pair, preserving
node/boot/path identity while a running node rotates its certificate. Existing
funded TCP survives ordinary rotation; new control/VLink connections use the new
serial. Invalid/expired/revoked credentials cannot create new authority.

## Accomplishments

- Task 1: formal Agent-to-Engine provider, managed startup without stale alias
  selection, approved node DNS/URI/EKU profile and actual binary live rotation.
- Task 2: current-stream authenticated additive revocation persisted in FULL
  transactions, conservative effective-at checks, current OPEN and renewal
  validation, crash/reopen/overlap/key mismatch and concurrent snapshot tests.
- Same-source Linux Docker/Actions gate and independent raw accounting audits.

## Task commits

`97eb2c2`, `a6f686e`, `f341db1`, final product source `a9aea9d`.

## Verification

Full `make phase5-check` passed on source
`a9aea9d29b35587fa281ff666877b3874d2c1932` in clean Linux arm64 Docker
and Ubuntu amd64 Actions42181891508. Each required actual node-process rotation,
70 named identity race passes, Engine identity/new-OPEN checks and previous
full-disk/recovery/source conservation suites. Raw audits independently passed
on both hosts. See05-09-EVIDENCE.md, pair.json and files.sha256. Failed initial
97eb2c2 evidence remains retained; it is not represented as passed.

## Deviations and boundaries

[Rule 2 - Missing critical] Added approved CredentialRevocations receiver and
additive Store journal because callbacks alone could not provide durable current
revocation to the actual node. NewManagedRuntimeEngine avoids stale runtime
aliases before manager recovery. No wire schema, version or dependency changed.

[Rule 1 - Bug] Docker exposed legacy fixture event-count/revocation expectation
regressions; fixes isolate lifecycle observations and manager-level local denial.
Active control streams also recheck peer expiry rather than trusting a past
handshake indefinitely. All are included in the final same-SHA gate.

D-01/02/03/12/19 preserved. T-05-15/16 have finite negative/failure tests.
Unknown debt remains funded: the process tracer retains unresolved512 bytes and
original expiry/source conservation through rotation/failover. Fixtures remain
protocol-only, not production backend authority or performance qualification.
Phase 5 is incomplete; 05-10 commands/drain/deployment is next.
