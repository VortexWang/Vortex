# Native Independent TLS Comparison — 2026-11-04

## Identity and method

Plan 04-06, clean SHA `baa0a02864e1d9bb8a2146ac9a7bd8d6a6a2650b`.
The authorized Ubuntu 24.04.5 amd64 KVM host is the same 4-vCPU/7,894-MiB
host used for the native scale series. All roles and the driver share it.
No code changed for this run. The runtime input is scale trial 1, and the
comparison runs subsequent to that trial, not concurrently.

```sh
BACKEND=native GOMAXPROCS=1 VORTEX_QUALIFICATION_EVENT_LIMIT=10000000 \
MIXED_DURATION_SECONDS=60 MIXED_TCP_STREAMS=16 \
QUALIFICATION_DIR=/root/Vortex/artifacts/phase4/native-baa0a02-16x60-r1 \
COMPARISON_DIR=/root/Vortex/artifacts/phase4/comparison/native-baa0a02-16x60 \
make phase4-runtime-comparison
```

The independent five-process Go TLS 1.3 baseline uses the same route/config
with byte-copy forwarding and no VLink, mux or accounting. Runtime and TLS
manifests record the same clean SHA, environment and semantic hash
`6aa503225d1c43abf4c477424ca4a622fa4e439890ba90307dc7fbbbe77a1e6e`.
Each mixed phase uses 16 TCP streams, 60 seconds, 1,200-byte UDP datagrams,
and requested 100 Mbps UDP. Actual UDP sent is 624,952 runtime versus 624,961
TLS, both approximately 99.99 Mbps. This confirms comparable offered load.

## Throughput

| Measurement | Result |
|---|---:|
| Runtime TCP under mixed load | 1,086.41 Mbps |
| Independent TLS TCP under mixed load | 1,314.60 Mbps |
| Same-load runtime/TLS ratio | **82.64%** |
| Target comparison ratio | 70% |
| Runtime UDP loss | 0.048004% |
| Independent TLS UDP loss | 0% |
| Independent TLS TCP-only | 1,854.66 Mbps |
| Runtime TCP-only / TLS TCP-only | 81.68% |
| Runtime mixed / TLS TCP-only (cross-load diagnostic) | 58.58% |

The 70% same-load comparison target is met in this one 16-stream/60-second
diagnostic. The cross-load ratio compares different UDP workloads and does
not establish relative forwarding overhead. No independent TLS repetitions
were made in this step, so the result is not a repeatability claim.

## Latency and finite fault probes

- Fresh-client-connect to first-byte median, 50 samples after 5 warmups:
  runtime 3.1883 ms, TLS 6.1799 ms. This includes their different connection
  policies and does not establish equal TLS setup/stream reuse costs.
- Five established userspace segment echo RTT medians sum to 0.11032 ms.
  Runtime first-byte median minus that sum is 3.0780 ms. These methods are
  different; this is a diagnostic subtraction, not the NFR-007 extra RTT.
- Deterministic 64 MiB payload SHA-256 matched.
- Unknown rule, future revision/generation, skipped hop, duplicate visited
  node and visited-local-loop requests were rejected; UDP link-loss probe
  passed. Direct bypass to relay-2 and destination was rejected.
- Terminating relay-2 stopped established TCP and UDP forwarding; new TCP
  did not forward and no automatic opaque replay occurred. No alternate
  authorized path was configured; this does not test path failover.

## Artifacts and next work

The command exited 0. Remote `sha256sum -c checksums.sha256` passed, the
worktree remained clean, and no test relay process remained. Full evidence
is at `/root/Vortex/artifacts/phase4/comparison/native-baa0a02-16x60`.
Reports, raw latency samples, logs, manifests and probe results were copied to
local `artifacts/phase4/comparison/native-baa0a02-16x60`; configs, certificates
and accounting JSONL remain remote. No new implementation needed CI in this
step; existing Actions 41813036017 checked the same source SHA, but its TLS
comparison uses the shorter workload and must not be called this scale test.

The shared KVM host does not establish exclusive resources or independent
physical links. This is a 60-second comparison, not 24-hour qualification.
NFR-003/005/006/007 and D-04 remain open for their unmet conditions. Next:
separate pool/mux connection and stream latency measurements, extend runtime
fault/unknown-outcome coverage, then review host eligibility and D-04. Phase 4
stays executing; no 04-06 completion summary is warranted yet.
