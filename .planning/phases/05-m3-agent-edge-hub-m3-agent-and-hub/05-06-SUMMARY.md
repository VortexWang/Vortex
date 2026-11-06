---
phase: 05-m3-agent-edge-hub-m3-agent-and-hub
plan: '06'
subsystem: contracts
tags: [protobuf, lifecycle, approval, compatibility]
requires:
  - phase: 05-04
    provides: Existing S/R handoff and recovery semantics
  - phase: 05-05
    provides: Control apply and generated contract gates
provides:
  - Exact owner-approved control1.3.0, authorization1.2.0 and runtime1.1.0
  - Hash-guarded normative lifecycle and identity companion artifacts
  - Reproducible generated Go and paired same-source regression evidence
affects: [05-07, 05-08, 05-09, 05-12, 05-13]
tech-stack:
  added: []
  patterns: [exact-approval-before-freeze, companion-hash-gate]
key-files:
  created: [contracts/protobuf/v1/lifecycle.md, contracts/protobuf/v1/lifecycle.schema.json, contracts/protobuf/v1/identity-profile.json, contracts/lifecycle_test.go]
  modified: [contracts/versions.yaml, contracts/requirements-map.yaml, docs/governance/contract-review.yaml, tools/phase1/contracts.py, tools/phase5/contracts_check.sh]
key-decisions:
  - Approved candidate bytes copied unchanged; later normative changes require new review.
  - Schema approval does not close runtime gaps or confer backend authority.
requirements-completed: []
coverage:
  - id: lifecycle-contracts
    description: Approved typed lifecycle contract and additive compatibility
    verification:
      - kind: integration
        ref: tests/phase1/test_lifecycle_candidates.py
        status: pass
      - kind: unit
        ref: contracts/lifecycle_test.go#TestLifecycleGeneratedWireRoundTrips
        status: pass
    human_judgment: false
  - id: approved-freeze
    description: Owner-reviewed exact versions and normative companions are frozen
    verification:
      - kind: integration
        ref: tools/phase5/contracts_check.sh
        status: pass
    human_judgment: false
completed: 2026-11-06
status: complete
---

# 05-06 lifecycle contract prerequisite

Frozen the exact approved lifecycle contracts and companion semantics, regenerated Go with pinned tools, and enforced companion-content hash/version checks. Existing legacy wire tags and authorization1.1 branch are preserved.

## Task commits

1. Candidate proposal and executable fixtures: `cf2ef60872c02c97aae770762cd6b04ad31bb79c`.
2. Exact approval: owner reply “批准”; hash fc67a1ec0b2036530d836f0d4e4b59694f21498230d45128b1f4a96dcce36e3a.
3. Approved freeze and gates: `8ca29f833e2c7bccb2eba3e668d1511c25d4f726`.

## Verification

Same implementation SHA passed local Docker Linux arm64 and [Actions42150094526](https://github.com/VortexWang/Vortex/actions/runs/42150094526), including full Phase5 tests/race/recovery and pinned contract checks. Independent Linux re-audits of both raw ledger sets passed (5 snapshots/10 faults each). Native macOS contract/root/Engine tests passed; its system SQLite WAL audit failed and was replaced by a successful Linux re-audit, not relabeled. Exact files and evidence are in 05-06-EVIDENCE.md and artifacts/phase5/check/8ca29f833e2c7bccb2eba3e668d1511c25d4f726/.

## Deviations and remaining boundaries

Used the existing contracts/CHANGELOG.md instead of nonexistent root CHANGELOG.md; added wire/companion drift tests. No dependencies added. Financial and lifecycle runtime requirements are shared with unfinished plans, so requirements-completed remains empty. No new runtime gap, performance qualification or production backend authority is marked passed. Next dependency-ready plan: 05-07 regional control.

## Self-check: PASSED

All three tasks completed, exact approval precedes normative freeze, paired source/evidence verified, unknown debt and Phase4/Phase6 boundaries retained. Unrelated user changes preserved.
