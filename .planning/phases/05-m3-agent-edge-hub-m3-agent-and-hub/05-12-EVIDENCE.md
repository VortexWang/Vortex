---
phase: 05
plan: '12'
status: incremental
plan_complete: false
updated: 2026-11-06
---

# 05-12 durable recovering barrier

Implemented within the approved existing contract and dependency baseline:

- The FULL S/R transaction closes the prior signer, applies revocations, transfers only existing R, writes its handoff ledger record and captures a finite target sequence, then publishes recovering. Repeating the same transaction does not recapture the target or refill R.
- Additive `recovery_mode_version=1` migration retains authoritative rows. Historical handoffs without a completion witness conservatively capture retained ledger and become recovering rather than being inferred online. Reopen preserves the original target and completed state.
- Recovering denies new delegation installation, preserves original scope/deadlines and allows only existing node-bound transferred balances. Heartbeat refresh cannot promote online or extend the frozen deadline.
- `CompleteRecovery` transaction requires the current open successor signer, trusted conservative clock, current heartbeat, original hard boundary, a confirmed contiguous captured prefix and no holes. Post-barrier tail remains pending independently; a failed later receipt cannot starve a confirmed finite barrier.
- The existing approved HubStatus already reads recovering from the durable Store; no additional wire field or schema version change is introduced.
- Real process tracer runs two Hub processes, a Node/Engine process and a mutual-TLS protocol backend fixture with its own FULL SQLite receipt ledger. Lost ACK, forged hash, SIGKILL before/after handoff and after catch-up retain live TCP, unchanged node boot/path generation, exactly one R transfer and original unresolved 512-byte debt. The backend fixture is explicitly not production backend authority.

Native named process scenarios and `go test -count=1 -race ./agent/store/... ./hub/...` passed during implementation. Named tests include TestRecoveryBarrierSurvivesRestartAndTail, TestRecoveryMigrationConservativelyCapturesHistoricalHandoff, TestRecoveryAtomicFailureAndConcurrentCompletion and TestRecoveryBoundedReplayReachesCapturedBarrierWithLiveTail. SQL abort injection verifies atomic failure boundaries, not physical power loss. Existing handoff concurrency/revocation, retention/dispute/unknown and crash suites remain required regressions.

Same committed source Docker/Actions, independent raw audits and final plan review are pending. No SUMMARY or plan completion is asserted by this incremental record. Phase 4/user dirty changes remain outside this commit.
