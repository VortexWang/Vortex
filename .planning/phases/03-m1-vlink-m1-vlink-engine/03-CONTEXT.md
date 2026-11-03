# Phase 3: M1 VLink Engine - Context

**Gathered:** 2026-11-03
**Status:** Ready for planning

<domain>
## Phase Boundary

Phase 3 delivers authenticated VLink links for authorized paths: normative frame encoding, TLS 1.3/mTLS identity binding, multiplexed bidirectional tunnels, reverse link establishment, OPEN authorization, and UDP stream admission. It does not implement multi-hop scheduling and path orchestration (Phase 4), production Agent/Hub runtime integration (Phase 5), backend policy authority (Phase 6), or final dedicated-hardware performance qualification (Phase 8).

V1.11 is a reference, not a frozen acceptance baseline. The VLink v1 encoding contract is currently a draft; this discussion locks implementation decisions for planning but does not itself approve or freeze the contract. Contract version/hash review remains an explicit planning and verification task.

</domain>

<decisions>
## Implementation Decisions

### Frame Encoding and Evolution
- **D-01:** Assign explicit numeric frame type codes and use a fixed field layout for each frame type.
- **D-02:** Encode variable-length fields with fixed-width length prefixes; define independent maximum item counts and byte sizes, and validate bounds before allocation.
- **D-03:** Close the link and record a protocol error for unknown frame types or nonzero reserved flags; do not silently ignore unknown semantics.
- **D-04:** Partition stream IDs by initiator parity, allocate them monotonically, and never reuse an ID during a link lifetime.

### TLS Identity and Freshness
- **D-05:** A trusted CA certificate SAN carries a canonical `node_id`. Accept a peer only after certificate revocation and current authorized-path membership checks pass.
- **D-06:** Normalize and sign HELLO fields bound to the TLS exporter, including both peer identities, both `boot_id` and `generation` values, protocol version, both nonces, and explicit role/direction labels.
- **D-07:** Track each peer's accepted boot/generation lower bounds. Combine them with a conservative time anchor and nonce/exporter binding to reject stale or replayed HELLOs.

### Multiplexing, Flow Control, and Pooling
- **D-08:** Reset only the affected stream for stream-local failures. Close the whole link for frame parsing or link identity failures.
- **D-09:** Use an independent byte window per stream and an aggregate byte window per link. TCP waits for window credit; UDP datagrams that exceed available credit are dropped and counted.
- **D-10:** Give each peer pool a configured hard cap and explicit minimum/maximum values. Scale based on concurrency and health, with retry backoff.
- **D-11:** On link loss, stop opening streams and close existing streams. Let the upper layer re-OPEN TCP only under its idempotency/business policy; end UDP sessions without opaque replay.

### Reverse Establishment and OPEN Authorization
- **D-12:** The authorized receiver for a reverse path initiates an outbound TLS connection to the configured ingress peer, retrying with backoff.
- **D-13:** Reject OPEN when its rule revision or path generation differs from locally applied configuration; return a stable stale-revision/no-route class error and require an upper-layer OPEN after configuration is current.
- **D-14:** Validate peer `node_id`, locally applied rule revision/path generation, hop index, visited-node list, and applicable gates against local runtime state. Do not fetch authorization from the control plane during OPEN.
- **D-15:** A receiver selects the destination only from local rule/runtime configuration. OPEN cannot carry a downstream target address. Reject private/reserved destinations and defend against DNS rebinding.
- **D-16:** UDP DGRAM requires a successful OPEN/OPEN_OK first. DGRAM on an unauthorized stream resets that stream and emits an audit event.
- **D-17:** OPEN rejection uses stable error codes without exposing internal destination details; every rejection emits an audit event with a correlation ID.

### Abuse Protection and Operations
- **D-18:** Close the link immediately for structural or identity errors. Reset recoverable stream-level violations and close the link after a per-link cumulative violation threshold; planning must define the threshold and reset conditions.
- **D-19:** Apply per-link, per-frame-type token buckets and hard limits to control frames. Reject/reset initial excess and close the link on sustained excess; planning must define concrete rates and burst limits.
- **D-20:** Expose link/frame-type traffic counts, rejection and close reasons, pool state, active stream counts, and flow-control wait time. Structured events include trace/link IDs.
- **D-21:** Keep metric labels low-cardinality: peer role, frame type, and error class only. Do not use addresses, tenant IDs, rule IDs, or stream IDs as metric labels. Logs may correlate with link/trace IDs.
- **D-22:** Record state transitions and security rejections in full. Aggregate normal DATA/DGRAM traffic instead of logging each packet; bound local event retention.

