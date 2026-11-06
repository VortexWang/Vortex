# Phase 6: M4 后端业务闭环 - Pattern Map

**Mapped:** 2026-11-07
**Files analyzed:** 17 个计划模块/契约与验证文件
**Analogs found:** 14 / 17（其中 8 个为角色/边界类比；当前仓库没有 `backend/` 实现）

## File Classification

| New/Modified File | Role | Data Flow | Closest Analog | Match Quality |
|---|---|---|---|---|
| `backend/go.mod` | config/module | request-response | `go.mod` | role-match |
| `backend/cmd/vortex-backend/main.go` | command/entrypoint | request-response | `cmd/vortex-test-hub/main.go` / `hub/testhub.go` | role-match |
| `backend/internal/auth/` | auth service | request-response | `agent/control.go` TLS identity checks | partial |
| `backend/internal/policy/` | policy service | CRUD/transform | `agent/authorization_gate.go` | role-match |
| `backend/internal/repository/` | repository | CRUD/transaction | `agent/store/store.go` | role-match |
| `backend/db/query/` | SQL query source | CRUD/transaction | `agent/store/store.go` SQL statements | role-match |
| `backend/db/migrations/` | migration/config | batch | `agent/store/store.go` schema setup | role-match, not SQL dialect analog |
| `backend/internal/service/` | domain service | CRUD/transaction | `agent/store/authorization.go`, `agent/store/handoff.go` | role-match |
| `backend/internal/compiler/` | utility/service | transform | `contracts/unknown_usage.go`, `engine/runtime/config/config.go` | role-match |
| `backend/internal/worker/` | worker | event-driven/batch | `hub/recovery.go`, `hub/usage.go` | role-match |
| `backend/internal/control/` | control adapter | streaming/request-response | `agent/control.go`, `hub/testhub.go` | exact boundary |
| `backend/internal/httpapi/` | controller/transport | request-response/streaming | `hub/testhub.go` gRPC serving and `hub/status.go` validation | partial |
| `backend/openapi/v1/openapi.yaml` | contract/config | request-response | `contracts/openapi/v1/openapi.yaml` | role-match (draft only) |
| `contracts/database/schema.sql` | schema contract | CRUD/transaction | `agent/store/store.go` durable tables | role-match |
| `contracts/openapi/v1/openapi.yaml` | public contract | request-response/streaming | same file (currently empty `paths`) | no implementation analog |
| `tools/phase6/check.sh`, `.github/workflows/phase6.yml` | validation/CI | batch | `tools/phase5/check.sh`, `.github/workflows/phase5.yml` | exact role |
| `backend/**/*_test.go` | tests/fixtures | CRUD/streaming/fault | `agent/store/*_test.go`, `hub/*_test.go`, `contracts/*_test.go` | role-match |

## Pattern Assignments

### `backend/internal/repository/` + `backend/db/query/`（repository, CRUD/transaction）

**Analog:** `agent/store/store.go`（tracked，SQLite durable authority）

**事务与回滚模式**（`agent/store/store.go:927-976`）：

```go
tx, err := s.db.BeginTx(ctx, nil)
if err != nil { return err }
defer tx.Rollback()
// 先读取现有 identity，比较 snapshot/payload/digest；冲突返回稳定错误。
// INSERT 业务事实后才提交。
if err := tx.Commit(); err != nil { return err }
```

**幂等与冲突模式**（`agent/store/store.go:945-976`）：同一 `(node_id,batch_id)` 先读已有 `snapshot_id/payload/payload_sha256/unknown_upper_bound`；全部相同则 replay 成功，任一字段不同返回 `ErrUsageBatchConflict`，禁止覆盖原事实。后端 SQL 应把这一模式提升为 `(node_id, boot_id, sequence)` 的唯一约束，并在 repository API 中显式传入 `TenantScope`。

**持久化事件模式**（`agent/store/store.go:1083-1113`）：

