# 06-03 rule authority acceptance

Source: `fc63d57406f9f0e3eebe99ad276f95b004add710`. The exact owner-approved
CR-005 proposal is `b02a73a9d3d4d8506cfd79904efa6a5ebc430cd4b06d729a37b518e19d582bb4`.
Evidence: `artifacts/phase6/06-03/fc63d57406f9f0e3eebe99ad276f95b004add710/reverification/docker/`.
All referenced tests passed normal and race on native Linux arm64 Docker, with
real PostgreSQL 18.6 and Valkey 9.1.3. This is backend-plan acceptance; final
same-SHA Actions pairing, HTTP adapters, frontend presentation, funded snapshot
producers and complete milestone acceptance remain their later plans.

| Requirement | Implemented boundary and named verification |
|---|---|
| RUL-001 | Normalize typed protocol, target/weight/priority, proxy, idle/limit and enable fields; durable fixed/auto port binding. `TestRuleValidationPathsTargetsAndDirections`, `TestCompileAutoPortBindingCanonicalReplay`, `TestRulePortActualAgentSocketsUnknownAndQuery`; runtime destination policy rechecks resolved admission addresses. |
| RUL-002 | Business input references ordered groups; compiler expands immutable member snapshots into per-node roles. `TestCompileCanonicalDigestReplay`, `TestRuleImmutableRevisionConflictAndTenantScope`. UI terminology remains Phase 7. |
| RUL-003 | Reject duplicate groups and repeated actual nodes. `TestRuleValidationPathsTargetsAndDirections`, `TestCompileInvalidAuthorityNoArtifacts`. |
| RUL-004 | Auto/forward/reverse follows complete group reachability; denied paid concealment remains fail closed until entitlement producer exists. `TestRuleValidationPathsTargetsAndDirections`, `TestCompileGrantPermitAndDirection`, `TestApplyMultihopForwardAndReverseRealTLS`. |
| RUL-005 | Grants and independently versioned segment permits are pinned and validated; implicit own-group allowance follows current node-domain policy. `TestCompileGrantPermitAndDirection`, `TestNodeSegmentPermitIndependentOfGrants`. |
| RUL-006 | Empty/offline members cannot compile or bypass a hop; executable artifacts fix the complete path. `TestCompileInvalidAuthorityNoArtifacts`, `TestApplyMultihopForwardAndReverseRealTLS`. |
| RUL-007 | Separate group-member scheduling from target weighted/primary-backup selection. `TestCompileCanonicalDigestReplay`, `TestApplyMultihopForwardAndReverseRealTLS`. |
| RUL-008 | Per-TCP connection/per-UDP session runtime selection; idle default 60 seconds. `TestPrepareCommitHotSessionAndAbortRealSockets`, `TestApplyMultihopForwardAndReverseRealTLS`. |
| RUL-009 | Existing sessions retain selected path, destination and snapshot through hot changes. `TestPrepareCommitHotSessionAndAbortRealSockets`, `TestApplyMultihopForwardAndReverseRealTLS`. |
| RUL-010 | Durable fixed-member Prepare holds sockets without forwarding, all-member prepare precedes Commit, matching activation receipts precede running. `TestApplyPrepareCommitUnknownAndLateReceipt`, `TestRulePortActualAgentSocketsUnknownAndQuery`, `TestRuleCommandPrepareCommitQueryRestartRealRuntime`. Funded billing inputs must come from 06-04/05; none are synthesized. |
| RUL-011 | Abort prepared generation preserves existing active sessions. `TestPrepareCommitHotSessionAndAbortRealSockets`, `TestCompileWorkerMissingSnapshotPreservesPriorAuthority`. |
| RUL-012 | Partial/unknown reconciliation, 120-second deadline and monotonic rollback revision; failed rollback does not recurse. `TestRollbackAuditAtomicSupersedeAndWorkerCrashRecovery`. |
| RUL-013 | Transport loss/restart produces unknown, keeps occupation and queries the same artifact/current incarnation. `TestRulePortActualAgentSocketsUnknownAndQuery`, `TestRuleCommandPrepareCommitQueryRestartRealRuntime`. |
| RUL-014 | Persist hot/rebuild/stop class and immutable revision; real hot/rebuild boundaries are tested. `TestRuleImmutableRevisionConflictAndTenantScope`, `TestPrepareCommitHotSessionAndAbortRealSockets`. Impact display remains public adapter/frontend work. |
| RUL-015 | Rollback only appends prior forwarding content; never rewinds grants, restrictions, billing or consumption. `TestRollbackAuditAtomicSupersedeAndWorkerCrashRecovery`. |
| RUL-016 | Rollback reuses current rights/resource validation and cannot revive obsolete work. `TestRollbackAuditAtomicSupersedeAndWorkerCrashRecovery`, `TestApplySupersededLateObservationCannotQueueCommit` (prepare/query-prepared/partial-commit). |
| BE-002 | Pure deterministic canonical compiler emits role/hop/upstream/next-group/direction and pinned snapshot/input identity; only committed artifacts dispatch. `TestCompileCanonicalDigestReplay`, `TestCompileRuntimeSchemaCompatibility`, `TestRuleCompileCommittedArtifactAndAuditRollback`. |
| BE-003 | Mutation, audit, operation, task and Outbox share PostgreSQL transactions; lease/running commit before transport, crash recovery retains uncertainty. `TestRuleAuditRollback`, `TestCompileWorkerAuditRollback`, `TestRollbackAuditAtomicSupersedeAndWorkerCrashRecovery`. DNS evidence is 06-10; notification delivery remains later scope. |

The three plan must-haves are covered by immutable revisions/CAS, canonical
committed input/artifact replay and fixed-member revision-scoped orchestration.
T-06-05 is checked by canonical/digest/schema/committed-only tests; T-06-06 by
revision/lease/receipt/obsolete-task fences and conservative unknown/release
tests; T-06-16 by exact toolchain and dependency/license closure checks.

The executable compiler is bounded to 16 groups and 256 members/group; desired
inputs beyond runtime bounds emit diagnostics and no artifact. The immutable
snapshot binder is a production seam used by the compile worker; missing real
billing inputs stay pending and preserve prior authority. This is an intentional
06-04/05 dependency, not mock-funded production execution.

当前最终验收：实现提交`65a5335894732b7d632e870acc3d8283be224cd3`，Docker及Actions42572674719原生amd64/arm64完整通过（含真实Cloudflare）。原始证据`artifacts/phase6/06-11/final-65a53358`；阶段完整结论与保留范围见06-VERIFICATION.md。以上较早证据仍按当时范围解释。
