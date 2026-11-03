# VLink Encoding Contract 1.0.0

Status: **frozen**. This document defines the first interoperable VLink wire version. It is an internal node-to-node contract, not a public API. V1.11 is a reference source; current project decisions and GSD acceptance govern.

## Compatibility and evolution

- This is the first frozen contract, version 1.0.0.
- A wire-incompatible change requires a new major contract version and explicit protocol negotiation. Compatible additions require a minor version and assigned codes; patches may clarify behavior without changing bytes.
- Every release records this file's SHA-256 in `encoding.sha256`. Generated implementations and fixtures must be reproducible from this contract.
- Implementations reject unknown frame types, nonzero reserved flags, and unsupported protocol versions. They do not guess or ignore unknown semantics.

## Transport and identity

- Transport is TCP protected by TLS 1.3 mutual certificate authentication. TLS 1.2 and lower are rejected.
- A trusted platform CA issues a certificate whose URI SAN contains exactly one canonical `urn:vortex:node:<lowercase-UUID>` identity. The receiver checks the configured revocation snapshot and current authorized-path membership before accepting a peer. A node ID is encoded on wire as its 16 UUID bytes in network byte order.
- HELLO is a three-message exchange on stream 0: OFFER, RESPONSE, CONFIRM. Each message includes the fixed fields below and a signature. OFFER carries the initiator identity, boot ID, generation, role, nonce and an offer proof over its identity fields plus TLS exporter. RESPONSE and CONFIRM sign the same canonical transcript containing both identities, boot IDs, generations, protocol version, both nonces, explicit client/server role labels and exporter context. A receiver accepts the peer only after its proof and freshness checks pass.
- HELLO signature keys are the leaf certificate public keys. Ed25519, ECDSA P-256 and RSA-PSS with RSA keys of at least 2048 bits are supported; other key types are rejected. Ed25519 signs the message directly. ECDSA and RSA-PSS sign its SHA-256 digest; ECDSA uses ASN.1 DER and RSA-PSS uses a 32-byte salt. Signature length is a u16 and is bounded to 512 bytes.
- Peer freshness state is keyed by canonical node ID. A lower boot/generation tuple, repeated nonce, invalid conservative time anchor, or mismatched connection/exporter binding fails the link. Lower-bound persistence is provided by the runtime interface; Engine does not access storage.

## Frame header

Each frame has an 8-byte header followed by exactly `length` payload bytes. All multibyte integers are unsigned big-endian.

| Offset | Width | Field | Rule |
|---:|---:|---|---|
| 0 | 1 byte | `type` | Code from the table below |
| 1 | 1 byte | `flags` | MUST be zero in v1 |
| 2 | 2 bytes | `length` | Payload bytes, 0..16384 |
| 4 | 4 bytes | `stream_id` | Stream ID; zero only for link-control frames |

The decoder reads and validates the complete header before allocating payload memory. It rejects truncated headers/payloads, invalid type/stream combinations, length above 16384, and invalid per-frame field bounds. A structural error closes the link.

## Frame types and stream rules

| Code | Name | Stream | Payload |
|---:|---|---|---|
| 0x01 | HELLO | 0 | phase u8; protocol_version u16 (0x0100); role u8 (0 client, 1 server); local_node_id 16 bytes; local_boot_id 16 bytes; local_generation u64; local_nonce 32 bytes; peer_node_id 16 bytes; peer_boot_id 16 bytes; peer_generation u64; peer_nonce 32 bytes; local_capability_count u16 (0..64); local capabilities as u16 codes; peer_capability_count u16 (0..64); peer capabilities as u16 codes; signature_length u16 (1..512); signature bytes |
| 0x02 | OPEN | nonzero | rule_id 16 bytes; rule_revision u64; path_generation u64; hop_index u16; network u8 (1 TCP, 2 UDP); visited_count u8 (0..16); visited node IDs (16 bytes each); client_family u8 (4 or 6); client_length u8 (4 or 16); original client address bytes |
| 0x03 | OPEN_OK | nonzero | Empty |
| 0x04 | OPEN_ERR | nonzero | error_code u16; correlation_id 16 bytes |
| 0x05 | DATA | nonzero | TCP bytes; empty payload is invalid |
| 0x06 | FIN | nonzero | Empty; half-closes the sending direction |
| 0x07 | RST | nonzero | error_code u16 |
| 0x08 | DGRAM | nonzero | One complete UDP datagram; payload must be 1..16384 bytes and is never fragmented |
| 0x09 | WINDOW | zero or nonzero | credit_delta u32; stream 0 updates aggregate link credit, nonzero updates that stream |
| 0x0a | PING | 0 | opaque_nonce u64 |
| 0x0b | PONG | 0 | exact opaque_nonce u64 from PING |
| 0x0c | GOAWAY | 0 | error_code u16; last_accepted_stream_id u32 |
| 0x0d | PADDING | 0 | Reserved and invalid in v1; receiving it closes the link |

