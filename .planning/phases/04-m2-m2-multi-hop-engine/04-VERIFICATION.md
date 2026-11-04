---
phase: 04-m2-m2-multi-hop-engine
verified: 2026-11-04
status: human_needed
score: 3/5 phase-scoped criteria verified; runtime UDP smoke added, Compose and mixed-load gates remain
behavior_unverified: 1
---

# Phase 4 Verification: Restarted Runtime-Backed Flow

## Goal Achievement

| # | Criterion | Status | Evidence |
|---|---|---|---|
| 1 | A single role-configured binary starts source, relay and destination processes | VERIFIED | `engine/cmd/vortex-relay`, strict config/TLS identity tests and native five-process readiness |
| 2 | Authorized TCP crosses exactly three relays with OPEN/OPEN_OK and half-close response | VERIFIED | `tools/phase4/topology-smoke.sh`, all five `OPEN authorized` logs, exact echo payload |
| 3 | Accounting reserves before writes and records successful/unknown outcomes | VERIFIED at tracer boundary | `runtime/accounting` race tests, destination JSONL evidence, qualification gate |
| 4 | Compose/native qualification entry refuses missing topology or accounting evidence | PARTIAL | Native TCP+UDP topology and exact accounting reconciliation pass on `9ddb724`; Docker daemon unavailable locally, so Compose remains unverified |
| 5 | NFR-003/NFR-005/D-04 final performance qualification | NOT QUALIFIED | Native Linux runtime topology and accounting smoke passed; library benchmarks measured 722.37/711.69 MB/s tracer throughput and UDP loopback lost 0/20,000. Sustained three-relay mixed load, same-path ratio, fairness/reconciliation, NFR-007 and D-04 approval remain open |

## Automated Verification

- `cd engine && go test -race ./...`: passed after runtime and bridge changes.
- `make phase4-runtime-mixed BACKEND=native`: passed.
- Native Ubuntu 24.04.5 topology qualification on clean `9ddb724` returned exact TCP and UDP payloads through three relays; all five role logs confirmed OPEN authorization and destination accounting JSONL reconciled both directions and both network payloads.
- The same host completed library benchmark and UDP loopback smoke. Those measurements do not exercise sustained mixed load through the three-relay runtime and cannot qualify NFR performance.
- GitHub Actions run `41744803772` on `86f06b3` exposed a flaky queued-OPEN_ERR receive race. The fix is locally race-tested; Actions must pass on the fix commit.

## Open Gate

Run sustained mixed TCP/UDP traffic through the runtime topology on the native
Linux host, compare the same-path baseline, reconcile accounting and queue/drop
evidence, run the Compose backend, and review D-04. Actions on `9ddb724` is still
in progress. Until those checks are complete, Phase 4 remains `human_needed` and
Phase 5 must not start.
