---
status: resolved
trigger: Phase 4 benchmark and runtime qualification did not pass
created: 2026-11-05
updated: 2026-11-05
---

# Phase 4 Benchmark Failures

## Symptoms

- Native mixed workload can fail accounting reconciliation on Darwin when the
  host UDP receive path drops packets without a `/proc` counter.
- The VLink mux benchmark can hit `context deadline exceeded` while transferring
  its fixed 64 MiB payload.
- The standalone UDP loopback probe can report small loss at a nominal 100 Mbps
  rate on Darwin.

## Current Focus

- hypothesis: the mux benchmark's fixed 30-second context is too short for the
  diagnostic payload on a slow host; the product protocol is not necessarily
  stalled.
- test: run the mux benchmark alone and in the full benchmark sequence.
- expecting: the mux run completes when given a budget derived from payload size
  and a bounded minimum.
- next_action: replace the fixed deadline and pace loopback UDP in coarse batches;
  rerun native and Docker checks.

## Evidence

- timestamp: 2026-11-05
  observation: isolated mux forward completed in 27.279 seconds at 2.55 MiB/s.
- timestamp: 2026-11-05
  observation: full throughput benchmark previously failed at the 30-second
  context deadline, while a later full run completed after the same workload took
  about 30 seconds.
- timestamp: 2026-11-05
  observation: Darwin loopback UDP initially lost 145/20,000 packets; after the
  paced probe and the full Docker benchmark it reported 0/20,000 loss.
- timestamp: 2026-11-05
  observation: Docker Linux runtime mixed accounting reconciled exactly with
  zero classified loss and the full benchmark exited zero.

## Eliminated

- hypothesis: UDP socket tuning failure is the only cause
  reason: bounded fallback fixed the native UDP smoke timeout, but separate
  benchmark and host accounting failures remained.

## Resolution

- root_cause: fixed 30-second mux correctness deadline and single-threaded or
  unobservable host UDP receive paths caused diagnostic false failures; Darwin
  cannot expose the Linux drop counters required by the strict native gate.
- fix: use a two-minute mux transfer deadline, pace loopback UDP in bounded
  batches, move echo-target UDP receive to a dedicated thread, and document the
  Docker backend as the complete macOS benchmark path.
- verification: `DOCKER_CONTEXT=orbstack BACKEND=docker make phase4-m2-benchmark`
  passed; native Darwin remains correctly fail-closed when host drops cannot be
  classified.
- files_changed: engine/vlink/benchmark_test.go, tools/phase4/udp_loss.go,
  tools/phase4/echo_target.py, tools/phase4/README.md
