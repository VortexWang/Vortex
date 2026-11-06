# Roadmap: VORTEX

## Overview

先完成 M0 所需的工程基线、五类契约和安全模型，再按引擎优先顺序交付 M0～M6。V1.11 是需求参考，不是冻结验收依据；阶段验收由当前项目决策定义。依赖在首次使用的阶段前锁定。PostgreSQL 与 Valkey 是外部服务，不纳入产品依赖许可门禁。Docker 和 GitHub Actions 对同一提交执行适用验证。

## Phases

- [x] **Phase 1: 工程基线与安全契约 (Engineering Foundation)** - 完成 M0 所需 Go 工具链、契约、安全模型与双宿主冒烟基线；其他依赖按首次使用阶段闭合。
- [x] **Phase 2: M0 引擎数据面 (M0 Engine Dataplane)** - 独立 Engine 实现直接 TCP/UDP 转发、门控、计量和性能基线。 (completed 2026-11-04)
- [x] **Phase 3: M1 VLink 引擎链路 (M1 VLink Engine)** - 实现加密隧道、连接复用、建链方向和段许可。功能验证完成；专用原生主机 NFR 资格化待补证。
- [x] **Phase 4: M2 多跳与引擎完备 (M2 Multi-Hop Engine)** - 实现范围已关闭；正式性能资格化、D-04 锁定和持久化权威缺口保留为后续验收债务。
- [ ] **Phase 5: M3 节点 Agent 与 Edge Hub (M3 Agent and Hub)** - 交付节点运行时、授权、自治账本和安全恢复交接。 *(current)*
- [ ] **Phase 6: M4 后端业务闭环 (M4 Backend)** - 交付租户、资源、规则、套餐、计量结算及权威账本。
- [ ] **Phase 7: M5 前端与运营能力 (M5 Frontend and Operations)** - 交付中文管理体验、DNS、审计、通知与恢复运维。
- [ ] **Phase 8: M6 全量验收与发布交付 (M6 Full Acceptance)** - 完成规格全量性能、稳定性、故障恢复和交付物验收。

## Phase Details

### Phase 1: 工程基线与安全契约 (Engineering Foundation)
**Goal**: M0 开始前，Go 工具链、Engine 所需依赖边界、五类正式契约和有限额度/租约安全模型具备可复现证据。
**Depends on**: Nothing
**Requirements**: ENGR-001, ENGR-002, ENGR-003, ENGR-004, ENGR-005, ENGR-006, SUP-FMT-001, SUP-STACK-001, SUP-PLAT-001, SUP-IFACE-001, SUP-STRUCT-001, SUP-TOOLS-001, SUP-MILE-001, SUP-PARAM-001
**Success Criteria** (what must be TRUE):
1. M0 使用 Go 1.27.3 与标准库，不引入第三方 Engine 依赖；M0 构建、测试、网络 fixture 和 Docker/Actions 入口已验证。后续阶段依赖在首次使用前完成版本、来源、完整性与许可审查。
2. 数据库、OpenAPI、Protobuf、运行配置和 VLink 编码五类契约已交付，逐条映射保留的需求编号，评审结论和证据可定位。
3. TLA+ 或确定性模拟器覆盖 ENGR-006 和 NFR-027～NFR-045 要求的状态、不变量、故障及两两组合，反例和运行结果归档；模型明确不是实现验收替代品。
4. Docker 与 Actions 有同一提交、可复用的测试入口和结果归档；GitHub Actions 的实际执行能力通过 VortexWang 连接或已配置的仓库通道核验。
5. V1.11 所引的 11 份历史材料已按项目重启决定登记为缺失；不重建材料，NFR-038 不作为本项目开发或验收门槛。
**Plans**: 5/5 plans complete; Phase 1 M0-entry criteria complete. Plans 01-05 retain remaining just-in-time dependency review; 01-06 is superseded by the owner restart decision.
**Wave 1**
- [x] 01-01-PLAN.md — Candidate versions, licenses and evidence register
- [x] 01-02-PLAN.md — Formal contracts and requirement traceability

**Wave 2** *(blocked on Wave 1 completion)*
- [x] 01-03-PLAN.md — Lease safety model and fault combinations

