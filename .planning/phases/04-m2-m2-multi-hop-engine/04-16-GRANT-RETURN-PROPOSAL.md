# D-04 sealed grant return protocol proposal

Status: proposed, not approved. Baseline: vortex.control.v1 1.3.0.
Proposed additive extension: 1.4.0. Existing approved contracts and hashes are
unchanged. Execute under 04-12; approval of 1.3.0 does not cover this extension.

## Concrete wire change

Add `GrantReturn grant_return = 17` to NodeMessage.body and
`GrantReturnAck grant_return_ack = 16` to ControlMessage.body.

```proto
message GrantReturn {
  string return_id = 1;
  string grant_id = 2;
  string delegation_id = 3;
  string origin_boot_id = 4;
  uint64 signer_epoch = 5;
  uint64 initial_raw_bytes = 6;
  uint64 used_raw_bytes = 7;
  uint64 unknown_raw_bytes = 8;
  uint64 unused_raw_bytes = 9;
  uint64 known_batch_count = 10;
  uint64 unknown_batch_count = 11;
  bytes batches_sha256 = 12;
  bytes proof_sha256 = 13;
}
message GrantReturnAck {
  string return_id = 1;
  string grant_id = 2;
  bytes proof_sha256 = 3;
  uint64 returned_raw_bytes = 4;
}
```

IDs are canonical lowercase UUIDs except existing usage batch IDs retain their
current encoding. Grant, delegation and origin boot must match the issuer's
immutable issued record. Signer epoch must equal the original issued epoch,
not an epoch supplied to renew or transfer rights. NodeMessage.node_id must
match verified mTLS identity; its boot_id is the current authenticated stream
boot. origin_boot_id can be older only for already persisted, sealed proof from
that node; changing the envelope boot does not change the original proof.
The Agent pins delivery and ACK validation to the grant's original Hub identity.
Hub derives source, rule, revision, path and snapshot from the issued grant;
the request cannot redirect funds or replace those bindings.

All amounts are raw bytes, 0..MaxInt64; initial is positive. Verify without
integer overflow: initial = used + unknown + unused. Counts are uint64 with
checked accumulation. Hashes are exactly 32 bytes. The deterministic protobuf
serialization of GrantReturn with proof_sha256 cleared is hashed with SHA-256.
Persist the exact serialized proof and hash before sending; reject unknown wire
fields in this message to keep the canonical representation unambiguous.
This wire hash is separate from the existing local GrantSettlementProof JSON
hash; never treat the current JSON digest as a 1.4.0 wire proof digest.

## Final batch commitment

The final batch set contains every committed known and unknown usage batch
for this grant; returned pre-write reservations have no consumption and are
excluded. Known batches must bind the issued node, original boot, source via
grant, rule and snapshot; sum only deltas for this grant. Unknown batches must
bind the same immutable identities and their upper bounds. Batch identities
cannot appear as both known and unknown. Duplicate delivery is not a new entry.

Canonical commitment: sort by kind (known byte 0, unknown byte 1), then raw UTF-8
batch ID bytes in ascending byte order. Each entry is:
`kind:1 || batch_id_length:4 big-endian || batch_id || payload_sha256:32`.
Hash the concatenation with SHA-256 (empty set uses SHA-256 of empty bytes).
Payload hashes use the existing persisted deterministic usage payload bytes.
Counts and the summed raw amounts must match as well as the commitment. Do not
use arrival order, locale sorting or only totals. Stream the sorted rows using
bounded pages; do not retain an unbounded batch list or include it on the wire.
The commitment requires a finalized, immutable per-grant set on both sides.

## Agent fencing and delivery

1. Serialize local admission and seal in SQLite. No open session may reference
   the grant as either current grant or original anchor. Once sealed it never
   becomes active again, including restart or a repeated AuthorizationUpdate.
2. Resolve every reservation durably as committed, returned or unknown. Pending
   reservations prohibit proof creation. Unknown amounts stay charged; expiry,
   elapsed idle time, disconnect or absent ACK never converts them into unused.
3. In one transaction, audit exact grant conservation, freeze the final batch
   set, and persist immutable return_id/proof/outbox. A second preparation gets
   identical bytes. A changed ledger or proof is a conflict, not a new return.
4. Send outstanding usage and the proof through the single control sender with
   bounded pages, retry/backoff and priority for usage/lease traffic. No separate
   data-plane RPC or Engine database/control dependency is introduced.
5. Persist a matching ACK before marking the outbox complete. ACK must match
   Hub, return_id, grant_id, proof hash and returned amount exactly. Receipt
   never reactivates the local grant or adds Agent spendable balance. Fresh
   credit still requires a separate committed CreditDemand issuance.

## Hub reconciliation and atomic return

