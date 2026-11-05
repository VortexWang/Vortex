---
phase: 04-m2-m2-multi-hop-engine
plan: 12
status: partial-slice-verified
---

# Remaining qualification work

## Completed safety fixes

Runtime commits `ea77a38` and `35c5c6b` correct two real gaps uncovered during
closure review:

- A live ShareGate session now rejects reservations at the absolute lease end,
  including expiry while the inner gate waits. It returns an unused reservation
  and retains unresolved/unknown consumption. Closing renewal ownership remains
  distinct from expiring an already valid lease.
- PolicyBoundary now changes the policy snapshot of an existing flow at T while
  retaining its original rule, execution point and path. Changing the active
  path before T does not cause the policy transition to roll it back. Pending
  batches keep their old resolvers; unknown liabilities remain in their original
  snapshot. Missing/ambiguous policy authority denies, and time rollback after
  activation cannot reopen the old policy.

The previous policy test explicitly expected post-T consumption from the old
snapshot. That assertion was incorrect and is replaced by tests for policy
transition, two paths x two policies, conservation and unknown retention.

Validation: local Engine `go test ./...`, full `go test -race ./...` (ea77a38),
targeted race checks after the rollback guard, and `go vet ./...` pass. The shared
`DOCKER_CONTEXT=orbstack make phase4-m2-check` passes on clean `35c5c6b`.
These are Engine/runtime checks, not proof of a persisted Agent/Hub ledger.

## Existing performance gate reconciled

The unchanged `fe2a4ba` raw measurements and corrected evaluator still pass
`qualification_gate.py`: three 300-second trials, 300-second warmup, finite
runtime source authority, zero UDP loss, and independent TLS ratio 0.737802.
NFR-003/005 passed on that measured build. Runtime changes in this plan require
a new same-build formal performance run before claiming the latest build qualified. The measured SHA is
not relabeled as either of the runtime safety commits above. See the complete
re-evaluation record in `../../quick/20261105-runtime-authority-gate/EVIDENCE.md`.
This is the existing single-host qualification contract; it does not add new
proof of per-node dedicated CPU/memory or physical independent link isolation.

## D-04 experiment design

Clean runtime SHA `d1ee7d2`, dedicated Ubuntu host `43.254.164.7`.
`tools/phase4/d04_run.py` runs seven actual one-factor configurations, three
repetitions each, using 50 fresh short flows plus five warmup connections, 16 TCP
streams, 15-second TCP-only and 15-second TCP+100 Mbps UDP phases. Every run
reconciles target bytes and source credit, checks zero outstanding/unknown/denial
state, and records hashes, actual throughput and raw latency samples.

The reference grant-rate input is explicitly fixed at 100 Mbps for controlled
comparison: 10/30/60 seconds mean 125/375/750 million bytes. It is not a claim
that the running >1 Gbps mixed stream has a 30-second adaptive grant. Timing
observations measure the synchronous process-local issuer only. Bounded log2
histograms report percentile bucket upper bounds, not exact quantiles. Small
issuer sample counts and scheduler/GC pauses limit p99 interpretation.

The old 27-entry generator remains a full-factorial inventory. Its incorrect
`one_factor_at_a_time=true` label is corrected; the actual seven-case executor
is the one-factor experiment.

### Completed D-04 scan results

All 21 runs completed, source credit conserved and settled, and UDP loss was
zero in every run. Median mixed TCP throughput across the three repeats:

| Changed factor | TCP Mbps | Short-flow median ms | Peak held bytes (max) |
| --- | ---: | ---: | ---: |
| Baseline: 256 KiB / 30 seconds / 30% | 1030.47 | 1.674 | 133658048 |
| Initial block 64 KiB | 1032.85 | 1.665 | 131230149 |
| Initial block 1024 KiB | 1031.93 | 1.678 | 135365144 |
| Grant reference 10 seconds | 1031.18 | 1.694 | 132925168 |
| Grant reference 60 seconds | 1033.88 | 1.688 | 132798728 |
| Waterline 20% | 1032.64 | 1.676 | 133387968 |
| Waterline 50% | 1034.22 | 1.689 | 132134456 |

