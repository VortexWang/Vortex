# Phase 4 GitHub Actions Evidence

- Workflow: `phase4-m2-multi-hop`
- Commit: `b4b484ea28b313e0969f063a7bb7820332401418`
- Run: [41750231407](https://github.com/VortexWang/Vortex/actions/runs/41750231407)
- Result: success; shared Phase 4 check, runtime benchmark smoke, host metadata and evidence upload all completed.
- Environment: GitHub-hosted Ubuntu 24.04, x86_64, Go 1.27.3, Docker 28.0.4.
- Scope: correctness/race suite, five-process TCP topology and accounting smoke, library benchmark, loopback UDP smoke. It does not run the sustained mixed TCP/UDP workload through three relays and does not qualify performance NFRs.

The preceding run `41744803772` on `86f06b3` failed in the unauthorized OPEN
integration test because a closed stream could mask a queued OPEN_ERR. Commit
`b4b484e` fixes that receive ordering race; this passing run verifies the fix.

