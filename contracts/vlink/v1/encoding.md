# VLink Encoding Contract 1.0.0 (Draft)

Status: **draft; not approved or frozen**. Normative sources: V1.11 ENG-010..023 and Appendix A.

## Transport and identity

- Transport is TCP with TLS 1.3 mutual certificate authentication (ENG-010).
- A peer certificate identifies `node_id`; revoked certificates are rejected at handshake.
- After TLS, HELLO carries protocol version, `node_id`, `boot_id`, `generation`, capability bits and a signature bound to the TLS exporter (ENG-011).
- Frame payload is at most 16 KiB. The fixed frame header is 8 bytes, all multibyte integers big-endian (ENG-012).

## Frame header

| Offset | Width | Field | Encoding | Source |
|---:|---:|---|---|---|
| 0 | 1 byte | `type` | Frame type code; numeric assignments pending review | ENG-013 |
| 1 | 1 byte | `flags` | Extension bits; assigned-bit policy pending review | ENG-013 |
| 2 | 2 bytes | `length` | Unsigned big-endian payload length, 0..16384 | ENG-012 |
| 4 | 4 bytes | `stream_id` | Unsigned big-endian stream identifier | ENG-012, ENG-014, ENG-041 |

Payload follows immediately and is exactly `length` bytes. A decoder must reject truncated headers, truncated payloads, payload length above 16384 and invalid stream/type combinations. Unknown type handling is negotiated by protocol version; the exact error behavior remains a freeze-blocking question.

## Frame types

The names are normative; numeric codepoints and detailed payload encodings are not assigned by V1.11 and remain pending review: `HELLO`, `OPEN`, `OPEN_OK`, `OPEN_ERR`, `DATA`, `FIN`, `RST`, `DGRAM`, `WINDOW`, `PING`, `PONG`, `GOAWAY`; `PADDING` and flags are reserved for a future VTP extension (ENG-013).

`OPEN` carries `rule_id`, rule version, path generation, `hop_index`, network type, the already-visited node list and original client address. It MUST NOT carry a downstream address or `session_id`; the link-local `stream_id` identifies the session, including UDP (ENG-014, ENG-041). Exact canonical field encoding, address representation, codepoints, flag allocation, HELLO signature format and malformed-frame error policy require approval before freeze.

## Authorization and routing behavior

An OPEN receiver validates the active rule revision/path generation, certificate peer membership, hop index, absence of its own node ID from the visited list and all applicable gates. Next-hop and target are selected only from local runtime configuration. A rejected OPEN returns one of `no_route`, `unauthorized`, `quota_exhausted`, `rate_limited`, `conn_limited` or `target_unreachable`; each rejection emits an audit event (ENG-020..023).

## Unresolved wire decisions

V1.11 does not specify numeric frame type assignments, HELLO signature serialization, variable-length field encodings, DATA/DGRAM payload details, OPEN_ERR numeric values, stream ID allocation/reuse rules or forward-compatible unknown-type behavior. No implementation may treat this draft as a frozen interoperable protocol until these are decided and its content hash is approved.
