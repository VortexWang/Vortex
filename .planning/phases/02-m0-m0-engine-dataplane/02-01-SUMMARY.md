---
phase: 02-m0-m0-engine-dataplane
plan: 01
status: complete
completed: 2026-11-03
requirements:
  - GEN-001
  - GEN-002
  - ENG-001
  - ENG-002
  - ENG-005
---

# Plan 02-01 Summary: Independent TCP Relay Tracer

Created a separate standard-library-only Go module at `engine/` and a bidirectional TCP relay. EOF closes only the matching write direction; context cancellation and hard I/O or meter failures close both endpoints. Meter callbacks receive direction and count bytes only after successful destination writes.

Added real TCP loopback tests proving a request half-close still permits a full reverse response, cancellation cleanup, and immediate shutdown on metering failure. Added a shared quick target and Linux Docker test that cross-builds the test binary for the Docker host architecture and runs it in a `scratch` container.

## Verification

- `make phase2-engine-check` — passed on macOS arm64 with OrbStack Linux arm64 Docker.
- Native `go test -race ./...` — passed.
- Linux scratch-container tests — 3 passed.
- Clean commit `a9b79f11b7d9b9b3a32bbf2a47b745ee856b9e0e` passed the same command in GitHub Actions Ubuntu 24.04 amd64; [run 41600595927](https://github.com/VortexWang/Vortex/actions/runs/41600595927).
- `splice`, UDP sessions, gates/rate limits, and performance target remain unimplemented and are not claimed by this plan.

## Deviations

- Implemented the portable TCP relay tracer first. Linux `splice` is separated into a follow-up plan because it needs architecture-specific syscall and fallback tests.
