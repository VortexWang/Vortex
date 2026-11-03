---
phase: 02-m0-m0-engine-dataplane
plan: 05
status: complete
completed: 2026-11-03
requirements: [ENG-003, ENG-004, ENG-005, ENG-009, ENG-010, ENG-011]
---

# Plan 02-05 Summary: Gates and Usage Batches

## Delivered

- Added the standard-library-only `engine/accounting` contract: Gate admission, session policy, pre-write reservation, and one-shot Commit/Return/Unknown batch resolution.
- Resolver failures mark the local batch unknown, retain the full reserved upper bound in its state, and reject a second resolution attempt.
- Added `TCPWithGate`; each read chunk is reserved before writes, and only successfully accepted bytes are committed. Gated TCP uses the portable write path so reservation precedes socket writes; ungated Linux TCP retains splice.
- Added UDP Gate session admission and per-datagram reservations for both request and response paths. Gate denials drop datagrams and are reported through the configured callback.
- Extended the scratch Docker fixture to run both relay and accounting test binaries.
- Tests cover gate denial, accepted-byte settlement, TCP short writes, zero-byte write failure, resolver failure/unknown state, duplicate resolution, UDP request/response accounting, and UDP gate denial.

## Verification

- `cd engine && go test -race ./...` — passed.
- `cd engine && go vet ./...` — passed.
- `make phase2-engine-check` — passed locally, including both Linux scratch test binaries.
- Same-commit `make phase2-engine-check` in GitHub Actions passed on SHA `213c0d83674686cc07eb7dc3b9d6d3db2560a6c8`; [run 41613449912](https://github.com/VortexWang/Vortex/actions/runs/41613449912).
- Phase 1 regression workflow also passed on that commit; [run 41613449906](https://github.com/VortexWang/Vortex/actions/runs/41613449906).

## Boundary

The Engine defines and enforces the call order but does not own durable quota state. Gate/Resolver implementations must persist reservations before returning them, apply outcomes idempotently, and retain unknown upper bounds in their authoritative state until reconciliation. Database and control-plane integration remain outside the Engine module.
