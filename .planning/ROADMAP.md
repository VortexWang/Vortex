# Roadmap: VORTEX

## Overview

先完成可审查的工程版本基线、五类正式契约和 ENGR-006 安全模型，再依 V1.11 的引擎优先顺序交付 M0～M6。共八阶段，所有原始 257 项及 20 项补充约束在 REQUIREMENTS.md 中各映射到唯一阶段。API 压测工具按用户批准的 CR-001 使用 Vegeta；冻结规格原件保持不变。Docker 和 GitHub Actions 对同一提交执行适用验证，系统验收仍须满足规格的真实 Linux、资源与时长条件。

## Phases

- [ ] **Phase 1: 工程基线与安全契约 (Engineering Foundation)** - 锁定并评审工程版本、许可、契约和安全模型。
- [ ] **Phase 2: M0 引擎数据面 (M0 Engine Dataplane)** - 独立 Engine 实现直接 TCP/UDP 转发、门控、计量和性能基线。
- [ ] **Phase 3: M1 VLink 引擎链路 (M1 VLink Engine)** - 实现加密隧道、连接复用、建链方向和段许可。
- [ ] **Phase 4: M2 多跳与引擎完备 (M2 Multi-Hop Engine)** - 完成多跳调度、配额门控、排空和原子配置应用。
- [ ] **Phase 5: M3 节点 Agent 与 Edge Hub (M3 Agent and Hub)** - 交付节点运行时、授权、自治账本和安全恢复交接。
- [ ] **Phase 6: M4 后端业务闭环 (M4 Backend)** - 交付租户、资源、规则、套餐、计量结算及权威账本。
- [ ] **Phase 7: M5 前端与运营能力 (M5 Frontend and Operations)** - 交付中文管理体验、DNS、审计、通知与恢复运维。
- [ ] **Phase 8: M6 全量验收与发布交付 (M6 Full Acceptance)** - 完成规格全量性能、稳定性、故障恢复和交付物验收。

## Phase Details

### Phase 1: 工程基线与安全契约 (Engineering Foundation)
**Goal**: 实现开始前，所有参与者能依照可复现的版本、许可和正式契约工作，安全模型已覆盖规格要求的故障及两两组合。
**Depends on**: Nothing
**Requirements**: NFR-038, ENGR-001, ENGR-002, ENGR-003, ENGR-004, ENGR-005, ENGR-006, SUP-FMT-001, SUP-STACK-001, SUP-PLAT-001, SUP-IFACE-001, SUP-STRUCT-001, SUP-TOOLS-001, SUP-MILE-001, SUP-PARAM-001
**Success Criteria** (what must be TRUE):
1. Go/Node、直接与传递依赖、生成器、容器镜像和 API 压测工具均有精确锁定版本、校验值与完整许可清单；Go/Node 等许可、SQLite 嵌入版本及 CR-001 的 Vegeta 选择通过评审。
2. 数据库、OpenAPI、Protobuf、运行配置和 VLink 编码五类契约已交付，逐条映射 V1.11 需求与验收编号，评审结论和证据可定位。
3. TLA+ 或确定性模拟器覆盖 ENGR-006 和 NFR-027～NFR-045 要求的状态、不变量、故障及两两组合，反例和运行结果归档；模型明确不是实现验收替代品。
4. Docker 与 Actions 有同一提交、可复用的测试入口和结果归档；GitHub Actions 的实际执行能力通过 VortexWang 连接或已配置的仓库通道核验。
5. V1.11 引用但仓库缺少的冻结/评审材料已找到并核验，或记录为阻塞证据及其对 NFR-038 的影响；不生成虚假评审证据。
**Plans**: TBD

