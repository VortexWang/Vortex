# Plan 04-06 execution progress

Status: **in progress**, not a completion SUMMARY. Phase 5 must not start.

Latest tested application commit: `349b70d145a707aaa083ed60d06b057edc0860e4` (clean on local Docker, native Ubuntu, and Actions).

## Implemented and exercised

- One runtime path carries TCP and UDP through exactly three relays. The shared entry runs TCP half-close/UDP payload smoke, a five-second four-flow TCP contention baseline and a five-second concurrent four-flow TCP / 100 Mbps, 1200-byte UDP workload.
- TCP generation runs in a separate process; payloads, response draining, target counters, successful execution bytes, returned reservations and end-to-end delivery are checked separately.
- WINDOW credit updates coalesce at bounded half-window thresholds. UDP send/receive queue saturation drops and counts datagrams, returns unconsumed credit and preserves authorized streams.
- Shutdown awaits source workers, tracked link closure and relay workers before the final accounting snapshot. Native launcher waits for all role exits; Compose launcher cancellation precedes container cleanup. Stale readiness cannot admit a previous topology.
- Application drop classes, host/container UDP kernel counters, exact and semantic config hashes, host metadata, reports and artifact checksums are recorded. Unexplained loss, unknown accounting, absent roles or mismatched totals fail the check.
- Candidate limits remain under review: stream window 1 MiB, aggregate 4 MiB, max credit 8 MiB, receive queue 128 frames; source queue 64 datagrams, up to 1024 source sessions; source/destination UDP receive-buffer request 4 MiB; adapter cap 1,000,000 events. This is not D-04 approval.

## Initial Same-SHA Evidence (Historical)

All rates are short diagnostic results; the table contains **no qualified NFR pass**.

| Environment | TCP-only Mbps | Mixed TCP Mbps | TCP retention | UDP lost / sent | Committed LTR / RTL bytes | Runtime reconciliation |
|---|---:|---:|---:|---:|---:|---|
| Local OrbStack Compose | 1148.54 | 1013.25 | 88.22% | 344/52,027 (0.661%) | 1,415,742,724 / 1,415,742,724 | pass |
| Ubuntu native | 627.39 | 366.22 | 58.37% | 7,711/52,037 (14.818%) | 680,604,036 / 680,604,036 | pass |
| Actions native | 982.56 | 561.01 | 57.10% | 2,192/52,038 (4.212%) | 1,028,086,372 / 1,028,086,372 | pass |
| Actions Compose | 788.41 | 343.70 | 43.59% | 10,430/52,049 (20.039%) | 765,288,564 / 765,288,564 | pass |

Semantic manifest SHA-256: `67446928a9e4d639179d344041ff864e9ed2daa2bdb0579e9e93d3eb3bcb449a`. Exact generated-config hashes differ because deployment paths, endpoints and ephemeral credentials differ; they were not asserted equal.

