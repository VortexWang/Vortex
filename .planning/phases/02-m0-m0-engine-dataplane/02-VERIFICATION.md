---
phase: 02-m0-m0-engine-dataplane
verified: 2026-11-03
status: gaps_found
score: 1/4 must-haves verified
behavior_unverified: 0
covered_files:
  - .planning/PROJECT.md
  - .planning/ROADMAP.md
  - .planning/phases/02-m0-m0-engine-dataplane/02-CONTEXT.md
  - .planning/phases/02-m0-m0-engine-dataplane/02-DISCUSSION-LOG.md
  - .planning/phases/02-m0-m0-engine-dataplane/02-RESEARCH.md
  - .planning/phases/02-m0-m0-engine-dataplane/02-RESEARCH-ADDENDUM.md
  - .planning/phases/02-m0-m0-engine-dataplane/02-01-PLAN.md
  - .planning/phases/02-m0-m0-engine-dataplane/02-01-SUMMARY.md
  - .planning/phases/02-m0-m0-engine-dataplane/02-02-PLAN.md
  - .planning/phases/02-m0-m0-engine-dataplane/02-02-SUMMARY.md
  - .planning/phases/02-m0-m0-engine-dataplane/02-03-PLAN.md
  - .planning/phases/02-m0-m0-engine-dataplane/02-03-SUMMARY.md
  - .planning/phases/02-m0-m0-engine-dataplane/02-04-PLAN.md
  - .planning/phases/02-m0-m0-engine-dataplane/02-04-SUMMARY.md
  - .planning/phases/02-m0-m0-engine-dataplane/02-05-PLAN.md
  - .planning/phases/02-m0-m0-engine-dataplane/02-06-PLAN.md
  - .planning/phases/02-m0-m0-engine-dataplane/CHECK-PLANS.md
  - .planning/phases/02-m0-m0-engine-dataplane/02-UAT.md
  - .planning/phases/02-m0-m0-engine-dataplane/02-VALIDATION.md
  - .planning/phases/02-m0-m0-engine-dataplane/02-REVIEW.md
  - .planning/phases/02-m0-m0-engine-dataplane/02-SECURITY.md
  - engine/relay/tcp.go
  - engine/relay/tcp_linux.go
  - engine/relay/tcp_portable.go
  - engine/relay/udp.go
  - engine/relay/udp_test.go
  - Makefile
  - .github/workflows/phase2.yml
covered_digest: "v4:sha256:9ca743844e1485c9ce04f978a5396355442a803399c1280c44c8b5990769c738"
verification_mode: inline-no-subagents
---

# Phase 2: M0 Engine Dataplane — Verification Report

**Phase Goal:** 用户可通过独立 Go Engine 建立直接 TCP/UDP 转发；门控与可信计量接口开启时满足 M0 指标。
**Status:** gaps found; implementation work may continue.

## Goal Achievement

| # | Truth | Status | Evidence |
|---|---|---|---|
| 1 | Engine is an independently testable Go module with the formal in-process integration boundary and runnable direct-forwarding entry needed by its consumers. | PARTIAL | The standalone `engine` module and relay package build and test. Current work delivers relay primitives; the complete integration boundary and product runnable entry are not verified by the current artifacts. |
| 2 | TCP forwarding preserves half-close and cancellation semantics, and Linux bare TCP selects the splice path. | VERIFIED | Automated coverage is recorded in `02-VALIDATION.md`; all four user UAT checks passed, including half-close, splice, UDP isolation, and UDP expiry/cancellation. Local and Actions Engine checks passed on SHA `88d59c220c536601ff22b9a5762362c6553e8a14` (Actions run 41604182140). |
| 3 | UDP sessions, gates, rate/connection limits, and usage batches enforce successful-write accounting and exactly-once commit/return behavior. | PARTIAL | Bounded UDP admission and injected Gate/Session/Batch behavior are covered by race-tested successful-write, short-write, denial, return, and unknown-outcome cases. The Engine intentionally does not provide durable quota/rate policy; authoritative Gate/Resolver adapters remain to be implemented by their owning runtime. The UDP resource threat is closed by Plan 02-04. |
| 4 | A Linux direct-forwarding benchmark is attempted and its environment, command, raw result, and gap to 700 Mbps are retained. | NOT VERIFIED | The benchmark has not been run. This is an explicit development target; a miss or constrained environment does not alone block continued correctness work. |

## UAT

`02-UAT.md` records 4/4 user-confirmed behaviors passed, 0 issues, and no pending UAT items. These checks establish the listed functional behaviors only; they do not establish M0 gate/accounting completion, bounded UDP resource use, production readiness, or throughput.

## Verification Evidence

- `make phase2-engine-check` passed locally and in GitHub Actions on SHA `88d59c220c536601ff22b9a5762362c6553e8a14`; Actions run: [41604182140](https://github.com/VortexWang/Vortex/actions/runs/41604182140).
- Phase 1 regression command `make phase1-release-gate` passed in the current local environment. The worktree was dirty, so this is not recorded as clean same-commit dual-host evidence.
- User UAT confirmations: 4 passed, 0 issues, recorded in `02-UAT.md`.
- Plan 02-04 bounded UDP admission passed local `make phase2-engine-check` and the same command on Actions run 41611109213, both on SHA `c79c2d1194a66711a68f66c2b5e7a5f474a8381d`.
- Plan 02-05 injected gate and usage-batch behavior passed local race, vet, Docker, and same-commit Actions checks on SHA `213c0d83674686cc07eb7dc3b9d6d3db2560a6c8` (run 41613449912).
- Code review: `02-REVIEW.md`, 1 critical and 1 warning finding.
- Security review: `02-SECURITY.md`, the high-severity UDP admission threat is closed; accounting boundary warning remains for Plan 02-05.
- 700 Mbps benchmark: not run; no performance claim is made.

## Gaps To Close

1. Finish and verify the Engine consumer integration contract and runnable entry required by the phase goal.
2. Implement authoritative Gate/Resolver policy adapters when the owning runtime is introduced; the Engine's injected contract and call ordering are tested, but persistence is outside this module.
3. Run the documented Linux benchmark when a suitable environment is available; retain raw evidence and measured distance from 700 Mbps (Plan 02-06). A missed target alone is not a progress blocker.

## Requirements Coverage

| Requirement area | Status | Notes |
|---|---|---|
| Direct TCP forwarding, half-close, cancellation, Linux splice | VERIFIED for tested behaviors | See validation strategy and UAT; no throughput claim. |
| Direct UDP routing, response isolation, idle expiry, cancellation | VERIFIED for tested behaviors | Resource admission is not bounded and remains a security gap. |
| Injected gate and usage batch boundary | PARTIAL | Call ordering and batch behavior pass tests; authoritative durable policy adapter is not part of the Engine module. |
| 700 Mbps direct-forwarding target | UNMEASURED | Benchmark not run; record evidence when an appropriate Linux host is available. |

## Verification Metadata

**Approach:** Goal-backward against Phase 2 roadmap success criteria.
**Verifier:** Inline in the main Codex window, honoring the user's instruction not to use agents.
**Phase disposition:** Continue Phase 2 correctness work. Do not mark the phase complete while the implementation and security gaps above remain open.
