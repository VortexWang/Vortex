# Phase 7: M5 前端与运营能力 — Pattern Map

**Mapped:** 2026-11-10
**Files analyzed:** 40 个文件/模块单元；目录分组代表待 planner 展开的职责范围，不是声称已确定全部新文件名。
**Analogs found:** 23 / 40（exact 9、role-match 7、partial 7、无匹配 17）。

本映射接续 `07-CONTEXT.md` 与 `07-RESEARCH.md`，仅描述仓库现有模式及建议归属。下表标记“拟新增”的路径尚未交付；具体 DTO、迁移编号、CR 编号、目录细分由规划形成，不能把研究中的候选接口当作现有签名。未安装依赖、未运行测试、未修改源码。

## File Classification

质量定义：**exact** 为同职责同数据流（包括修改原文件）；**role-match** 为相同分层职责但新业务；**partial** 只复用协议或边界，不能照搬实现；**none** 为没有可复制的实现。所有下述现有源文件均通过 `git ls-files -- <path>` 的非空检查；不引用安装镜像或忽略目录中的副本。

| # | New/Modified File / 模块范围 | Role | Data Flow | Closest Analog | Match Quality |
|---|---|---|---|---|---|
| 1 | 拟新增 `frontend/package.json`、冻结锁、Vite/TS/Vitest/Playwright 配置 | config | transform | 无前端工程 | none |
| 2 | 拟新增 `frontend/src/app/`：provider、中文主题、角色工作台、路由 | provider / route | event-driven | 无 React provider/路由实现 | none |
| 3 | 拟新增 `frontend/src/api/generated/` 与独立生成器工具区 | model / config | transform | `backend/db/sqlc.yaml`；仅生成源/生成物分离思路 | partial |
| 4 | 拟新增 `frontend/src/api/`：Bearer 请求、错误、refresh single-flight | service | request-response | `backend/internal/httpapi/router.go`（A） | partial |
| 5 | 拟新增 `frontend/src/api/`：fetch SSE 传输与重连 | service | streaming | `backend/internal/httpapi/sse.go`（共享 S2） | partial |
| 6 | 拟新增 `frontend/src/features/` 中 Query hooks/query keys | hook | request-response / event-driven | 无 TanStack Query 实现 | none |
| 7 | 拟新增 `frontend/src/features/` 中身份绑定草稿 store | store | event-driven | 无 Zustand 实现 | none |
| 8 | 拟新增 `frontend/src/components/`、CSS Modules：状态、金额、时间、确认、路径段 | component | transform / event-driven | 无组件/样式实现 | none |
| 9 | 拟新增登录、安全设置、恢复码、step-up 页面 | component | request-response | 无公网登录或前端安全设置实现 | none |
| 10 | 拟新增租户/平台概览、系统设置页面 | component | request-response | 无仪表盘实现 | none |
| 11 | 拟新增规则列表、详情、编辑、路径及影响确认页面 | component | CRUD / event-driven | 无路径编辑器；后端 A/B 仅是接入边界 | none |
| 12 | 拟新增用户/租户、节点/个人组/平台组、段许可/Hub/部署入口页面 | component | CRUD | 无资源管理组件 | none |
| 13 | 拟新增套餐、权益管理、权益码管理及兑换页面 | component | CRUD / request-response | 无财务界面组件 | none |
| 14 | 拟新增用量、流量统计、平台统计页面 | component | request-response / transform | 无统计界面；共享 S3 约束金额 | none |
| 15 | 拟新增统一 DNS、组内 DNS、通知、公告页面 | component | CRUD / event-driven | 无运营界面；两 DNS 入口共享同一 API | none |
| 16 | 拟新增对象历史、全局审计、个人设置页面 | component | request-response | 无审计/设置组件 | none |
| 17 | 拟新增恢复工作台、逐租户进度及用户影响页面 | component | request-response / event-driven | 无恢复界面；共享 S4 限制其权力 | none |
| 18 | 拟新增 `frontend/tests/`、组件/真实 API 浏览器用例 | test | request-response / streaming | 无 Vitest/Playwright 工程 | none |
| 19 | 修改 `backend/internal/httpapi/router.go`，新增按业务拆分的 handler | controller / route | request-response | `backend/internal/httpapi/router.go`（A） | exact |
| 20 | 扩展 `backend/internal/auth/` 及身份服务/持久化：密码、TOTP、恢复码、step-up | service / model | request-response | `backend/internal/auth/tokens.go`（S1）；只有认证后的发会话口 | partial |
| 21 | 扩展 `backend/internal/policy/`、`repository/tenant_scope.go` 及任务权限复核 | middleware / utility | request-response / batch | `backend/internal/repository/tenant_scope.go`（S1） | exact |
| 22 | 扩展 `backend/internal/repository/public_resources.go`、`public_resource_lists.go`、`public_operations.go`、`public_recoveries.go` | model | request-response | `backend/internal/repository/public_resource_lists.go`（C） | exact |
| 23 | 扩展 `backend/internal/service/rule.go` 及规则预览/状态/实测服务 | service | CRUD / event-driven | `backend/internal/service/rule.go`（B） | exact |
| 24 | 新增/扩展资源 CRUD、账号生命周期、授权、Hub/部署领域服务 | service | CRUD | `backend/internal/service/rule.go`（B）；锁/审计模式，不复制规则算法 | role-match |
| 25 | 扩展套餐、权益码、用量聚合及不可逆确认服务 | service | CRUD / batch | `backend/internal/service/rule.go`（B）；事务框架，不替代现有财务语义 | role-match |
| 26 | 拟新增通知/公告/偏好、审计查询及设置服务 | service | CRUD / event-driven | `backend/internal/service/rule.go`（B）；业务模型需另设计 | role-match |
| 27 | 扩展 DNS 凭据受控写、域名/desired/observed 读模型及任务 | service | event-driven / request-response | `backend/internal/worker/dns.go`（D）；复用现有 `ServingService` 接点 | role-match |
| 28 | 扩展恢复概览/既有批准推进：`service/recovery_advance_job.go`、`hub_recovery_orchestration.go` | service | event-driven | `backend/internal/service/recovery_advance_job.go`（D/S4） | exact |
| 29 | 新增/扩展 `backend/internal/worker/`：通知、聚合、保留及恢复任务 | service | batch / event-driven | `backend/internal/worker/dns.go`（D） | role-match |
| 30 | 追加 `backend/db/migrations/` 迁移（编号待定） | migration | transform | `backend/db/migrations/00009_domain_events.sql`（S5） | role-match |
| 31 | 扩展 `backend/db/query/` 安全读模型、事件、DNS、恢复/清理查询 | model | CRUD / batch | `backend/db/query/public_resources.sql`（S3） | exact |
| 32 | 再生成 `backend/db/generated/`、数据库 schema 投影及后端 OpenAPI 副本 | model | transform | `backend/db/sqlc.yaml`、`backend/db/migrate.go`（S5） | partial |
| 33 | 必要时扩展 `agent/`、`hub/`：实测/执行证据、吊销、恢复接续及进程内接口 | service | event-driven / streaming | `agent/engine_apply.go`；仅 Agent→Engine 进程内适配骨架 | partial |
| 34 | 扩展 `engine/vlink/target.go` 及真实建连调用链目标策略 | utility / service | request-response | `engine/vlink/target.go`（S6） | exact |
| 35 | 新增后端 hold-aware 保留及必要 Agent/Hub 证据保护回归 | service | batch | `agent/store/usage_retention.go`（S7）；SQLite 特定规则不可搬成 PG TTL | partial |
| 36 | 拟新增 `tools/phase7/`：统一入口、契约/依赖检查、生成与运行证据 | utility / test | batch / file-I/O | `tools/phase6/check.sh`（E） | exact |
| 37 | 拟新增 `.github/workflows/phase7.yml` 与隔离测试 Compose | config | batch | `.github/workflows/phase6.yml`、`deploy/backend/compose.test.yml`（E） | exact |
| 38 | 拟新增 `deploy/backup/`：物理全备、WAL、加密、独立存储/generation 与恢复工具 | config / service | file-I/O / batch | 无完整物理灾备实现；阶段6临时数据库不算匹配 | none |
| 39 | 扩展 `contracts/`、治理记录、新变更提案及阶段7审批检查器 | config | transform | `tools/phase6/rest_contract_check.py`（S8）；历史 CR-009 保持不变 | role-match |
| 40 | 新增运维手册、六步恢复证据、页面/API/权限/要求覆盖矩阵 | config / test | file-I/O / batch | 无完整阶段7运行手册/覆盖物；研究提供组织输入 | none |

