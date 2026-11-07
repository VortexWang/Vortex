---
phase: "06"
slug: "m4-m4-backend"
status: draft
nyquist_compliant: false
wave_0_complete: false
created: "2026-11-07"
---

# Phase 06 — Validation Strategy

本文件是执行时的验证契约；规划检查通过不代表测试基础设施或业务实现已经通过。所有未运行项保持 pending。

## Test Infrastructure

| Property | Value |
|----------|-------|
| Framework | Go testing/race、真实 PostgreSQL/Valkey Docker 集成、Phase 5 Agent/Hub mTLS 进程拓扑 |
| Module/config | 独立 backend/go.mod；backend/db/sqlc.yaml；backend/db/migrations；deploy/backend/compose.test.yml |
| Quick run | `cd backend && test "$(GOTOOLCHAIN=local go env GOVERSION)" = go1.27.3 && GOTOOLCHAIN=local go test ./... -count=1`（先由 Compose 导出 PHASE6_DATABASE_URL；不得因缺少数据库而静默 skip） |
| Full suite | 提交后从仓库根执行 `./tools/phase6/check.sh --expected-sha "$(git rev-parse HEAD)" --through-plan 06-11`；脚本运行固定镜像 Docker/race/故障拓扑并核对本机与 Actions 的同 SHA 证据 |
| Runtime | 尚未测量；focused 目标不超过 120 秒，全套目标不超过 10 分钟；1,000 规则 reconciliation 单独断言 <=60 秒 |

## Sampling Rate

- 每个实现任务先执行其 PLAN 中的完整 automated verify；所有 Go 入口使用 backend 模块、GOTOOLCHAIN=local，并断言 GOVERSION=go1.27.3。
- 数据库迁移只追加，按执行顺序 00001..00008 升级；06-08 必须验证 seeded 00001 数据库升级后保留既有权威记录，并先提供限制事实/投影供 06-09 消费。
- 每次 SQL 变更在测试前执行锁定版本 sqlc generate，提交 backend/db/generated 产物；以临时目录二次生成并比较验证无漂移。
- 每个波次提交后运行已实施范围的证据门禁；最终 06-11 才要求完整 96 项覆盖及全部证据。不得让早期门禁依赖未来计划尚未创建的文件。
- clean-tree/expected-SHA 断言在任务提交后运行；生成的日志位于约定证据目录，不能污染提交源树。若存在其他工作改动，使用干净检出运行证据，不删除他人的改动。
- 在 `$gsd-verify-work` 前，本机 Docker 与 GitHub Actions 必须对相同提交完成适用验证；缺失、skip、失败或不同 SHA 不能标记通过。

## Per-Task Verification Map

下表引用各计划中完整 verify（含失败信号），避免复制后产生命令漂移。任何新任务必须同步本表。决策检查点沿用精确提案批准规则。

