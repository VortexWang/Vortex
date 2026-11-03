---
phase: "02"
status: active
---

# Phase 02 — Validation Strategy

## Test Infrastructure

| Property | Value |
|---|---|
| Framework | Go standard library tests, race detector, Docker scratch test image |
| Quick command | `cd engine && go test -race ./...` |
| Shared local/CI command | `make phase2-engine-check` |
| Local Linux execution | OrbStack Docker on arm64 for current host |
| CI execution | GitHub Actions Ubuntu 24.04 amd64 |

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
| Linux bare-TCP splice | Not implemented | Pending |
| UDP session routing and 60-second idle expiry | Not implemented | Pending |
| Gate/rate/connection controls and accounting failure behavior | Not implemented | Pending |
| 700 Mbps benchmark | Not run | Development target; miss is recorded, not a progress blocker |

## Evidence Rules

An unrun behavior remains pending. Docker smoke validates that the Linux-targeted test binary runs in the local Linux VM; it does not prove `splice`, throughput, leak, or production behavior. Actions evidence is separate and must be captured from the workflow run for the same commit.
