---
phase: 02-m0-m0-engine-dataplane
researched: 2026-11-03
status: complete-inline
---

# Phase 2 Research: M0 Engine Dataplane

## Repository Findings

- The repository currently has only the root Go module (`go 1.27.3`) and Phase 1 model/fixture code. There is no Engine package, Engine module, forwarding code, or product TCP/UDP tests.
- Phase 1 uses standard-library Go tests and `make` entry points. `.github/workflows/phase1.yml` runs `make phase1-check` on Ubuntu 24.04 and artifacts are tied to the commit/environment.
- `test/netns/fixture` is a small standalone test fixture; it is not production Engine code.
- Phase 2 owns GEN-001..007, ENG-001..011 and NFR-001. Later VLink, multi-hop, Agent/Hub, backend, and frontend behavior belongs to later phases.
- Root `.planning/PROJECT.md` and `02-CONTEXT.md` require Engine/data-plane independence from databases and the control plane.

## Implementation Guidance

- Create a separate `engine/` Go module with its own `go.mod`, initially using only the Go standard library. This keeps the Engine dependency boundary mechanically inspectable and separately testable.
- Start with a public, narrow TCP relay API and a behavioral test that proves half-close: EOF in one direction must propagate to the matching destination write side while reverse traffic continues.
- Keep forwarding, byte accounting, and write-side shutdown in a small package boundary. Meter only bytes accepted by destination writes; expose direction so the caller can apply the project's charging policy.
- Add Linux `splice` as a build-tagged implementation after the portable relay tests establish the behavior. Keep non-Linux builds supported for local development and fallback tests. A splice path must account only successful destination-pipe-to-socket writes and must not claim success from bytes merely staged in a pipe.
- UDP requires per-client session ownership, response routing, idle expiry, close/cancel behavior, and a clearly specified accounting point. Implement it as its own plan task rather than sharing TCP stream assumptions.
- Keep rate/connection gates injectable at the session boundary; avoid database, telemetry-server, or control-plane dependencies in the Engine module.
- Performance measurement needs a Linux-native run. Mac/OrbStack results are useful for functional iteration but do not establish the Linux `splice` or 700 Mbps target.

## Validation

- Fast feedback: `cd engine && go test ./...` and `go test -race ./...`.
- Linux path: run the Engine test command in the repository's Ubuntu 24.04 Docker/Actions environment; add syscall-path evidence before asserting `splice` is active.
- Full integration validation will be added to the shared CI entry without replacing `make phase1-check` until the Engine target is stable.
- Benchmark output must record CPU, memory, kernel/OS, architecture, command, connection count, warmup, duration, and raw result. An environment-limited or below-target result is evidence, not a phase progress blocker.

## Risks

- `io.Copy` can hide optimized `ReaderFrom`/`WriterTo` behavior, so a metered wrapper must not be described as zero-copy.
- Linux `splice` may return short counts or unsupported errors; fallback must preserve bytes and meter exactly once.
- TCP EOF is directional. Closing the entire connection on the first EOF breaks half-close.
- UDP idle expiry races with packet arrival and response delivery; session replacement must not route an old response to a new session accidentally.
- M0's direct throughput target depends on a qualified Linux environment. No dedicated 4-vCPU/8-GiB test host has been confirmed in this turn.
