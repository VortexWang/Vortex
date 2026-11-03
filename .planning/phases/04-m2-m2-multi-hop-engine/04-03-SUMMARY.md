---
phase: 04-m2-m2-multi-hop-engine
plan: 03
status: complete
---

# Plan 04-03 Summary

Implemented and exercised the five-process qualification topology:

- Strict topology manifest validation and deterministic SHA-256 digest.
- Shared certificate/config generator for native and Docker backends.
- Five-service Compose topology and native process launcher.
- Single `vortex-relay` binary now runs source, relay and destination roles.
- Real three-relay TCP route with local target execution and structured OPEN
  authorization logs.
- Added `MuxStream.WaitOpen` so DATA cannot precede OPEN_OK.
- Preserved half-close responses by allowing both bridge directions to drain.
- Added retry/backoff for relay next-hop startup ordering.

Verification:

```text
cd engine && go test -race ./...
BACKEND=native QUALIFICATION_DIR=<temp> VORTEX_RELAY_BIN=<binary> tools/phase4/topology-smoke.sh
```

Both passed. The smoke returned the exact payload
`vortex-three-relay-smoke` after crossing source, relay-1, relay-2, relay-3 and
destination. This is topology correctness evidence, not yet performance
qualification or production accounting evidence.