| Task ID | Wave | Behavior / artifact | Verification | File Exists | Status |
|---------|------|---------------------|--------------|-------------|--------|
| 06-01-01 | 1 | desired state 到 Agent/Hub receipt 的端到端 tracer | a9481584 本机 Docker/Actions 42316152923 同 SHA normal/race 真实进程通过 | ✅ 已实现 | ✅ passed implemented scope |
| 06-08-01 | 2 | tenant scope, roles and transaction-scoped audit | a9481584 真实 PostgreSQL 固定角色、隔离与审计 normal/race；两端通过 | ✅ 已实现 | ✅ passed implemented scope |
| 06-08-02 | 2 | sqlc/toolchain reproducibility and Valkey loss gate | a9481584 双次 sqlc、精确许可闭包、已实施权威 Valkey 丢失重建；两端通过；来源余额待 06-05/11 | ✅ 已实现 | ✅ passed implemented scope |
| 06-09-01 | 3 | no-generator OpenAPI and fail-closed engineering gate | a9481584 Docker/真实 Actions 42316152923 通过，check/manifest 同 SHA 配对均返回 0 | ✅ 已实现 | ✅ passed implemented scope |
| 06-09-02 | 3 | all restriction causes must clear | a9481584 两端真实 PG normal/race，多原因恢复、账号限制与 audit rollback 通过 | ✅ 已实现 | ✅ passed implemented scope |
| 06-02-01 | 4 | 节点组、成员、grant 与生命周期服务 | 自动化；执行 06-02-PLAN.md 对应任务的完整 verify | ❌ 待实现 | ⬜ pending |
| 06-02-02 | 4 | 端口预留、限制保留和释放确认 | 自动化；执行 06-02-PLAN.md 对应任务的完整 verify | ❌ 待实现 | ⬜ pending |
| 06-10-01 | 5 | serving eligibility and encrypted DNS credential boundary | 自动化；执行 06-10-PLAN.md 对应任务的完整 verify | ❌ 待实现 | ⬜ pending |
| 06-10-02 | 5 | controlled Cloudflare CRUD and fail-closed evidence | 自动化；执行 06-10-PLAN.md 对应任务的完整 verify | ❌ 待实现 | ⬜ pending |
| 06-03-01 | 6 | immutable rule revisions 与输入校验 | 自动化；执行 06-03-PLAN.md 对应任务的完整 verify | ❌ 待实现 | ⬜ pending |
| 06-03-02 | 6 | deterministic compiler 与 immutable per-node artifacts | 自动化；执行 06-03-PLAN.md 对应任务的完整 verify | ❌ 待实现 | ⬜ pending |
| 06-03-03 | 6 | revision-scoped Prepare/Commit/rollback/unknown worker | 自动化；执行 06-03-PLAN.md 对应任务的完整 verify | ❌ 待实现 | ⬜ pending |
| 06-04-01 | 7 | catalog 与 immutable subscription/billing snapshot | 自动化；执行 06-04-PLAN.md 对应任务的完整 verify | ❌ 待实现 | ⬜ pending |
| 06-04-02 | 7 | entitlement code atomic redemption | 自动化；执行 06-04-PLAN.md 对应任务的完整 verify | ❌ 待实现 | ⬜ pending |
| 06-04-03 | 7 | restriction projection 与 downgrade preview/confirm | 自动化；执行 06-04-PLAN.md 对应任务的完整 verify | ❌ 待实现 | ⬜ pending |
| 06-05-01 | 8 | source ledger、计费快照和 B/F/L projection | 自动化；执行 06-05-PLAN.md 对应任务的完整 verify | ❌ 待实现 | ⬜ pending |
| 06-05-02 | 8 | usage idempotency、reserve/consume/final/dispute | 自动化；执行 06-05-PLAN.md 对应任务的完整 verify | ❌ 待实现 | ⬜ pending |
| 06-05-03 | 8 | unknown upper bound 与 fault reconciliation | 自动化；执行 06-05-PLAN.md 对应任务的完整 verify | ❌ 待实现 | ⬜ pending |
| 06-06-01 | 9 | share owner chain、lease 与 boot fencing | 自动化；执行 06-06-PLAN.md 对应任务的完整 verify | ❌ 待实现 | ⬜ pending |
| 06-06-02 | 9 | successor handoff、receipt confirmation 与差额释放 | 自动化；执行 06-06-PLAN.md 对应任务的完整 verify | ❌ 待实现 | ⬜ pending |
| 06-06-03 | 9 | limits 与规则/计量联动检查 | 自动化；执行 06-06-PLAN.md 对应任务的完整 verify | ❌ 待实现 | ⬜ pending |
| 06-07-01 | 10 | REST auth、resource routes、errors 与 cursor | 自动化；执行 06-07-PLAN.md 对应任务的完整 verify | ❌ 待实现 | ⬜ pending |
| 06-07-02 | 10 | durable SSE replay 与 gap resync | 自动化；执行 06-07-PLAN.md 对应任务的完整 verify | ❌ 待实现 | ⬜ pending |
| 06-11-01 | 11 | timed 1,000-rule reconciliation workload | 自动化；执行 06-11-PLAN.md 对应任务的完整 verify | ❌ 待实现 | ⬜ pending |
| 06-11-02 | 11 | same-SHA portable evidence gate | 自动化；执行 06-11-PLAN.md 对应任务的完整 verify | ❌ 待实现 | ⬜ pending |

## Requirement and Safety Coverage

