# Phase 3: M1 VLink Engine - Research

**Researched:** 2026-11-03
**Domain:** authenticated multiplexed TCP/UDP links inside the standalone Go Engine
**Confidence:** High for repository boundaries and Go APIs; medium for wire defaults until contract review

## Summary

Phase 3 should implement a small VLink transport package inside `engine/` using Go standard library networking and TLS. The package must expose injected identity, authorization, freshness, accounting, and observability interfaces so it remains independent of PostgreSQL, Valkey, the control plane, and production Agent/Hub lifecycle. The existing 8-byte big-endian header and 16 KiB payload ceiling are retained as draft inputs; the first implementation task must assign codepoints, field widths, limits, canonical serialization, and a content hash before interoperability is claimed.

The safest delivery order is a tracer slice (TLS link, HELLO, one authorized TCP OPEN, DATA, FIN, close) followed by strict frame parsing and failure isolation, UDP admission and flow control, reverse/pool lifecycle, and abuse/observability evidence. Every slice needs malformed-input, replay, resource-cleanup, and same-command Docker/Actions tests.

## Architectural Responsibility Map

| Responsibility | Phase 3 Engine | Later owner | Boundary |
| --- | --- | --- | --- |
| Frame encoding, mux state, windows, link lifecycle | Owns | None | Pure Engine APIs and `net.Conn`/`tls.Conn` |
| Certificate parsing and peer identity extraction | Owns validation hook | PKI/runtime supplies roots and revocation source | Inject verifier; no database/control-plane call |
| Rule/path authorization and target selection | Evaluates local snapshot | Agent/Hub and backend provide snapshot later | Inject deterministic policy interface |
| Usage reservation/commit and unknown upper bound | Reuses Phase 2 accounting gate | Runtime supplies gate implementation | Preserve reserve-before-write and conservative unknown state |
| Pool/retry metrics and structured events | Owns emission | Runtime exports/sinks later | Low-cardinality metric interface; bounded event sink |
| Multi-hop scheduling and path transitions | Excluded | Phase 4 | No path orchestration in this phase |

## Standard Stack

- `[VERIFIED: engine/go.mod]` Go module is `vortex/engine` and declares Go 1.27.3.
- `[VERIFIED: go doc crypto/tls.ExportKeyingMaterial]` Go TLS exposes `ConnectionState.ExportKeyingMaterial(label, context, length)` after a TLS 1.3 handshake; the method returns an error when exporter prerequisites are not met.
- `[VERIFIED: go doc crypto/tls.ConnectionState]` `ConnectionState` exposes negotiated version, verified peer chains, peer certificates, stapled OCSP response, and handshake completion. It does not itself enforce application freshness, path membership, or revocation policy.
- `[CITED: RFC 8446]` TLS 1.3 authenticates the handshake and establishes shared keying material; application HELLO data still needs an explicit binding to the completed connection.
- `[VERIFIED: AGENTS.md]` Engine must not access databases or actively call the control plane; PostgreSQL is authoritative outside this module and Valkey is non-authoritative cache only.
- `[VERIFIED: existing phase 2 code]` TCP/UDP relay and accounting packages already provide injected gate/accounting boundaries and Linux-specific relay behavior.

No new third-party Engine dependency is recommended. A token bucket, bounded event retention, fixed-width codec, stream state machine, and pool backoff are small enough to implement and test locally. Any dependency added later requires a separate version/license review.

## Architecture Patterns

### 1. Strict framed transport

Use a reader that reads exactly 8 header bytes, validates type/flags/length/stream combination before allocating, then reads exactly the bounded payload. Dispatch only after the whole frame is present. Unknown types and nonzero reserved flags close the link; stream-local semantic failures reset one stream. Keep encoding and decoding symmetrical and expose a byte-level test corpus.

### 2. Canonical HELLO and freshness

Build HELLO from a canonical fixed-order byte representation. Include protocol version, canonical local/peer node IDs, boot IDs, generations, both nonces, explicit role/direction labels, and an exporter-derived binding. Verify certificate SAN identity and the injected revocation/path-membership result before accepting the HELLO. Persist or inject peer freshness lower bounds so a new TLS connection cannot replay an older boot/generation tuple.

### 3. Explicit mux state

Maintain a link state machine and a per-stream state machine. Allocate monotonically increasing stream IDs from initiator parity, reject reuse, and require OPEN/OPEN_OK before DATA or DGRAM. Give each stream a byte window plus an aggregate link window; TCP waits for credit and UDP drops/counts over-window datagrams. Link parsing or identity errors close all streams; stream errors reset only the affected stream.

### 4. Local authorization and reverse links

OPEN carries rule/path coordinates, hop and visited information, and original-client metadata, but never a downstream target. The receiver validates the peer identity, locally applied revision/generation, hop, visited list, and gates. It selects the target from local runtime configuration, rejects private/reserved destinations and DNS rebinding, and returns stable non-sensitive error classes with an audit correlation ID. Reverse receivers dial their configured ingress peer with bounded retry/backoff.

### 5. Pool and duplicate-link control

