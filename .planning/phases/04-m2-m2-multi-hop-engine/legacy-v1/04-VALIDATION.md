---
phase: "04"
slug: "m2-m2-multi-hop-engine"
status: draft
nyquist_compliant: true
wave_0_complete: false
created: "2026-11-04"
---

# Phase 04 - Validation Strategy

## Test Infrastructure

| Property | Value |
|---|---|
| Framework | Go `testing`, `go test -race`, fuzz tests, Docker topology fixtures, benchmark smoke |
| Module | `engine/go.mod` |
| Quick run | `cd engine && go test ./...` |
| Full phase check | `make phase4-m2-check` |
| Evidence | `artifacts/phase4/` with commit, dirty state, host/container architecture, topology, raw output and qualification status |
| Feedback target | <= 90 seconds for unit/race subset; mixed-load and three-relay benchmark reported separately |

## Verification Matrix

| ID | Scope | Requirements | Test | Command / evidence |
|---|---|---|---|---|
| 04-01 | Candidate path ordering, authorization, health and no-bypass | ENG-029, ENG-049, NFR-006 | unit/property | `cd engine && go test -race ./path -run 'Test(Path|Candidate|NoBypass)'` |
| 04-02 | Generation change, stale OPEN rejection, TCP drain and force-close deadline | ENG-027/028, ENG-032, NFR-039 | state-machine/integration | `cd engine && go test -race ./path ./vlink -run 'Test(Generation|Drain|Stale|ForcedClose)'` |
| 04-03 | UDP old-generation close and new OPEN without opaque replay | ENG-041, ENG-047, NFR-006 | integration | `cd engine && go test -race ./vlink -run 'Test(UDP.*Generation|Dgram.*Reopen)'` |
| 04-04 | Immutable session billing snapshot and coefficient isolation | ENG-035..039, NFR-044 | unit/property | `cd engine && go test -race ./accounting ./path -run 'Test(Snapshot|Grant|Coefficient|Disputed)'` |
| 04-05 | Unknown reservation upper bound and one-shot resolution | ENG-037, ENG-039, ENG-050 | unit/fault injection | `cd engine && go test -race ./accounting -run 'Test(Unknown|Resolver|UpperBound)'` |
| 04-06 | Share prepare, renewal closure, fencing/CAS, rate vs connection reduction | ENG-026/027, NFR-040 | state-machine/race | `cd engine && go test -race ./share -run 'Test(Prepare|CAS|Fence|Reduction|Drain)'` |
| 04-07 | Prepare/Commit/Abort idempotency and activation-unknown | ENG-029..034, NFR-039/040 | state-machine/fault injection | `cd engine && go test -race ./apply -run 'Test(Idempot|ActivationUnknown|Abort|Conflict)'` |
| 04-08 | TCP/UDP independent queues, 50% round cap and drop classes | ENG-048, NFR-005 | unit/race/mixed-load | `cd engine && go test -race ./scheduler ./vlink -run 'Test(Fair|Queue|Drop|Dgram)'` |
| 04-09 | Three-relay ordering, checksum, forged OPEN and no-bypass | NFR-003, NFR-006 | Docker integration | `make phase4-m2-check` |
| 04-10 | D-04 candidate parameter benchmark and first-byte overhead | NFR-003/005/007, ENG-037/050 | benchmark/evidence | `make phase4-m2-benchmark` |

## Wave 0 Requirements

- [ ] Add pure state-machine packages and fixtures with deterministic clocks/randomness.
- [ ] Add three-relay Docker topology and a mixed TCP/UDP traffic generator with sequence/checksum validation.
- [ ] Add `tools/phase4/check.sh`, `tools/phase4/benchmark.sh`, Makefile targets and pinned Actions workflow.
- [ ] Define artifact schema for environment-limited performance and native qualification gaps.

## Acceptance Boundaries

- Correctness tests may pass Phase 4 implementation work while NFR-003/005/007 remain `environment-limited` when the required native host is unavailable.
- Docker and Actions must run the same command on the same commit; neither result alone proves dedicated native performance.
- D-04 is not frozen by code defaults. The review artifact must include throughput, loss, queue occupancy, authorization wait, reservation churn and first-byte measurements before recording approved values.

## Manual-Only Verification

| Behavior | Reason | Required evidence |
|---|---|---|
| Native three-relay throughput and NFR-005 sustained loss | Hosted/virtualized resources may not meet dedicated path conditions | Native Ubuntu 24.04 host metadata, topology, duration, raw results, same-path baseline and signed review decision |
| D-04 final parameter approval | Requires engineering/product review of measured tradeoffs | Review record links to raw artifacts and selected values; no automatic promotion from benchmark output |

## Validation Sign-Off

- [x] Every planned slice has an automated verification boundary or explicit Wave 0 dependency.
- [x] No three consecutive tasks without automated verification.
- [x] Performance claims preserve environment and qualification status.
- [ ] Wave 0 infrastructure is implemented.
- [ ] Same-SHA Docker and Actions evidence is recorded.
