# Phase 4 Restart Decision

The previous Phase 4 planning, execution summaries, UAT and verification
artifacts are superseded by the owner decision on 2026-11-04. They remain under
`legacy-v1/` for audit history only and must not be used as current acceptance
evidence.

The phase is reopened from research. The replacement workflow must prove a
startable multi-process relay runtime and a reproducible three-relay topology
before running performance qualification. Library benchmarks, injected state
machines, loopback UDP, Docker smoke and hosted Actions results are supporting
evidence only until that topology exists.

Required sequence:

1. Research the missing runtime and topology integration.
2. Discuss and lock the Phase 4 implementation boundary.
3. Produce new plans with a production tracer slice before benchmark work.
4. Execute the plans and verify the runtime, topology, accounting and gates.
5. Re-run native Linux and GitHub Actions evidence from the replacement plans.