```go
tx, err := s.db.BeginTx(ctx, nil)
defer tx.Rollback()
err = tx.QueryRowContext(ctx,
  `SELECT owner,kind,payload FROM durable_events WHERE event_id=?`, eventID).
  Scan(&oldOwner, &oldKind, &oldPayload)
if err == nil {
    if oldOwner != s.owner || oldKind != kind || !bytes.Equal(oldPayload, payload) {
        return ErrEventConflict
    }
    return tx.Commit() // exact replay
}
// INSERT only after no prior identity; commit is the ACK boundary.
```

后端实现应使用 `pgx/v5` + sqlc 生成代码，查询源文件必须保留 `tenant_id = $scope`，平台跨租户路径要单独命名并由 policy/audit 保护。Repository 测试覆盖双租户负向读取、digest 冲突和事务回滚。

### `backend/db/migrations/` + `contracts/database/schema.sql`（schema/migration, CRUD）

**Analog:** `agent/store/store.go:152-456` 的显式表、唯一键、状态字段和迁移元数据；**没有** PostgreSQL/Goose 现成类比。

可复制的约束意图：`store.go:163-179` 以主键保存 durable identity；`store.go:238-260` 把 `snapshot_id`、payload、digest、upper bound、state 分开；`store.go:446-456` 通过版本元数据和触发器维护 ledger 记录。Phase 6 应改成 Goose 有序、前向迁移，把 identity/scope、资源 grant、revision/snapshot、ledger/source、share/task/outbox/domain-event 分层；使用 PostgreSQL `UNIQUE`、`CHECK`、部分唯一索引与外键，不在应用启动时自动跑迁移。

### `backend/internal/service/`（service, CRUD/transaction）

**Analogs:** `agent/store/authorization.go`、`agent/store/handoff.go`。

**授权读取与多边界校验**（`agent/store/authorization.go:31-49,93-161`）：先开启短事务，读取持久化记录，再同时比较 `boot_id`、`kind`、`rule_id`、`rule_revision`、`path_generation`、`billing_snapshot_id` 和可信时间窗；`signer_epoch` 与 `path_generation` 分开检查（`124-136`）。后端 service 应在 repository 行锁后执行同样的 scope、revision、snapshot、generation 校验，拒绝 stale write，而不是 last-write-wins。

**Handoff 的同事务边界与幂等 request key**（`agent/store/handoff.go:30-76`）：读取 `handoff_id` 已存在请求；相同 JSON 返回之前结果，内容不同返回冲突；新请求在同一事务中校验 epoch/heartbeat、撤销 delegation、写 handoff/transfer/ledger/recovery 状态后提交。该模式直接适用于 entitlement redemption、rule edit、share CAS 和 receipt ingestion：业务事实、audit、task、outbox 必须同一 PostgreSQL 事务提交。

### `backend/internal/policy/` + `backend/internal/auth/`（policy/auth, request-response）

**Analogs:** `agent/authorization_gate.go:16-40,63-81` 与 `agent/control.go:130-174`。

`AuthorizationGateConfig` 明确把 `authorization_id`、`rule_revision`、`path_generation`、`snapshot_id`、trusted clock 作为不可省略输入（`agent/authorization_gate.go:20-40`）；`Open` 先以持久化检查结果决定是否准入（`73-81`）。后端 policy 需把 principal、tenant、固定角色和 resource/action scope 传到 service/repository，不能只靠 Gin middleware 或前端过滤。

控制身份边界可复制 `agent/control.go:130-174`：强制 TLS 1.3、拒绝 `InsecureSkipVerify`、要求 verified chain/peer identity，并限制 gRPC 消息大小（`168-172`）。用户 Bearer/refresh token 与 Agent/Hub mTLS 分离；敏感操作带 `request_id` 并和 audit 同事务写入。

### `backend/internal/compiler/`（compiler, transform）

**Analogs:** `contracts/unknown_usage.go:16-48`、`engine/runtime/config/config.go:98-177`、`engine/runtime/accounting/book.go:11-55`。

