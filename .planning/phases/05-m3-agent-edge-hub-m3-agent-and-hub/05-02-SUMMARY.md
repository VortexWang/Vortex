---
phase: 05-m3-agent-edge-hub-m3-agent-and-hub
plan: 02
subsystem: agent-control
tags: [mtls, sqlite, reconcile, upgrade]
requires:
  - phase: 05
    provides: Startable node and independent durable Agent/Hub stores
provides:
  - One-time enrollment token redemption, node SAN certificates and local credential persistence
  - TLS 1.3 mutual-authenticated control exchange with durable receipt and acknowledgement correlation
  - Configuration transaction reconciliation with durable activation-unknown results
  - Signed upgrade preflight and bounded drain, health-check rollback boundary
affects: [phase-05, phase-06, agent, hub]
tech-stack:
  added: []
  patterns: [local-key CSR enrollment, durable-before-ack, bounded-upgrade-hooks]
key-files:
  created:
    - agent/control.go
    - agent/identity/identity.go
    - agent/reconcile.go
    - agent/upgrade/upgrade.go
    - tools/phase5/wave2_check.sh
  modified:
    - agent/store/store.go
    - hub/testhub.go
    - .github/workflows/phase5.yml
    - Makefile
key-decisions:
  - "Enrollment and upgrade mechanisms are implementation/test boundaries; the Hub remains a protocol-test service, not a production authority."
  - "Unknown Commit results are durably recorded as activation-unknown before the caller receives the error."
  - "A fixed protoc 36.2 GitHub Action is installed because the Wave 1 CI command requires protoc."
requirements-completed: []
coverage:
  - id: D1
    description: One-time token CSR enrollment issues a node SAN certificate and stores the private key with restrictive permissions.
    verification:
      - kind: unit
        ref: agent/identity/identity_test.go#TestEnrollmentTokenCanBeRedeemedOnlyOnceConcurrently
        status: pass
      - kind: unit
        ref: agent/identity/identity_test.go#TestAttachKeyRejectsKeyThatDoesNotMatchCSR
        status: pass
      - kind: unit
        ref: agent/identity/identity_test.go#TestCredentialRotationValidatesBeforeOverlapAndExpiresOldCredential
        status: pass
      - kind: integration
        ref: agent/control_test.go#TestPersistCredentialStoresNodeIdentityAndExpiry
        status: pass
    human_judgment: false
  - id: D2
    description: TLS 1.3 mutual-authenticated control ACKs are correlated and durably persisted.
    verification:
      - kind: integration
        ref: agent/control_test.go#TestControlClientUsesMutualTLSAndPersistsAck
        status: pass
      - kind: integration
        ref: agent/control_test.go#TestControlRejectsAcknowledgementForDifferentMessage
        status: pass
    human_judgment: false
  - id: D3
    description: Config reconciliation preserves activation-unknown durably; upgrade drain and rollback hooks are bounded and guarded.
    verification:
      - kind: unit
        ref: agent/reconcile_test.go#TestReconcilerPersistsActivationUnknownBeforeReturning
        status: pass
      - kind: unit
        ref: agent/store/store_test.go#TestControlSequenceAndStateSurviveReopen
        status: pass
      - kind: unit
        ref: agent/upgrade/upgrade_test.go#TestDrainReceivesBoundedContext
        status: pass
      - kind: unit
        ref: agent/upgrade/upgrade_test.go#TestIrreversibleMigrationBlocksRollback
        status: pass
      - kind: unit
        ref: agent/upgrade/upgrade_test.go#TestHealthFailureRestoresCompatibilitySnapshotBeforeRollback
        status: pass
    human_judgment: false
  - id: D4
    description: Wave 2 check passes on native macOS arm64, Docker Linux arm64, and GitHub Actions Ubuntu amd64 at the same SHA.
    verification:
      - kind: integration
        ref: artifacts/phase5/wave2/native/report.json
        status: pass
      - kind: integration
        ref: artifacts/phase5/wave2/docker/report.json
        status: pass
      - kind: integration
        ref: artifacts/phase5/wave2/actions.json
        status: pass
    human_judgment: false
duration: 40min
completed: 2026-11-05
status: complete
---

# Phase 5 Plan 02: Registration, mTLS Control, Reconcile and Upgrade Summary

Added a testable Agent identity and control slice: one-time CSR enrollment, node SAN certificates, local private-key storage, TLS 1.3 mTLS exchange, durable ACK receipts, configuration result persistence, and signed upgrade supervision hooks.

## Continuation: Engine runtime lifecycle integration

