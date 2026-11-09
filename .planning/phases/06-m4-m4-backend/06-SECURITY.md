---
phase: "06"
slug: m4-m4-backend
status: verified
threats_open: 0
asvs_level: 1
register_authored_at_plan_time: true
review_source: 65a5335894732b7d632e870acc3d8283be224cd3
execution: inline
created: "2026-11-09"
---

# Phase 06 安全验证

按计划原有24项威胁核对L1缓解措施存在性；未扩展为渗透测试、独立安全审计或全形式证明。配置block_on=high；24项均有实现及对应自动化验证路径，未接受新的风险。最终同SHA运行结果由06-VERIFICATION及原始producer报告确认。

## Trust Boundaries

Bearer→当前PG身份→TenantScope；后端事务→PG账本/audit/task/Outbox；角色mTLS→Node/Hub独立FULL回执；Cloudflare secret→AEAD持久化/固定provider端点；clean源码→显式原始验收产物。

## Threat Register

| Threat ID | Category | Component | Severity | Disposition | Mitigation | Status |
| --- | --- | --- | --- | --- | --- | --- |
| T-06-01 | Tampering | tenant-scoped repository/policy | high | mitigate | `backend/internal/repository/tenant_scope.go` (NewTenantScope); TenantScope/Allows + SQL tenant predicates; TestTenantRepositoryScopeAndPlatformAudit | closed |
| T-06-02 | Tampering | task/outbox/control receipt | high | mitigate | `backend/internal/worker/outbox.go` (Commit); durable lease then transport; TestTracerRealProcesses/transaction rollback | closed |
| T-06-03 | Tampering | platform grant/lifecycle | high | mitigate | `backend/internal/service/node.go` (Grant); ownership/version/validity; TestNodeGrantValidityHistoryAndAudit | closed |
| T-06-04 | Elevation of Privilege | port release/serving | high | mitigate | `backend/internal/service/port.go` (Release); fixed members/proof/boot; TestReleaseOldBootAndDisconnectRetainOccupation | closed |
| T-06-05 | Tampering | compiler canonicalization/artifact | high | mitigate | `backend/internal/compiler/compiler.go` (sha256); canonical/digest/committed artifacts; TestCompileCanonicalDigestReplay | closed |
| T-06-06 | Elevation of Privilege | revision-scoped apply worker | high | mitigate | `backend/internal/worker/rule_worker.go` (ReceiptWithLease); revision/lease/unknown fence; TestApplySupersededLateObservationCannotQueueCommit | closed |
| T-06-07 | Tampering | package snapshots/subscriptions | high | mitigate | `backend/internal/service/package.go` (Snapshot); immutable snapshots; TestPackageCatalogSnapshotAndSubscriptionImmutable | closed |
| T-06-08 | Replay | entitlement redemption | high | mitigate | `backend/internal/service/entitlement.go` (LockEntitlementCode); locked redemption and idempotency; TestEntitlementAtomicRedemptionContract | closed |
| T-06-09 | Tampering | usage ingestion | high | mitigate | `backend/internal/service/metering.go` (Digest); tuple/digest/final separation; actual Agent/Hub lost-ACK replay test | closed |
| T-06-10 | Repudiation | ledger/source projection | high | mitigate | `backend/internal/service/ledger.go` (Rebuild); source-local ledger and rebuild; TestLedgerConservationRebuildSourceIsolationAndAuditRollback | closed |
| T-06-11 | Elevation of Privilege | share CAS/generation | high | mitigate | `backend/internal/service/share.go` (Generation); per-kind chain/CAS; TestShareConcurrentLinearChainCapacityKindsAndAuditRollback | closed |
| T-06-12 | Tampering | activation unknown/handoff | high | mitigate | `backend/internal/service/share_handoff.go` (Unknown); closure/FULL receipt before release; TestShareHandoffLostReceiptRestartRetainsAndConfirmsExactDifference | closed |
| T-06-13 | Spoofing | Bearer/refresh authentication | high | mitigate | `backend/internal/repository/token_repository.go` (LockRefreshFamily); current PG principal, family rotation/reuse; token integration tests | closed |
| T-06-14 | Information Disclosure | REST cursor/SSE/error/evidence | high | mitigate | `backend/internal/httpapi/cursor.go` (hmac.Equal); signed scoped cursor + SSE safe projections; cursor/SSE/schema integration tests | closed |
| T-06-15 | Tampering | dependency/toolchain installs | high | mitigate | `tools/phase6/dependency_lock_check.sh` (two clean generations); exact module/license/checksum and sqlc reproducibility | closed |
| T-06-16 | Tampering | dependency/toolchain installs | high | mitigate | `tools/phase6/dependency_lock_check.py` (license); approved dependency closure; no added compiler module | closed |
| T-06-17 | Tampering | dependency/toolchain installs | high | mitigate | `tools/phase6/dependency_lock_check.py` (license); approved dependency closure; no added package module | closed |
| T-06-18 | Tampering | dependency/toolchain installs | high | mitigate | `tools/phase6/dependency_lock_check.py` (license); approved dependency closure; no added metering module | closed |
| T-06-19 | Tampering | dependency/toolchain installs | high | mitigate | `tools/phase6/dependency_lock_check.py` (license); approved dependency closure; existing lease modules only | closed |
| T-06-20 | Tampering | dependency/toolchain installs | high | mitigate | `tools/phase6/rest_contract_check.py` (CR-009); exact owner proposal/freeze; no OpenAPI code generator | closed |
| T-06-21 | Information Disclosure | DNS credential store | high | mitigate | `backend/internal/control/credential_crypto.go` (a.Seal); AES-GCM identity AAD/key version; actual Cloudflare controlled CRUD + encrypted-store tests | closed |
| T-06-22 | Tampering | qualification evidence | medium | mitigate | `tools/phase6/final_evidence_check.py` (60000000000); exact1000/2000+nanoseconds/SHA/raw checksums; final gate negative tests | closed |
| T-06-23 | Elevation of Privilege | tenant policy and cache rebuild | high | mitigate | `backend/internal/repository/tenant_scope.go` (InsertAudit); explicit scope + transactional audit; nonempty Valkey-loss rebuild tests | closed |
| T-06-24 | Tampering | API ownership and evidence gate | high | mitigate | `tools/phase6/final_check.sh` (PHASE6_EXPECTED_SHA); single API source and clean exact-SHA producer; deterministic copy/lint | closed |

## Summary Threat Flags

control-extension：CR-005/007/008精确批准与独立capability/identity/boot/revision检查；tooling-trust：临时container只信任精确source mount，clean SHA与产物门禁复核；persistent_domain_ownership：migration4的owner/zone suffix/唯一性/FK与同事务审计。

## Accepted Risks Log

无新增接受风险。历史Phase3 VLink review-hash差异被单独记录且未作为全局冻结通过；物理断电、专用性能/24h、完整模型、备份/WAL restore、UI属于原阶段范围。Cloudflare policy证据为经审查的attestation，不宣称独立在线读取provider权限。

## Security Audit Trail

| Audit Date | Threats Total | Closed | Open | Run By |
| --- | --- | --- | --- | --- |
| 2026-11-09 | 24 | 24 | 0 | Codex inline L1, source65a53358 |

## Sign-Off

- [x] 原计划24个威胁均有disposition和实现证据。
- [x] 无新增风险豁免。
- [x] threats_open: 0。
- [x] status: verified（L1实现缓解验证；运行验收另行门控）。

## Security Audit 2026-11-09

| Metric | Count |
|---|---|
| Threats found | 24 |
| Closed | 24 |
| Open | 0 |
