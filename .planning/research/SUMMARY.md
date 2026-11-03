# Project Research Summary

**Project:** VORTEX  
**Domain:** 商业闭源、多租户 TCP / UDP 多跳转发与可信计量平台  
**Researched:** 2026-11-02  
**Confidence:** MEDIUM

## Executive Summary

VORTEX 是以自研数据面为核心的完整转发平台。V1.11 已规定首版功能、架构和验收，研究用于确定实施依赖，不重新裁剪 MVP。建议严格采用独立 Go Engine、单进程 Agent+Engine 的 vortex-node、有限自治 Edge Hub、PostgreSQL 模块化后端和中文 React SPA；后端与 Hub 不承载业务流量。四种转发方式及 TCP / UDP、套餐权益、自治恢复、DNS 和运营界面均属首版。

先交付 ENGR-001 版本基线、ENGR-004 五类正式契约及 ENGR-006 安全模型，再按 M0～M6 引擎优先推进。模型须在实现前建立，并随真实状态机持续复验，M4 完成规定退出验证。技术栈遵循规格；STACK.md 的精确版本仅是候选，不能视为锁定或兼容测试已通过。用户已批准以 MIT Vegeta 替代 k6，保留全部 API 压测目标；主流程交付正式变更记录，冻结 V1.11 原件不改写。

首要风险是把超时当作未执行、把到期当作已结清：这会造成首次激活重复占用、份额超发、恢复复制额度或未知负债无资金释放。以持久化意图、独立状态机、不可变快照、逐来源守恒和保守时间交集防范。当前没有已实现能力或已通过测试。主流程确认 Docker 可用，并核验用户指定 GitHub 应用连接身份 VortexWang 对 origin 为 push=true/admin=true。另一本机 gh 的 nodeox 凭据仅有读取权限，两者独立；其403不代表应用连接受阻。尚无项目工作流执行证据，Docker 可用也不证明原生双架构、专用性能资源或系统验收成立。历史冻结/评审附件及治理脚本缺失仍须明确追踪。

## Key Findings

### Recommended Stack

采用 [STACK.md](STACK.md) 的规格指定组合，保持 SQL 优先、控制面与数据面隔离。先验证组合，再冻结工具链、直接/传递依赖、生成器、锁文件、镜像 digest、许可证与 SBOM。

| 技术 | 职责与实施建议 |
|---|---|
| Go 标准库、TLS 1.3 | 独立 Engine、Linux splice、VLink；热路径不得引入数据库或控制 RPC |
| Gin、pgx/v5、sqlc、Goose、PostgreSQL | 后端模块化单体；显式事务、租户仓储、可靠任务与 Transactional Outbox |
| Valkey | 可丢失缓存、限流和通知；不保存余额、所有权或可靠任务事实 |
| SQLite WAL | Agent/Hub 本地耐久账；建议 modernc 驱动、FULL 同步、短事务，实测嵌入版本与每连接参数 |
| gRPC / Protobuf、mTLS | 控制双向流；固定编码、幂等 ID、ACK 层级及版本兼容，不承担业务流量 |
| React、严格 TypeScript、Vite、Ant Design | 中文 SPA；Router 管路由，Query 管服务端状态，Zustand 管本地交互，Zod 管边界校验 |
| Docker、Actions、Go race/goleak、Vitest、Playwright、Vegeta、iperf3/netns/tc | 统一入口、同提交双端证据；功能、正确性、原生运行和性能分别判定 |

关键候选包括 Go 1.27.3、PostgreSQL 18.6、modernc.org/sqlite v1.61.1（嵌入 SQLite 3.54.1）、Node 24.22.2、React 19.3.0、TypeScript 6.0.3 和 Vegeta v12.13.0。全部待 ENGR-001 集成、许可与构建验证；SQLite 必须包含 WAL-reset 修复并查询实际运行版本，不能只检查系统 CLI。完整清单与版本兼容下限见 STACK.md。

### Expected Features

[FEATURES.md](FEATURES.md) 将基础能力与核心特色分组，两组都必须首版交付。

**基础必需：** 租户隔离与账号限制、节点/组/段许可、端口事务、规则编译和两阶段应用、Agent 生命周期、套餐/流量包/权益码、中文用户及管理页面、真实 Cloudflare DNS、通知审计、安全与备份恢复。

