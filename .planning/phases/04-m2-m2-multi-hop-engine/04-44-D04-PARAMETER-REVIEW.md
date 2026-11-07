---
phase: 04-m2-m2-multi-hop-engine
status: in_progress
locked: false
scope: Engine / in-process qualification adapter
---

# D-04 parameter review

This review applies the Phase 4 boundary in [04-CLOSURE.md](04-CLOSURE.md).
It does not approve production Agent/Hub parameters, change the frozen control
contract or claim PostgreSQL authority. Selection remains pending until the
complete native dynamic matrix and final formal gate are audited.

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
debt is retained: the matrix is an Engine/in-process candidate comparison,
and the formal run is a 50% candidate profile. No production default or
authority grant-return protocol is approved by this review.
