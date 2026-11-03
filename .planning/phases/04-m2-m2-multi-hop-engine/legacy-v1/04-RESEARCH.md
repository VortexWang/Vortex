# Phase 4: M2 多跳与引擎完备 - Research

**Researched:** 2026-11-04
**Domain:** 多跳路径编排、代次排空、预扣/计量和两阶段 Engine 门控
**Confidence:** High for repository boundaries and existing Go behavior; Medium for new scheduler defaults until benchmark review

## Summary

Phase 4 should extend the standalone `engine` module with explicit, injected state machines rather than connecting Engine to PostgreSQL, Valkey, Agent, Hub, or backend code. The existing VLink package already validates local rule revision/path generation and owns stream/link lifecycle, while `accounting.Batch` already preserves an unknown reservation upper bound. The missing layer is orchestration: an authorized candidate-path set, monotonic path generations, drain-aware admission, immutable session billing snapshots, share lease fencing/CAS, idempotent Prepare/Commit/Abort, and a separate bounded UDP scheduler.

The implementation should be staged around pure state transitions first, then connected to VLink/relay data paths, then exercised with three-relay and mixed TCP/UDP fixtures. The roadmap performance thresholds remain evidence gates. Docker and GitHub Actions results are reproducibility evidence; they do not replace native Linux qualification.

## Repository Findings

| Existing asset | Evidence | Phase 4 implication |
|---|---|---|
| Reserve-before-write accounting | `engine/accounting/accounting.go`, `Batch.Commit/Return/MarkUnknown` | Reuse the one-shot resolver contract. A resolver failure becomes `Unknown` and retains the reserved upper bound; do not add a retry that changes the outcome. |
| Local OPEN policy | `engine/vlink/open.go` | Extend the injected snapshot/policy boundary with candidate path, generation, lease and session snapshot data. Keep target selection local and never accept a downstream target from OPEN. |
| Stream/path generation fields | `engine/vlink/tracer.go`, `stream.go` | Add admission checks that reject new OPEN on draining generations while allowing existing stream records to finish. Existing stream IDs remain link-local and are not migrated. |
| Link windows and pool | `engine/vlink/window.go`, `pool.go`, `dispatch.go` | TCP can continue to use stream/aggregate credit. UDP scheduling must be separate from byte-window blocking, with bounded queue accounting and drop classes. |
| UDP admission and accounting | `engine/vlink/udp.go`, `engine/relay/udp.go` | Preserve OPEN-before-DGRAM and whole-datagram reserve/commit. Queue overflow, window pressure and downstream write failure need distinct counters. |
| Docker/CI checks | `Makefile`, `tools/vlink/docker-test.sh`, `.github/workflows/phase3.yml` | Add a Phase 4 check that runs unit/race/integration/mixed-load smoke on the same SHA locally and in Actions, with artifacts recording architecture and environment. |

## Normative Constraints Mapped to Design

### Path and admission

- `ENG-026/027`: the execution point owns rate and combined TCP+UDP connection limits; reductions reject new sessions and drain excess connections, with a maximum 60-second forced-close boundary.
- `ENG-049`: a self-hosted ingress is not a trusted execution point; the first platform node on the authorized path enforces rate, connection and metering.
- `ENG-028`: a failed mandatory gate rejects new sessions and stops/cleans affected traffic. Version-protection and share CAS handoffs are explicit exceptions with their own state transitions, not generic grace periods.
- `ENG-040/041/047`: UDP session identity remains the VLink stream ID and requires OPEN/OPEN_OK before DGRAM.

Design consequence: represent a path as an ordered, immutable candidate sequence plus a generation and health/authorization status. Admission checks the generation and all hops before allocating a stream. A transition creates a new generation; old generation enters `draining`, rejects new OPEN, and tracks active streams until empty or the explicit drain deadline. No automatic route discovery may bypass the candidate list.

### Configuration application and shares

- `ENG-029..034`: configuration has monotonic versions; Prepare validates/classifies and reserves resources; Commit atomically switches the rule table; Abort releases only this attempt; actual per-object state is reported.
- `NFR-040`: same node/boot share changes require prepared state, old renewal closure, CAS, one active revision, and safe handling of lost receipts. Prepared state cannot consume.
- `ENG-027`, `LIM-006`: rate reduction releases the difference only after switch acknowledgement; connection reduction waits for drain completion before releasing the difference.

Design consequence: use a transaction record keyed by `apply_id` and a monotonic fencing generation. State transitions must be explicit (`prepared`, `committing`, `active`, `aborted`, `activation-unknown`, `released`, `disputed`). Repeated requests return the original result. A stale fence cannot commit or release a newer generation. The Engine interface reports intent and execution receipts; durable storage remains a later runtime responsibility.

### Immutable billing and unknown debt

