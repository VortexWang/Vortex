---
phase: 04-m2-m2-multi-hop-engine
verified: 2026-11-06
status: gaps_found
score: implementation closed; Phase 4 qualification gaps retained; durable integration transferred to Phase 5/6
behavior_unverified: 5
---

# Phase 4 Verification — current qualification review

## Current final-commit audit — 2026-11-07

The final branch commit `000fe6b7d470482152a9be73068d866f9678472e` now has
current-code evidence for the bounded Q2/Q3/Q4/Q5 slices in
[04-43-CURRENT-SHA-EVIDENCE.md](04-43-CURRENT-SHA-EVIDENCE.md). Q2's 16-case
attack matrix and eight recovery cases pass; Q3's snapshot and TCP/UDP credit
boundaries pass; Q4's 21 candidate trials pass conservation and remain
`locked=false`; and Docker `pool`/`mux`/`auto` mode regression passes.

These results close the finite implementation and tracer evidence slices only.
P4-Q1 native formal performance, P4-Q4 production D-04 locking, P4-Q5 native
controlled RTT, and persistent Agent/Hub authority remain explicitly open.
The overall verification status therefore remains `gaps_found`.

## Current scope decision — 2026-11-06

[04-CLOSURE.md](04-CLOSURE.md) defines the current Phase 4 gates P4-Q1..Q5.
Implementation is closed; qualification remains gaps_found. Persistent Agent/Hub
and production backend work belong to Phase 5/6, not Phase 4 closure blockers.
No previous failed check is promoted to pass.

## Bounded early TCP DATA increment — 2026-11-06

The runtime bridge now has an authenticated delayed-authorization regression
at `engine/runtime/relay/bridge_test.go`:
`TestBridgeOpeningConnBuffersTCPUntilRemoteAuthorization`. It sends a bounded
TCP prefix while OPEN is pending, verifies the remote stream exposes no DATA
before authorization, then authorizes the stream and verifies exact delivery to
the target with zero held, unknown, or open-session accounting on both sides.
The low-level VLink tests continue to enforce strict UDP OPEN/OPEN_OK gating.

This closes the finite implementation/evidence slice for authorized early TCP
DATA. It does not provide the controlled pool/mux RTT timeline, dedicated-host
resource evidence, or formal NFR-007 qualification; those remain open.

## Clean Docker runtime-authority evidence — 2026-11-06

The clean `e5bcb03f146ff2dba0400e4a03e43efcadc8027d` build passed the bounded
Docker runtime-authority mixed workload and the same-SHA comparison. Runtime
accounting reconciled exactly in both directions, source credit was conserved
and settled, and the finite forged-OPEN, bypass, 64 MiB integrity and relay-2
failure checks passed. The independent TLS comparison is retained with the
runtime-authority semantic manifest; its same-load TCP ratio was `0.5946`,
below the `0.70` diagnostic threshold. Details and output directories are in
`04-37-CLEAN-DOCKER-RUNTIME-EVIDENCE.md`.

This remains a five-second, four-stream Docker run on an OrbStack development
VM. It is not the dedicated Ubuntu host, 60-second warmup, repeated 300-second
trial set, or resource-isolated evidence required by P4-Q1. Therefore the
formal performance status remains `not-qualified`; no NFR-003/NFR-005 result
is promoted. The finite attack results improve the enumerated NFR-006 evidence
but do not claim exhaustive coverage.

The source authority is still an in-process qualification adapter. No Agent or
Edge Hub durable receipt, SQLite WAL recovery, PostgreSQL authority, or
persistent balance claim is made. D-04 remains `pending-review` and its
parameters remain unlocked.

## Current transport-mode regression — 2026-11-06

The final-code Docker mode sweep passed explicit `pool`, `mux`, and adaptive
`auto` on one clean SHA. It observed bounded link reuse, pool/mux selection and
finite fault/integrity coverage; the result is recorded in
`04-40-Q5-MODE-EVIDENCE.md`. This preserves Q5's Engine regression evidence,
but the native Linux controlled-RTT gate remains open and NFR-007 is not
promoted.

