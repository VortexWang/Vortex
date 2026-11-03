---
phase: 02-m0-m0-engine-dataplane
status: complete
mode: correction-after-verification
date: 2026-11-03
---

# Phase 2 Discussion Log

## Why the phase was reopened

The initial Phase 2 flow produced three implementation plans and executed them, but it did not create a follow-up discussion, research refresh, plan-check result, or gap-closure plans after verification exposed missing gates, usage accounting, benchmark evidence, and bounded UDP admission. The phase is therefore reopened for a formal GSD correction cycle.

## Locked decisions

1. Keep the current M0 scope and four owner-approved targets: TCP half-close, Linux `splice`, UDP default idle timeout of 60 seconds, and a 700 Mbps direct-forwarding development target.
2. Continue correctness work when the benchmark is below target or the environment is constrained; preserve the raw result and measured gap.
3. Keep Engine independent of PostgreSQL, Valkey, Agent, Hub, and active control-plane calls. Gates and metering are injected in-process interfaces.
4. Count only successful destination writes. A reservation batch has exactly one terminal outcome: commit or return. Unknown acknowledgement keeps the conservative upper bound.
5. UDP session admission must be bounded globally and per client or equivalent deterministic policy. Saturation behavior must be explicit and tested.
6. Local Docker and GitHub Actions must run the same entry command against the same commit for each completed wave.
7. This correction cycle is documentation-first: research and plan checks must be recorded before more implementation changes are made.

## Scope boundary

The correction cycle closes M0 Engine dataplane gaps only. VLink, multi-hop, Agent/Hub, backend, frontend, and PostgreSQL/Valkey integration remain later phases.

## Deferred decisions

- Exact production quota values are not invented here; the injected gate contract uses explicit test fixtures and leaves policy values to the caller.
- The 700 Mbps target remains a development target, not a release claim.
