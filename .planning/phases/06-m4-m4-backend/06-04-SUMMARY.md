---
phase: 06-m4-m4-backend
plan: 04
subsystem: packages
tags: [postgresql, sqlc, immutable-snapshot, entitlement, restriction, lifecycle]
requires:
  - phase: 06-03
    provides: immutable rule revisions, durable compile/deploy and conservative execution observations
provides:
  - Immutable package/catalog versions, subscription grants, independent traffic sources and production billing snapshots
  - Locked atomic entitlement redemption and actor/code/impact-bound future package confirmation
  - Durable package lifecycle and restriction projection with offline-safe expiry and unknown retention
affects: [06-05, 06-06, 06-07, 06-11, phase-7]
tech-stack:
  added: []
  patterns: [immutable grant terms, code-scoped redemption locks, due-time authority revalidation, pure-stop historical dependencies]
key-files:
  created: [backend/db/migrations/00006_packages_entitlements.sql, backend/db/query/packages.sql, backend/db/query/subscriptions.sql, backend/db/query/entitlements.sql, backend/internal/service/package.go, backend/internal/service/entitlement.go, backend/internal/service/package_lifecycle.go, docs/coverage/phase6/PACKAGES.md, docs/changes/CR-006-APPROVAL.json]
  modified: [backend/db/generated/, backend/internal/service/restriction.go, backend/internal/service/rule.go, backend/db/query/rules.sql, backend/cmd/vortex-backend/main.go, contracts/database/schema.sql, contracts/openapi/v1/openapi.yaml, backend/openapi/v1/openapi.yaml, contracts/versions.yaml, docs/governance/contract-review.yaml, tools/phase6/check.sh, tools/phase6/evidence_manifest.sh, tools/phase6/rule_contract_check.py]
key-decisions:
  - "Owner explicitly approved CR-006 exact proposal SHA256 60487b773b60f9b1bfee80811e49c17d43b910b7ff19c68025a33c9c5e83b28d; database/API2.1.0 are frozen, preserving CR005 history."
  - "Successful confirmation consumes the code atomically; future stale/revoked application failure preserves the accepted consumption without automatic refunds."
  - "Expiry authority cannot depend on node reachability or activation grants; exact existing pure-stop revisions reuse historical dependencies while actual stop remains pending proof."
requirements-completed: [PLN-001, PLN-002, PLN-006, PLN-007, PLN-008, PLN-003, PLN-004, PLN-005]
coverage:
  - id: D1
    description: Immutable snapshots isolate catalog edits, enforce one primary package and preserve source/monthly terms
    requirement: PLN-001
    verification:
      - kind: integration
        ref: backend/internal/service/package_test.go#TestPackageCatalogSnapshotAndSubscriptionImmutable
        status: pass
      - kind: integration
        ref: backend/internal/service/package_test.go#TestPackageDurableSnapshotProductionCompileAndHistory
        status: pass
    human_judgment: false
  - id: D2
    description: Redemption and cross-package confirmation consume once with exact replay and all-effects audit rollback
    requirement: PLN-008
    verification:
      - kind: integration
        ref: backend/internal/service/entitlement_confirmation_test.go#TestEntitlementDifferentPackageConfirmationAtomic
        status: pass
      - kind: integration
        ref: backend/internal/service/package_test.go#TestEntitlementRedeemConcurrentUsesAndRequestConflict
        status: pass
    human_judgment: false
  - id: D3
    description: Durable future changes and expiry preserve all restriction causes and unknown occupation despite offline or revoked nodes
    requirement: PLN-005
    verification:
      - kind: integration
        ref: backend/internal/service/package_lifecycle_test.go#TestPackageLifecycleExpiryIndependentOfNodeAuthority
        status: pass
      - kind: integration
        ref: backend/internal/service/package_lifecycle_test.go#TestPackageLifecycleAuditRollbackAndUnknownRetention
        status: pass
    human_judgment: false
actuals:
  tokens: 405074
  tasks: 3
  commits: 26
  plan_head_before: 69875dfcc41b68389603c214dd3330b15a800811
  plan_head_after: 28e4f153affb0c16084c40f8edc3d5b2f4b9eef8