Commit `7754bae` adds the first production-shaped Agent-to-Engine lifecycle boundary. `agent.RuntimeEngine` loads a validated runtime config and delegates Start/Stop to the new `engine/runtime.Runtime`; `cmd/vortex-node` accepts `--engine-config` and passes the adapter into `agent.New`. The runtime owns the Engine Supervisor and relay service lifecycle, so stopping the node cancels links and waits for relay workers instead of silently using `NoopEngine`.

The lifecycle fixture creates a temporary TLS 1.3 identity, starts the destination runtime through the Agent, observes that the serving goroutine remains alive, and stops it through the Agent boundary. It is intentionally a lifecycle and wiring check; it does not claim a complete multi-hop data-plane qualification.

Commit `3aed10a` hardens that boundary: listener binding is synchronous and a bind conflict fails `Start`; the runtime exposes its bound address and terminal result; the node remains alive after the one-shot test Hub handshake, propagates Engine serving failures, and performs a bounded stop on signal. Repeated stop is safe. Tests cover live listener reachability and occupied-port failure.

Same-SHA validation for `3aed10a2344231786d066558ed15a339d1770a5e`:

- Native macOS arm64: `make phase5-wave2-check`, Agent/Hub race tests, Go vet and Engine runtime package tests passed.
- Docker `golang:1.27.3` on Linux arm64: `make phase5-wave2-check` passed; report in `artifacts/phase5/wave2/report.json`.
- GitHub Actions Ubuntu 24.04: run `41998791768` passed on the same SHA.

## Performance

- **Duration:** about 40 min
- **Started:** 2026-11-05T05:53:06Z
- **Completed:** 2026-11-05
- **Tasks:** 4
- **Files modified:** 16

## Accomplishments

- Enrollment tokens are single-use under concurrent redemption; invalid CSR attempts do not consume the token. CSR identity and local private key are checked before credentials are accepted. Private-key file and containing directory permissions are enforced. Validated credential rotation retains both certificates during overlap, and Agent storage persists the current certificate.
- Agent control uses TLS 1.3 mutual authentication, selects only configured same-region Hub endpoints, and persists sequence, applied version, heartbeat and correlated acknowledgement. Config transaction IDs are validated without truncation; transaction intent and per-object results are durable, and unknown Commit outcomes remain `activation-unknown` across retries.
- Upgrade hooks verify an Ed25519 artifact signature and binary/disk/config preflight, pass a deadline-bounded drain context, restore a compatibility snapshot before rollback, and prevent rollback after an irreversible migration.
- The Wave 2 check is used by local and Actions workflows. A pinned Protoc 36.2 setup step fixes the existing Wave 1 workflow prerequisite.

## Task Commits

1. **Task 1-4: Registration, control, reconciliation and upgrade supervision** - `593a620`.
2. **Hardening: isolate per-host evidence outputs** - `7b2158b`.
3. **Hardening: serialize one-time enrollment redemption** - `1c8296a`.
4. **CI prerequisite: install pinned protoc** - `f618e14`.
5. **Hardening: enforce credential file permissions** - `5f34619`.
6. **Hardening: preserve enrollment token after invalid CSR** - `3153797`.
7. **Complete control state, object reconciliation and upgrade recovery** - `a5bff0b`.
8. **Connect Agent lifecycle to Engine runtime** - `7754bae`.
9. **Constrain runtime tracer mode and verify identity binding** - `5fac111`.
10. **Make listener lifecycle fail-fast and supervised** - `3aed10a`.

## Files Created/Modified

- `agent/identity/` - CSR enrollment, CA signing, credential persistence, TLS config and detached-signature verification.
- `agent/engine_runtime.go`, `engine/runtime/runtime.go` - Engine runtime lifecycle adapter and bounded qualification destination tracer.
- `agent/control.go`, `agent/credential.go`, `agent/reconcile.go` - regional mTLS control, credential persistence and durable config reconciliation.
- `agent/upgrade/` - signed preflight, bounded drain, health rollback and irreversible migration guard.
- `agent/store/store.go`, `hub/testhub.go` - durable config result query and TLS test listener.
- `tools/phase5/wave2_check.sh`, `Makefile`, `.github/workflows/phase5.yml` - repeatable Wave 2 checks across local and CI.
- `artifacts/phase5/wave2/` - same-SHA native, Docker and Actions evidence.

## Decisions Made