## Pattern Assignments

以下五组是主要复制起点；共享模式补充认证、SSE、SQL、恢复与运行边界。每个表格单元均可按编号追溯到具体摘录。不要继续泛搜第十个相似服务代替阅读这些现有签名。

### A. 公共 handler、REST 传输及安全错误（#4、#19）

**Analog:** `backend/internal/httpapi/router.go`。

**Imports**（12–20）：使用 Gin、pgx、项目模块完整路径；handler 调用领域服务/仓储，不直接持有业务算法。

```go
"github.com/gin-gonic/gin"
"github.com/google/uuid"
"github.com/jackc/pgx/v5"
"github.com/jackc/pgx/v5/pgxpool"
"vortex/backend/internal/auth"
"vortex/backend/internal/compiler"
"vortex/backend/internal/policy"
"vortex/backend/internal/repository"
"vortex/backend/internal/service"
```

**Auth + CAS + 验证**（242–254）：

```go
func (a *API) reviseRule(c *gin.Context) {
    s, ok := scope(c, policy.Mutate)
    if !ok { return }
    expected, ok := expectedRevision(c)
    if !ok { return }
    var body service.RuleInput
    if !decodeBody(c, &body) { return }
```

实际代码随后校验路由 UUID 与 `body.ID` 一致（255–259），调用当前签名（260）：