**核心特色，同属强制：** 可独立压测 Engine；直接、正向、反向、多跳均支持 TCP/UDP；VLink mux/pool 无额外新流握手与 UDP 公平性；首个平台节点可信执行；不可变计费快照；写成功计量和预扣；逐来源额度/未知负债；安全份额 CAS 与连接排空；bootstrap 激活未知；12 小时上界自治及持续尾账恢复。全自托管仅豁免规定计费/份额约束，身份、运行授权及自治边界仍有效。

**后续版本：** 在线支付/自动续费/财务结算、团队邀请、完整 VTP 与伪装、QUIC/WebSocket 承载等规格明确延期项。用户上传引擎插件、跨地域数据库多活及强制 Kubernetes 不加入首版。

需求提取必须回到 V1.11，保留原编号、完整多行单元格、表外编号、NFR-025 十四项、非编号规范性列表和附录 A/B；本摘要不是逐条需求清单，也不补造缺失编号。

### Architecture Approach

[ARCHITECTURE.md](ARCHITECTURE.md) 的核心模式是分离期望、实际执行、授权签发与已确认消费，再以稳定 ID 和持久化证据收敛。

1. **Engine：** 独立模块，负责 socket、VLink、路由、门控、CAS、预扣及不可变批次；仅暴露规格 §8.2 接口，生产和模拟 Agent 共用。
2. **Agent：** 注册身份、可信时间、配置协调、意图/回执/拒绝栅栏、SQLite 落盘后 AckUsage、重放及升级。
3. **Edge Hub：** 运行/计费授权签发、本实例委托及账本、有界自治；唯一事务转接原 R，旧授权消费进入持续尾部，不复制委托。
4. **后端：** 唯一规则编译器、租户/资源/套餐、应用编排、份额链、逐源结算和恢复；PostgreSQL 保存权威事实，Outbox 支持至少一次投递及接收端幂等。
5. **中文前端与运营：** 展示五层状态、未知/限制/争议原因及影响；DNS 由实际 serving 集合驱动；物理恢复后维护对账，不直接依据旧余额重发。

先持久化再确认贯穿签发、计量 ACK、CAS 回执及恢复轮转。Engine 最后未落盘窗口不能宣称零损失，以未知上界冻结而非伪造实测消费处理。新旧会话与政策快照并存分别占款，回滚产生更高修订，不能复活旧授权。

### Critical Pitfalls

详见 [PITFALLS.md](PITFALLS.md)。优先防范五类问题：

1. **混淆时间边界：** 相对租约从请求单调起点计算且安全余量仅扣一次；政策、运行、所有权及自治截止独立求交，重启不得重算 12 小时。
2. **余额合并掩盖超发：** 每来源保持 F_s≤B_s+L_s，新发只用有效来源 A_s；L 仅保障旧承诺，未知负债无证明或足额获批预算不得释放。
3. **超时回收未知执行：** prepared、activation-unknown、active、released 分离；Commit/Abort 持久化互斥，份额交接保留 max(a,b)，连接差额排空后释放。
4. **恢复或配置变更改写历史：** grant/batch/final 绑定原不可变快照；Hub 唯一后继只移权不复制 R，前缀与持续尾账分别确认，原硬截止不延长。
5. **局部成功冒充验收：** WAL 不自动保证耐久，进程 kill 不等于掉电；有限模型不等于完整验证，绿色 CI 不等于规定硬件、真实 DNS/PG 恢复和 24 小时系统验收。

## Implications for Roadmap

建议采用 **1 个工程前置阶段 + 7 个 M0～M6 交付阶段**；GSD 可细分计划，但不能改序或消失退出门槛。

