---
phase: 04-m2-m2-multi-hop-engine
plan: 09
status: partial-slice-verified
source_sha: ccb3065
---

# Plan 04-09 Evidence — Share CAS and Lease Drain

## Implemented

`Config.Share` is an explicit source-only runtime input. It requires a funded
source authority, UUID-shaped boot and transaction identifiers, positive rate
and connection limits, and a lease deadline. `vortex-relay` converts the node,
boot and transaction tuple into a process-local `share.Key`, runs
`BeginBoot -> PrepareWithID -> Commit`, checks the expected fencing revision,
and wraps the existing source credit gate with `ShareGate`.

The wrapper acquires a share lease before the inner accounting session and
releases it exactly once when the session closes. An expired configured lease
fails before acquisition, so it cannot consume or release an unknown share.
The existing manager keeps stale boot fencing, monotonic CAS generations,
idempotent transaction replay, overlap occupancy and old-generation drain.

## Verification

- `cd engine && go test -race ./runtime/accounting ./runtime/link ./runtime/relay ./runtime/config ./share ./cmd/vortex-relay` passed.
- `DOCKER_CONTEXT=orbstack make phase4-m2-check` passed, including Engine race, topology, VLink authorization, UDP bounds and recovery regressions.
- Added runtime tests cover expired lease rejection, config fail-closed checks,
  source admission capacity, and post-close share reuse.

## Boundary

This is an injected process-local share authority. Prepare/Commit state is not
persisted by Engine and no Agent/Hub authoritative receipt or restart recovery
is claimed. Therefore NFR-040 and the durable portions of ENG-038..040 remain
open until the Agent/Hub phase supplies persistence and authoritative proof.
NFR-003/005/006/007/039/044 and D-04 remain separately unqualified.
