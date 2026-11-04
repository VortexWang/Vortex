---
phase: 04-m2-m2-multi-hop-engine
verified: 2026-11-04
status: gaps_found
score: 4/5 narrow phase criteria verified; relative performance and formal qualification remain open
behavior_unverified: 1
---

# Phase 4 Verification: Restarted Runtime-Backed Flow

## Goal Achievement

| # | Criterion | Status | Evidence |
|---|---|---|---|
| 1 | A single role-configured binary starts source, relay and destination processes | VERIFIED | `engine/cmd/vortex-relay`, strict config/TLS identity tests and native five-process readiness |
| 2 | Authorized TCP crosses exactly three relays with OPEN/OPEN_OK and half-close response | VERIFIED | `tools/phase4/topology-smoke.sh`, all five `OPEN authorized` logs, exact echo payload |
| 3 | Accounting reserves before writes and records successful/unknown outcomes | VERIFIED at tracer boundary | `runtime/accounting` race tests, destination JSONL evidence, qualification gate |
| 4 | Compose/native qualification entry refuses missing topology or accounting evidence | VERIFIED for five-second diagnostic | Clean `b71a9d0` passed local Compose, Ubuntu native and Actions native/Compose; full directional accounting and classified loss match. See 04-06-PROGRESS.md |
| 5 | NFR-003/NFR-005/NFR-006/NFR-007 and D-04 final qualification | NOT QUALIFIED | Latest clean `0b9615e` has Ubuntu mixed TCP 555.72 Mbps and UDP loss 0%, but runtime/TLS ratio 33.48% is below 70%; independent latency/fault checks pass finite diagnostic scope, while formal NFR-007, duration/host eligibility and D-04 review remain open |

## Current Automated Evidence

- Tested application SHA: `b71a9d0c9997b2237e151d3b4a08176b3db49cd1`; all recorded host metadata says `dirty=false`.
- Local `make phase4-m2-check`, including full Engine race, targeted cases and Linux Docker protocol regressions: passed.
- Ubuntu native full Engine race and artifact SHA-256 verification: passed.
- Same workload and semantic manifest passed runtime/accounting checks in local Compose, Ubuntu native and Actions native/Compose. Exact config hashes differ across environments and were not asserted equal.
- [Actions 41768502985](https://github.com/VortexWang/Vortex/actions/runs/41768502985): success with both backends and artifact upload.
- Full measured results and boundaries: [04-06-PROGRESS.md](04-06-PROGRESS.md). Latest clean-SHA results and comparison scope: 04-06-PROGRESS.md. Earlier smoke results remain historical evidence only.

## Open Gate

Plan 04-06 remains in progress. Improve measured mixed-load performance, add the independent same-path minimum TLS and latency baselines, complete runtime fault/per-flow/unknown-outcome coverage, and review D-04 and host eligibility. The five-second runtime diagnostics do not qualify NFRs or durable accounting. Phase 4 has gaps and Phase 5 must not start.
