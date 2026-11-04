---
phase: 04-m2-m2-multi-hop-engine
plan: 07
status: complete
completed: 2026-11-05
source_sha: a7a7432b11b5c3c7801e7fcfe3fe1664fe6c5b4e
actions_run: 41887199379
---

# Plan 04-07 Summary — Fail-Closed Source Authorization

Closed G-04-01 for the default runtime boundary. Source startup now requires
explicit qualification-only test mode or configured source authorization;
the missing-Gate bridge path rejects payload instead of forwarding it, and
static tracer policy no longer invents runtime, segment, share, or billing
authorization. Qualification fixtures opt in explicitly and record that
boundary in metadata.

## Verification

- Clean source SHA `a7a7432b11b5c3c7801e7fcfe3fe1664fe6c5b4e` passed local
  OrbStack arm64 `DOCKER_CONTEXT=orbstack make phase4-m2-check`.
- Same SHA passed the five-role Docker TCP/UDP smoke with reconciled payload
  counters; exact host mapping and outcome are in `04-07-EVIDENCE.md`.
- GitHub Actions [41887199379](https://github.com/VortexWang/Vortex/actions/runs/41887199379)
  completed successfully. All workflow steps passed, including race and shared
  checks, accounting/snapshot cases, mixed workload, pool/mux/auto modes, and
  authorized route-switch/restart recovery.
- Threat T-04-11 is closed for this default-boundary failure. T-04-02 remains
  open because the in-process qualification adapter is not durable Agent/Hub
  authority and does not prove full per-source conservation.

## Scope and Remaining Work

The passing checks are correctness and regression evidence. They do not
qualify NFR-003/005/006/007/039/040/044, approve D-04, or demonstrate production
accounting authority. Phase 4 remains executing with `gaps_found`; Phase 5 is
not started. See `04-07-EVIDENCE.md` and `04-VERIFICATION.md`.