**确定性编码与校验**（`contracts/unknown_usage.go:16-30`）：先严格校验 UUID/枚举/上界/未知 protobuf 字段，再用 `proto.MarshalOptions{Deterministic:true}`；解码后重新编码并逐字节比较（`33-48`）。Compiler 应对 revision/grant/member/snapshot 做规范化排序，canonical JSON 后 SHA-256，保存 input versions、artifact digest 和 diagnostics；任何 invalid path/permit 产生稳定诊断，不能直接下发。

**不可变 billing binding**（`engine/runtime/accounting/book.go:11-55,112-120`）：`Binding` 同时保存 `RuleID`、execution node、`PathGeneration`、policy revision、coefficient；`BookConfig.Validate` 要求 active snapshot 存在、每个 snapshot ID 与 credit 一致且资金预留不溢出；`Open` 将会话永久绑定原 snapshot，即使后续 selection 改变。后端 compiler/ledger 必须以 `billing_snapshot_id` 绑定历史结算，不能读最新 package/catalog。

### `backend/internal/worker/`（worker, event-driven/batch）

**Analogs:** `hub/usage.go:14-18,27-40,80-110`、`hub/recovery.go:10-18,32-72`。

`UsageBackend` 是显式 adapter 接口，`AcceptKnown/AcceptUnknown` 先把 deterministic payload 写入 durable store，再在 `ackBoundary` 后调用 backend；收到 digest 后再 `ConfirmBackendUsage`（`hub/usage.go:27-40,80-93`）。Phase 6 worker 应把 task lease/outbox delivery 与 receipt handling 分开：lease 事务 `FOR UPDATE SKIP LOCKED` 后提交，再进行外部 Agent/Hub I/O；确认事务按 task key/idempotency key/target revision 去重。

`ReplayPending` 按持久化顺序逐项转发（`hub/usage.go:95-111`）；`ReplayLedger` 有界读取、确认后才继续，并在尾部错误时仍调用 `CompleteRecovery`（`hub/recovery.go:32-72`）。后端 timeout/disconnect 必须进入 `unknown`，重试/对账不能释放 port/share/source occupation。

### `backend/internal/control/`（control adapter, streaming/request-response）

**Analog:** `agent/control.go` + `hub/testhub.go` + `contracts/protobuf/v1/vortex.proto`。

Agent 控制流在 `agent/control.go:168-225` 使用 mTLS gRPC 双向流、固定消息大小、持久化 sequence、首个 heartbeat 和 lifecycle hello；`hub/testhub.go:95-143` 验证 TLS 1.3、证书身份、node/boot/sequence，并在回复前 `RecordReceipt`。生产 adapter 只能映射现有 `NodeControl`/`HubControl` Protobuf，不把 wire/runtime config 暴露到 REST；后端需有 fake adapter 做单测、真实 mTLS 集成测试做门禁。

`contracts/protobuf/v1/vortex.proto:10-50,100-177` 是契约边界：Node/Hub 消息带 `node_id`、`boot_id`、sequence、digest、revision；`signer_epoch`、`path_generation`、`fencing_generation` 是独立字段。控制 adapter 的 receipt 必须匹配 revision/generation，迟到结果不得推进新状态。

### `backend/internal/httpapi/` + `backend/openapi/v1/openapi.yaml` + `contracts/openapi/v1/openapi.yaml`（controller/contract, request-response/streaming）

**Analog:** `hub/status.go:12-30,51-58` 的输入校验与 protobuf 状态快照；`contracts/openapi/v1/openapi.yaml:1-33` 仅是 draft。

`hub/status.go` 先拒绝错误版本、region、session、capabilities 和未知字段，再读取已提交状态快照并复制到 response（`14-30`）；发送 status 时生成 message ID、递增 sequence、clone payload（`51-58`）。HTTP handler 应采用同样的“验证 -> service snapshot -> 安全 envelope”顺序，统一返回 `{code,message,request_id,field_errors?}`；mutation 返回 `operation_id` 与 accepted desired state。

