# Phase 5 Research: M3 Agent and Edge Hub

**Date:** 2026-11-05
**Scope:** AGT-001..012, HUB-001..018 and SUP-AGENT-001/SUP-B1-001/SUP-B3-001/SUP-B4-001.

## Findings

1. The repository has no Agent or Edge Hub runtime yet. The first implementation must be a startable `vortex-node` plus a test Hub rather than a library-only state model.
2. `engine/apply` already provides a useful in-process Prepare/Commit/Abort/Release shape and `ActivationUnknown`; it must be wrapped by durable Agent state, fencing and idempotency rather than treated as authority.
3. `engine/runtime/link` and `engine/runtime/accounting` define the Engine integration boundary. Agent owns policy, persistence and control-plane decisions; Engine must remain database-free and receive explicit runtime inputs.
4. `contracts/protobuf/v1/vortex.proto` is draft and explicitly leaves payload encoding, timestamps, signatures, retry/idempotency, expiry and lifecycle messages open. Phase 5 must version or extend the control contract before implementation and run the existing requirements-map CI checks.
5. Existing database SQL is PostgreSQL-oriented and incomplete for Agent/Hub local state. SQLite schema must be separate, WAL-enabled, with `synchronous=FULL` for ledger/authorization/receipt transactions, bounded readers/writers, busy timeout, and reported `sqlite_version()`/compile options.
6. The project baseline already selects `modernc.org/sqlite` for Linux amd64/arm64 single binaries, but its dependency and actual SQLite version must be verified when the module is introduced. No new dependency is locked by this research.
7. Recovery must distinguish `signer_epoch` (Hub authority handoff) from Engine path `generation`; old authorization remains valid only within its original boundary. Unknown activation, unknown usage, and unproven transfer remain occupied/frozen.
8. Docker and GitHub Actions must run the same test entry on the same SHA. Native performance qualification remains a separate Phase 4 branch and is not a Phase 5 gate.

## Recommended wave structure

- Wave 1: frozen control/runtime contracts, local SQLite store, boot identity and a startable Agent/Hub tracer.
- Wave 2: registration, mTLS control stream, credential renewal/revocation, Engine process-internal supervision and durable reconcile.
- Wave 3: grant/lease renewal, usage batching and ACK ordering, signer_epoch autonomy, hard deadlines and fail-closed disk/time behavior.
- Wave 4: atomic ledger handoff, tail replay, activation-unknown restart recovery, bounded upgrade rollback, Docker/Actions fault evidence.

## Risks to carry into plans

- Do not use the draft Protobuf payload bytes as an implicit authority; define canonical fields, IDs, TTLs and errors before wiring.
- Do not ACK Engine usage before the local FULL transaction commits.
- Do not release an old lease or unknown balance from timeout, connection switch, or a new boot_id alone.
- Do not make the test Hub look like the Phase 6 PostgreSQL backend; label it as a protocol test service.

## Evidence strategy

Each wave has deterministic package tests plus a startable integration fixture. The final plan runs crash, duplicate, lost-receipt, full-disk and clock-boundary scenarios in Docker and Actions on one commit, preserving raw logs and JSON evidence. Unrun native or production-backend checks remain unverified.