Pool state needs explicit min, max, hard cap, health and concurrency inputs. Creation and retry must be bounded and jittered. A duplicate link policy must be deterministic before a second link can expose streams; link loss stops new opens, closes existing streams, and leaves TCP re-opening to an upper idempotency policy. UDP is ended rather than replayed opaquely.

### 6. Abuse and observability

Apply per-link and per-control-frame token buckets plus hard bounds. Initial excess rejects/resets; sustained excess closes the link and increments a low-cardinality reason class. Metrics may label only peer role, frame type, and error class. Structured events may include link/trace IDs but must not put address, tenant, rule, or stream IDs into metric labels. Record state transitions and security rejections fully; aggregate normal DATA/DGRAM traffic and enforce bounded retention.

## Don't Hand-Roll

- `[CITED: Go crypto/tls API]` Do not implement TLS, certificate chain validation, or exporter derivation; configure the standard library and inject application policy checks.
- `[CITED: RFC 8446]` Do not treat a successful TLS handshake as proof of VLink boot/generation freshness or path authorization.
- `[VERIFIED: Phase 2 boundary]` Do not import database drivers or control-plane clients into Engine.
- Do not use arbitrary JSON/TLV framing for v1 when the locked discussion requires fixed layouts and pre-allocation bounds.
- Do not use per-stream, tenant, address, or rule identifiers as Prometheus-style metric labels.

## Common Pitfalls

1. **Allocation before validation:** a malicious length prefix can consume memory. Validate every field and aggregate length before allocation.
2. **Handshake-only replay defense:** TLS session resumption does not replace peer boot/generation lower bounds and nonce/exporter binding.
3. **Authorization drift:** accepting an OPEN based on a peer-supplied target or stale revision can bypass local policy. Require local snapshot validation and stable rejection classes.
4. **Window deadlock:** separate stream and aggregate windows need wake-up and close paths; test blocked TCP writes, window updates, and link shutdown.
5. **UDP bypass:** accepting DGRAM before OPEN_OK creates an authorization hole. Test unauthorized DGRAM on a valid and invalid stream.
6. **Leaked goroutines/connections:** every link, pool retry, stream, timer, and blocked writer needs cancellation and leak tests.
7. **Duplicate authorization:** concurrent duplicate links can both accept OPEN. Establish deterministic link arbitration and make authorization state idempotent.
8. **False performance claims:** Docker/Actions smoke and benchmark results are not dedicated-hardware NFR proof. Record first-packet and throughput measurements separately and preserve unmet status.

## Code Examples

```go
state := tlsConn.ConnectionState()
if state.Version != tls.VersionTLS13 || !state.HandshakeComplete {
	return errProtocolIdentity
}
exporter, err := state.ExportKeyingMaterial("vortex/vlink/hello", helloContext, 32)
if err != nil {
	return errProtocolIdentity
}
```

`[CITED: Go crypto/tls API]` This illustrates the standard-library exporter boundary only; the canonical HELLO bytes, signature scheme, peer policy, and freshness checks remain VLink responsibilities.

## Open Questions

- Exact numeric type codes, flag allocation, integer widths, length prefixes, per-field limits, error code values, and contract hash are still to be frozen in `contracts/vlink/v1/`.
- The revocation source and freshness-state persistence mechanism must be injected by the runtime; Engine needs a deterministic interface and negative tests, not a database implementation.
- Pool min/max/cap, control-frame rate/burst, cumulative violation threshold, event retention, and window defaults need bounded values plus rationale and tests.
- `[VERIFIED: REQUIREMENTS-SOURCE.md, NFR-002/004/007]` NFR-002 requires at least 600 Mbps and at least 70% of the same-protocol/same-encryption baseline; NFR-004 limits reverse-vs-forward difference to 5%; NFR-007 limits median first-byte extra wait to 5 ms, measured separately for pool and mux. Phase 3 can measure but must not claim dedicated hardware qualification from Docker/hosted CI.
- GitHub Actions runner architecture availability and exact Docker/Compose versions remain environment evidence, not code assumptions.

## Validation Architecture

- Unit tests cover codec round trips, every malformed/truncated/bounded input, canonical HELLO bytes, stream ID parity/reuse, state transitions, windows, token buckets, and stable error classes.
- Fuzz tests target frame and HELLO decoders with allocation bounds and no panics.
- Integration tests use `net.Pipe` or local TLS listeners for forward and reverse links, plus a local TCP/UDP target; they assert OPEN gating, data exchange, half-close, link loss cleanup, and no duplicate authorization.
- Race and leak tests run on the same package command as Phase 2, with explicit goroutine/connection cleanup assertions.
- A new `phase3-vlink-check` command must be runnable in local Docker and GitHub Actions on the same commit. It should include unit, fuzz-smoke, race, integration, and benchmark-smoke subsets; dedicated NFR evidence remains separate.

## Ready for Planning

- The tracer-first slice is feasible with current repository boundaries and no new Engine dependency.
- Plans must freeze the contract before claiming interoperability, then expand transport, identity, mux/UDP, pool/reverse, abuse/observability, and evidence in dependency order.
- Every plan must include threat references, automated verification, and explicit residual risks for unresolved revocation and dedicated-performance evidence.
