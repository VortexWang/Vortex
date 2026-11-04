# Runtime auto transport evidence — 2026-11-04

Plan 04-06 continues on clean source SHA
`cba1bef165bb53aa6331199e0f02ab8e2997cd98`.

## Implementation and review

The tracer accepts optional `transport_mode: auto`. It reuses the existing
bounded first-packet history (up to 1,024 samples within ten minutes) and
selection policy: no history selects mux; median below ten seconds selects
pool; ten seconds or more selects mux. UDP always uses its separate shared
mux pool and does not populate TCP history. Explicit pool/mux and the omitted
legacy mode remain supported.

The source samples from accepted client connection to the first downstream
DATA response. Each relay samples from processing the incoming OPEN to the
first downstream response. Samples include setup/application delay and are
local adaptive policy inputs, not equal end-to-end performance measurements.
Selection applies to subsequent opens and does not migrate active streams.
Each peer retains its bounded pools across mode changes. There is no idle
shrink loop or active-session mode migration in this tracer.

The first diagnostic exposed UDP sample contamination at relays; the observer
now records only TCP. Review also caught the source observer attached to the
client-request direction; the final commit moves it to downstream DATA.
`TestSourceFirstTargetByteObserverIgnoresClientRequest` proves a request emits
no sample and repeated response chunks emit exactly one. Ten race-enabled
repetitions passed. Existing history tests cover the ten-second boundary,
missing/expired samples, bounded sample count and UDP selection. Full Engine
race passed locally and on native Ubuntu.

## Same-SHA mode matrices

```sh
DOCKER_CONTEXT=orbstack BACKEND=docker GOMAXPROCS=1 \
MODE_EVIDENCE_DIR="$PWD/artifacts/phase4/modes/docker-cba1bef" \
make phase4-runtime-modes

BACKEND=native GOMAXPROCS=1 \
MODE_EVIDENCE_DIR=/root/Vortex/artifacts/phase4/modes/native-cba1bef \
make phase4-runtime-modes
```

Both commands exited zero with `dirty=false`, four TCP streams and five seconds
per throughput phase, requested 100 Mbps UDP and 50 latency samples after five
warmups. Runtime and minimal TLS baselines use the same five-process route.
All source and relay auto logs include initial TCP mux followed by TCP pool;
UDP stays mux. Explicit modes retain their selected transport throughout.
The matrix fails for missing mode evidence, link cap violation, missing roles,
accounting mismatch, probe failures, dirty input or SHA mismatch.

| Host | Mode | First-byte median ms | Mixed TCP Mbps | Actual UDP Mbps | UDP loss | Same-load runtime/TLS |
|---|---|---:|---:|---:|---:|---:|
| OrbStack Docker arm64 | pool | 0.5341 | 2483.56 | 99.96 | 0% | 81.81% |
| OrbStack Docker arm64 | mux | 0.5638 | 2368.93 | 99.91 | 0% | 77.87% |
| OrbStack Docker arm64 | auto | 0.5250 | 2514.49 | 99.94 | 0% | 83.51% |
| Native Ubuntu amd64 | pool | 1.0703 | 1063.12 | 99.91 | 0% | 92.73% |
| Native Ubuntu amd64 | mux | 1.3188 | 880.59 | 99.93 | 0% | 81.09% |
| Native Ubuntu amd64 | auto | 1.0947 | 950.49 | 99.91 | 0% | 95.01% |

Directional accounting reconciled with target counters in every mode; runtime
classified drops were zero. Each mode passed 64 MiB SHA-256 integrity,
forged OPEN and bypass rejection, UDP link-loss and relay-2 process-stop checks.
These remain finite failure-stop probes without alternate authorized paths.
The short shared-host measurements fluctuate between trials; they do not
establish sustained throughput, hardware qualification or absence of OPEN RTT.

## Evidence and remaining work

- Local Docker evidence: `artifacts/phase4/modes/docker-cba1bef`;
  all 100 JSON checksums passed.
- Native full evidence: `/root/Vortex/artifacts/phase4/modes/native-cba1bef`;
  reports, samples and logs copied to local
  `artifacts/phase4/modes/native-cba1bef`; config, certificates and accounting
  JSONL excluded. All 82 JSON checksums passed.
- Same-SHA [Actions run 41830048180](https://github.com/VortexWang/Vortex/actions/runs/41830048180)
  succeeded, including shared correctness/race, 16-stream/60-second accounting
  regression, and all three modes. Its measured offered UDP rate is not assumed
  to be 100 Mbps. Artifacts remain uploaded in that run.

Phase 4 and Plan 04-06 remain executing. Runtime automatic selection is now
integrated, while broader fault/recovery and authorized-path failover coverage,
host/link/duration eligibility and D-04/NFR review remain open. No formal NFR
qualification, 04-06 SUMMARY or Phase 4 completion is claimed.