```go
result, e := (service.RuleService{Pool: a.config.Pool}).Mutate(c.Request.Context(), s, body, expected, requestID(c))
```

`decodeBodyLimit`（101–113）限制 Content-Type/体积，执行 `DisallowUnknownFields()` 并要求第二次 decode 为 EOF。新登录及敏感写也要显式请求结构、体积和错误边界，不接收浏览器提供的角色/可信证明。

**Error handling**（263–276）：修订冲突→412、输入无效→400、无权→403、路径不可达/状态冲突→409、无行→404、未知内部错误→503。统一安全输出（95–96）：

```go
func fail(c *gin.Context, status int, code, message string, fields map[string]string) {
    c.AbortWithStatusJSON(status, apiError{Code: code, Message: message, RequestID: requestID(c), FieldErrors: fields})
}
```

**异步响应**（280–288）：ETag、Location、202 + `operation_id`、`sync_status: pending`。前端只记录受理后跟踪 operation/REST；此模式不允许把 accepted 渲染为 applied。错误中文映射保留 code/request_id，别向用户透出原始 SQL 或密钥错误。

### B. 领域写、版本锁、审计与持久任务（#23–26）

**Analog:** `backend/internal/service/rule.go`；事务基础见 S1。当前入口（223–240）：

```go
func (s RuleService) Mutate(ctx context.Context, scope TenantScope, in RuleInput, expected int64, request string) (RuleMutation, error) {
    return s.mutate(ctx, scope, in, expected, request, 0)
}
```

核心包装（235–240）：

```go
e = (repository.AuditWriter{Pool: s.Pool}).WithTransaction(ctx, scope, policy.Mutate, &repository.Audit{Action: "rule.revise", ResourceID: in.ID, RequestID: request, Reason: "append immutable expected revision"}, func(q *generated.Queries) error {
    var err error
    result, err = appendRuleRevision(ctx, q, scope, in, expected, rollbackOf)
    return err
})
return result, e
```

`compiler.NormalizeRule` 在事务前执行（228–234）；事务中先 `LockTenant`，再 `LockRuleHead` 并核对 `DesiredRevision`（246–263）。`resolveRule`（128–220）稳定排序锁资源，复核当前节点、套餐、grant、permit 与方向；不能由前端的候选列表代替最终复核。

**任务入事务**（352–359）：

```go
op, e := newDomainTask(ctx, q, scope, in.ID, "rule.compile", "rule_compile", expected+1)
if e != nil { return e }
if e = q.BindRuleCompileTask(ctx, generated.BindRuleCompileTaskParams{TenantID: scope.TenantID(), TaskID: op.ID, RuleID: in.ID, RuleRevision: expected + 1}); e != nil {
    return e
}
result.OperationID = op.ID
```

