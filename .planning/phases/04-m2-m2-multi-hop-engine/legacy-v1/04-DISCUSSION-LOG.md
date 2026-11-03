# Phase 4: M2 多跳与引擎完备 - Discussion Log

> **Audit trail only.** Do not use as input to planning, research, or execution agents.
> Decisions are captured in CONTEXT.md — this log preserves the alternatives considered.

**Date:** 2026-11-04
**Phase:** 4-M2 多跳与引擎完备
**Areas discussed:** 多跳路径与代次, 会话排空, 计费快照与预扣, 两阶段配置应用, TCP/UDP 公平性与验收

## 多跳路径与代次

| Option | Description | Selected |
|---|---|---|
| 预先授权候选路径 | 仅在预先授权且健康的候选路径中切换，不绕过中间节点 | ✓ |
| 规则内备用路径 | 允许规则内备用路径并逐跳重新授权 | |
| 自动健康选路 | 从所有健康节点自动选择 | |

**User's choice:** 只在预先授权且健康的候选路径中切换。
**Notes:** 每次生效路径变更递增 generation；旧代次拒绝新 OPEN。

## 会话排空

| Option | Description | Selected |
|---|---|---|
| 旧路径排空 | 旧路径不接纳新流，既有 TCP 留在旧路径直到排空，新连接走新代次 | ✓ |
| 重置重试 | 尽快重置旧 TCP，由上层重试 | |
| 无排空期限 | 保持旧路径直到自然结束 | |

**User's choice:** 旧路径排空。
**Notes:** UDP 旧会话结束，新报文按新代次重新 OPEN，不透明重放。

## 计费快照与预扣

| Option | Description | Selected |
|---|---|---|
| 建立时快照 | 既有会话固定建立时不可变快照，新会话使用新快照 | ✓ |
| 立即迁移 | 既有会话立即切到新快照 | |
| 仅宽松迁移 | 新快照更宽松时自动切换 | |

**User's choice:** 会话固定使用建立时快照。
**Notes:** 未知结果保留预扣上界并冻结额度；D-04 以候选值为基线，混合负载后评审定值；额度耗尽停止写入并保留未知负债。

## 两阶段配置应用

| Option | Description | Selected |
|---|---|---|
| activation-unknown | Commit 未知进入 activation-unknown，保留两代资源并查询/重试同一事务 | ✓ |
| 超时 Abort | 超时即 Abort 并释放新代次 | |
| 重新 Prepare | 认为失败后重新 Prepare | |

| Decision | Selected behavior |
|---|---|
| 重复事务 | Prepare/Commit/Abort 按事务 ID 幂等重放；阶段冲突返回稳定错误 |
| 并发代次 | 单调 fencing generation + CAS，最多一个 active |
| 丢失 Abort 回执 | 只有状态证明未激活且旧续租权关闭后释放 |

## TCP/UDP 公平性与验收

| Option | Description | Selected |
|---|---|---|
| 独立有界队列 | TCP/UDP 独立队列和调度份额，TCP 不阻塞 UDP | ✓ |
| 共享 FIFO | 所有流共用 FIFO | |
| UDP 绝对优先 | UDP 始终绝对优先 | |

**User's choice:** 队列/窗口满时 UDP 丢弃并分类计数，不阻塞 TCP。
**Notes:** 同一 SHA 在 Docker 与 Actions 验证；性能不足记录差距，不放宽 NFR；D-04 仅在实测评审后锁定。

## the agent's Discretion

- 具体调度份额、队列容量、健康阈值、排空超时、错误码、代次状态枚举和测试拓扑由研究与计划确定。

## Deferred Ideas

- 生产 Agent/Hub 持久化生命周期和专用原生性能资格留给后续阶段。
