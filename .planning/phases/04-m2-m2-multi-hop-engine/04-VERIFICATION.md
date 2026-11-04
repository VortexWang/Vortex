---
phase: 04-m2-m2-multi-hop-engine
verified: 2026-11-04
status: human_needed
score: 4/5 phase-scoped criteria verified
behavior_unverified: 1
---

# Phase 4 Verification: Restarted Runtime-Backed Flow

## Goal Achievement

| # | Criterion | Status | Evidence |
|---|---|---|---|
| 1 | A single role-configured binary starts source, relay and destination processes | VERIFIED | `engine/cmd/vortex-relay`, strict config/TLS identity tests and native five-process readiness |
| 2 | Authorized TCP crosses exactly three relays with OPEN/OPEN_OK and half-close response | VERIFIED | `tools/phase4/topology-smoke.sh`, all five `OPEN authorized` logs, exact echo payload |
| 3 | Accounting reserves before writes and records successful/unknown outcomes | VERIFIED at tracer boundary | `runtime/accounting` race tests, destination JSONL evidence, qualification gate |
| 4 | Compose/native qualification entry refuses missing topology or accounting evidence | VERIFIED | `tools/phase4/qualification.sh`, native `qualification=topology-and-accounting-pass` |
| 5 | NFR-003/NFR-005/D-04 final performance qualification | NOT QUALIFIED | Current run has topology correctness and loopback UDP only; sustained three-relay mixed load, same-path ratio review and D-04 approval remain open |

## Automated Verification

- `cd engine && go test -race ./...`: passed after runtime and bridge changes.
- `make phase4-runtime-mixed BACKEND=native`: passed.
- Native topology smoke returned `vortex-three-relay-smoke` through three relays.
- `make phase4-m2-benchmark`: runtime gate passed, library benchmark and loopback UDP smoke passed; these results remain environment-limited for final NFR claims.
- GitHub Actions must run the new runtime-backed entry on the latest clean SHA before the restart can be closed.

## Open Gate

Run the same runtime-backed topology on the dedicated native Linux qualification
host with sustained mixed TCP/UDP traffic, production-shaped accounting event
reconciliation, same-path baseline ratio and D-04 review. Until that evidence
exists, Phase 4 remains `human_needed` and Phase 5 must not start.