**应用方式：**新资源/通知/公告/设置写复制分层、验证及事务框架，按自身权限行动选择 `policy.Action`。套餐/权益继续接既有财务服务，不复制规则的 `enabled`/版本逻辑当账务实现。规则预览须新增权威快照/会话水位并最终复核；这里没有已实现的完整影响预览接口。

### C. 安全读模型与平台读取审计（#22）

**Analog:** `backend/internal/repository/public_resource_lists.go`。

当前完整签名（28）及 guard（29–31）：

```go
func (r PublicResources) List(ctx context.Context, s TenantScope, collection string, after time.Time, id string, limit int, request string) ([]PublicResourceRow, error) {
    if !s.Allows(policy.Read) || !PublicCollection(collection) || limit < 1 || limit > 101 {
        return nil, policy.ErrForbidden
    }
```

SQL 参数显式携带 `TenantID`、Collection、游标及 PageLimit（34），不从返回集合成员关系推断权限。平台读取同样经事务审计（48–53）：

```go
if s.Platform() {
    e := (AuditWriter{Pool: r.Pool}).WithTransaction(ctx, s, policy.Read, &Audit{Action: collection + ".list", ResourceID: s.TenantID(), RequestID: request, Reason: "platform scoped public business list"}, read)
    return result, e
}
e := read(generated.New(r.Pool))
return result, e
```

全局租户枚举还逐条写目标租户审计（39–42）。分页调用模式复制 A 的 `listOperations`（201–239）：身份/租户/collection 绑定的签名 cursor，查询 `limit+1`，保留稳定排序；新筛选字段必须纳入契约与游标作用域设计。

**应用方式：**审计查询、恢复概览、统计及 DNS 新投影应各有安全 SQL/DTO；不得把生成模型或控制 protobuf 整体 JSON 输出。当前 `PublicCollection` 白名单（18–24）不能自动使新集合获得授权。

### D. worker 的有界调度与执行时当前权限（#27–29）

**Analog:** `backend/internal/worker/dns.go`；业务执行范例 `backend/internal/service/recovery_advance_job.go`。

薄 worker 注入权威池和凭据访问接口（15–18）：

```go
type DNSWorker struct {
    Pool  *pgxpool.Pool
    Store service.DNSCredentialAccess
}
```

**Core + error handling**（24–37）：

```go
rows, e := generated.New(w.Pool).PendingDNSJobs(ctx)
if e != nil { return false, e }
for _, row := range rows {
    worked, e := (service.ServingService{Pool: w.Pool}).ProcessDNSJob(ctx, row.TenantID, row.ID, uuid.NewString(), w.Store)
    if errors.Is(e, service.ErrScope) || errors.Is(e, service.ErrConflict) || errors.Is(e, pgx.ErrNoRows) {
        continue
    }
    if e != nil || worked { return worked, e }
}
return false, nil
```

`Run(ctx) error` 使用 ticker、`defer ticker.Stop()` 与 `ctx.Done()`（39–52）。新 worker 保留可单次驱动的 `Once`，但不要把 DNS 的所有可忽略错误照搬到财务/恢复任务。

**任务不继承旧角色：**`recovery_advance_job.go`（77–84）重新加载当前主体再建 scope：

```go
p, e := (repository.TokenRepository{Pool: s.Pool}).LocalSessionPrincipal(ctx, job.ActorID, "")
if e != nil || p.Role() != auth.PlatformAdmin { return false, ErrScope }
scope, e := repository.NewTenantScope(p, tenant, policy.Grant)
if e != nil { return false, e }
```

执行端先校验 payload hash/schema（66–69），租约更新同事务（85–110），结果必须核对 lease token 更新行数（133–139）。协调错误进入 `unknown`（117–123），不能自动当未执行释放占用。新通知归并/清理任务需定义自己的幂等键、当前权限、持久租约和恢复策略。

DNS 查询 `backend/db/query/dns_jobs.sql`（4–17）只选 actor-bound、未投递、有界任务，并核查当前平台/租户账号状态。统一页和组内页共用该服务及数据，凭据访问保持部署信任边界。

### E. clean SHA、双宿主/双架构与证据入口（#36–37）

**Analogs:** `tools/phase6/check.sh`、`.github/workflows/phase6.yml`、`deploy/backend/compose.test.yml`。

