---
phase: 04-m2-m2-multi-hop-engine
status: in_progress
locked: false
scope: Engine / in-process qualification adapter
---

# D-04 parameter review

This review applies the Phase 4 boundary in [04-CLOSURE.md](04-CLOSURE.md).
It does not approve production Agent/Hub parameters, change the frozen control
contract or claim PostgreSQL authority. The complete native dynamic matrix and both formal candidate gates are
audited below. Parameter approval and formal acceptance debts remain retained.

## Evidence inventory

| Evidence | Source / result | Limit |
| --- | --- | --- |
| Durable safety matrix | Clean `2543fb745d942ddf710c02d502a3675b9b3c7fe9`; 27 cases, each TCP + UDP, all pass | Finite in-process Agent/Hub/backend fixture; target floor equals ceiling, so target seconds do not change this workload |
| Closed SQLite audit | All 54 transport sets pass identity/payload/hash, source/delegation conservation and settlement checks | Closed fixture ledgers; no production PostgreSQL qualification |
| Dynamic native matrix | Same clean 2543fb7, 27 combinations × 3 repeats; full raw and independent audit pass | Fifteen seconds per load phase; not formal duration evidence |
| Formal candidate gate | Clean `0c2ba71`, 256 KiB / 30 s / 50%; 300 s warmup, 3 x 300 s trials, independent TLS; gate pass | Measured candidate profile, not an approved D-04 lock |
| Exact-head Actions | [42308608919](https://github.com/nodeox/VORTEX/actions/runs/42308608919), clean `0c2ba71c0653ca1c806dc3df1db93d847544011b`, passed | Hosted runner regression, not dedicated native performance |
| Same-SHA Docker | Clean 0c2ba71, 16-stream mixed accounting passed | Fifteen-second OrbStack diagnostic |

Durable scan summary SHA256:
`2227e52603dda9599114fe2c4868e26348d28c9f127ba69a4254229bf3bb4f2e`.
Independent SQLite audit SHA256:
`3eb8a17025a1c46f771916d16024e8a009a6b0fa1c57ab9beaa019515defbd73`.
Raw native evidence is retained at
`/root/Vortex-phase4-current/artifacts/phase4/d04-durable-2543fb7` and
`/root/Vortex-phase4-current/artifacts/phase4/d04-factorial-2543fb7`.

The Engine tree for edb1849c, 2543fb7 and 0c2ba71 is identical:
`dacb5480c65c5563ed9a954482766b270730263d`. Later independent auditors and CI
checkout/benchmark parsing changes do not change the measured Engine path.
Individual reports retain their actual commit identities.

## Candidate interpretation and selection criteria

The dynamic matrix crosses initial blocks 64/256/1024 KiB, target horizons
10/30/60 seconds and renewal waterlines 20/30/50 percent. Session blocks grow
up to 4 MiB. The rolling minute counts committed raw bytes only. The target is
`min(ceiling, max(8 MiB, ceil(rolling_committed_bytes * horizon / 60)))`.
The candidate ceilings are 125000000/375000000/750000000 bytes, derived from
the scanner's fixed 100 Mbps reference rate. They are explicit caps; the
horizon must not be described as an uncapped number of seconds at measured
aggregate TCP + UDP throughput.

Review must compare all three repeats for:

- Source conservation at every sampled instant, final settlement, exact
  per-flow byte accounting, no traffic errors and no unresolved liabilities.
- The actual configured factors and semantic/config digests. Cold-start
  samples before 58 seconds must match the consumption formula, including
  uncapped growth where observed. The 65-second post-load idle window must
  end at the 8 MiB target floor with no sessions, held or pending/unknown bytes.
- Sixteen-stream mixed throughput, paced UDP offered rate/loss and fifty
  short-flow samples per trial. The chosen profile must separately meet the
  formal duration/TLS gate; short candidate runs cannot establish that gate.
- Reservation amortization, issuer calls and latency histogram upper bounds,
  renewal merge rate, peak held/pending/unknown occupancy and idle block-tail
  reclaim. A zero merge rate under a synchronous issuer does not establish
  coalescing behavior; injected asynchronous tests provide that proof.

Smaller initial blocks bound new-session occupancy but may add allocations.
A longer target horizon/cap reduces renewal pressure while retaining more
issued credit. An earlier waterline provides more response headroom but can
increase issuer calls. Select from measured tradeoffs; neither the middle
candidate nor a single high-throughput sample automatically wins.

## Safety and measurement boundary

The dynamic issuer timing histogram measures synchronous process-local
allocation work. It excludes network transport, durable Hub commits and
production authority latency. The bounded reservation wait remains 200 ms;
injected-delay tests separately verify coalescing, cancellation, timeout,
late acceptance, definitive rejection and frozen unknown responses. Prior
600 ms pressure failures remain failures; they do not justify increasing the
wait bound or claiming availability under that issuer latency.

Idle target shrink changes future demand. Block-tail return moves held bytes
back into the same source's available balance. Neither is proof of issuer
credit return. Already issued or unknown authority is retained conservatively;
no grant-return protocol change is approved by this review. Production issuer
latency, lease release, durable occupancy and parameter requalification belong
to the Phase 5/6 handoff.

## Decision

## Completed comparison

The full matrix completed 81 trials (27 combinations, three repeats) on clean
`2543fb7`; the independent raw-workload, accounting, target-formula and
65-second idle-window audit returned `status=pass`. Summary SHA256 is
`135a289cdf96d46c4b3668befa45e87c289403c669541fe7fb5b1932abc4566a` and the
independent audit SHA256 is
`bfe95ec5ac574c6fbf529378c6f95f35c9f8ba1dab6927ecb7d529e444c7b9b4`.

The center profile (256 KiB / 30 s) had median TCP throughput 636.861 Mbps
and 41–42 grants per trial. The 50% waterline was similar in throughput but
used 58–59 grants per trial; 20% used 36 grants. The 10 s and 60 s horizons
showed the expected higher and lower grant counts. All audited trials had
source conservation, settlement, zero UDP loss, and idle target shrink to the
8 MiB floor. The timeline audit does not prove issuer credit return.

The queued 600 ms renewal-response pressure run failed all nine trials with
transport resets and/or UDP offered-load loss. Raw failures are retained under
`d04-delay-0c2ba71`; this is an availability boundary and does not justify
increasing the 200 ms reservation wait.

Formal candidate evidence was collected separately for 256 KiB / 30 s / 50%
on clean `0c2ba71`: 300 s warmup, three 300 s trials and independent TLS.
Trial TCP throughput was 637.626 / 634.512 / 630.515 Mbps; UDP offered rate
was 99.998 Mbps with zero loss in each. TLS mixed TCP was 578.600 Mbps and
the runtime/TLS ratio was 1.089726. The machine gate returned `pass`; gate
SHA256 is `5be4202a989dcb005011abe0f03e0184610519f2cf2b35c861829f7c6204baef`.
This formal result is evidence for the 50% candidate only and does not lock
the 30% center candidate.

`locked=false` remains intentional. The user-required D-04 parameter-lock
debt is retained: the matrix and both formal runs are Engine/in-process
candidate qualification evidence. No production default or
authority grant-return protocol is approved by this review.

## All candidate comparisons

Each row summarizes three runs. TCP and short-flow values are medians of the
three per-run results; occupancy is the maximum observed counter across them.
These are short candidate diagnostics, not statistical confidence estimates.
All rows passed raw accounting and had zero UDP loss. Pending grants remain
funded, and pending occupancy must not be described as unknown consumption.

| Initial KiB | Horizon s | Waterline % | TCP Mbps median | Short ms median | Grants per run | Peak held MiB | Peak pending MiB |
| --- | --- | --- | --- | --- | --- | --- | --- |
| 64 | 10 | 20 | 637.503 | 1.307 | 89/88/88 | 119.275 | 98.500 |
| 64 | 10 | 30 | 646.469 | 1.336 | 102/100/102 | 122.251 | 85.125 |
| 64 | 10 | 50 | 636.079 | 1.444 | 142/141/140 | 118.369 | 61.125 |
| 64 | 30 | 20 | 632.211 | 1.282 | 36/36/36 | 117.945 | 289.909 |
| 64 | 30 | 30 | 636.763 | 1.329 | 41/42/41 | 119.072 | 253.979 |
| 64 | 30 | 50 | 634.255 | 1.366 | 59/58/58 | 122.282 | 181.127 |
| 64 | 60 | 20 | 624.153 | 1.341 | 22/22/22 | 120.927 | 576.125 |
| 64 | 60 | 30 | 628.142 | 1.403 | 25/25/25 | 117.466 | 504.000 |
| 64 | 60 | 50 | 628.807 | 1.276 | 36/36/36 | 121.659 | 381.489 |
| 256 | 10 | 20 | 635.929 | 1.278 | 89/89/88 | 118.658 | 98.962 |
| 256 | 10 | 30 | 641.593 | 1.338 | 102/101/100 | 117.099 | 86.750 |
| 256 | 10 | 50 | 642.487 | 1.413 | 141/141/141 | 120.488 | 62.292 |
| 256 | 30 | 20 | 622.969 | 1.366 | 36/36/36 | 117.505 | 289.604 |
| 256 | 30 | 30 | 636.861 | 1.252 | 42/42/41 | 115.201 | 254.000 |
| 256 | 30 | 50 | 638.836 | 1.429 | 58/59/58 | 121.578 | 182.354 |
| 256 | 60 | 20 | 645.270 | 1.363 | 21/22/22 | 119.262 | 580.942 |
| 256 | 60 | 30 | 631.386 | 1.526 | 24/24/24 | 116.997 | 504.520 |
| 256 | 60 | 50 | 625.884 | 1.248 | 35/35/35 | 121.134 | 360.661 |
| 1024 | 10 | 20 | 629.003 | 1.281 | 88/87/87 | 118.183 | 98.753 |
| 1024 | 10 | 30 | 636.887 | 1.318 | 103/103/102 | 123.702 | 87.250 |
| 1024 | 10 | 50 | 635.020 | 1.301 | 144/143/144 | 121.567 | 63.147 |
| 1024 | 30 | 20 | 633.006 | 1.234 | 37/37/37 | 119.516 | 289.000 |
| 1024 | 30 | 30 | 642.916 | 1.394 | 42/42/42 | 125.682 | 256.760 |
| 1024 | 30 | 50 | 632.870 | 1.316 | 59/60/60 | 124.978 | 192.492 |
| 1024 | 60 | 20 | 634.523 | 1.288 | 23/23/22 | 119.047 | 576.051 |
| 1024 | 60 | 30 | 642.719 | 1.464 | 27/26/26 | 121.078 | 504.000 |
| 1024 | 60 | 50 | 643.717 | 1.214 | 37/38/38 | 119.703 | 361.500 |

## Center candidate formal follow-up — 2026-11-07

Clean `0c2ba71c0653ca1c806dc3df1db93d847544011b` also completed a separate
256 KiB / 30 s / 30% candidate run in
`/root/Vortex-phase4-final-0c2ba71/artifacts/phase4/formal-provisional-256k-30s-30pct`.
The 300-second warmup and three 300-second trials use semantic digest
`84a3e5c9e6efe6b8496ba6cd1a33fe92ecf9d5cad3c4e0182a14e6a03fec2de6`.
Trial TCP throughput was 628.675400 / 631.912733 / 636.537656 Mbps; actual
UDP rates were 99.998368 / 99.998112 / 99.997408 Mbps with zero loss. Each
trial conserved and settled source credit. Independent TLS mixed TCP measured
568.820579 Mbps; the runtime/TLS ratio was 1.119048221. The machine gate
returned `pass`, runner exit was 0, and socket maxima restored to 212992 each.

The gate SHA256 is
`5be4202a989dcb005011abe0f03e0184610519f2cf2b35c861829f7c6204baef`.
(The 30% and 50% gate files contain the same verdict fields and therefore have
the same hash; they do not identify a workload.) The 30% comparison SHA256 is
`9a427d3e8fc5e1ada784c82218ceafbd550f563570e5ef9f4e0f3a6e2557d81f`;
trial-1 host SHA256 is
`dffe1deb3456fb01f5d665163b2d24bf3ee2f2b3147efd794a964a3a25bfb206`.

The center candidate now has its own formal-duration evidence. The comparison
supports recommending 256 KiB initial / 4 MiB maximum block, 8 MiB demand
floor, 30 s horizon with explicit 375000000-byte cap, 30% renewal waterline,
and unchanged 200 ms reservation wait for this Engine/in-process adapter.
This is a measured recommendation, not a production parameter approval.
The user-required formal qualification and D-04 lock debts remain retained;
`locked=false` / `gaps_found` are unchanged. Production persistence authority
and requalification remain Phase 5/6 handoffs.
