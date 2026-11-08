---
phase: 06-m4-m4-backend
plan: 06
subsystem: limits-runtime
tags: [postgresql, sqlite-full, share-cas, funded-runtime, mtls, recovery]
requires:
  - phase: 06-05
    provides: source-local ledger and original usage provenance
provides:
  - Independent rate/connection share chains, conservative handoff and Node FULL CAS
  - Durable allocation windows, funded Hub issuance, runtime/share lease refresh and complete source finalization
  - Explicit Node boot and Hub same-store/cold recovery with independent manifest, fences, roots and admission
affects: [06-07, 06-11, phase-7]
tech-stack:
  added: []
  patterns: [finite source-funded authority, complete closure inventory, explicit incarnation recovery]
key-files:
  created: [backend/db/migrations/00008_limits_shares.sql, backend/internal/service/share.go, backend/internal/service/runtime_funding_scheduler.go, backend/internal/service/hub_recovery_admission.go, docs/coverage/phase6/LIMITS.md, docs/changes/CR-008-APPROVAL.json]
  modified: [agent/store/, agent/control.go, hub/runtime_server.go, backend/internal/control/, backend/internal/worker/, contracts/protobuf/v1/]
key-decisions:
  - "Exact CR-0084e29497f82808b9a8693030ef400dae3a2bc42cc27cf8a39f24bdcf8605074bd explicitly approved; database/API2.3.0 and control1.6.0 frozen."
  - "Expiry, disconnect and unknown execution never substitute for complete closure or financial final proof."
  - "Node/Hub recovery retains original source/limits/deadline; each independent FULL proof is required before advancing."
requirements-completed: [LIM-001, LIM-002, LIM-003, LIM-004, LIM-005, LIM-006, LIM-007, LIM-008, LIM-009, LIM-010]
coverage:
  - id: D1
    description: Linear independent shares and complete closure before durable CAS release
    verification:
      - kind: integration
        ref: backend/internal/service/share_transport_test.go#TestShareHandoffRealMTLSAgentAndPostgresWorker
        status: pass
      - kind: integration
        ref: backend/internal/service/share_fault_test.go#TestShareHandoffFaultSIGKILLAfterDurableCASAndExpiredWorkerRecovery
        status: pass
    human_judgment: false
  - id: D2
    description: Finite funded runtime, independent leases and complete original-source finalization
    verification:
      - kind: integration
        ref: backend/internal/service/hub_runtime_lease_transport_test.go#TestRuntimeLeaseActualMTLS60SecondsAgentInstallAndLostBackendReceipts
        status: pass
      - kind: integration
        ref: backend/internal/service/runtime_effective_funding_test.go#TestEffectiveRuntimeFundingAfterInstalledOldCreditFinalKeepsOriginalShare
        status: pass
    human_judgment: false
  - id: D3
    description: Explicit recovery proofs retain debt and require actual Node FULL witnesses
    verification:
      - kind: integration
        ref: backend/internal/service/hub_recovery_admission_transport_test.go#TestHubRecoveryAdmissionActualMTLSExplicitProofFULLLossReconnectAndNewIssue
        status: pass
      - kind: integration
        ref: backend/internal/service/runtime_pending_engine_test.go#TestRuntimeRecoveryPendingEngineCommitAutomaticallyRenewsAfterSR
        status: pass
    human_judgment: false
completed: 2026-11-08
status: complete
---

# 06-06: shares, funded runtime and explicit recovery

三项计划任务已完成。逐需求生产路径及主要测试见 `docs/coverage/phase6/LIMITS.md`，扩展任务单元与全部历史故障证据见 `06-06-CONTINUATION.md`。实现超出原估算，持续拆成有界原子提交；没有缩减来源守恒、未知占用或恢复证明要求。

- Rate/connection各自按tenant/node/kind建立同boot线性代次；预留max、完整旧续租关闭、Node持久floor/实际CAS、匹配回执后才释放差额。连接降额需排空。未知激活/不可达签发方保持占用。
- 持久60秒candidate window、50%保底、稳定整数余数、真实不足排队和逐节点引用复用。平台Commit先获两类bootstrap容量，执行还需独立运行/share/credit权利。
- Hub每次签发来自已预留来源parent，FULL journal持久后才响应；60秒lease与10秒heartbeat独立于credit。续资需全部predecessor完整seal、全部child final、原来源parent final。过期、耗尽估计或丢回执均不能释放未知财务负债。
- 新Node boot、Hub冷恢复/同库恢复均有显式授权及停止/安全截止检查。同库恢复验证完整prefix/manifest、Node实际安装inventory fence、新根FULL receipt和全租户admission；PG admitted不冒充本地online或ACK。重复恢复保留最初来源、限额与deadline。
- 用户明确“批准 CR-008”；批准绑定固定33项候选清单及proposal摘要，批准源为已测`cf92b7e24b6c557a5d14a18b0549a67eac349eb4`。历史CR005/006/007原件不变，仅catalog冻结、changelog追加notice和治理批准记录按授权更新。

## Verification and limits

完整本机backend race(service334.663s)通过；精确干净克隆cf92b7e2在Linux Docker arm64通过backend normal/race(service211.753/359.668s)、Agent/Store/Hub/contracts/Engine normal/race、隔离磁盘满、ledger fault2.820s、share/recovery fault139.984s及Node/Backend/Hub六项双架构构建。前后源码干净。两次独立sqlc/protobuf生成、依赖/许可、旧wire、OpenAPI和精确历史批准检查通过。语义证据检查及五种错SHA/脏源码/缺fault/skip/缺关键pass负例通过。原始证据在 `artifacts/phase6/06-06/task3-unified-acceptance/`。

更早开发Linux用例观察到额外重连，原因未定位，失败日志保留；后续独立3次、完整affected和本次clean normal/race/fault均保持严格断言并通过。五项相关contract freeze通过；VLink历史review hash不一致早已存在，原字节和版本未改，不把该项声明为通过。

交叉编译不等于nativeamd64执行；Docker arm64不等于Ubuntu24.04资格或专用性能。pending Engine恢复fixture仅选定成员运行实际Engine；不声称全成员转发/重启/滚动升级、物理断电或完整模型证明。同SHA Actions配对及跨域验收仍06-11，REST/auth/SSE和Phase7恢复依赖契约继续06-07。