**验证入口前置**（check.sh 19–24）：

```sh
test -n "$expected" || { echo '--expected-sha or GITHUB_SHA required' >&2; exit 1; }
test "$(git rev-parse HEAD)" = "$expected" || { echo 'expected SHA differs from HEAD' >&2; exit 1; }
test -z "$(git status --porcelain=v1)" || { echo 'source must be clean, including untracked files' >&2; exit 1; }
export PHASE6_EXPECTED_SHA="$expected"
export GOTOOLCHAIN=local
test "$(go env GOVERSION)" = go1.27.3
```

第7阶段新入口应替换自身阶段变量/套件，不修改旧 producer 的历史声明。证据路径在 checkout 外（37–38）；docker/actions producer 身份显式区分（43–46）；已完成结果不得覆盖（49）；未知 plan 失败（34–35）。工作区已有阶段4/5改动，应使用独立 clean checkout 生产证据，不清理他人改动。

**Actions 矩阵**（phase6.yml 10–18）：

```yaml
strategy:
  fail-fast: false
  matrix:
    include:
      - runner: ubuntu-24.04
        arch: amd64
      - runner: ubuntu-24.04-arm
        arch: arm64
runs-on: ${{ matrix.runner }}
```

沿用 action 完整 SHA 固定（22、26、49）、失败仍上传（47–53），新增前端/浏览器精确工具身份及对应许可证。阶段7须自己记录适用套件的执行结果、资源、镜像 digest、正常/race 和生成 diff；不得把阶段6 artifact 当本阶段通过。

`deploy/backend/compose.test.yml` 是隔离集成结构 analog；真实灾备必须另建持久 PG18 数据卷、独立备份存储/凭据及 generation 权威，不能把临时测试 DB 改个服务名就声称耐久恢复已验证。

## Shared Patterns

### S1. 主体、租户范围与事务审计（#19–29）

**Sources:** `backend/internal/auth/tokens.go`、`backend/internal/policy/policy.go`、`backend/internal/repository/tenant_scope.go`。

`tokens.go`（82–95）只有可信认证之后的发会话入口：

```go
// Issue is a trusted post-authentication adapter entrypoint, never an API accepting actor/role claims.
func (m Tokens) Issue(ctx context.Context, p Principal, request string) (TokenPair, error) {
```

新密码/TOTP/challenge/恢复码服务必须先验证真实凭据及账号现态，再调用此入口；现有 `issue-session` CLI 和测试直接构造 Principal 不能充当浏览器最终登录。`TokenStore`（29–34）沿用创建/轮转/查询/撤销接口及会话族语义，未发现密码/TOTP/step-up 的现成算法可复制。

`tenant_scope.go`（19–24）：

```go
func NewTenantScope(p auth.Principal, tenant string, action policy.Action) (TenantScope, error) {
    if err := policy.Authorize(p, tenant, action); err != nil { return TenantScope{}, err }
    return TenantScope{principal: p, tenant: tenant, action: action}, nil
}
```

`policy.go`（32–42）现有五角色矩阵：管理员可 Grant，operator 不能 Grant/ManageAccess，auditor 只读，owner 可 ManageAccess，member 可 Mutate。新增 UI 入口不能扩大这些权限；资源 grant 另受所有权边界约束。

`tenant_scope.go`（51–54、64–68、76）把 authority/audit/task/outbox 一起提交：

```go
func (w AuditWriter) WithTransaction(ctx context.Context, scope TenantScope, action policy.Action, event *Audit, fn func(*generated.Queries) error) error
```

回调只做权威数据库操作，返回任何错误就回滚；缓存、DNS/外部网络 I/O 不放回调。摘要只能放安全业务字段。`ForTenant`（34–42）保留当前主体/行动并独立授权目标租户，不能从存储的 actor ID 自行升级权限。

### S2. SSE 是提示，REST 是事实（#4–7、#18、#19）

**Source:** `backend/internal/httpapi/sse.go`。

身份/租户绑定（16–17）：

```go
func eventScope(p auth.Principal, tenant string) CursorScope {
    return CursorScope{PrincipalID: p.ID(), TenantID: tenant, Collection: "events", Direction: "forward"}
}
```