Persist the incoming proof as pending, bound to authenticated node and issued
record, before reconciliation. A matching proof may arrive before some usage
batches; retain it pending and send no ACK until the full commitment and amounts
match. Missing usage is not zero usage. Conflicting identity, hash, totals,
duplicate IDs or extra batches fail closed. Do not accept a receipt as authority
from an unauthenticated endpoint or treat a timeout as successful reconciliation.

Within one SQLite transaction:
- verify proof identity/hash, final batch commitment, known/unknown sums and
  original grant amount against durable issuance and usage;
- reject any conflicting prior return for the same grant, regardless of a new
  return_id; identical proof retries reuse the original receipt;
- permanently mark this grant reconciled/closed, store the immutable return
  receipt and credit exactly unused bytes back to its original delegation;
- retain original gross issuance and consumed/unknown liabilities. Track returns
  separately. Before any backend ownership transfer, audit: original delegation
  allocation + accepted returns = current unissued remainder + gross issued +
  nonspendable recovered funds. Ownership transfers require separate balanced
  debit/credit entries, never a second accepted return. Per grant, unused is returned at
  most once. A return must not decrement gross issue history or erase usage.

New use of a closed grant is denied. Later exact duplicate usage is idempotent;
new or changed usage for a reconciled grant is rejected/quarantined and raises
an accounting fault, never silently changes its final proof or returned amount.
All writes must commit before ACK. Failed commit yields neither an ACK nor a
partial credit. Crash after commit but before ACK returns the same receipt on
retry. Returned amounts may be zero; the grant is still finalized exactly once.

For a still-owned open delegation, the returned remainder can be considered by
normal issuance checks, which independently require trusted time, active signer,
valid deadlines, scope and funds. Return acceptance alone is not permission to
sign. If delegation or epoch ownership has moved, been revoked, or expired,
record the proven return as nonspendable recovered funds instead of adding it to
an active allocation. Only the authenticated backend ownership/reconciliation
path may transfer these recovered funds later, once. No automatic credit into a
new node, boot, delegation, source or epoch. This proposal does not extend any
lease/deadline or reopen a revoked signer.

An ACK means the return transaction and disposition are durable at this Hub;
it does not mean credit is immediately spendable or that a backend transfer has
occurred. Return handling and backend reconciliation preserve a durable outbox
and the existing accounting acknowledgement boundary, including autonomous mode.

## D-04 idle lifecycle and trust boundary

After a conservatively measured idle interval, fence excess grants and settle
before return. Preserve the explicit warm allowance through independently valid
remaining grants or a newly issued grant; never split/resize an immutable grant
locally. Prevent the normal cold-target controller from immediately refilling
an idle rule. Twenty-four-hour reclaim similarly requires final proof. Candidate
10-minute / 1MiB / 24-hour values remain unlocked until measured review.

This is a crash/retry-safe return from the trusted Agent accounting component.
mTLS authenticates that component but cannot prove an arbitrarily compromised
Agent stopped using secret state. No remote-attestation claim is made. All
externally supplied IDs, amounts and batches are nevertheless independently
validated against the Hub's issued/usage ledger. A new executor cannot fabricate
an old executor's seal; absence of persisted proof retains its unknown liability.

## Compatibility and required verification

Explicit opt-in only for configured 1.4.0 peers; unsupported peers leave proof
pending and funds unavailable. Existing message numbers are unchanged. No opaque
NodeEvent or UsageAck fallback. Approval permits implementation and generation,
not acceptance of D-04 or Phase 4.

Required work after approval:
1. Bump version, exact hash, changelog and requirements map for ENG-037/ENG-050
   and trusted accounting; regenerate pinned protobuf outputs and contract tests.
2. Agent schema/outbox/final batch commitment and immutable seal enforcement;
   Hub schema, per-grant usage reconciliation, atomic credit and receipt replay.
3. Test unknown exclusion, pending batch refusal, sum overflow, same-total but
   different batches, forged node/boot/Hub/epoch/source, changed hash/return ID,
   reordered/duplicate/lost usage and ACK, injected disk failure at each commit,
   both process restarts, sealed grant resurrection and late extra usage.
4. Test return versus new issue/handoff/revocation races, nonspendable recovered
   funds and subsequent backend one-time reconciliation. Audit every source.
5. Real TCP/UDP consumption -> idle -> seal -> proof -> return -> new issuance
   plus delayed/lost responses under sustained dedicated-host load. Run one clean
   SHA with Node/Agent/Hub/Engine, Docker and CI evidence recorded separately.
6. Implement/measure session blocks, idle warm allowance and sustained candidate
   scans; keep D-04 unlocked until throughput, latency and tied-up funding review.

Owner decision requested: approve this exact 1.3.0 -> 1.4.0 extension for
implementation, including final batch commitment and one-time unused return.
