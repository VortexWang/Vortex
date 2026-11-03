---
phase: "02"
status: complete
nyquist_compliant: true
validated: "2026-11-04"
---

# Phase 02 — Validation Strategy

## Test Infrastructure

| Property | Value |
|---|---|
| Framework | Go standard library tests, race detector, Docker scratch test image |
| Quick command | `cd engine && go test -race ./...` |
| Shared local/CI command | `make phase2-engine-check` |
| Local Linux execution | OrbStack Docker on arm64 for current host |
| CI execution | GitHub Actions Ubuntu 24.04 amd64, run 41684319179 on SHA `75d66a7823fde6b743745daffb9cf9f607d137df` |

## Sampling

- Run `make phase2-engine-quick` after each code task.
- Run `make phase2-engine-check` after each plan wave and before phase verification.
- Record local Docker and Actions results separately against the exact commit.

## Current Coverage

| Behavior | Test | Status |
|---|---|---|
| TCP half-close preserves reverse response | `TestTCPPropagatesHalfCloseAndMetersWrittenBytes` | Passed locally and in local Linux scratch container |
| Directional successful-write metering | Same test | Passed |
| Context cancellation closes both directions | `TestTCPContextCancellationClosesBothDirections` | Passed locally and in local Linux scratch container |
| Meter failure closes both directions | `TestTCPMeterFailureStopsRelay` | Passed locally and in local Linux scratch container |
| Linux bare-TCP splice | `TestTCPPlatformSelectsSpliceForBareTCP` | Passed in local Linux arm64 scratch container; amd64 cross-compiles |
| UDP client routing and response isolation | `TestUDPForwardsResponsesToOriginatingClientAndMetersWrites` | Passed locally and in Docker/Actions on SHA `88d59c220c536601ff22b9a5762362c6553e8a14` |
| UDP 60-second default and idle expiry override | `TestUDPDefaultsIdleTimeoutToSixtySeconds`, `TestUDPExpiresIdleSessionAndReplacesIt` | Passed locally and in Docker/Actions on the same SHA |
| UDP cancellation and socket cleanup | `TestUDPCancellationClosesListenerAndSessions` | Passed locally and in Docker/Actions on the same SHA |
| UDP global and per-source-IP admission limits | `TestUDPPerClientSessionLimitDropsBeforeCreatingSession`, `TestUDPGlobalSessionLimitReleasesAfterExpiry` | Passed under race detection locally and in Docker/Actions on SHA `c79c2d1194a66711a68f66c2b5e7a5f474a8381d` |
| Gate admission, usage resolution, accounting failure, short writes, unknown outcomes | `engine/accounting`, `TestTCPWithGate*`, `TestUDPWithGate*` | Passed locally in race tests and both Linux scratch binaries and in Actions on SHA `213c0d83674686cc07eb7dc3b9d6d3db2560a6c8` (run 41613449912) |
| Concrete production quota/rate policy and durable resolver | Owned by future Agent/Hub integration | Not implemented in the independent Engine module; the injected policy contract is tested |
| Linux direct-forwarding benchmark | `BenchmarkTCPRelayBareTCP` | OrbStack Linux/arm64 loopback measured 4,748.28 MB/s over 15 seconds; environment-limited, not qualified hardware acceptance |

## Evidence Rules

Docker validates that the Linux-targeted test binary runs in the local Linux VM; it does not prove qualified throughput or complete production behavior. TCP splice and UDP tests executed in the local Linux container. The current Engine and benchmark smoke passed local `make phase2-engine-check` and the same command in GitHub Actions on SHA `b59565459db046c5f7b709d63bdd9a9c82c2ed08`, run 41615021796.

## Validation Sign-Off

- [x] Every Phase 2 automated behavior maps to an existing test.
- [x] `make phase2-engine-check` passed at current verification HEAD `41ae509e8ed41c982ff03810c9057dc60a396485` on local OrbStack Linux arm64.
- [x] GitHub Actions run [41684319179](https://github.com/VortexWang/Vortex/actions/runs/41684319179) passed `make phase2-engine-check` and benchmark smoke on SHA `75d66a7823fde6b743745daffb9cf9f607d137df` (Ubuntu 24.04 amd64).
- [x] Environment-limited benchmark evidence is not represented as qualified throughput acceptance.
- [x] Phase 5/6 runtime authority and Phase 8 dedicated performance gates remain deferred to their owning phases.
