# Phase 4 Discussion Log: Restart

## Session

- Date: 2026-11-04
- Decision: prior Phase 4 is superseded and archived under `legacy-v1/`.
- Reason: benchmark smoke existed before a startable multi-relay runtime and
  could not produce formal three-relay qualification evidence.

## Decisions

| Area | Decision | Rationale |
| --- | --- | --- |
| Runtime roles | One `vortex-relay` binary with source/relay/destination configuration | One deployable entry point, fewer divergent code paths |
| Topology | Docker Compose and native Linux scripts share config, certs and workload | Same topology semantics across CI and qualification host |
| Accounting | Auditable in-process adapter with later Agent/Hub-compatible interface | Enables tracer qualification while preserving Engine/control-plane boundary |
| Performance gate | Missing runtime or accounting evidence fails and blocks qualification | Prevents library benchmark smoke from being reported as system acceptance |

## Scope guard

The existing M2 path, generation, accounting, apply and scheduler state
machines remain in scope. The restart adds the missing executable runtime and
topology integration before any performance verdict.
