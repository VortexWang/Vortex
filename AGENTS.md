<!-- GSD:project-start source:PROJECT.md -->

## Project

**VORTEX**

VORTEX 是商业级、多租户、多节点、支持多跳且可私有部署的 TCP / UDP 流量转发与管理平台，服务于平台运营者及使用转发服务的租户。自研转发引擎是核心，Agent、Edge Hub、后端及中文管理界面共同提供节点部署、规则编排、套餐权益、可信计量及故障恢复的业务闭环。

本项目重启后以当前项目决策和阶段验收为准。`docs/VORTEX 需求规格说明书 V1_11.md` 仅作需求参考，不是冻结验收依据；保留有用的需求编号与语义，按 GSD 阶段讨论、规划、实施、验证和里程碑审计持续开发。

**Core Value:** 在租户隔离、额度与租约安全边界内可靠转发 TCP / UDP 业务流量，并在正常运行及故障恢复中保持计量可信、权利不重复签发、未知负债不被无资金释放。

### Constraints

- **需求基线**: V1.11 仅供参考，不是冻结验收依据；重启后要求或验收边界的调整记录于项目决策与 GSD 阶段文档。
- **架构边界**: 后端与 Edge Hub 不承载业务流量；Engine 独立 Go 模块，不访问数据库或主动调用控制面；Agent 与 Engine 仅通过规定的进程内接口交互。
- **技术栈**: 按规格 3.3 使用 Go、Gin、pgx/v5、sqlc、Goose、PostgreSQL、Valkey、SQLite WAL、gRPC / Protobuf、TLS 1.3；前端使用 React、严格 TypeScript、Vite、Ant Design、CSS Modules、React Router、TanStack Query、Zustand、Zod；精确版本待工程基线锁定。
- **持久化**: PostgreSQL 是业务与期望配置唯一事实来源；Valkey 只作非权威缓存等用途；可靠任务使用 PostgreSQL 任务表和 Transactional Outbox。
- **安全与记账**: mTLS、租户隔离、保守时间边界、不可变计费快照、逐来源额度守恒及未知负债冻结遵循正文与附录 B；未确认执行不能视为停止或未激活。
- **部署与兼容**: Linux amd64 / arm64，标准验收系统 Ubuntu 24.04 LTS；后端和前端使用 Docker Compose，节点与 Hub 提供二进制、systemd 和容器镜像。
- **版本与许可**: 产品源码、链接依赖及随 VORTEX 分发的镜像/工具锁定版本并审查许可；PG 与 Valkey 作为外部数据服务，不纳入产品依赖许可门禁，但记录版本、来源与镜像摘要。
- **验证证据**: 相同提交的本机 Docker 与 GitHub Actions 结果分别记录；未运行、受资源限制或失败的检查保持未通过，不将有限模型算例当作完整模型验证或系统验收。

<!-- GSD:project-end -->

<!-- GSD:stack-start source:research/STACK.md -->

## Technology Stack

## 结论与基线状态

## 推荐核心技术

### Go、控制通信与数据库