## Formal qualification gate audit — 2026-11-06

The actual qualification gate and direct host preflight both fail closed for
the current environment. The reasons and exact report paths are recorded in
`04-42-FORMAL-GATE-BOUNDARY.md`; no Docker diagnostic is promoted to P4-Q1.

## Rejected renewal increment — 2026-11-06

The delayed process-local issuer now has a regression for a definitive renewal
rejection. It preserves the budget invariant, returns the pending amount to
`unissued`, leaves `unknown_grant` at zero, and records a final failure rather
than a timeout. This closes the adapter's rejected-versus-unknown distinction
for the tested finite case only. It does not qualify remote issuance,
persistent authority, formal performance, or D-04 locking; see
`04-35-REJECTED-RENEWAL-EVIDENCE.md`.

The complementary delayed-accepted case also passes: a caller timeout leaves
the exact pending request occupied until its late accepted reply arrives, then
the grant settles once and can be consumed. This is finite adapter recovery
evidence only; it does not qualify a remote issuer or durable authority. See
`04-36-LATE-ACCEPTED-RENEWAL-EVIDENCE.md`.

The same commit passed `DOCKER_CONTEXT=orbstack make phase4-m2-check`, including
the VLink integration and UDP bounded-resource regressions. The result remains
runtime regression evidence and does not change the formal qualification status.

## Prior reconciliation — Plan 04-12

The latest closure evidence is [04-12-EVIDENCE.md](04-12-EVIDENCE.md).
The review text below this section is historical and retains its original SHA.

- `fe2a4ba` passed the existing formal performance gate; this is not a performance
  measurement of the later runtime corrections.
- `d1ee7d2` completed 21 real static D-04 trials, with source conservation and no
  UDP loss. Production parameter lock is still open.
- `91e409e` corrects live share expiry, policy snapshot transition and the actual
  early TCP data path. Native pool/mux additional first-byte medians are
  2.1215/2.1623 ms; the raw-data latency gate passes.
- Native Engine race/vet, source TCP/UDP credit, snapshot checks and local Docker
  pass. Finite integrity/bypass/forged-OPEN probes also pass on this runtime.
- Full NFR-006 coverage, persisted NFR-039/040/044 integration and a formal
  performance qualification of the final integrated runtime remain open.

Overall status remains **gaps_found**. No full Phase 4 acceptance is claimed.

## Historical reviews

Review source: `5bd384a` (Plans 04-08 through 04-11 code and evidence successor). Phase 4 stays **executing** because formal qualification and durable authority gaps remain. V1.11 is reference only. No old restart documents are required to continue implementation.

## Verified boundaries

