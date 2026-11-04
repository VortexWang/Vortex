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

