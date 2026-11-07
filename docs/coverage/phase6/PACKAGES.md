# 套餐、权益码与限制生命周期（06-04）

契约候选为 CR-006：数据库 / Public API 2.1.0。实现验证与契约 owner
批准分别记录；pending 不等于冻结或阶段验收。HTTP 适配明确归属 06-07。

| 需求 | 当前实现与可执行验证 |
| --- | --- |
| PLN-001 | 目录编辑只追加版本；`TestPackageCatalogSnapshotAndSubscriptionImmutable` |
| PLN-002 | 单主套餐、UTC 原始月锚点、到期不复活；`TestPackageSubscriptionConcurrentPrimaryAndAuditRollback`、`TestPackageLifecycleMonthlyExpiryAndReplay` |
| PLN-003 | 固定影响预览与摘要、未来确认、过时拒绝；`TestDowngradePreviewConfirmDueAuthority`、`TestDowngradeAuditRollbackWorkerRetryAndStaleConfirmation` |
| PLN-004 | 独立限制原因与账户状态、清本来源且保留其他原因；`TestRestrictionAllCauses`、`TestRestrictionAllCausesAccountState` |
| PLN-005 | 到期禁用修订、受限状态仍能停止、保留未知占用；`TestRestrictionAllowsStopRevisionAndCompile`、`TestPackageLifecycleAuditRollbackAndUnknownRetention` |
| PLN-006 | 编译使用持久化不可变计费快照及真实授权路径系数；`TestPackageDurableSnapshotProductionCompileAndHistory` |
| PLN-007 | 独立流量来源不合并、独立到期；`TestEntitlementRenewalMonthlyNoneDurationAndIndependentSources` |
| PLN-008 | 锁码与幂等键、次数、兑换与所有事务副作用；`TestEntitlementAtomicRedemptionContract`、`TestEntitlementDifferentPackageConfirmationAtomic`、并发/审计回滚/适用性测试 |

`TestPackageMigrationUpgradeSeeded00005` 在独立 PostgreSQL schema 真实执行
00005 到 00006 升级，逐行比较既有任务、审计、规则修订与未知执行观察。
00001..00005 不改写；00006 是唯一创建 billing_snapshots 的迁移。

跨套餐码通过 PreviewEntitlement / ConfirmEntitlement 真实成功兑换。
预览只读且绑定码哈希、操作者、版本、完整影响和生效时刻；确认事务重查
当前操作者、锁码、消费次数，追加 redemption、grant snapshot、package_change、
operation/task/outbox/audit。码中的 duration_days / amount 覆盖授予快照的时长
与流量；能力、价格和重置策略来自码固定的目录版本。同套餐有效期延长
保留原快照和月锚点。确认响应是未来操作，不提前改变当前订阅。

码在确认时消费。后续 due worker 因操作者撤销或影响已变化而失败时，消费
记录仍保留，不自动退款、恢复次数或重复签发。返回的旧结果按相同 key
精确重放；新 key 不能再次消费耗尽码。原始码不进入日志/审计/任务摘要。

生产 main 注册 package due 与 lifecycle worker。月度新来源、来源到期事实、
套餐到期限制和真实 disabled rule revision 在审计事务中持久化。disabled
修订可在限制下编译和排队，既有 RuleWorker/Agent 两阶段协议负责执行；
未知回执始终保留占用，不把 desired disabled 当已停止。单规则数量限制
不会停止合规兄弟规则，移除一项原因不会绕过仍有效的原因。

持久化 disabled 修订不等于当前整个平台已能签发部署资金。生产资金账本、
signer/path/lease producer 属于 06-05/06-06；本计划不填造默认 epoch 或资金。
相应 compile_input 未就绪时 compile worker 等待，原执行状态保持未知/原状态。
单测显式提供可审核的既有接口输入不能证明该后续生产者已上线。

证据入口为 `tools/phase6/check.sh --through-plan 06-04`，从同一提交 clean clone
运行完整 backend 普通/race、真实进程及组件回归、sqlc 两次独立生成和 Linux
amd64/arm64 编译。交叉编译不等于目标 CPU 原生执行。契约未批准或 Cloudflare
setup 缺失时 acceptance_complete=false，最终 06-11 配对门禁必须拒绝放行。
