---
phase: 04-m2-m2-multi-hop-engine
verified: 2026-11-05
status: gaps_found
score: tracer runtime and finite recovery verified; D-04 and seven NFR rows remain unqualified
behavior_unverified: 7
---

# Phase 4 Verification — current qualification review

Review source: `a7a7432b11b5c3c7801e7fcfe3fe1664fe6c5b4e` (Plan 04-07 code and evidence successor). Phase 4 stays **executing** because formal qualification and runtime integration gaps remain. V1.11 is reference only. No old restart documents are required to continue implementation.

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
- **NFR-007:** pool/mux samples now exist, but runtime TCP waits for OPEN_OK before bridging, including downstream relay confirmation. Controlled RTT/timeline evidence and bounded authorized early-DATA behavior are missing. Small loopback medians do not prove no extra network round trip.
- **NFR-039/040/044:** c8c2a37 verifies funded immutable snapshot selection, old-session affinity, new-session admission, coefficient binding, late-grant isolation, and zero held/session leakage. It still does not establish policy-boundary leases, share-CAS/drain, or full per-source conservation. In-process injected authority remains non-authoritative; Engine must not connect to a database.
- **D-04:** parameter review is complete in 04-06-D04-REVIEW.md. Keep 256 KiB→4 MiB / 8 MiB minimum / 30-second target / 30% waterline as unapproved candidates; no adaptive grant/block/renewal experiment has yet run. VLink windows are distinct parameters.

## Checks this review

`DOCKER_CONTEXT=orbstack make phase4-m2-check` passed on clean 6bfe976 before documentation edits. It runs full Engine race, targeted Engine checks and Linux Docker protocol regression checks; the host portion runs on Darwin and must not be described as a new Ubuntu full-race run. Log: `artifacts/phase4/review/6bfe976-phase4-m2-check.log`. No new performance run or formal qualification was performed in this review.

Actions [41838307562](https://github.com/VortexWang/Vortex/actions/runs/41838307562) was queried and confirmed successful for the same clean 6bfe976 as the local regression. The preceding recovery run 41836183498 succeeded for d0c1cb2. Historical performance results keep their original SHAs. Any newer documentation commit is not automatically assigned the measured performance values of an older commit.

## Next execution slice

Continue with NFR-039/040/044 runtime integration, NFR-006 attack cases, and safe early TCP DATA / controlled RTT for NFR-007. Run D-04 parameter comparisons before proposing lock values. Formal performance qualification follows on an eligible topology. See the matrix for exact ordering and evidence requirements.

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
