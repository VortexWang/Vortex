---
phase: 04-m2-m2-multi-hop-engine
plan: 08
status: partial-slice-verified
source_sha: 0000000
---

# Plan 04-08 Evidence — Policy-Time and Snapshot Runtime Slice

## Implemented

`Config.PolicyWindow` is strict and opt-in. It requires a validated immutable
old/new snapshot window, pre-issue and lease coverage, hard deadline, bounded
clock uncertainty and an issuer timeout no greater than 200 ms. The source
command constructs `PolicyBoundary` around the injected `CreditBook` only when
both are explicitly configured. Default and qualification-only configurations
keep their existing boundaries. `StaticPolicy` enables billing/path fields
only for an explicit source credit book plus policy window, preserving default
deny behavior.

## Verification

Source SHA: local implementation commit recorded with the follow-up summary.

- `cd engine && go test -race ./runtime/accounting ./runtime/config ./runtime/relay ./cmd/vortex-relay` passed.
- `cd engine && go vet ./runtime/accounting ./runtime/config ./runtime/relay ./cmd/vortex-relay` passed.
- `DOCKER_CONTEXT=orbstack make phase4-m2-check` passed on the clean working
  tree before this evidence commit, including full Engine race, targeted tests
  and Docker VLink regression.
- Configuration tests reject policy windows without a source credit book and
  retain the 200 ms upper bound and explicit timestamp validation.

## Boundary

This is a process-local runtime wiring slice. The standard five-process
qualification fixture was not converted to a policy-window workload in this
plan, and no durable Agent/Hub issuer or ledger exists. Therefore NFR-039 and
NFR-044 remain unqualified; the remaining runtime workload and conservation
evidence are carried by the gap register and later plans.
