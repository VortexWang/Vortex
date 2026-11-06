---
status: complete
phase: 05-m3-agent-edge-hub-m3-agent-and-hub
source: [05-01-SUMMARY.md, 05-02-SUMMARY.md, 05-03-SUMMARY.md, 05-04-SUMMARY.md, 05-05-SUMMARY.md]
started: "2026-11-06T00:50:09.654515+00:00"
updated: 2026-11-06
renewed_source_sha: deb8b08e5cc8ee4e6f77bc700a4b5b36836c68c1
---

## Current Test

[testing complete]

## Historical tests — original acceptance

### 1. 实际节点重启与账本恢复边界
expected: 新 boot 不释放旧未知负债；实际 TCP 经新授权恢复；S/R 只转未签发 R，不延长旧授权。
result: pass
source: human
reported: "确认"
verification: agent/runtime_restart_test.go#TestRuntimeSIGKILLFreshBootReprepareAndRecoverDebt; agent/epoch_handoff_test.go#TestAuthenticatedHubEpochHandoffKeepsOldGrantAndBoundsSuccessor

### 2. 注册与文件升级边界
expected: 注册由本地生成私钥和 CSR，一次令牌重复兑换被拒绝；签名/摘要失败不切换二进制，健康失败恢复旧版本。当前文件安装器测试不等同已交付完整服务升级。
result: pass
reported: "符合预期"
source: human
reason: 05-01 and 05-05 legacy summaries lack structured coverage; do not infer human acceptance from prose.
verification: agent/registration_test.go; agent/identity/identity_test.go; agent/upgrade/installer_test.go

### 3. One-time token CSR enrollment issues a node SAN certificate and stores the private key with restrictive permissions.
expected: One-time token CSR enrollment issues a node SAN certificate and stores the private key with restrictive permissions.
result: pass
source: automated
coverage_id: 05-02-SUMMARY.md:D1
verification: agent/identity/identity_test.go#TestEnrollmentTokenCanBeRedeemedOnlyOnceConcurrently; agent/identity/identity_test.go#TestAttachKeyRejectsKeyThatDoesNotMatchCSR; agent/identity/identity_test.go#TestCredentialRotationValidatesBeforeOverlapAndExpiresOldCredential; agent/registration_test.go#TestRegisterGeneratesLocalKeyAndPersistsIssuedCredential

### 4. TLS 1.3 mutual-authenticated control ACKs are correlated and durably persisted.
expected: TLS 1.3 mutual-authenticated control ACKs are correlated and durably persisted.
result: pass
source: automated
coverage_id: 05-02-SUMMARY.md:D2
verification: agent/control_test.go#TestControlClientAppliesRuntimeConfigOverMutualTLSGRPC; agent/unknown_usage_test.go#TestUnknownLiabilitySurvivesBadAckLostAckAndRestart

### 5. Config reconciliation preserves activation-unknown durably; upgrade drain and rollback hooks are bounded and guarded.
expected: Config reconciliation preserves activation-unknown durably; upgrade drain and rollback hooks are bounded and guarded.
result: pass
source: automated
coverage_id: 05-02-SUMMARY.md:D3
verification: agent/reconcile_test.go#TestReconcilerPersistsActivationUnknownBeforeReturning; agent/store/store_test.go#TestControlSequenceAndStateSurviveReopen; agent/upgrade/upgrade_test.go#TestDrainReceivesBoundedContext; agent/upgrade/upgrade_test.go#TestIrreversibleMigrationBlocksRollback; agent/upgrade/upgrade_test.go#TestHealthFailureRestoresCompatibilitySnapshotBeforeRollback

### 6. Wave 2 check passes on native macOS arm64, Docker Linux arm64, and GitHub Actions Ubuntu amd64 at the same SHA.
expected: Wave 2 check passes on native macOS arm64, Docker Linux arm64, and GitHub Actions Ubuntu amd64 at the same SHA.
result: pass
source: automated
coverage_id: 05-02-SUMMARY.md:D4
verification: artifacts/phase5/wave2/native/report.json; artifacts/phase5/wave2/docker/report.json; artifacts/phase5/wave2/actions.json

### 7. Durable source-bound grants, leases and live-session renewal
expected: Durable source-bound grants, leases and live-session renewal
result: pass
source: automated
coverage_id: 05-03-SUMMARY.md:D1
verification: agent/runtime_authority_test.go#TestRuntimeAutomaticallyUsesDurableLeaseAuthorityBeforeTarget

### 8. Typed unknown liabilities survive lost ACK and restart without release
expected: Typed unknown liabilities survive lost ACK and restart without release
result: pass
source: automated
coverage_id: 05-03-SUMMARY.md:D2
verification: agent/unknown_usage_test.go#TestUnknownLiabilitySurvivesBadAckLostAckAndRestart

### 9. Online backend-before-ACK and retained autonomous replay
expected: Online backend-before-ACK and retained autonomous replay
result: pass
source: automated
coverage_id: 05-03-SUMMARY.md:D3
verification: agent/usage_handoff_test.go#TestUsageOnlineBackendAckAndAutonomousRetention; agent/usage_handoff_test.go#TestControlReportsNewUsageWithoutReconnecting

### 10. Restricted autonomy, fixed deadlines and signer/path separation
expected: Restricted autonomy, fixed deadlines and signer/path separation
result: pass
source: automated
coverage_id: 05-03-SUMMARY.md:D4
verification: agent/store/issuer_test.go#TestIssuerConcurrentAutonomyConservesSource; agent/store/autonomy_test.go#TestAutonomyUsesFixedHeartbeatDeadlineAcrossRestart; agent/authorization_test.go#TestSignerEpochCannotTransferSourceWithoutHandoffEvidence

