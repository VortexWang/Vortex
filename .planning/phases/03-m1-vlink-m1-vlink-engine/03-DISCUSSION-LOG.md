# Phase 3: M1 VLink Engine - Discussion Log

> **Audit trail only.** Do not use as input to planning, research, or execution agents.
> Decisions are captured in `03-CONTEXT.md` — this log preserves the alternatives considered.

**Date:** 2026-11-03
**Phase:** 3-m1-vlink-m1-vlink-engine
**Areas discussed:** Frame encoding and evolution; TLS identity and freshness; multiplexing, flow control and pooling; reverse establishment and OPEN/UDP authorization; abuse protection and operations

---

## Frame Encoding and Evolution

| Option | Description | Selected |
|--------|-------------|----------|
| Explicit type codes and per-frame fixed layouts | Assign numeric codes and define each payload as a fixed layout. | ✓ |
| Explicit type codes with TLV payloads | Easier extension, more parser complexity. | |
| Defer concrete layout to implementation review | Implement, then submit for contract freeze. | |
| Close on unknown type or nonzero reserved flags | Do not ignore undefined semantics. | ✓ |
| Skip explicitly ignorable extension frames | Permit negotiated ignorable frame classes. | |
| Version-specific type rules, flags must be zero | Handle unknown types by version but reject reserved flags. | |
| Initiator parity, monotonic IDs, no reuse per link | Direction-separated IDs with simple duplicate checks. | ✓ |
| Random 32-bit ID with OPEN collision detection | Resolve rare random collisions. | |
| Independent local counters plus direction bit | Encode initiator direction separately. | |
| Fixed-width field length prefixes and independent limits | Validate item/byte limits before allocation. | ✓ |
| One unified payload structure bounded only by frame size | Simpler top-level structure, less granular limits. | |
| Fixed-size maximum arrays | Avoid variable-length fields. | |

**User's choice:** Explicit type codes; per-frame fixed layouts; explicit fixed-width length prefixes with per-field limits checked before allocation; close and record protocol error for unknown type or reserved flag; initiator parity with monotonically increasing, non-reused stream IDs.
**Notes:** Existing 8-byte big-endian header and 16 KiB maximum payload remain draft inputs; contract version/hash approval is still required before freeze.

---

## TLS Identity and Freshness

| Option | Description | Selected |
|--------|-------------|----------|
| Canonical node_id in trusted certificate SAN | Check revocation and current path membership before acceptance. | ✓ |
| Certificate proves identity; authorize only at OPEN | Defer path authorization to stream creation. | |
| Certificate carries allowed path list | Reduce runtime lookups. | |
| Sign normalized HELLO fields bound to TLS exporter | Bind identities, boot/generation, protocol version, nonces and direction. | ✓ |
| Rely on TLS certificate and HELLO field equality | No additional signature. | |
| HMAC HELLO with exporter-derived key | Authenticate payload with a derived MAC. | |
| Persist peer boot/generation lower bounds | Combine conservative time anchor and nonce/exporter freshness. | ✓ |
| Validate only within each new TLS connection | No cross-connection replay state. | |
| Use generation maximum only | Omit time anchor. | |

**User's choice:** Use canonical certificate SAN `node_id`, check revocation and path membership; sign canonical HELLO bound to TLS exporter and both peer identities, boot IDs, generations, version, nonces and role/direction; track accepted boot/generation lower bounds with conservative time and nonce/exporter replay protection.
**Notes:** Exact canonical serialization and freshness-state persistence/expiry need technical research and contract review.

---

## Multiplexing, Flow Control and Pooling

| Option | Description | Selected |
|--------|-------------|----------|
| Reset stream-local failures; close link for parsing/identity failures | Isolate application stream errors from framing and identity errors. | ✓ |
| Close link for every stream error | Simplifies failure handling. | |
| Attempt local recovery for all errors | Keep the link open where possible. | |
| Per-stream byte window plus aggregate link window | TCP waits for credit; UDP drops/counts over-window datagrams. | ✓ |
| One shared link window | Simpler but one stream can consume capacity. | |
| Per-stream window only | No aggregate buffer bound. | |
| Hard-capped peer pool with explicit bounds and backoff | Scale by concurrency/health within min/max and retry bounds. | ✓ |
| Single TLS link per peer | Rely on multiplexing before adding pool links. | |
| Scale pool without a hard cap | Let connection pressure determine pool size. | |
| Stop opens and close all streams on link loss | Upper layer may re-OPEN TCP by idempotency policy; UDP ends without opaque replay. | ✓ |
| Rebuild link and restore all in-flight state | Attempt transparent stream continuation. | |
| No automatic retry | Leave all retry decisions to caller. | |