**Wave 3** *(blocked on Wave 2 completion)*
- [x] 01-04-PLAN.md — Docker and GitHub Actions verification path

### Phase 2: M0 引擎数据面 (M0 Engine Dataplane)
**Goal**: 用户可通过独立 Go Engine 建立直接 TCP/UDP 转发；门控与可信计量接口开启时满足 M0 指标。
**Depends on**: Phase 1 M0 foundation criteria (Go toolchain, contracts, safety model, and same-commit Docker/Actions smoke)
**Requirements**: GEN-001, GEN-002, GEN-003, GEN-004, GEN-005, GEN-006, GEN-007, ENG-001, ENG-002, ENG-003, ENG-004, ENG-005, ENG-006, ENG-007, ENG-008, ENG-009, ENG-010, ENG-011, NFR-001
**Success Criteria** (what must be TRUE):
1. Engine 是可独立构建、运行、测试和压测的 Go 模块，生产 Agent 和模拟 Agent 只通过正式进程内契约接入；后端与 Hub 不承载转发流量。
2. 直接 TCP 转发正确处理半关闭、并发拷贝取消和清理，平台规定路径采用经证明的零拷贝实现；数据面没有数据库或主动控制面依赖。
3. 直接 UDP 会话、门控、速率/连接限制和计量批次边界具有自动化测试，正常消费按写成功载荷计量，预扣批次仅提交或退回一次。
4. 在适用的 Linux 环境尝试直接转发基准，保存硬件、命令、配置、原始结果和与 700 Mbps 开发目标的差距；未达目标时继续正确性工作，不单独阻断 Phase 2 进度。
**Plans**: 6/6 plans complete (TCP relay, Linux splice, UDP sessions, bounded admission, injected gate/usage contract, benchmark harness). The environment-limited benchmark does not claim qualified 700 Mbps acceptance; durable policy adapters remain with their owning runtime.
- [x] 02-01 — TCP relay and half-close
- [x] 02-02 — Linux splice path
- [x] 02-03 — UDP sessions and idle expiry
- [x] 02-04 — Bounded UDP admission (gap closure)
- [x] 02-05 — Gates, limits, and usage batches (gap closure)
- [x] 02-06 — Linux benchmark evidence (gap closure)

### Phase 3: M1 VLink 引擎链路 (M1 VLink Engine)
**Goal**: 节点可在已授权路径上通过受认证加密 VLink 建立正向和反向隧道，链路身份与授权不可伪造或重放。
**Depends on**: Phase 2
**Requirements**: ENG-012, ENG-013, ENG-014, ENG-015, ENG-016, ENG-017, ENG-018, ENG-019, ENG-020, ENG-021, ENG-022, ENG-023, ENG-024, ENG-025, NFR-002, NFR-004, NFR-007
**Success Criteria** (what must be TRUE):
1. VLink 帧编码、HELLO、连接身份、代次、时间锚点和会话流 ID 与锁定契约一致，畸形帧、旧身份和伪造路径在数据面被拒。
2. TLS 1.3 链路、mux/pool、多路双向隧道和反向建链在自动化拓扑中可运行，链路断开后资源回收且不重复执行授权。
3. 每条会话执行段许可和目标限制；UDP 开流遵循规格认证流程，不存在绕过 OPEN/许可的 DGRAM 通路。
4. 正向隧道与反向隧道分别达到 NFR-002、NFR-004；pool 与 mux 的首包额外等待分别测量并满足 NFR-007。
**Plans**: 6/6 plans complete in 5 waves; ready to execute after research, validation strategy, decision coverage, and plan review.
**Wave 1**
- [x] 03-01-PLAN.md — Freeze VLink v1 codec and authenticated TCP tracer

**Wave 2** *(blocked on Wave 1 completion)*
- [x] 03-02-PLAN.md — TLS identity, mux state, windows, and liveness

**Wave 3** *(blocked on Wave 2 completion)*
- [x] 03-03-PLAN.md — Local OPEN authorization and UDP admission

**Wave 4** *(blocked on Wave 3 completion; plans 03-04 and 03-05 may run in parallel)*
- [x] 03-04-PLAN.md — Pool modes and reverse link establishment
- [x] 03-05-PLAN.md — Abuse limits and privacy-preserving observability

