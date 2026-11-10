# 合并后审查

## F-01 / high / 已修复：字节窗口内小 DATA 帧误触发队列溢出

位置：engine/vlink/dispatch.go 接收队列。原 StreamWindow=8 MiB、StreamQueueFrames=512；小 DATA 的帧数先于字节窗口达到上限。实跑最早错误为 stream receive queue is full，随后重置/级联违规关闭链路。该问题双方原先均有，并非只在 FIN 出现。

新增慢消费者 + 分片合法窗口 + 第二流响应 + FIN 顺序复现，修复前稳定失败。改为 TCP 专用有界接收队列：按字节窗口限制载荷，只有帧槽耗尽才压紧队列内相邻 DATA，保留弹出帧的独立 lease 及控制顺序；UDP 原队列不变，链路读循环不等待业务消费者。无 wire 或契约修改。

Darwin 和 Linux 完整 Engine race 均通过。原 64 MiB integrity、伪造 OPEN、旁路及中继故障验证与 native comparison 全入口通过。第一轮修复后 comparison 因缺配套 workload baseline 最终报告失败，未将其标通过；随后先生成 qualification 基线，再执行完整 comparison 成功。以上工作树验证非 clean-source SHA 验收，性能不提升为正式资格化。

## F-02 / medium / 已修复（夹具）

200ms 是明确 admission 上限，不能以延长生产 deadline 掩盖 FULL 事务测试成本。6238、2968 CI 均在16个并发 reserve/commit 的同一128字节块夹具失败；需要分开验证短写结算正确性与有界排队拒绝。

## F-03 / medium / 已修复

policy-cutover 的40秒激活超时实际日志为 Node bind address already in use，且与刚启动 Hub 端口完全相同。regionalPort 分配随机端口后立刻关闭，后续同夹具可再次分配到同一端口。修复应阻止活动夹具内重复地址，不扩大激活超时。

## F-04 / medium / 已获批准并修复

旧 VLink encoding 实际字节与旧批准摘要不一致。保留批准历史，准备精确治理提案；不视为 CR-010 批准范围。

## F-05 / medium / 工具兼容限制

query audit-uat 拒绝历史 Phase 5 passed_with_scope。未改写历史状态；直接审阅现有 UAT/VERIFICATION 和真实 CI 失败。此处不修改外部 GSD 安装。

F-03 修复：regionalPort 在当前活动测试范围登记已选地址并在 cleanup 释放，避免重复选择。完整 SharePolicyProcess 两轮真实进程 race 通过，54.706 秒；未增加等待超时。

## F-06 / high / 已修复：早期 DATA 与 OPEN_OK 竞争删除合法流

reserveEarlyData 在锁外读取 Opening；OPEN_OK 可能在 MarkLocalData 前完成。旧 MarkLocalData 会因状态不再 Opening 删除该已授权流，随后原本用于竞争恢复的 Snapshot 失效。拒绝过时早期记账应保留新状态；仅真正超出 pending-open 字节上限仍删除流。

新增确定性状态交错测试，修复前失败，修复后完整 VLink race 通过 6.839 秒。原 overflow、FIN、负向状态测试均保留。

F-02 修复保留生产200ms上限：短写结算测试改为4轮、每轮4个并发写者，使每轮最坏 in-flight=128 字节符合块大小；仍验证16笔17字节实际提交、块周转、wire用量与关闭后逐来源守恒。原确定性排队超时、取消不增权、group独立结算与未知负债测试保留。四项 race 连续10轮通过10.441秒。这不是提升生产存储性能或保证过载流量成功。

## F-07 / high / 已修复：部分入队后 early-data 失败提前归还载荷所有权

WriteDataFrames 第一个帧可能已由 TLS writer 使用，第二个帧在早期字节上限处等待 OPEN_OK 超时；旧代码直接返回，调用方可以 Release pooled payload，writer却仍在读原内存。确定性 net.Pipe 无读端测试先失败，修复后用既有 stopDataWrite 关闭并等待 writerStopped 再返回，完整VLink race通过。错误仍保守关闭，不把未确认写入当未执行。