- Local `make phase4-m2-check`: passed on `b71a9d0c9997b2237e151d3b4a08176b3db49cd1`, including Engine race, targeted regressions and Linux Docker VLink checks. Local Compose checksum verification passed and no `qualification` containers remained.
- Ubuntu native `go test -race ./...`: passed; `sha256sum -c checksums.sha256` passed. Host: Ubuntu 24.04.5, Linux 6.8.0-142-generic, amd64, 4 vCPU / 7894 MiB. All five relay roles and the load generator share this host.
- Local Docker host: OrbStack 29.8.2, Linux arm64 VM, 10 CPUs / 8,392,671,232 bytes reported memory. First image pull encountered Docker Hub Bad Gateway; it was not counted as a test pass. The subsequent current-SHA Compose run passed.
- [Actions run 41768502985](https://github.com/VortexWang/Vortex/actions/runs/41768502985): success; shared check, native benchmark/mixed runtime, Compose mixed runtime and evidence upload. Raw artifact `phase4-evidence` is downloaded at `artifacts/phase4/actions-41768502985`.
- Dedicated native evidence is at `/root/Vortex/artifacts/phase4/qualification/native` on the authorized Ubuntu host; its JSON/checksum copies are at `artifacts/phase4/ubuntu-b71a9d0`. Local Compose evidence is at `artifacts/phase4/qualification/docker`.

## Previous clean-SHA validation (historical)

The following `0b9615e` measurements are historical and were superseded by the current comparison below. Compose runs the workload and independent baseline from a Linux client container on the fixture bridge, so host port-forwarding is not part of the measured UDP path.

| Environment | Mixed TCP Mbps | UDP loss | Independent TLS Mbps | Runtime / TLS ratio | Binary / fault / per-flow checks |
|---|---:|---:|---:|---:|---|
| Local OrbStack Compose | 1731.79 | 0.000% | 3452.90 | 50.15% | pass |
| Ubuntu native | 555.72 | 0.000% | 1659.81 | 33.48% | pass |
| Actions native | 533.14 | 0.000% | 1783.68 | 29.89% | pass |
| Actions Compose | 449.38 | 0.000% | 1462.24 | 30.73% | pass |

Actions run [41773089707](https://github.com/VortexWang/Vortex/actions/runs/41773089707) succeeded. The runtime probe rejected forged revisions/generations, duplicate visited nodes, skipped hops and direct bypasses; a 64 MiB deterministic file matched SHA-256 before fault injection; terminating relay-2 stopped established TCP/UDP forwarding and did not permit new TCP bypass. These are finite runtime checks, not exhaustive NFR-006 or failover qualification. Fresh first-byte samples and five segment RTT probes were recorded; NFR-007 remains not-qualified because pool/mux modes and the exact formal segment-RTT method are not both covered.

This short run was five seconds with four TCP flows and did not use a same-load independent TLS comparison. It is historical evidence only; it does not qualify NFR-003/NFR-005/NFR-006/NFR-007 or D-04.

## Current Same-SHA Comparison

Application commit `349b70d145a707aaa083ed60d06b057edc0860e4` is clean on local Docker, native Ubuntu, and Actions. The fair relative ratio uses runtime mixed-load TCP divided by independent TLS TCP under the same duration, TCP stream count, UDP rate, path, and runtime environment. `compare_results.py` now fails if those workload parameters differ. The legacy mixed-runtime / TLS-TCP-only ratio is retained as a cross-workload diagnostic, not as a measure of forwarding overhead.

| Environment | Runtime mixed TCP Mbps | TLS mixed TCP Mbps | Same-load ratio | Runtime TCP-only / TLS TCP-only | Legacy cross-load ratio | UDP loss |
|---|---:|---:|---:|---:|---:|---:|
| Local OrbStack Compose, `GOMAXPROCS=1` | 2454.88 | 2960.66 | 82.92% | 83.32% | 71.61% | 0.000% |
| Native Ubuntu, three runs | 817.95 / 879.38 / 822.88 | 1055.51 / 999.74 / 895.78 | 77.49% / 87.96% / 91.86% | 80.78% / 87.14% / 78.82% | 55.15% / 63.57% / 58.86% | 0.184% / 0.000% / 0.361% |
| Actions native | 613.31 | 544.84 | 112.57% | 80.71% | 48.58% | 0.433% |
| Actions Compose | 514.23 | 583.54 | 88.12% | 83.30% | 50.56% | 0.000% |

The same-load ratio cleared 70% in each recorded comparison, including all three native Ubuntu repetitions. The old cross-load ratio still ranges from 48.58% to 71.61%; that number compares different UDP/CPU workloads and is not a valid relative-overhead comparison. Actions run [41784760530](https://github.com/VortexWang/Vortex/actions/runs/41784760530) succeeded on the same SHA and uploaded both native and Compose evidence. Local Docker evidence is under `artifacts/phase4/qualification/docker` and `artifacts/phase4/comparison/docker`; Ubuntu native evidence is under `/root/Vortex/artifacts/phase4/native-349b70d` on the authorized host.

`GOMAXPROCS=1` is part of the current Phase 4 comparison fixture and is recorded in the semantic manifest; it is not a product default. These are five-second, four-stream diagnostics, not the long-duration dedicated-host qualification. UDP loss exceeded 0.1% in two of the three Ubuntu runs and in Actions native, so NFR-005 remains open. NFR-003, NFR-006, NFR-007 and D-04 also remain unqualified pending formal duration/host criteria, broader fault/latency coverage, and tuning review.

## Remaining work in 04-06

1. Continue profiling and optimize transport scheduling under native Linux; same-load ratios clear 70% at `GOMAXPROCS=1`, while the cross-load legacy ratio remains low and is not a fair comparison.
2. Reduce and stabilize UDP loss on native Linux; two of three repeated Ubuntu diagnostics exceeded 0.1%.
3. Extend same-load measurements to the formal duration, flow count, and dedicated host; current runs remain five-second, four-stream diagnostics.
4. Add independent same-path latency measurements for NFR-007; first-byte samples alone do not establish extra RTT.
5. Complete runtime fault/forged OPEN coverage for NFR-006 and per-flow reconciliation/unknown-outcome integration coverage. The in-process accounting adapter is not a durable production ledger.
6. Review D-04 after the above measurements and host eligibility checks. Do not promote these diagnostics to production accounting or performance acceptance.

No 04-06 SUMMARY or Phase 4 completion has been written. Existing Phase 4 legacy-v1 completion remains invalidated.