OpenAPI 当前 `paths: {}`（`contracts/openapi/v1/openapi.yaml:1-33`），没有可复用 endpoint analog。新契约只暴露 Tenant/Node/Rule/Package/Subscription/Usage/Restriction/Share/Operation/Audit 等业务模型；runtime JSON、VLink/Protobuf wire payload、凭据和内部 target 不进入公共 schema。SSE 需用 PostgreSQL durable event cursor，支持 `Last-Event-ID`，gap 时发送 resync/要求 REST refetch；不得使用进程内 subscriber map 作为事实源。

### `backend/internal/service/limit` 或 share 相关实现（service, CRUD/transaction）

**Analogs:** `agent/store/share_occupancy.go:8-53`、`engine/apply/apply.go:15-24,49-130`、`engine/runtime/accounting/share.go:24-68`。

`HoldRuntimeShares` 在一个事务中检查 fencing/state、按链逐层读取 share，并以 `ON CONFLICT DO NOTHING` 固定 holds（`share_occupancy.go:12-53`）；注释明确 activation unknown 时保留历史 revisions。`engine/apply.Manager` 的状态机把 `Prepared -> Active`、`ActivationUnknown`、`Aborted`、`Released` 分开，查询缺失不是 inactive 证明（`apply.go:49-130`）。后端 share CAS 应采用 prepare successor -> 关闭旧 renewal authority -> durable CAS -> receipt -> active，丢失 receipt 维持 unknown/双重占用。

### `backend/internal/control` 的凭据/配置映射（control/config, transform）

**Analog:** `engine/runtime/config/config.go:53-64,98-113,115-177`。

`ShareConfig` 把 `boot_id`、`transaction_id`、revision、rate/connections、lease deadline 分开；`LoadFile` 使用 `DisallowUnknownFields`、拒绝多 JSON 文档并立即 `Validate`。后端 artifact serializer 需同样拒绝未知字段、边界值和缺失 lease，且将 backend desired config 与内部 Engine 配置隔离；仅已提交 artifact 才能创建 task。

### `tools/phase6/check.sh` + `.github/workflows/phase6.yml`（validation/CI, batch）

**Analogs:** `tools/phase5/check.sh:1-35,63-74`、`tools/phase5/contracts_check.sh:1-34`、`.github/workflows/phase5.yml:11-24,35-74`、`Makefile:1-10,99-105`。

Phase 5 gate 的可复制约定：`set -eu`、计算 `git rev-parse HEAD`、Linux/clean-source/untracked-source 失败即停、输出带 commit 的 JSON report、检查缺失 evidence 不得通过（`check.sh:1-35,63-74`）；contract check 固定生成器版本并做双次生成 `cmp`（`contracts_check.sh:6-22`）；Actions 固定 `ubuntu-24.04`、action 完整 SHA、Go 版本和 artifact upload `if-no-files-found: error`（workflow `13-24,35-74`）。新增 Phase 6 目标应在 Makefile 中只调用一个同 SHA 入口，并分别保存 migration、cross-tenant、ledger/concurrency、OpenAPI/SSE、Agent/Hub integration 原始证据。

## Shared Patterns

### 事务与审计/任务/Outbox

**Sources:** `agent/store/store.go:927-976`、`agent/store/handoff.go:30-176`、研究中的 pgx 模板。所有 business mutation 先锁 authority rows、检查 scope/expected revision，再在同一事务写 immutable facts/projection、audit、task、outbox，commit 后才返回 `operation_id`。禁止在数据库事务中等待 Agent/Hub/DNS。

### 幂等、digest 与未知结果

