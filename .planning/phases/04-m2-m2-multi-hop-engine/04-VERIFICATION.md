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
| 5 | NFR-003/NFR-005/NFR-006/NFR-007 and D-04 final qualification | NOT QUALIFIED | On clean `3b9a04c`, same-workload runtime/TLS ratios exceeded 70% in local Docker and Actions native/Compose; three native Ubuntu repeats on application SHA `349b70d` also exceeded 70%, all with `GOMAXPROCS=1`. Two of three Ubuntu repeats and Actions native exceed the 0.1% UDP-loss target. Runs are five-second diagnostics; formal host/duration, NFR-006/NFR-007 and D-04 gates remain open |

## Current Automated Evidence

- Latest tested comparison SHA: `3b9a04ce28de4b935b1f668cae4db5e88aec296b`; local Docker and Actions evidence records `dirty=false`. Native Ubuntu's three-run application evidence is on `349b70d145a707aaa083ed60d06b057edc0860e4`, also clean.
- Local `make phase4-m2-check`, including full Engine race, targeted cases and Linux Docker protocol regressions: passed.
- Ubuntu native full Engine race and artifact SHA-256 verification: passed.
- Same workload and semantic manifest passed runtime/accounting checks in local Compose, Ubuntu native and Actions native/Compose. Exact config hashes differ across environments and were not asserted equal.
- [Actions 41768502985](https://github.com/VortexWang/Vortex/actions/runs/41768502985): success with both backends and artifact upload.
- [Actions 41788441381](https://github.com/VortexWang/Vortex/actions/runs/41788441381): success on `3b9a04c`; native and Compose same-load TLS comparison plus artifacts passed. Diagnostic same-load ratios were 115.24% and 90.76% respectively.
- Native Ubuntu three-run same-load ratios were 77.49%, 87.96%, and 91.86%; the legacy cross-load ratios remained below 70%. Two runs exceeded 0.1% UDP loss. Exact run evidence is retained on the authorized host at `/root/Vortex/artifacts/phase4/native-349b70d`.
- Full measured results and boundaries: [04-06-PROGRESS.md](04-06-PROGRESS.md). Latest clean-SHA results and comparison scope: 04-06-PROGRESS.md. Earlier smoke results remain historical evidence only.

## Open Gate

Plan 04-06 remains in progress. The same-load relative throughput gate is met in short diagnostics with the current fixture settings; improve UDP loss stability, complete formal-duration and dedicated-host testing, add pool/mux latency coverage, complete runtime per-flow/unknown-outcome coverage, and review D-04 and host eligibility. These runs do not qualify NFRs or durable accounting. Phase 4 has gaps and Phase 5 must not start.
