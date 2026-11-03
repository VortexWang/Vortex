---
phase: 03-m1-vlink-m1-vlink-engine
verified: 2026-11-04
status: passed
score: 4/4 phase-scoped success criteria verified
behavior_unverified: 0
covered_files:
  - .planning/PROJECT.md
  - .planning/REQUIREMENTS.md
  - .planning/ROADMAP.md
  - .planning/phases/03-m1-vlink-m1-vlink-engine/03-CONTEXT.md
  - .planning/phases/03-m1-vlink-m1-vlink-engine/03-DISCUSSION-LOG.md
  - .planning/phases/03-m1-vlink-m1-vlink-engine/03-RESEARCH.md
  - .planning/phases/03-m1-vlink-m1-vlink-engine/03-VALIDATION.md
  - .planning/phases/03-m1-vlink-m1-vlink-engine/03-UAT.md
  - .planning/phases/03-m1-vlink-m1-vlink-engine/03-SECURITY.md
  - .planning/phases/03-m1-vlink-m1-vlink-engine/03-REVIEW.md
  - .planning/phases/03-m1-vlink-m1-vlink-engine/03-01-PLAN.md
  - .planning/phases/03-m1-vlink-m1-vlink-engine/03-01-SUMMARY.md
  - .planning/phases/03-m1-vlink-m1-vlink-engine/03-02-PLAN.md
  - .planning/phases/03-m1-vlink-m1-vlink-engine/03-02-SUMMARY.md
  - .planning/phases/03-m1-vlink-m1-vlink-engine/03-03-PLAN.md
  - .planning/phases/03-m1-vlink-m1-vlink-engine/03-03-SUMMARY.md
  - .planning/phases/03-m1-vlink-m1-vlink-engine/03-04-PLAN.md
  - .planning/phases/03-m1-vlink-m1-vlink-engine/03-04-SUMMARY.md
  - .planning/phases/03-m1-vlink-m1-vlink-engine/03-05-PLAN.md
  - .planning/phases/03-m1-vlink-m1-vlink-engine/03-05-SUMMARY.md
  - .planning/phases/03-m1-vlink-m1-vlink-engine/03-06-PLAN.md
  - .planning/phases/03-m1-vlink-m1-vlink-engine/03-06-SUMMARY.md
  - engine/vlink/codec.go
  - engine/vlink/hello.go
  - engine/vlink/identity.go
  - engine/vlink/stream.go
  - engine/vlink/dispatch.go
  - engine/vlink/window.go
  - engine/vlink/open.go
  - engine/vlink/target.go
  - engine/vlink/udp.go
  - engine/vlink/pool.go
  - engine/vlink/reverse.go
  - engine/vlink/limits.go
  - engine/vlink/observability.go
  - engine/vlink/codec_test.go
  - engine/vlink/hello_test.go
  - engine/vlink/identity_test.go
  - engine/vlink/stream_test.go
  - engine/vlink/window_test.go
  - engine/vlink/open_test.go
  - engine/vlink/target_test.go
  - engine/vlink/udp_test.go
  - engine/vlink/pool_test.go
  - engine/vlink/reverse_test.go
  - engine/vlink/limits_test.go
  - engine/vlink/observability_test.go
  - engine/vlink/integration_test.go
  - engine/vlink/fuzz_test.go
  - engine/vlink/benchmark_test.go
  - Makefile
  - tools/vlink/docker-test.sh
  - tools/phase3/benchmark.sh
  - .github/workflows/phase3.yml
  - contracts/vlink/v1/encoding.md
  - contracts/vlink/v1/encoding.sha256
  - artifacts/phase3/benchmark/20261103-0143cdd-ubuntu-kvm.txt
  - artifacts/phase3/benchmark/20261103-0143cdd-ubuntu-kvm.raw.txt
  - artifacts/phase3/benchmark/20261103T165901Z.txt
  - artifacts/phase3/benchmark/20261103T165901Z.txt.raw.txt
covered_digest: "v4:sha256:2f28178fd3470667f36a057088eba14ce087df28982825c6bb586f50cffd8a7b"
---

# Phase 3: M1 VLink Engine Verification

**Phase Goal:** 节点可在已授权路径上通过受认证加密 VLink 建立正向和反向隧道，链路身份与授权不可伪造或重放。