**Wave 5** *(blocked on Wave 4 completion)*
- [x] 03-06-PLAN.md — Docker/Actions verification and NFR evidence (environment-limited; native qualification pending)

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
**Plans**: 11/11 executed; implementation closed by owner decision on 2026-11-05. `04-VERIFICATION.md` remains `gaps_found`; formal performance qualification, D-04 approval and durable authority are carried forward and are not represented as passed.
- [x] 04-01 — Startable runtime config and accounting adapter
- [x] 04-02 — Supervised VLink relay and stream bridge
- [x] 04-03 — Five-process Docker/native topology
- [x] 04-04 — Mixed TCP/UDP workload and evidence gate
- [x] 04-05 — Native/Actions qualification and report
- [x] 04-06 — Runtime UDP, mixed-load qualification and accounting reconciliation (execution record recovered; formal qualification gaps remain open for Phase 4 verification)
- [x] 04-07 — Fail-closed source authorization (G-04-01 closed; production accounting remains out of scope)
- [x] 04-08 — Wire policy-time and snapshot accounting (runtime slice; full NFR-039/NFR-044 remain open)
- [x] 04-09 — Wire share CAS and lease drain (G-04-040; process-local runtime slice, durable authority remains open)
- [x] 04-10 — Early TCP DATA and finite attack matrix (G-04-006/G-04-007; timeline qualification remains open)
- [x] 04-11 — D-04 parameter scan and formal qualification gate (G-04-003/G-04-005/G-D04; candidates remain unlocked and formal gate not-qualified)

### Phase 5: M3 节点 Agent 与 Edge Hub (M3 Agent and Hub)
**Goal**: 节点可安全注册、持久运行和上报，Edge Hub 在有效边界内自治并在失联、重启和账本转接期间保守维持或停止授权。
**Depends on**: Phase 4 已交付的实现基础。资格化与优化在 `gsd/phase-4-optimization-closure` 分支并行，不阻断 Phase 5 研究、规划、执行与验证。
**Requirements**: AGT-001, AGT-002, AGT-003, AGT-004, AGT-005, AGT-006, AGT-007, AGT-008, AGT-012, AGT-009, AGT-010, AGT-011, HUB-001, HUB-002, HUB-003, HUB-004, HUB-005, HUB-006, HUB-007, HUB-008, HUB-009, HUB-010, HUB-011, HUB-012, HUB-013, HUB-014, HUB-015, HUB-016, HUB-017, HUB-018, SUP-AGENT-001, SUP-B1-001, SUP-B3-001, SUP-B4-001
**Success Criteria** (what must be TRUE):
1. Agent 与 Engine 交付单一 `vortex-node`，注册凭据和运行身份分离；mTLS 双向流、证书续期、排空和可回退升级可验证。
2. Agent/Hub SQLite 记录在响应/回执前耐久提交；运行时报告 SQLite 版本和连接参数，磁盘满、进程崩溃、重启、重复批次等测试不丢已确认账本。
3. Edge Hub 仅在区域额度、原份额和硬截止内自治；HUB 分区/重启发生时关闭旧签发权并按单一代次和交接证据恢复。
4. 恢复账本轮转在单个本地事务封存前缀并转移原 R，不复制授权；后端尚未追平时持续尾账按唯一 ID 幂等上报。
5. 节点激活状态区分 prepared、activation-unknown、active 和 released；Commit/Abort 竞争、迟到回执及重启未知均保留占用，只有规范证明可释放。
**Plans**: 14 total:11 executed. 05-11 signed actual-node upgrade/rollback completed on `5ca72e0` with Docker/Actions42193600373 and independent ledger audits. 05-12 durable recovering barrier is next;05-13..05-14 remain dependency-waiting. G05-01/02/03/04 resolved within the Phase 5 protocol-adapter/static-destination scope; G05-05 remains open. Phase-wide verification/UAT must be refreshed after all gap plans; historical UAT (17 passed) is retained. Production backend remains Phase 6; Phase 4 work stays separate.
**Wave 1**
- [x] 05-01 — Startable node and durable local store
**Wave 2** *(blocked on Wave 1 completion)*
- [x] 05-02 — Registration, mTLS control, reconcile and upgrade supervision
- [x] 05-05 — Close 05-02 control-contract and file-upgrade gaps (owner-approved 1.1.0)
**Wave 3** *(blocked on Wave 2 completion)*
- [x] 05-03 — Grants, usage handoff and bounded autonomy (depends on 05-05)
**Wave 4** *(blocked on Wave 3 completion)*
- [x] 05-04 — Ledger handoff, activation recovery and evidence gate

