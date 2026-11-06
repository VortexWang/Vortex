# Phase 6: M4 后端业务闭环 (M4 Backend) - Discussion Log

> **Audit trail only.** Do not use as input to planning, research, or execution agents.
> Decisions are captured in `06-CONTEXT.md`; this log preserves the alternatives considered.

**Date:** 2026-11-06
**Phase:** 6-M4 后端业务闭环 (M4 Backend)
**Areas discussed:** 租户、角色与权限隔离, 规则编排与节点应用, 套餐、权益与限制生命周期, 计量、账本与份额所有权, 任务、Outbox 与 Agent/Hub 交付, REST/OpenAPI 与实时状态

---

## 租户、角色与权限隔离

| Option | Description | Selected |
|--------|-------------|----------|
| 单租户用户 + 平台管理员 | 普通用户绑定一个 tenant_id；平台管理员独立跨租户，强制审计。 | ✓ |
| 多租户成员 | 引入 memberships，用户可加入多个租户。 | |
| 统一角色绑定 | 所有主体通过 platform/tenant role bindings 授权。 | |

**User's choice:** 单租户用户 + 平台管理员。
**Notes:** 首版不引入团队邀请和多租户切换。

| Option | Description | Selected |
|--------|-------------|----------|
| 固定角色 + 作用域策略 | platform_admin/operator/auditor 与 owner/member，服务层按资源和动作判断。 | ✓ |
| 纯 RBAC | 仅按固定角色授予整组权限。 | |
| RBAC + 资源授权 | 角色之外再增加资源级 ACL。 | |

**User's choice:** 固定角色 + 作用域策略。

| Option | Description | Selected |
|--------|-------------|----------|
| 同事务追加审计 | 敏感操作与不可变审计事件同一 PostgreSQL 事务提交。 | ✓ |
| Outbox 异步审计 | 事务写 Outbox，再异步生成审计。 | |
| 数据库触发器审计 | 主要依赖表触发器自动捕获。 | |

**User's choice:** 同事务追加审计。

| Option | Description | Selected |
|--------|-------------|----------|
| 显式租户授权记录 | 平台资源保持所有权，通过有效期/状态/版本 grant 授予租户。 | ✓ |
| 全局可见 + 使用时校验 | 目录全局可见，创建规则时再校验。 | |
| 复制到租户作用域 | 授权时复制资源快照。 | |

**User's choice:** 显式租户授权记录。

## 规则编排与节点应用

| Option | Description | Selected |
|--------|-------------|----------|
| 不可变修订 + 乐观并发 | 每次编辑产生 rule_revision，使用 expected_revision/If-Match。 | ✓ |
| 可变当前记录 + 历史快照 | 直接更新当前记录并另存快照。 | |
| 数据库序列化编辑 | 所有编辑排队串行。 | |

**User's choice:** 不可变修订 + 乐观并发。

| Option | Description | Selected |
|--------|-------------|----------|
| 纯函数编译 + 不可变产物 | 根据授权和拓扑依赖生成可重放的 per-node artifact。 | ✓ |
| 运行时即时编译 | Agent/Hub 收到业务模型后自行解析。 | |
| 只保存最终配置 | 不保存完整编译输入依赖。 | |

**User's choice:** 纯函数编译 + 不可变产物。

| Option | Description | Selected |
|--------|-------------|----------|
| 保留旧版本并异步重编译 | 旧修订继续服务，依赖变化触发新产物和两阶段应用。 | ✓ |
| 阻塞依赖修改 | 所有引用规则应用完成后才提交依赖变化。 | |
| 立即失效旧规则 | 依赖变化提交即停止受影响规则。 | |

**User's choice:** 保留旧版本并异步重编译。

| Option | Description | Selected |
|--------|-------------|----------|
| 按修订持久化并合并待执行项 | superseded 未开始任务，保留进行中任务；只接受匹配 revision 的成功回执。 | ✓ |
| 严格按版本串行 | 每个修订都依次应用。 | |
| 直接取消旧任务 | 新修订取消全部旧任务。 | |

**User's choice:** 按修订持久化并合并待执行项。

## 套餐、权益与限制生命周期

| Option | Description | Selected |
|--------|-------------|----------|
| 目录可变、订阅快照固定 | 目录可编辑；新订阅或明确换套餐创建新快照。 | ✓ |
| 可变套餐 + 使用时读取 | 当前用户直接读取管理员最新套餐值。 | |
| 复制套餐到用户 | 开通时复制完整套餐配置。 | |

**User's choice:** 目录可变、订阅快照固定。
**Notes:** 用户最初选择“可变套餐 + 使用时读取”，但该语义会破坏 `MTR-016`/`PLN-006`；经澄清改为兼容解释。

| Option | Description | Selected |
|--------|-------------|----------|
| 同事务幂等兑换 | 锁定 redemption key，事务内扣次数并创建订阅/流量来源。 | ✓ |
| 先记兑换后异步发放 | 兑换与发放分离。 | |
| 允许部分成功 | 套餐和流量包分别提交。 | |

**User's choice:** 同事务幂等兑换。

| Option | Description | Selected |
|--------|-------------|----------|
| 独立不可变原因 + 当前限制投影 | restriction event 保留来源/时间，投影支持快速判断。 | ✓ |
| 只在请求时计算 | 不保存限制记录。 | |
| 只维护可变限制表 | 只覆盖当前限制。 | |

**User's choice:** 独立不可变原因 + 当前限制投影。

| Option | Description | Selected |
|--------|-------------|----------|
| 预览确认 + 事务生效 | 先计算损失和影响，确认后生成新快照与限制。 | ✓ |
| 立即替换 | 确认后立即切换并暂停不合规规则。 | |
| 排队到周期末 | 当前周期结束才切换。 | |

**User's choice:** 预览确认 + 事务生效。

## 计量、账本与份额所有权

**Selected recommended decisions:** PostgreSQL 不可变账本事件加可重建投影；用 `(node_id, boot_id, sequence)` 幂等接收用量；所有事实绑定不可变 `billing_snapshot_id` 并按资金来源分别守恒；份额使用带 boot 的线性代次 CAS 交接，缺少关闭证明则保留旧所有权。

## 任务、Outbox 与 Agent/Hub 交付

**Selected recommended decisions:** 业务事务、任务表和 Outbox 同事务提交；至少一次投递加幂等处理；保留 `unknown` 执行状态并通过查询/对账收敛；规则/DNS/节点编排用异步 `operation_id` 返回。

## REST/OpenAPI 与实时状态

**Selected recommended decisions:** `/api/v1` 资源化 REST 与 OpenAPI 唯一契约；短期 Bearer access + 轮换 refresh，内部 Agent/Hub 使用独立 mTLS；稳定错误模型和 cursor 分页；带 ID、可用 `Last-Event-ID` 的 SSE，事件缺口时回读 REST。

## the agent's Discretion

- Exact PostgreSQL tables, indexes, isolation levels, migration order, sqlc layout, token TTLs, cursor encoding, worker lease/backoff values, artifact serialization, and concrete API resource names remain for research and planning under the locked invariants.

## Deferred Ideas

- Online payment/auto-renewal, team invitations/multi-tenant membership, Phase 7 frontend and operations UX, and Phase 4/8 qualification work remain outside this context.