### 11. Same-SHA full-disk and raw-ledger evidence
expected: Same-SHA full-disk and raw-ledger evidence
result: pass
source: automated
coverage_id: 05-03-SUMMARY.md:D5
verification: artifacts/phase5/authorization/ebff5b0/ledger-audit.json; artifacts/phase5/authorization/ebff5b0/unknown-audit.json; artifacts/phase5/authorization/ebff5b0/handoff-audit.json

### 12. Durable activation and proof before resource release
expected: Durable activation and proof before resource release
result: pass
source: automated
coverage_id: 05-04-SUMMARY.md:D1
verification: agent/recovery_test.go#TestRecoveryQueriesExecutionBeforeReleasingResources; agent/recovery_test.go#TestNewExecutorCannotForgeOldAbortProof

### 13. Fresh boot resumes a real configured Engine while retaining old debt
expected: Fresh boot resumes a real configured Engine while retaining old debt
result: pass
source: automated
coverage_id: 05-04-SUMMARY.md:D2
verification: agent/runtime_restart_test.go#TestRuntimeSIGKILLFreshBootReprepareAndRecoverDebt

### 14. Unique S/R transfer and continued durable tail replay
expected: Unique S/R transfer and continued durable tail replay
result: pass
source: automated
coverage_id: 05-04-SUMMARY.md:D3
verification: agent/store/handoff_test.go#TestHandoffPreservesIssuedRightsAndTransfersOnlyR; hub/recovery_test.go#TestRecoveryReplaysDurablePrefixAndTailAfterLostACK

### 15. Authenticated successor epoch and revocations retain original authority bounds
expected: Authenticated successor epoch and revocations retain original authority bounds
result: pass
source: automated
coverage_id: 05-04-SUMMARY.md:D4
verification: agent/epoch_handoff_test.go#TestAuthenticatedHubEpochHandoffKeepsOldGrantAndBoundsSuccessor

### 16. Stale config admission and competing Commit/Abort cannot gain new rights
expected: Stale config admission and competing Commit/Abort cannot gain new rights
result: pass
source: automated
coverage_id: 05-04-SUMMARY.md:D5
verification: agent/store/runtime_authority_test.go#TestRuntimeFencingAdvanceBlocksNewAdmissionButRetainsSession; engine/apply/apply_test.go#TestConcurrentCommitAbortHasOneOutcome

### 17. Same-SHA dual-host recovery and missing-evidence rejection
expected: Same-SHA dual-host recovery and missing-evidence rejection
result: pass
source: automated
coverage_id: 05-04-SUMMARY.md:D6
verification: artifacts/phase5/check/79a1c0b148f77e765190ee51075adf845e5ad115/pair.json

## Summary

total: 17
passed: 17
issues: 0
pending: 0
skipped: 0
blocked: 0

Automated classifications retain the source SUMMARY boundaries. Same-SHA 71502ce Docker/Actions pair and downloaded ledger audits pass. Human confirmation does not clear known implementation gaps or mark the whole phase complete.

## Gaps

No user-reported issues; both owner checkpoints confirmed. Source-audit gaps G05-01/02/03/05 are recorded separately in 05-REQUIREMENT-AUDIT.md and 05-VERIFICATION.md; they remain open even if both human checkpoints pass. G05-04 was separately fixed through GSD debug in f6a5891 and verified by same-SHA Docker/Actions 42140165239; it was a source-audit finding, not a user-reported UAT gap. User-reported gaps require executed closure plans.

## Workflow hooks

verify:pre api-coverage gate passed via `check api-coverage.verify-pre 05`: no external-API integration detected. The rendered hook uses an older query spelling; the current installed gate module documents the check command. No gate was skipped. Phase has no UI deliverable; frontend browser UAT does not apply. Work performed inline at owner request.

verify:post closure: Nyquist coverage is complete for the bounded runtime/protocol-adapter scope (`05-VALIDATION.md`), security register verified with zero open threats (`05-SECURITY.md`), no UI deliverable in this phase. Full-phase verification is `passed_with_scope`; Phase 6 production authority and Phase 4 qualification remain separate.

## Renewed runtime UAT — 2026-11-06

### 18. 新增实际运行时流程与边界
expected: 实际进程支持区域 Hub 故障切换、控制/VLink 凭据轮换与吊销、节点排空及签名升级失败回退；恢复追账、share 校验和新旧计费快照切换保持原未知负债。当前验收限于 Agent/Engine 与协议后端夹具，生产后端属于 Phase 6。
result: pass
source: human
reported: "符合预期，通过"
verification: test/phase5/{regional_process_test.go,scheduler_process_test.go,identity_process_test.go,node_commands_test.go,upgrade_process_test.go,recovering_process_test.go,share_policy_process_test.go}; 05-14-EVIDENCE.md
source_sha: deb8b08e5cc8ee4e6f77bc700a4b5b36836c68c1

All seven workflows are automatically covered by 31 mandatory named process cases; this is the single owner confirmation summary required for automatically covered deliverables. Clean Docker and Ubuntu Actions passed on the same `deb8b08` source SHA, and the independent manifest/raw snapshot audit passed on both hosts. Original 17 tests and their historical scope above remain unchanged. Total recorded UAT: 18 passed, zero issues/pending for the bounded Phase 5 runtime/protocol-adapter scope. Production backend authority and Phase 4 performance remain outside this UAT.
