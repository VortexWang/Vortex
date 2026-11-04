---
phase: 04-m2-m2-multi-hop-engine
plan: 08
status: complete
completed: 2026-11-05
source_sha: pending-code-commit
---

# Plan 04-08 Summary — Policy-Time and Snapshot Runtime Slice

Added strict opt-in `policy_window` configuration and wired the existing
process-local `PolicyBoundary` around `CreditBook` at source runtime startup.
The boundary validates pre-issue/lease coverage, clock uncertainty, hard
deadline and issuer timeout limits; sessions retain snapshot selection through
the injected Gate. Static runtime policy remains deny-by-default unless an
explicit credit book and policy window provide the local authorization.

Focused race tests, `go vet`, and the full local Phase 4 Docker/race regression
passed. This closes the runtime wiring slice only. NFR-039/NFR-044 remain
unqualified because the fixture does not yet run the policy-window workload
and no durable Agent/Hub authority exists.
