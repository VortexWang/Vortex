# Phase 4 Plan 04-06: Policy-Time Boundary Slice

Date: 2026-11-05

Implemented an injectable process-local policy boundary in `engine/runtime/accounting/policy.go`. It uses conservative clock intervals, validates that the next snapshot was prepared no later than `T-60s` with a 300-second TTL and that policy/runtime/share leases cover `T`, switches at an exact boundary, pauses existing-session reservations while a <=10-second interval straddles `T`, and resumes within the tested 12-second band without changing that session's immutable snapshot. New admissions pause during ambiguity; invalid wider intervals and the hard deadline fail closed.

The adapter is not wired into `vortex-relay` yet because the runtime has no trusted clock-health input or dynamic pre-issue provider. A missing next snapshot is denied at construction in this slice. The 200 ms one-shot issuer request remains open for a later injected issuer adapter. This is deterministic Engine behavior coverage, not NFR-039 qualification or a claim that wall clock uncertainty is known in production.

Validation on the current worktree:

- `go test -race ./runtime/accounting`
- `go test ./...`
- `go vet ./...`
- `DOCKER_CONTEXT=orbstack make phase4-m2-check`

Tests cover pre-issue/lease validation, exact `T`, old/new selection, admission rejection during uncertainty, <=10-second straddle and recovery, hard cutoff, too-wide uncertainty, and unknown usage remaining in the original snapshot. Native/Docker process-topology testing for this injectable clock is not claimed because no runtime clock injection endpoint is configured.