| 技术 | 精确候选 | 用途与选型理由 | 来源 / 验证边界 |
| --- | --- | --- | --- |
| Go | **1.27.3** | Engine、Node、Hub、后端统一工具链；标准库提供 net、tls、x509、slog、pprof，避免额外数据面框架；该补丁包含 x509 安全修复 | [官方发布历史](https://go.dev/doc/devel/release#go1.27.0)；仅确认发布，不证明 VORTEX 性能 |
| Gin | **v1.12.0** | 仅 HTTP 接入、认证上下文、请求解析、REST / SSE；业务规则放领域服务 | [官方发布](https://github.com/gin-gonic/gin/releases/tag/v1.12.0)；不启用其 HTTP/3 能力作为 VLink |
| pgx/v5 | **v5.11.0** | PostgreSQL 原生驱动、连接池和事务；2026-11-03 复核时 GitHub 正式 release 与 Go module proxy 均未发现 v5.12.0，采用 v5.11.0 作为最新已发布稳定候选 | [官方 latest release](https://api.github.com/repos/jackc/pgx/releases/latest)；[精确 Go module sum](https://sum.golang.org/lookup/github.com/jackc/pgx/v5@v5.11.0) |
| sqlc | **v1.31.1** | 从显式 SQL 生成类型安全数据访问，配置 `sql_package: pgx/v5`；生成代码仍须经 tenantscope 封装 | [官方发布](https://github.com/sqlc-dev/sqlc/releases/tag/v1.31.1)、[pgx 指南](https://docs.sqlc.dev/en/stable/guides/using-go-and-pgx.html) |
| Goose | **v3.29.0** | 纳入版本控制的 PostgreSQL / SQLite SQL 迁移；迁移工具与应用进程分开执行，防止多实例并发迁移 | [官方发布](https://github.com/pressly/goose/releases/tag/v3.29.0)；使用所需驱动裁剪构建，避免 CLI 全驱动依赖膨胀 |
| PostgreSQL | **18.6** | 事务、唯一约束、任务表、Outbox、计费账本和期望状态唯一事实来源；支持外置部署 | [官方支持表](https://www.postgresql.org/support/versioning/) 列出 18.6、支持至 2030-11；研究时 19 仍显示 RC，不采用 RC |
| Valkey | **9.1.3** | 缓存、登录限流、短期通知；清空后能从权威状态重建 | [官方版本页](https://valkey.io/download/9.1/)；不保存余额、份额所有权、可靠任务确认 |
| SQLite Go 驱动 | **modernc.org/sqlite v1.61.1** | 推荐纯 Go 驱动，简化 Linux amd64 / arm64 单二进制和静态镜像；只允许 Agent / Hub 导入，Engine 不接数据库 | [驱动文档](https://pkg.go.dev/modernc.org/sqlite)、[精确 go.mod](https://proxy.golang.org/modernc.org/sqlite/@v/v1.61.1.mod)；要求 Go ≥1.26，依赖 libc v1.79.0 |
| SQLite 内嵌库 | **3.54.1，随上述驱动** | 锁定真正执行的 SQLite 核心，覆盖 WAL-reset 修复；不是额外依赖系统 libsqlite3 | 已读取 [v1.61.1 官方代理源包](https://proxy.golang.org/modernc.org/sqlite/@v/v1.61.1.zip) 的 doc.go，linux/amd64、linux/arm64 均列 3.54.1；上线须实测查询版本 |
| grpc-go | **v1.85.0** | 后端 ↔ Hub ↔ Agent 的 mTLS 双向流；不承载业务流量 | [官方发布](https://github.com/grpc/grpc-go/releases/tag/v1.85.0) |
| Go Protobuf runtime / protoc-gen-go | **v1.36.12 / v1.36.12** | 显式固定运行库和生成器；采用 google.golang.org/protobuf | [官方发布](https://github.com/protocolbuffers/protobuf-go/releases)；不采用 1.37 RC |
| protoc / protoc-gen-go-grpc | **36.2 / v1.6.2** | 契约代码生成，独立记录版本与校验值 | [protoc 发布](https://github.com/protocolbuffers/protobuf/releases)、[Go gRPC 生成器发布](https://github.com/grpc/grpc-go/releases)；生成代码与 runtime 兼容性须实际编译 |
| TLS 1.3、mTLS、slog | **随 Go 1.27.3** | 使用标准库，不叠加第三方 TLS 栈；VLink 明确最低/最高协议策略、证书身份与吊销校验 | [crypto/tls](https://pkg.go.dev/crypto/tls)；普通 TLS 握手不自动满足 VLink 的 boot/generation、exporter 绑定要求 |

### 前端

| 技术 | 精确候选 | 职责与理由 | 来源 |
| --- | --- | --- | --- |
| Node.js | **26.13.0 LTS** | 构建、类型检查、测试的统一运行时；锁定附带 npm 的版本 | [官方 release index](https://nodejs.org/dist/index.json)；Linux amd64/arm64 SHA-256 见 version-candidates |
| React / react-dom | **19.3.1 / 19.3.1** | SPA 渲染；两者精确版本保持相同 | [React package metadata](https://registry.npmjs.org/react/19.3.1)、[react-dom package metadata](https://registry.npmjs.org/react-dom/19.3.1) |
| TypeScript | **7.0.2** | 最新稳定编译器候选；是否与 Vite/Vitest 插件、配置及类型包兼容须构建验证后再批准 | [官方发布](https://github.com/microsoft/TypeScript/releases)、[精确包 integrity/license](https://registry.npmjs.org/typescript/7.0.2) |
| Vite / React 插件 | **8.3.5 / @vitejs/plugin-react 6.2.1** | 构建、开发和 CSS Modules；采用匹配 Vite 8 的插件 | [Vite 发布](https://github.com/vitejs/vite/releases)、[插件元数据](https://registry.npmjs.org/@vitejs/plugin-react/6.2.1) |
| Ant Design / icons | **antd 6.6.8 / @ant-design/icons 6.3.4** | 中文管理界面、主题 Token、Form；不套用额外后台框架 | [官方入门](https://ant.design/docs/react/getting-started/)、[迁移兼容说明](https://ant.design/docs/react/migration-v6/)、[antd 元数据](https://registry.npmjs.org/antd/6.6.8) |
| React Router | **8.5.1** | 按角色组织 SPA 路由；后端仍独立执行权限 | [官方发布](https://github.com/remix-run/react-router/releases)、[精确元数据](https://registry.npmjs.org/react-router/8.5.1) |
| TanStack Query | **@tanstack/react-query 5.105.3** | API 读取、mutation、缓存失效、SSE 触发重取；不把账本真实状态只留在浏览器缓存 | [官方发布](https://github.com/TanStack/query/releases)、[精确元数据](https://registry.npmjs.org/@tanstack/react-query/5.105.3)；不要误用同仓其他框架 6.x 版本 |
| Zustand | **5.0.16** | 本地交互状态，如编辑器草稿、侧栏、筛选偏好；不复制 Query 中的服务器实体 | [官方发布](https://github.com/pmndrs/zustand/releases) |
| Zod | **4.6.7** | 表单输入、接口边界校验；和 Ant Design Form 适配 | [官方发布](https://github.com/colinhacks/zod/releases) |
| CSS Modules | **随 Vite 8.3.5** | 页面局部样式；共用颜色/间距使用主题变量 | [Vite CSS 文档](https://vite.dev/guide/features.html#css-modules) |

### 支持库与开发工具

| 工具 / 库 | 候选版本或锁定要求 | 使用范围 / 原因 |
| --- | --- | --- |
| Prometheus Go client | **v1.25.0** | [官方发布](https://github.com/prometheus/client_golang/releases)；暴露规格指标，避免 tenant/rule/session 等无界标签；业务查询不能依赖采样指标 |
| OpenTelemetry Go API / SDK | **v1.47.0** | [官方发布](https://github.com/open-telemetry/opentelemetry-go/releases)；控制面请求和编排链路追踪；不同信号模块版本分别锁定，不把同一发布组的 0.x 当成 1.x；不对每包生成 span |
| golang.org/x/sys | **v0.49.0 候选** | 与 SQLite v1.61.1 go.mod 对齐；Engine 如需 Linux syscall、splice 才引入并再次审查依赖边界 |
| Go testing / race / pprof | **随 Go 1.27.3** | 独立 Engine、集成、竞态和性能分析；race 产物与正式压测产物分开 |
| go.uber.org/goleak | **v1.3.0** | [官方发布](https://github.com/uber-go/goleak/releases)；会话、链路和监听关闭后检测 goroutine 泄漏，结合 FD/连接实际计数 |
| Vitest | **5.1.0** | [官方发布](https://github.com/vitest-dev/vitest/releases)、[engines/peer 元数据](https://registry.npmjs.org/vitest/5.1.0)；支持 Vite 8；Node 26 支持仍须实测，coverage 插件保持同版本 |
| React Testing Library | **16.3.3** | [官方发布](https://github.com/testing-library/react-testing-library/releases)；验证用户可见行为；DOM / user-event 等直接依赖仍须精确锁定 |
| Playwright | **1.64.1** | [官方发布](https://github.com/microsoft/playwright/releases)；锁定对应浏览器下载与测试镜像；Chromium / Firefox / WebKit 回归不等同于真实 Safari / Edge 全部验收 |
| Vegeta | **v12.13.0 候选；用户已批准替代 k6** | API 定速负载、吞吐/延迟与状态码结果；[官方发布](https://github.com/tsenart/vegeta/releases/tag/v12.13.0)、[MIT LICENSE](https://github.com/tsenart/vegeta/blob/master/LICENSE)。身份令牌准备、业务串联和幂等断言由测试脚本配合，不假定它直接执行 k6 脚本 |
| iperf3、iproute2 / tc、strace、文件校验与 UDP 序列工具 | **在 Ubuntu 24.04 测试镜像中按包版本、镜像 digest 锁定** | 保留同协议同加密最小基准、网络拓扑和 splice 证据；测试工具不随业务镜像打包；许可范围仍须明确 |
| Docker Engine / Compose v2 / Buildx | **按本机与 CI 实测输出锁定具体版本** | 本轮未查询/验证宿主安装状态，不捏造已安装版本；优先在固定测试镜像内复用命令，记录宿主差异 |
| GitHub Actions | **每个 action 固定完整 commit SHA** | runner 标签选 ubuntu-24.04 / ubuntu-24.04-arm，避免 ubuntu-latest；标签对应的宿主镜像仍会更新，记录 image version |
| SBOM / 漏洞 / 许可工具 | **工程基线阶段确定并锁定版本** | 建议 Syft 生成二进制/镜像 SBOM，govulncheck 检查 Go；另检查 npm 与镜像系统包许可证。工具加入前先核实其精确版本、许可和双架构制品 |

## SQLite 驱动与持久化决策

| 项目 | 推荐约束 / 验证 |
| --- | --- |
| 断电安全 | `journal_mode=WAL`、`synchronous=FULL`；签发响应和 AckUsage 前等事务成功。FULL 的额外同步成本计入每秒批处理与 Hub 签发延迟预算 |
| 连接策略 | 单进程单写入队列，写事务短且有界；读池有上限；每连接明确 foreign_keys / busy_timeout / synchronous 设置；不以无上限重试掩盖磁盘故障 |
| 文件系统 | 使用本地磁盘和容器 named volume；WAL 不放 NFS / 跨机器共享目录；本机压测数据优先放 Linux VM 内 volume，而非 macOS 文件共享路径 |
| WAL 运维 | 监测 WAL 大小、检查点耗时、写入队列深度、磁盘剩余、fsync 延迟；备份使用一致性快照/备份流程，不只复制运行中的单个 db 文件 |
| WAL-reset 修复 | SQLite 官方列 3.7.0～3.51.2 受影响，3.51.3+ 或明确回补版本修复；候选嵌入 3.54.1。构建后在两架构输出 `sqlite_version()`、compile_options，并加入基线证据 |
| 故障试验 | 提交前后 kill、磁盘满、只读盘、检查点与写并发、重复批次、授权响应丢失、轮转事务崩溃；确认已 ACK 不丢、未 ACK 重试稳定、未知上界持续冻结 |
| 停止语义 | 写失败必须阻止继续产生无法记录的消费；仅停止新会话不满足 AGT-006 |

## Linux 双架构与双宿主验证

### 构建与运行矩阵

| 环境 | 应执行的验证 | 不能据此声称的结果 |
| --- | --- | --- |
| macOS 原生 | 编辑、类型检查及辅助单测 | Darwin 不提供 Linux splice / netns / tc 语义，不能替代 Engine 验收 |
| macOS + OrbStack Docker，宿主原生架构 | 固定 Ubuntu 24.04 测试容器，运行 Compose 集成、权限隔离、Linux 拓扑、故障与泄漏回归 | VM 资源和存储/网络必须记录；不能将 Mac 虚拟网络结果直接写为规格独享硬件性能 |
| OrbStack 外来架构仿真 | 构建与基本启动 smoke test | amd64 仿真不能当原生 amd64 吞吐、延迟、race、稳定性证据 |
| GitHub Actions ubuntu-24.04 amd64 | 同一提交相同入口脚本、原生测试、race、集成和网络拓扑回归 | 托管 runner 动态资源/网络不自动满足 §13.2 的专用压测环境 |
| GitHub Actions ubuntu-24.04-arm | 原生 arm64 构建、运行、账本/协议/race 回归 | 交叉编译成功不等于在目标 CPU 上执行成功；runner 可用性须按当前仓库配额/权限核实 |
| 专用 Ubuntu 24.04 Linux 压测机，原生架构 | 指定 4 vCPU / 8 GiB、独立链路条件、正式门控计量下的性能与 24 小时验收，可由 Actions 自托管 runner 调度 | 没有符合条件硬件或测试未执行时，NFR 保持未通过 |

### 可复现证据约定

## 许可与商业发布风险

| 对象 | 已核实事实 / 推荐处理 |
| --- | --- |
| Go / Gin / pgx / sqlc / Goose / gRPC / Protobuf | 核心项目采用 BSD / MIT / Apache 等宽松许可证的候选路线；正式放行仍检查精确版本 LICENSE / NOTICE 和完整传递图，不把根项目许可证当全图结论 |
| 前端直接依赖 | 查询的 React、Ant Design、Vite、Router、Query、Zustand、Zod、Vitest npm 元数据标 MIT；Playwright 标 Apache-2.0。类型包、原生 bundler、浏览器及其他传递依赖另外审查 |
| PostgreSQL / Valkey / SQLite | PostgreSQL 与 Valkey 按项目所有者决定作为外部数据服务，服务镜像 OS 包许可不纳入 VORTEX 产品依赖门禁；仍记录版本、来源、镜像摘要与集成兼容性。SQLite 驱动及扩展作为 VORTEX 链接依赖单独审查许可证 |
| k6 → Vegeta 已获用户批准 | [上游 k6](https://github.com/grafana/k6) 明确 AGPL-3.0，与原 §14.3 / ENGR-002 的组合形成冲突；用户已明确批准采用 [MIT Vegeta](https://github.com/tsenart/vegeta/blob/master/LICENSE) 替代。研究据此采用 Vegeta，保持全部 API 压测目标；原 V1.11 不覆写，主流程补充需求变更记录。Vegeta 精确版本、传递依赖与镜像仍须工程基线审查 |
| 系统与镜像 | Ubuntu、Debian、Alpine 镜像和网络测试工具可能含 GPL/LGPL 系统组件；只检查 go.mod / npm 不足以证明镜像合规。分别盘点构建环境、内部测试、运行制品和分发镜像，按最终明确的政策判定，禁止将“非链接工具”自动当合规 |
| OrbStack | [官方条款](https://docs.orbstack.dev/legal/terms) 对商业/专业使用要求购买相应许可；它是开发宿主工具，不随 VORTEX 发布；当前是否已持有授权未核实 |
| 发布输出 | 每个架构二进制和镜像分别生成 SBOM、许可证清单、校验值；保留必要版权/NOTICE，许可证扫描异常阻断发布，不能仅凭顶层 permissive 名称放行 |

## 版本兼容与锁定步骤

| 组合 | 已检查的条件 | 锁定前必须执行 |
| --- | --- | --- |
| Go 1.27.3 + SQLite v1.61.1 + pgx v5.11.0 | Go 官方 release 与 Go checksum database 已验证版本摘要；SQLite/pgx module sums 已查询 | 双架构 `go mod graph`、构建、数据库事务与崩溃测试；检查 MVS 是否改写 libc 或其他关键依赖 |
| Node 26.13.0 + Vite 8.3.5 + React 插件 6.2.1 + Vitest 5.1.0 | 最新 Node LTS 已确认；Node 26 与工具 peer/engine 组合仍待验证 | 锁定后执行精确安装、peer 冲突检查、类型检查、测试与 production build；未经用户批准不安装 |
| React/DOM 19.3.1 + Router 8.5.1 + antd 6.6.8 + TypeScript 7.0.2 | npm 官方 registry 已给出精确 versions、integrity 和 license；TypeScript 7 兼容性未验证 | 真实页面渲染、Form、Modal、中文 locale、SSE 重连、路由权限和完整 TS 构建回归 |
| protobuf runtime、两个生成器、protoc | 所选均为正式版，版本号体系互不等同 | 生成后无差异校验、后端/Hub/Agent 编译、双向流兼容与升级/降级测试 |
| PG 18.6 + sqlc + Goose + pgx | 推荐 SQL 优先组合；未验证全部 DDL/SQL 特性支持 | 真实 PG 建库、迁移、sqlc generate、并发计费事务与物理恢复演练 |
| Playwright 1.64.1 + 浏览器 | 测试库版本已确认 | 对应浏览器和镜像精确匹配；规格要求真实 Chrome/Edge/Safari 当前及前一主版本的覆盖另列证据 |

### 供基线验证使用的安装示例

# backend 模块

# node / edge-hub 各自模块

# 统一工具环境；其他生成器和镜像同样显式锁定

# frontend；辅助类型、DOM 环境等依赖另行核实后锁定

## 不采用的替代技术

| 不采用 | 理由 | 本项目处理 |
| --- | --- | --- |
| Redis 替换 Valkey | 规格指定 Valkey，替换还引入不同版本许可判断 | 固定 Valkey 正式版；缓存可丢失 |
| GORM / 隐式租户 ORM scope | 不符合 pgx + sqlc 的 SQL 优先基线，隐藏查询不利于租户审查 | sqlc 生成查询 + tenantscope + CI 检查 |
| Kafka / RabbitMQ / Redis Streams 作为可靠任务事实源 | 规格已确定 PG 任务表与 Transactional Outbox | 同一 PG 事务持久化业务变更与 Outbox |
| 现成代理核心、QUIC、WebSocket、HTTP/3 数据承载 | 自研独立 Engine 和 VLink TLS 1.3 是强制范围，其他承载属于后续版本 | net / tls + 自研 VLink；不因依赖框架支持就扩展范围 |
| io.Copy 加计量包装连接就认为满足零拷贝 | 封装可能破坏 TCP 特化路径，且无法直接表达规定的预扣/实际写入提交/短写边界 | Linux 专用有界 splice 路径，结合 syscall 证据、半关闭和预扣测试；先 M0 验证 |
| Next.js / Ant Design Pro / Redux 重新组织栈 | 规格是 Vite SPA + Router + Query + Zustand；额外框架增加运行面和版本组合 | 保持指定职责分工 |
| 漂浮 latest、只打单架构镜像、用 QEMU 证明性能 | 无法满足版本锁定、双架构和原生性能证据要求 | 精确版本/digest、原生测试和专用压测 |

## 路线图应保留的技术研究门槛

- **工程前置：** 许可证范围、完整版本/SBOM、生成器、镜像 digest、双宿主入口必须先成形。候选版本数量多，不应把“查到版本”当集成已完成。
- **M0：** splice 与正式预扣/限速/半关闭的组合是最早技术风险；用 Linux 实验和真实计量压测验证后再扩展 VLink。
- **M1～M2：** 自研 mux/pool、TLS exporter、UDP 调度和版本切换不可交给普通 gRPC/HTTP 框架代替；工具链升级后保留性能对照。
- **M3：** SQLite 驱动、FULL fsync 延迟、磁盘满停止、账本轮转和两架构原生恢复演练必须专项验证；必要时再评审 cgo 驱动替代。
- **M4～M6：** 真实 PostgreSQL 事务与物理 WAL 恢复、外置数据库版本兼容、镜像系统许可、专用压测硬件及真实浏览器证据不可延后到发布当天。

## 尚未得到证据的事项

<!-- GSD:stack-end -->

<!-- GSD:conventions-start source:CONVENTIONS.md -->

## Conventions

Conventions not yet established. Will populate as patterns emerge during development.
<!-- GSD:conventions-end -->

<!-- GSD:architecture-start source:ARCHITECTURE.md -->

## Architecture

Architecture not yet mapped. Follow existing patterns found in the codebase.
<!-- GSD:architecture-end -->

<!-- GSD:skills-start source:skills/ -->

## Project Skills

No project skills found. Add skills to any of: `.claude/skills/`, `.agents/skills/`, `.cursor/skills/`, `.github/skills/`, or `.codex/skills/` with a `SKILL.md` index file.
<!-- GSD:skills-end -->

<!-- GSD:workflow-start source:GSD defaults -->

## GSD Workflow Enforcement

Before using Edit, Write, or other file-changing tools, start work through a GSD command so planning artifacts and execution context stay in sync.

Use these entry points:
- `$gsd-fast` for a trivial task inline, with no subagents and no PLAN.md
- `$gsd-quick` for small fixes, doc updates, and ad-hoc tasks
- `$gsd-debug` for investigation and bug fixing
- `$gsd-execute-phase` for planned phase work

Do not make direct repo edits outside a GSD workflow unless the user explicitly asks to bypass it.
<!-- GSD:workflow-end -->

<!-- GSD:profile-start -->

## Developer Profile

> Profile not yet configured. Run `$gsd-profile-user` to generate your developer profile.
> This section is managed by `generate-claude-profile` -- do not edit manually.
<!-- GSD:profile-end -->
