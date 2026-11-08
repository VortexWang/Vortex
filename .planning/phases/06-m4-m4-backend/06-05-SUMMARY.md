---
phase: 06-m4-m4-backend
plan: 05
subsystem: metering
tags: [postgresql, immutable-ledger, usage-idempotency, unknown-liability, mtls]
requires:
  - phase: 06-04
    provides: immutable package grants, traffic sources and billing snapshots
provides:
  - Source-local immutable hash-chain ledger and rebuildable B/F/L projections
  - Provenance-preserving idempotent Agent/Hub usage with reserve, consume, unused return and final proof
  - Persistent unknown reconciliation and explicitly funded conservative closure with independent compensation
affects: [06-06, 06-07, 06-11, phase-7]
tech-stack:
  added: []
  patterns: [source-local fixed-point rounding, original usage tuple retention, funded unknown closure]
key-files:
  created: [backend/db/migrations/00007_metering_ledger.sql, backend/db/query/ledger.sql, backend/db/query/usage.sql, backend/internal/service/ledger.go, backend/internal/service/metering.go, backend/internal/service/usage_reconciliation.go, backend/internal/worker/usage_reconciliation.go, docs/coverage/phase6/LEDGER.md, docs/changes/CR-007-APPROVAL.json]
  modified: [backend/db/generated/, backend/internal/control/adapter.go, backend/cmd/vortex-backend/main.go, agent/store/usage_origin.go, hub/usage_backend.go, contracts/versions.yaml, docs/governance/contract-review.yaml, tools/phase6/rule_contract_check.py]
key-decisions:
  - "Owner explicitly approved exact CR-007 proposal 3ddd1ab79c656b3684e64a3854f32785ff2fb8998743ac73353cdc90b086b0c5; database/API2.2.0 and control1.5.0 frozen with CR005/006 history unchanged."
  - "Unknown liability remains reserved after 72h; closure requires actual approved platform backing and atomically issues a separately funded compensation source."
  - "Agent reports retain their first node/report-boot/sequence across repeated restart and lost acknowledgements."
requirements-completed: [MTR-001, MTR-002, MTR-003, MTR-004, MTR-005, MTR-006, MTR-007, MTR-008, MTR-009, MTR-010, MTR-011, MTR-013, MTR-014, MTR-015, MTR-016]
coverage:
  - id: D1
    description: Immutable source ledger, billing policy snapshots and source-local accounting survive projection/cache rebuild
    verification:
      - kind: integration
        ref: backend/internal/service/ledger_test.go#TestLedgerConservationRebuildSourceIsolationAndAuditRollback
        status: pass
      - kind: integration
        ref: backend/internal/service/ledger_test.go#TestRateVersionSnapshotLeadAndBillingExplanation
        status: pass
    human_judgment: false
  - id: D2
    description: Usage tuples and snapshot bindings prevent duplicate charging, fabricated self-hosted reports and provenance changes across reboot
    verification:
      - kind: integration
        ref: backend/internal/service/metering_transport_test.go#TestUsageActualAgentHubPostgresLostAckRestartAndOriginalSequence
        status: pass
      - kind: integration
        ref: backend/internal/service/metering_test.go#TestUsageSourceFixedPointRemainderAcrossGrants
        status: pass
      - kind: integration
        ref: backend/internal/service/metering_wire_validation_test.go#TestUsageWireRejectsMalformedAndUnfundedSelfHosted
        status: pass
    human_judgment: false
  - id: D3
    description: Unknown bounds remain frozen without funding; funded closure, late exact results and audit failure conserve each source
    verification:
      - kind: integration
        ref: backend/internal/service/metering_test.go#TestUnknownRiskBudget72HourFreezeLateExactAndAuditRollback
        status: pass
      - kind: integration
        ref: backend/internal/service/metering_fault_test.go#TestUsageFaultPowerLossAuditTransactionRollback
        status: pass
    human_judgment: false
actuals:
  tokens: 466517
  tasks: 3
  commits: 17
  plan_head_before: 9749c7af71b5b6d430f58b67ef5398c0adc90ce2
  plan_head_after: 4403ac529641ae8330f214893b015cf3bb8f8f10
duration: multi-session including exact owner approval checkpoint
completed: 2026-11-08
status: complete
---

# Phase 6 Plan 5: Source Ledger, Usage and Unknown Liability Summary

**逐来源不可变账本与原始用量幂等贯通 Agent、Hub 和 PostgreSQL；未知负债只有在明确足额平台资金及原子补偿下才保守封账。**

## Accomplishments and Verification