- Kept enrollment and upgrade as explicit Agent/testable interfaces; this does not represent an implemented production Hub or installer.
- Kept Phase 5 Wave 2 verification separate from Phase 4 performance qualification.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 1 - correctness] Harden identity, acknowledgement, transaction and drain boundaries**
- **Found during:** Wave 2 review
- **Issue:** Token redemption could race; key/certificate mismatch was not rejected; ACK correlation, transaction IDs, persisted Commit intent, per-object results and upgrade-state rollback were not fully guarded.
- **Fix:** Serialize redemption, validate credentials and ACK identity, add same-region reconnect and durable control state, persist transaction/object outcomes, and restore compatible state before bounded rollback.
- **Files modified:** `agent/identity/`, `agent/control.go`, `agent/credential.go`, `agent/reconcile.go`, `agent/store/`, `agent/upgrade/`, `hub/`, tests.
- **Verification:** focused race tests, `go test ./...`, `go vet ./...`, and `make phase5-wave2-check` passed.
- **Committed in:** `1c8296a`, `5f34619`, `3153797`, `a5bff0b`.

**2. [Rule 2 - missing critical] Restore missing Protoc setup in Phase 5 Actions**
- **Found during:** same-SHA Actions run
- **Issue:** Wave 1 workflow failed because `protoc` was absent, so Wave 2 never ran.
- **Fix:** Added the pinned `arduino/setup-protoc` action at commit `c65c819552d16ad3c9b72d9dfd5ba5237b9c906b` and version 36.2.
- **Verification:** Actions run `41986823563` succeeded for Wave 1 and Wave 2 on SHA `3127fb9`.
- **Committed in:** `f618e14`.

**Total deviations:** 2 auto-fixed. **Impact:** Both corrections close verification and identity safety gaps within the planned scope.

## Issues Encountered

The first Actions run (`41980275603`) stopped at Wave 1 because `protoc` was not installed. The workflow prerequisite was fixed. Final native, Docker and Actions Wave 2 reports all reference SHA `3127fb9`.

## Continued Execution: Durable Credential Renewal

Commit `4c408566a9dddb570c8d5f3d8304f893eac89491` closes credential-rotation crash windows found while continuing 05-02. The node now validates its persisted active identity before starting the Engine listener, uses SQLite as the authoritative active/overlap set, recovers and repairs compatibility aliases after restart, retains only the current overlap key material, and rejects locally revoked credentials without reactivating the node. Replacement key/certificate files are synced before the atomic SQLite rotation; active aliases are repaired only after the DB commit.

Focused regression coverage includes rejected renewal leaving the old DB and alias untouched, overlap recovery after restart when the alias is stale, expired overlap pruning, revoked previous-credential removal, and refusing rotation after node revocation.

Same-SHA evidence for `4c408566a9dddb570c8d5f3d8304f893eac89491`:

- Native macOS arm64: `make phase5-wave2-check` passed; raw logs under `artifacts/phase5/wave2/native/`.
- Docker `golang:1.27.3` Linux arm64: `make phase5-wave2-check` passed from a clean detached checkout; raw logs under `artifacts/phase5/wave2/docker/`.
- GitHub Actions Ubuntu 24.04 amd64: run `42013055297` passed both Wave 1 and Wave 2 on the same SHA; metadata in `artifacts/phase5/wave2/actions.json`.

This continuation does not complete 05-02. The Protobuf `RuntimeConfigUpdate` payload and lifecycle semantics remain draft, so control-stream delivery is not yet connected to durable Reconciler → Engine Prepare/Commit/Abort. The Hub enrollment/revocation authority is still a protocol-test service, and upgrade remains bounded supervision hooks rather than a complete signed installer and file-level automatic rollback. Keep 05-02 `in_progress` until those planned boundaries and fault evidence are complete.

GSD routing note: the partial summary was closed by gap-closure plan `05-05-PLAN.md`; see `05-05-SUMMARY.md` and `05-05-EVIDENCE.md` for the approved control contract, durable config apply boundary, file installer and same-SHA evidence.

## User Setup Required

None for this test slice. Production Hub CA/token lifecycle, certificate renewal orchestration, systemd integration, and an actual binary replacement installer remain incomplete and must not be inferred from these hooks/tests.

## Next Phase Readiness

Ready for `05-03`. Production Hub authority, certificate renewal/revocation orchestration, service-level installer orchestration, business-route hot application, and source/relay Agent integration remain open and must not be inferred from the completed control and installer boundary work. The legacy no-config mode remains a control-only tracer and is not treated as production data-plane integration.

---
*Phase: 05-m3-agent-edge-hub-m3-agent-and-hub*
*Completed: 2026-11-05*
