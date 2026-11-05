---
phase: 05-m3-agent-edge-hub-m3-agent-and-hub
plan: 03
subsystem: agent-hub-authority
tags: [sqlite, grants, autonomy, accounting, protobuf, mtls]
requires:
  - phase: 05
    provides: Durable Agent store, Engine interface and approved control 1.1.0
provides:
  - Source-bound durable grants, leases, in-session renewal and restricted Hub issuance
  - Approved control 1.2.0 typed unknown-liability handoff
  - Online backend-before-ACK and autonomous retained-batch replay
  - Same-SHA full-disk TCP/UDP and raw SQLite verification
affects: [05-04, phase-06, accounting]
tech-stack:
  added: []
  patterns: [durable-reservation-before-write, typed-unresolved-liability, hash-bound-ack, immutable-delegation]
key-files:
  created:
    - agent/store/issuer.go
    - agent/store/runtime_renewal.go
    - agent/store/unknown_usage.go
    - agent/store/backend_usage.go
    - hub/usage.go
    - contracts/unknown_usage.go
    - tools/phase5/audit_usage_handoff.py
  modified:
    - agent/control.go
    - agent/usage.go
    - agent/runtime_authority_test.go
    - contracts/protobuf/v1/vortex.proto
    - docs/governance/contract-review.yaml
requirements-completed: []
key-decisions:
  - "Owner approved exact unknown-usage proposal 4f44708c18bd4086c49ee1fb9af611e23a98501ff9e52679e0a51995f8e38d67 as control 1.2.0."
  - "An ACK confirms transport durability; it never consumes or refunds an unknown liability."
  - "Online Hub ACK requires backend durability; autonomous ACK retains the batch for replay."
  - "Full signer S/R transfer and activation recovery remain in dependent 05-04."
coverage:
  - id: D1
    description: Durable source-bound grants, leases and live-session renewal
    verification:
      - kind: integration
        ref: agent/runtime_authority_test.go#TestRuntimeAutomaticallyUsesDurableLeaseAuthorityBeforeTarget
        status: pass
    human_judgment: false
  - id: D2
    description: Typed unknown liabilities survive lost ACK and restart without release
    verification:
      - kind: integration
        ref: agent/unknown_usage_test.go#TestUnknownLiabilitySurvivesBadAckLostAckAndRestart
        status: pass
    human_judgment: false
  - id: D3
    description: Online backend-before-ACK and retained autonomous replay
    verification:
      - kind: integration
        ref: agent/usage_handoff_test.go#TestUsageOnlineBackendAckAndAutonomousRetention
        status: pass
      - kind: integration
        ref: agent/usage_handoff_test.go#TestControlReportsNewUsageWithoutReconnecting
        status: pass
    human_judgment: false
  - id: D4
    description: Restricted autonomy, fixed deadlines and signer/path separation
    verification:
      - kind: unit
        ref: agent/store/issuer_test.go#TestIssuerConcurrentAutonomyConservesSource
        status: pass
      - kind: unit
        ref: agent/store/autonomy_test.go#TestAutonomyUsesFixedHeartbeatDeadlineAcrossRestart
        status: pass
      - kind: unit
        ref: agent/authorization_test.go#TestSignerEpochCannotTransferSourceWithoutHandoffEvidence
        status: pass
    human_judgment: false
  - id: D5
    description: Same-SHA full-disk and raw-ledger evidence
    verification:
      - kind: other
        ref: artifacts/phase5/authorization/ebff5b0/ledger-audit.json
        status: pass
      - kind: other
        ref: artifacts/phase5/authorization/ebff5b0/unknown-audit.json
        status: pass
      - kind: other
        ref: artifacts/phase5/authorization/ebff5b0/handoff-audit.json
        status: pass
    human_judgment: false
duration: multi-session
completed: 2026-11-05
status: complete
---

# Phase 5 Plan 03: Grants, Usage Handoff and Bounded Autonomy

Durable pre-write reservations now connect Engine traffic to source-bound Agent grants and restricted Hub issuance. Live TCP/UDP sessions renew without changing their billing snapshot or releasing older holds. The approved control 1.2.0 extension carries unknown liabilities separately from actual consumption, preserving exact payloads and unresolved balances through ACK loss and restart.

## Accomplishments

- Completed all four current plan tasks: durable authority/time, durable usage handoff, restricted autonomy/disk failure, and signer handoff guards.
- Added online backend persistence before Agent ACK, autonomous retained-batch replay, and periodic outbox delivery over an existing control stream.
- Preserved unknown liabilities, original grant ownership and failed-close session occupancy under crash, replay, conflicting data, full disk and authority expiry.
- Verified the same implementation SHA `ebff5b001b9a6d4323c042e6bd3d269be697aa70` in clean Docker arm64 and [Actions 42120412297](https://github.com/VortexWang/Vortex/actions/runs/42120412297), with 48 archived SQLite files independently audited.

## Commits and evidence

- Durable gate and runtime integration: `c8c0dc6`, `84dd896`, `1d900cd`, `d64fe72`, `cebe5ab`.
- Hub issuer, immutable ownership and renewal: `e702704`, `e04384d`, `04bd2bf`, `1ee2d03`, `86640e7`.
- Typed unknown/backend handoff and continuing reporting: `d8d0ac8`.
- Final ACK drain and deterministic fixture correction: `ebff5b0`.
- Full task-by-task audit: [05-03-VERIFICATION.md](05-03-VERIFICATION.md). Detailed history and limits: [05-03-EVIDENCE.md](05-03-EVIDENCE.md).

## Deviations and issues resolved

The approved 1.2.0 additive extension was necessary because the old consumed-byte message could not safely represent unknown liabilities. Review also found that local Hub persistence alone did not prove online backend handoff, and that newly produced usage waited for reconnection. Both paths were implemented and tested. The first clean Docker run exposed an EOF/duplicate-count timing issue; its failed evidence is retained, the defect was fixed, and both final hosts pass.

## Boundaries and next plan

This completes 05-03, not Phase 5 as a whole. The tested executable slice is the static destination runtime, and the backend is an explicitly marked protocol fixture. Production backend integration belongs to Phase 6. Prefix/remainder S/R transfer, activation-unknown orchestration and restart recovery are ready for 05-04. Historical VLink approval drift and the Phase 3 benchmark-report parser failure remain separately recorded; no Phase 4 qualification is claimed.

## Self-Check: PASSED

All four plan tasks have passing evidence. Contract proposal/source hashes agree with owner approval, no new dependencies were introduced, and the user's unrelated Phase 4/plan edits remain outside these commits. Ready for 05-04.
