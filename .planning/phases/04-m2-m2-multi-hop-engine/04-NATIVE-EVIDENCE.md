# Phase 4 Native Linux Evidence

**Host:** `VM-HKG1-AGAU3JCGKD`
**OS:** Ubuntu 24.04.5 LTS
**Kernel:** 6.8.0-142-generic
**Architecture:** amd64
**Resources:** 4 vCPU, 7.7 GiB RAM
**Go:** 1.27.3
**Docker:** 29.1.3
**Commit:** `9778eca4717ada93ab36f6dc0fcf62e6e75d715a`

## Results

- `make phase4-m2-check`: passed, including Engine race, targeted state-machine tests and Docker topology smoke.
- VLink tracer forward: `829.59 MB/s`.
- VLink tracer reverse: `695.87 MB/s`.
- TLS baseline: `923.78 MB/s`.
- VLink mux forward: `394.99 MB/s`.
- VLink mux reverse-established: `468.55 MB/s`.
- First-byte mux and pool benchmarks failed with `expected OPEN_OK before first byte`.
- No UDP 1,200-byte/100 Mbps loss run was produced by the current benchmark harness.

## Qualification Disposition

This is native amd64 evidence, but it does not qualify the Phase 4 performance gates: mux forward is below 400 Mbps and below 70% of the measured TLS baseline; first-byte benchmarks fail; NFR-005 UDP loss evidence is absent. D-04 remains unapproved. Raw output is retained under local `artifacts/phase4/native/benchmark/` and is ignored from source commits.