读取 `Last-Event-ID` 且拒绝重复头/任意 query（27–43）；重放缺口发送 `resync` 后结束流（97–108）；每页/心跳前重新 `Tokens.Authenticate` 并创建当前 scope（119–144）。前端需实现能携 Bearer 头的 fetch SSE、取消、有界解析、退避和重认证；仓库无该 TS 实现。不得把 token 放 URL 来适配原生 EventSource。

Query keys 绑定当前 principal+显式 tenant；resync 后 REST 重取，断流保留最后确认值及更新时间。身份/租户切换和注销清流/cursor/cache/草稿。single-flight、Zustand 草稿、短期 step-up 仍是拟新增设计，研究中的“五分钟”“内存 token”不是已获批准的具体安全参数。

**Testing analog:** `backend/internal/httpapi/sse_test.go`（51–64）用真实 HTTP 请求设置 Bearer 与 Last-Event-ID；`TestSSECommittedEventsReplayScopeAndRESTConvergence`（66–119）验证重放、跨主体 cursor 拒绝、unknown 与 REST 收敛；`TestSSERetentionGapResyncAndReauthentication`（121–166）验证 resync/注销。前端行为用例可复制这些验收断言，不能照搬 Go 的测试会话签发方式到最终浏览器 tracer。

### S3. 显式 SQL 安全投影与精确金额（#14、#22、#25、#31）

**Source:** `backend/db/query/public_resources.sql`。

节点读取（7–13）使用所有权或有效 grant 的 SQL 谓词；规则读取（43–46）目前仅有 `id/desired_revision/active_revision/sync_status/name/protocol/mode/enabled`。没有完整路径/入口地址/逐节点 actual_version/run_status/实测/影响会话数；必须规划真实投影与证据输入，不靠组件推导。

金额摘录（48–50）：

```sql
-- name: GetPublicSourceLedger :one
SELECT jsonb_build_object('source_id',source_id,'balance_bytes',b::text,'outstanding_bytes',f::text,'guarantee_bytes',l::text,'charged_bytes',charged::text,'remainder_milli',remainder_milli,'sequence',sequence::text) AS body
FROM ledger_projections WHERE tenant_id=sqlc.arg(tenant_id) AND source_id=sqlc.arg(resource_id);
```

沿用十进制字符串/精确展示；JS Number 不承担账务计算。F/未知占用不是 confirmed charge，不能跨来源抵销。页面五层事实命名与现有 `*_revision` 数据库字段之间需明确审批后的 DTO 映射，不能简单把 active_revision 改名为“全部已应用”。

### S4. 恢复工作台只推进已有批准（#17、#28、#33、#38）

**Sources:** `backend/internal/service/recovery_advance_job.go`、`backend/internal/service/hub_recovery_orchestration.go`。

当前对外服务签名（recovery_advance_job.go 26）：

```go
func (s ShareService) RequestRecoveryAdvance(ctx context.Context, scope TenantScope, recovery, key, request string) (generated.Operation, error)
```

必须平台 Grant scope，校验恢复ID/幂等键，在审计事务中排持久任务（28–47）。当前协调签名（hub_recovery_orchestration.go 36）：

```go
func (s ShareService) AdvanceHubSameStoreRecovery(ctx context.Context, scope TenantScope, recovery, backend, request string) (RecoveryProgress, error)
```

该文件19–35明确 `RecoveryProgress` 只描述已提交 PG 证据，最多32个缺失命令/次，消费已有 explicit approval，没有网络 I/O、不批准 boot/epoch。`admitted` 返回（118–122）不能证明本地 Hub FULL 或 Engine 转发。恢复 UI 应分别展示 PG 编排、Hub/Agent 本地提交、原委托续签、逐租户新发放和完整对账。

Agent/Engine 接点是已跟踪的 `agent/engine_apply.go`：`EngineApplier.Prepare(id [16]byte, generation uint64) (any, error)`、`Commit(id [16]byte, unknown bool) (any, error)`、`Abort(id [16]byte) (any, error)`；属于进程内适配，不能从浏览器直接调用或让 Engine 查 PG。Agent/Hub 继续持久化本地恢复/用量证明；后端和 Hub 不承载业务流量。

六步物理恢复、独立单调 generation、加密备份与旧签发隔离没有完整代码 analog。规划必须补受控部署/故障演练；不能由旧 PG generation+1、网页确认或协调 stage 制造成功证明。

