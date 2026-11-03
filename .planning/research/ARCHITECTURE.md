# VORTEX 架构研究

**领域：** 多租户、多节点、TCP / UDP 多跳转发与可信计量平台  
**研究日期：** 2026-11-02（America/New_York）  
**总体置信度：** MEDIUM  
**状态：** 绿地设计建议；没有已实现组件、已通过模型或系统验收的声明。

## 结论与依据

采用规格已经选定的架构：**独立 Go Engine + 内嵌 Engine 的 vortex-node + 有限自治 Edge Hub + PostgreSQL 支撑的模块化后端单体 + React 管理界面**。后端和 Hub 不进入业务数据路径。Engine 只消费运行配置、授权、份额和可信时间锚点，不访问数据库、不主动调用控制面；Agent 通过规格 8.2 的唯一进程内接口驱动 Engine。入口、中继、出口是节点在规则中的角色，不能拆成三个不同的节点产品。[需求依据：V1.11 §3、§8、§9](../../docs/VORTEX%20需求规格说明书%20V1_11.md)

架构设计重点是把业务期望、节点执行事实、授权签发事实和已确认消费分开保存，再用稳定 ID 与证据收敛。端到端成功不能由单次 RPC 成功定义，资源释放也不能由 RPC 超时定义。gRPC 官方明确指出，修改状态的操作即使完成，也可能向客户端返回超时；取消不会回滚已经发生的修改。因此下文提出的持久化意图、未知状态和幂等重放是对规格 B.3 / B.4 的落实方式，而非 gRPC 自动提供的保证。[gRPC 状态码](https://grpc.io/docs/guides/status-codes/)、[调用生命周期](https://grpc.io/docs/what-is-grpc/core-concepts/)

本报告中的需求约束直接来自 V1.11 正文与附录 B，不以历史附件是否存在替代正式契约。外部技术结论均通过官方资料核对；`classify-confidence --provider websearch --verified` 返回 MEDIUM，沿用该等级。目录、事务划分和扩展策略属于 MEDIUM 的工程建议，仍须通过 ENGR-004 评审与 ENGR-006 模型、故障及性能验证。

## 标准架构与组件责任

### 系统视图

```text
管理面
浏览器 ── REST / SSE ── 后端模块化单体 ── PostgreSQL
                            │                 │
                            │             业务事务 + Outbox
                            ├── Valkey（非权威缓存）
                            │
                      gRPC / mTLS
                            │（Hub 主动连接后端）
                     区域 Edge Hub ── 本实例 SQLite WAL
                            │（Agent 主动连接 Hub）
                      gRPC / mTLS
                            │
                     vortex-node 单一进程
                     ┌─────────────────────────────┐
                     │ Agent ── SQLite WAL         │
                     │   │ 唯一规定的进程内接口     │
                     │ Engine（独立 Go 模块）        │
                     └─────────────────────────────┘

业务面
客户端 ══ 入口 Engine ══ 中继 Engine ══ 出口 Engine ══ 目标
                      VLink / TLS 1.3
直接转发：客户端 ══ 入口 Engine ══ 目标
```

控制图中的箭头说明连接与消息边界，不表示 Hub 或后端代转业务流量。反向隧道仅改变节点间链路的主动拨号方向，链路建立后仍由同一套 Engine 开流与转发。[需求依据：GEN-001～GEN-005、ENG-018](../../docs/VORTEX%20需求规格说明书%20V1_11.md)

### 组件边界

| 组件 | 拥有的责任与权威状态 | 通信边界 | 不能越界的责任 |
|---|---|---|---|
| 前端 | 角色路由、表单、状态与限制原因展示；服务端状态用 TanStack Query，本地交互用 Zustand | OpenAPI REST、SSE | 不直接生成运行配置、额度或授权，不把提交状态显示成执行完成 |
| 后端 `identity/fleet/ports/billing` | 租户、身份、节点归属、段许可、端口、套餐与资金来源等业务事实 | PostgreSQL 事务、领域服务 | 不依赖缓存恢复余额；查询必须进入 tenant scope |
| 后端 `rules` | 唯一规则编译器；固定路径/成员、节点角色与运行配置、不可变计费快照 | 读取期望与政策，输出确定性编译结果 | 不从当前健康状态偷偷改写已经发出的快照内容 |
| 后端 `apply` | 期望/编译/下发/应用/实际状态，apply 生命周期、重试、撤销、回滚和未知激活引用 | Outbox → Hub → Agent；执行回执反向回流 | 不以 actual_version 未更新或 120 秒截止证明未激活 |
| 后端 `leases` | 区域来源委托、份额链修订、允许续租 Hub 集合、所有权截止及安全回收证据 | PostgreSQL 原子分配；Hub 控制协议 | 不自行取代 Hub 签发运行/计费授权；不跨来源挪用冻结负债 |
| 后端 `metering` | 消费批次去重、来源与快照验证、结算、未知上界、风险资金补偿及恢复对账 | Hub 账本前缀/尾部与 Agent 批次 | 不按最新配置重算历史、不把未知上界混入实测消费 |
| Edge Hub | 唯一运行/计费授权签发方；本实例来源额度、签发账本、自治硬截止、signer_epoch 转接 | 后端 gRPC；Agent 双向流；本地 SQLite | 不编译新配置、不承载业务流量；自治不能跨节点重分份额或增加委托 |
| Agent | 注册/身份、可信时间握手、Prepare/Commit 协调、本地意图/回执/拒绝栅栏、计量持久化与补报、升级 | 规格 8.2 Engine 接口；Hub gRPC；本地 SQLite | 不操作业务 socket，不把未持久化计量确认给 Engine |
| Engine | socket、会话与 VLink 生命周期；不可变运行表和会话快照；门控、份额 CAS、预扣/写成功提交、待确认批次 | 仅 Prepare/Commit/Abort/UpdateGrants/Drain/DrainUsage/AckUsage/Events/State | 不读写 SQLite/PG，不发控制 RPC；不向 Agent 暴露绕过接口的内部 socket |
| PostgreSQL | 业务和期望配置唯一事实来源；任务、Outbox、来源负债和执行证据 | 后端仓储层 | 备份旧余额不能覆盖恢复后仍在外的承诺 |
| SQLite WAL | 各节点/Hub 的本地恢复事实；本实例写入顺序与待确认记录 | 所属 Agent / Hub | 不是共享多机账本，不用网络共享盘提供 Hub 集群一致性 |
| Valkey | 非权威缓存、限流、短通知 | 后端 | 丢失不能改变已确认业务、余额、幂等或签发权 |

上述权限分工是 V1.11 §8～§10 的约束；后端模块名虽然包含授权或租约，不改变 HUB-002 的签发主体。SQLite 官方要求 WAL 参与者位于同一主机，因此 Hub 间接管应交换证据和委托，不共享一个 WAL 数据库。[SQLite WAL](https://www.sqlite.org/wal.html)

### Engine 内部分层

| 包 | 责任 | 关键设计约束 |
|---|---|---|
| `pipe` | 双向拷贝、半关闭、空闲、计量/限速执行点 | 裸 TCP Linux 路径保留 splice；不能用连接包装破坏零拷贝；计量和门控必须实际启用 |
| `carrier` | TCP/TLS 承载 | 不绑定套餐、节点数据库或控制连接 |
| `vlink` | HELLO、帧编解码、流控、mux/pool、反向链路 | 帧边界、ID 分配和错误语义先锁定；UDP 有独立队列并遵守公平性限制 |
| `route` | 监听、固定运行表、下一跳、探测状态、UDP 会话 | OPEN 不携带可信下游地址；下一跳从本地已激活配置选取 |
| `gate` | 运行/计费/份额和独立截止、CAS、拒绝及排空 | 权限取交集；区分真正失效、政策保护带、同主体交接 |
| `meter` | 稳定批次、按快照/授权累计、预扣提交退回 | 不把不同快照的额度合并；每笔预扣只完成一次 |

代码导入方向以 `route` 协调数据路径和狭窄接口为主，不把 Engine 变成依赖整个控制面的共享领域包。具体循环依赖消解与接口归属在 M0 设计评审锁定。

## 建议仓库结构

沿用规格 14.1 的 Monorepo，下面子目录是建议，不代表文件已经存在。

```text
VORTEX/
├── engine/                 独立 go.mod；公开 Engine 门面与运行时类型
│   ├── pipe/ carrier/ vlink/ route/ gate/ meter/
│   └── testkit/             模拟 Agent、时钟、授权与基准夹具
├── node/                   vortex-node：Agent + Engine，单个二进制
│   └── internal/           control/store/reconcile/grants/report/probe/upgrade
├── edge-hub/               独立可部署 Hub
│   └── internal/           signer/delegation/ledger/recovery/control
├── backend/
│   ├── internal/           规格 §9.3 的领域模块与 tenantscope
│   ├── migrations/         Goose 迁移、约束、索引
│   └── queries/            sqlc SQL 与生成配置
├── frontend/               用户端和管理端 SPA
├── proto/                  gRPC 消息、服务、版本与错误契约
├── api/                    OpenAPI；前端生成类型
├── contracts/              建议新增：运行配置/授权规范、VLink 编码、兼容矩阵
├── models/                 建议新增：ENGR-006 模型、边界、反例、覆盖映射
├── test/                   拓扑、故障、压测、恢复、证据清单
├── deploy/                 Compose/systemd/镜像及版本基线
└── .github/workflows/      与本机相同测试入口的 CI
```

Engine 的公开接口不能放到只允许模块内部导入的包里；生产 Agent 与模拟 Agent 应使用同一公开门面。可用 `go.work` 支持同仓开发，但 Engine 必须另跑单模块构建与测试，避免工作区替换掩盖独立模块缺依赖。Go 官方支持多模块工作区，也明确 `GOWORK=off` 会回到单模块上下文。node 与其他组件如何在 CI 固定同提交模块依赖，应在版本基线中落定；不能只依赖开发机路径。[Go 工作区教程](https://go.dev/doc/tutorial/workspaces)、[Go 模块参考](https://go.dev/ref/mod#workspaces)

不建议首版引入通用微服务框架、独立消息集群或 Kubernetes：规格已明确后端模块化单体、PG 任务表与 Outbox、Compose/systemd 部署。独立发布边界已有 Engine/node/Hub/backend/frontend，无需以拆后端服务来代替领域和事务边界设计。

## 实现前必须锁定的契约与模型

### ENGR-004：正式可执行契约先于实现

| 契约 | 必须覆盖的内容 | 对应责任与验收 |
|---|---|---|
| PostgreSQL / SQLite 数据模型 | 主外键、tenant scope、来源资金与 F/B/L/A 定义、唯一幂等键、快照不可变性、账本序号、单后继、引用、回收证据、迁移与保留策略 | BE-001～BE-004、HUB-012～HUB-017、MTR-004/015/016、LIM-004～010 |
| OpenAPI | 平台业务对象、角色/404/403、幂等请求、逐对象状态、限制/未知/争议原因、SSE 重连与全量刷新规则 | GEN-007、BE-005、SEC-003、FE-004/012 |
| Protobuf | apply/boot/generation/revision、请求 nonce 与时间绑定、授权快照集合、份额 phase 与 CAS 前代、签发方关闭证明、checkpoint/transfer、去重/ACK、错误与重连语义 | AGT/HUB、NFR-027～045 |
| 运行配置/授权 | 确定性编译、成员许可、内容摘要、路径修订/政策向量、独立期限、旧会话允许集合、Commit/Abort 绑定与执行事件 | ENG-024～039、MTR-016、附录 B.1/B.3/B.5 |
| VLink 编码 | 8 字节头与大小界、字段类型/长度、stream ID 分配、OPEN/UDP、未知帧处理、TLS exporter 绑定、流控、公平性、拒绝码 | ENG-010～023、040～048；M1 前不临时猜编码 |

每项必须交付文件、正反例、需求和 NFR 映射、评审记录与锁定版本。冻结需求正文不等于这些文件已交付。规格 8.2 中 `Commit(version)` 与 `UpdateGrants`、Events 的 apply/transition/receipt 关联方式尤其要前置解决，不能随实现擅自扩展公开接口或以 version 代替 boot/apply 身份。

### ENGR-006：实现前验证协议安全性

使用 TLA+ 或确定性模拟器，对节点、控制端、Hub 的状态分别建模，禁止用一个共享布尔变量代替跨进程事实。模型至少包含：发送前意图、提交后响应丢失、重复/乱序/迟到、单调时间与重启、分区与宕机、磁盘持久化失败、政策/套餐/来源边界、Abort/Commit 竞态，以及规定故障的两两组合。确切故障全集、探索界与覆盖判据须在模型计划显式列出，不能把下面摘要当作完整枚举。

必须验证逐来源 `F_s ≤ B_s + L_s`、新增委托不超过 `A_s`、份额交接占用 `max(a,b)` 且同时只有一个活跃代次、R 的唯一转接、消费持久化幂等、未知负债不无资金释放、旧代次不复活、计费快照隔离、激活未知持续占用。测试覆盖 NFR-027～NFR-045，并将状态转换映射到契约和后续实现测试。输出边界、种子/探索配置、状态数或覆盖证据和反例；有限回归脚本不替代完整模型。模型前置完成，M4 再按实际实现复核，不能把 M4 出口条件理解为可先跳过模型实现资金协议。[需求依据：ENGR-004/006、附录 B](../../docs/VORTEX%20需求规格说明书%20V1_11.md)

## 核心架构模式与数据流

### 1. 不可变编译结果和会话快照

后端从固定业务输入编译逐节点配置，并为平台执行点生成不可变 `billing_snapshot_id`，绑定 tenant/rule/执行点、递增且不复用的 `rule_path_revision`、有序组/角色、政策版本向量与有效区间、精确系数 k。内容变更产生新 ID；相同 k 不允许复用不同路径的授权。

配置应用流程：

```text
PG 事务：期望配置 + apply + 编译/快照引用 + Outbox
  → Hub 转发固定配置
  → Agent 持久化准备状态 → Engine.Prepare
  → 后端为合格候选预留 bootstrap / 引用既有份额
  → Hub 在合法委托内先持久化签发匹配快照的授权
  → Agent 校验运行/计费/份额及时间边界
  → 控制端持久化发送意图与 activation-unknown，再送 Commit 许可
  → Agent 持久化 Commit 意图 → Engine 幂等激活
  → Agent 持久化执行回执 → 上报 → 控制端 active
```

prepared 阶段无数据权限。首次资格依据 `desired_enabled`、权限、固定配置与 Prepare 成功，不等待 actual Commit，避免冷启动循环依赖。许可可能执行后丢回执，即使准备上限 120 秒已过也保持占用。取消需持久化撤销许可；仅有原子互斥的“从未激活且迟到 Commit 不可激活”栅栏证据，或全部数据权限/续租权与连接占用安全终结，才能回收。新 boot 不能证明旧 boot 未激活。[需求依据：AGT-010、LIM-008/009、B.3、NFR-042/045](../../docs/VORTEX%20需求规格说明书%20V1_11.md)

Commit 只原子切换新会话入口；hot/rebuild 的已有会话保留旧配置和原计费快照，权限收紧或 stop 按规则关闭。新旧快照分别取得授权并占用来源资金；回滚生成更高修订与新快照，不能恢复旧授权/拒绝栅栏或抹去已发生的负债。政策时点变化还要为每条仍存续路径生成对应新政策快照，不能把旧路径会话迁到新路径系数。

### 2. 单一数据管线、门控与计量对齐

```text
入口监听 / VLink OPEN
  → 本节点规则/证书身份/上游/跳位/防环检查
  → 必需门控和可信期限交集
  → 本地配置选择目标或下一跳
  → 执行点按会话快照预扣 + 令牌/连接控制
  → 双向 pipe 写入
  → 按实际写成功载荷提交，未写部分退回
  → 不可变待确认消费批次
```

执行点为首个平台节点，自托管入口的计量不作为可信依据；其他路径节点仍校验运行/段许可并报告原始计量，但不重复扣执行点的授权。全自托管路径依 MTR-011 豁免计费与份额，仍保留其他必要运行和安全约束。UDP 首包也必须先 OPEN 验证，再允许 DGRAM；整包预扣，失败整包退回。成功写入仅代表本地接口接受，不代表目标最终收妥。[需求依据：ENG-020～028、035～049](../../docs/VORTEX%20需求规格说明书%20V1_11.md)

相对租约使用请求起点、nonce、连接与 boot 绑定的单调截止；政策区间、ownership_until、运行依赖、自治硬截止为独立边界。不能把 ttl 先裁到绝对时点再扣余量，也不能因重收同一修订而延长权限。Engine 不负责网络校时：Agent 验证认证时间锚点后通过规定接口注入，误差界与重启处理由 B.1 契约锁定。

### 3. 分层持久化确认，不声称故障零字节丢失

| 边界 | 允许确认的时刻 | 丢失/失败处理 |
|---|---|---|
| Engine → Agent | 稳定 batch ID 的不可变批次已成功提交节点 SQLite 后，Agent 才调用 AckUsage | 未 ACK 批次重返；磁盘满/提交失败停止业务，旧会话也不能继续产出不可记录消费 |
| Agent → 在线 Hub → 后端 | Hub 先落盘，再获后端幂等入账确认，最后回 Agent | 重连重发同批次；不能只凭 gRPC 流成功发送删账 |
| Agent → 自治 Hub | Hub 本地账本提交后回 Agent | 留待恢复补报；未结清/争议记录不清理 |
| Hub → Agent 授权 | 来源预留、grant 与快照引用同一 SQLite 事务提交后发出 | 不确定本地提交结果先查唯一键，不再次生成额度 |
| Hub → 后端恢复 | 前缀及持续尾部幂等应用到明确 checkpoint K 后确认 | K 后继续接收；缺失部分 disputed，不能标全部完成 |

Engine 每秒拉取/Agent 落盘仍存在断电前未持久化窗口；恢复以每授权最后可信累计与剩余上界冻结不确定量。实测消费和未知上界分列，只有精确证据或获批足额平台资金按 B.2/MTR-015 处理后才能结清，不能因授权过期自动回收。

SQLite 配置应以 WAL + `synchronous=FULL` 作为签发/ACK 事务的设计起点，限制写者、缩短写事务，监测 checkpoint 和磁盘余量，再在正式持久化设置下压测。WAL 本身不等于断电持久：官方说明 NORMAL 会牺牲断电后的提交耐久性。还需验证驱动实际 SQLite 运行库包含 WAL-reset 修复：官方列出的修复版本为 3.51.3+ 或相应回补（3.44.6、3.50.7），仅锁 Go 驱动名不足以证明修复。存储、文件系统和同步失败行为仍须故障验证。[SQLite WAL 与 WAL-reset 说明](https://www.sqlite.org/wal.html)、[同步参数](https://www.sqlite.org/pragma.html#pragma_synchronous)

### 4. 逐来源事务与 Outbox

后端采用短业务事务：锁定租户相应资金来源/份额主体，验证预期 revision 和来源可用量，写业务变化、幂等结果与 Outbox 后提交；网络下发由事务外 worker 完成。来源账和快照账分开：快照标识计费语义，来源标识钱从哪里来，不能按快照丢弃或合并定点余数。唯一键建议覆盖业务事件 ID、消费 batch、委托、grant、transfer 和份额前代→后继关系，具体复合键在 ENGR-004 锁定。

多个任务 worker 可用 `FOR UPDATE SKIP LOCKED` 领取任务，配合处理租期、重试与幂等完成。余额/份额计算不能跳过被锁资金行后假定其不存在；采用稳定锁序的显式行锁或经过验证的可串行化事务方案。数据库锁解决的是事务并发，不能替代跨节点的持久化证明。[PostgreSQL 行锁](https://www.postgresql.org/docs/current/explicit-locking.html)、[SKIP LOCKED 用途和不一致视图限制](https://www.postgresql.org/docs/current/sql-select.html)

### 5. 份额链与资源安全交接

后端拥有 `(tenant_id,node_id,kind,boot_id)` 份额链及允许续租 Hub 集合；Hub 在该范围续租，Agent 持久化最低接受代次/CAS 意图，Engine 检查前代与 boot 并原子替换全部门控，带 transition_id 回执。交接顺序为：

1. 后端预留 `max(a,b)` 并生成唯一 prepared 后继；一条链同时最多一笔未终结交接。
2. 固定的全部旧签发方持久化关闭旧 revision 续租权；现有有效子租约不因此突然作废。
3. Agent 持久化 CAS 意图，Engine 原子切换；重复 transition 返回相同结果、不叠加额度。
4. 回执落盘与控制端确认后才能释放速率差额；连接下调还须实际并发降到新值且 enforced 回执持久化。
5. 丢回执保留最大占用并查询收敛；新节点/新 boot 走跨主体安全交接，不能借同主体 CAS 捷径。

自治只原值续同链，不跨节点再平衡。控制连接切换不是资源释放证明；允许新 Hub 签发也必须纳入链中签发集合并服从关闭栅栏。[需求依据：B.3、LIM-004/010、HUB-011/016](../../docs/VORTEX%20需求规格说明书%20V1_11.md)

### 6. Hub 恢复轮转与数据库恢复分开

Hub 在一个 SQLite 事务里封存旧 epoch 至 S、建立唯一 transfer_id 和后继 epoch，只转接同一 delegation 的未签发 R；旧在外授权 A 保持原预留。事务提交后后继可在原 R、原配置/份额链/ownership_until/自治硬截止内继续签发，旧授权消费写入后继尾部。禁止把轮转当成新 generation、新委托或关闭整份委托的证明。后端核对前缀与尾部至 K，逐来源保持 F 守恒；无法证明转接唯一性则冻结签发。[需求依据：HUB-013～017、B.4](../../docs/VORTEX%20需求规格说明书%20V1_11.md)

PG 恢复先基础备份与连续 WAL 物理恢复，再进入可写维护对账；此后逻辑补账不能再混放旧时间线 WAL。部署侧独立单调恢复标识分配新 generation，不能用备份 generation+1。先下达吊销/限制，未核清租户不得新委托或新所有权；已有 Hub 可在原冻结资源内继续续签。兑换事实只认 PG/WAL 或可信业务证据，Hub 的消费账不能重建丢失兑换。需要独立设计恢复标识分配器的存放、持久化、失效与操作流程，这是阶段深研项，当前没有实现保证。[需求依据：§12.4、SEC-010、NFR-023/024](../../docs/VORTEX%20需求规格说明书%20V1_11.md)

## 扩展与容量边界

以需求基线（200 节点、1,000 规则、2～5 Hub、单节点 10,000 空闲 TCP/5,000 UDP）设计和测量；不先承诺百万用户能力。吞吐与账本写入量比注册用户数更能决定容量。

| 压力源 | 首版处理方式 | 扩展触发与后续措施 |
|---|---|---|
| 数据面拷贝/加密/系统调用 | Engine 独立基准；splice、分级缓冲、批量 UDP、链路写合并和正式门控 | CPU/pps/延迟证据指明瓶颈后调优；加节点不能绕过同租户全局份额 |
| Hub 本地写入与 fsync | 本实例独立 WAL、短事务、按允许语义批量化与 checkpoint 监测 | 恢复滞后/写延迟持续上升才增 Hub；每实例单独委托和签发账不能共享总额度 |
| 后端 Outbox/入账/份额计算 | 单体内领域隔离、索引、批次幂等、任务分工、短锁 | 先定位热点租户/来源锁及重放性能，再按租户/实例分区任务；保持原子守恒边界 |
| 历史消费与快照增长 | 活跃引用可查、历史不可变、保留期与争议保留、索引/归档计划 | 不以 TTL 删除未结清账或仍被旧会话引用的快照 |
| SSE 状态与前端刷新 | 增量事件用于提示，重新查询权威状态；断线恢复明确 | 不依赖 Valkey 通知重建 apply/余额；高扇出时再优化推送 |

首先测数据面实际吞吐和门控成本，其次测每秒计量落盘、Hub ACK 与恢复追平；不能为了快而把落盘 ACK 提前或放松未知冻结。

## 反模式

| 反模式 | 为什么破坏需求 | 正确边界 |
|---|---|---|
| 把引擎塞进 Agent 内部并让引擎读数据库/拿授权 | 不能独立压测，数据面依赖控制可用性 | Engine 独立模块与接口；Agent 注入已校验输入 |
| 以 HTTP/RPC 超时、Commit 许可过期回收 bootstrap | 节点可能已经激活且旧续租仍有效 | activation-unknown 保留占用，查询并取得 B.3 终结证据 |
| 改规则时更新旧 billing_snapshot 行 | 历史消费和存量会话系数漂移 | 新 revision/新快照，新旧预留并存，按 grant 原快照结算 |
| Hub 失联后把剩余额度重新发给接管实例 | 不知道旧实例实际已发和消费，可能双发 | 旧来源负债冻结，新发只取逐来源 A_s |
| 把 Outbox 当成恰好一次网络投递 | 发送成功但确认丢失仍会重投 | 至少一次重试 + 接收端持久化幂等 + 稳定业务 ID |
| 只设 SQLite WAL，先 ACK 再异步落盘 | 断电可丢失已经确认的签发/消费事实 | 按确认边界耐久提交后应答，测同步失败与磁盘满 |
| Hub 封存 S 后把整份委托释放 | 后继仍合法消费 R，旧 A 仍未结算 | 唯一转接链 + 前缀/尾部 checkpoints + 来源守恒 |
| 自治重启重算 12 小时，恢复就变 normal | 延长旧权利并跳过负债核清 | 持久化原硬截止，recovering 保持冻结边界 |
| 用用户态 mock/禁门控压测代替 Linux 验收 | 不覆盖 splice、netns、半关闭、FD、tc 与真实记账成本 | 正式配置与真实 Linux 拓扑，绝对和相对阈值同时验 |

## 集成点与运营边界

| 边界 | 合同与集成方式 | 必须保留的事实 |
|---|---|---|
| 前端 ↔ 后端 | OpenAPI + SSE | 服务端状态、逐对象实际结果、未知/冻结/限制原因 |
| 后端 ↔ Hub ↔ Agent | gRPC/mTLS，稳定消息 ID 和连接绑定 | 版本、boot、允许签发集合、ACK 层级与恢复 checkpoint |
| Agent ↔ Engine | 规格 8.2 原有进程内接口 | 意图与执行事件的关联、稳定计量批次、实际代次 |
| Engine ↔ Engine | VLink/TLS 1.3 | 证书身份、boot/generation、段许可、帧界与流 ID |
| `dns` ↔ Cloudflare | 适配器；PG 期望 + Outbox 重试 + 实际集合对账 | 入口组成员与端口实际可服务集合，失败不能谎报解析已生效 |
| 备份/恢复 ↔ 部署 | PG 备份/WAL + 维护模式 + 单调恢复标识 + Hub 账本重放 | 目标 LSN、时间线、K、缺失实例、未知来源、实测 RPO/RTO |
| 发布 ↔ 升级 | 版本兼容矩阵、签名/摘要、排空、迁移与回退检查 | 不可逆迁移阻止自动回退，回滚不能撤销账务与安全栅栏 |

DNS/备份等业务语义来自需求；本次没有核验 Cloudflare API 具体字段或实际恢复工具，相关阶段须再查官方接口和操作契约。

## 构建顺序与验证门槛

### 工程前置（M0 业务实现之前）

先交付 ENGR-001 精确工具链/依赖/镜像版本、ENGR-004 全套契约及映射评审、ENGR-006 安全模型及规定故障覆盖。建立各组件独立构建与统一证据目录。本机 Docker 与 GitHub Actions 的环境能力只读核验后记录；本次研究没有运行这两端测试，也没有确认 runner 性能资源。

### M0～M6 必须保留的依赖关系

| 里程碑 | 先建什么及原因 | 出口验证与后续依赖 |
|---|---|---|
| M0 引擎数据面 | pipe、直接 TCP/UDP、计量/门控接口、模拟 Agent、真实拓扑基准；从一开始走规定接口 | 规格出口 NFR-001/005；Linux splice、半关闭和批次持久化接口夹具，为 M1/M2 留住实际数据路径 |
| M1 引擎链路 | 先按锁定 VLink 编码实现 TLS/HELLO、mux/pool、正反向、段许可 | NFR-002/004/007；帧边界与伪造 OPEN/UDP；不能在控制面出现后才发现承载协议不可测 |
| M2 引擎完备 | 多跳、调度/探测、正式预扣、耗尽、排空、两阶段与快照并存 | NFR-003/006/011；压测后评审 D-04 参数；Engine 在正式门控下达标后才进入完整控制面 |
| M3 node 与 Hub | 持久化协调、授权、账本、boot 安全交接、有限自治/恢复轮转、升级 | NFR-016～018；在已锁定契约下用后端模拟器驱动，测真实 SQLite 断电窗口与 ACK、磁盘满、丢回执 |
| M4 后端 | 业务领域、规则编译、应用/份额/来源委托、套餐、幂等结算与恢复核清 | NFR-012～015/019/021～023/026；ENGR-006 对照实现复核通过；专项 NFR-027～045 不可丢到“未来增强” |
| M5 前端与运营 | 中文状态/原因、DNS、通知/审计、备份恢复、空环境部署 | NFR-020/024/025 清单；真实恢复与限制反馈，保留 RPO/RTO 证据 |
| M6 全量验收 | 组合规模、正式网络性能、24 小时稳态、规定故障与恢复 | 第 13 节全部通过，产出版本/校验/SBOM/许可/测试与恢复报告；未运行项不通过 |

M3 的后端模拟器不是后端实现；M0/M1 的模拟授权不是正式授权安全验收。前置模型、协议约束和后续真实恢复各自提供不同证据，必须都完成。

### 本机 Docker 与 Actions 的同提交证据

两端复用仓库内统一测试入口与固定镜像 digest，分别保存实际 `git rev-parse HEAD`、脏工作区状态、镜像/工具链/模块依赖、平台架构、内核、CPU/内存/磁盘、拓扑、命令、随机种子、原始输出和阈值判定。只有实际源码 SHA 一致才能称同提交；相同分支名不充分。`pull_request` 默认 SHA 是 PR 合并提交，如本机测试的是 PR head，Actions 应显式 checkout 相同 head，或本机也测试同一 merge SHA，并分别记录，不混用结果。[GitHub 事件 SHA](https://docs.github.com/en/actions/reference/workflows-and-actions/events-that-trigger-workflows)、[checkout head 用法](https://github.com/actions/checkout#checkout-pull-request-head-commit-instead-of-merge-commit)、[Docker digest 固定](https://docs.docker.com/build/building/best-practices/)

常规矩阵覆盖静态检查、各模块独立/组合构建、单元/集成、race、泄漏、契约生成一致性、租户隔离、许可证与制品。race 结果用于正确性；性能另用正式优化构建，不以带 race 的数值冒充吞吐基线。

ENGR-005 的性能/泄漏回归必须在真实 Linux netns/veth/tc 网络执行，并具备建 namespace 和设置网络的所需权限。macOS 本机 Docker 结果应注明 Linux VM 内核与资源；普通容器单测不替代拓扑证据。tc netem 有内核定时粒度等限制，TCP 实验还应按官方说明考虑接收侧 ingress 放置，避免错误的排队位置失真。[iproute2 netem 手册](https://github.com/iproute2/iproute2/blob/main/man/man8/tc-netem.8)

性能脚本严格保存规格 13.2 的环境与样本：转发节点独享 4 vCPU/8 GiB、各段 ≥1 Gbps / RTT≤2 ms / 丢包≤0.01%、全路径不能共享一个 1 Gbps 总瓶颈；TCP 16 并发、预热 60 秒、采样 300 秒、三次中位数，正式门控和计量开启。验收同时判断绝对和同路径/协议/加密相对吞吐；UDP、首字节延迟、文件校验、反向差异、混合流公平性、FD/goroutine 释放各有独立结论。共享 CI 资源若不满足环境，报告“资源不足/未验收”，可配置符合要求的 Linux runner 重跑；绿色功能 CI 不等于性能通过。

24 小时验收需要 1 小时预热后的稳定负载和规定内存/泄漏窗口；故障矩阵涵盖节点/Hub/后端重启、分区、延迟/丢失回执、策略到点、时钟异常、恢复持续尾账和资金/份额安全终结。有限模型或较短 soak 都不能冒充 M6 全量通过。[需求依据：§13、ENGR-003/005/006](../../docs/VORTEX%20需求规格说明书%20V1_11.md)

## 待阶段研究与证据缺口

| 项目 | 何时解决 | 当前边界 |
|---|---|---|
| Commit(version)/UpdateGrants/Events 与 apply/transition/Abort 栅栏精确编码 | 前置 ENGR-004，M0 前 | 需求定义语义，当前无可直接复用的正式接口文件 |
| 安全模型完整故障全集、两两组合、实现映射与探索规模 | 前置 ENGR-006，M3/M4 持续回归 | 不声称有限条款算例已经证明系统安全 |
| 含门控/预扣的 splice 调用与成功字节计数、TLS/mux 的公平性 | M0～M2 深研 | 需求强制，具体实现与性能尚未验证 |
| SQLite 驱动实际运行库、耐久参数、checkpoint 与存储失效行为 | 版本基线、M3 | 官方能力不等于目标环境已经提供完整耐久保证 |
| PG 来源锁序/幂等键、decimal/整数上界与定点舍入 | ENGR-004、M4 | 事务划分建议需并发/溢出/恢复测试 |
| 部署侧独立单调恢复标识分配器、CA 与时间锚点灾难恢复 | ENGR-004、M3/M5 | 不以备份 generation+1 或重置时钟替代设计 |
| Docker/Actions 身份、Linux 权限、两种架构及独占性能资源 | 工程前置、M6 | 当前研究未运行或确认环境，不宣称测试已通过 |
| 冻结记录、历史评审附件与治理脚本缺失 | 需求治理独立追踪 | PROJECT.md 已指出仓库缺件；不伪造附件、不把历史编号当现成契约 |

## 来源与研究方法

所有外部页面在本研究日期访问；日期不等于页面发布日期。搜索结果的抓取时间仅作新鲜度参考，不能把抓取时间当发布事实。Go/SQLite/gRPC 等滚动文档须在版本锁定时重新对照具体版本。

| 来源 | 用途 | 置信度 |
|---|---|---|
| [V1.11 需求正文与附录 B](../../docs/VORTEX%20需求规格说明书%20V1_11.md)、[PROJECT.md](../PROJECT.md) | 产品约束、M0～M6、门控/授权/恢复语义 | 规范输入；不代表已实现验收 |
| [Go 模块参考](https://go.dev/ref/mod)、[多模块工作区](https://go.dev/doc/tutorial/workspaces) | 独立模块和工作区构建边界 | MEDIUM（seam 计算） |
| [SQLite WAL](https://www.sqlite.org/wal.html)、[synchronous](https://www.sqlite.org/pragma.html#pragma_synchronous) | 本地事务耐久、共享限制、checkpoint 和修复门槛 | MEDIUM（seam 计算） |
| [PostgreSQL locking](https://www.postgresql.org/docs/current/explicit-locking.html)、[SELECT](https://www.postgresql.org/docs/current/sql-select.html) | 行锁与任务领取；不能代替分布式安全证明 | MEDIUM（seam 计算） |
| [gRPC status](https://grpc.io/docs/guides/status-codes/)、[lifecycle](https://grpc.io/docs/what-is-grpc/core-concepts/) | 超时/取消后的未知执行事实 | MEDIUM（seam 计算） |
| [iproute2 tc-netem](https://github.com/iproute2/iproute2/blob/main/man/man8/tc-netem.8) | Linux 故障与性能实验限制 | MEDIUM（seam 计算） |
| [GitHub workflow events](https://docs.github.com/en/actions/reference/workflows-and-actions/events-that-trigger-workflows)、[actions/checkout](https://github.com/actions/checkout)、[Docker build practices](https://docs.docker.com/build/building/best-practices/) | 同提交和镜像可复现证据 | MEDIUM（seam 计算） |

研究按 `research-plan` 产生 6 个问题并完成 digest 缓存。计划为文档问题选择 Context7，但此运行时无该 MCP、无 ctx7 CLI，已按回退查官方网页；使用可用的浏览工具搜索和打开原始资料，缓存注明实际 `websearch` provider。原生 web 工具返回 404 后改用已暴露的网页连接器，未把失败当作完成查询。仅产出本文件，未提交 Git，未修改需求基线。
