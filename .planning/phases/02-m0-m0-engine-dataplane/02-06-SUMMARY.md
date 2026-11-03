---
phase: 02-m0-m0-engine-dataplane
plan: 06
status: complete
completed: 2026-11-03
requirements: [NFR-001, SUP-TEST-001]
---

# Plan 02-06 Summary: Linux Benchmark Evidence

## Delivered

- Added `BenchmarkTCPRelayBareTCP` using persistent real TCP loopback connections and the bare relay API; supported Linux builds use the splice implementation.
- Added a Linux Docker harness that records commit, source digest/cleanliness, topology, architecture, kernel, CPU/memory, command, duration, raw Go benchmark output, Mbps, and difference from 700 Mbps.
- Added distinct one-second CI benchmark smoke and full local benchmark targets; benchmark output does not alter correctness gate status.
- Retained two exploratory 2-second runs and one 15-second run. The 15-second run is on commit `b59565459db046c5f7b709d63bdd9a9c82c2ed08`, with clean benchmark sources and digest `a4bff06e6cadb66fcad15f9d81c29b7d429df3806f1ba7803cfe2e7e2bb6bdec`.

## Measurement

- Environment: OrbStack Linux VM, arm64, kernel `7.2.12-orbstack-00394-gbecdd6130068`, 10 vCPUs, 8,392,671,232 bytes RAM.
- Topology: host-local loopback client → relay → sink; this measures relay-path throughput, not external NIC throughput.
- Command: `VORTEX_BENCH_DURATION=15s make phase2-benchmark`.
- Raw result: `4,748.28 MB/s`, calculated as `37,986.24 Mbps`; difference from 700 Mbps: `+37,286.24 Mbps`.
- Status: `environment-limited`. OrbStack is virtualized and memory is below the 8-GiB reference capacity. This result is not qualified performance acceptance.
- Artifact: `artifacts/phase2/benchmark/20261103T095609Z.txt`.

## Verification

- `make phase2-engine-check` — passed locally after benchmark integration.
- `make phase2-benchmark-smoke` — passed locally.
- `make phase1-release-gate` — passed locally.
- Same-commit Actions correctness and smoke passed on commit `b59565459db046c5f7b709d63bdd9a9c82c2ed08`; phase2 run 41615021796 and phase1 run 41615021837.
- `cd engine && go vet ./...` — passed.

## Limits

The benchmark is a valid development comparison tool, but current evidence does not establish the 700 Mbps target on qualified hardware. A dedicated Linux run remains future acceptance evidence; a missed target alone does not block correctness progress.
