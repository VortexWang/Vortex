# 租户归属与缓存丢失的跨域验证

TEN-002 的归属不是仅由 HTTP 路由中的 tenant_id 决定。私有实体有持久归属，repository/service 接收不可伪造字段的 TenantScope，SQL 同时限定资源和租户；平台跨租户读取经过同事务审计。平台 grant 授权使用资源，不转移所有权。

| 私有事实 | 持久与访问边界 | 自动化验证 |
| --- | --- | --- |
| 规则及修订/产物 | rule_heads/revisions/artifacts 的 tenant_id 与组合外键；当前版本 CAS | TestRuleImmutableRevisionConflictAndTenantScope；TestRuleAuditRollback |
| 自托管节点、个人节点组 | resource_ownership 区分 platform/tenant，tenant 必须有 owner_tenant_id；成员触发器检查相同归属/能力 | TestNodeGroupOwnershipAndStableMembers；TestNodeGrantValidityHistoryAndAudit |
| 端口、服务成员 | allocation 固定租户/成员版本；平台物理端口全局冲突检查；授权不构成释放证明 | TestPortPlatformCrossTenantPhysicalConflict；TestPortAuditRollbackAndForeignAllocation |
| 用量、来源、消费/结算 | usage_batches、source/ledger/reservation/settlement 的 tenant_id；独立 source 约束与不可变 tuple/digest | TestLedgerConservationRebuildSourceIsolationAndAuditRollback；TestUsageIdempotentTupleDigestDisputeAndFinal |
| 公共列表与事件 | 当前 PG principal；每个 collection/cursor 绑定 principal/tenant；安全投影；SSE 每页重验 | TestHTTPBusinessReadsOwnershipGrantExpiryAndSafeFields；TestHTTPResourceListCursorScopeOrderingAndMutableBusinessState；TestSSECommittedEventsReplayScopeAndRESTConvergence |

固定角色、单租户用户和独立平台角色由 policy 与 PG 身份约束共同执行。`TenantScope.ForTenant` 保留原认证主体和动作，不能把 read/mutate 提升为 grant。审计写失败时不能返回未经审计的平台读取结果。

`TestValkeyLossRebuildAllProducedAuthority` 先建立非空用户、私有/平台资源、grant、restriction、已确认与 unknown task，再真实 FLUSHALL、断开缓存并从 PG 重建；操作、所有权、任务与审计事实不变。`TestLedgerConservationRebuildSourceIsolationAndAuditRollback` 另行建立非空来源余额和随机有界承诺，验证来源投影重建及 FLUSHALL 后字节相同，拒绝外租户来源读取和审计部分提交；`TestSourceLocalUnknownDoesNotNetOrFreezeNewSource` 独立验证未知负债不能跨来源抵消。这闭合早期 WINDOWS #1 的来源余额验证路径；最终验收仍取决于同提交三端原始结果。

最终运行提交为 65a53358，Docker 与原生 amd64/arm64 Actions 42572674719 完整通过。完整原始结果和最终状态以 06-VERIFICATION.md 为准。前端和 Phase7 操作流程、Phase4/8 专用性能与物理恢复不由这组测试证明。

当前最终验收：实现提交`65a5335894732b7d632e870acc3d8283be224cd3`，Docker及Actions42572674719原生amd64/arm64完整通过（含真实Cloudflare）。原始证据`artifacts/phase6/06-11/final-65a53358`；阶段完整结论与保留范围见06-VERIFICATION.md。以上较早证据仍按当时范围解释。
