# Phase 5: M3 节点 Agent 与 Edge Hub - Context

**Gathered:** 2026-11-05
**Status:** Ready for planning

<domain>
## Phase Boundary

交付可运行的 vortex-node 与 Edge Hub：节点身份、控制连接、持久化授权和计量、有限自治、恢复交接、配置激活及安全升级。Engine 保持独立 Go 模块、无数据库、无主动控制面调用；Agent 通过正式进程内接口接入。后端与 Hub 不承载业务流量。

V1.11 仅作参考，当前项目决策与本阶段验收定义优先。Phase 4 按用户决定关闭实现范围，原验证 gaps_found 及性能、D-04 缺口保留，不构成已通过资格化。Phase 5 接续持久化权威与恢复集成，不冒充生产后端已经交付。
</domain>

<decisions>
## Implementation Decisions

### 注册与凭据

- **D-01:** 一次性令牌仅首次 CSR 注册；运行证书表达 node_id，boot_id 在运行握手绑定；私钥节点本地生成并受限存储。
- **D-02:** 短期证书自动续期；新证书校验成功后退役旧证书；吊销拒绝重连和续期，无无限过期宽限。

### 进程监督与升级

- **D-03:** 单一 vortex-node，正式进程内接口协调；Agent 崩溃进程退出，由 systemd 重启。
- **D-04:** 停止新流，有界排空；超时关闭残余会话再替换。
- **D-05:** 校验二进制/签名、磁盘和配置兼容性，保留旧二进制与兼容状态快照；健康失败自动回退，不可逆 schema 迁移阻止自动回退。

### SQLite 耐久与磁盘故障

- **D-06:** 账本、授权和回执使用 FULL；对外 ACK 前提交并同步落盘，普通遥测可单独批处理。
- **D-07:** 满盘/写失败停止新授权与新流，并停止产生无法记账消费的既有流；保留未确认记录，恢复后对账。
- **D-08:** Agent/Hub 独立 SQLite，通过幂等协议交接，不共享 WAL。

### Hub 自治边界

- **D-09:** 最后成功后端心跳计时；连续 30 秒失联进入自治，硬截止为心跳+自治上限，重启不延长。
- **D-10:** 仅既有规则、逐节点既有份额、已委托额度；禁止新规则/端口/DNS/注册与跨节点再平衡。
- **D-11:** 可信时间未恢复或硬截止不明确，禁止跨绝对边界新授权并停止相关新流。
- **D-12:** 区分 signer_epoch 与路径 generation；恢复时原子关闭旧签发权并转接余量，旧授权在原边界内有效，路径仅在配置变化时切代。

### 账本交接与重放

- **D-13:** 先执行吊销与限制；同一 SQLite 事务封存旧前缀至 S，以唯一转接记录将同一委托未签发余量 R 转给后继 epoch；在外授权由原预留覆盖。
- **D-14:** 前缀与持续尾账按唯一 ID 幂等重放；仅确认检查点推进后清理已结清记录，至少保留 7 天；争议记录持续保留。
- **D-15:** 停止相关签发并冻结未知负债，保留证据；控制连接切换或超时本身不能释放资源。

### 激活状态与重启恢复

- **D-16:** 发送 Commit 许可前持久化事务和占用；发送后结果未知保持 activation-unknown，查询同一事务，不凭超时释放。
- **D-17:** 每次启动生成新 boot_id；旧授权仅补报和结算；先恢复账本与可信时间，再 Prepare、取得合法授权、Commit；新 boot_id 不是旧进程已停止的证明。
- **D-18:** 按事务 ID 持久化幂等结果；查询持久化 fencing 下限及 Engine 实际代次；冲突转换返回稳定错误，满足安全终结证明后才释放。
- **D-19:** 明确标记的后端协议测试服务配合真实 Agent/Hub、SQLite 与 Engine；验证崩溃、满盘、丢回执，同一 SHA 的 Docker 与 Actions 保存证据；生产后端业务闭环留给 Phase 6。

### 实施与研究裁量

- 研究确定模块布局、SQLite schema/连接策略、协议字段和测试注入机制，遵循已冻结契约的 SemVer 与需求映射检查。未确定的新依赖按首次使用前评审；候选清单需用户确认后锁定。
- 具体证书寿命、排空超时、自治上限、重试和健康检查数值尚未在本轮单独锁定；以参考默认值作为研究输入，规划时列出候选与验收依据。
- 升级回退的兼容状态快照不得回滚已确认计量、签发权关闭记录、fencing 下限或未知负债；这是已确认耐久性与不重复签发约束对回退方案的限制。
- 签发权转接不是强制路径切换。旧授权仍受原截止与吊销/限制约束，不因恢复或换连接增加权利。
</decisions>

<canonical_refs>
## Canonical References

- `.planning/PROJECT.md` — 项目重启决定、核心价值、Agent/Engine 架构边界。
- `.planning/ROADMAP.md` — Phase 5 目标、需求范围和成功条件。
- `.planning/REQUIREMENTS.md` — 需求追踪状态。
- `.planning/REQUIREMENTS-SOURCE.md` — AGT-001..012、HUB-001..018、SUP-AGENT-001、SUP-B1-001、SUP-B3-001、SUP-B4-001 的参考语义；非冻结验收依据。
- `contracts/protobuf/v1/vortex.proto` — 当前控制面契约，集成前检查覆盖与版本变更。
- `contracts/runtime/config.schema.json` — 运行配置契约。
- `contracts/versions.yaml`、`contracts/requirements-map.yaml` — 契约版本与需求映射。
- `.planning/phases/04-m2-m2-multi-hop-engine/04-VERIFICATION.md` — 既有有限实现证据和未资格化边界；历史路由限制由用户推进决定替代，验证失败不改为通过。
</canonical_refs>

<code_context>
## Existing Code Insights

### Reusable Assets
- `engine/apply/apply.go` — 两阶段应用状态机。
- `engine/share/share.go` — 份额与代次边界。
- `engine/accounting/accounting.go` — 预扣和计量接口。
- `engine/runtime/accounting/` — 当前进程内 credit、grant、policy、share 切片；需衔接 Agent/Hub 耐久状态，不能当权威数据库。
- `engine/runtime/link/supervisor.go` — 现有链路监督。
- `engine/cmd/vortex-relay/` — 可启动 tracer 的集成参考。

### Established Patterns and Integration Points
- 本轮扫描尚无 Agent/Hub 生产模块；规划需包含可启动节点与 Hub 的端到端切片，避免仅实现状态机而缺少运行时。
- 持久化属于 Agent/Hub；Engine 通过正式进程内接口接收授权、配置、计量 ACK 和停止指令。
- Docker 与 Actions 的结果必须来自同一 SHA，分别记录环境；未执行或失败项保持未通过。
</code_context>

<specifics>
## Specific Ideas

用户选择六个领域全部讨论，逐项确认推荐方案。最后一组通过“按推荐”确认。signer_epoch 与路径 generation 的区分为后续明确澄清，替代先前混用代次的提问。
</specifics>

<deferred>
## Deferred Ideas

- 完整生产后端业务闭环属于 Phase 6；Phase 5 使用明确标记的协议测试服务，不将其称为生产后端权威。
- Phase 4 性能资格化、独立 TLS 比例及 D-04 仍保持未完成；本轮讨论不锁定参数、不变更资格化结果。
</deferred>