- 三项任务完成，15个MTR需求按本计划边界验证。每个来源独立保存B/F/L、不可变事件哈希链和重建投影；来源到期不跨来源抵销负债，Valkey清空后从PG恢复。计费快照固定有序政策倍率、系数与版本边界，政策候选需提前24h；余数在来源内跨授权累计，安全预留ceil与实际消费floor分开。
- 用量按(node_id,report_boot_id,sequence)和digest去重；冲突隔离争议，不覆盖原事实。预留、消费、退未用量、final和unknown均校验同一快照/执行点/原授权。自托管零系数不生成收费预留；空delta或收费快照伪装self_hosted被拒绝。保护带最多12s，未来起点与越过硬截止均不允许。
- Unknown保留安全预留且不冒充实测字节；72h只生成持久争议/告警，无预算继续冻结。有明确批准平台资金时，同事务扣预算、保守封账并发放独立补偿来源；迟到精确结果只减少平台损失，不再增加余额。生产UsageReconciliationWorker已注册并消费持久job。
- 真实双段mTLS Agent→Hub→PG验证丢回执、重复用量、Hub恢复和第二次Agent boot；首个原始报告身份不可变。历史无原始身份的SQLite payload不捏造boot/sequence，继续待对账。
- 用户明确“批准 CR-007”，于2026-11-08T02:49:25Z记录。候选SHA256保持`3ddd1ab79c656b3684e64a3854f32785ff2fb8998743ac73353cdc90b086b0c5`，20项原候选字节从`995d7fca`验证。仅catalog三项freeze、changelog追加notice及三条governance批准发生合法变化，CR005/006原文件和历史前缀不变。
- 批准源`84821a04cec4dec78a8a3b192d2003d1f44518bc`由独立完整clean clone运行`check.sh --through-plan 06-05 --produce docker`，exit0、result0、source_clean=true、acceptance_complete=true；前后status为空。完整backend、Agent/Hub/Engine普通/race、真实进程/mTLS、故障专项、四项selected freeze、sqlc/Protobuf两次生成、旧wire/schema兼容、OpenAPI/map/依赖许可和真实Cloudflare CRUD均通过。
- 本机原生运行环境为Linux arm64 Docker，Go1.27.3、PG18.6、Valkey9.1.3。Linux amd64/arm64二进制与Engine包编译通过，不把交叉编译当原生amd64执行证据。原始35项producer checksum和附加证据保存于`artifacts/phase6/06-05/84821a04cec4dec78a8a3b192d2003d1f44518bc/`，证据提交`4403ac52`。
- 17项工具测试通过；CR007原话、proposal/hash、companion、重复记录、时间和规范字节等篡改均被拒绝。删除保护带/原始身份关键用例并重算checksum的语义负例仍拒绝；缺Actions明确失败。全部新证据做真实token字面值扫描，无泄露。

## Task Commits and TDD

1. Task1 source authority：RED`929ba6be` → GREEN`04260fb1`。
2. Task2 usage identity/reservations：RED`518e522d` → GREEN`6c754473`；OpenAPI封套`36e69cf5`。
3. Task3 unknown/funding/reconciliation：RED`981659c8` → GREEN`577b69a0`；候选及门禁`23574367`。
4. 实际反例修复：空条目/伪装self_hosted RED`3b3a91df`→`577b69a0`；未来保护带RED`56c63092`→`0c18aab8`；再次boot报告身份RED`33db4c25`→`995d7fca`。这些均是目标业务断言实际失败，未以编译或缺环境当RED；原始记录及语义分类保留在development证据中。
5. 批准前clean证据`8b4a0d9e`及checkpoint`3e011b43`保留原acceptance_complete=false；真实批准/freeze`84821a04`及批准后clean证据`4403ac52`形成新结果，没有覆写旧失败/待批准事实。

actuals按基线到summary前HEAD的17个提交、241个变更文件、diff字符数/4估算，包含保留原始证据；不是模型运行token统计。超42000初估来自真实双段用量重放、安全反例修复、完整重复验证和证据保留。

## Deviations from Plan

- [Rule1 Bug] 真实wire负例暴露空delta panic与收费快照绕过；严格验证用量及零系数身份，拒绝时权威不变。
- [Rule1 Bug] 未来保护带起点可绕过12秒；增加实际起点边界，所有硬截止继续独立取交集。
- [Rule1 Bug] 回执丢失后再次Agent boot曾覆盖报告身份；首个原始身份永久复用，新增store与双段mTLS回归。
- [Rule3 Blocker] 共享Git对象克隆不能在Docker解析alternate路径；改独立完整clone，旧失败/中断1/137结果保留于failed目录，不改判通过。

## Retained Scope and Next Plan

- 当前acceptance_complete=true仅为本机producer。本计划最终同SHA Actions配对仍属于06-11，尚未运行；没有宣称Phase6或全系统验收完成。
- SIGKILL应用事务与服务器连接终止、虚拟24h/72h推进不等同物理断电、PG磁盘恢复或真实72h等待。
- 活跃signer、运行/份额租约和Hub资金委托producer由06-06实现，HTTP由06-07负责；本计划不合成默认epoch、租约或资金。旧VLink冻结差异、Phase4资格与Phase5历史判定保持原事实。
- Ready for 06-06：线性份额链、租约、boot fencing及可证明的handoff，随后06-07/06-11。

## Self-Check: PASSED

确认全部源/批准/证据存在、提交为HEAD祖先、原候选摘要及35项producer摘要准确。新clone前后clean，必要普通/race用例均实际执行；SDK状态同步将在summary提交后进行，历史Phase5 passed_with_scope不被改写。