| Plans | Requirements / decisions | Required negative and recovery evidence |
|-------|--------------------------|-----------------------------------------|
| 06-01 | BE-001、SUP-B2-001、SUP-B5-001；D-06-17..20 | 真实后端→Agent/Hub mTLS→回执；事务失败无可消费事件，重复/丢回执不重复执行、不释放未知占用 |
| 06-08 / 06-09 | TEN-001..009、BE-004；D-06-01..03、11、21 | 双租户读写隔离、固定角色矩阵、同事务 AuditWriter 回滚、多原因全部解除；Valkey 清空重建不改变权威事实 |
| 06-02 / 06-10 | NOD-001..014、PRT-001..016；D-06-04 | grant、端口竞争与保留、serving 集合；Cloudflare 加密凭据持久化、最小 zone scope、TTL 60/DNS-only、CRUD/重试和秘密不泄露 |
| 06-03 | RUL-001..016、BE-002/003；D-06-05..08 | If-Match 冲突、确定性产物、依赖重编译、Prepare/Commit/Abort、superseded、unknown 与迟到回执 |
| 06-04 | PLN-001..008；D-06-09..12 | 不可变订阅快照、幂等兑换、降级预览确认、限制事件与投影，不从可变目录重算历史 |
| 06-05 | MTR-001..011、MTR-013..016；D-06-13..15 | 逐来源 B_s/F_s/L_s 守恒、用量唯一键、快照绑定、未知负债冻结、争议/故障注入；不存在 MTR-012 |
| 06-06 | LIM-001..010；D-06-16 | boot/generation 线性 CAS、关闭旧续租权、差额释放证明、activation-unknown、Hub 切换与故障注入 |
| 06-07 | BE-005；D-06-21..24 | refresh token PostgreSQL family/rotation/reuse/revocation 与审计；稳定错误/cursor；SSE 授权、Last-Event-ID 与 REST resync |
| 06-11 | BE-006；NFR-019 的 Phase 6 局部验证 | 真实 PostgreSQL 1,000 条规则完整 reconciliation，count=1000 且 elapsed_ms<=60000；显式 manifest、同 SHA 双宿主证据；不宣称 Phase 8 全规模通过 |

## Wave 0 Requirements

- [ ] 06-01：backend 模块、精确工具链、pgx/sqlc/Goose、最小权威 schema/query/generated、测试数据库配置。
- [ ] 06-01：真实 Phase 5 Agent/Hub mTLS tracer、进程证据和数据库故障断言。
- [ ] 06-08：租户策略、统一 AuditWriter、Valkey 可丢失重建、依赖与 sqlc reproducibility。
- [x] 06-09：单一 OpenAPI source、确定性 copy、Ruby structural/security lint；Phase 6 不安装 OpenAPI generator，TypeScript 生成由 Phase 7 负责。
- [x] 06-09：分阶段 check.sh/evidence_manifest.sh 与 Actions 入口，明确 expected-SHA producer；a9481584 同 SHA 配对通过，06-11 仍须扩展最终完整门禁。
- [ ] 后续计划每次新增 SQL 都生成并消费 backend/db/generated；所有 fault shell/test 引用具有本计划或前置 producer。

## Deferred Manual Verification

| Behavior | Owner | Boundary |
|----------|-------|----------|
| PostgreSQL 基础备份、WAL restore、外部服务恢复、RPO/RTO 与密钥恢复 | Phase 7 | Phase 6 只记录依赖契约，不实施恢复演练、不宣称已通过 |
| 全规模、专用原生硬件性能、24 小时稳定性与完整浏览器流程 | Phase 8 / Phase 7 | 保留各阶段验收；1,000 条 reconciliation 局部结果不能替代 |

真实 Agent/Hub mTLS 接入属于 06-01 自动化拓扑验证，不归为人工验证。

## Validation Sign-Off

- [ ] 每个实现任务都有 automated verify 或明确前置基础设施
- [ ] 连续任务不存在超过两项无自动化反馈
- [ ] 所有验证目标均由自身或前置计划创建
- [ ] 无 watch 模式；缺少环境/秘密不能静默 skip 为通过
- [ ] focused 检查实际耗时满足目标并记录
- [ ] 全部执行证据通过后才将 nyquist_compliant / wave_0_complete 标记 true

**Approval:** pending execution evidence