**Wave 5** *(blocked on Wave 4 completion)*
- [x] 05-06 — Exact lifecycle contract proposal, approval and freeze
**Wave 6** *(blocked on Wave 5 completion)*
- [x] 05-07 — Actual regional failover and current-session fencing
**Wave 7** *(blocked on Wave 6 completion)*
- [x] 05-08 — Probe/report scheduler, durable remote renewal and retention
**Wave 8** *(blocked on Wave 7 completion)*
- [x] 05-09 — Control/VLink identity rotation and revocation
**Wave 9** *(blocked on Wave 8 completion)*
- [x] 05-10 — Operational commands, Engine drain and deployment packaging
**Wave 10** *(blocked on Wave 9 completion)*
- [x] 05-11 — Actual service upgrade and financial-safe rollback
**Wave 11** *(blocked on Wave 10 completion)*
- [ ] 05-12 — Durable recovering/caught-up state and finite catch-up barrier
**Wave 12** *(blocked on Wave 11 completion)*
- [ ] 05-13 — Full share-chain CAS and immutable policy cutover
**Wave 13** *(blocked on Wave 12 completion)*
- [ ] 05-14 — Paired process evidence and renewed phase acceptance

**Cross-cutting constraints:** Unknown financial liability remains frozen through timeout, reconnect, restart, rotation and rollback. Authoritative SQLite rows are never restored from an older snapshot. Normative contracts require the exact owner-approved proposal in 05-06; later changes repeat that gate. Every integration plan starts with a real running tracer after contract approval. Docker/Actions evidence uses identical source SHA; missing/skipped scenarios cannot pass. Production backend authority stays Phase 6; Phase 4 qualification continues independently.

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
**Goal**: 当前项目范围和 owner 确认的验收项以真实可复现证据通过，源码、镜像及运维材料可交付。V1.11 第 13 节仅作参考输入，不自动构成冻结验收门槛。
**Depends on**: Phase 7
**Requirements**: NFR-008, NFR-009, NFR-010, NFR-011, NFR-012, NFR-013, NFR-014, NFR-015, NFR-016, NFR-017, NFR-018, NFR-019, NFR-020, NFR-021, NFR-022, NFR-023, NFR-024, NFR-026, NFR-025, NFR-027, NFR-028, NFR-029, NFR-030, NFR-031, NFR-032, NFR-033, NFR-034, NFR-035, NFR-036, NFR-037, NFR-041, NFR-042, NFR-043, NFR-045, SUP-SCALE-001, SUP-TEST-001, SUP-METHOD-001, SUP-ARTIFACT-001
**Success Criteria** (what must be TRUE):
1. 全部纳入当前项目范围的业务与恢复场景、性能、稳定性和交付验收逐项有结果、环境、原始数据和复核记录；NFR-038 历史原件要求按项目重启决定保持 out-of-scope，不宣称历史材料已恢复。
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
| 1. Engineering foundation | 5/5 | Complete    | 2026-11-03 |
| 2. M0 Engine dataplane | 6/6 | Complete    | 2026-11-04 |
| 3. M1 VLink engine links | 6/6 | Complete (functional; native NFR evidence pending) | 2026-11-04 |
| 4. M2 Multi-hop engine | 11/11 plans executed | Implementation closed; qualification debt carried forward | 2026-11-05 |
| 5. M3 Agent and Edge Hub | 11/14 | Implementation verified; phase acceptance pending | - |
| 6. M4 Backend | 0/TBD | Not started | - |
| 7. M5 Frontend and operations | 0/TBD | Not started | - |
| 8. M6 Full acceptance | 0/TBD | Not started | - |
