---
phase: 05-m3-agent-edge-hub-m3-agent-and-hub
plan: 05
subsystem: agent-control
tags: [protobuf, mTLS, sqlite, upgrade, recovery]
requires:
  - phase: 05
    provides: Agent identity, durable local store, control stream and Engine transaction interface
provides:
  - Owner-approved vortex.control.v1 1.1.0 runtime configuration update contract
  - Durable validated control config delivery through Engine Prepare/Commit/Abort
  - File-backed signed upgrade activation, restart recovery and guarded rollback
  - Same-SHA native, Docker and GitHub Actions Wave 2 evidence
affects: [phase-05, agent, contracts]
tech-stack:
  added: []
patterns: [approved-contract-hash, durable-config-apply, staged-upgrade-transaction]
key-files:
  created:
    - agent/control_apply.go
    - agent/config_validate.go
    - agent/upgrade/installer.go
    - agent/upgrade/installer_test.go
    - .planning/phases/05-m3-agent-edge-hub-m3-agent-and-hub/05-05-EVIDENCE.md
  modified:
    - contracts/protobuf/v1/vortex.proto
    - contracts/versions.yaml
    - contracts/requirements-map.yaml
    - docs/governance/contract-review.yaml
    - tools/phase1/contracts.py
    - tools/phase5/contracts_check.sh
    - .github/workflows/phase5.yml
requirements-completed: []
key-decisions:
  - "The owner-approved control contract is 1.1.0; the separate VLink hash drift remains outside that approval."
  - "Activation uncertainty is retained durably and cannot be cleared by timeout or process restart."
  - "Installer rollback restores compatible state and the previous binary only when migration reversibility is proven."
  - "The test Hub and process-local Engine apply manager are not represented as production authority or complete route hot-apply."
duration: 2h
completed: 2026-11-05
status: complete
---

# Phase 5 Plan 05-05: Close Control Contract and Upgrade Gaps

Implemented the owner-approved `vortex.control.v1` 1.1.0 contract and connected control-stream updates to schema validation, durable Agent reconciliation and the Engine Prepare/Commit/Abort transaction boundary. Added a signed, staged file installer with disk and compatibility preflight, bounded drain, atomic activation, health verification, crash recovery and migration-aware rollback.

## Verification

- Native macOS arm64: `make phase5-wave2-check` passed on `a1ab77c637f1a2c523f9d94a6d16db3d2bc0676a`.
- Docker Linux arm64: `make phase5-wave2-check` passed on the same SHA using `golang:1.27.3`.
- GitHub Actions Ubuntu 24.04 amd64: Wave 1 and Wave 2 passed on the same SHA; run `42026504229`.
- Agent/Hub and Engine apply/runtime race tests passed.
- Control Protobuf generation was reproducible; the approved control hash, requirements map, contract tests and SemVer change check passed.

Detailed machine reports and raw logs are in [05-05-EVIDENCE.md](05-05-EVIDENCE.md).

## Remaining Boundaries

Production Hub enrollment/revocation authority, service-level installer orchestration and applying full runtime payloads to business routes remain outside this plan's implementation claims. Phase 4 qualification remains separate.

## Self-Check: PASSED

- Exact contract approval is recorded and the VLink drift was not implicitly approved.
- Runtime updates validate before durable preparation, persist replayable outcomes, and retain activation-unknown through SQLite reopen.
- File installation tests cover signatures, digest, disk/config preflight, bounded drain, repeated upgrades, retry after staging, atomic activation, healthy restart, uncertain activation and crash-safe rollback.
- Native, Docker and Actions reports all refer to the same clean implementation SHA and all pass.
