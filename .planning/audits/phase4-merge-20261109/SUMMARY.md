---
workflow: gsd-audit-fix
execution: inline
status: complete-with-open-findings
base: a102bb31d45711acfade8eab9ac4ca7bc3016a78
source_head: 03ca3547a8d229b378e8178dd9856e6cc2262afd
pull_request: https://github.com/VortexWang/Vortex/pull/1
---

# 合并后审查与修复结果

已提交经过复现与相关测试验证的修复，PR #1保持草稿；完整验收没有全部通过，也没有合并到main。原Phase 6工作区的HEAD、porcelain状态与tracked binary diff摘要均与审查前一致。

| 发现 | 结果 | 提交 |
|---|---|---|
| F-01：合法小TCP帧先填满帧槽，导致接收重置 | TCP按字节窗口有界压紧排队DATA；保留FIN及其他流响应 | 31830266 |
| F-02：短写正确性夹具隐含16路突发存储性能假设 | 保留16笔结算，分为每轮4并发，仍验证短写/周转/守恒；不改200ms生产上限 | 2ce76d77 |
| F-03：活动进程夹具重复随机端口 | 登记当前fixture地址并在cleanup释放；完整policy进程两轮通过 | f322e984 |
| F-04：旧VLink精确批准摘要不覆盖当前规范 | 用户批准CR-011后落盘精确候选、catalog1.1.0、映射和变更记录；wire0x0100不变，保留旧批准 | 168be8a5 |
| F-05：外部GSD拒绝历史passed_with_scope | 保留历史判定，直接审阅UAT/VERIFICATION；未修改全局工具 | 外部限制 |
| F-06：OPEN_OK竞争导致早期记账删除已授权流 | 过时Opening记账不删除新状态；真正overflow仍拒绝 | b85ca3a0 |
| F-07：部分帧已入队时提前归还payload所有权 | WriteData/WriteDataFrames错误返回前关闭并join实际writer | 0b36fc15、77670097 |
| F-08：多个race package争用持久化/时限资源 | 授权入口逐package执行，保留全部测试和race | 0300588e |
| F-09：清理时目标服务先退出，制造未知UDP用量 | 转发节点先退出并核验记账，再关闭目标；不豁免unknown | 03ca3547 |
| F-10：连续流量授权在200ms内未完成 | 未解决，保留失败及交易/余额诊断 | 无 |
| F-11：恢复入口silent-established切换5秒超时 | 未解决，保留失败；不通过增大时限掩盖 | 无 |

新增确定性复现先失败后通过，Darwin/Linux完整Engine race、policy真实进程、短写十轮等开发验证见REVIEW与原始压缩日志。运行中工作树的授权入口通过仅作开发证据，不冒充固定干净源码。

固定168be8a5：Actions Phase1/2/3/4均通过，含64MiB完整性与半关闭、伪造OPEN、旁路、中继故障、16流60秒记账和pool/mux/auto功能检查。Phase4工作流明确checkout分支HEAD，报告内commit也是168be8a5。Phase5工作流checkout PR临时合并提交0bca7120；其tree与168be8a5完全一致，但commit不同，不能称同SHA配对。

Phase5 Actions42606221450完整授权race失败：TestRuntimeFundingBlockContinuousTraffic的TCP/UDP子用例未通过。干净168be8a5本机Docker完整phase5-check亦失败，失败位于最后recovery_check的区域切换，前面的全模块、真实进程、regional十轮、Engine等均通过。两项失败不能被之前的局部通过覆盖。

03ca3547相对168be8a5只改了Phase4清理脚本与该finding的报告/证据，Agent、Hub、Engine、contracts源码无差异。干净03ca3547本机Docker的pool/mux/auto完整功能模式入口全部通过、dirty=false。该源码的Actions Phase1/2/3/4通过，Phase4运行42608942763与本机模式检查形成相同源码SHA的功能回归记录；Phase5在收尾快照时仍运行。状态以final-validation中的实测快照为准。后续纯文档/证据提交不伪称已经重新执行全部测试。

本轮不重新授予历史D-04/NFR性能资格、不改变Phase6计划执行进度、不导入另一工作区的Phase6实现；短功能模式检查不是专用硬件吞吐、时延或24小时验收。

下一轮应先单独复现F-10/F-11，区分授权生效时间、控制往返、事务排队、持久化与夹具初始化时间；保留200ms和未知负债冻结边界。在完整门禁通过前，PR保持草稿。