| 阶段 | 顺序理由与交付 | 主要能力/退出证据 | 必须避免 |
|---|---|---|---|
| 工程前置 | M0 实现前锁 ENGR-001、ENGR-004，建立 ENGR-006 模型及故障/两两组合覆盖、映射和评审；登记治理缺口与 Vegeta 变更 | 数据库、OpenAPI、Protobuf、运行配置、VLink 五类契约；版本/许可策略；同提交双端证据入口 | 用候选版本充锁定；用不存在附件充契约；有限算例充完整模型 |
| M0 引擎数据面 | 先验证最底层性能与计量可共存；独立 Engine+模拟 Agent，直接 TCP/UDP、splice、半关闭、门控与批次接口 | NFR-001/005；正式门控计量开启的 Linux 基准 | 连接包装破坏零拷贝；计读入量；引擎依赖数据库 |
| M1 引擎链路 | 在真实数据管道上实现锁定 VLink：TLS/HELLO/exporter、mux/pool、正反向、段许可与 UDP 开流 | NFR-002/004/007；帧/路径安全和两模式延迟；允许参数范围 | 伪造下一跳；DGRAM 绕 OPEN；新流增加等待往返 |
| M2 引擎完备 | 在链路之上完成多跳、调度、正式预扣、排空、两阶段应用及快照并存 | NFR-003/006/011 与 NFR-039/040/044/045 引擎侧证据；D-04 压测评审 | 不同快照混用额度；CAS 空窗；TCP 饿死 UDP；未经压测锁死参数 |
| M3 Agent / Hub | 引擎出口达标后接真实本地持久化；可用后端模拟器，交付身份、授权、重放、自治、轮转和升级 | NFR-016～018；SQLite 实际版本/PRAGMA；提交/ACK/回执边界、磁盘满、重启与硬截止试验 | 先响应后落盘；只停新会话；轮转复制 R；丢回执回收占用 |
| M4 后端业务闭环 | 接入真实 PG 权威业务和来源事务，替换模拟控制面；完成编译/应用、套餐、份额、结算、持续恢复 | NFR-012～015/019/021～023/026；专项 NFR-027～045 映射不遗漏；ENGR-006 对照实现完整退出验证 | 缓存存权威账；全局余额抵旧债；重发未知资源；RPC 当恰好一次 |
| M5 中文前端与运营 | 后端状态语义稳定后交付完整页面、SSE、DNS、通知审计、部署及真实物理备份恢复 | NFR-020/024/025；真实 DNS 增删改；PG 基础备份+WAL、维护对账与 RPO/RTO | 保存即生效；固定解冻承诺；mock 替真 DNS；逻辑 dump 替物理恢复 |
| M6 全量验收与交付 | 集成稳定后按第 13 节组合验证，产出源码/迁移/部署恢复资料、版本校验、许可/SBOM 与报告 | 200 节点/1,000 规则、规定资源及网络、原生双架构、24 小时稳定性、同提交双端原始日志 | 仿真替原生性能；短测替24小时；资源不足仍判通过；遗漏表外验收 |

### Phase Ordering Rationale

引擎性能、协议及执行安全是控制闭环的基础，因此 M0～M2 使用模拟 Agent，M3 使用后端模拟器；模拟结果不冒充真实持久化或业务验收。正式模型先于实现，再随 M2～M4 状态机持续校验。来源、快照、时间、未知激活和转接 ID 的契约必须前置，避免后期无法从已有记录重建安全证明。

治理缺口独立跟踪，不能靠删减需求解决。最终验收环境应在工程前置阶段就预留；M6 负责组合证明，不承担首次发现测试设施缺失的风险。

### Research Flags

规划时建议使用 `$gsd-plan-phase --research-phase <N>`，N 为实际 GSD 阶段号：

- **工程前置：** 完整安全模型边界/两两故障覆盖、五类编码契约、许可政策及版本组合需深研。
- **M0～M2：** splice+预扣/限速、TLS exporter、自研 mux/pool、UDP 公平性、政策切换与 CAS 需要专项原型和性能证据。
- **M3：** SQLite 连接初始化、FULL fsync 预算、真实存储故障及未知回执恢复必须深研。
- **M4：** 来源锁序/隔离、幂等唯一键、定点舍入溢出、流式追账及模型实现映射需深研。
- **M5：** Cloudflare 实际接口、PG 时间线/WAL 缺口、独立单调恢复标识及密钥灾难恢复需深研。
- **M6：** 规定网络/独占资源和双架构测试拓扑先验证可用性，不能从托管 runner 标签推断满足性能基线。

**标准模式可跳过额外通用调研：** Gin 路由、sqlc/Goose 常规接入、React 中文表单和列表、Query/Zustand 职责、普通 Compose/systemd 与 Outbox 基础结构已有成熟资料；直接按锁定版本实施。涉及资源安全、耐久和未知状态的集成仍保留上述专项研究，不能整阶段免研。

## Confidence Assessment

| Area | Confidence | Notes |
|---|---|---|
| Stack | MEDIUM | 官方发布/元数据支撑候选，未安装、构建或完成全依赖许可审查 |
| Features | HIGH（范围）；LOW（工具分类） | 用户指定 V1.11 可直接定位，分类器不识别本地来源；复杂度和拆分仍为判断，非市场验证 |
| Architecture | MEDIUM | 规范明确边界，官方资料支撑机制；具体契约、并发和性能未被实现证明 |
| Pitfalls | MEDIUM | 从规范安全不变量及官方耐久/恢复资料推导；无项目故障试验结果 |

