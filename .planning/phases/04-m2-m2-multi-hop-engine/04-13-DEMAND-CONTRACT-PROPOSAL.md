# D-04 demand transport proposal

Status: proposed, not approved. Baseline: vortex.control.v1 1.2.0.
Proposed compatible extension: 1.3.0. No approved contract/hash is changed here.

## Concrete wire change

Add `CreditDemand credit_demand = 16` to NodeMessage.body, and
`CreditDemandDenied credit_demand_denied = 15` to ControlMessage.body.

```proto
message CreditDemand {
  string request_id = 1;
  string delegation_id = 2;
  uint64 raw_bytes = 3;
  uint32 ttl_seconds = 4;
}
message CreditDemandDenied {
  string request_id = 1;
  CreditDemandDenial reason = 2;
}
enum CreditDemandDenial {
  CREDIT_DEMAND_DENIAL_UNSPECIFIED = 0;
  CREDIT_DEMAND_DENIAL_INVALID_SCOPE = 1;
  CREDIT_DEMAND_DENIAL_INSUFFICIENT_FUNDS = 2;
  CREDIT_DEMAND_DENIAL_EXPIRED = 3;
}
```

Success uses the existing AuthorizationUpdate. Its payload.update_id equals
CreditDemand.request_id; the installed immutable credit grant must exactly
match the original demand. There is no grant conveyed by CreditDemandDenied.

## Identity and bounds

Request/delegation IDs are canonical lowercase UUIDs. Amount is 1..MaxInt64
raw bytes. TTL is 1..300 whole seconds. Node/boot/sequence use the existing
NodeMessage envelope. Hub derives node identity from verified client TLS and
compares it with the envelope and installed delegation, checks boot, current
signer epoch, source budget, deadline and offline/autonomy restrictions. The
request cannot supply or alter rule, snapshot, path, source or share limits.
The Agent dispatches only to the authenticated Hub UUID pinned in its durable
DemandScope. Success and denial require that same verified identity.

The source/path/snapshot/epoch demand key and coalesced UUID are already stored
before dispatch. A request does not confer permission; Engine continues to use
only committed, valid grants and runtime/share leases. No business data uses
this stream. Existing messages/field numbers and billing units are unchanged.

## Durable idempotency and refusal

The Hub stores one immutable outcome per (Hub, request UUID), binding the whole
request and authenticated receiving node/boot. Success atomically deducts the
installed delegation and stores the exact response. A definitive denial is
also committed before its response is sent; it records that this request did
not issue any rights. Replaying a request returns the original outcome even
if funds or deadlines later change. Changed fields under the same UUID are
conflicts; they never overwrite the original success or denial.

Only a matching persisted definitive denial can retire an Agent's pending
request without installing a grant. This state transition does not refund any
existing grant, reserved usage, unknown usage or unknown issuance. A fresh
request may then use the current smaller target/funds estimate. A storage
failure, malformed request, identity failure, connection close, timeout or
unsupported peer is not proof of denial: keep the original pending UUID.
A stale/duplicate denial cannot retire a subsequent request. Unspecified or
unknown denial enum values are rejected, not treated as definitive denial.

## Scheduling and compatibility

Use the existing single-sender control loop and durable sequence allocator.
Keep at most one pending request per immutable scope; do not spawn unbounded
per-session requests. Bound scans/batches and use bounded retry/backoff so
usage acknowledgements and lease refresh remain serviceable. A data-path wait
for missing authority is at most the candidate 200 ms and may time out while
the durable request remains pending; timeout never authorizes a write.

This is a compatible protobuf extension, but older endpoints cannot implement
its semantics. Enable request dispatch only for explicitly configured capable
Hub/Agent pairs, default off until deployment compatibility is established.
No fallback through NodeEvent payload or fabricated local credits is allowed.
This proposal does not lock D-04's 8 MiB floor, 30-second target or 30% waterline.

## Required implementation and proof

1. Update contract version, exact approved hash, changelog, requirements map
   (ENG-037, ENG-050 and Agent/Hub durable authorization) and generated Go code.
2. Persist both issue and definitive-denial outcomes; test commit failure,
   concurrent duplicate, changed payload, lost response, restart and replay.
3. Dispatch persisted Agent requests over authenticated gRPC; bind both response
   forms to the originating Hub/node/boot/scope and commit before exposing them.
4. Test quota reduction/retry without duplicated rights, late success/denial,
   old-boot/old-epoch responses and unknown liabilities remaining frozen.
5. Run real TCP/UDP workload with adaptive demand, delayed/lost responses,
   funds/rate/connection bounds and evidence from both durable ledgers.
6. Retain parameter-lock, idle drain/proof and complete multi-hop attack review
   as separate outstanding acceptance requirements.

Owner decision requested: approve this exact proposal as the 1.2.0 -> 1.3.0
control extension. Approval permits implementation and regeneration; it does
not certify that tests, D-04 or Phase 4 have passed.
