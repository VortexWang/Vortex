# Phase 4 GitHub Actions Evidence

- Workflow: `phase4-m2-multi-hop`
- Commit: `eb2737e43efe16ce09f60a5af6bd5e27b777ac8f`
- Run: [41757634273](https://github.com/VortexWang/Vortex/actions/runs/41757634273)
- Result: success; shared Phase 4 check, runtime benchmark smoke, host metadata and evidence upload all completed.
- Environment: GitHub-hosted Ubuntu 24.04, x86_64, Go 1.27.3, Docker 28.0.4.
- Scope: correctness/race suite, five-process TCP+UDP topology and exact accounting smoke, library benchmark, loopback UDP smoke. It does not run the sustained mixed TCP/UDP workload through three relays and does not qualify performance NFRs.

Artifact `phase4-evidence` records `tcp-udp-topology-and-accounting-pass` and
four committed destination events reconciling 28-byte UDP and 24-byte TCP
payloads in both directions. Performance remains environment-limited. This
workflow passed again on `eb2737e` after adding explicit failed-stream reset
cleanup; it adds no sustained mixed-load or performance-NFR coverage.

Earlier run `41744803772` on `86f06b3` failed in the unauthorized OPEN
integration test because a closed stream could mask a queued OPEN_ERR. Commit
`b4b484e` fixed that receive ordering race.


## Current mixed-runtime and Compose evidence

- Application SHA: `b71a9d0c9997b2237e151d3b4a08176b3db49cd1`, clean on both backends.
- [Run 41768502985](https://github.com/VortexWang/Vortex/actions/runs/41768502985): **success**, including full shared check, native benchmark/mixed load, Compose mixed load and `phase4-evidence` upload.
- Native mixed TCP: 561.01 Mbps; contention retention 57.10%; UDP loss 2,192 / 52,038 (4.212%). Compose mixed TCP: 343.70 Mbps; retention 43.59%; UDP loss 10,430 / 52,049 (20.039%). All drops classify; accounting reconciles on both backends.
- Green workflow means diagnostic correctness and complete evidence; performance NFRs remain `not-qualified` in both reports.
- Earlier run 41766416814 passed native checks but failed Compose reconciliation during final worker shutdown. It is not accepted as current pass evidence. Later worker-drain, kernel-loss classification and readiness/cleanup fixes are included in the tested SHA above.
- All host comparisons, bounds and remaining work are in [04-06-PROGRESS.md](04-06-PROGRESS.md).
