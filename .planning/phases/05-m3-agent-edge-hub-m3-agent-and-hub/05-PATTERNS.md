# Phase 5 gap patterns — source snapshot 5adbde9

Generated inline at owner request. Existing 05-RESEARCH.md is historical pre-implementation context; its “no runtime yet / draft contracts” statements are superseded by current source and this gap-mode scan. --gaps reuses audited evidence and skips a fresh research workflow; no library/version selection is being refreshed.

| New/changed responsibility | Closest current analog / exact signature | Integration consequence |
|---|---|---|
| Region loop and serialized health/renewal | agent/control.go `Run`, `Connect`, `ConnectRegion`; receive loop already shares one sender | Bound readiness/watchdog separately from long-lived stream, keep monotonic sequences and current-session incarnation. |
| Durable renewal/report cache | agent/store/issuer.go `IssueHubAuthorization`; agent/store/store.go FULL transaction/outbox | Persist correlated ID/hash before reply; telemetry cannot create authority or trust time. |
| Identity publication | agent/credentials.go `CredentialManager`; engine/runtime/link/supervisor.go `hello` | Supervisor currently reads Signer/TLS fields separately. Capture one immutable identity for handshake and HELLO. |
| Certificate format seam | agent registration DNS node identity vs engine/vlink/hello.go `NodeIDFromCertificate` requiring urn:vortex:node:UUID | Exact reviewed profile must satisfy both; no DNS-only cast or shared qualification hostname bypass. |
| Drain | engine/runtime/relay/bridge.go Service `closing`, mutex and workers; runtime Stop | New OPEN on established links needs explicit admission barrier; listener close alone is insufficient. |
| Service upgrade | agent/upgrade/installer.go `Installer.Install/Recover`; transaction journal | Add actual process controller; preserve authoritative database through rollback, never restore old financial snapshot. |
| Catch-up state | agent/store/handoff.go `HandoffHub`, `ConfirmLedger`; hub/recovery.go `ReplayLedger` limit 1000 | Capture a finite catch-up barrier and persist recovering mode; live tail and contiguous ACK have distinct status. |
| Share/policy | engine/share; agent/store/authorization.go; agent/runtime_activation.go `RuntimeApplier` | Current adapter supports static destination only; new session snapshot selection and full share chain must reach real Gate/Commit. |
| Evidence gate | tools/phase5/check.sh, audit_recovery.py; tests/phase1/test_contracts.py | Keep raw snapshots and fail on missing/skipped tests; package success alone is inadequate. |

No new dependency or external API is needed. New proposed symbols are listed in each PLAN's “Artifacts this phase produces” section. Existing exact signatures above were read from source, not inferred from old research.