## Goal Achievement

| # | Phase-scoped success criterion | Status | Evidence |
|---|---|---|---|
| 1 | VLink frame encoding, HELLO, identity, freshness and stream IDs reject malformed, stale or forged inputs. | VERIFIED | Bounded codec, TLS 1.3/mTLS and exporter-bound HELLO, peer identity/freshness callbacks, and non-reusable stream IDs are implemented. Codec, HELLO, identity, stream and fuzz checks passed. |
| 2 | TLS links, mux/pool, bidirectional streams and reverse establishment operate with cleanup and bounded retries. | VERIFIED | Six user-confirmed UAT checks plus race-tested mux, pool, reverse connector, half-close, cancellation and link-loss integration tests passed locally and in Linux Docker. |
| 3 | OPEN enforces local path/target policy and UDP cannot bypass OPEN/OPEN_OK. | VERIFIED | Local snapshot authorization, DNS/address restrictions, gate admission, DGRAM lifecycle checks and unauthorized UDP integration tests passed. |
| 4 | Performance measurements are reproducible and accurately scoped to the actual environment. | VERIFIED for Phase 3 evidence scope | Ubuntu 24.04 KVM candidate run at `0143cdd` measured NFR-002 761.67 MB/s at 70.01% of same-host TLS, NFR-004 3.74% directional delta, and NFR-007 198/195 us. The run is virtualized, loopback, single-transfer and has production accounting disabled. Current OrbStack smoke also passes the harness but reports NFR-004 candidate-fail (28.75% direction delta). Neither result is represented as full production-path qualification. |

## Automated Verification

- `make phase3-vlink-check` passed locally: bounded fuzz smoke, `go test -race ./...`, Linux Docker integration, and benchmark evidence capture.
- `cd engine && go vet ./...` passed.
- Cross-phase regression checks passed: `make phase2-engine-check` and Phase 1 baseline unittest discovery (10 tests).
- GitHub Actions run [41675109713](https://github.com/VortexWang/Vortex/actions/runs/41675109713) completed successfully on SHA `3ad17ce46f50a10ca58b6343c0174e555922d43e`. Phase 3 source/workflow/entry files have no diff from that SHA at this verification.
- `git diff --check` passed before evidence/report generation.

## Human Verification

`03-UAT.md` records all six checks as passed with zero issues and zero pending. User responses confirmed TCP half-close, stream isolation, OPEN/UDP gating, pool/reverse-link behavior, privacy/rate limits, and the stated dual-host/performance evidence limits.

## Requirements Coverage

| Requirement group | Status | Notes |
|---|---|---|
| ENG-010..019, ENG-025 | SATISFIED at VLink protocol/Engine boundary | Authenticated link, HELLO freshness contract, mux lifecycle, windows, liveness and link pool/reverse establishment have code and test evidence. Runtime revocation, current-path authority and durable freshness remain injected interfaces owned by later runtime phases. |
| ENG-020..024 | SATISFIED at local authorization boundary | OPEN checks local snapshot/path/segment authorization, target policy and UDP admission before confirmation. No control-plane lookup is performed by Engine. |
| NFR-002, NFR-004, NFR-007 | CANDIDATE MEASUREMENTS RECORDED; production qualification deferred | Ubuntu KVM values meet candidate numeric limits, but topology/workload/gate constraints prevent qualification. The latest local smoke independently reports an NFR-004 candidate failure and remains preserved. Phase 8 owns full production-path qualification. |

## Security and Review

The 16 plan threat IDs are recorded closed at the VLink boundary in `03-SECURITY.md`; injected runtime authority limitations are documented. Inline code review found no confirmed blocker or warning in the reviewed security-critical paths. UI review was not applicable: this phase contains no UI-SPEC or frontend implementation.

## Deferred Qualification

Full performance acceptance remains open for the agreed production topology with external path segments, production accounting enabled, concurrent flows and sustained duration on a qualifying native Linux host. The successful functional phase verification does not close this later release gate.

**Approach:** Goal-backward verification against Phase 3 roadmap criteria, six UAT checks, implementation and retained local/Actions evidence.
**Verifier:** Inline GSD verification in the main Codex window per user instruction to avoid subagents.
