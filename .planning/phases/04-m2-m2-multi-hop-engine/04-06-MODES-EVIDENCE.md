# Runtime pool/mux and unknown-outcome evidence — 2026-11-04

## Implementation and identity

Plan 04-06 continues on clean source SHA
`03cc7f38b984100c5bca8871ff230adde8943987`. The standalone tracer now accepts
explicit `pool` and `mux` modes. Source and each relay use authenticated
TLS 1.3/HELLO links through bounded pools. TCP pool leases are exclusive for
the stream lifetime; mux streams share links. UDP uses its own shared pool.
Default bounds are one to four links per outgoing TCP or UDP pool; explicit
bounds are capped at sixteen. Omitted mode preserves the previous tracer.
Automatic mode/history selection and runtime idle-pool shrink are not wired.
These tracer fields do not change the separately versioned Agent contract.

Before this change the library pool existed, but the source opened a new TLS
connection for each TCP flow. Prior samples could not prove runtime pool/mux
latency. The new matrix checks actual mode configuration, opaque link/stream
identities, repeated authenticated link reuse, exclusive-pool expansion and
configured link caps. Each mode runs the five-process route separately.

## Reproduction and scope

```sh
BACKEND=docker DOCKER_CONTEXT=orbstack GOMAXPROCS=1 \
MODE_EVIDENCE_DIR="$PWD/artifacts/phase4/modes/docker-03cc7f3" \
make phase4-runtime-modes

BACKEND=native GOMAXPROCS=1 \
MODE_EVIDENCE_DIR=/root/Vortex/artifacts/phase4/modes/native-03cc7f3 \
make phase4-runtime-modes
```

Both commands exited zero, with `dirty=false` in both mode reports. Each uses
four TCP streams, five seconds per throughput phase, requested 100 Mbps UDP,
50 first-byte samples after five warmups, a separate minimal TLS baseline,
and five established segment echo RTT probes. Local Docker is OrbStack
Linux arm64; the native Ubuntu 24.04.5 amd64 KVM host has four vCPUs and
7,894 MiB RAM. All five roles and driver share their host. This is a shorter
mode regression, not a repeat of the earlier 16-stream/60-second series.

## Measurements

| Host | Mode | First-byte median ms | Median minus segment RTT sum ms | Mixed TCP Mbps | Actual UDP Mbps | UDP loss | Same-load runtime/TLS |
|---|---|---:|---:|---:|---:|---:|---:|
| OrbStack Docker | pool | 0.5266 | 0.4851 | 2613.79 | 99.90 | 0% | 86.54% |
| OrbStack Docker | mux | 0.5651 | 0.5341 | 2512.14 | 99.95 | 0% | 82.67% |
| Native Ubuntu | pool | 1.6922 | 1.5598 | 823.07 | 99.91 | 0% | 80.63% |
| Native Ubuntu | mux | 1.2833 | 1.1736 | 784.60 | 99.92 | 0% | 73.54% |

All source/relay outgoing TCP pools grew to four links under concurrent pool
load and remained at one link in mux mode. Sequential latency streams reused
links. All directional target/accounting totals reconciled and all classified
runtime drops were zero. The subtraction uses separately measured userspace
echo RTTs; it is diagnostic, not proof that OPEN adds no network round trip.
No thresholds in these samples establish full NFR-007 qualification.

## Fault and unknown-outcome checks

Both modes on both hosts passed deterministic 64 MiB SHA-256 integrity,
unknown-rule/future-revision/future-generation/skipped-hop/visited-list OPEN
rejection, direct bypass rejection, UDP link-loss and relay-2 process-stop
checks. Established forwarding stops, new TCP does not forward on the broken
route, and UDP is not transparently replayed. No alternate authorized path is
configured, so these cases do not establish failover or exhaustive NFR-006.

New race-enabled runtime tests exercise a real authenticated VLink, the runtime
target bridge and memory accounting adapter for TCP and UDP, each with short
writes returning nil or an error. Each case preserves exactly one unknown
reservation for the full requested byte upper bound, commits zero confirmed
bytes and does not resolve that reservation during stream/session shutdown.
The full Engine `go test -race ./...` passed locally and on native Ubuntu.
This proves four finite adapter cases; no crash-durable authority, disk-failure
coverage or authoritative Agent/Hub recovery is claimed.

## Artifacts and CI

- Local clean reports, raw samples, logs and checksums:
  `artifacts/phase4/modes/docker-03cc7f3`.
- Native full evidence: `/root/Vortex/artifacts/phase4/modes/native-03cc7f3`;
  reports, samples and logs copied to local
  `artifacts/phase4/modes/native-03cc7f3`. Private config/certificates and large
  accounting JSONL were excluded from the copy. All 55 JSON checksums passed. Actions uploaded evidence as artifact `phase4-evidence` in run 41822272112.
- Local Docker's 67 JSON checksums passed.
- Same-SHA [Actions run 41822272112](https://github.com/VortexWang/Vortex/actions/runs/41822272112)
  succeeded, including the 16-stream/60-second accounting regression and the
  explicit pool/mux mode matrix. The hosted runner requests 100 Mbps UDP; its
  actual delivered rate is recorded in the uploaded Actions artifact and is not
  assumed to meet the dedicated-host qualification condition.

## Remaining gates

Phase 4 and Plan 04-06 remain executing. These samples close the missing
explicit runtime mode evidence and add short-write unknown-bound coverage.
Automatic runtime mode selection, broader fault/recovery coverage, eligible
host/link conditions, duration/repetition requirements and D-04 review remain
open. NFR-003/005/006/007 are not newly marked qualified. The older Phase 4
completion remains invalidated; no 04-06 SUMMARY is created.
