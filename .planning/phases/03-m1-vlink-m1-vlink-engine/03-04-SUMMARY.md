---
phase: 03-m1-vlink-m1-vlink-engine
plan: 04
status: complete
requirements: [ENG-017, ENG-018]
commits: []
---

# Plan 03-04 Summary: Bounded Pools and Reverse Establishment

Added explicit mux, pool, and auto mode selection. UDP always selects mux; auto mode uses a bounded ten-minute first-packet history and selects pool only when the median is below ten seconds. Peer pools enforce configured minimum, maximum, and hard bounds, reserve concurrent dials before starting them, lease idle links in pool mode, share the least-loaded healthy link in mux mode, prewarm on demand, scale down idle links, and discard unhealthy links. Backoff is exponential, capped, injectable-jittered, and cancellation-aware.

Duplicate authenticated links converge on the direction initiated by the lexicographically lower node ID. A transcript rank breaks ties between same-direction links; the caller derives that rank from authenticated HELLO nonces. Reverse connectors use injected authenticated link creation and post-HELLO rule declaration, retry after failures or link loss, stop cleanly during backoff, and expose an explicit TCP reopen hook that requires an idempotency decision. UDP reopen is rejected.

The 03-03 OPEN policy was tightened to reject malformed original-client addresses, duplicate/zero visited nodes, and overlong paths when called directly, and to require an explicit remaining-billing authorization bit at execution points in addition to snapshot/coefficient matching.

## Verification

- `cd engine && go test -race ./vlink -run 'Test(Pool|Mode|Reverse|Cleanup)'` passed.
- `cd engine && go test -race ./...` passed on local macOS arm64.
- `cd engine && go vet ./...` passed.
- `git diff --check` passed.
- Pool tests exercise concurrent acquisition, hard caps, scaling, unhealthy-link removal, arbitration and backoff; reverse tests exercise unreachable-to-reachable retry, OPEN gating, UDP non-replay and cancellation.

## Scope Notes

- Concrete pool bounds are configurable with conservative defaults (min 1, max/hard cap 4); operators can set 2-4 links per peer as demand requires.
- The reverse connector receives its authenticated `MuxLink` from an injected dial function; certificate and HELLO verification remain owned by `NewAuthenticatedMuxLink` and runtime policy.
- Docker and GitHub Actions dual-host verification remains in plan 03-06.
