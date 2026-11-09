# 合并后审查

## F-01 / high / 已修复：字节窗口内小 DATA 帧误触发队列溢出

位置：engine/vlink/dispatch.go 接收队列。原 StreamWindow=8 MiB、StreamQueueFrames=512；小 DATA 的帧数先于字节窗口达到上限。实跑最早错误为 stream receive queue is full，随后重置/级联违规关闭链路。该问题双方原先均有，并非只在 FIN 出现。

新增慢消费者 + 分片合法窗口 + 第二流响应 + FIN 顺序复现，修复前稳定失败。改为 TCP 专用有界接收队列：按字节窗口限制载荷，只有帧槽耗尽才压紧队列内相邻 DATA，保留弹出帧的独立 lease 及控制顺序；UDP 原队列不变，链路读循环不等待业务消费者。无 wire 或契约修改。

Darwin 和 Linux 完整 Engine race 均通过。原 64 MiB integrity、伪造 OPEN、旁路及中继故障验证与 native comparison 全入口通过。第一轮修复后 comparison 因缺配套 workload baseline 最终报告失败，未将其标通过；随后先生成 qualification 基线，再执行完整 comparison 成功。以上工作树验证非 clean-source SHA 验收，性能不提升为正式资格化。

## F-02 / medium / 待修复

200ms 是明确 admission 上限，不能以延长生产 deadline 掩盖 FULL 事务测试成本。6238、2968 CI 均在16个并发 reserve/commit 的同一128字节块夹具失败；需要分开验证短写结算正确性与有界排队拒绝。

## F-03 / medium / 已修复

policy-cutover 的40秒激活超时实际日志为 Node bind address already in use，且与刚启动 Hub 端口完全相同。regionalPort 分配随机端口后立刻关闭，后续同夹具可再次分配到同一端口。修复应阻止活动夹具内重复地址，不扩大激活超时。

## F-04 / medium / 精确批准待准备

旧 VLink encoding 实际字节与旧批准摘要不一致。保留批准历史，准备精确治理提案；不视为 CR-010 批准范围。

## F-05 / medium / 工具兼容限制

query audit-uat 拒绝历史 Phase 5 passed_with_scope。未改写历史状态；直接审阅现有 UAT/VERIFICATION 和真实 CI 失败。此处不修改外部 GSD 安装。

F-03 修复：regionalPort 在当前活动测试范围登记已选地址并在 cleanup 释放，避免重复选择。完整 SharePolicyProcess 两轮真实进程 race 通过，54.706 秒；未增加等待超时。

## F-06 / high / 已修复：早期 DATA 与 OPEN_OK 竞争删除合法流

reserveEarlyData 在锁外读取 Opening；OPEN_OK 可能在 MarkLocalData 前完成。旧 MarkLocalData 会因状态不再 Opening 删除该已授权流，随后原本用于竞争恢复的 Snapshot 失效。拒绝过时早期记账应保留新状态；仅真正超出 pending-open 字节上限仍删除流。

新增确定性状态交错测试，修复前失败，修复后完整 VLink race 通过 6.839 秒。原 overflow、FIN、负向状态测试均保留。
