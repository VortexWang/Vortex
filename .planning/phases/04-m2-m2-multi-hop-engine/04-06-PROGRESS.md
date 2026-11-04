# Plan 04-06 execution progress

Status: **in progress**, not a completion SUMMARY. Phase 5 must not start.

Tested application commit: `b71a9d0c9997b2237e151d3b4a08176b3db49cd1` (clean on every host).

## Implemented and exercised

- One runtime path carries TCP and UDP through exactly three relays. The shared entry runs TCP half-close/UDP payload smoke, a five-second four-flow TCP contention baseline and a five-second concurrent four-flow TCP / 100 Mbps, 1200-byte UDP workload.
- TCP generation runs in a separate process; payloads, response draining, target counters, successful execution bytes, returned reservations and end-to-end delivery are checked separately.
- WINDOW credit updates coalesce at bounded half-window thresholds. UDP send/receive queue saturation drops and counts datagrams, returns unconsumed credit and preserves authorized streams.
- Shutdown awaits source workers, tracked link closure and relay workers before the final accounting snapshot. Native launcher waits for all role exits; Compose launcher cancellation precedes container cleanup. Stale readiness cannot admit a previous topology.
- Application drop classes, host/container UDP kernel counters, exact and semantic config hashes, host metadata, reports and artifact checksums are recorded. Unexplained loss, unknown accounting, absent roles or mismatched totals fail the check.
- Candidate limits remain under review: stream window 1 MiB, aggregate 4 MiB, max credit 8 MiB, receive queue 128 frames; source queue 64 datagrams, up to 1024 source sessions; source/destination UDP receive-buffer request 4 MiB; adapter cap 1,000,000 events. This is not D-04 approval.

## Same-SHA evidence

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

## Remaining work in 04-06

1. Diagnose and improve native/CI mixed-load scheduling and receive-queue behavior. The Ubuntu run is below 400 Mbps, TCP retention is below 70%, and every environment exceeds 0.1% UDP loss in this recorded run. These are measured gaps, not waived thresholds.
2. Add an independent minimal TLS implementation on the same path and encryption configuration. The current TCP-only runtime run measures contention retention; it cannot establish the required minimum-TLS baseline ratio.
3. Add independent same-path latency measurements for NFR-007; first-byte samples alone do not establish extra RTT.
4. Complete runtime fault/forged OPEN coverage for NFR-006 and per-flow reconciliation/unknown-outcome integration coverage. Existing adapter tests establish unknown reservation retention only at their finite test boundary; the runtime currently uses a shared `qualification` flow label in its emitted accounting.
5. Review D-04 after the above measurements and verify host/path eligibility and longer duration. The current adapter is in-process/non-durable. Do not promote these diagnostics to production ledger or performance acceptance.

No 04-06 SUMMARY or Phase 4 completion has been written. Existing Phase 4 legacy-v1 completion remains invalidated.