**Sources:** `agent/store/backend_usage.go:20-89`、`agent/store/unknown_usage.go:74-100,154-175`、`hub/usage.go:80-93`。重复相同 payload/digest 是 no-op；相同 identity 携带不同 bytes/snapshot 记录冲突证据；unknown liability 与 occupation 在 receipt/reconciliation 前保持冻结。`failed` 与 `unknown` 不得合并。

### Generation / snapshot 分离

**Sources:** `agent/store/authorization.go:123-136`、`contracts/protobuf/v1/vortex.proto:147-177`、`engine/runtime/accounting/book.go:11-18`。`signer_epoch`、`path_generation`、`fencing_generation`、share revision、billing snapshot 各自持久化和比较；新 boot 不是同链 CAS 快捷方式。历史 session/grant 继续使用原 snapshot，后续配置选择不回写旧账。

### 控制边界

**Sources:** `agent/control.go:130-174`、`hub/testhub.go:95-143`、`contracts/protobuf/v1/vortex.proto:10-50`。控制平面使用 TLS 1.3/mTLS、bounded gRPC、sequence/digest/revision；业务 TCP/UDP 流量仍由 Engine 独立模块承载，backend 不承载数据面。

### 测试证据

**Sources:** `tools/phase5/check.sh:23-35,63-74`、`.github/workflows/phase5.yml:35-74`。测试必须包含 Go unit/race、真实 PostgreSQL migration/concurrency、双租户 negative scope、compiler golden digest、unknown/lost receipt、真实 Agent/Hub mTLS，以及本地 Docker 与 Actions 同一 SHA 的原始日志。

## No Analog Found

| File/Module | Role | Data Flow | Reason |
|---|---|---|---|
| `backend/internal/auth/` 的 Bearer/rotating refresh token | auth | request-response | 现有代码只有 mTLS/证书身份；没有用户 token/refresh family 实现，需先锁定标准库/依赖和 hash/rotation 语义。 |
| `backend/internal/repository/` 的 PostgreSQL/sqlc tenant wrapper | repository | CRUD/transaction | 只有 SQLite 手写 SQL；没有 pgx/sqlc 生成代码或 TenantScope helper。必须新增 SQL 查询、scope lint 和真实 PG 测试。 |
| `backend/db/migrations/` Goose | migration | batch | `agent/store/store.go` 是启动时 SQLite 建表，不是 Goose/PG migration。不能把启动 DDL 当生产迁移类比。 |
| `backend/internal/httpapi/` REST cursor/error/SSE | controller | request-response/streaming | 现有仓库没有 Gin HTTP API、cursor、SSE replay 或 Last-Event-ID。`hub/status.go` 只能提供校验/快照顺序类比。 |
| `backend/internal/compiler/` 的完整 rule revision -> per-node artifact | compiler | transform | 现有 deterministic protobuf 和 runtime config 校验可复用，但没有跨 tenant grant/member/permit/billing snapshot 的纯 compiler。 |
| `backend/internal/worker/` PostgreSQL `SKIP LOCKED` lease/outbox | worker | event-driven | Hub replay 是顺序 durable adapter，不是多消费者 PG lease；需要新增 lease SQL、backoff、unknown reconciliation。 |
| `contracts/openapi/v1/openapi.yaml` 的完整路径/业务 schema | contract | request-response | 当前 `paths: {}`，仅有 bearer scheme 与 `ApiError` 草稿；所有 endpoint/schema/status/pagination 需首次设计。 |

## Metadata

**Analog search scope:** `agent/store/`, `agent/control*.go`, `agent/authorization*.go`, `hub/`, `contracts/`, `engine/runtime/`, `engine/apply/`, `tools/phase5/`, `.github/workflows/`, `Makefile`
**Files scanned:** 28 个 tracked 源文件/契约/脚本
**Pattern extraction date:** 2026-11-07
**重要边界:** `hub/testhub.go`、`hub/usage.go`、`hub/recovery.go` 的注释明确它们是 Phase 5 protocol-test fixture/adapter；规划生产 backend 时只能复制 durable/idempotency 语义，不能把 fixture 当 production authority。
