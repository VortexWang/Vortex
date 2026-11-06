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

## Operational wiring increment (not final verification)

Added fixed-argv SystemdService with selected/running executable digest check and inactive/zero PID/unpopulated cgroup stop witness. Added external upgrade and read-only schema16 preflight commands, private-root checks, pinned signature/digest validation, mandatory current boot/build/FULL-WAL/control/config observation and authenticated existing-rule TCP health probe. No financial snapshots: explicit no-settings-changed marker only. Root helper can access private node socket only with matching directory/socket owner. New status observations are local IPC, not normative control contracts.

The actual Linux node tracer now launches distinct built candidate bytes through the Installer/OS-process service controller and validates healthy replacement and failed candidate rollback using the same database. Protocol-test backend explicitly supplies fresh fenced transactions and separately funded source anchors for replacement boots; old occupied roots are never reused/refunded. Initial tracer failures (short RPC cleanup allowance, stale closed configuration, reuse of occupied share roots) were retained in development logs and not counted as passed. Darwin cannot prove the Linux executor witness and its upgrade process scenario is explicitly skipped; required Linux gates reject skipped scenarios.

The tracer passed healthy/rollback/current TCP and unknown512 retention on Linux before the additional confirmed-consumption/fence comparison was added. The stronger assertion now also passed: original usage payload digests unchanged, fencing floor >=9, prior boot has no active authorization, unresolved512 retains original boot. Native Agent/CLI/upgrade race and vet passed. Actual systemd boot/upgrade, all-stage crash/full-disk recovery and same-source Docker/Actions acceptance still pending. Plan remains incomplete.


### Same-source gate attempt8566b1e

Actions42192956408 failed at Wave1; full recovery/deployment steps did not run.
Legacy redirected Wave1 diagnostics were not retained. Failed-step log/report are
retained under that source's actions directory; new source prints/archives Wave1
failure diagnostics. Local Docker for8566b1e remains a distinct result, never a
paired pass. Subsequent source adds same-transaction restart reconciliation,
recovered digest enforcement, service crash-boundary tests and actual isolated
full-disk pre-stop rejection; native race and pinned Linux full-disk tests passed.
No05-11 completion claim until the latest committed source passes both gates.
