---
phase: 04-m2-m2-multi-hop-engine
verified: 2026-11-04
status: human_needed
score: 4/5 phase-scoped correctness criteria verified
behavior_unverified: 1
---

# Phase 4: M2 多跳与引擎完备 Verification

## Goal Achievement

| # | Phase success criterion | Status | Evidence |
|---|---|---|---|
| 1 | Authorized healthy candidate paths, monotonic generations, no-bypass admission and TCP drain semantics | VERIFIED at Engine boundary | `engine/path`, injected VLink generation admission, race tests, UAT test 1, and same-SHA Docker/Actions checks passed. Dedicated three-relay throughput remains unqualified. |
| 2 | Immutable billing snapshots, coefficient isolation, reserve/unknown upper-bound behavior | VERIFIED at injected Engine boundary | `engine/accounting` snapshot tests, existing `Batch` unknown semantics, VLink admission tests and UAT tests 2-3 passed. Durable runtime reconciliation is later-phase scope. |
| 3 | Share fencing/CAS and idempotent Prepare/Commit/Abort without dual active generations or unsafe release | VERIFIED | `engine/share`, `engine/apply`, race tests, UAT tests 2-3 passed. |
| 4 | Independent bounded TCP/UDP scheduling and classified DGRAM drops | VERIFIED at transport boundary | `engine/scheduler`, VLink write-loop integration, race tests and Docker/Actions targeted tests passed. |
| 5 | NFR-003/NFR-005/NFR-006 and D-04 parameter review under qualifying topology | NOT QUALIFIED | Native amd64 evidence is now available, but mux forward measured 394.99 MB/s versus the 400 Mbps target and 70% baseline criterion, first-byte mux/pool benchmarks failed, and no NFR-005 UDP loss run was produced. See `04-NATIVE-EVIDENCE.md`; D-04 remains unapproved. |

## Automated Verification

- `cd engine && go test -race ./...` passed locally.
- `make phase4-m2-check` passed locally, including targeted state-machine tests and Docker topology smoke.
- GitHub Actions run `41728267582` passed on SHA `7899188e757a83d2ed9ec2c226f2e7ddb460cb02`, Ubuntu 24.04.5 amd64, `dirty=false`.
- `make phase4-m2-benchmark` records `status: environment-limited` and `benchmark_exit: 1`; native evidence is retained in `04-NATIVE-EVIDENCE.md`, but it does not meet all performance gates.
- UAT `04-UAT.md` has 4/4 tests passed with no issues.

## Requirements Coverage

| Requirement group | Status | Notes |
|---|---|---|
| ENG-026..034, ENG-049 | SATISFIED at injected Engine state-machine boundary | Share, path generation, apply fencing and admission behavior are implemented and tested. Durable Agent/Hub authority remains later scope. |
| ENG-035..041, ENG-047 | SATISFIED at accounting/VLink boundary | Snapshot identity, unknown upper bounds, OPEN/OPEN_OK and stream-scoped behavior have code/test evidence. |
| ENG-042..048 | SATISFIED at scheduler/transport boundary | Queue bounds, DGRAM integration, observability and existing VLink limits pass race/Docker checks. |
| ENG-050, NFR-039, NFR-040, NFR-044 | PARTIAL / boundary verified | Safety states and isolation are tested; runtime persistence and full failure recovery remain later integration work. |
| NFR-003, NFR-005, NFR-007 | NOT QUALIFIED | Dedicated native Linux topology and production accounting workload are unavailable in this run. |

## Human Verification / Open Gate

Re-run after fixing the first-byte benchmark and adding the native three-relay UDP loss workload with production accounting, concurrent TCP/UDP flows, sustained duration and a D-04 review decision. Until then the phase remains `human_needed` for performance qualification even though correctness UAT passed.