Client-initiated streams use odd IDs; server-initiated streams use even IDs. Each initiator allocates monotonically increasing IDs in its parity space and never reuses an ID during a link lifetime. ID zero is reserved for link-control frames. Exhaustion requires closing and replacing the link.

HELLO, PING, PONG, and GOAWAY MUST use stream 0. OPEN, OPEN_OK, OPEN_ERR, DATA, FIN, RST, and DGRAM MUST use nonzero stream IDs. WINDOW may use zero or a nonzero stream ID as described above. Any unknown code or invalid combination closes the link and emits a protocol-error event.

## OPEN authorization and errors

OPEN does not carry a downstream target address or session ID. The receiver validates the peer certificate identity, local rule revision/path generation, hop index, visited list and applicable runtime gates. It selects the next hop and target only from local configuration, rejects private/reserved destinations and validates DNS results before dialing. A stale revision/path generation is rejected with a stable stale/no-route class.

| Code | Name |
|---:|---|
| 0x0001 | no_route |
| 0x0002 | unauthorized |
| 0x0003 | quota_exhausted |
| 0x0004 | rate_limited |
| 0x0005 | conn_limited |
| 0x0006 | target_unreachable |
| 0x0007 | stale_revision |
| 0x0008 | protocol_error |

OPEN_ERR never includes internal destination or policy detail. Every rejection emits an audit event with a correlation ID. TCP may send DATA immediately after OPEN; the receiver bounds pending-open data and either admits the stream or returns OPEN_ERR. UDP DGRAM is forbidden until OPEN_OK. Unauthorized DGRAM resets that stream and is audited.

## Flow control and failure scope

- Each stream has an independent byte window and the link has an aggregate byte window. TCP waits for credit. UDP datagrams that cannot fit are dropped and counted.
- Stream-local semantic errors reset that stream. Framing, unknown type/flag, certificate identity and HELLO errors close the link. Sustained per-link violations close after the configured threshold.
- Link loss stops new OPENs and closes existing streams. An upper layer may re-OPEN TCP only under its idempotency policy. UDP sessions end and are never opaquely replayed.

## Variable-length field limits

- `node_id`, `rule_id`, and visited node IDs are UUIDs encoded as 16 bytes in network byte order. Certificate SAN text uses canonical lowercase UUID form.
- HELLO signature uses a u16 length, 1..512 bytes. Each local/peer capability list is a u16 count followed by at most 64 u16 capability codes (128 bytes maximum each).
- OPEN visited nodes use a u8 count, 0..16, followed by exactly 16 bytes per node. Original-client address length is explicit and must match its family (IPv4 4 bytes, IPv6 16 bytes).
- HELLO phases are OFFER=0, RESPONSE=1, CONFIRM=2. Client sends OFFER with zero peer fields and zero peer capability count. Server validates the offer and sends RESPONSE containing both sides' values and its transcript signature. Client verifies RESPONSE and sends CONFIRM with the same ordered values and its transcript signature. Server verifies CONFIRM before accepting the client. For RESPONSE/CONFIRM, client values and capabilities always precede server values and capabilities in the canonical signed transcript regardless of sender; phase byte and signature fields are excluded from that transcript. The signature input is `"VORTEX-VLINK-HELLO-v1\0" || canonical_transcript || exporter_bytes`; OFFER uses the same domain and a canonical offer transcript with zero peer values.
- `exporter_bytes` are 32 bytes from TLS `ExportKeyingMaterial` using label `EXPORTER-VORTEX-VLINK-v1` and context `SHA-256(protocol_version_u16 || client_node_id_16 || server_node_id_16)`.
- Every decoder validates count, length, and total-frame bounds before allocating a variable-length field.

## Operational constraints

- Metrics may label only peer role, frame type and error class. Address, tenant, rule and stream IDs are not metric labels.
- Structured events include trace/link IDs. State transitions and security rejections are retained; normal DATA/DGRAM traffic is aggregated, not logged per packet. Local event retention is bounded by configuration.
- Link and per-frame control-rate limits, pool limits, retry bounds, and violation thresholds are runtime-configurable within implementation hard bounds.

## Change record

| Version | Change |
|---|---|
| 1.0.0 | First frozen VLink v1 frame, identity, OPEN, flow-control and failure-scope contract |
