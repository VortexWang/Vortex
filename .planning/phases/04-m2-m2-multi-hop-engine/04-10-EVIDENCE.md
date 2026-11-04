---
phase: 04-m2-m2-multi-hop-engine
plan: 10
status: partial-slice-verified
source_sha: d068de0
---

# Plan 04-10 Evidence — Early TCP DATA and Finite Attack Matrix

## Implemented

TCP streams now account for a bounded pre-authorization prefix on the local
side with the same `MaxPendingOpenBytes` limit used by the remote pending
queue. A sender can pipeline that prefix while OPEN is in flight; the peer
holds it until authorization, and `OPEN_ERR`, stream removal, stale generation
or an over-limit prefix discards/resets it. UDP remains strictly gated on
`OPEN_OK` and continues to reject pre-authorization datagrams.

The finite runtime probe already exercises unknown rule, future revision,
future generation, skipped hop, local-loop and duplicate-visited combinations,
plus direct middle/destination bypass and UDP link-loss termination. The new
stream tests prove the early TCP bound and rejection cleanup.

## Verification

- `cd engine && go test -race ./vlink ./runtime/link ./runtime/relay ./cmd/phase4-probe` passed.
- `cd engine && go vet ./vlink ./runtime/link ./runtime/relay ./cmd/phase4-probe` passed.
- `DOCKER_CONTEXT=orbstack make phase4-m2-check` passed, including the Docker
  VLink regression and finite authorization matrix.

## Boundary

This closes the bounded early-data and finite attack implementation slice. The
existing latency scripts measure end-to-end first byte and window waits, but do
not yet provide a dedicated OPEN-sent/authorization-complete timeline for every
pool and mux mode. NFR-006/NFR-007 therefore remain implementation evidence,
not formal qualification; NFR-003/005/039/040/044 and D-04 remain open too.
