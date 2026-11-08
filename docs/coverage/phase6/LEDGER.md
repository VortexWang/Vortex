# 来源账本、用量与未知负债（06-05）

当前候选尚未获 owner 冻结批准，acceptance_complete=false。HTTP adapter 属于
06-07，签发代次、运行/份额租约、Hub 资金委托 producer 属于 06-06。
本计划的 PostgreSQL 预留不能代替这些尚未签发的权利。

| 需求 | 实现与真实验证 |
| --- | --- |
| MTR-001 / 006 / 016 | Agent 原始 wire delta 经 NodeControl 或 Hub 原始 envelope 落 PG；tuple/digest 冲突保留原始用量并落争议；`TestUsageIdempotentTupleDigestDisputeAndFinal`、`TestUsageActualAgentHubPostgresLostAckRestartAndOriginalSequence` |
| MTR-002 | 来源级固定点余数跨授权累计，安全 ceil 与用户实际 floor 分离；`TestUsageSourceFixedPointRemainderAcrossGrants` |
| MTR-003 / 010 | 不可变有序组倍率向量、有效边界与 24h 提前同步候选；`TestRateVersionSnapshotLeadAndBillingExplanation`；新版 signer 激活仍须06-06生产证据 |
| MTR-004 / 005 / 013 | 逐来源 B/F/L、不可变哈希链、可重建投影、来源过期保障；`TestLedgerConservationRebuildSourceIsolationAndAuditRollback`、`TestSourceLocalUnknownDoesNotNetOrFreezeNewSource` |
| MTR-007 / 008 | 授权绑定 boot 与 snapshot，不复用已结算/已退回额度；`TestReserveConcurrentNonceDualVersionAndReturnUnused`；运行停止与实际门控保持 Agent/Engine 既有边界，完整新签发生产者06-06 |
| MTR-009 / 015 | 未知上界不当实测消费，24h/72h继续冻结，足额明确批准平台预算同事务补偿；`TestUnknownRiskBudget72HourFreezeLateExactAndAuditRollback`、`TestUnknownExactFinalReconciliationAndProtectionBand` |
| MTR-011 / 014 | 自托管零系数不创建预留；UTC 区间交叠时禁止新增预留；保护带最多12s且不延长硬截止；完整运行/份额生产者仍06-06 |
| SUP-B2-001 / SUP-B5-001 | 守恒/快照绑定、共享 AuditWriter 回滚、强杀应用事务、回执丢失与 Hub 重启；`TestUsageFaultPowerLossAuditTransactionRollback`、上述 real transport 与 ledger tests |

`TestUsageWireRejectsMalformedAndUnfundedSelfHosted` 拒绝空用量条目及收费快照
伪装自托管的上报，失败不改变来源权威；自托管必须绑定零系数快照且不携带计费授权。
`TestUnknownProtectionRejectsFutureStart` 拒绝未来保护带起点，防止以无效时间绕过12秒上限。

生产 UsageReconciliationWorker 实际消费已过期的预留、来源签发事实及 PostgreSQL
reconciliation job，退避有界，72h落持久争议与告警。没有资金、最终证明或原始
报告序号时保持 pending/unknown；不以 timeout、当前授权清单或进程重启释放。

Hub 增加独立 usage_origin_envelopes 元数据，保留原 node/report boot/sequence 与
payload digest。既有 SQLite 用量 payload 原字节保持；历史无 envelope 的记录
由生产 BackendUsageClient 返回 ErrUsageOriginMissing，继续保留待对账。
已保存身份的未知用量在回执丢失、Agent 再次重启后复用首个报告身份，既不覆盖
原 boot/sequence，也不重复消费；store 反例和双段 mTLS 重启测试共同验证。
生产 AccountingServer/UsageRelay 与 BackendUsageClient 只转交计量事实，不能
签发运行或资金权利。活跃 signer 的完整后端握手仍由06-06接管。

Valkey FLUSHALL 后 source balance 从 PG 重建测试已执行。WINDOWS #1 的06-05
实现部分已覆盖，跨域最终验收与同 SHA Actions 配对仍归06-11。
`ledger_fault_injection.sh` 用真实 PG、SQLite 和双段 mTLS 执行应用进程 SIGKILL、
服务器连接终止、丢失回执、重复批次、24h/72h推进。它不声称物理主机断电、
PG磁盘物理恢复或72小时真实等待；这些边界与虚拟时钟推进明确区分。