### Phase 2: M0 引擎数据面 (M0 Engine Dataplane)
**Goal**: 用户可通过独立 Go Engine 建立直接 TCP/UDP 转发；门控与可信计量接口开启时满足 M0 指标。
**Depends on**: Phase 1
**Requirements**: GEN-001, GEN-002, GEN-003, GEN-004, GEN-005, GEN-006, GEN-007, ENG-001, ENG-002, ENG-003, ENG-004, ENG-005, ENG-006, ENG-007, ENG-008, ENG-009, ENG-010, ENG-011, NFR-001
**Success Criteria** (what must be TRUE):
1. Engine 是可独立构建、运行、测试和压测的 Go 模块，生产 Agent 和模拟 Agent 只通过正式进程内契约接入；后端与 Hub 不承载转发流量。
2. 直接 TCP 转发正确处理半关闭、并发拷贝取消和清理，平台规定路径采用经证明的零拷贝实现；数据面没有数据库或主动控制面依赖。
3. 直接 UDP 会话、门控、速率/连接限制和计量批次边界具有自动化测试，正常消费按写成功载荷计量，预扣批次仅提交或退回一次。
4. 在规格指定 Linux 测试环境、正式门控和计量开启时，直接转发吞吐达到 NFR-001；保存硬件、命令、配置和原始基准结果。
**Plans**: TBD

### Phase 3: M1 VLink 引擎链路 (M1 VLink Engine)
**Goal**: 节点可在已授权路径上通过受认证加密 VLink 建立正向和反向隧道，链路身份与授权不可伪造或重放。
**Depends on**: Phase 2
**Requirements**: ENG-012, ENG-013, ENG-014, ENG-015, ENG-016, ENG-017, ENG-018, ENG-019, ENG-020, ENG-021, ENG-022, ENG-023, ENG-024, ENG-025, NFR-002, NFR-004, NFR-007
**Success Criteria** (what must be TRUE):
1. VLink 帧编码、HELLO、连接身份、代次、时间锚点和会话流 ID 与锁定契约一致，畸形帧、旧身份和伪造路径在数据面被拒。
2. TLS 1.3 链路、mux/pool、多路双向隧道和反向建链在自动化拓扑中可运行，链路断开后资源回收且不重复执行授权。
3. 每条会话执行段许可和目标限制；UDP 开流遵循规格认证流程，不存在绕过 OPEN/许可的 DGRAM 通路。
4. 正向隧道与反向隧道分别达到 NFR-002、NFR-004；pool 与 mux 的首包额外等待分别测量并满足 NFR-007。
**Plans**: TBD

### Phase 4: M2 多跳与引擎完备 (M2 Multi-Hop Engine)
**Goal**: 多跳 TCP/UDP 路径可正确调度和排空，份额、计费快照与两阶段应用门控在并发和失败中保持安全。
**Depends on**: Phase 3
**Requirements**: ENG-026, ENG-027, ENG-049, ENG-028, ENG-029, ENG-030, ENG-031, ENG-032, ENG-033, ENG-034, ENG-035, ENG-036, ENG-037, ENG-038, ENG-039, ENG-050, ENG-040, ENG-041, ENG-047, ENG-048, ENG-042, ENG-043, ENG-044, ENG-045, ENG-046, NFR-003, NFR-005, NFR-006, NFR-039, NFR-040, NFR-044
**Success Criteria** (what must be TRUE):
1. 多中继选择、路径变更与切换遵循许可和健康状态，三中继多跳达到 NFR-003，端到端顺序正确且不能绕过任何中间节点。
2. 计费授权按不可变快照预扣，只有写成功载荷被确认计量；未知量保留上界，不把授权到期当作消费结清。
3. 同节点/boot 的份额修改通过 prepare、关闭旧续租权、CAS 和执行回执切换，速率/连接降额在规定排空条件满足前保留旧占用。
4. 两阶段配置应用具有幂等 Prepare/Commit/Abort，失败和丢失回执不产生双活代次、重复资源释放或未授权会话。
5. TCP 满载下 UDP 公平性和 NFR-005 丢包指标、NFR-006 校验/绕行判据通过；D-04 预扣与水位参数经 M2 压测和正式评审确认。
**Plans**: TBD

