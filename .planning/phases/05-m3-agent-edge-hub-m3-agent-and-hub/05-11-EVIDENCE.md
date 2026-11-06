---
phase: 05
plan: '11'
status: verified
plan_complete: true
updated: 2026-11-06
---

# 05-11 incremental service integration

05-10 prerequisite SUMMARY is complete on source061ffb5, paired Docker/Actions42186204143. Final product source `5ca72e06898f20d105c099e60f83defa1ebf5c5f` passed clean Docker arm64 and Ubuntu amd64 Actions42193600373. This plan is complete within the actual OS-process service controller/protocol-backend scope. Earlier incremental notes below are historical, superseded by the final verdict.

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


## Final verdict — source5ca72e0

Evidence root `artifacts/phase5/check/5ca72e06898f20d105c099e60f83defa1ebf5c5f/`, bound by pair.json and files.sha256. Complete `make phase5-check` passed on clean pinned Go1.27.3 Linux arm64 Docker and Ubuntu24.04 amd64 Actions42193600373. Both deployment scripts also passed actual non-root container signal/restart, dual-architecture builds and retained hashes. Ubuntu systemd-analyze verified both node/upgrade units in an isolated root; no actual systemd daemon boot/upgrade is claimed.

Actions https://github.com/VortexWang/Vortex/actions/runs/42193600373 . Artifact13491488094 selection retains the current gate/deployment outputs and omits2458 historical members; archive hash/member list are recorded. Separate Wave1 artifact13491491096 now preserves actual diagnostics. Previous source8566b1e local gate passed but Actions42192956408 failed Wave1; it remains an unsuccessful pair, never reused as final evidence.

Required explicit PASS on both hosts: `TestUpgradeProcessRollback/upgrade-healthy`, `/upgrade-rollback`; `TestServiceFullDiskCannotStopOrSelectCandidate`; nine `TestServiceCrashBoundariesRetainOneTransaction` stages; fresh-witness same-transaction reconciliation and changed recovered-digest refusal. Required scenarios are not skipped on Linux. Native Darwin process termination witness remains unavailable; that optional run is explicitly skipped and never contributes qualification.

Tracer uses two distinct actual node executables, real Agent/Engine private drain, OS wait/reap of the old/candidate process, SQLite FULL/WAL/current build and new boot observation, authenticated protocol-test Hub and genuine TLS/HELLO/OPEN/TCP response. An unhealthy actual candidate fails runtime startup; old binary is restarted only after candidate termination and must pass the same current authority/traffic check. Snapshot is a strict no-settings-changed marker, never any DB/WAL/credentials/grant/authority backup. Existing consumption payload hashes remain unchanged, authorization fencing floor never falls below its original9, original boot has no active grants, unknown512 remains unresolved with original boot. The test backend provides explicitly separate fresh funded sources/root anchors and fresh fenced config, preserving old occupied roots rather than refunding them.

Legacy Installer signature/digest, disk reserve, incompatible schema, irreversible rollback, bounded drain, all stage and rollback crash tests remain in the upgrade race gate. Operational signed candidate and old binary both inspect schema16 read-only; missing/incompatible schema cannot reach drain. Real small tmpfs exhaustion blocks staging without stopping/selecting a candidate. Lost stop reply and failed candidate stop keep durable unknown/rollback intent; no elapsed time becomes stop proof. Interrupted selected candidate recovery verifies recorded digest and uses the same transaction, a fresh service stop witness, restart and traffic health. New journal OS lock blocks independent instances; release paths reject nested symlink escapes and stale active selection is not overwritten.

Independent pinned-Linux raw ledger audits passed on both retained result sets:5 recovery snapshots/10 faults,12 renewal-safety snapshots and retention/source conservation. Prior full-disk, identity/renewal/regional process and Engine race suites remain passed. D-03/04/05/06/07/15/17/18 and T-05-19/20 are covered within this finite scope. No normative contract/dependency change, Engine SQL or backend business traffic introduced.

05-11 completes G05-02 operational commands/drain/signed replacement within this stage. Production backend authority remains Phase6; actual systemd daemon acceptance is separate from the verified unit and real OS-process tracer. Phase5 remains executing:05-12 recovering barrier,05-13 policy switch and05-14 consolidated verification/UAT remain outstanding.
