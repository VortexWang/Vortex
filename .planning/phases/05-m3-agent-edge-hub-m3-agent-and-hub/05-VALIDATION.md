---
phase: "05"
slug: m3-agent-edge-hub-m3-agent-and-hub
status: validated
nyquist_compliant: false
wave_0_complete: true
reviewed_sha: deb8b08e5cc8ee4e6f77bc700a4b5b36836c68c1
created: "2026-11-06"
---

# Phase 5 validation audit

Inline re-audit covers all 14 plan groups and 34 IDs in 05-REQUIREMENT-AUDIT.md. Earlier missing runtime behaviors are implemented through 05-13; consolidated same-source Linux evidence and renewed owner UAT passed on deb8b08. Historical audit versions are retained in Git. No missing behavior is converted into a manual-only exception.

## Test infrastructure

Pinned Go 1.27.3/protoc 36.2, Go testing/race, Python evidence and contract tests, real node/Hub/backend-fixture subprocesses, separate SQLite FULL/WAL files and isolated 4 MiB tmpfs. Full entry: `make phase5-check`; clean local Linux entry: `sh tools/phase5/docker_check.sh`; Actions phase5.yml uses the same gate. Quick evidence negatives: `python3 -m unittest discover -s tests/phase5`. No watch modes. arm64 Docker execution and amd64 Actions execution are distinct native cells; neither is an arm64 Actions result.

## Verification map

| Plan group | Behavior / ID groups | Actual tests and mandatory evidence | Current status |
|---|---|---|---|
| 05-01/03 | WAL/FULL, grants/reservations, write failure, unknown debt; AGT-003/005/006/011, HUB-001..009 | agent/store and authority/usage tests; real disk-full, renewal and retention audit | Same-SHA Docker/Actions pair passed |
| 05-02/05 | enrollment, config, credentials; AGT-001/002/004/008/012 | registration, control/identity and reconciler tests; contract gate | Same-SHA Docker/Actions pair passed |
| 05-04 | activation, S/R and restart; AGT-009/010, HUB-010..018, SUP-B1/B3/B4 | recovery_check.sh: actual SIGKILL/full-disk, concurrent Commit/Abort and five SQLite snapshots | Same-SHA Docker/Actions pair passed |
| 05-06 | exact approved lifecycle/identity schema | tests/phase1/test_lifecycle_candidates.py; contracts/lifecycle_test.go; contracts_check.sh | Complete per 05-06-SUMMARY |
| 05-07 | bounded regional failover | TestRegionalProcessFailover: 5 real cases; 10x regional races | Complete per 05-07-EVIDENCE |
| 05-08 | renewal/probes/reports/retention | TestSchedulerProcessRenewal: 7 cases; repeated journal, safety, scheduler and retention tests; independent raw audits | Complete per 05-08-EVIDENCE |
| 05-09 | common control/VLink identity | TestIdentityProcessRotation; 10x identity races and Engine admission tests | Complete per 05-09-EVIDENCE |
| 05-10 | operational commands/drain | TestNodeCommandsDrain: 3 real cases; admin/lock races; container and unit verification | Complete per 05-10-EVIDENCE |
| 05-11 | service replacement/rollback | TestUpgradeProcessRollback: 2 Linux cases; journal crash/digest/full-disk faults | Complete per 05-11-EVIDENCE |
| 05-12 | recovering/caught-up and tail | TestRecoveringProcessCatchup: 5 real cases; >1000 record replay, migration and atomicity tests | Complete per 05-12-EVIDENCE |
| 05-13 | share/policy transitions | TestSharePolicyProcess: 8 TCP/UDP and hostile-chain cases; policy races and six raw snapshots | Complete per 05-13-EVIDENCE |
| 05-14 | fail-closed consolidated evidence | 31 mandatory process cases; eleven evidence negative/WAL tests; independent snapshot audits | Passed: Docker and Actions run 42224211933 on deb8b08 |

All 34 IDs have scoped test references. HUB-005/006 production transport/allocation remains explicitly Phase 6; this audit does not call those whole-product requirements complete. Exact command/test names are enforced in tools/phase5/check.sh and audit_gap_evidence.py rather than inferred from package success. Missing tests, SKIP, missing/changed evidence and invalid raw snapshots fail.

## Manual acceptance and limits

Renewed owner confirmation covers the newly delivered failover, live identity, service operation and recovery/share/policy behavior. Historical UAT is retained separately. Physical power loss, actual systemd daemon boot, arbitrary Agent source/relay modes, Phase 6 production authority and Phase 4 performance are not claimed by process/fixture coverage. No UI is delivered in this phase, so browser verification does not apply.

## Sign-off

- [x] All 14 task groups and 34 IDs mapped to existing tests.
- [x] Previously missing runtime scenarios implemented with real process assertions.
- [x] Negative evidence fixtures execute and reject absent/partial/skipped/changed data.
- [x] Final current-source Docker/Actions pair independently re-audited.
- [x] Renewed owner UAT recorded.
- [x] Nyquist final status updated after current consolidated results.