F-07 同类路径补齐：WriteData 也可能在同批第一帧入队后因第二帧早期上限超时；统一关闭并join writer。两个真实writer阻塞回归及完整Engine race通过。

## F-08 / medium / 已修复：持久化时限测试被跨package资源竞争干扰

授权入口原本并行执行Agent/Store/Hub及真实进程构建/测试，双核Actions产生排队等待、FULL/fsync竞争；有界200ms正确拒绝不能被解释为必须成功的正确性断言。入口加 go test -p 1，保留全部测试、race与生产时限，不采用重试掩盖。

Linux真实4MiB tmpfs完整authorization_check入口已通过：Agent287.455秒、Store102.395秒、真实进程100.311秒，vet和Engine也通过。本次为运行中工作树快照（报告SHA2ce76d77，但含当时未提交入口变更），不作clean-source或最终SHA配对证据。先前首调用因未挂载worktree Git元数据退出128，修正只读元数据挂载后运行成功。

F-04：用户明确批准 CR-011（推荐）。精确候选 SHA-256 0631f2fd4ed7f9d9f028a55a26e57c3a7e404edb7c2f9fcdf77a4e8e3f3c7d2e 已写入；旧Draft批准原记录保留。catalog1.1.0、encoding.sha256、CHANGELOG、63条现有VLink需求review metadata同步；wire0x0100和实现字节不变。全catalog冻结、映射/SemVer及两次Protobuf生成均通过。批准仅关闭当前治理缺口，不重批历史验收。

Linux Docker comparison已通过64MiB完整性/半关闭和中继故障片段；其最终比对因运行期间HEAD从31830266变为b85ca3a0而正确拒绝跨SHA，保留失败，最终验收将使用固定干净提交。

## F-09 / medium / 已修复：目标服务与转发节点同时退出

最终干净168be8a5 Docker模式检查中，pool通过，mux的qualification被unknown用量断言正确拒绝。唯一unknown为1200字节UDP，destination日志在清理窗口明确记录向target写入ECONNREFUSED。清理原本对所有Compose服务同时stop，目标服务可能先于转发节点退出。改为只先stop五个转发节点，收取退出状态与记账日志后再down目标服务；不修改未知负债或验收断言。原失败证据保留，下一提交重新验证。

F-09 修复后实际Docker mux workload通过：309264条记账事件全部已提交，左右各3910425492字节与target相等，无unknown或UDP丢失。该工作树测试只用于修复验证，最终提交另行运行。

## F-10 / high / 未解决：连续流量授权在200ms内未完成

Actions42606221450的完整授权race中，TestRuntimeFundingBlockContinuousTraffic的TCP/UDP子用例均失败。TCP日志明确指出1500字节admission总耗时约205ms、read-current-authority返回context deadline exceeded，随后收到RST；UDP收到两字节RST载荷，尚未确定其独立根因。Agent package299.405s，Store102.852s，真实进程105.793s。不能据此证明只是夹具问题或降低200ms上限，也未证明在该宿主上连续流量全部可成功。保留原失败，后续需要单独复现并区分控制签发、事务排队、SQLite持久化及调度时延。

## F-11 / medium / 未解决：最终恢复入口的区域切换超时

干净168be8a5本机Docker完整phase5-check已通过前面的全模块、真实进程、regional十轮、Engine等步骤，但最后recovery_check中的TestRegionalFailoverRetainsUnknownDebt/silent-established在5.04秒失败，PendingUnknownUsage查询返回context deadline exceeded。完整入口及恢复子报告均为failed。该5秒context也覆盖初始化，且本轮有其他本机Docker负载，但尚未证明因果，不能直接增大超时或标记通过。

以上两项作为未解决项保留；不合并修复PR，不将完整Phase5验收标通过。