duration: multi-session including explicit owner approval checkpoints
completed: 2026-11-07
status: complete
---

# Phase 6 Plan 4: Packages, Entitlements and Restriction Lifecycle Summary

**不可变套餐与计费快照、原子权益码兑换和真实未来确认，由 PostgreSQL 任务驱动月度发放、到期限制与保守停止。**

## Accomplishments and Verification

- 三个任务及 PLN-001..008 已完成。目录编辑只创建新版本，订阅/来源/计费继续绑定旧快照；单主套餐、UTC 原始月锚点、能力交集与最小限制、独立来源到期均有实库验证。00006 是唯一 CREATE billing_snapshots 的迁移；00001..00005 保持原字节。
- RedeemEntitlement 与 PreviewEntitlement/ConfirmEntitlement 有实际成功路径。确认将锁码、次数消费、redemption、不可变授予快照、未来 change、operation/task/outbox/audit 纳入同一事务；预览绑定 actor、码哈希、版本、时刻及完整影响。相同 key 精确重放，冲突、撤销、耗尽、过期及审计失败整体回滚。原码不进入日志或任务摘要。
- 生产 main 注册 due/lifecycle worker。未来生效前不改变当前订阅，到期重新验证操作者及影响；月度来源幂等、来源独立到期、限制仅清本来源，所有原因清除前不恢复。停止意图追加实际 disabled revision；当前节点离线或 grant 撤销不再阻止权威到期，未知执行及七天保留不会被提前释放。
- 最终实现/批准源为 `9db5da0eb1d8d5f20baa7802ca37007588d6cc35`。独立 clean clone 执行 `check.sh --through-plan 06-04 --produce docker --expected-sha 9db5da0eb1d8d5f20baa7802ca37007588d6cc35`：exit0、result0、source_clean=true、acceptance_complete=true。该字段仅表示本机 producer；最终同提交 Actions 配对仍归属06-11。before/after source status 均为空。
- 原始证据在 `artifacts/phase6/06-04/9db5da0eb1d8d5f20baa7802ca37007588d6cc35/`，证据提交 `28e4f153`。Go1.27.3/Linux arm64 Docker 全 backend 普通/race 通过（service8.915s/15.177s）；完整 contracts/Agent/Engine 普通/race、真实 backend/Agent/Hub mTLS/套接字/恢复测试均通过，无 skip、FAIL 或 DATA RACE。修复后的本机全 backend 普通/race13.965s/19.098s 另保留在 a9275d86 证据。
- sqlc1.31.1 两次独立生成与 committed output 精确相同；OpenAPI source/copy lint/cmp、wire 两次生成与 legacy 兼容、schema snapshot、requirements-map、SemVer 和四个 selected freezes 均通过。真实 PostgreSQL seeded00005→00006 测试逐行保留已有规则、任务、审计与未知观察；Goose v3.29.0 PG-only runner up/status 的源、模块及结果保留在前轮验证目录。
- Linux amd64/arm64 backend/Agent/Engine 编译通过，VCS stamped clean；本轮原生运行/race仅为Linux arm64 Docker，交叉编译不作原生amd64或性能证据。精确依赖与许可门禁通过，没有新增依赖。
- 真实 Cloudflare CRUD 通过，私密 token/policy 从 gitignored 配置读取，证据仅记录 policy hash 与身份审核边界。所有新证据已扫描真实 token 字面值，未发现泄露。
- 批准后防篡改反例验证 owner原话、旧批准历史、规范schema、catalog来源和changelog 任一变动均拒绝；manifest 也拒绝删到期测试及校验和篡改。缺Actions明确返回 `actions result.json missing`，没有伪造配对。

## Task Commits

