# Durable session block implementation boundaries

Status: funding reservation Store API implemented; consumption/checkpoint/runtime
integration still required. No parameter lock or wire change.
Execution context: 04-12. This note specifies the next production change, not
an alternative acceptance criterion or a claim of integrated block forwarding.

## Existing seams

ReserveRuntimeUsage currently selects/validates authority, consumes the shared
rate bucket and inserts one durable hold per write. SettleUsageReservation
atomically records final usage, returns an unused tail, updates the consumption
window and inserts the outbox batch. Node startup converts every previous-boot
unresolved reservation to typed unknown usage before Engine traffic starts.

A block cannot just reuse ReserveRuntimeUsage with a larger byte count: doing
so consumes rate allowance for future writes and can exceed the bounded burst.
Nor can one reuse a successful CheckStoredAuthorization snapshot as permission
for subsequent writes: revocation, session close and time validity may change.

## Required implementation sequence

1. Separate durable funding reservation from per-write rate/admission checks.
   Allocate one direction-bound block against a single immutable grant, with
   exact node/boot/session/flow/rule/revision/path/snapshot identity. Store its
   complete upper bound before handing it to Engine. Never combine grants or
   borrow other scopes. Keep per-session and global outstanding-block bounds.
2. Give one Agent-owned allocator exclusive ownership of each block. Suballocate
   concurrent writes without exceeding its held upper bound. Every write must
   still check session/lease/time/revocation and shared rate permission. The
   implementation must specify how these checks serialize with control updates;
   a cached valid bit without an invalidation barrier is insufficient.
3. Atomically checkpoint known consumption and the unresolved residual. Preserve
   stable identities and idempotent retries for both. A crash between forwarding
   and checkpoint retains the residual as unknown, never as available credit.
   Unknown liability cannot be silently converted to measured usage. Avoid
   overlapping a checkpointed known batch with a full-size recovery unknown.
4. Flush on block exhaustion, bounded age, direction/session close and controlled
   shutdown. A failed flush faults the gate. Unknown writes stop recycling that
   block. Return only proven unused bytes after all writers are joined. Agent
   startup fences the old owner before any block-recovery action.
5. Keep block partial checkpoints visible to demand consumption and outbox
   accounting so dynamic renewal does not stall until a large block finishes.
   Bound checkpoint age explicitly; measure the tradeoff between disk writes,
   durable unknown exposure and delayed consumption observations.
6. Wire the implementation behind explicit experimental configuration, preserving
   the current per-write mode for comparison. No permissive fallback on a block
   allocation, validation or settlement error. Engine remains database-free.

## Evidence required before candidate scan

- Exact per-source conservation for allocations, concurrent subwrites, short
  writes, returns and unknown outcomes; no negative or duplicated block tails.
- Cross-direction/session/boot/flow/snapshot and replay mutations leave funding,
  rate state and pending batches unchanged. Use the existing Store attack matrix
  as a baseline, then exercise the real Engine integration.
- Kill/restart on both sides of every block checkpoint; partial disk writes,
  SQLITE_FULL and failed commits. Already ACKed usage survives and every possible
  uncheckpointed write stays covered by the old owner's frozen upper bound.
- Revocation, expired lease, uncertain clock, reduced share and session close
  block further suballocations even if a block still has nominal unused bytes.
- Reproduce the 32-round TCP/UDP durable renewal and delayed/lost response tests
  through the block path, then run multiple streams and actual offered UDP load.
- Compare clean-SHA per-write and block paths with WAL/FULL retained. Report
  throughput, loss, p50/p95/p99 wait, fsync/checkpoint count, held/unknown peaks,
  consumption observation age and exact conservation. Only then review growth,
  target and waterline values; existing process-local scans cannot select them.

Grant return to Hub remains separate from local block-tail release. The proposed
control 1.4.0 return protocol still needs owner approval before frozen wire edits.

## Funding reservation progress

ReserveRuntimeFundingBlock now reserves a single-grant hold without consuming
per-write rate allowance. It retains runtime scope/session validation, caps a
block at 4 MiB and unresolved blocks at 4096 per node, and permits one unresolved
block per session/direction. Marker insertion and funding debit are atomic.
Block identities cannot replay through ordinary write reservation APIs. Existing
old-boot recovery retains the full unresolved upper bound as unknown.

Local Store and full Agent/Hub race regressions pass. This completes the funding
reservation portion of step 1 only: no child allocation, checkpoint or runtime
configuration uses this API yet, and a returned grant ID is not write permission.
The bounds above are implementation safety caps, not D-04 measured parameters.