### S5. SQL 源、Goose 追加与生成物（#3、#30–32）

**Sources:** `backend/db/sqlc.yaml`（1–11）、`backend/db/migrate.go`（12–38）、`backend/db/migrations/00009_domain_events.sql`。

```yaml
version: "2"
sql:
  - engine: postgresql
    queries: query
    schema: migrations
    gen:
      go:
        package: generated
        out: generated
        sql_package: pgx/v5
        emit_empty_slices: true
```

迁移以 `-- +goose Up` 开始（00009 第1行），多语句函数用 `StatementBegin/StatementEnd`（68–79）。`db.Migrate(ctx, url)` 是显式管理命令，`MigrateTo` 支持旧 schema 升级验证，不由 server startup 执行。既有迁移1–9保持字节不变，只追加新迁移；从迁移生成 schema/sqlc，做新建库、旧版升级和两次干净生成 diff。

TS 类型生成仅复制“唯一源→受控生成物→无漂移检查”的组织模式。OpenAPI唯一源是 `contracts/openapi/v1/openapi.yaml`，`backend/openapi/v1/openapi.yaml` 为副本；应用 TS7 与研究推荐生成器 TS5 工具区仍需真实兼容探针及依赖审查，不手工维护第二份请求模型。

### S6. 目标策略在真实解析/建连边界执行（#34）

**Source:** `engine/vlink/target.go`（37–39、59–77、92–101）。

```go
// ResolveLocalTarget resolves once, validates every answer, and returns a
// numeric endpoint so the eventual dial cannot trigger a second DNS lookup.
func ResolveLocalTarget(ctx context.Context, target LocalTarget, resolver TargetResolver) (netip.AddrPort, error)
```

实现逐个 `Unmap()` 后校验所有解析答案，再返回数值 AddrPort；`DialLocalTarget(ctx, network, endpoint)` 连接数值地址并再次检查。复制 resolver 注入和全答案校验的思路。

这是局部 helper，不是 SEC-011 全部路径已覆盖的证明。当前 `AllowedPrefixes` 是额外收紧条件，不能绕过 `forbiddenAddress` 开自托管例外。新策略必须纳入 compiler、Agent 配置、Engine Manager 的静态/远端 TCP/UDP/OPEN 每次真实建连链路，覆盖硬禁控制端口及有限私网例外；runtime/control 变更遵循 S8。不要只增加 helper 单测就勾选完成。

### S7. 保留须凭终态证据，不是通用 TTL（#35）

**Source:** `agent/store/usage_retention.go`（39–56、80–92）。

```go
// RecordUsageDispute retains conflicting bytes indefinitely. There is deliberately
// no automatic resolution API: retries, expiry and ACKs cannot clear a dispute.
func (s *Store) RecordUsageDispute(ctx context.Context, node, batch string, payload []byte) error
```

`PruneSettledUsage(ctx context.Context, now IssuerTime, limit int) (int, error)` 只供 Hub，校验可信时间/批量上限；查询同时要求 backend receipt、confirmed journal、checkpoint、无前缀洞、无 dispute/unknown。墓碑与删除在同一写事务提交。其七天常量是特定已结清重复载荷的历史规则，不是阶段7普通日志/任务/流量/审计保留期限。

后端30/90/365天策略须建立分类与 hold：未完成/未结算/争议/F/恢复证明不被普通时间清理；批量幂等、保留原始账本及 tombstone 等边界需真实反例验证。SSE事件重放保留（`backend/db/query/domain_events.sql`）也不能当审计或资金证据清理模板。

### S8. 具体提案与历史冻结门禁（#1、#3、#30、#34、#38–39）

**Source:** `tools/phase6/rest_contract_check.py`（32–45、64–94）；治理原文 `docs/governance/contract-review.yaml` 第70行要求规范改动经 owner approval、SemVer、requirements-map 和 changelog。

当前冻结基线：database 2.4.0、public-api 3.0.0、control 1.6.0、runtime-config 1.2.0、VLink 1.0.0、authorization 1.2.0。32项交互选择不替代具体规范制品批准。

