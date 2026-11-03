# Phase 1: 工程基线与安全契约 (Engineering Foundation) - Discussion Log

> **Audit trail only.** Do not use as input to planning or implementation. Decisions are captured in `01-CONTEXT.md`.

**Date:** 2026-11-03
**Phase:** 1-engineering-foundation
**Areas discussed:** 契约冻结、额度与租约故障模型、版本与许可基线、治理证据缺失处理

---

## 契约冻结

| Option | Description | Selected |
|--------|-------------|----------|
| 五类契约独立版本化并由 CI 校验需求映射 | 数据库、OpenAPI、Protobuf、运行配置、VLink 编码分别管理 | ✓ |
| 只冻结 OpenAPI/Protobuf | 其他契约通过评审记录管理 | |
| 其他方式 | 用户自定义 | |

**User's choice:** 五类契约全部独立版本化，并在 CI 校验需求 ID 映射。

| Option | Description | Selected |
|--------|-------------|----------|
| SemVer | 破坏性变更升主版本，兼容性扩展升次版本 | ✓ |
| 单调递增版本号 | 兼容性由人工评审判断 | |
| 其他方式 | 用户自定义 | |

**User's choice:** 使用 SemVer。

| Option | Description | Selected |
|--------|-------------|----------|
| 全量需求映射 | 每条需求映射到契约，或标注不适用并附理由 | ✓ |
| 只校验协议和数据模型需求 | 其他需求人工审查 | |
| 其他方式 | 用户自定义 | |

**User's choice:** 每条需求必须有契约映射或带理由的不适用标记。

| Option | Description | Selected |
|--------|-------------|----------|
| CI 阻断不完整变更 | 契约变化须同步版本、映射和变更记录 | ✓ |
| CI 只强制版本号 | 其他部分由评审检查 | |
| 只提示 | 不阻断合并 | |

**User's choice:** 缺少版本、映射或变更记录时 CI 阻断。

**Notes:** 契约兼容策略在发布后影响依赖方，视为需要严格控制的承诺。

---

## 额度与租约故障模型

| Option | Description | Selected |
|--------|-------------|----------|
| 覆盖全部相关状态及关键组合 | NFR-027..045 全状态与关键两两故障组合 | ✓ |
| 先核心模型 | 后续阶段逐步增加故障组合 | |
| 其他方式 | 用户自定义 | |

**User's choice:** Phase 1 覆盖 NFR-027..045 全部状态及关键两两故障组合。

| Option | Description | Selected |
|--------|-------------|----------|
| 表格与清单 | 状态转移表、不变量、可达状态和禁止状态 | ✓ |
| 场景描述 | 自然语言为主并附关键状态图 | |
| 自动化规格 | 以模型/属性测试规格为核心 | |

**User's choice:** 用状态转移表和明确的不变量、可达/禁止状态清单表达。

| Option | Description | Selected |
|--------|-------------|----------|
| 按安全风险筛选并说明 | 聚焦权利不可重复签发、未知负债不被无资金释放 | ✓ |
| 穷举全部组合 | NFR 状态全排列两两组合 | |
| 后续确定 | Phase 1 只列状态 | |

**User's choice:** 按风险选择关键组合，并记录纳入和排除理由。

| Option | Description | Selected |
|--------|-------------|----------|
| 自动模型验证 | 模型测试/属性检查，Docker 可复现，CI 同命令并归档 | ✓ |
| 人工为主 | 评审和手工演练，自动化后补 | |
| 其他方式 | 用户自定义 | |

**User's choice:** 使用模型测试和属性/不变量检查；Docker 与 CI 使用相同命令并保存结果。

---

## 版本与许可基线

| Option | Description | Selected |
|--------|-------------|----------|
| Phase 1 精确锁定 | 版本、校验值、许可证一并锁定 | ✓ |
| 各阶段锁定 | Phase 1 仅定范围 | |
| 其他方式 | 用户自定义 | |

**User's choice:** Phase 1 锁定精确版本、校验值和许可证。

