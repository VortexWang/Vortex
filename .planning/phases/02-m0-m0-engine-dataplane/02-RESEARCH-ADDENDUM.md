---
phase: 02-m0-m0-engine-dataplane
researched: 2026-11-03
status: complete-inline-correction
source: verification gaps and current engine code
---

# Phase 2 Research Addendum: Gap Closure

## Findings

- `engine/relay/udp.go` allocates one connected upstream socket and response goroutine per client address. Idle expiry is bounded in time but admission is not bounded in count.
- The current meter callback runs after a destination write. It can report failure, but the payload has already been accepted by the destination and there is no reservation or unknown-debt result type.
- Existing tests establish TCP half-close, Linux splice selection, UDP routing, 60-second default expiry, replacement isolation, cancellation, and cleanup. They do not establish gate, rate, connection, reservation, or usage-batch behavior.
- The shared `make phase2-engine-check` entry is the correct local/CI verification boundary and must remain the command used for every gap-closure wave.

## Implementation guidance

- Add small interfaces for admission, rate, and reservation decisions at the relay boundary. Keep them deterministic and synchronous at the point where a datagram or stream write would otherwise be accepted.
- Make reservation lifecycle explicit: reserve before accepting a chargeable write, commit exactly once for the successful byte count, return exactly once when no bytes are accepted, and retain an unknown upper bound when the outcome cannot be confirmed.
- Add bounded UDP session options with global and per-client limits, deterministic saturation behavior, and metrics/test hooks that do not require a telemetry backend.
- Keep benchmark tooling separate from correctness tests. The harness must print environment, command, raw throughput, duration, and gap to 700 Mbps; an unavailable qualified host is recorded as unmeasured.
- Verify each gap-closure plan independently, then run the complete shared command in Docker and Actions on one commit.

## Risks

- Admission checks that occur after socket allocation do not close the denial-of-service finding; the limit must be checked before creating session-owned resources.
- A callback-shaped meter API can still lose accounting semantics if commit/return are not represented as a state machine with idempotence tests.
- A local macOS or emulated Linux benchmark cannot be presented as qualified native Linux throughput evidence.
