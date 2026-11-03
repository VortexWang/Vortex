# Phase 4 Context: M2 Multi-Hop Engine Restart

## Restart boundary

The prior Phase 4 artifacts under `legacy-v1/` are historical only. This
restart begins from research because the previous work exercised library
boundaries and benchmark smoke without a startable multi-relay runtime.

## Locked decisions

### Runtime tracer

- Deliver one `vortex-relay` executable with configuration-selected roles:
  `source`, `relay`, and `destination`.
- The first production-quality tracer slice must start processes, establish
  authenticated VLink links, forward TCP and UDP through three relays, and
  expose enough accounting evidence for qualification.
- Engine remains independent of PostgreSQL, Valkey, Agent and Hub. Runtime
  wiring may inject adapters through the existing process-local interfaces.

### Topology and reproducibility

- Provide both Docker Compose and native Linux launch scripts.
- Compose and native launchers must consume the same node configuration,
  certificates, route definition and workload commands.
- The route is `source -> relay-1 -> relay-2 -> relay-3 -> destination`.
  Bypass routes must be rejected or impossible in the qualification fixture.

### Accounting boundary

- The tracer uses an auditable in-process accounting adapter for Phase 4.
- The adapter must expose reservation, successful-write commit, unknown
  outcome, release and drop evidence through a stable interface compatible with
  later Agent/Hub integration.
- This adapter is test/qualification authority only; it must not be described
  as the durable PostgreSQL/SQLite ledger.

### Performance gate

- If the runtime cannot start the three-relay topology, or accounting evidence
  is incomplete, the benchmark command fails and qualification is blocked.
- `environment-limited` may describe a separately identified host limitation,
  but cannot convert a missing runtime or missing evidence into success.
- Native and Actions runs must retain the same commit, configuration digest,
  topology, workload, raw output and qualification verdict.

## Scope retained from M2

- Authorized candidate paths, monotonic path generations and TCP drain.
- UDP termination/reopen on generation changes without opaque replay.
- Immutable billing snapshots, bounded reservations and unknown upper bounds.
- Share fencing/CAS and idempotent Prepare/Commit/Abort.
- Independent bounded TCP/UDP scheduling and classified drops.
- NFR-003, NFR-005, NFR-006 and D-04 evidence only after the runtime tracer
  and topology are operational.

## Explicit non-goals

- No PostgreSQL/Valkey integration in Engine.
- No Agent/Hub durable ledger implementation in this phase.
- No claim that the in-process adapter satisfies final production durability.
- No benchmark pass based only on unit tests, injected state machines,
  loopback UDP or a hosted runner without the three-relay runtime.

## Research questions

1. Which existing VLink APIs and relay functions can form the tracer without
   weakening TLS identity, OPEN authorization or accounting semantics?
2. What configuration and certificate format can be shared by Compose and
   native launchers without adding an unapproved dependency?
3. How should the process supervise links, streams, shutdown and failures so
   the qualification workload has deterministic cleanup?
4. Which accounting adapter events and raw artifact schema are sufficient to
   prove successful-write-only metering and unknown reservation retention?