| Option | Description | Selected |
|--------|-------------|----------|
| 必须验证，不合格则替换 | 构建/来源和许可证审查均须通过 | ✓ |
| 临时例外 | 未验证项留待后续阶段 | |
| 其他方式 | 用户自定义 | |

**User's choice:** 不能验证或许可不合规时替换并重新锁定，不采用临时例外。

| Option | Description | Selected |
|--------|-------------|----------|
| 全依赖面审查 | 应用、直接/传递依赖、生成器、镜像、测试和压测工具 | ✓ |
| 只审生产依赖 | 开发与测试工具仅记录 | |
| 其他方式 | 用户自定义 | |

**User's choice:** 全面覆盖应用和工程链中的所有依赖与工具。

| Option | Description | Selected |
|--------|-------------|----------|
| 升级重新评审 | 校验版本、哈希、许可证、兼容性并更新基线 | ✓ |
| 安全补丁自动升级 | 月度汇总审查 | |
| CI 通过即可 | 不另做评审 | |

**User's choice:** 所有升级重新评审并更新锁定记录。

**User's choice:** 候选以 Phase 1 评审时的最新稳定版为主，完整清单提交用户确认后才锁定。

**Notes:** 研究中的版本均为候选，未构建验证；需重新核验后再请用户确认。

### Go toolchain selection

**User's choice:** Go 采用 1.27 版本线。

根据 [Go 官方发布索引](https://go.dev/dl/?mode=json&include=all) 在 2026-11-03 的结果，`go1.27.3` 是该版本线当前最新稳定补丁候选。该选择只确认 Go 版本线；完整工具链来源/校验、兼容性和全量依赖基线仍按 D-08/D-09 待审，不据此生成锁定文件。

---

## 治理证据缺失处理

| Option | Description | Selected |
|--------|-------------|----------|
| 继续可验证工作，保持阻断 | NFR-038 未通过且 Phase 1 不得完成 | ✓ |
| 暂停全部 Phase 1 工作 | 找到材料后恢复 | |
| 接受偏差 | 允许 Phase 1 通过 | |

**User's choice:** 继续可验证工作，但 NFR-038 标记未通过并阻断 Phase 1 完成。

| Option | Description | Selected |
|--------|-------------|----------|
| 逐项追查清单 | 记录来源/责任方、动作和证据链接，找到后核验原件 | ✓ |
| 简单缺失记录 | 只记名称和状态 | |
| 其他方式 | 用户自定义 | |

**User's choice:** 建立可审计的逐项追查记录并核验找到的原件。

| Option | Description | Selected |
|--------|-------------|----------|
| 允许独立工作继续 | 不依赖缺失证据的实现可继续，发布/Phase 1 验收仍阻断 | ✓ |
| 停止全部开发 | 等待材料补齐 | |
| 其他方式 | 用户自定义 | |

**User's choice:** 允许继续不依赖证据的实现，但发布及 Phase 1 验收保持阻断。

| Option | Description | Selected |
|--------|-------------|----------|
| 原件或正式变更 | 找回原件，或正式修订 NFR-038 后解除阻断 | ✓ |
| 签署替代证据 | 评审通过后解除 | |
| 接受永久风险 | 不再追查 | |

**User's choice:** 保持阻断，直到找到原件或通过正式变更修订要求。

---

## the agent's Discretion

用户没有将已讨论决策交由代理自行决定的事项。后续计划可在上述边界内选择具体模型工具和数据结构。

## Implementation refinement (2026-11-03)

Inline review of the candidate set found license-boundary conflicts in the proposed Java/TLC runtime, Ubuntu test image and `iproute2/tc` / `strace` tools. Applying the user's D-09 rule (replace candidates that cannot satisfy the license policy), Phase 1 uses a standard-library Go finite-state explorer and a `scratch` image containing only a static repository fixture. This fits ENGR-006's deterministic-simulator allowance and does not change V1.11 requirements. The implementation refinement is recorded as I-01..I-03 in `01-CONTEXT.md`; it is not an additional user decision. Any later packet-shaping tool needs its own version, checksum, license and compatibility review.

## Deferred Ideas

None.
