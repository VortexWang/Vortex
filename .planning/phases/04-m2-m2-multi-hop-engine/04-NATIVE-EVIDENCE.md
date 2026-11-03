# Phase 4 Native Linux Evidence

**Host:** `VM-HKG1-AGAU3JCGKD`
**OS:** Ubuntu 24.04.5 LTS
**Kernel:** 6.8.0-142-generic
**Architecture:** amd64
**Resources:** 4 vCPU, 7.7 GiB RAM
**Go:** 1.27.3
**Docker:** 29.1.3
**Commit:** `930c6303099b97c07ad73467858da6757ecc45ac`

The host used the temporary test-only sysctls `net.core.rmem_max/default` and
`net.core.wmem_max/default=67108864` while running the UDP fixture. These are
host qualification settings, not product defaults.

## Qualification attempt from runbook

The host satisfies the base OS/toolchain requirements, but the repository has
no executable three-relay runtime or compose topology. The available native
entry points are Engine/VLink tests and the loopback UDP fixture, so the full
three-relay production-accounting workload could not be started. This attempt
is recorded as incomplete rather than treated as a qualification pass.

## Results

- `make phase4-m2-check`: passed, including Engine race, targeted state-machine tests and Docker topology smoke.
- VLink tracer forward: `793.57 MB/s`.
- VLink tracer reverse: `743.42 MB/s`.
- TLS baseline: `1210.17 MB/s`.
- VLink mux forward: `497.94 MB/s`.
- VLink mux reverse-established: `409.81 MB/s`.
- First-byte mux: median extra `287 us`, median first byte `374 us`.
- First-byte prewarmed pool: median extra `329 us`, median first byte `403 us`.
- UDP loopback fixture: `20,000` x `1,200` bytes at target `100 Mbps`,
  `received=20000`, `loss_pct=0.0000`, duration `21.69s`.

## Qualification Disposition

This is native amd64 evidence, but it does not qualify all Phase 4 performance
gates. Mux forward exceeds the 400 Mbps absolute target, while `497.94 / 1210.17`
is about 41.1%, below the 70% same-path baseline criterion. First-byte is below
the 5 ms boundary at this host. UDP loss is zero for the loopback fixture, but
it is not the required three-relay topology with production accounting and
mixed sustained load. D-04 remains unapproved. Raw output is retained under
local `artifacts/phase4/native/benchmark/` and is ignored from source commits.
