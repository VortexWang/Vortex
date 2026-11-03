# VORTEX 技术栈研究

**领域：** 商业闭源、多租户 TCP / UDP 转发平台，控制面与自研数据面分离  
**研究日期：** 2026-11-02（America/New_York）  
**研究置信度：** MEDIUM  
**需求来源：** `docs/VORTEX 需求规格说明书 V1_11.md` §3.3、§3.4、§8、§14 与附录 B

## 结论与基线状态

采用规格 §3.3 的全部指定技术。Go 标准库主导独立 Engine，Gin / pgx / sqlc / Goose 构成后端单体；PostgreSQL 保存唯一权威业务事实，Valkey 保持可丢失；Agent / Hub 使用本地 SQLite WAL；React SPA 按服务端状态、本地交互状态和表单校验分工。此处不引入新消息中间件、ORM、Kubernetes 或现成代理核心。用户已在本次研究期间批准以 MIT 许可的 Vegeta 替代 §14.3 的 k6；该工具选择已获授权，原 V1.11 文件保持原状，规范变更记录由主流程交付。

**当前已锁定的精确版本：无。** 下表是已查阅官方发布页、包元数据后提出的候选，不是 ENGR-001 已交付的工程版本基线，也不证明组合已经构建、许可证审查或双端测试。正式锁定必须交付工具链版本、直接与传递依赖锁文件、生成器版本、镜像 digest、许可证清单、SBOM、测试记录并完成评审。发布页中的滚动 “Latest” 不作为可复现基线。

研究工具按 `query research-plan` 生成四组问题；Context7 MCP / CLI 均未可用，内置 web 工具返回 404 后使用会话提供的网页搜索连接器查询官方来源，并对关键候选补查 npm / Go 官方代理元数据。`query classify-confidence --provider websearch --verified` 返回 MEDIUM，本文外部事实沿用该等级；需求约束直接来自仓库规格，实施建议是工程判断。未运行项目安装、构建或性能验收。

## 推荐核心技术

### Go、控制通信与数据库

