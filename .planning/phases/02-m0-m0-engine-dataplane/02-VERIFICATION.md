---
phase: 02-m0-m0-engine-dataplane
verified: 2026-11-03T17:24:58Z
status: passed
score: 4/4 must-haves verified
covered_files:
  - .github/workflows/phase2.yml
  - .planning/phases/02-m0-m0-engine-dataplane/02-01-PLAN.md
  - .planning/phases/02-m0-m0-engine-dataplane/02-01-SUMMARY.md
  - .planning/phases/02-m0-m0-engine-dataplane/02-02-PLAN.md
  - .planning/phases/02-m0-m0-engine-dataplane/02-02-SUMMARY.md
  - .planning/phases/02-m0-m0-engine-dataplane/02-03-PLAN.md
  - .planning/phases/02-m0-m0-engine-dataplane/02-03-SUMMARY.md
  - .planning/phases/02-m0-m0-engine-dataplane/02-04-PLAN.md
  - .planning/phases/02-m0-m0-engine-dataplane/02-04-SUMMARY.md
  - .planning/phases/02-m0-m0-engine-dataplane/02-05-PLAN.md
  - .planning/phases/02-m0-m0-engine-dataplane/02-05-SUMMARY.md
  - .planning/phases/02-m0-m0-engine-dataplane/02-06-PLAN.md
  - .planning/phases/02-m0-m0-engine-dataplane/02-06-SUMMARY.md
  - Makefile
  - artifacts/phase2/benchmark/20261103T095609Z.txt
  - engine/accounting/accounting.go
  - engine/accounting/accounting_test.go
  - engine/go.mod
  - engine/relay/gate_test.go
  - engine/relay/tcp.go
  - engine/relay/tcp_benchmark_test.go
  - engine/relay/tcp_linux.go
  - engine/relay/tcp_linux_test.go
  - engine/relay/tcp_portable.go
  - engine/relay/tcp_test.go
  - engine/relay/udp.go
  - engine/relay/udp_test.go
  - test/engine/Dockerfile
  - test/engine/benchmark.Dockerfile
  - tools/engine/docker-test.sh
  - tools/phase2/README.md
  - tools/phase2/benchmark.sh
covered_digest: "v4:sha256:1433122e32c2b8096e2b4dace00303fffa3c9de9b1bb8ea078597a29d40736c7"
behavior_unverified: 0
deferred:
  - truth: "Production and simulated Agent consumers use the formal in-process Engine contract."
    addressed_in: "Phase 5"
    evidence: "Phase 5 roadmap success criterion 1 delivers Agent and Engine as vortex-node; M0 intentionally remains a standalone Engine module."
  - truth: "Gate and Resolver implementations persist authoritative quota, rate, and connection policy and reconcile unknown outcomes."
    addressed_in: "Phases 5-6"
    evidence: "Phase 2 exposes and tests injected Gate/Session/Resolver contracts; Phase 5 owns Agent/Hub runtime and Phase 6 owns PostgreSQL-backed authority."
  - truth: "A complete multi-hop pipeline assigns ingress, relay, and egress roles within the same node."
    addressed_in: "Phase 4"
    evidence: "Phase 4 roadmap success criterion 1 covers multi-hop TCP/UDP path scheduling and intermediate nodes."
  - truth: "Direct-forwarding throughput is qualified on dedicated native Linux hardware."
    addressed_in: "Phase 8"
    evidence: "Phase 8 success criteria require dedicated Linux performance evidence; the current 15-second OrbStack result is explicitly environment-limited."
---

# Phase 2: M0 Engine Dataplane Verification Report

**Phase Goal:** 用户可通过独立 Go Engine 建立直接 TCP/UDP 转发；门控与可信计量接口开启时满足 M0 指标。
**Verified:** 2026-11-03T10:28:22Z
**Status:** passed for Phase 2 local success criteria; deferred items remain open in their owning phases.

## Goal Achievement

### Observable Truths

| # | Truth | Status | Evidence |
|---|---|---|---|
| 1 | Engine is independently buildable, testable, and benchmarkable; consumers are constrained to its in-process boundary. | VERIFIED for M0 Engine boundary | `engine/go.mod` is a standalone module; `go list -m all` returns only `vortex/engine`. Relay and accounting expose in-process APIs and import no database/control-plane package. Agent consumers are deferred to Phase 5. |
| 2 | TCP half-close/cancellation and Linux bare-TCP splice preserve payload and directional accounting. | VERIFIED | Current `make phase2-engine-check` passed native race tests and the Linux arm64 scratch container. The container executed `TestTCPPlatformSelectsSpliceForBareTCP`, half-close, cancellation, and gated write tests. |
| 3 | UDP sessions and injected policy/accounting boundaries enforce tested admission, reserve-before-write, successful-write usage, and one terminal batch resolution. | VERIFIED for the Engine boundary | Linux scratch tests passed global/per-client UDP admission, gate denial/accounting, TCP short-write settlement, and accounting unknown/duplicate-resolution cases. No durable production Gate/Resolver is claimed; that integration is deferred. |
| 4 | A Linux benchmark attempt retains environment, command, raw result, and distance from the 700 Mbps development target without claiming qualified acceptance. | VERIFIED | `artifacts/phase2/benchmark/20261103T095609Z.txt` records the 15-second command and raw output, 37,986.24 Mbps measured, +37,286.24 Mbps from target, and `environment-limited` status for OrbStack. |