- `ENG-035..039`: only successful business payload writes are measured; each pre-debit resolves once; UDP reserves an entire datagram; batches retain stable IDs until acknowledgement; mismatched snapshots are disputed.
- `ENG-037/050`: baseline block grows from 256 KiB toward 4 MiB, per rule/execution-point/snapshot starts at 8 MiB and renews at 30% remaining; idle shrink/reclaim requires proof of no unknown usage; unavailable authorization waits at most 200 ms.
- `NFR-044`: path coefficient changes cannot reuse old grants. Old and new sessions carry separate snapshot and grant identities.

Design consequence: bind every session, batch and final usage record to an immutable `billing_snapshot_id`, path coefficient and grant ID. Never merge reservations across snapshots. On resolver uncertainty, retain the upper bound and expose a reconciliation handle; expiry alone cannot release it.

### Fairness and performance

- `ENG-048`: DGRAM uses an independent per-link queue, default 1 MiB; it is not subject to stream/link byte windows; each write round caps DGRAM at 50% of bytes so TCP cannot starve.
- `NFR-003`: three-relay multi-hop target is 400 Mbps and at least 70% of the same-path minimum baseline.
- `NFR-005`: 1,200-byte UDP at 100 Mbps has loss <= 0.1% under the agreed workload.
- `NFR-006`: large-file checksum, ordering, no-bypass and forged-open rejection must hold.
- `NFR-007`: first-byte extra wait is <= 5 ms median, measured separately for pool and mux; the 200 ms authorization wait and version-protection exception are excluded as specified.

Design consequence: make queue capacity, round share, drain deadline and health decisions injectable and observable. A performance report must include same-path baseline, three-relay path, accounting enabled/disabled mode, topology, architecture, duration and raw output. A Docker/Actions miss is a recorded qualification gap, not permission to loosen the threshold.

## Recommended Architecture

1. **`engine/path` pure state machine:** candidate paths, hop authorization/health, generation allocation, admission, draining, old/new session binding, and no-bypass checks.
2. **`engine/share` or `engine/lease` state machine:** prepared ownership, renewal closure, fencing generation, CAS, rate/connection reduction rules, and receipt states.
3. **`engine/apply` transaction state machine:** Prepare classification, resource reservation, Commit/Abort idempotency, activation-unknown, conflict errors and release barriers.
4. **`engine/scheduler` bounded queues:** separate TCP and UDP scheduling, 50% per-round DGRAM cap, queue/window/downstream drop classes, and low-cardinality metrics.
5. **Adapters in `engine/vlink` and `engine/relay`:** consume the injected decisions, preserve existing stream and accounting APIs, and keep transport/protocol errors separate from policy-state errors.

No new third-party Engine dependency is required. Standard library synchronization, atomics, context cancellation and existing observer interfaces are sufficient. Any new dependency would require a fresh version/license review.

## Risks and Mitigations

| Risk | Mitigation |
|---|---|
| New generation accidentally admits a stale OPEN | Centralize admission in the path state machine; test stale revision, stale generation, draining and unauthorized hop combinations. |
| Old resources released after an unknown Commit/Abort | Require receipt/fencing proof before release; retain `activation-unknown` and expose reconciliation. |
| TCP starvation or UDP queue growth | Separate queues, bounded capacity, round byte cap, explicit drop counters, mixed-load race tests. |
| Old billing grants reused after path coefficient change | Include snapshot/coefficient/grant in session and batch keys; reject mismatches as disputed. |
| Deadlock between share reduction and session admission | Model prepare/fence/active transitions first; never invalidate the only effective gate before replacement is confirmed. |
| False NFR claims from virtualized environments | Preserve environment-limited status and raw artifacts; require native Linux evidence for final qualification. |

## Open Decisions for Planning

- Exact path health score/threshold and candidate selection tie-breaker.
- Drain deadline representation and force-close event semantics.
- Stable error classes for stale generation, draining, fence conflict, activation unknown and disputed usage.
- Scheduler queue byte limits and deterministic round-robin/weighted-fair algorithm.
- Receipt/query interface shape for runtime persistence, without adding persistence to Engine.
- Exact test topology for three relays and mixed TCP/UDP load in Docker and Actions.

## Sources

- `.planning/REQUIREMENTS-SOURCE.md` ENG-026..ENG-050, NFR-003, NFR-005, NFR-006, NFR-007, NFR-039, NFR-040, NFR-044.
- `.planning/research/FEATURES.md`, `.planning/research/PITFALLS.md`, `.planning/research/ARCHITECTURE.md`, `.planning/research/SUMMARY.md`.
- `.planning/phases/02-m0-m0-engine-dataplane/02-CONTEXT.md` and `02-VERIFICATION.md`.
- `.planning/phases/03-m1-vlink-m1-vlink-engine/03-CONTEXT.md`, `03-RESEARCH.md`, and `03-VALIDATION.md`.
- `engine/accounting`, `engine/relay`, and `engine/vlink` source and tests.

## Ready for Planning

The phase can be planned as dependency-ordered slices: pure path/generation model, share/apply transaction model, scheduler and VLink integration, then mixed-load and three-relay evidence. Each slice has a direct test boundary and can preserve the existing standalone Engine constraint.