1. Task1 immutable authority：RED `cf9c3f8f`；GREEN `3e55bdfd`；迁移/actor加强 `a5d3ba08`；schema copy修正 `a071c601`。
2. Task2 redemption：RED `b0a27f1f`；GREEN `7de2edf1`；跨套餐确认 RED `b594a877` / GREEN `6a66412a`；portable wrapper `ad00a4b4`；UTC实库RED `9d81cc22` / GREEN `d7e83d76`。
3. Task3 future/restriction/lifecycle：RED `fe18e898` / GREEN `e87dce38`；在途观察与审计重试 `c884c73b` / `a0188f13`；monthly/expiry RED `6f7f76bc` / GREEN `b47724ba`；受限停止 RED `a77e0549` / GREEN `9ff95f0d`；离线/revoked到期 RED `e81835b8` / GREEN `a9275d86`。
4. 精确候选/gates `f2d9641b`；历史证据/checkpoint `ccba6615`、`345dea19`；真实批准/freeze `9db5da0e`；批准后完整证据 `28e4f153`。

actuals 按磁盘 ledger 基线到 summary 前 HEAD 测量26个提交、232个文件及 diff字符数/4（405074），包含所有保留的原始证据。远超34000初估主要来自真实未来确认、生命周期安全缺陷、整套重复验证与保留失败证据；未使用模型运行token数充当估算尺度。

## TDD Gate Compliance

- 三任务均先实库 RED 后 GREEN；原始失败和分类器结果保留在 task red JSON 中。缺表/缺确认路径、UTC replay位置差异和 offline/revoked 到期失败均是真实业务断言失败，未用编译失败或数据库不可用充作RED。
- 新离线/revoked负例证明原到期事务因激活校验回滚并阻塞下一租户；GREEN同时证明 package_expired、源到期、retention、unknown occupation、历史依赖与其他租户推进。修改目标的 disabled edit 仍被当前授权校验拒绝。
- 后续完整普通/race覆盖所有回归，未降低/skip检查。无额外REFACTOR提交；本计划 type:execute，无单独plan-level TDD模式。

## Decisions and Deviations

1. 精确owner门禁：用户明确“批准 CR-006”，于2026-11-07T20:45:23Z记录。proposal SHA256保持 `60487b773b60f9b1bfee80811e49c17d43b910b7ff19c68025a33c9c5e83b28d`；数据库/API兼容2.0→2.1冻结，原候选不可变。CR005历史前缀与全部旧批准保持精确，仅追加两条CR006批准；catalog/changelog只允许该批准元数据变换。
2. 用户批准的消费政策：码在成功确认时消费，未来权限撤销或影响过时不自动退款次数、不重新签发；审计保留实际失败。码duration/amount固定到grant，catalog能力/价格/reset保持版本语义。
3. [Rule1 Bug] Linux UTC暴露Go时间Location对象导致首次结果和JSON重放不等；实库RED后规范返回UTC。前轮真正失败的ad00/a071 clean-source结果保留，未改为passed。
4. [Rule1 Bug] 到期不能依赖节点在线/激活授权。纯停止仅当新输入严格等于旧修订且Enabled=false时复用旧segments/dependencies，仍排队编译；任何其他编辑仍resolve。期望disabled不冒充执行已停止。
5. [Rule3 Tooling] 未安装上游全驱动Goose CLI；使用已有审核v3.29.0库的临时PostgreSQL-only runner运行同一up/status API，无新依赖。源/模块/build metadata及输出完整保留，未宣称原CLI运行。

## Scope and Retained Limits

- 所有本计划业务边界已完成，无阻断stub。正式逐来源资金账本、signer/path/lease producer仍属06-05/06-06，HTTP适配属06-07。真实生产编译仅消费已有真实输入；缺资金/epoch时继续pending，未制造资金或默认授权。
- 最终同SHA Actions、1000规则reconciliation与其他Phase6需求仍属06-11。当前缺Actions的负例不是计划06-04要求跳过的验证；后续强制配对仍不能放行。
- 历史VLink approved-hash mismatch、Phase4资格与Phase5 passed_with_scope均保持原事实。四个当前selected冻结通过，不冒称全契约/全系统发布完成。
- 新代码的请求、快照/计费及限制/规则边界均属于计划T-06-07/08/17；没有另开未审查网络或认证边界。

## Self-Check: PASSED

已核对源/批准/证据文件存在，所有上列提交均为HEAD祖先，proposal hash和新结果checksums准确；ledger与测量HEAD一致。最新clone clean且关键普通/race测试、五类批准反例和三项manifest反例均通过预期断言。当前checkpoint已解决。
