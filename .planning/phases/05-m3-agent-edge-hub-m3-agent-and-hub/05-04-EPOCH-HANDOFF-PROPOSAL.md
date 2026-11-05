# 05-04 Authorization epoch handoff proposal

Status: owner approval required. Frozen authorization 1.0.0 remains unchanged.

## Decision

Approve `vortex.authorization.v1` 1.1.0 with the exact additive field and rules
below. Control Protobuf remains 1.2.0. The 1.1.0 payload schema changes
`schema_version` const to `1.1.0`, changes title to `VORTEX Authorization Update
1.1.0`, and adds this optional top-level property. No existing required field,
limit, record layout, restriction or wire field number changes.

```json
{
  "epoch_handoffs": {
    "type": "array",
    "maxItems": 4096,
    "items": {
      "type": "object",
      "additionalProperties": false,
      "required": ["transfer_id", "parent_authorization_id", "source_id", "from_epoch", "to_epoch", "remaining_bytes", "sealed_sequence", "record_sha256"],
      "properties": {
        "transfer_id": {"type": "string", "format": "uuid"},
        "parent_authorization_id": {"type": "string", "format": "uuid"},
        "source_id": {"type": "string", "format": "uuid"},
        "from_epoch": {"type": "integer", "minimum": 1},
        "to_epoch": {"type": "integer", "minimum": 2},
        "remaining_bytes": {"type": "integer", "minimum": 0, "maximum": 9223372036854775807},
        "sealed_sequence": {"type": "integer", "minimum": 0},
        "record_sha256": {"type": "string", "pattern": "^[0-9a-f]{64}$"}
      }
    }
  }
}
```

## Normative semantics

1. Only the same authenticated mTLS Hub issuer that owns the existing parent and
   source may deliver a transfer. Node identity and boot remain those of the
   enclosing payload. A node request cannot install a handoff. This is the
   authenticated issuer's attestation of its durable transaction, not a signed
   proof independently verifiable against a production backend. Phase 5 backend
   remains protocol-test-only; production provisioning is Phase 6.
2. The Hub must commit revocations/restrictions, close the prior issuer, seal S
   and transfer R in one FULL SQLite transaction before emitting the attestation.
   `record_sha256` binds the immutable Hub handoff ledger payload, and
   `sealed_sequence` is S. It does not mean the backend has already ACKed S.
3. `to_epoch = from_epoch + 1 = payload.signer_epoch`. An existing source must
   have an accepted lineage to `from_epoch`; no gap, branching successor, parent
   substitution, issuer change or boot change is accepted. The transfer ID and
   its complete content are immutable. Duplicate identical content is a no-op;
   changed content is a stable conflict. The same source/from epoch has at most
   one successor. A source absent at the receiver must first have explicit
   authority installed; a handoff cannot fabricate prior ownership.
4. The Agent stores the attestation and transferred unissued budget before
   accepting successor credit grants. New unique credit grants under that
   successor atomically consume this local transfer budget; replay consumes
   nothing. Their total cannot exceed R. The Hub remains the authority for the
   original allocation; the Agent's transfer budget is an additional bound.
5. Old issued grants/leases remain valid within their original time, scope and
   revocation boundaries. Neither transfer nor ACK releases old reservations or
   unknown debt. The path generation, rule revision, billing snapshot, original
   parent and absolute hard deadline remain unchanged. Changing signer epoch
   does not force path generation change or terminate existing sessions.
6. Apply revocations before successor eligibility in the same transaction. A
   revoked source root blocks new grants; already revoked authorizations remain
   revoked. Late old-epoch updates cannot mint additional rights after handoff,
   but exact payload replay may reproduce the original receipt.
7. 1.0.0 payloads remain accepted with existing behavior. They cannot carry the
   new property and cannot authorize successor grants over existing lineage.
   1.1.0 payloads without handoffs retain existing behavior. Old 1.0.0-only nodes
   reject 1.1.0; there is no silent fallback interpretation.
8. Trusted authenticated recovery heartbeat may resume online mode only within
   the existing absolute autonomy deadline and only in the same atomic handoff
   transaction. A reconnect, newer heartbeat, new boot or timeout alone is not
   handoff evidence and cannot reopen authority or release resources.

## Verification and traceability

- Exact replay, changed transfer content, wrong issuer/boot/parent, missing
  predecessor, forked epoch, excess successor grants and late old updates.
- Old authorization remains usable until original expiry/revocation; transfer
  preserves outstanding and unknown balances and path generation.
- Crash before/after FULL commit, disk full, lost delivery and reopen replay.
- Same-SHA Docker and Actions; raw ledger/hash evidence; all gates fail closed.
- Review mappings for HUB-005..018, AGT-003..006 and SUP-B4-001; record SemVer,
  approval hash, frozen schema hash and changelog before normative changes.
