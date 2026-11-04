# Plan 04-06 execution progress

Status: **in progress**, not a completion SUMMARY. Phase 5 must not start.

Latest tested comparison commit: `3b9a04ce28de4b935b1f668cae4db5e88aec296b` (clean on local Docker and Actions). The native Ubuntu three-run series used application commit `349b70d145a707aaa083ed60d06b057edc0860e4`; the later `3b9a04c` change adds strict same-workload comparison checks and reporting.

## Implemented and exercised

- One runtime path carries TCP and UDP through exactly three relays. The shared entry runs TCP half-close/UDP payload smoke, a five-second four-flow TCP contention baseline and a five-second concurrent four-flow TCP / 100 Mbps, 1200-byte UDP workload.
- TCP generation runs in a separate process; payloads, response draining, target counters, successful execution bytes, returned reservations and end-to-end delivery are checked separately.
- WINDOW credit updates coalesce at bounded half-window thresholds. UDP send/receive queue saturation drops and counts datagrams, returns unconsumed credit and preserves authorized streams.
- Shutdown awaits source workers, tracked link closure and relay workers before the final accounting snapshot. Native launcher waits for all role exits; Compose launcher cancellation precedes container cleanup. Stale readiness cannot admit a previous topology.
- Application drop classes, host/container UDP kernel counters, exact and semantic config hashes, host metadata, reports and artifact checksums are recorded. Unexplained loss, unknown accounting, absent roles or mismatched totals fail the check.
- Candidate limits remain under review: stream window 1 MiB, aggregate 4 MiB, max credit 8 MiB, receive queue 128 frames; source queue 64 datagrams, up to 1024 source sessions; source/destination UDP receive-buffer request 4 MiB; adapter cap 1,000,000 events. This is not D-04 approval.

## Initial Same-SHA Evidence (Historical)

All rates are short diagnostic results; the table contains **no qualified NFR pass**.

| Environment | TCP-only Mbps | Mixed TCP Mbps | TCP retention | UDP lost / sent | Committed LTR / RTL bytes | Runtime reconciliation |
|---|---:|---:|---:|---:|---:|---|
| Local OrbStack Compose | 1148.54 | 1013.25 | 88.22% | 344/52,027 (0.661%) | 1,415,742,724 / 1,415,742,724 | pass |
| Ubuntu native | 627.39 | 366.22 | 58.37% | 7,711/52,037 (14.818%) | 680,604,036 / 680,604,036 | pass |
| Actions native | 982.56 | 561.01 | 57.10% | 2,192/52,038 (4.212%) | 1,028,086,372 / 1,028,086,372 | pass |
| Actions Compose | 788.41 | 343.70 | 43.59% | 10,430/52,049 (20.039%) | 765,288,564 / 765,288,564 | pass |

Semantic manifest SHA-256: `67446928a9e4d639179d344041ff864e9ed2daa2bdb0579e9e93d3eb3bcb449a`. Exact generated-config hashes differ because deployment paths, endpoints and ephemeral credentials differ; they were not asserted equal.

