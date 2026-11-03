# Phase 4: M2 多跳与引擎完备 - Context

**Gathered:** 2026-11-04
**Status:** Ready for planning

<domain>
## Phase Boundary

本阶段在 Phase 3 VLink 单跳能力之上交付多跳路径编排、授权候选路径切换、会话排空、不可变计费快照与预扣边界、份额/代次安全切换、Prepare/Commit/Abort 原子应用，以及 TCP/UDP 混合负载公平调度。Engine 仍保持独立，不访问数据库或主动调用控制面；控制面和持久化权威由后续阶段提供，当前通过注入接口和可复现夹具验证。

</domain>

<decisions>
## Implementation Decisions

### 多跳路径、代次与会话排空
- **D-01:** 路径只允许在预先授权且当前健康的候选路径中切换；不得绕过任何中间节点。
- **D-02:** 每次生效路径变更递增 `generation`；旧代次停止接纳新流，只允许既有 TCP 会话排空；新 TCP 连接使用新代次。
- **D-03:** UDP 旧代次会话结束；后续数据报按新代次重新 OPEN，不进行不透明重放。

### 计费快照、预扣与未知负债
- **D-04:** 会话建立时绑定不可变计费快照；快照或套餐周期变化只影响新会话，既有会话不迁移。
- **D-05:** 预扣结果超时或未知时保留预扣上界并冻结相关额度；不得把授权到期视为已消费或已释放。
- **D-06:** 新会话额度不足或续申失败时拒绝新流；既有会话只能在有效授权内继续，耗尽后停止写入并保留未知负债。
- **D-07:** D-04 的预扣块、水位和续申参数以现有候选值作为可调基线，必须经过 Docker/CI 混合负载和正式评审后定值。

### 两阶段配置应用
- **D-08:** Commit 超时且结果未知时进入 `activation-unknown`，保留新旧资源占用，并查询或重试同一事务 ID。
- **D-09:** 同一事务的 Prepare/Commit/Abort 按事务 ID 幂等重放原结果；冲突阶段转换返回稳定错误。
- **D-10:** 同一节点同一 boot 的并发配置使用单调 fencing generation 与 CAS，最多一个 active generation；旧准备不能覆盖新代次。
- **D-11:** Abort 回执丢失时，只有持久化或执行状态证明新代次未激活且旧续租权已关闭，才允许释放相关资源。

### TCP/UDP 调度与验收边界
- **D-12:** TCP 与 UDP 使用独立有界队列和明确调度份额，TCP 长流不得阻塞 UDP。
- **D-13:** UDP 队列或 VLink 窗口满时丢弃数据报并分类计数；不阻塞 TCP，区分窗口、队列和下游丢弃。
- **D-14:** 三中继吞吐低于 NFR-003 时记录环境、同路径基准和差距，性能资格保持未通过，不放宽门槛。
- **D-15:** NFR-005/006 与混合负载在同一 SHA 上分别通过本机 Docker 和 GitHub Actions 验证；缺少专用原生机时明确记录未完成。D-04 只有在实测评审后锁定。

### the agent's Discretion
- 具体路径评分、健康阈值、代次状态枚举、排空超时、fencing 错误码、队列容量、调度份额、丢弃计数名和测试拓扑由研究与计划确定，但必须可注入、可观测、可复现，并遵循上述边界。

</decisions>

<canonical_refs>
## Canonical References

**Downstream agents MUST read these before planning or implementing.**

### Project scope and phase contract
- `.planning/PROJECT.md` — 重启治理、Engine 边界、验证证据和外部数据库服务约束。
- `.planning/ROADMAP.md` § Phase 4 — 阶段目标、需求 ID、成功标准和后续边界。
- `.planning/REQUIREMENTS.md` — 保留的需求 ID 与阶段映射。
- `.planning/REQUIREMENTS-SOURCE.md` §§ ENG-026..ENG-050, NFR-003, NFR-005, NFR-006, NFR-039, NFR-040, NFR-044 — 需求语义参考；V1.11 不是冻结验收依据。
- `.planning/STATE.md` — 当前阶段状态与既有决策，规划前需刷新。

### Prior phase contracts and implementation
- `.planning/phases/02-m0-m0-engine-dataplane/02-CONTEXT.md` — Engine 隔离、注入门控、Linux splice 和计量边界。
- `.planning/phases/02-m0-m0-engine-dataplane/02-VERIFICATION.md` — M0 已验证边界和多跳延期项。
- `.planning/phases/03-m1-vlink-m1-vlink-engine/03-CONTEXT.md` — VLink 帧、身份、mux、OPEN、UDP admission 及 Phase 4 边界。
- `.planning/phases/03-m1-vlink-m1-vlink-engine/03-VERIFICATION.md` — Phase 3 验证证据与残余风险。
- `contracts/vlink/v1/encoding.md` — VLink 当前编码和 OPEN/path 字段草案。

### Existing Engine code
- `engine/relay/tcp.go` — TCP 半关闭、预扣后写入和成功字节结算路径。
- `engine/relay/udp.go` — UDP 有界转发、门控和响应计量路径。
- `engine/accounting/accounting.go` — Reserve/Commit/Return/Unknown 语义。
- `engine/vlink/open.go` and `engine/vlink/stream.go` — 本地 OPEN 授权、path generation 和 stream 生命周期。
- `engine/vlink/window.go`, `engine/vlink/pool.go`, `engine/vlink/tracer.go` — 窗口、链路池和 VLink 计量集成点。

</canonical_refs>

<code_context>
## Existing Code Insights

### Reusable Assets
- `engine/accounting` 已提供一次性 Batch 解析和 Unknown 保留上界，可作为 Phase 4 预扣状态机的底层接口。
- `engine/relay` 已有 TCP/UDP 注入 Gate、半关闭传播及写成功字节计量，可接入路径代次和调度层。
- `engine/vlink` 已有 OPEN 的 rule revision/path generation 校验、stream table、per-stream/aggregate window 和 pool，可扩展多跳编排而不引入数据库。

### Established Patterns
- Engine 是独立 Go module；Phase 2/3 的测试入口和 Docker 检查应继续作为本阶段本机/CI 共同入口。
- 协议结构错误、链路身份错误和 stream 级错误已分层处理；Phase 4 的路径/代次错误应保持同样的隔离粒度。
- 所有控制面权威信息通过显式接口注入；不得在 Engine 中新增 PostgreSQL、Valkey、Hub 主动调用或隐式全局状态。

### Integration Points
- 路径调度器连接 `engine/vlink` 的 OPEN、stream、window 和 pool 生命周期。
- 计费快照与预扣连接 `engine/accounting` 和 relay/tracer 的写成功结算。
- 两阶段应用需要与路径 generation、share lease 和 drain 状态联合，但持久化回执接口由后续 Agent/Hub 阶段实现。

</code_context>

<specifics>
## Specific Ideas

- 旧 TCP 路径必须真实排空，不能通过重置连接伪装切换成功。
- UDP 不透明重放；所有丢弃原因必须能在指标中区分。
- 性能不足只产生可审计差距证据，不得通过放宽 NFR 或将 Docker 仿真当作原生资格。

</specifics>

<deferred>
## Deferred Ideas

- Agent/Edge Hub 生产生命周期、持久化状态恢复和 PostgreSQL 权威编排属于 Phase 5/6。
- 专用原生 Linux 性能资格与最终发布验收属于后续阶段。

</deferred>

---

*Phase: 4-M2 Multi-Hop Engine*
*Context gathered: 2026-11-04*