历史检查器校验精确 proposal hash、候选清单、旧 approval 字节与批准记录；包含历史 `assert len(migrations)==9`（86–88）。第7阶段应新增适配新变更的检查器并验证历史不变，不删除旧断言或重写 CR-009 hash 使新迁移通过旧门禁。

前端不存在已安装基线。精确版本以当前 `docs/governance/version-lock.yaml` 为准，**Vite 是7.3.7**，不是 AGENTS 早期表的8.x候选。新增 SWC/DOM peer/生成器/TOTP/age 等先形成完整版本/来源/integrity/许可/安装脚本及兼容证据，再完成具体批准和安装；研究的 SUS/null 查询降级不等于包不存在或安装获准。

`07-UI-SPEC.md` 当前缺失，设计契约与 UI safety gate 仍须独立完成。本文件没有替代 gate 或要求用户提前批准尚未成形的方案。

### S9. 测试与证据不能跳过实际权威服务

**Source:** `backend/internal/testutil/postgres.go`（12–18）：

```go
// Postgres always fails when the real database is unavailable. No skips or
// in-memory replacements may turn a missing authority service into a pass.
func Postgres(t *testing.T) *pgxpool.Pool {
    t.Helper()
    url := os.Getenv("PHASE6_DATABASE_URL")
    if url == "" { t.Fatal("PHASE6_DATABASE_URL required; run tools/phase6/tracer_real.sh") }
```

沿用一次性隔离 PG、显式迁移和 cleanup；新阶段入口兼容现有 fixture 变量或有计划地扩展，不能误指现有业务库。浏览器最终 tracer 必须真实登录→预览/CAS→PG任务→Agent/Engine→SSE→REST→UI，同时覆盖 unknown/冲突/失联和当前权限撤销。组件 mock 可验证渲染，但不构成页面真实 API 或恢复演练通过。

## No Analog Found

| 文件/模块单元 | Role | Data Flow | 原因 / planner 的下一步 |
|---|---|---|---|
| #1–2 前端工程/provider/路由 | config / provider | transform / event-driven | `frontend/` 不存在；先完成精确工程候选、依赖闭包与 UI-SPEC |
| #6–8 hooks/store/components/CSS | hook / store / component | request-response / event-driven | 无 Query/Zustand/AntD 实现；以研究建议+批准设计契约建立首套模式 |
| #9–17 业务页面 | component | CRUD / request-response | 无页面模板；每片同时交付后端投影、授权、任务和真实证据，不复制假成功 |
| #18 前端行为/浏览器工程 | test | request-response / streaming | 无 Vitest/Playwright 配置；用 S2/S9 的行为断言建立真实前端验证 |
| #38 物理备份/密钥/独立 generation | service / config | file-I/O / batch | 无完整物理灾备；研究路线只是待批准设计，须实际 PG18 演练 |
| #40 六步恢复手册及覆盖矩阵 | config / test | batch / file-I/O | 无本阶段成品；明确每页/每角色/API/任务/证据以及未跑状态 |

**仅部分匹配也要保留缺口：**#20 没有完整密码/TOTP/恢复码实现；#26 没有通知生命周期模型；#23 没有完整实测/影响预览公共读模型；#35 没有完整 PG 分类 hold-aware 清理。这些不能因已有通用 service/worker 模式而标记能力已存在。

## Metadata

**Analog search scope:** Git 跟踪的 `backend/internal/`、`backend/db/`、`agent/`、`hub/`、`engine/`、`contracts/`、`tools/phase6/`、`deploy/`、`.github/workflows/`；确认无 `frontend/`，无本阶段 UI-SPEC。

**Project context:** 已读取根 `AGENTS.md`；`.claude/CLAUDE.md`、`.codex/skills/`、`.agents/skills/` 未发现。本次是既有 GSD plan-phase 委派的只读模式分析，仅产出此文件；未启动额外执行流程。

**Files scanned:** 以40个目标职责单元分类；五组主要 analog 深读，加上认证、scope、SSE、SQL、恢复、目标策略、保留、迁移及测试支持文件。不是遍历统计，也不把 glob 内未读文件声称为已分析。

**Pattern extraction date:** 2026-11-10（Asia/Shanghai）。所有摘录/签名来自当前工作区跟踪源码；无源码、依赖、分支、STATE/ROADMAP 修改；本文件不是运行验收证据。