### Phase 5: M3 节点 Agent 与 Edge Hub (M3 Agent and Hub)
**Goal**: 节点可安全注册、持久运行和上报，Edge Hub 在有效边界内自治并在失联、重启和账本转接期间保守维持或停止授权。
**Depends on**: Phase 4
**Requirements**: AGT-001, AGT-002, AGT-003, AGT-004, AGT-005, AGT-006, AGT-007, AGT-008, AGT-012, AGT-009, AGT-010, AGT-011, HUB-001, HUB-002, HUB-003, HUB-004, HUB-005, HUB-006, HUB-007, HUB-008, HUB-009, HUB-010, HUB-011, HUB-012, HUB-013, HUB-014, HUB-015, HUB-016, HUB-017, HUB-018, SUP-AGENT-001, SUP-B1-001, SUP-B3-001, SUP-B4-001
**Success Criteria** (what must be TRUE):
1. Agent 与 Engine 交付单一 `vortex-node`，注册凭据和运行身份分离；mTLS 双向流、证书续期、排空和可回退升级可验证。
2. Agent/Hub SQLite 记录在响应/回执前耐久提交；运行时报告 SQLite 版本和连接参数，磁盘满、进程崩溃、重启、重复批次等测试不丢已确认账本。
3. Edge Hub 仅在区域额度、原份额和硬截止内自治；HUB 分区/重启发生时关闭旧签发权并按单一代次和交接证据恢复。
4. 恢复账本轮转在单个本地事务封存前缀并转移原 R，不复制授权；后端尚未追平时持续尾账按唯一 ID 幂等上报。
5. 节点激活状态区分 prepared、activation-unknown、active 和 released；Commit/Abort 竞争、迟到回执及重启未知均保留占用，只有规范证明可释放。
**Plans**: TBD

### Phase 6: M4 后端业务闭环 (M4 Backend)
**Goal**: 用户和管理员可通过权威后端管理租户、节点、规则和权益；授权、额度、份额及结算由事务性业务事实控制。
**Depends on**: Phase 5
**Requirements**: TEN-001, TEN-002, TEN-003, TEN-004, TEN-005, TEN-006, TEN-007, TEN-008, TEN-009, NOD-001, NOD-002, NOD-003, NOD-004, NOD-005, NOD-006, NOD-007, NOD-008, NOD-009, NOD-010, NOD-011, NOD-012, NOD-013, NOD-014, PRT-001, PRT-002, PRT-003, PRT-004, PRT-005, PRT-006, PRT-007, PRT-016, PRT-008, PRT-009, PRT-010, PRT-011, PRT-012, PRT-013, PRT-014, PRT-015, RUL-001, RUL-002, RUL-003, RUL-004, RUL-005, RUL-006, RUL-007, RUL-008, RUL-009, RUL-010, RUL-011, RUL-012, RUL-013, RUL-014, RUL-015, RUL-016, BE-001, BE-002, BE-003, BE-004, BE-005, BE-006, PLN-001, PLN-002, PLN-006, PLN-007, PLN-008, PLN-003, PLN-004, PLN-005, MTR-001, MTR-002, MTR-003, MTR-004, MTR-005, MTR-006, MTR-007, MTR-008, MTR-009, MTR-010, MTR-011, MTR-013, MTR-014, MTR-015, MTR-016, LIM-001, LIM-002, LIM-003, LIM-004, LIM-005, LIM-006, LIM-007, LIM-008, LIM-009, LIM-010, SUP-B2-001, SUP-B5-001
**Success Criteria** (what must be TRUE):
1. 用户与租户数据隔离在后端访问层强制执行，管理员权限与资源归属限制正确，越权读写拒绝且按规范审计。
2. 节点、节点组、端口、段许可、规则编译和变更编排能从 API 走到 Agent/Engine 并展示逐对象执行状态；事务失败或部分应用按规则回滚。
3. 套餐、流量包、权益码、计费快照、预留与结算能逐来源幂等更新；Valkey 丢失不会改变余额、所有权或已确认业务。
4. 速率和连接份额按 `(tenant_id,node_id)` 唯一归属，使用 PostgreSQL 事务、持久化 CAS/Outbox 与规范所有权截止，不因 Hub 切换重复发放。
5. 余额变化、旧来源承诺和未知负债满足逐来源不变量；没有精确证明或获批扣减风险预算时持续冻结，不向用户收取未知上界。
**Plans**: TBD