| Behavior | Result | Evidence |
| --- | --- | --- |
| Single role-configured binary, authenticated five-process three-relay TCP/UDP topology | Verified tracer behavior | runtime command/config/link/relay; 04-01..05 summaries and mixed-load reports |
| Reserve before target write, successful-write commits, ambiguous-write upper bounds | Verified at in-memory tracer boundary only | runtime bridge/accounting tests and byte reconciliation; no authoritative credit balance or durable ledger claim |
| Explicit pool/mux/auto, real lease reuse, mode-specific first-target-byte samples | Verified finite mode integration | clean cba1bef Docker/native and Actions 41830048180; 04-06-AUTO-EVIDENCE.md |
| Authorized route selection, monotonic generation, old TCP drain, UDP reopen, middle-hop failure/restart | Verified eight finite process scenarios | clean d0c1cb2 Docker/native and Actions 41836183498; 04-06-RECOVERY-EVIDENCE.md |
| Harness rejects missing runtime/accounting and protects complete JSONL shutdown | Verified regression boundary | baa0a02 local/Actions scale checks, exact accounting, nonzero relay exit/evidence-write checks |
| Immutable billing snapshot selection and session affinity | Verified finite runtime slice | clean c8c2a37 native/Docker `tools/phase4/snapshots.py`; Actions [41862303853](https://github.com/VortexWang/Vortex/actions/runs/41862303853) |

## Qualification findings

The previous verification referenced early five-second trials and missing mode/fault coverage. Those observations are historical; the authoritative current coverage and remaining work are in **04-06-QUALIFICATION-MATRIX.md**, covering NFR-003/005/006/007/039/040/044 and D-04.

- **NFR-003/005:** baa0a02 native scale trials reached 1086–1200 Mbps TCP and <0.1% UDP loss at approximately 100 Mbps actual offered UDP. The one same-load independent TLS comparison reached 82.64%. These diagnostic numeric targets were met. Formal per-node resource/link, 60-second warmup / 300-second three-trial evidence and actual authorization gating are absent. Actions scale offered only 24.21 Mbps, separately recorded.
- **NFR-006:** integrity, forged OPEN, bypass and process-recovery tests pass for their enumerated finite cases. Full attack/path/configuration coverage has not been demonstrated. Dedicated hardware is not the blocker for closing these functional gaps.
- **NFR-007:** the current runtime bridge now has finite delayed-authorization evidence for bounded authorized TCP early DATA, while UDP remains OPEN/OPEN_OK gated. Controlled pool/mux RTT timelines, dedicated-host resource evidence, and formal NFR-007 qualification remain open; small loopback medians do not prove no extra network round trip.
- **NFR-039/040/044:** c8c2a37 verifies funded immutable snapshot selection, old-session affinity, new-session admission, coefficient binding, late-grant isolation, and zero held/session leakage. It still does not establish policy-boundary leases, share-CAS/drain, or full per-source conservation. In-process injected authority remains non-authoritative; Engine must not connect to a database.
- **D-04:** parameter review is complete in 04-06-D04-REVIEW.md. Keep 256 KiB→4 MiB / 8 MiB minimum / 30-second target / 30% waterline as unapproved candidates; no adaptive grant/block/renewal experiment has yet run. VLink windows are distinct parameters.

## Checks this review

`DOCKER_CONTEXT=orbstack make phase4-m2-check` passed on clean 6bfe976 before documentation edits. It runs full Engine race, targeted Engine checks and Linux Docker protocol regression checks; the host portion runs on Darwin and must not be described as a new Ubuntu full-race run. Log: `artifacts/phase4/review/6bfe976-phase4-m2-check.log`. No new performance run or formal qualification was performed in this review.

Actions [41838307562](https://github.com/VortexWang/Vortex/actions/runs/41838307562) was queried and confirmed successful for the same clean 6bfe976 as the local regression. The preceding recovery run 41836183498 succeeded for d0c1cb2. Historical performance results keep their original SHAs. Any newer documentation commit is not automatically assigned the measured performance values of an older commit.

## Next execution slice

Continue with NFR-039/040/044 runtime integration, NFR-006 attack cases, and controlled pool/mux RTT evidence for NFR-007. Run D-04 parameter comparisons before proposing lock values. Formal performance qualification follows on an eligible topology. See the matrix for exact ordering and evidence requirements.

No Phase 4 completion or Phase 5 start is recorded. Correctness work continues without waiting for old governance documents or exclusive performance hardware.

## c8c2a37 verification increment

The same immutable snapshot fixture passed on the native local five-process topology, Docker Compose, and GitHub Actions run 41862303853. An existing TCP connection exchanged bytes before and after a SIGHUP selection change and remained bound to `old`; a new connection after revision 2 used `new`. Final evidence retained old and new usage in separate gates with zero sessions and held bytes. This closes the finite snapshot-selection slice only; it does not qualify D-04 or NFR-039/040/044 in full.

## 90353ac policy-time increment

The deterministic `PolicyBoundary` Engine adapter passed package race tests, full `go test ./...`, `go vet ./...`, clean-SHA `DOCKER_CONTEXT=orbstack make phase4-m2-check`, and Actions 41870988836 on `1e378406b82cc23bede79cb68a2119f9a1e5d266`. It validates pre-issue/TTL and lease coverage, exact-T snapshot selection, conservative interval pause and recovery, a hard cutoff, and retaining unknown usage on the old snapshot. It is not wired into the relay: production clock-health input and a dynamic pre-issue provider are not available. Therefore NFR-039 remains unqualified; see `04-06-POLICY-CLOCK-EVIDENCE.md`.

## d7ca856 share CAS increment

Finite share revision coexistence, CAS fencing, connection overlap occupancy, boot rejection, drain and injectable Gate lease lifecycle passed race, full Engine tests/vet and clean-SHA Docker regression. Actions 41875373595 caught a partial-state mutation on a rejected third-generation Commit; d7ca856 fixes it by separating validation and mutation. Every step in Actions 41876404597 reports success on d7ca856, but the run aggregate is still `in_progress` with no terminal conclusion, so CI is pending. ShareGate is not wired into relay configuration and NFR-040 remains unqualified. See `04-06-SHARE-EVIDENCE.md`.


## Incremental verification: finite source credit — 6a1faf2

The source-authorization gap is partially closed for opt-in static-snapshot TCP:
finite credit, directional blocks/local replenish, settlement/unknown and
exhaustion checks pass Docker/native at clean 6a1faf2. The normal throughput
fixture still runs without this mode. Real grant identity/response semantics,
UDP, snapshot switching and rate-adaptive targets are missing; full NFR/D-04
verdicts remain unqualified. See 04-06-CREDIT-EVIDENCE.md for exact scope,
checksums, recovery-readiness repair and Actions 41849553357.


## d2f21f8 增量

静态单快照 source Gate 现支持 UDP 双向预扣及 TCP/UDP 交错共用预算；
可信进程内签发请求/结果已加入身份绑定、幂等、pending/unknown 占用
和迟到确认测试。热切路/多快照/系数、远端 Hub、200 ms 请求、动态
授权目标与正式性能门控仍缺。故原有“不支持 UDP / 没有任何 grant
ID”的描述仅适用于此前切片；不扩大 NFR/D-04 验收结论。详见
04-06-GRANT-UDP-EVIDENCE.md。Phase 4 仍 executing。

## Gaps

- gap_id: G-04-01
  truth: "A runtime without an explicitly selected qualification-only mode must not forward traffic unless its source authorization Gate is funded and present; runtime policy must not synthesize billing or share authorization as true."
  status: closed
  resolved_by: "Plan 04-07, source SHA a7a7432b11b5c3c7801e7fcfe3fe1664fe6c5b4e"
  resolution: "Default configuration requires explicit qualification-only test mode or source authorization; absent Gate is rejected before forwarding; policy defaults do not synthesize authorization; fixtures identify qualification-only mode. Same-SHA local Docker and Actions passed."
  severity: resolved
  artifacts:
    - path: "engine/cmd/vortex-relay/main.go"
      issue: "Original gap: sourceGate was optional and source listeners could serve without a funded Gate"
    - path: "engine/runtime/relay/bridge.go"
      issue: "Original gap: nil accounting Gate skipped session creation and still forwarded payload"
    - path: "engine/runtime/relay/policy.go"
      issue: "Original gap: static tracer snapshot asserted runtime, share and billing authorization"
  evidence: ".planning/phases/04-m2-m2-multi-hop-engine/04-07-EVIDENCE.md"

## Plan 04-07 Verification — a7a7432

`G-04-01` is closed for the default runtime boundary. On the same clean SHA,
OrbStack arm64 passed `DOCKER_CONTEXT=orbstack make phase4-m2-check` and the
five-role Docker TCP/UDP smoke; GitHub Actions run 41887199379 passed all steps,
including pool/mux/auto runtime and authorized route-switch/restart regressions.
Full results and scope limits are in `04-07-EVIDENCE.md`.

Verification remains `gaps_found`: NFR-003/005/006/007/039/040/044 and D-04
are not qualified. Qualification-only in-process evidence is not durable
production authority. Phase 4 remains executing; this update does not authorize
Phase 5 advancement.

## Remaining Gap-Closure Register

These gaps are intentionally separate from the resolved default-boundary gap
above and map directly to the queued plans:

- gap_id: G-04-039
  truth: "Policy-time clock bounds, pre-issue, lease coverage, hard deadline and bounded issuer wait are enforced at the runtime source Gate."
  status: failed
  severity: major
  planned_by: "04-08-PLAN.md"
- gap_id: G-04-044
  truth: "Runtime accounting preserves immutable snapshot, path coefficient, grant, write and unknown upper-bound conservation per source key."
  status: failed
  severity: blocker
  planned_by: "04-08-PLAN.md"
- gap_id: G-04-040
  truth: "Runtime share admission is fenced by node/boot/revision and drains old leases without duplicate rights or premature release."
  status: failed
  severity: major
  planned_by: "04-09-PLAN.md"
- gap_id: G-04-006
  truth: "The finite integrity and bypass matrix covers the current authorized configuration and records included/excluded combinations."
  status: failed
  severity: major
  planned_by: "04-10-PLAN.md"
- gap_id: G-04-007
  truth: "Authorized TCP first-byte behavior is bounded and measured without unauthorized target writes or an unbounded OPEN wait."
  status: failed
  severity: major
  planned_by: "04-10-PLAN.md"
- gap_id: G-04-003
  truth: "Three-relay throughput qualification runs with complete authorization, resource isolation, warmup and repeated trial evidence."
  status: failed
  severity: major
  planned_by: "04-11-PLAN.md"
- gap_id: G-04-005
  truth: "TCP/UDP fairness qualification uses actual offered load and complete classified loss/accounting evidence."
  status: failed
  severity: major
  planned_by: "04-11-PLAN.md"
- gap_id: G-D04
  truth: "D-04 block, target and waterline candidates are experimentally compared and explicitly reviewed before lock."
  status: failed
  severity: major
  planned_by: "04-11-PLAN.md"

## 2026-11-05 Gap Reconciliation

Plans 04-08 through 04-11 executed on clean commits `9385d42`, `ccb3065`,
`d068de0` and `fc9013e`, with documentation finalized at `5bd384a`.

- **G-04-039:** process-local policy-window validation is verified by 04-08;
  full runtime clock/issuer authority remains failed.
- **G-04-044:** source snapshot wiring is verified by 04-08; durable per-source
  conservation remains failed.
- **G-04-040:** node/boot/revision fencing and ShareGate lease drain are wired
  and race/Docker tested by 04-09; durable Agent/Hub receipt reconciliation
  remains failed.
- **G-04-006:** finite identity/path/revision/hop/visited/bypass matrix and
  rejection cleanup are verified by 04-10; no exhaustive proof is claimed.
- **G-04-007:** bounded early TCP DATA and strict UDP OPEN_OK gating are
  verified by 04-10; dedicated per-mode authorization timelines remain failed.
- **G-04-003 / G-04-005 / G-D04:** 04-11 adds the 27-case candidate matrix and
  fail-closed gate, but no eligible isolated native host, runtime-authority
  report, 60-second warmup or three 300-second trials were available. These
  remain failed and candidates remain unlocked.

The verification route is therefore **not** Phase 5. Correctness slices are
implemented, while formal performance qualification and durable authority must
be completed before Phase 4 can transition.

## 2026-11-05 optimization closure branch

Branch `gsd/phase-4-optimization-closure` added bounded UDP socket-buffer
fallback shared by source, relay, and destination paths. Hosts that reject the
preferred 32 MiB request now try 8 MiB, 4 MiB, 1 MiB, and 256 KiB before
retaining the operating-system default; tuning failure is warning-level because
it is a throughput optimization and does not invalidate a usable socket.

The regression was reproduced on native Darwin: the prior fixed 32 MiB request
returned `ENOBUFS` at the destination and the UDP smoke timed out. After the
fallback and a dedicated UDP reader in the echo fixture, the UDP smoke and
Docker runtime accounting completed. OrbStack Docker produced zero classified
UDP loss, exact left/right accounting, successful 64 MiB mux forward/reverse
benchmarks, and zero loopback UDP loss. The mux benchmark now uses a generous
correctness deadline so low diagnostic throughput is measured instead of
turning into a fixed 30-second false failure.

Native Darwin still cannot classify all host UDP receive losses under the
100 Mbps three-relay workload; its strict accounting run remains fail-closed.
No NFR or D-04 qualification is promoted from either host.