**Score:** 4/4 Phase 2 success criteria verified. Later-phase integrations and release qualification are listed as deferred, not passed.

### Required Artifacts

| Artifact | Expected | Status | Details |
|---|---|---|---|
| `engine/go.mod` | Independent Go module | VERIFIED | Standard-library-only module; `go list -m all` reports only `vortex/engine`. |
| `engine/relay/tcp.go`, `tcp_linux.go`, `tcp_portable.go` | Relay API, splice and portable paths | VERIFIED | Linux/amd64 and arm64 splice path is build-tagged; other platforms retain the portable copy path. |
| `engine/relay/udp.go` | Direct UDP sessions, gate, cleanup | VERIFIED | Per-session upstream socket, 60-second default idle expiry, configurable global/per-client admission, gate and usage batch hooks. |
| `engine/accounting/accounting.go` | In-process reservation contract | VERIFIED | Gate, Session, Batch and Resolver API; duplicate terminal resolution is rejected and resolver failure becomes Unknown with the reserved bound retained. |
| `tools/phase2/benchmark.sh`, benchmark artifact | Reproducible benchmark evidence | VERIFIED for attempt | Script records topology, source digest, host, architecture, kernel, capacity, command, raw result and target delta. |

### Key Link Verification

| From | To | Via | Status | Details |
|---|---|---|---|---|
| `Makefile` | `tools/engine/docker-test.sh` | `phase2-engine-check` target | WIRED | The shared target runs Go race tests and builds/runs relay and accounting tests in a Linux scratch container. |
| `.github/workflows/phase2.yml` | `make phase2-engine-check` | Ubuntu 24.04 workflow step | WIRED | Same shared command is used in CI; a separate one-second benchmark smoke does not gate correctness. |
| `engine/relay/tcp.go` | `engine/accounting` | `TCPWithGate` and `Session.Reserve` | WIRED | Reservations precede writes; only destination-accepted bytes are committed; zero accepted bytes return the batch. |
| `engine/relay/udp.go` | `engine/accounting` | `UDPOptions.Gate` and per-datagram reservations | WIRED | Request and response paths reserve before writes and settle accepted byte counts. |

### Data-Flow Trace (Level 4)

Not applicable to UI/database rendering. Engine input flows from caller-provided `net.Conn`, `UDPConn`, and injected policy interfaces to destination writes; no control-plane fetch or database query exists in the Engine module.

### Behavioral Spot-Checks

| Behavior | Command | Result | Status |
|---|---|---|---|
| Half-close and reverse response | `go test ./relay -run '^TestTCPPropagatesHalfCloseAndMetersWrittenBytes$' -count=1` | Passed on Darwin arm64. | PASS |
| Linux splice selection | `make phase2-engine-check` | Linux arm64 scratch output includes `TestTCPPlatformSelectsSpliceForBareTCP`; passed. | PASS |
| Gate reserves before write and settles accepted bytes | `make phase2-engine-check` | `TestTCPWithGateReservesBeforeWriteAndSettlesAcceptedBytes` passed in Linux scratch. | PASS |
| UDP gate accounting and routing | `make phase2-engine-check` | `TestUDPWithGateAccountsRequestAndResponseWrites` passed in Linux scratch. | PASS |
| Unknown usage retains reservation upper bound | `go test ./accounting -run '^TestBatchResolverFailureBecomesUnknownAndCannotRetry$' -count=1` | Passed on Darwin arm64; Linux scratch accounting suite also passed. | PASS |

### Probe Execution

No phase-specific probe scripts are declared by the plans.

### Requirements Coverage

V1.11-derived IDs remain traceability references under the project restart decision; the current Phase 2 roadmap success criteria are the acceptance contract. IDs listed below but not implemented in M0 are explicitly assigned to later work or remain pending for roadmap disposition.

