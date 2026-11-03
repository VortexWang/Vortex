---
phase: 02-m0-m0-engine-dataplane
researched: 2026-11-03
status: complete-inline-correction
---

# Phase 2 Benchmark Research

## Findings

- No benchmark harness or throughput archive exists yet.
- Linux splice is selected only for bare `*net.TCPConn` pairs. A `net.Pipe` benchmark would exercise the portable copy path and would not measure the target path.
- A persistent TCP pair with a concurrent sink can measure payload forwarding without including connection setup in every sample. The benchmark should use `b.SetBytes` and preserve the exact Go test output.
- Current local Docker reports OrbStack, arm64, 10 CPUs, and 8,392,671,232 bytes of memory. This is virtualized and slightly below 8 GiB; any result is development evidence, not qualified performance acceptance.
- GitHub-hosted Actions is also a smoke environment. It can compile and run the benchmark briefly, but its result is not dedicated hardware evidence.

## Implementation guidance

- Add a Go benchmark using persistent real TCP connections and `relay.TCP`; ensure Linux uses the `*net.TCPConn` splice path.
- Run the benchmark binary in a disposable Linux Docker image, capture full stdout and environment in a timestamped artifact, and calculate Mbps and the difference from 700 Mbps.
- Mark OrbStack and hosted runner output `environment-limited`; reserve `qualified` for a documented dedicated Linux host meeting the project's hardware conditions.
- Keep a short benchmark smoke target in Actions separate from correctness status and from the full local benchmark target.
