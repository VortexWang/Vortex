# Durable session block implementation boundaries

Status: funding/checkpoint/admission Store APIs and an experimental Agent gate
implemented. Real runtime renewal, idle bounds and qualification still required.
No parameter lock or wire change.
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

## Checkpoint persistence progress

CheckpointRuntimeFundingBlock now atomically transfers confirmed consumption
from the residual hold to an independently identified committed usage batch,
including outbox insertion and demand-consumption observation. The caller uses
the expected current residual; stale updates fail. Exact checkpoint retries
remain valid after the parent residual is settled by recovery. Exhaustion marks
the zero tail returned without refunding funds, freeing the direction slot.

This supplies the persistence portion of step 3. It does not supply the
exclusive allocator, write-admission barrier, bounded flush scheduling or Engine
integration in steps 2/4/6. Until those exist, the API is not used for forwarding.
Tests currently simulate transaction failure and close/reopen recovery; actual
process kill, disk-full and native block runtime qualification remain required.

## Write admission progress

AdmitRuntimeFundingBlockWrite validates the current durable session, exact block
residual and metadata, held and current grant, runtime/share leases, connection
count and shared rate bucket in one transaction. It reads current lease IDs from
the session; caller-supplied replacement IDs cannot substitute authority. No
credit is debited again. Control updates committed first are visible to the
next admission, matching the existing per-write transaction ordering.

This is an admission primitive, not a concurrent child allocator. Every call
spends rate again; ambiguous commit errors must fault the caller. The exclusive
Agent allocator still must bound aggregate in-flight subwrites and serialize
checkpoints with allocation. The persistent rate bucket still writes per
admission, so this step does not establish a throughput improvement or meet
the measured D-04 performance requirement. Runtime wiring remains disabled.

## Experimental Agent allocator progress

NewExperimentalFundingBlockGate is an explicit opt-in constructor implementing
the Engine accounting.Gate/Session interfaces. It owns one block per direction,
serializes allocation/admission/checkpoint operations and tracks aggregate
in-flight upper bounds. Concurrent writes wait for capacity, with cancellation.
Known short writes checkpoint actual usage; returned child reservations only
restore in-memory availability. Close prevents admission immediately and releases
unused tails only after that direction's outstanding callbacks finish. An
unknown write freezes the entire residual, faults the gate, and prevents further
recycling; failed checkpoints retain the durable upper bound for boot recovery.

This first adapter checkpoints each completed write, so it has no delayed known
usage to flush. It still persists rate on every admission. Production runtime
defaults remain unchanged. It has not yet been exercised with real VLink traffic,
automatic renewal, delayed responses or the native multi-hop attack matrix.
Bounded idle block return, adaptive block sizing under partially available
credit, and renewed-authority retry integration remain required. No candidate
performance or final D-04 parameter claim follows from interface-level tests.

## Idle return and renewal progress

The experimental adapter now returns fully idle residuals after a one-second
local timer, with a two-second settlement context. Generation checks under the
session mutex invalidate stale callbacks; timers never return in-flight or
poisoned bounds. Close stops timers. Storage failure faults admission and leaves
the durable residual for recovery. One second is an experimental implementation
value, not a measured D-04 parameter or a Hub grant-return protocol.

When target-sized allocation is denied, the adapter may try the current write
size under the identical Store authority checks. It cannot combine grants or
borrow other scopes. An idle block whose admission is denied is settled before
one fresh allocation attempt; current authority selection then renews the
session. The existing bounded missing-authority wait/demand notification is
used, retaining its 200 ms total deadline per attempt cycle.

Tests cover old-expiry replacement with known usage pinned to the renewed grant,
idle release without premature in-flight refund, and exact final scoped-credit
consumption below the configured target. Real VLink, delayed/lost Hub response,
performance and full hostile qualification still remain to be run in this mode.

## Real VLink integration progress

The existing real TCP/UDP + mTLS control renewal fixture now explicitly installs
the experimental gate before Engine start. Both ordinary and delayed-response
block modes execute two continuous renewals after initial issue, lost-response
replay and initial-authority expiry. Agent, Hub and protocol backend usage
ledgers still reconcile; tests wait for local idle tails before final checks.

Integration exposed direction fragmentation: an idle opposite-direction block
could retain the credit needed for the next response. Allocation denial now
returns only proven-idle, non-poisoned blocks from the same session before
retrying the existing authoritative funding call. A regression proves that an
in-flight opposite block is never reclaimed.

Block occupancy can also change renewal size: demand prepared while unused
credit is held can issue up to the configured 4096-byte target; idle return then
raises usable credit above that target. The fixture records actual funded
issuance, checks its per-round bounds and exact conservation, rather than
assuming every later renewal is exactly 3000 bytes. This occupancy/timing effect
remains part of D-04 measurement; it is not evidence for selecting parameters.

The real fixture additionally permits proactive delivery of the next valid
grant while the previous round waits for idle-tail return. Intermediate waits
accept that monotonic issuance; final assertions still require exact Agent/Hub
issued totals, exact metered payload bytes, zero unresolved/unknown balance and
source conservation. Three repetitions of both ordinary and delayed modes
passed after this sequencing correction. This is 12 TCP/UDP subcases, not the
complete attack or sustained-load matrix.

## Bounded group admission persistence seam

AdmitRuntimeFundingBlockBatch supports up to 64 existing-block writes in one
transaction, coalescing exact block/scope identities and checking aggregate
bytes against the residual. It unions conservative time bounds, validates every
distinct group and debits shared rate atomically. Any later denial rolls back
earlier rate changes. The limit is an implementation bound, not a D-04 value.

The Agent must still own every participating block and account for existing
in-flight writes. This Store API alone is not a concurrent allocator or a
capability cache. Next: integrate a bounded session queue with per-request
deadlines, cancellation before/after commit, fair grouping by direction, and
checkpoint cooperation. Do not return a Batch until group admission commits;
canceled committed entries may waste rate tokens but must release only proven
unused in-memory credit. Unknown commit results fault the group. The current
runtime does not yet invoke this API, so native concurrency remains failed.