| Requirement | Status | Evidence / disposition |
|---|---|---|
| `GEN-003`, `GEN-004` | SATISFIED for M0 | Standalone Engine module; no database or active control-plane imports. |
| `GEN-001`, `GEN-002` | DEFERRED | Backend/Hub traffic boundary and Agent+Engine executable integration are outside M0; Agent/Hub delivery is Phase 5. |
| `GEN-005`, `GEN-006` | DEFERRED | Multi-hop node roles and Edge Hub autonomy are later-phase behavior (Phase 4/5). |
| `GEN-007` | DEFERRED | Public business API boundary is owned by the Phase 6 backend. |
| `ENG-004`, `ENG-005`, `ENG-006`, `ENG-008` | SATISFIED | Linux splice, half-close, 60-second UDP expiry, and lifecycle cleanup are covered by source and passing tests. |
| `ENG-001`, `ENG-002` | DEFERRED | Actual production/simulated Agent consumers are not in M0; Phase 5 owns the node executable and consumers. |
| `ENG-003` | PARTIAL / LATER SCOPE | Direct forwarding plus injected gate path is present; next-hop/multi-hop pipeline belongs to Phase 4. |
| `ENG-007`, `ENG-009` | PENDING REFERENCE SCOPE | Buffer-pool/no-allocation policy and PROXY protocol are not current M0 acceptance criteria; retain as pending owner roadmap disposition. |
| `ENG-010`, `ENG-011` | DEFERRED | TLS/mTLS control link and TLS-exporter HELLO are Phase 3 VLink work. |
| `NFR-001` | PARTIAL / ENVIRONMENT-LIMITED | Development benchmark attempt and delta are recorded; dedicated-hardware qualification remains Phase 8 evidence. |
| `SUP-TEST-001` | SATISFIED for harness | Local Docker test and CI workflow share `make phase2-engine-check`; benchmark smoke is separate. |

Note: `GEN-005` and the full multi-hop portion of `ENG-003` are not claimed as completed in M0. The requirement map still shows all V1.11 references as Pending; this report does not rewrite that source traceability table.

### Anti-Patterns Found

| File | Line | Pattern | Severity | Impact |
|---|---|---|---|---|

No TODO/FIXME/XXX, placeholder implementation, or empty runtime stub was found in the verified Engine and benchmark source paths.

### Human Verification Required

None. Phase 2 UAT records 4/4 user-confirmed behaviors with 0 issues. Dedicated performance qualification is deferred and is not represented as human-passed evidence.

### Deferred Items

| # | Item | Addressed In | Evidence |
|---|---|---|---|
| 1 | Production and simulated Agent integration with the Engine process contract | Phase 5 | Roadmap success criterion 1 delivers Agent and Engine as one `vortex-node`. |
| 2 | Durable authoritative Gate/Resolver state and policy adapter | Phases 5-6 | Phase 5 owns Agent/Hub runtime; Phase 6 owns the PostgreSQL-backed quota and accounting authority. |
| 3 | Qualified direct-forwarding throughput evidence | Phase 8 | Roadmap Phase 8 requires dedicated Linux performance acceptance; current OrbStack loopback is explicitly limited. |

### Verification Evidence

- Current HEAD `41ae509e8ed41c982ff03810c9057dc60a396485`: `make phase2-engine-check` passed locally. Native Go race tests passed; the Linux arm64 scratch container ran splice, TCP, UDP, gate, and accounting tests. Phase 1 regression discovery passed 10 tests.
- GitHub Actions run 41615021796 passed `make phase2-engine-check` and benchmark smoke on `b59565459db046c5f7b709d63bdd9a9c82c2ed08`. Since that run, the only Phase 2 entry-file change is additive Phase 3 target declarations in `Makefile`; the `phase2-engine-check` and `phase2-benchmark-smoke` recipes are unchanged. The current commit has not yet run in Actions; this report does not claim otherwise.
- After Phase 2 completion was committed, push run [41684319179](https://github.com/VortexWang/Vortex/actions/runs/41684319179) passed both Phase 2 shared checks on SHA `75d66a7823fde6b743745daffb9cf9f607d137df` (Ubuntu 24.04 amd64). Phase 3 run [41684319209](https://github.com/VortexWang/Vortex/actions/runs/41684319209) also passed on that SHA. These runs exercise the same Phase 2 source and Makefile recipes; subsequent changes to this report are documentation-only.
- `go list -m all` returned only `vortex/engine`.
- Benchmark evidence: `artifacts/phase2/benchmark/20261103T095609Z.txt`; status `environment-limited`, not qualified throughput acceptance.
- UAT: 4 passed, 0 issues, 0 pending in `02-UAT.md`.

### Gaps Summary

No M0-local implementation gaps remain against the current Phase 2 roadmap success criteria. The Engine and its injected accounting boundary pass local Docker/race tests, and the benchmark-attempt requirement has an honest, environment-limited record. Durable runtime policy adapters, Agent consumers and dedicated-hardware throughput are still open in their owning phases; none is reported as completed here.

---

_Verified: 2026-11-03T10:28:22Z_
_Verifier: GSD goal-backward verification (inline codebase checks)_
