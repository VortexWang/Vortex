# Phase 1: 工程基线与安全契约 (Engineering Foundation) - Context

**Gathered:** 2026-11-03
**Status:** Ready for planning

<domain>
## Phase Boundary

在产品实现前交付可复现、可审查的工程版本与许可基线，冻结数据库、OpenAPI、Protobuf、运行配置和 VLink 编码契约，并建立覆盖额度、租约、计量及恢复安全边界的 ENGR-006 故障模型。此阶段不实现产品功能。V1.11 引用的历史冻结/评审证据仍缺失时，继续可验证工作，但 NFR-038 和 Phase 1 验收保持阻断。

</domain>

<decisions>
## Implementation Decisions

### 契约冻结
- **D-01:** 数据库、OpenAPI、Protobuf、运行配置和 VLink 编码五类契约分别版本化，并采用 SemVer：破坏性变更升主版本，兼容性扩展升次版本。对已发布契约的版本承诺具有 **one-way** 性质；改变兼容规则会影响所有依赖方。
- **D-02:** 每条需求 ID 必须映射到一个或多个契约，或明确标记为不适用并说明理由。
- **D-03:** CI 强制检查契约内容变更同步更新契约版本、需求映射和变更记录；缺少任何一项时阻断。

### 额度与租约故障模型
- **D-04:** Phase 1 定义并覆盖 NFR-027 至 NFR-045 的全部状态，以及关键两两故障组合。
- **D-05:** 用状态转移表、明确安全不变量、可达状态清单和禁止状态清单表达模型。
- **D-06:** 按“不重复签发权利、未知负债不被无资金释放”的风险选择两两故障组合，记录纳入与排除理由。
- **D-07:** 使用模型测试和属性/不变量检查验证；Docker 提供可复现入口，GitHub Actions 使用同一命令并保存结果。模型结果不能替代实现或系统验收。

### 版本与许可基线
- **D-08:** Phase 1 为工具链、直接和传递依赖、生成器、容器镜像、测试与压测工具锁定精确版本、校验值和许可证。候选以 Phase 1 评审时的最新稳定版为主，完整清单须先提交用户确认，再锁定。
- **D-09:** 必须完成可复现构建/来源校验和许可证审查；无法验证或不合规的候选必须替换并重新锁定，不采用未验证例外。
- **D-10:** 锁定后任何升级都要重新审查版本、校验值、许可证和兼容性，并更新基线记录。

### 治理证据缺失处理
- **D-11:** 允许继续不依赖缺失历史证据的工作，但 NFR-038 标记未通过并阻断 Phase 1 完成及发布验收。
- **D-12:** 对缺失材料逐项登记名称、来源/责任方、追查动作和证据链接；找到后核验原件，不生成替代或虚构的历史证据。
- **D-13:** 只有找回并核验原件，或通过正式规格变更修订 NFR-038，才能解除阻断。

### the agent's Discretion
用户没有将以上决策交由代理自行决定的事项。具体建模工具与数据结构可由计划阶段在这些约束内选择，并须满足可复现、可审查和安全不变量要求。

</decisions>

<canonical_refs>
## Canonical References

**Downstream agents MUST read these before planning or implementing.**

### 规范与变更
- `docs/VORTEX 需求规格说明书 V1_11.md` — 冻结的规范性需求、验收编号、工程门槛与安全要求；不得在实现中弱化。
- `docs/changes/CR-001-API压测工具替换.md` — 用户批准 Vegeta 替代 k6 的许可冲突处理与验收影响。
- `.planning/REQUIREMENTS.md` — 全部 277 项需求及其阶段映射。
- `.planning/REQUIREMENTS-SOURCE.md` — 原始需求逐项来源、哈希及补充约束追踪。
- `.planning/ROADMAP.md` — Phase 1 范围、成功标准及 M0～M6 阶段边界。
- `.planning/PROJECT.md` — 项目目标、技术栈、全局安全与交付约束。

### 工程研究
- `.planning/research/STACK.md` — 工具链、运行时和依赖候选版本；这些候选尚未构建验证，须按 D-08/D-09 重新核验并经用户确认。
- `.planning/research/ARCHITECTURE.md` — 模块边界和跨组件契约研究。
- `.planning/research/PITFALLS.md` — 持久化、租约、恢复和验证中的已识别风险。
- `.planning/research/SUMMARY.md` — 初始化研究结论及未验证事项汇总。

</canonical_refs>

<code_context>
## Existing Code Insights

### Reusable Assets
- 当前仓库没有产品源码、契约实现或现成测试框架可复用；本阶段从空工程建立治理与验证入口。

### Established Patterns
- 项目采用 GSD 分阶段工作流。V1.11 是规范来源，需求编号与来源追踪应保持稳定。

### Integration Points
- Phase 1 输出的五类版本化契约和 ENGR-006 模型是 M0～M6 后续阶段的输入。
- 本机 Docker 与 GitHub Actions 必须针对同一提交执行相同的适用验证命令并留存各自结果。

</code_context>

<specifics>
## Specific Ideas

- 原始 V1.11 保持不变；任何需求变更按规格正式变更流程处理。
- 对历史冻结/评审材料缺口保持可见、可追踪状态，不能用新写的材料冒充历史证据。
- 版本按 Phase 1 评审时的最新稳定版提出候选，须经用户确认后再锁定。
</specifics>

<deferred>
## Deferred Ideas

None — discussion stayed within phase scope.
</deferred>

---

*Phase: 1-engineering-foundation*
*Context gathered: 2026-11-03*
