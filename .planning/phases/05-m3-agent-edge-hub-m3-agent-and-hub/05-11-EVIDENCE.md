---
phase: 05
plan: '11'
status: incremental
plan_complete: false
updated: 2026-11-06
---

# 05-11 incremental service integration

05-10 prerequisite SUMMARY is complete on source061ffb5, paired Docker/Actions42186204143. This plan is executing; no SUMMARY/completion verdict yet.

## Implemented first increment

- Installer OS flock protects the durable journal across processes/instances, with non-symlink owned0600 regular single-link lock checks.
- Optional formal external ServiceController is additive to the legacy file-only Installer fixture. New operational coordinator must require it; it is not yet wired to CLI.
- Before active-link mutation, service stop intent is durable and StopAndWait must prove namespace/process termination. Lost stop reply becomes process-stop-unknown and returns ErrActivationUnknown; recovery must requery/prove stop with the same transaction.
- Candidate starts before mandatory health verification. Rollback persists intent, proves candidate stopped before compatible-state restore/old-link activation, starts old service and requires health before declaring rolled-back.
- Repeated healthy service transaction rechecks health. Changed active selection before activation is detected and retained as unknown, never overwritten by stale installer.

Native `go test -count=1 -race ./agent/upgrade` and `go vet ./agent/upgrade` passed. Named tests: TestServiceStopUnknownCannotActivateAndRecoveryUsesSameTransaction, TestServiceRollbackStopsCandidateBeforeRestoringBinary, TestServiceRollbackStopFailureRetainsDurableIntent, TestServiceInstallerJournalLockAcrossInstances, TestServiceStaleActiveLinkCannotBeOverwritten. They are finite controller-boundary tests, not actual node service upgrade evidence.

## Open

Production external systemd controller, exclusive operational upgrade CLI, actual built-node healthy candidate/rollback tracer, current-authority and TCP health checks, allowlisted nonfinancial compatibility/schema preflight, financial monotonicity and crash/full-disk audits, deployment helper and same-source Docker/Actions remain unverified. Legacy Installer's generic callback interfaces remain library fixtures, not production upgrade acceptance. No new dependencies or normative contract changes.