**Overall confidence:** MEDIUM。需求范围明确，实施可行性和验收达标仍须证明。

### Gaps to Address

- **历史治理证据：** 冻结记录、修订评审附件和检查脚本当前缺失；登记真实材料、来源与核验结果，NFR-038 不预判通过，不补造评审人/日期/哈希。
- **正式契约与完整模型：** 当前研究不是 ENGR-004/006 交付；保留状态覆盖、探索边界、反例及实现映射证据。
- **版本与许可：** 候选组合、生成器、镜像、传递依赖及系统包待 ENGR-001/002 核验；Vegeta 正式变更由主流程记录。源码交付与闭源分发分别明确接收方和用途。
- **环境与实测：** Docker 已可用；VortexWang 应用连接身份和 origin 写入/管理权限已验证。CLI nodeox 凭据独立，本机 Git 推送凭据链须另验，不把两种连接混为一谈。项目工作流与测试尚未运行；原生架构、netns/tc 权限、实际 SQLite 版本、专用压测硬件与真实浏览器矩阵逐项留证。
- **持久化与灾难恢复：** SQLite 驱动负载/掉电行为、PG 来源事务、恢复 generation 分配器、CA/时间锚点恢复及真实 Cloudflare 测试域在相关阶段解决。
- **待评审参数与运营预算：** D-04 参数按 M2 压测确认；风险自动预算默认 0，审批和资金不足时继续冻结，不承诺固定期限解冻。

## Sources

### Primary（规范性来源）

- [V1.11 需求规格](../../docs/VORTEX%20需求规格说明书%20V1_11.md)：首版范围、§3.3 栈、§8～12 安全/计量、§13 验收、§14 顺序及附录 A/B；冻结声明不代替治理证据。
- [PROJECT.md](../PROJECT.md)：项目目标、约束和治理缺口；环境可用状态以本次主流程核验为准，研究文件中的早期“未验证环境”描述不作为失败结论。
- [STACK.md](STACK.md)、[FEATURES.md](FEATURES.md)、[ARCHITECTURE.md](ARCHITECTURE.md)、[PITFALLS.md](PITFALLS.md)：本摘要整合的四份研究，保留详细编号、版本与来源。

### Official technical sources（外部结论 MEDIUM，沿用研究提供方等级）

- [Go 模块](https://go.dev/ref/mod)、[单调时间](https://pkg.go.dev/time#hdr-Monotonic_Clocks)、[race 要求](https://go.dev/doc/articles/race_detector#Requirements)：模块隔离、重启时间及独立正确性测试。
- [SQLite WAL](https://sqlite.org/wal.html)、[同步设置](https://sqlite.org/pragma.html#pragma_synchronous)、[3.51.3 修复](https://sqlite.org/releaselog/3_51_3.html)、[modernc 驱动](https://pkg.go.dev/modernc.org/sqlite)：确认耐久边界、修复门槛及运行库验证。
- [PostgreSQL 行锁](https://www.postgresql.org/docs/current/explicit-locking.html)、[SELECT / SKIP LOCKED](https://www.postgresql.org/docs/current/sql-select.html)、[PG18 PITR](https://www.postgresql.org/docs/18/continuous-archiving.html)：资金事务、任务领取与物理恢复。
- [gRPC 状态](https://grpc.io/docs/guides/status-codes/)、[调用生命周期](https://grpc.io/docs/what-is-grpc/core-concepts/)：RPC 结果与实际执行不可等同。
- [Docker 多平台](https://docs.docker.com/build/building/multi-platform/)、[构建实践](https://docs.docker.com/build/building/best-practices/)、[GitHub runner](https://docs.github.com/en/actions/reference/runners/github-hosted-runners)、[事件 SHA](https://docs.github.com/en/actions/reference/workflows-and-actions/events-that-trigger-workflows)、[netem](https://github.com/iproute2/iproute2/blob/main/man/man8/tc-netem.8)：同提交、镜像及真实 Linux 证据。
- [Vegeta MIT](https://github.com/tsenart/vegeta/blob/master/LICENSE)、[k6](https://github.com/grafana/k6)：已获批准的压测工具替换依据；完整版本与前端包来源见 STACK.md。

不以社区传闻或竞品功能扩张首版；本摘要未新增安装、模型运行或测试通过声明。

---
*Research completed: 2026-11-02*  
*Ready for roadmap: yes — 工程前置和证据缺口须进入路线图，非已通过实施门槛。*
