---
phase: "03"
slug: "m1-vlink-m1-vlink-engine"
status: verified
threats_open: 0
asvs_level: 1
created: "2026-11-04"
---

# Phase 03 — Security

## Trust Boundaries

| Boundary | Description | Data Crossing |
|----------|-------------|---------------|
| VLink peer to frame decoder | Untrusted authenticated peer supplies frame bytes | Frame type, flags, lengths, stream IDs and payloads |
| TLS link to peer identity | TLS peer identity is bound to VLink HELLO transcript and exporter | Node ID, boot ID, generation, nonces and capabilities |
| OPEN request to local runtime policy | Remote peer requests use of a locally configured rule and path | Rule ID/revision, path generation, hop, visited nodes and network |
| Local target resolver to outbound socket | Runtime-selected names are resolved and checked before dialing | Numeric target address and port |
| Protocol events to metrics/log sinks | Engine emits bounded protocol observations | Low-cardinality classes and opaque correlation IDs |

## Threat Register

| Threat ID | Category | Component | Severity | Disposition | Mitigation | Status |
|-----------|----------|-----------|----------|-------------|------------|--------|
| T-03-01 | Tampering / DoS | Frame codec | high | mitigate | `codec.go` validates type, flags, stream ID and fixed payload bounds before allocation; codec and fuzz tests pass. | closed |
| T-03-02 | Spoofing / replay | TLS HELLO | critical | mitigate | `hello.go` requires TLS 1.3, verified chains, exporter-bound signatures, canonical identities and injected peer freshness policy; HELLO tests cover exporter mismatch, role binding, stale generation and replay. | closed |
| T-03-03 | Elevation of privilege | TCP tracer | high | mitigate | OPEN carries no target; target comes from the local authorization callback; tracer and OPEN tests pass. | closed |
| T-03-04 | Spoofing | Stream allocator | high | mitigate | `codec.go` allocates initiator-parity IDs monotonically without reuse; stream tests cover lifecycle and isolation. | closed |
| T-03-05 | DoS | Mux windows | high | mitigate | `window.go` bounds stream and aggregate credit, blocks TCP cancellably, drops excess UDP, and wakes waiters on close; window race tests pass. | closed |
| T-03-06 | DoS / isolation failure | Stream dispatcher | medium | mitigate | `stream.go` and `dispatch.go` reset recoverable stream violations and close on link-level protocol errors; isolation tests pass. | closed |
| T-03-07 | Spoofing / authorization bypass | OPEN policy | high | mitigate | `open.go` validates local rule revision, path generation, hop, peer, visited nodes and runtime authorization; authorization tests pass. | closed |
| T-03-08 | SSRF | Target resolution | critical | mitigate | `target.go` resolves once, rejects forbidden/mixed answers, applies allowlisted prefixes and dials the checked numeric endpoint; target tests cover rebinding and forbidden ranges. | closed |
| T-03-09 | DoS / authorization bypass | UDP admission | high | mitigate | UDP requires OPEN_OK, applies stream and aggregate windows, resets unauthorized datagrams and closes on link loss without replay; integration and UDP tests pass. | closed |
| T-03-10 | DoS | Link pool | high | mitigate | `pool.go` reserves dials within configured hard bounds and uses cancellation-aware bounded backoff; concurrent cap and cleanup tests pass. | closed |
| T-03-11 | Replay / duplicate execution | Link arbitration | high | mitigate | Authenticated link rank arbitrates duplicate links deterministically; pool and mux admission tests pass. | closed |
| T-03-12 | Availability / stale state | Reverse connector | high | mitigate | `reverse.go` bounds retries, stops on cancellation, closes old streams, and forbids UDP replay; reverse lifecycle tests pass. | closed |
| T-03-13 | DoS | Control-frame limiter | high | mitigate | `limits.go` applies per-frame token buckets and cumulative link-close thresholds; limiter and fuzz tests pass. | closed |
| T-03-14 | Information disclosure / cardinality | Observability | medium | mitigate | `observability.go` uses enum allowlists, bounded event retention and opaque IDs; address, tenant, rule and stream labels are absent; privacy-shape tests pass. | closed |
| T-03-15 | Misrepresentation | Benchmark evidence | medium | mitigate | Benchmark manifests retain host/topology/accounting limitations and distinguish candidate measurements from full qualification. | closed |
| T-03-16 | Test reliability | Fuzz/race harness | medium | mitigate | `make phase3-vlink-check` bounds fuzzing and runs race tests, Docker tests and benchmark capture; the complete local command passed. | closed |

## Accepted Risks Log

Runtime certificate revocation, current-path membership and durable freshness lower bounds are injected policy/state boundaries. Phase 03 requires those callbacks before authenticated mux admission but does not implement their control-plane or durable runtime authorities; these integrations remain in their owning later phases. The protocol package is not a standalone production authorization service.

## Security Audit Trail

| Audit Date | Threats Total | Closed | Open | Run By |
|------------|---------------|--------|------|--------|
| 2026-11-04 | 16 | 16 | 0 | Inline GSD security verification |

## Sign-Off

- [x] All threats have a disposition
- [x] Accepted residual scope documented
- [x] `threats_open: 0` confirmed
- [x] `status: verified` set in frontmatter

**Approval:** verified 2026-11-04