### the agent's Discretion
- Research and planning may choose concrete frame code values, fixed-width integer widths, maximum field limits, token-bucket values, pool sizing defaults, retry/backoff bounds, and retention limits, provided they satisfy the decisions above and are included in the contract review.
- Select the Go TLS/mux implementation structure and test topology based on repository patterns and measurable correctness/performance evidence; do not introduce a third-party Engine dependency without phase-specific review.

</decisions>

<canonical_refs>
## Canonical References

### Project Scope and Decisions
- `.planning/PROJECT.md` — restart governance, system boundaries, and current project decisions.
- `.planning/ROADMAP.md` § Phase 3 — M1 goal, requirements, success criteria, and downstream phase boundaries.
- `.planning/REQUIREMENTS.md` — retained requirement IDs and source traceability.
- `.planning/STATE.md` — accumulated decisions and current project position; refresh before planning because Phase 2 completion was recently recorded.

### Protocol and Prior Phase Context
- `contracts/vlink/v1/encoding.md` — current VLink draft: TLS identity, 8-byte frame header, 16 KiB payload limit, OPEN semantics, and unresolved wire decisions. Draft only until reviewed and frozen.
- `docs/VORTEX 需求规格说明书 V1_11.md` — reference source for ENG-012..025 and NFR-002/004/007; not a frozen acceptance baseline.
- `.planning/phases/02-m0-m0-engine-dataplane/02-CONTEXT.md` — Engine isolation, injected gate boundary, Linux splice and validation decisions.
- `.planning/phases/02-m0-m0-engine-dataplane/02-VERIFICATION.md` — verified M0 boundary and explicit later-phase deferrals.

</canonical_refs>

<code_context>
## Existing Code Insights

### Reusable Assets
- `engine/relay/tcp.go` and `engine/relay/udp.go` — direct TCP/UDP relay and injected policy boundaries from Phase 2; useful as the local stream/relay interface, but they do not implement VLink.
- `engine/accounting/accounting.go` — reservation batch state behavior; link work must preserve the rule that Engine policy is injected and uncertain outcomes remain conservative.
- `contracts/vlink/v1/encoding.md` — initial frame/header and authorization draft to refine, version, hash, and review.

### Established Patterns
- `engine/` is a standalone Go module and Phase 2 verification confirmed no database or active control-plane imports. VLink stays inside this boundary.
- `make phase2-engine-check` is the established Docker/race verification pattern; Phase 3 should define a shared VLink check entry for local Docker and Actions on the same commit.
- User-confirmed GSD workflow: discuss, research, plan, execute, verify, then phase transition. The user previously requested work in the main window without agents; keep this phase inline.

### Integration Points
- VLink transport and mux connect to the Phase 2 TCP/UDP relay and injected gate/accounting interfaces.
- Reverse links connect an authorized receiver to its configured ingress peer; full multi-hop path scheduling remains Phase 4.
- Agent/Hub consumers and durable runtime policy implementations arrive in Phase 5/6 and must use the formal in-process Engine boundary.

</code_context>

<specifics>
## Specific Ideas

- Preserve the existing draft's 8-byte big-endian header and 16 KiB payload ceiling unless research demonstrates a contract-level reason to revise them; any change needs a versioned contract decision.
- Keep protocol structural failures distinct from recoverable stream failures so malformed framing cannot desynchronize unrelated streams.
- Make protocol authorization decisions local and deterministic; the wire peer supplies identity and requested rule/path coordinates, never an arbitrary destination.
- Measure the forward/reverse tunnel and mux/pool first-packet targets from the roadmap separately; do not infer them from the Phase 2 direct relay benchmark.

</specifics>

<deferred>
## Deferred Ideas

- Multi-hop role scheduling, path transitions, drain semantics, and two-phase configuration application — Phase 4.
- Agent/Hub production link lifecycle and durable local policy runtime — Phase 5; PostgreSQL-backed authority — Phase 6.
- Dedicated native Linux performance qualification — Phase 8.
- Full VTP camouflage, padding, and other traffic-obfuscation capabilities remain out of scope for this milestone.

</deferred>

---

*Phase: 3-M1 VLink Engine*
*Context gathered: 2026-11-03*
