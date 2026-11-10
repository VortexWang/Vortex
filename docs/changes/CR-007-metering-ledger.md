# CR-007：来源级账本与用量原始身份候选

状态：待 owner 明确批准。精确审查单元为同目录
`CR-007-metering-ledger.json`，其中固定全部规范文件 SHA-256 与已批准的
CR-006 基线。没有创建或暗示新的 approval，历史 CR-005/006 原字节不改写。

候选 JSON SHA-256：
`3ddd1ab79c656b3684e64a3854f32785ff2fb8998743ac73353cdc90b086b0c5`。

| 契约 | 候选 | 改动 |
| --- | --- | --- |
| database | 2.1.0 → 2.2.0 | 追加00007；不可变来源账本、B/F/L、快照政策边界、usage tuple/摘要冲突、未知上界、足额风险预算与任务 |
| public API | 2.1.0 → 2.2.0 | 明示逐组倍率解释与逐来源账本；HTTP adapter仍06-07 |
| control | 1.4.0 → 1.5.0 | 协商usage-ledger-v1后，既有LedgerRecord携带version1原始Node envelope；保留原始node/report boot/sequence/digest |
| runtime / authorization / VLink | 维持已批字节和版本 | 不签发默认epoch、份额、运行租约或资金权利 |

72小时仅触发持久争议/告警。无完整精确证明或足额、明确批准的实际平台预算，
未知负债持续冻结。保守封账同事务扣预算、记旧来源费用、追加独立补偿来源；
迟到精确事实只减少平台损失，不重复增减用户余额。预算默认不存在。

原始Agent报文在Hub追加独立provenance记录，旧SQLite payload原字节不改；
历史缺失序号的事实保持pending/reconcile。已提交回执丢失重放时返回原确认。
新生产AccountingServer与BackendUsageClient只传事实，不签发权限。
活跃signer与完整Hub资金/运行权限 producer 属于06-06，不能据本候选宣布上线。

验证范围、原生宿主及未运行证据见LEDGER coverage与本提交的原始Docker结果。
应用进程SIGKILL和虚拟24h/72h推进不等于物理断电、磁盘恢复或真实等待72小时。
本候选审核通过前acceptance_complete=false，最终同SHA Actions配对仍06-11。
