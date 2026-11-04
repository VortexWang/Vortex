# Native Linux Scale Regression — 2026-11-04

## Scope and identity

Plan 04-06 continued after the accounting/UDP fixes. All three runs used clean
`baa0a02864e1d9bb8a2146ac9a7bd8d6a6a2650b`, the same code previously checked by
local OrbStack and Actions [41813036017](https://github.com/VortexWang/Vortex/actions/runs/41813036017).
No production code changed during this verification.

Host: Ubuntu 24.04.5, Linux 6.8.0-142-generic, amd64, Go 1.27.3, 4 vCPU,
7,894 MiB RAM, AMD EPYC 7713 exposed through KVM. The five runtime roles,
echo target and load generator share this host. Independent physical links,
exclusive CPU allocation and a separate driver are not established.

Command (trial = 1, 2, 3):

```sh
BACKEND=native GOMAXPROCS=1 VORTEX_QUALIFICATION_EVENT_LIMIT=10000000 \
MIXED_DURATION_SECONDS=60 MIXED_TCP_STREAMS=16 \
QUALIFICATION_DIR=/root/Vortex/artifacts/phase4/native-baa0a02-16x60-r$trial \
make phase4-runtime-mixed
```

Each run includes a 60-second TCP-only contention baseline and a 60-second
mixed TCP/100 Mbps requested UDP phase. The workload generated approximately
99.99 Mbps actual UDP in every trial. All host manifests have `dirty=false`
and semantic hash `6aa503225d1c43abf4c477424ca4a622fa4e439890ba90307dc7fbbbe77a1e6e`,
matching the local Docker repair regression.

## Results

| Trial | UDP sent / received | UDP loss | TCP mixed Mbps | TCP contention retention | Accounting bytes each direction | Events |
|---|---:|---:|---:|---:|---:|---:|
| 1 | 624,952 / 624,652 | 0.048004% | 1,086.41 | 71.71% | 20,265,089,140 | 2,990,520 |
| 2 | 624,961 / 624,897 | 0.010241% | 1,169.10 | 77.00% | 20,909,995,236 | 3,047,275 |
| 3 | 624,962 / 624,934 | 0.004480% | 1,200.00 | 75.78% | 21,630,935,636 | 3,111,213 |

All workloads and qualification commands exited 0. Every TCP flow reconciled
and aggregate accounting equaled target-derived expectations. UDP losses were
fully classified as source queue drops (300 / 64 / 28); VLink window, send
queue, receive queue and observed kernel receive-buffer drops were zero.
Each trial remained below the 0.1% loss target at the measured offered rate.
`cd engine && go test -race ./...` passed after the series. All artifact
checksums passed on the remote host, the Git worktree stayed clean and no
test relay or echo-target processes remained.

## Evidence and limits

Full raw JSONL/config/certificate/log evidence remains on the authorized host
under `/root/Vortex/artifacts/phase4/native-baa0a02-16x60-r{1,2,3}`. Selected
JSON reports, logs, original checksum manifests and race output are copied to
local `artifacts/phase4/native-baa0a02-summary/`; this selected copy omits raw
JSONL, so the original checksums must be verified against the full remote set.

This confirms the repaired runtime/accounting behavior and repeatable UDP
loss below 0.1% on this topology. It does not qualify dedicated hardware,
independent link conditions, 24-hour stability, durable accounting, exhaustive
fault coverage or pool/mux latency. TCP retention above compares runtime
mixed load with runtime TCP-only load; it is not an independent TLS ratio.
NFR-003/005/006/007 and D-04 remain unqualified where their full conditions
are not established. Next: collect the independent TLS comparison with the
same 16 streams, 60-second phases and environment, then extend latency/fault
coverage. Do not complete Phase 4 or start Phase 5 from this series alone.
