---
phase: 02-m0-m0-engine-dataplane
plan: 03
status: complete
commit: 88d59c220c536601ff22b9a5762362c6553e8a14
requirements:
  - ENG-006
  - ENG-007
  - ENG-008
---

# Plan 02-03 Summary: UDP Sessions and Idle Expiry

Implemented direct UDP forwarding with a connected upstream socket per client address. Responses are returned to the originating client only while that exact session remains current. Activity refreshes on requests and responses; zero timeout selects the 60-second default, and expiry or cancellation closes owned sockets and response workers. Successful UDP writes are passed to the directional meter.

## Verification

- `cd engine && go test -race -count=20 ./relay` passed on local macOS arm64.
- `cd engine && go vet ./...` passed.
- `make phase2-engine-check` passed on local OrbStack Linux arm64; the scratch container executed TCP, splice, and UDP tests.
- The same commit passed GitHub Actions Ubuntu 24.04 amd64, run [41604182140](https://github.com/VortexWang/Vortex/actions/runs/41604182140).
- GSD plan structure validation passed for `02-03-PLAN.md`.

## Scope Notes

The relay currently has no rate/connection gate or reservation/usage batch contract. Those remain subsequent Phase 2 work. The 700 Mbps benchmark has not been run; it is a development target whose measured gap will not block correctness work.