| 技术 | 精确候选 | 用途与选型理由 | 来源 / 验证边界 |
| --- | --- | --- | --- |
| Go | **1.27.3** | Engine、Node、Hub、后端统一工具链；标准库提供 net、tls、x509、slog、pprof，避免额外数据面框架；该补丁包含 x509 安全修复 | [官方发布历史](https://go.dev/doc/devel/release#go1.27.0)；仅确认发布，不证明 VORTEX 性能 |
| Gin | **v1.12.0** | 仅 HTTP 接入、认证上下文、请求解析、REST / SSE；业务规则放领域服务 | [官方发布](https://github.com/gin-gonic/gin/releases/tag/v1.12.0)；不启用其 HTTP/3 能力作为 VLink |
| pgx/v5 | **v5.12.0** | PostgreSQL 原生驱动、连接池和事务；避免从过期教程复制旧驱动版本 | [官方 CHANGELOG](https://github.com/jackc/pgx/blob/master/CHANGELOG.md) 2026-10-30 记录安全修复；[精确 go.mod](https://proxy.golang.org/github.com/jackc/pgx/v5/@v/v5.12.0.mod) 要求 Go ≥1.25 |
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

选择 Node 24 LTS 作为构建环境，不把 Node 放入静态前端运行镜像。研究时 Node 26 已进入 LTS，但 24 仍受支持，能满足本组合的 engines，较少引入工具链同时升级变量。[Node 官方发布与支持状态](https://nodejs.org/en/about/previous-releases)、[24.22.2 发布入口](https://nodejs.org/en/blog/release)。

| 技术 | 精确候选 | 职责与理由 | 来源 |
| --- | --- | --- | --- |
| Node.js | **24.22.2 LTS** | 构建、类型检查、测试的统一运行时；锁定附带 npm 的版本 | [官方发布](https://nodejs.org/en/blog/release) |
| React / react-dom | **19.3.0 / 19.3.0** | SPA 渲染；两者精确版本保持相同 | [官方版本说明](https://react.dev/versions)、[react-dom 包元数据](https://registry.npmjs.org/react-dom/19.3.0) |
| TypeScript | **6.0.3** | `strict: true`，单独执行类型检查；建议同时启用 noUncheckedIndexedAccess | [官方发布](https://github.com/microsoft/TypeScript/releases)；7.0.2 已存在，本建议先固定 6.0.3，避免初始基线同时引入 native 编译器迁移；不声称 7 不兼容 |
| Vite / React 插件 | **8.3.5 / @vitejs/plugin-react 6.2.1** | 构建、开发和 CSS Modules；采用匹配 Vite 8 的插件 | [Vite 发布](https://github.com/vitejs/vite/releases)、[插件元数据](https://registry.npmjs.org/@vitejs/plugin-react/6.2.1) |
| Ant Design / icons | **antd 6.6.6 / @ant-design/icons 6.3.4** | 中文管理界面、主题 Token、Form；不套用额外后台框架 | [官方入门](https://ant.design/docs/react/getting-started/)、[迁移兼容说明](https://ant.design/docs/react/migration-v6/)、[antd 元数据](https://registry.npmjs.org/antd/6.6.6) |
| React Router | **8.5.1** | 按角色组织 SPA 路由；后端仍独立执行权限 | [官方发布](https://github.com/remix-run/react-router/releases)、[精确元数据](https://registry.npmjs.org/react-router/8.5.1) |
| TanStack Query | **@tanstack/react-query 5.105.3** | API 读取、mutation、缓存失效、SSE 触发重取；不把账本真实状态只留在浏览器缓存 | [官方发布](https://github.com/TanStack/query/releases)、[精确元数据](https://registry.npmjs.org/@tanstack/react-query/5.105.3)；不要误用同仓其他框架 6.x 版本 |
| Zustand | **5.0.16** | 本地交互状态，如编辑器草稿、侧栏、筛选偏好；不复制 Query 中的服务器实体 | [官方发布](https://github.com/pmndrs/zustand/releases) |
| Zod | **4.6.7** | 表单输入、接口边界校验；和 Ant Design Form 适配 | [官方发布](https://github.com/colinhacks/zod/releases) |
| CSS Modules | **随 Vite 8.3.5** | 页面局部样式；共用颜色/间距使用主题变量 | [Vite CSS 文档](https://vite.dev/guide/features.html#css-modules) |

“前后端同一套校验规则”通过 ENGR-004 的 OpenAPI / Schema 契约、共享规范用例和 Go / Zod 一致性回归兑现。Go 后端不能直接执行 Zod 类型；表单通过也不能跳过服务端鉴权、资源归属和业务状态校验。生成客户端的技术选项与具体版本在工程基线阶段选定，不在研究中假装已经交付。

### 支持库与开发工具

| 工具 / 库 | 候选版本或锁定要求 | 使用范围 / 原因 |
| --- | --- | --- |
| Prometheus Go client | **v1.25.0** | [官方发布](https://github.com/prometheus/client_golang/releases)；暴露规格指标，避免 tenant/rule/session 等无界标签；业务查询不能依赖采样指标 |
| OpenTelemetry Go API / SDK | **v1.47.0** | [官方发布](https://github.com/open-telemetry/opentelemetry-go/releases)；控制面请求和编排链路追踪；不同信号模块版本分别锁定，不把同一发布组的 0.x 当成 1.x；不对每包生成 span |
| golang.org/x/sys | **v0.49.0 候选** | 与 SQLite v1.61.1 go.mod 对齐；Engine 如需 Linux syscall、splice 才引入并再次审查依赖边界 |
| Go testing / race / pprof | **随 Go 1.27.3** | 独立 Engine、集成、竞态和性能分析；race 产物与正式压测产物分开 |
| go.uber.org/goleak | **v1.3.0** | [官方发布](https://github.com/uber-go/goleak/releases)；会话、链路和监听关闭后检测 goroutine 泄漏，结合 FD/连接实际计数 |
| Vitest | **5.1.0** | [官方发布](https://github.com/vitest-dev/vitest/releases)、[engines/peer 元数据](https://registry.npmjs.org/vitest/5.1.0)；支持 Vite 8、Node 24；coverage 插件保持同版本 |
| React Testing Library | **16.3.3** | [官方发布](https://github.com/testing-library/react-testing-library/releases)；验证用户可见行为；DOM / user-event 等直接依赖仍须精确锁定 |
| Playwright | **1.64.1** | [官方发布](https://github.com/microsoft/playwright/releases)；锁定对应浏览器下载与测试镜像；Chromium / Firefox / WebKit 回归不等同于真实 Safari / Edge 全部验收 |
| Vegeta | **v12.13.0 候选；用户已批准替代 k6** | API 定速负载、吞吐/延迟与状态码结果；[官方发布](https://github.com/tsenart/vegeta/releases/tag/v12.13.0)、[MIT LICENSE](https://github.com/tsenart/vegeta/blob/master/LICENSE)。身份令牌准备、业务串联和幂等断言由测试脚本配合，不假定它直接执行 k6 脚本 |
| iperf3、iproute2 / tc、strace、文件校验与 UDP 序列工具 | **在 Ubuntu 24.04 测试镜像中按包版本、镜像 digest 锁定** | 保留同协议同加密最小基准、网络拓扑和 splice 证据；测试工具不随业务镜像打包；许可范围仍须明确 |
| Docker Engine / Compose v2 / Buildx | **按本机与 CI 实测输出锁定具体版本** | 本轮未查询/验证宿主安装状态，不捏造已安装版本；优先在固定测试镜像内复用命令，记录宿主差异 |
| GitHub Actions | **每个 action 固定完整 commit SHA** | runner 标签选 ubuntu-24.04 / ubuntu-24.04-arm，避免 ubuntu-latest；标签对应的宿主镜像仍会更新，记录 image version |
| SBOM / 漏洞 / 许可工具 | **工程基线阶段确定并锁定版本** | 建议 Syft 生成二进制/镜像 SBOM，govulncheck 检查 Go；另检查 npm 与镜像系统包许可证。工具加入前先核实其精确版本、许可和双架构制品 |

## SQLite 驱动与持久化决策

**建议先采用 modernc v1.61.1，M3 前必须以实际账本负载验证，而不是仅跑通 SQL。** 纯 Go 使普通发行构建可用 `CGO_ENABLED=0` 交叉编译；代价是其转译 SQLite 在 CPU 密集查询上可能比原生 C 慢，且 modernc.org/libc 必须与驱动 go.mod 的验证版本一致。不要独立随意升级 libc。驱动官方文档列出这些约束与性能差别，但上游微基准不是 VORTEX 的每秒计量和签发负载证据。[驱动官方说明](https://pkg.go.dev/modernc.org/sqlite)。

| 项目 | 推荐约束 / 验证 |
| --- | --- |
| 断电安全 | `journal_mode=WAL`、`synchronous=FULL`；签发响应和 AckUsage 前等事务成功。FULL 的额外同步成本计入每秒批处理与 Hub 签发延迟预算 |
| 连接策略 | 单进程单写入队列，写事务短且有界；读池有上限；每连接明确 foreign_keys / busy_timeout / synchronous 设置；不以无上限重试掩盖磁盘故障 |
| 文件系统 | 使用本地磁盘和容器 named volume；WAL 不放 NFS / 跨机器共享目录；本机压测数据优先放 Linux VM 内 volume，而非 macOS 文件共享路径 |
| WAL 运维 | 监测 WAL 大小、检查点耗时、写入队列深度、磁盘剩余、fsync 延迟；备份使用一致性快照/备份流程，不只复制运行中的单个 db 文件 |
| WAL-reset 修复 | SQLite 官方列 3.7.0～3.51.2 受影响，3.51.3+ 或明确回补版本修复；候选嵌入 3.54.1。构建后在两架构输出 `sqlite_version()`、compile_options，并加入基线证据 |
| 故障试验 | 提交前后 kill、磁盘满、只读盘、检查点与写并发、重复批次、授权响应丢失、轮转事务崩溃；确认已 ACK 不丢、未 ACK 重试稳定、未知上界持续冻结 |
| 停止语义 | 写失败必须阻止继续产生无法记录的消费；仅停止新会话不满足 AGT-006 |

SQLite 的 WAL 与同步语义见 [WAL 官方文档](https://sqlite.org/wal.html)、[PRAGMA synchronous](https://sqlite.org/pragma.html#pragma_synchronous)。这些设置保障数据库持久化边界，不能消除 Engine 内尚未落盘的最后窗口；MTR-015 仍须保留授权上界负债。

备选 **mattn/go-sqlite3** 仍满足 SQLite 技术要求，适合当真实 Hub 负载证明 pure Go CPU/内存成本不能接受时评审切换；它需要 `CGO_ENABLED=1` 和 C 编译器，双架构原生构建或交叉 C 工具链、glibc/musl 及静态链接都增加基线工作。切换时必须重新验证其嵌入 SQLite 版本和全图许可证，不因驱动名已成熟而豁免。[上游构建说明](https://github.com/mattn/go-sqlite3)。本研究不同时引入两个生产驱动。

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

Docker 官方提供 QEMU、原生多节点、交叉编译三种构建策略；推荐普通 Go 制品交叉编译、目标架构原生运行测试，并通过 Buildx 组装 amd64/arm64 manifest。[Docker 多平台构建](https://docs.docker.com/build/building/multi-platform/)。`CGO_ENABLED=0` 是普通发行构建建议，**不是 race 测试设置**；race 在受支持平台需开启 cgo 和 C 编译器，独立测试镜像应包含这些工具。[Go race 官方要求](https://go.dev/doc/articles/race_detector#Requirements)。

网络拓扑脚本统一运行在 Linux 内。按最小需求授予 NET_ADMIN 等能力；需特权的 namespace 场景单独定义测试容器，不能让业务 Compose 服务默认 privileged。OrbStack 的容器与 Mac 访问方式、`host.docker.internal` 和容器网络语义见 [官方网络说明](https://docs.orbstack.dev/docker/network)；Linux 容器内部的 host network 不能理解成直接接管 macOS 内核网络。

GitHub 提供 runner 类型与资源说明，但可用性及规格需在接入 CI 时核实。[GitHub runner 文档](https://docs.github.com/en/actions/reference/runners/github-hosted-runners)。若当前仓库不能使用 arm64 托管 runner，使用明确配置的原生 arm64 自托管 runner；保持未执行项可见，不用 QEMU 结果填补原生栏位。

### 可复现证据约定

本机和远端使用相同 `test/` 入口、镜像 digest、测试参数和源代码提交，结果分别保存。每份记录至少包含 commit SHA、源码是否 dirty、宿主/runner、CPU 架构、内核、Docker/Compose/Buildx、工具链和 SQLite 核心版本、cgroup 限额、拓扑和 tc 参数、测试起止时间、命令、退出码、原始吞吐/延迟/丢包/泄漏数据。成功截图不能替代机器可读结果。对相同 SHA 保留“本机”“Actions”两套结论；缺一不推断另一套通过。

## 许可与商业发布风险

| 对象 | 已核实事实 / 推荐处理 |
| --- | --- |
| Go / Gin / pgx / sqlc / Goose / gRPC / Protobuf | 核心项目采用 BSD / MIT / Apache 等宽松许可证的候选路线；正式放行仍检查精确版本 LICENSE / NOTICE 和完整传递图，不把根项目许可证当全图结论 |
| 前端直接依赖 | 查询的 React、Ant Design、Vite、Router、Query、Zustand、Zod、Vitest npm 元数据标 MIT；Playwright 标 Apache-2.0。类型包、原生 bundler、浏览器及其他传递依赖另外审查 |
| PostgreSQL / Valkey / SQLite | [PostgreSQL License](https://www.postgresql.org/about/licence/)、[Valkey BSD 许可说明](https://valkey.io/topics/license/)、[SQLite public domain](https://sqlite.org/copyright.html)；驱动和扩展分别保留许可证。public domain 等归类在允许清单中显式定义，不能让扫描器默认为未知而静默放行 |
| k6 → Vegeta 已获用户批准 | [上游 k6](https://github.com/grafana/k6) 明确 AGPL-3.0，与原 §14.3 / ENGR-002 的组合形成冲突；用户已明确批准采用 [MIT Vegeta](https://github.com/tsenart/vegeta/blob/master/LICENSE) 替代。研究据此采用 Vegeta，保持全部 API 压测目标；原 V1.11 不覆写，主流程补充需求变更记录。Vegeta 精确版本、传递依赖与镜像仍须工程基线审查 |
| 系统与镜像 | Ubuntu、Debian、Alpine 镜像和网络测试工具可能含 GPL/LGPL 系统组件；只检查 go.mod / npm 不足以证明镜像合规。分别盘点构建环境、内部测试、运行制品和分发镜像，按最终明确的政策判定，禁止将“非链接工具”自动当合规 |
| OrbStack | [官方条款](https://docs.orbstack.dev/legal/terms) 对商业/专业使用要求购买相应许可；它是开发宿主工具，不随 VORTEX 发布；当前是否已持有授权未核实 |
| 发布输出 | 每个架构二进制和镜像分别生成 SBOM、许可证清单、校验值；保留必要版权/NOTICE，许可证扫描异常阻断发布，不能仅凭顶层 permissive 名称放行 |

对于无 cgo 的 Go 服务，建议尽量以 scratch 最终镜像交付，显式装入所需信任根、时区文件等受控资产；前端可由独立 Go 静态服务器容器提供，保持 Compose 前后端服务边界。此为降低运行镜像依赖的建议，不保证“scratch = 自动合规”。PostgreSQL / Valkey 自身与其发行镜像系统包仍须审查，不能用应用镜像最小化替代整套交付检查。

## 版本兼容与锁定步骤

| 组合 | 已检查的条件 | 锁定前必须执行 |
| --- | --- | --- |
| Go 1.27.3 + SQLite v1.61.1 + pgx v5.12.0 | SQLite go.mod 要求 1.26、pgx 要求 1.25，工具链满足下限；SQLite libc 固定 v1.79.0 | 双架构 `go mod graph`、构建、数据库事务与崩溃测试；检查 MVS 是否改写 libc 或其他关键依赖 |
| Node 24.22.2 + Vite 8.3.5 + React 插件 6.2.1 + Vitest 5.1.0 | npm engines/peer 范围允许；插件需 Vite 8，Vitest 允许 Vite 8 / Node 24 | 精确安装、peer 冲突检查、类型检查、测试与 production build；插件可选 peer 按实际使用精确锁定 |
| React/DOM 19.3.0 + Router 8.5.1 + antd 6.6.6 | Router 要求 Node ≥22.22、React ≥19.2.7；antd 要求 React ≥18；候选满足 | 真实页面渲染、Form、Modal、中文 locale、SSE 重连、路由权限与浏览器回归 |
| protobuf runtime、两个生成器、protoc | 所选均为正式版，版本号体系互不等同 | 生成后无差异校验、后端/Hub/Agent 编译、双向流兼容与升级/降级测试 |
| PG 18.6 + sqlc + Goose + pgx | 推荐 SQL 优先组合；未验证全部 DDL/SQL 特性支持 | 真实 PG 建库、迁移、sqlc generate、并发计费事务与物理恢复演练 |
| Playwright 1.64.1 + 浏览器 | 测试库版本已确认 | 对应浏览器和镜像精确匹配；规格要求真实 Chrome/Edge/Safari 当前及前一主版本的覆盖另列证据 |

工程版本基线建议按以下顺序落地：

1. 将用户已批准的 k6 → Vegeta 工具变更写入正式需求变更记录；明确 ENGR-002 对测试/构建/系统包的适用边界，将许可证允许项写入机器可检查策略。
2. 为 engine、node、edge-hub、backend 建独立模块并统一工具链；使用 go.work 便于开发，但发行构建显式验证本地替换与模块依赖。Engine 的依赖检查禁止 Gin、SQLite、pgx 或控制面 client 流入。
3. 固定 Go、Node、npm、sqlc、Goose、protoc、两个 Go 生成器、测试及扫描工具版本和 SHA256；关闭未审查的自动工具链下载漂移。
4. 提交各模块 go.mod / go.sum、前端 package-lock.json、固定 packageManager / engines；前端直接依赖用精确版本并使用 npm ci；CI 生成代码必须无未提交 diff。
5. 锁定每个测试/运行镜像 digest，验证其 manifest 含 amd64、arm64；正式清单记录 manifest digest 及平台 child digest，不只记 tag。
6. 运行两架构构建和原生 smoke、数据库/协议/许可检查，再执行相同 SHA 的本机 Docker 与 Actions 套件。上述候选通过后才成为工程基线；之后安全更新以受控变更推进，不能为“锁版本”长期停留在有漏洞补丁。

### 供基线验证使用的安装示例

以下命令只示范已研究候选，**本轮未执行，也不构成完整安装脚本**；应在各自模块目录按实际依赖安装，而非把数据库包装入 Engine。

```bash
# backend 模块
go get github.com/gin-gonic/gin@v1.12.0 github.com/jackc/pgx/v5@v5.12.0

# node / edge-hub 各自模块
go get modernc.org/sqlite@v1.61.1 google.golang.org/grpc@v1.85.0 google.golang.org/protobuf@v1.36.12

# 统一工具环境；其他生成器和镜像同样显式锁定
go install github.com/sqlc-dev/sqlc/cmd/sqlc@v1.31.1
go install google.golang.org/protobuf/cmd/protoc-gen-go@v1.36.12
go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@v1.6.2

# frontend；辅助类型、DOM 环境等依赖另行核实后锁定
npm install --save-exact react@19.3.0 react-dom@19.3.0 antd@6.6.6 @ant-design/icons@6.3.4 react-router@8.5.1 @tanstack/react-query@5.105.3 zustand@5.0.16 zod@4.6.7
npm install --save-dev --save-exact typescript@6.0.3 vite@8.3.5 @vitejs/plugin-react@6.2.1 vitest@5.1.0 @testing-library/react@16.3.3 @playwright/test@1.64.1
```

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

本轮没有验证 Docker/OrbStack 安装与授权、GitHub 认证、runner 配额、镜像 tag/digest 是否已经发布、全部传递依赖许可、Go 生成代码组合、TS6 与每个前端类型包的实际安装、吞吐及持久化延迟。不得将这些项标为已通过。官方网页、npm 元数据与精确源码包支持候选选择；完整锁定仍由工程前置阶段完成。

---
*范围：仅 STACK.md；研究文件不修改冻结需求，不等同于技术基线评审或验收通过。*