The spread is small; these results do not establish a parameter winner or
justify changing a production default. The largest observed p99 issuer bucket
upper bound is 2.097151 ms, for the local allocator only. D-04 measurement of
these static candidates is complete; production lock remains open.

## TCP opening path correction

The delayed-path run on `d1ee7d2` found extra first-byte delay of 103.65 ms
(pool), 103.79 ms (mux), and 103.87 ms (auto) with 10 ms one-way netem delay.
The delayed ports were the four VLink legs and the exit target; the client
entry was not delayed. This is an out-of-profile stress diagnostic, not a
production latency qualification.

Code inspection confirmed `source.WaitOpen` and each relay's downstream
`WaitOpen` still serialized data despite the earlier low-level prefix support.
`91e409e` connects the actual TCP path: source credit is acquired before sending,
each locally authorized hop pipelines a bounded 64 KiB prefix, the receiver
consumes OPEN_OK independently, and excess early data waits within the existing
bounded windows. The explicit 64 KiB runtime pending limit replaces the prior
effective one-byte default, which also prevented useful early forwarding. UDP still waits for OPEN_OK. Batched writes now enforce the
same early-data limit, and FIN remains ordered when it precedes authorization.

A raw 100 Mbps Darwin trial failed with UDP loss/accounting mismatch and is
retained as a failed host-limited attempt. A reduced 1 Mbps native functional
trial reconciled all bytes. It is not used as Linux performance evidence.

### Current native validation — 91e409e

The dedicated machine completed all three modes with 500 microseconds of
one-way delay on each measured segment, preserving an undelayed client entry.
The five independently sampled median segment RTTs were each below 2 ms.
`latency_gate.py` recomputes medians from the raw samples and returns pass:

| Mode | First byte median ms | Sum of segment medians ms | Additional ms |
| --- | ---: | ---: | ---: |
| pool | 8.722570 | 6.601066 | 2.121505 |
| mux | 8.806085 | 6.643779 | 2.162306 |

Auto additionally measured 2.178 ms, but is not substituted for either required
mode. All three modes passed binary integrity, configured forged-OPEN and
bypass probes, and middle-hop failure checks. Explicit mode/pool-reuse assertions
in `modes.sh` passed. Each first-byte and RTT series retains 50 raw samples.
The delay harness, qdisc/filter dumps and raw JSON remain in the evidence tree.
The 10 ms stress run and 0.5 ms qualifying run have different network conditions;
their medians are not presented as a same-environment speedup comparison.

Native Ubuntu full Engine race and vet, finite TCP and UDP source-credit tests,
and immutable-snapshot runtime tests passed on `91e409e`. The same-source local
Docker shared check passed; 13 Python tests cover report gates, including missing
or inconsistent latency data and non-finite samples. No GitHub Actions run for
this new commit is claimed.

Artifacts are retained at `artifacts/phase4/d04-d1ee7d2`,
`artifacts/phase4/modes-d1ee7d2-delay`, `artifacts/phase4/modes-91e409e-delay`, and
`artifacts/phase4/closure-91e409e` on both the dedicated machine and this worktree
(compact JSON/metadata copy; large JSONL remains on the dedicated machine).
The local archive is `/tmp/phase4-closure-evidence.tar.gz`.
The host was checked after completion: both buffer maxima are back to 212992,
loopback qdisc is `noqueue`, and the remote worktree is clean.

## Remaining completion boundaries

D-04 cannot be marked production-locked solely from this sweep. Dynamic
one-minute rate estimation, cold/idle policies, delayed/lost issuer responses,
share drain occupancy, durable issuance and a final selected-combination trial
are not established by a static funded allocator. The scope is recorded rather
than labeling these observations as remote Hub measurements.

NFR-039/040/044 retain integration obligations: trusted persistent time,
pre-issue/missing-authority bounded request, persisted share ownership/CAS
receipts, restart recovery, and per-source durable snapshot accounting. These
are already assigned to Agent/Hub integration, but assignment is not a pass.
