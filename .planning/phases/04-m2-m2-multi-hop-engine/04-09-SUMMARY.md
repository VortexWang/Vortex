---
phase: 04-m2-m2-multi-hop-engine
plan: 09
status: complete
completed: 2026-11-05
source_sha: pending
---

# Plan 04-09 Summary — Share CAS and Lease Drain

Connected the existing process-local `share.Manager` and `ShareGate` to the
source runtime through an explicit `share` configuration block. Startup now
fences node/boot identity, prepares and commits one transaction with CAS
generation checking, and admits source flows only while the configured share
lease is valid. Session close preserves exactly-once release, while stale boot,
stale revision, capacity exhaustion and old-generation drain remain fail-closed.

Focused race tests and the full local Phase 4 Docker check passed. This closes
the runtime wiring slice only; durable Agent/Hub share authority, lost-receipt
reconciliation and formal NFR qualification remain open.
