---
phase: 02-m0-m0-engine-dataplane
reviewed: 2026-11-03T09:03:11.837Z
depth: standard
files_reviewed: 5
files_reviewed_list:
  - engine/relay/tcp.go
  - engine/relay/tcp_linux.go
  - engine/relay/tcp_portable.go
  - engine/relay/udp.go
  - engine/relay/udp_test.go
findings:
  critical: 1
  warning: 1
  info: 0
  total: 2
status: issues_found
review_mode: inline-no-subagents
---

# Phase 02: Code Review Report

**Reviewed:** 2026-11-03T09:03:11.837Z
**Depth:** standard
**Files Reviewed:** 5
**Status:** issues_found

## Summary

Inline review found an unbounded UDP session allocation path and a usage-accounting boundary that is not yet represented by a durable reservation contract. No subagents were used, per the user's instruction.

## Critical Issues

### CR-01: UDP Sessions Have No Admission Bound

**File:** `engine/relay/udp.go:101`
**Issue:** Every new client address allocates a connected socket, a map entry, a response buffer, and a reader goroutine. Idle expiry eventually reclaims inactive sessions, but there is no global or per-client cap, so a sustained stream of unique clients can exhaust file descriptors and goroutines before sessions expire.
**Fix:** Add a bounded session admission control with explicit saturation behavior, then test concurrent unique-client load, rejection behavior, and cleanup after cancellation/expiry.

**Resolution update (2026-11-03):** Plan 02-04 added global and per-source-IP session limits checked before socket/goroutine allocation. Saturation drops the incoming datagram and calls the optional `OnSessionLimit` callback. Race tests and `make phase2-engine-check` passed locally and in Actions on SHA `c79c2d1194a66711a68f66c2b5e7a5f474a8381d` (run 41611109213).

## Warnings

### WR-01: Meter Failure Happens After Datagram Delivery

**File:** `engine/relay/udp.go:214`
**Issue:** The target accepts a datagram before the meter callback runs. If metering fails, the relay stops, but the delivered payload cannot be withdrawn and this API has no durable reservation/unknown-debt contract. The same general boundary applies to successful TCP writes.
**Fix:** Define the gate and reservation/usage-batch contract before claiming trusted billing; preserve an upper bound when acknowledgement is unknown and test meter failure after destination acceptance.

**Resolution update (2026-11-03):** Plan 02-05 defines an injected Gate/Session/Batch contract, reserves before TCP/UDP writes, commits only accepted bytes, and makes resolver failure locally unknown without retry. The contract explicitly requires authoritative implementations to retain reservations and unknown upper bounds. Local race, vet, Docker, and same-commit Actions checks passed on SHA `213c0d83674686cc07eb7dc3b9d6d3db2560a6c8` (run 41613449912). No durable production quota resolver is claimed by the standalone Engine.

---

_Reviewed: 2026-11-03T09:03:11.837Z_
_Reviewer: Codex inline review (main window)_
_Depth: standard_