**User's choice:** Use per-stream and aggregate byte windows; pool each peer within configured hard/min/max limits and scale on concurrency/health with backoff; terminate existing streams on link loss, letting the upper layer re-OPEN TCP under its idempotency policy and never opaque-replay UDP.
**Notes:** Stream/link window sizes, pool defaults and retry limits need measurement and explicit bounds in the plan.

---

## Reverse Establishment and OPEN/UDP Authorization

| Option | Description | Selected |
|--------|-------------|----------|
| Authorized reverse-path receiver makes outbound TLS link | Connect to configured ingress peer with bounded retry/backoff. | ✓ |
| Ingress node calls back to reverse-path receiver | Requires inbound reachability. | |
| Either side may dial; first link wins | Requires deterministic duplicate-link arbitration. | |
| Reject stale rule revision/path generation | Return stable stale/no-route error; upper layer re-OPENs later. | ✓ |
| Temporarily accept previous revision | Allow a bounded grace period. | |
| Query control plane during OPEN | Resolve missing state online. | |
| Validate mTLS identity and local rule/path/hop/visited state | Do not fetch authorization during OPEN. | ✓ |
| Check certificate identity only | Skip path-coordinate validation. | |
| Signed OPEN ticket from ingress | Validate a forwarded authorization token. | |
| Require OPEN_OK before UDP DGRAM | Reset and audit DGRAM on an unauthorized stream. | ✓ |
| Implicitly create a session on first datagram | Validate only after data arrives. | |
| Require OPEN only for metered UDP | Make authorization conditional. | |
| Select destination only from local config | OPEN contains no target; reject private/reserved destinations and DNS rebinding. | ✓ |
| Allow target in OPEN under local CIDR allowlist | Trust peer-supplied address after filtering. | |
| Permit tenant-level arbitrary address | General-purpose egress. | |
| Stable errors without internal target details, audit every rejection | Correlate with request/event ID. | ✓ |
| Return detailed route and destination data | Aid peer diagnosis. | |
| Log only, no wire rejection reason | Avoid protocol error surface. | |

**User's choice:** Reverse receiver dials its configured ingress peer; reject stale OPEN coordinates; validate peer and local rule/path/hop/visited state; require OPEN_OK before UDP data; select destinations locally and reject private/reserved/rebinding targets; return stable non-sensitive error codes and audit every rejection with correlation ID.
**Notes:** The control-plane-free Engine boundary is retained. Phase 4 owns complete multi-hop scheduling; Phase 5/6 own runtime and durable policy authority.

---

## Abuse Protection and Operations

| Option | Description | Selected |
|--------|-------------|----------|
| Structural/identity errors close link; recoverable stream errors reset, threshold closes link | Accumulate per-link violations and define a threshold. | ✓ |
| Close link on every violation | Simplest but less isolated. | |
| Reset streams without cumulative link closure | Avoid cascading failures. | |
| Per-link and per-frame-type token buckets/hard limits | Reject/reset initial excess; close on sustained excess. | ✓ |
| Limit OPEN only | Leave other control frames unbounded. | |
| One shared rate limit | No frame-specific budget. | |
| Link/frame traffic, errors, pool, streams and wait metrics | Structured events correlate via trace/link ID. | ✓ |
| Lifecycle/error logs only | Minimal metric surface. | |
| Per-tenant/rule/stream metric labels | Detailed dimensions with high cardinality. | |
| Low-cardinality metric labels only | Peer role, frame type and error class; link/trace ID in logs only. | ✓ |
| Include node/rule/stream IDs in metrics | Easier per-object queries. | |
| Log all handshake and target details | Full diagnostic data. | |
| Record state transitions/security rejection fully; aggregate normal traffic | Bound local retention and avoid per-packet logs. | ✓ |
| Log every frame and packet | Complete replay trail at high cost. | |
| Sample all events, including security rejection | Lower volume but may lose evidence. | |

**User's choice:** Close immediately for structural/identity errors, reset recoverable stream errors and close after a cumulative threshold; rate-limit control frames by link and type; expose low-cardinality metrics and correlated structured logs; retain all state transitions/security rejections while aggregating DATA/DGRAM counts with bounded local retention.
**Notes:** Concrete thresholds, burst windows, metric names, event schema, and retention limits are implementation/research tasks, not yet selected values.

---

## the agent's Discretion

- Technical research may recommend exact numeric codepoints, field widths/limits, thresholds, pool defaults, retry bounds, and retention values.
- Choose Go TLS/mux structure and test topology consistent with the independent Engine boundary and dependency review.

## Deferred Ideas

- Multi-hop scheduling: Phase 4.
- Agent/Hub production lifecycle and durable policy: Phases 5-6.
- Dedicated performance qualification: Phase 8.
- Full VTP camouflage/padding: outside this milestone.