- Local `make phase4-m2-check`: passed on `b71a9d0c9997b2237e151d3b4a08176b3db49cd1`, including Engine race, targeted regressions and Linux Docker VLink checks. Local Compose checksum verification passed and no `qualification` containers remained.
- Ubuntu native `go test -race ./...`: passed; `sha256sum -c checksums.sha256` passed. Host: Ubuntu 24.04.5, Linux 6.8.0-142-generic, amd64, 4 vCPU / 7894 MiB. All five relay roles and the load generator share this host.
- Local Docker host: OrbStack 29.8.2, Linux arm64 VM, 10 CPUs / 8,392,671,232 bytes reported memory. First image pull encountered Docker Hub Bad Gateway; it was not counted as a test pass. The subsequent current-SHA Compose run passed.
- [Actions run 41768502985](https://github.com/VortexWang/Vortex/actions/runs/41768502985): success; shared check, native benchmark/mixed runtime, Compose mixed runtime and evidence upload. Raw artifact `phase4-evidence` is downloaded at `artifacts/phase4/actions-41768502985`.
- Dedicated native evidence is at `/root/Vortex/artifacts/phase4/qualification/native` on the authorized Ubuntu host; its JSON/checksum copies are at `artifacts/phase4/ubuntu-b71a9d0`. Local Compose evidence is at `artifacts/phase4/qualification/docker`.

## Previous clean-SHA validation (historical)

The following `0b9615e` measurements are historical and were superseded by the current comparison below. Compose runs the workload and independent baseline from a Linux client container on the fixture bridge, so host port-forwarding is not part of the measured UDP path.

| Environment | Mixed TCP Mbps | UDP loss | Independent TLS Mbps | Runtime / TLS ratio | Binary / fault / per-flow checks |
|---|---:|---:|---:|---:|---|
| Local OrbStack Compose | 1731.79 | 0.000% | 3452.90 | 50.15% | pass |
| Ubuntu native | 555.72 | 0.000% | 1659.81 | 33.48% | pass |
| Actions native | 533.14 | 0.000% | 1783.68 | 29.89% | pass |
| Actions Compose | 449.38 | 0.000% | 1462.24 | 30.73% | pass |

Actions run [41773089707](https://github.com/VortexWang/Vortex/actions/runs/41773089707) succeeded. The runtime probe rejected forged revisions/generations, duplicate visited nodes, skipped hops and direct bypasses; a 64 MiB deterministic file matched SHA-256 before fault injection; terminating relay-2 stopped established TCP/UDP forwarding and did not permit new TCP bypass. These are finite runtime checks, not exhaustive NFR-006 or failover qualification. Fresh first-byte samples and five segment RTT probes were recorded; NFR-007 remains not-qualified because pool/mux modes and the exact formal segment-RTT method are not both covered.

This short run was five seconds with four TCP flows and did not use a same-load independent TLS comparison. It is historical evidence only; it does not qualify NFR-003/NFR-005/NFR-006/NFR-007 or D-04.

## Current Same-SHA Comparison

The application code at `349b70d145a707aaa083ed60d06b057edc0860e4` was tested on native Ubuntu; the final comparison/reporting code at `3b9a04ce28de4b935b1f668cae4db5e88aec296b` was tested on local Docker and Actions. The fair relative ratio uses runtime mixed-load TCP divided by independent TLS TCP under the same duration, TCP stream count, UDP rate, path, and runtime environment. `compare_results.py` now fails if those workload parameters differ. The legacy mixed-runtime / TLS-TCP-only ratio is retained as a cross-workload diagnostic, not as a measure of forwarding overhead.

| Environment | Runtime mixed TCP Mbps | TLS mixed TCP Mbps | Same-load ratio | Runtime TCP-only / TLS TCP-only | Legacy cross-load ratio | UDP loss |
|---|---:|---:|---:|---:|---:|---:|
| Local OrbStack Compose, `GOMAXPROCS=1` | 2503.81 | 3075.30 | 81.42% | 84.13% | 72.78% | 0.000% |
| Native Ubuntu, three runs | 817.95 / 879.38 / 822.88 | 1055.51 / 999.74 / 895.78 | 77.49% / 87.96% / 91.86% | 80.78% / 87.14% / 78.82% | 55.15% / 63.57% / 58.86% | 0.184% / 0.000% / 0.361% |
| Actions native | 597.07 | 518.13 | 115.24% | 80.53% | 47.02% | 0.431% |
| Actions Compose | 517.59 | 570.31 | 90.76% | 84.23% | 50.65% | 0.000% |

The same-load ratio cleared 70% in each recorded comparison, including all three native Ubuntu repetitions. The old cross-load ratio ranged from 47.02% to 72.78%; that number compares different UDP/CPU workloads and is not a valid relative-overhead comparison. Actions run [41788441381](https://github.com/VortexWang/Vortex/actions/runs/41788441381) succeeded on `3b9a04c` and uploaded both native and Compose evidence. Local Docker evidence is under `artifacts/phase4/qualification/docker` and `artifacts/phase4/comparison/docker`; Ubuntu native evidence is under `/root/Vortex/artifacts/phase4/native-349b70d` on the authorized host.

`GOMAXPROCS=1` is part of the current Phase 4 comparison fixture and is recorded in the semantic manifest; it is not a product default. These are five-second, four-stream diagnostics, not the long-duration dedicated-host qualification. UDP loss exceeded 0.1% in two of the three Ubuntu runs and in Actions native, so NFR-005 remains open. NFR-003, NFR-006, NFR-007 and D-04 also remain unqualified pending formal duration/host criteria, broader fault/latency coverage, and tuning review.

## Remaining work in 04-06

1. Continue profiling and optimize transport scheduling under native Linux; same-load ratios clear 70% at `GOMAXPROCS=1`, while the cross-load legacy ratio remains low and is not a fair comparison.
2. Reduce and stabilize UDP loss on native Linux; two of three repeated Ubuntu diagnostics exceeded 0.1%.
3. Extend same-load measurements to the formal duration, flow count, and dedicated host; current runs remain five-second, four-stream diagnostics.
4. Add independent same-path latency measurements for NFR-007; first-byte samples alone do not establish extra RTT.
5. Complete runtime fault/forged OPEN coverage for NFR-006 and per-flow reconciliation/unknown-outcome integration coverage. The in-process accounting adapter is not a durable production ledger.
6. Review D-04 after the above measurements and host eligibility checks. Do not promote these diagnostics to production accounting or performance acceptance.

2026-11-04 update: the repaired `baa0a02` now has three clean native Ubuntu
16-stream/60-second repetitions at approximately 100 Mbps actual UDP. Loss was
0.048004% / 0.010241% / 0.004480%, all below 0.1%, with exact accounting and
TCP 1,086.41 / 1,169.10 / 1,200.00 Mbps. Native full Engine race and artifact
checksums passed. See [04-06-NATIVE-SCALE-EVIDENCE.md](04-06-NATIVE-SCALE-EVIDENCE.md).
The same-size independent TLS comparison subsequently completed: runtime
mixed TCP 1,086.41 Mbps versus TLS 1,314.60 Mbps, **82.64%** retention at
comparable actual UDP offered load (approximately 100 Mbps each). Finite
integrity, forged OPEN, direct bypass and middle-hop failure probes passed.
See [04-06-NATIVE-TLS-EVIDENCE.md](04-06-NATIVE-TLS-EVIDENCE.md). Next are
separate pool/mux latency and expanded fault/unknown-outcome measurements;
dedicated host/link eligibility and D-04 remain open.

## Ordered Follow-up: UDP Stability and Scale

The first follow-up added a bounded `UDPWindowReserve` of 512 KiB to each aggregate VLink window so TCP admission cannot consume the entire initial aggregate credit. Admission telemetry distinguishes per-stream and aggregate credit rejection; unit tests and `go test -race ./...` pass. This has not yet resolved UDP stability: a 10-second, four-stream OrbStack run on `9c35c78` lost 131 of 104,134 datagrams (0.126%), classified as window drops with no kernel receive-buffer errors.

The first formal-size attempt (16 TCP streams, 60 seconds, 100 Mbps UDP) on `41278c4` exposed fixture and runtime gaps. At the default one-million-event cap, accounting capacity exhausted and caused subsequent stream resets. A bounded test-only override was added (default unchanged; hard cap 10 million per execution process). With the cap set to 10 million, TCP completed without workload errors, but qualification failed: UDP loss was 1.699%; target counters recorded 49.89 GB while destination accounting committed 30.89 GB (left/right accounting totals were close to each other but did not reconcile with target totals). Logs also show a UDP idle timeout and stream reset. This is a failed run, not qualification evidence. Raw files are retained under `artifacts/phase4/qualification/docker` but are overwritten by the next local Docker run; archive them before rerunning.

The ordered follow-up is paused at item 2 until the high-concurrency accounting discrepancy, UDP timeout/reset, and packet loss are understood and corrected. Do not proceed to the independent latency item until the 16-stream/60-second runtime workload and accounting checks pass.

Follow-up diagnosis: increasing relay container `stop_grace_period` to two minutes allowed the 60-second-per-phase run to serialize all 6,015,013 accounting events. Accounting then reconciled exactly with target counters (53,980,247,076 / 53,980,015,476 bytes); the previous 30.89 GB artifact was a truncated shutdown evidence file, not a forwarding discrepancy. The same run still lost 8,822 of 624,944 UDP datagrams (1.412%), attributed to both stream and aggregate credit pressure. The fixture now rejects nonzero relay exit codes; relay evidence open/write/close errors are also fatal.

Increasing aggregate/stream credit isolated long-run stream-credit stalls. Accounting stored millions of events in one growing slice, whose large append copies stalled the single-core diagnostic. Fixed 4,096-entry chunks remove these full-history copies; JSONL output snapshots chunk headers instead of cloning all events. The final dirty-worktree 16-stream/60-second run sent and received all 624,959 UDP datagrams, with zero classified drops, TCP 3,159.85 Mbps and 84.42% contention retention. All 5,879,776 events reconciled exactly at 52,526,929,796 bytes in each direction and relay exit checks passed. This is diagnostic evidence pending clean-SHA confirmation. Candidate limits are 2 MiB stream, 8 MiB aggregate, 16 MiB hard credit cap, 2 MiB UDP reserve, 256 TCP receive frames and 2 MiB/2,048-frame UDP queues; UDP returns stream credit after one-quarter window while TCP keeps larger grants. These values remain subject to D-04 review and native qualification.

No 04-06 SUMMARY or Phase 4 completion has been written. Existing Phase 4 legacy-v1 completion remains invalidated.

## Clean-SHA Repair Confirmation

Repair commit `baa0a02864e1d9bb8a2146ac9a7bd8d6a6a2650b` passed the full Engine race suite and local `make phase4-m2-check`. Both hosts then ran 16 TCP streams and 60 seconds per phase with the bounded 10-million-event override:

| Environment | UDP sent/received | Actual UDP Mbps | Mixed TCP Mbps | Accounting events | Bytes per direction |
|---|---:|---:|---:|---:|---:|
| Local OrbStack, dirty=false | 624,959 / 624,959 | 99.993 | 3,151.92 | 5,865,532 | 52,301,813,636 |
| GitHub Actions Compose | 151,285 / 151,285 | 24.206 | 635.87 | 1,449,397 | 12,732,800,164 |

Both had zero classified drops, exact target/accounting reconciliation and clean relay exits. Local evidence is preserved in `artifacts/phase4/qualification/docker-baa0a02-16x60`; [Actions run 41813036017](https://github.com/VortexWang/Vortex/actions/runs/41813036017) succeeded with native and Compose checks, independent TLS/latency/fault probes, and the new scale regression. Actions requested 100 Mbps but generated only 24.21 Mbps; its zero loss proves correctness only at the measured offered load, not the 100 Mbps NFR. The local TCP retention was 84.71% versus its TCP-only contention baseline; Actions retention was 61.33%. Neither is the independent TLS relative-overhead ratio. Native host/rate eligibility, further latency/fault coverage and D-04 review remain open. Debug details: `.planning/debug/resolved/udp-accounting-drift.md`.


## Explicit runtime mode and unknown-outcome follow-up

Clean `03cc7f3` now connects the tracer source and every relay to bounded,
authenticated TCP pool/mux leases; UDP has a separate shared pool. Same-SHA
Docker and native Ubuntu mode matrices passed, including actual link reuse,
four exclusive TCP links under pool load versus one shared link under mux,
accounting reconciliation and finite integrity/OPEN/bypass/middle-hop probes.
Native first-byte medians are 1.6922 ms pool and 1.2833 ms mux; same-load
TLS ratios are 80.63% and 73.54%. Four runtime short-write cases preserve
unknown upper bounds after stream/session shutdown; full Engine race passed
locally and natively. Actions 41822272112 succeeded, including scale and mode regression. See
[04-06-MODES-EVIDENCE.md](04-06-MODES-EVIDENCE.md) for scope and remaining gates.
No formal NFR or Phase 4 completion is claimed.


## Automatic runtime mode follow-up

Clean `cba1bef` integrates bounded ten-minute TCP first-response history into
source and relay transport selection. UDP always stays mux and is excluded
from TCP history. A direction-specific regression proves requests do not emit
a source response sample and multiple target chunks emit only one. Full race
and same-SHA Docker/native/Actions matrices pass; every auto role selected
both mux and pool, with accounting reconciliation and finite fault probes.
Auto first-byte medians were 0.5250 ms Docker and 1.0947 ms native; same-load
TLS ratios were 83.51% and 95.01%, with actual UDP approximately 100 Mbps and
zero loss in both local/native five-second diagnostics.
[Actions run 41830048180](https://github.com/VortexWang/Vortex/actions/runs/41830048180)
succeeded. See [04-06-AUTO-EVIDENCE.md](04-06-AUTO-EVIDENCE.md). Broader recovery,
authorized-path failover, eligible-host duration and D-04/NFR review remain
open. Phase 4 is executing; no completion is claimed.


## Authorized runtime route and recovery follow-up

Clean `d0c1cb2` pins local route candidates at startup and accepts SIGHUP
selection only with increasing generation and successful authenticated prewarm.
Old TCP leases drain, old UDP contexts end, fresh old-generation OPENs fail,
and a spare middle relay with independent identity preserves all route hops.
Eight actual-process cases pass on local Docker and native Ubuntu, including
failed/unlisted/stale activation, old TCP half-close, primary/active relay stop,
no bypass, UDP non-replay and fresh-session recovery after spare restart.
Full race and shared checks pass.
[Actions 41836183498](https://github.com/VortexWang/Vortex/actions/runs/41836183498)
passed shared, scale/mode, recovery and artifact upload checks. Earlier recovery
CI attempts failed due container recreation in readiness counting and root-owned
evidence; both fixture defects are fixed and documented. See
[04-06-RECOVERY-EVIDENCE.md](04-06-RECOVERY-EVIDENCE.md). This is bounded local
tracer activation and finite recovery coverage, not durable Agent/Hub authority
or automatic health scheduling. D-04 and remaining NFR review are next; Phase 4
remains executing.


## D-04 review and remaining-NFR reconciliation — 2026-11-04

Completed 04-06-D04-REVIEW.md and 04-06-QUALIFICATION-MATRIX.md. Reviewed real
source wiring: source TCP uses nil Gate, destination memory accounting has no
funded credit balance, and TCP forwarding waits for OPEN_OK. Current Batch,
Snapshot/share/apply unit cases are narrower than NFR-039/040/044. These are
implementation/integration gaps, not all environment limitations. Retained
candidate D-04 values without freezing; defined a measured sweep and next
finite-issuer/source-gate slice. Updated stale 04-VERIFICATION.md and validation
commands/hash expectations. Clean 6bfe976 phase4-m2-check passed; log retained
under artifacts/phase4/review. Actions 41838307562 also succeeded on that same
6bfe976. No new benchmark, native Ubuntu run, parameter
lock, Phase 4 completion or Phase 5 transition is claimed.


## Finite TCP source credit slice — 6a1faf2

Injected startup-pinned finite raw-byte credit at the source TCP bridge;
implemented directional growing blocks, bounded local issuance/waterline,
write settlement, unused-tail return and unknown retention. Reject UDP/hot
routes in this mode instead of silently forwarding without authorization.
Docker/native clean 6a1faf2 both consumed exactly 262144 bytes in eight 16-KiB
echo exchanges through four local allocations; exhausted existing/new flows
sent no extra target bytes, no held/session leak. Semantic manifests match.
Both backends also passed all eight recovery cases; full race passed locally
and on native Ubuntu. Actions 41849553357 completed successfully on clean 6a1faf2, including
credit, race, mixed, mode, recovery and artifact-upload steps. Repaired a
prior readiness EOF race exposed by failed run 41840426926; intermediate
41849096326 was cancelled, not passed. See 04-06-CREDIT-EVIDENCE.md.
No production issuer, multi-snapshot/UDP credit or D-04 lock is claimed.


## Grant result identity and UDP source gate — d2f21f8

Implemented bounded trusted in-process BeginGrant/ApplyGrant records, pending
and unknown issuance occupation, idempotent same-result replay, late acceptance,
non-issuance proof boundary and snapshot/amount/grant-ID conflict rejection.
Wired both UDP write directions through the source Gate; explicit local queue
refusal returns the batch, ambiguous network writes freeze it. Receive workers
join before session.Close. Scarce balance safely reclaims only idle block tails
across sessions of this same authority. Clean d2f21f8 Docker/native passed TCP
and interleaved TCP/UDP 256-KiB exhaustion with matching semantic manifests,
plus full race / local phase4-m2-check. The first Docker attempt failed before
workload on registry-token EOF and remains recorded; same-SHA r2 passed.
Actions 41856742608 completed all steps successfully on clean d2f21f8;
parsed CI TCP/UDP credit results and semantic hashes match both hosts.
No durable/remote issuer, snapshot switching,
full NFR qualification or D-04 lock is claimed. See 04-06-GRANT-UDP-EVIDENCE.md.