### Phase 7: M5 前端与运营能力 (M5 Frontend and Operations)
**Goal**: 中文用户和运营人员可理解并管理授权业务状态，DNS、审计、通知和恢复操作反映系统真实执行事实。
**Depends on**: Phase 6
**Requirements**: FE-001, FE-002, FE-003, FE-004, FE-005, FE-006, FE-007, FE-008, FE-009, FE-010, FE-011, FE-012, SEC-001, SEC-002, SEC-003, SEC-004, SEC-005, SEC-011, SEC-012, SEC-013, SEC-006, SEC-007, SEC-008, SEC-009, SEC-010, SUP-PAGES-001, SUP-RECOVERY-001
**Success Criteria** (what must be TRUE):
1. 普通用户和管理员的中文页面按角色提供规格要求的资源、规则、套餐权益、运行状态、通知和审计视图。
2. 前端将期望、准备、实际执行、降级和争议状态分开呈现；API 和节点端仍独立执行鉴权、限制和拒绝逻辑。
3. Cloudflare DNS 记录集合与有效 serving 成员一致；端口释放、变更、失败及重试过程可观察且不可过早重新分配。
4. TLS/mTLS、秘密存储、目标地址过滤、速率限制、管理审计及数据保留要求具备可执行配置和回归测试。
5. PostgreSQL 基础备份与 WAL 恢复演练、Hub/Agent 恢复对账、RPO/RTO 和密钥恢复资料完成并可审阅。
**Plans**: TBD

### Phase 8: M6 全量验收与发布交付 (M6 Full Acceptance)
**Goal**: V1.11 第 13 节和 ENGR 全量门槛以真实可复现证据通过，源码、镜像及运维材料可交付。
**Depends on**: Phase 7
**Requirements**: NFR-008, NFR-009, NFR-010, NFR-011, NFR-012, NFR-013, NFR-014, NFR-015, NFR-016, NFR-017, NFR-018, NFR-019, NFR-020, NFR-021, NFR-022, NFR-023, NFR-024, NFR-026, NFR-025, NFR-027, NFR-028, NFR-029, NFR-030, NFR-031, NFR-032, NFR-033, NFR-034, NFR-035, NFR-036, NFR-037, NFR-041, NFR-042, NFR-043, NFR-045, SUP-SCALE-001, SUP-TEST-001, SUP-METHOD-001, SUP-ARTIFACT-001
**Success Criteria** (what must be TRUE):
1. 全部 NFR-025 业务与恢复场景、NFR-027～NFR-045 修订专项和第 13 节其余验收逐项有结果、环境、原始数据和复核记录；缺少来源证据的 NFR-038 不得伪报通过。
2. 在规格规模与独享 Linux 硬件条件下完成吞吐、UDP、200 节点/1,000 规则控制面和 24 小时稳定性验收，记录资源、配置、命令、误差和复测结论。
3. 同一 Git commit 在本机 Docker 与 GitHub Actions 各运行适用的静态检查、许可检查、构建、单测、集成、race、泄漏、网络拓扑和 Vegeta API 压测；原始日志与制品可追溯，未执行项目标保持未验证。
4. ENGR-006 模型与真实 Engine、Agent、Hub、PostgreSQL 实现状态映射完成，规定不变量、故障及两两组合通过模型与系统证据复核。
5. 源码、数据库迁移、锁定版本与依赖许可、镜像、校验值、SBOM、部署/升级/备份恢复说明及最终验收报告均已交付。
**Plans**: TBD

## Progress

**Execution Order:**
1 → 2 → 3 → 4 → 5 → 6 → 7 → 8

| Phase | Plans Complete | Status | Completed |
|-------|----------------|--------|-----------|
| 1. Engineering foundation | 0/TBD | Not started | - |
| 2. M0 Engine dataplane | 0/TBD | Not started | - |
| 3. M1 VLink engine links | 0/TBD | Not started | - |
| 4. M2 Multi-hop engine | 0/TBD | Not started | - |
| 5. M3 Agent and Edge Hub | 0/TBD | Not started | - |
| 6. M4 Backend | 0/TBD | Not started | - |
| 7. M5 Frontend and operations | 0/TBD | Not started | - |
| 8. M6 Full acceptance | 0/TBD | Not started | - |
