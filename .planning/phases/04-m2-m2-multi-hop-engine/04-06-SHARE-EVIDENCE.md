# Phase 4 Plan 04-06: Share CAS and Drain Slice

Date: 2026-11-05

`share.Manager` now retains prepared, active, and draining revisions by node/boot/generation. Prepare no longer replaces active state. `PrepareWithID` replays the original prepared result for the same transaction, Commit is fenced/CAS and idempotent, an unsuccessful overlap transition leaves active state unchanged, and a node boot change rejects commands/admissions carrying the old boot ID. Existing connection leases keep the old generation until close; new opens use the active generation and are limited by both its own connection cap and the overlap occupancy `max(old,new)`. A third overlapping generation is rejected while an older generation still drains. Revision history is bounded to 64 entries.

`runtime/accounting.ShareGate` acquires a connection lease before calling the wrapped Engine Gate, rolls it back when inner Open fails, and releases it exactly once on close. The wrapper is an injectable process-local adapter; `vortex-relay` configuration does not yet construct it, and share rate enforcement remains outside this slice.

Validation:

- `go test -race ./share ./runtime/accounting`
- full Engine `go test ./...` and `go vet ./...`
- upgrade/downgrade, stale generation, idempotent Prepare/Commit, cross-boot, drain, capacity, history bound, concurrent acquire/release and Gate rollback tests
- overlap transition cases repeated 100 times
- `DOCKER_CONTEXT=orbstack make phase4-m2-check` passed on clean `d7ca856`
- Actions run 41875373595 failed on `3f7c90f` because a rejected Commit could partially clear active state depending on Go map iteration order; fixed in `d7ca856`
- Actions run 41876404597 is on clean `d7ca856`; all job steps report success, but the GitHub run aggregate remains `in_progress` with no conclusion after 17 minutes. Do not mark Actions passed until the API reports a terminal conclusion.

This slice does not establish durable Agent/Hub CAS, lost network receipt reconciliation, production relay wiring, or complete NFR-040 qualification.
