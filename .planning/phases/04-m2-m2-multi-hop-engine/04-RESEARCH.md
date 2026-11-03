# Phase 4 Research: Runtime Tracer and Qualification Topology

## Summary

The Engine already exposes the protocol and state-machine primitives needed for
a tracer, but it intentionally has no process entry point. The restart must
add a thin runtime composition layer before qualification work. The runtime
should reuse existing VLink authorization and accounting boundaries rather than
reimplementing frame or billing semantics.

## Existing reusable APIs

| Area | Existing API | Runtime use |
| --- | --- | --- |
| TLS/VLink identity | `NewAuthenticatedMuxLink`, `HelloConfig`, `PeerVerifier` | Establish and authenticate each hop with node identity, boot ID and generation |
| Multiplexing | `MuxLink.Open`, `MuxLink.Accept`, `AuthorizeRemote`, `MuxStream` methods | Open downstream streams and authorize/serve upstream streams |
| TCP forwarding | `ServeTCPWithGate`, `TraceTCP` | Entry and relay TCP data paths with half-close and successful-write metering |
| UDP forwarding | `UDPAdmission`, `UDPResource.Run` | Authorized DGRAM path, bounded windows and accounting |
| Rule authorization | `OpenPolicy`, `RuleSnapshot`, `GenerationAdmission` | Local rule/path/visited-hop checks at every relay |
| Path state | `engine/path.Coordinator` | Active/draining generation and old TCP/new UDP behavior |
| Billing | `accounting.Gate`, `Session`, `Batch` | In-process qualification adapter with unknown reservation retention |
| Scheduling | `scheduler.Scheduler` | Independent bounded TCP/UDP queues and classified drops |

## Runtime composition design

Use one binary with an explicit role in config:

```text
vortex-relay --config node.json --role source|relay|destination
```

The runtime owns process concerns only: config validation, certificate/key
loading, listener lifecycle, link dialing, reconnect/backoff, signal-driven
shutdown and structured evidence output. Engine packages remain unaware of
files, processes, databases and control-plane calls.

### Source

- Listen for client TCP/UDP traffic.
- Select a configured authorized path and create an `OpenRequest` with rule
  revision, generation, hop index and visited list.
- Dial relay 1 over TLS and call `MuxLink.Open` or the existing TCP tracer.
- Forward local payloads and record accounting adapter events.

### Relay

- Listen for incoming mTLS VLink links and maintain configured outbound links.
- Accept remote streams and call `AuthorizeRemote` with a local `OpenPolicy`.
- Select the configured next hop without accepting a target address from the
  peer; open the downstream stream and bridge data/DGRAM frames.
- On generation drain, reject new OPEN while allowing existing TCP to drain.

### Destination

- Accept authorized streams and connect only to configured local targets.
- Run `ServeTCPWithGate` / `UDPResource.Run` with the accounting adapter.
- Return data and closure frames through the existing MuxStream semantics.

## Configuration and certificates

`contracts/runtime/config.schema.json` is JSON Schema and is the least risky
shared format because the repository has no approved YAML/TOML dependency.
The runtime should validate a strict JSON document and use a separate
qualification overlay for process addresses, peer addresses, certificate
paths, target listeners and workload ports. Do not silently extend the frozen
runtime contract; record any overlay fields and version them independently.

Each node needs a private key and certificate whose URI SAN contains exactly
one canonical `urn:vortex:node:<uuid>` identity. The trust store must be
explicit, and peer verification must check the configured path membership and
revocation list before accepting the VLink HELLO.

## Topology

The Compose and native launchers must call the same `tools/phase4/run.sh` (or
equivalent) with a backend selector. The topology must have distinct network
namespaces/processes for source, relay-1, relay-2, relay-3 and destination.
The qualification harness must validate ordered payload/checksum, visited-hop
progression, forged OPEN rejection and absence of a bypass route before any
throughput result is accepted.

## Accounting adapter

Implement a bounded, concurrency-safe in-process `accounting.Gate` adapter that
stores event records by stable batch ID. It must:

- reserve before every TCP write or complete UDP datagram;
- commit only the successful write length;
- return unused reservation exactly once;
- retain the full reserved upper bound on unknown resolver failure;
- expose counters and JSONL events without high-cardinality metric labels;
- fail the qualification run if an event cannot be persisted in the adapter.

This adapter is suitable for tracer qualification only. Later Agent/Hub phases
must replace its authority with durable SQLite/PostgreSQL-backed integration.

## Failure and lifecycle hazards

- A relay must not accept an incoming stream before `OPEN_OK` authorization.
- A link close must cancel all stream goroutines and close local sockets.
- Reconnect must not reuse stream IDs or replay UDP datagrams.
- A stale generation must reject new OPEN and retain old TCP resources until
  drain completion.
- Config reload must use a single active generation and not mutate an existing
  session billing snapshot.
- Evidence writing must be bounded and must not block the data path forever.

## Qualification gate design

`benchmark.sh` should first start the five-process topology and run a health
handshake. Missing process, missing accounting events, bypass detection, or
checksum failure exits non-zero. Only after the health gate passes should it
run same-path and three-relay mixed load. The report must include commit,
configuration digest, topology, host, duration, throughput, ratio, UDP loss,
first-byte latency, queue/drop counters and accounting reconciliation.

## Research conclusion

The first new plan must be a vertical tracer slice: one binary, one strict
config loader, one authenticated source-to-relay-to-relay-to-relay-to-
destination TCP path, and the in-process accounting adapter. Docker/native
launch parity and mixed UDP qualification follow only after that slice is
running end to end.
