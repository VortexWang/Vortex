---
phase: 02-m0-m0-engine-dataplane
status: discussed
source: owner decisions on 2026-11-03; V1.11 is reference only
---

# Phase 2: M0 Engine Dataplane Context

## Decisions

- V1.11 informs the initial M0 design but is not a frozen acceptance baseline. Current owner decisions and phase evidence govern.
- Retain these as current development targets: Linux bare-TCP forwarding uses `splice`; TCP half-close propagates independently in each direction; UDP idle timeout defaults to 60 seconds; direct forwarding targets 700 Mbps.
- If a test run does not reach 700 Mbps, record the hardware, configuration, command, raw result, and gap, then continue correctness work. The miss alone does not block Phase 2 progress.
- Correctness, accounting, resource cleanup, and gate behavior remain required in the M0 implementation; benchmark results must distinguish unmeasured, environment-limited, and measured-below-target states.
- Engine remains an independent Go module with no database or control-plane calls. Production and simulated agents interact through the defined in-process contract.
- PostgreSQL and Valkey are external data services. Track versions, image digests, provenance, and integration compatibility when introduced; their server-image package licenses do not gate VORTEX development.
- The eleven historical records cleared during restart remain recorded as missing. Do not reconstruct them; NFR-038 does not gate development or Phase 1.
- Validation should run through the shared Docker entry locally and GitHub Actions on the same commit. A result applies only to the environment that actually ran it.

## Scope

Implement the standalone Engine's direct TCP and UDP forwarding, gate and metering interfaces, and reproducible correctness/performance test harness. Do not pull VLink, multi-hop, Agent/Hub, backend, or frontend work into this phase.

## Acceptance Guidance

- Establish the executable module and tests first, then add TCP and UDP behavior in small verified slices.
- Verify TCP half-close with a test where one direction finishes while the reverse direction continues.
- Verify UDP session expiry at the configured default and allow explicit test-time overrides without changing production defaults.
- Verify metering counts successfully written payload only and that failed/short writes cannot silently commit untransferred bytes.
- Attempt the 700 Mbps benchmark only in a documented Linux environment with adequate capacity; preserve results and continue correctness work if the environment is constrained or the target is missed.
- Keep architecture-specific `splice` behavior behind a Linux implementation with a portable fallback for development/test hosts.

## Deferred

- Any change to the four current M0 targets requires a recorded owner decision; their reference-document origin alone does not freeze them.
- Dedicated performance hardware and final release acceptance remain later evidence questions, not prerequisites for implementing the Engine.
