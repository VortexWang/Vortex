---
phase: 05
plan: '12'
status: verified
plan_complete: true
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

## Final paired verdict

Source `1c06bc91ad9e8cb72493b875684d8740c89c0ee9` passed full clean pinned Go 1.27.3 Linux arm64 Docker and Ubuntu 24.04 amd64 Actions [42198081076](https://github.com/VortexWang/Vortex/actions/runs/42198081076). Actions also passed deployment lifecycle. Raw results are retained under `artifacts/phase5/check/1c06bc91ad9e8cb72493b875684d8740c89c0ee9/` with pair.json and files.sha256. The historical pending statements above are superseded by this final result.

Both hosts explicitly passed all five named real-process recovery scenarios, recovery race tests, atomic failure and concurrent completion, historical migration/reopen, >1000-record bounded replay and lost post-barrier tail receipt. Existing handoff revocation/concurrency, Engine, identity, signed upgrade, renewal, retention and disk/crash regressions passed. The standalone Linux audit reran against retained Actions recovery snapshots: 5 snapshots/10 fault cases passed; renewal (12 snapshots) and settled retention/source conservation audits also passed on each host.

Actions artifact13493684048 retains115 current files;2822 historical or binary members omitted with archive checksum and member list recorded. Product scope remains finite protocol-backend integration, not Phase6 production backend authority or Phase4 performance qualification. SQL trigger failures model transactional write failure, not physical power loss at every instruction. G05-05 recovery-barrier portion is covered; share/policy integration and phase consolidation remain05-13/05-14. Requirements remain sibling-blocked rather than globally completed.
