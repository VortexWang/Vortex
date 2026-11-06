# Phase 6: M4 后端业务闭环 - Research

**Researched:** 2026-11-06  
**Domain:** Go/Gin + PostgreSQL/pgx/sqlc/Goose authoritative business backend  
**Confidence:** HIGH（现有契约和项目决策）；MEDIUM（尚未存在的后端模块布局和 API 生成工具）

<user_constraints>
## User Constraints (from CONTEXT.md)

### Locked Decisions

- **D-06-01:** V1.0 uses a single-tenant user model. A normal user has one `tenant_id`; platform administrators are platform-scoped principals and cross-tenant actions are audited. Multi-tenant membership switching and team invitations remain out of scope. — **Reversibility:** costly — changing the identity and session model after API and database adoption requires a membership migration and revised authorization semantics.
- **D-06-02:** Use fixed roles plus explicit scope policies: platform `platform_admin`, `operator`, `auditor`; tenant `owner`, `member`. Service and repository layers enforce resource/action policy; the frontend is never the authority. — **Reversibility:** costly — role names and policy checks become API and audit compatibility points.
- **D-06-03:** Sensitive reads and mutations, including cross-tenant reads, permission/credential/node/rule/entitlement/ledger changes, append an immutable audit event in the same PostgreSQL transaction. Record actor, scope, action, resource, `request_id`, before/after summaries, and reason; never record secrets. — **Reversibility:** costly — moving audit out of the transaction would change the durability and compliance contract.
- **D-06-04:** Platform nodes and node groups remain platform-owned. Tenants use them through explicit grant records with validity, status, and version; rule creation and compilation validate the grant, and revocation preserves history. — **Reversibility:** costly — ownership and grant history cannot be reconstructed safely from copied tenant resources.
- **D-06-05:** Rule edits create immutable `rule_revision` records. Mutations require `expected_revision`/`If-Match`; stale writes return a stable conflict error rather than last-write-wins. — **Reversibility:** one-way — revision identifiers and optimistic-concurrency behavior become public API semantics.
- **D-06-06:** The rule compiler is a deterministic pure transformation. It consumes the rule revision, grants, node-group membership, segment permits, and billing snapshot and produces immutable per-node artifacts with input versions, output digest, and diagnostics. Runtime configuration accepts only committed artifacts. — **Reversibility:** one-way — artifact digests and replay inputs become the authority for deployment and audit.
- **D-06-07:** A shared dependency change marks affected rules and schedules recompilation asynchronously. The previously active revision remains in force within its original authorization boundary until the new artifact completes the existing two-phase application. — **Reversibility:** costly — changing this to immediate invalidation would alter availability and authorization behavior during dependency updates.
- **D-06-08:** Persist tasks per rule revision. Pending obsolete tasks may become `superseded`; in-flight tasks are not force-killed. Only a successful receipt for the matching revision can update the current execution state. — **Reversibility:** costly — task history and late receipts depend on revision-scoped identity.
- **D-06-09:** The package catalog may be edited, but an active subscription keeps an immutable package snapshot. New subscriptions or an explicit package change create a new snapshot; historical settlement and existing sources never read a newly edited catalog value. This preserves `MTR-016`, `PLN-006`, and `PLN-008`. — **Reversibility:** costly — snapshot references are required to explain historical balances and settlements.
- **D-06-10:** Entitlement-code redemption is one idempotent PostgreSQL transaction: lock the code by redemption/request key, validate applicability and remaining uses, write the redemption record, and create the subscription or independent traffic-source record. Any failure rolls back all effects. — **Reversibility:** costly — redemption keys and source creation must remain replay-safe.
- **D-06-11:** Account state remains separate from business restrictions. Each restriction cause produces an immutable event with source and effective time, while a current-active projection supports fast checks; service resumes only after all active restrictions are cleared. — **Reversibility:** costly — retention of first-effective and clear history is needed for the seven-day port and restriction semantics.
- **D-06-12:** Package replacement or downgrade starts with a preview of lost capabilities, invalid rules, discarded amounts, and affected resources. User confirmation then creates the new snapshot and restrictions transactionally, with an explicit `effective_at`. — **Reversibility:** reversible — the preview/confirm boundary can evolve without changing historical source records.
- **D-06-13:** PostgreSQL stores immutable ledger events; balance and available-authority views are rebuildable projections. Every non-reusable source tracks its own conservation values, and unknown liabilities remain frozen. — **Reversibility:** one-way — changing to mutable aggregate-only balances would lose the evidence needed for conservative recovery.
- **D-06-14:** Usage ingestion is idempotent on `(node_id, boot_id, sequence)`. Raw cumulative usage, billable usage, final receipts, and unknown upper bounds are separate facts; duplicate reports do not deduct twice. — **Reversibility:** one-way — the tuple is the durable replay identity from Agent/Hub into the authority.
- **D-06-15:** Reservations, consumption, settlement, disputes, and unknown liabilities reference an immutable `billing_snapshot_id`. Each source keeps separate `B_s`, `F_s`, and `L_s` conservation; no cross-source netting and no recalculation from the latest path are allowed. — **Reversibility:** one-way — snapshot and source IDs define accounting provenance.
- **D-06-16:** Rate/connection shares use a linear generation chain keyed by `(tenant_id, node_id, kind)` and bound to `boot_id`. Handoffs follow prepare successor, close all old renewal authority, durable node CAS, receipt confirmation, and only then release the difference; missing closure proof retains old ownership. — **Reversibility:** one-way — generation and fencing semantics are shared with Phase 5 and cannot be safely rewritten by a later aggregate update.
- **D-06-17:** Business changes, task rows, and Outbox events commit in one PostgreSQL transaction. An uncommitted business change cannot produce a consumable control event. — **Reversibility:** one-way — the transaction boundary is the reliability contract for all downstream workers.
- **D-06-18:** Delivery is at-least-once. Handlers deduplicate by task key, idempotency key, and target revision; duplicate delivery is acceptable, duplicate business effect is not. Do not claim distributed exactly-once. — **Reversibility:** costly — changing delivery semantics would require changing all workers and replay evidence.
- **D-06-19:** Persist execution states including `pending`, `leased`, `running`, `succeeded`, `failed`, and `unknown`. Timeout, disconnect, or lost receipt enters `unknown` and must converge by query/reconciliation; it is never assumed not executed and cannot release unknown occupation. — **Reversibility:** costly — Phase 5 recovery and activation semantics depend on preserving unknown outcomes.
- **D-06-20:** Rule application, DNS operations, and node orchestration return an asynchronous `operation_id` and accepted desired state. Workers and Agent/Hub receipts update execution status; the backend never carries business traffic. — **Reversibility:** costly — operation IDs and status transitions are consumed by the future frontend and operations phase.
- **D-06-21:** Public API is resource-oriented `/api/v1` REST with OpenAPI as the source contract. It exposes platform business models only; runtime configuration and VLink wire types remain internal. — **Reversibility:** one-way — published route and schema compatibility will constrain later clients.
- **D-06-22:** User API uses short-lived Bearer access tokens with rotating refresh tokens. Agent/Hub control traffic uses a separate mTLS identity boundary; neither credential type is reused for the other plane. — **Reversibility:** costly — token claims, rotation, and control-plane identity are security compatibility points.
- **D-06-23:** Errors use stable machine `code`, user-safe `message`, `request_id`, and optional field errors. Lists use cursor pagination with stable ordering; responses do not reveal internal targets, credentials, SQL, or tenant data outside scope. — **Reversibility:** costly — clients and audit tooling will depend on error and pagination contracts.
- **D-06-24:** SSE emits domain state events with event IDs and supports `Last-Event-ID`. If the event retention window has a gap, the client refetches authoritative REST state; SSE is a delivery hint, not a separate source of truth. — **Reversibility:** costly — replay and recovery behavior must match the durable event cursor and frontend cache invalidation.

### the agent's Discretion

- Research and planning may choose the exact PostgreSQL schema decomposition, Goose migration layout, sqlc query organization, tenant-scope helper API, token implementation details and TTLs, cursor encoding, task lease/backoff values, Outbox polling/notification mechanism, compiler artifact serialization, and concrete API resource names, provided these decisions preserve the locked invariants above.
- Research must define the full database constraints, indexes, retention, transaction isolation, PostgreSQL advisory/row-lock strategy, generated OpenAPI/TypeScript workflow, and same-SHA Docker/Actions evidence. New dependencies require the existing version/license review before adoption.

### Deferred Ideas (OUT OF SCOPE)

- Online payment and automatic renewal remain out of scope per `PROJECT.md`.
- Team invitations and multi-tenant membership remain out of scope for V1.0.
- Frontend pages, Cloudflare DNS operator UX, notifications, backup/recovery operations, and full browser workflows belong to Phase 7 after the backend contracts stabilize.
- Phase 4 native performance qualification, D-04 approval, and final full-scale acceptance belong to their existing qualification/final-acceptance scope, not Phase 6 implementation.
</user_constraints>

<phase_requirements>
## Phase Requirements

| ID 集合 | 研究支持 |
|---|---|
| `TEN-001..009` | `users.tenant_id` 与平台主体分离、固定角色/作用域策略、账号状态与 restriction 事件分离、同事务审计和平台节点 grant。 |
| `NOD-001..014` | 节点/组/成员/段许可约束、生命周期/排空/删除前依赖检查、平台/租户所有权与组倍率版本。 |
| `PRT-001..007`, `PRT-016`, `PRT-008..015` | 端口资源模型、成员集合快照、租户级占用唯一约束、七日保留/释放确认和 Cloudflare 适配任务边界。 |
| `RUL-001..016` | 不可变 `rule_revision`、确定性编译、路径/目标/段许可校验、Prepare/Commit/Abort、部分应用/unknown/回滚新版本。 |
| `BE-001..006` | PostgreSQL authority、sqlc 仓储租户过滤、任务+Outbox、OpenAPI REST/SSE、对账查询索引。 |
| `PLN-001..008` | 能力交集/最小限制、单主套餐、UTC 月度锚点、独立流量包、权益码原子兑换/预览确认。 |
| `MTR-001..016`, `SUP-B2-001`, `SUP-B5-001` | 执行点计量、千分位系数、来源 `B_s/F_s/L_s` 守恒、快照绑定、未知负债冻结、故障上界及不可变并发配置。 |
| `LIM-001..010` | 独立租约边界、`(tenant_id,node_id,kind)` 份额链、CAS/代次/boot fencing、activation-unknown 和稳定分配。 |
</phase_requirements>

## Summary

Phase 6 的核心交付不是在已有 Hub fixture 上增加一个 HTTP 层，而是建立 PostgreSQL 作为业务与期望配置唯一权威，并把租户、节点、规则、套餐、来源账本、份额所有权和异步执行状态放进可审计的事务边界。当前 `contracts/database/schema.sql` 只有租户、用户、节点、组、成员和段许可六类草稿表，并明确标记缺少 Outbox、快照、grant、reservation、ledger、幂等 usage 与 retention/index 策略 [VERIFIED: `contracts/database/schema.sql:1-64`; quote: “DRAFT 1.0.0; not approved or frozen.” and “exact complete entity/column schemas and retention/index policies must be resolved before this file can be frozen.”]。OpenAPI 仍是 `paths: {}` 的草稿 [VERIFIED: `contracts/openapi/v1/openapi.yaml:1-33`; quote: “paths: {}”]，因此计划必须先冻结数据库和公共 API，而不是直接围绕空契约写业务代码。

Phase 5 已提供可复用的持久化语义和协议适配边界：Agent/Hub 在 SQLite FULL/WAL 下保存授权、配置事务、usage batch、unknown upper bound、`billing_snapshot_id`、`grant_id`、`signer_epoch`、`path_generation`、`fencing_generation` 及 activation-unknown；`hub.UsageRelay`、`LedgerBackend`、`TestServer` 明确属于协议测试/适配器，生产 backend transport 和区域 allocation 仍属于本阶段 [VERIFIED: `.planning/phases/05-m3-agent-edge-hub-m3-agent-and-hub/05-REQUIREMENT-AUDIT.md`; quote: “production backend transport/allocation strategy Phase 6” and `hub/usage.go` comment “The production backend transport belongs to Phase 6.”]。后端应发送现有 Protobuf 控制模型，而不把运行配置或 VLink wire type 暴露为 REST 模型 [VERIFIED: `contracts/protobuf/v1/vortex.proto:10-13,64-121`; quote: “Business forwarding traffic must never use it.”]。

**Primary recommendation:** 先以 Goose 迁移冻结 PostgreSQL 约束/索引和不可变事实表，再用 sqlc 生成只读/写查询并由 tenant-scoped repository 包装；所有业务写入在同一事务中写 audit、task、Outbox，worker 采用 `FOR UPDATE SKIP LOCKED` at-least-once lease；确定性 compiler 只消费已锁定 revision/grant/member/snapshot 并生成 SHA-256 artifact，Agent/Hub 回执按 `(task_key, target_revision, idempotency_key)` 幂等推进 operation；REST/OpenAPI 和 SSE 仅暴露业务模型和可重取的状态提示。

## Architectural Responsibility Map

| Capability | Primary Tier | Secondary Tier | Rationale |
|---|---|---|---|
| 用户、租户、角色和审计 | API / Backend | Database / Storage | 授权策略和跨租户审计必须由服务/仓储强制执行，数据库保存不可变事件。 |
| 节点/组/端口/规则期望状态 | Database / Storage | API / Backend | PostgreSQL 是期望配置唯一事实；服务负责校验、revision 和 operation 编排。 |
| 规则确定性编译 | API / Backend | Database / Storage | 编译器是纯函数，读取固定数据库快照并写 immutable per-node artifacts。 |
| Agent/Hub control delivery | API / Backend | Agent/Hub | Backend 只通过既有 mTLS/gRPC control adapter 发控制消息，Hub/Agent 报回 receipt；不承载业务流量。 |
| 套餐、来源、计量和账本 | Database / Storage | API / Backend | PostgreSQL 保存 immutable ledger/source facts，服务在锁定来源行后更新可重建 projection。 |
| 份额 CAS 和 lease ownership | Database / Storage | Agent/Hub | 后端保存代次链和授权权威；节点持久化最低代次并 CAS，回执前旧占用不能释放。 |
| REST/OpenAPI/SSE | API / Backend | Browser / Client | Backend 发布 `/api/v1` 业务资源；SSE 是带 cursor 的提示，客户端缺口时重新读取 REST。 |
| Valkey cache/notifications | Database / Storage | API / Backend | Valkey 仅可丢失缓存、限流或短通知，不能重建余额、所有权或可靠任务。 |

## Standard Stack

### Core

| Library/Service | Version | Purpose | Why Standard |
|---|---:|---|---|
| Go | `1.27.3` | Backend runtime and build | Project version lock marks it approved and exact; use `go.sum` and `GOTOOLCHAIN=local` [VERIFIED: `docs/governance/version-lock.yaml:19-33`]. |
| Gin | `v1.12.0` | HTTP routing, middleware, REST/SSE response | Project lock already records source, checksum, MIT license [VERIFIED: `docs/governance/version-lock.yaml:35-43`]. |
| pgx/v5 | `v5.11.0` | PostgreSQL driver/pool/transactions | SQL-first driver selected by project, with module checksum and explicit real-PG compatibility gate [VERIFIED: `docs/governance/version-lock.yaml:45-54`]. |
| sqlc | `v1.31.1` | Generate typed queries from explicit SQL | Keeps tenant predicates and lock clauses reviewable; generator artifact is checked in and regenerated in CI [VERIFIED: `docs/governance/version-lock.yaml:56-69`]. |
| Goose | `v3.29.0` | Versioned PostgreSQL migrations | Migration tool is separately version-locked and should run outside app startup [VERIFIED: `docs/governance/version-lock.yaml:71-79`]. |
| PostgreSQL | `18.6` candidate | Business/desired-config authority, ledger, task/outbox | Project stack selects PostgreSQL; actual server/image digest must be locked before implementation [VERIFIED: `AGENTS.md` project stack section; quote: “PostgreSQL 是业务与期望配置唯一事实来源”]. |
| `google.golang.org/grpc` / Protobuf | `v1.85.0` / `v1.36.12` | Existing Agent/Hub control adapter | Reuse frozen `vortex.proto` control boundary; do not invent REST representations of wire messages [VERIFIED: `docs/governance/version-lock.yaml:92-109`; `contracts/protobuf/v1/vortex.proto:1-13`]. |

### Supporting

| Tool | Version/constraint | Use |
|---|---|---|
| PostgreSQL client | target image version, local client may differ | `psql`, `pg_isready`, migration and integration diagnostics. |
| Docker Engine/Compose | host-specific, record output | Same-SHA local integration with PostgreSQL/Valkey and Agent/Hub. |
| GitHub Actions | `ubuntu-24.04`, action full commit SHA | Re-run the exact local entrypoint at the same source SHA; existing workflows pin actions [VERIFIED: `.github/workflows/phase5.yml:15-26`]. |
| OpenAPI generator | **未锁定** | Freeze a generator and license in Wave 0 before adding it; generated frontend types are a Phase 7 consumer. This is `[ASSUMED]` until tool/version review. |

**Installation/lock procedure:** Add only approved Go modules at their exact locked versions; run `go mod tidy`, `GOTOOLCHAIN=local go mod verify`, `go version -m`; install sqlc/Goose from the locked artifacts, record checksums, and run a license/transitive closure scan before commit. Do not install npm packages for the backend.

## Package Legitimacy Audit

No new package is recommended beyond the already approved Go candidates. The package-legitimacy seam in this environment accepts npm/PyPI/crates only; it cannot produce a meaningful Go-module verdict. The authoritative in-repo lock records Gin, pgx/v5, sqlc and Goose with source URLs, checksums, licenses and `status: locked` [VERIFIED: `docs/governance/version-lock.yaml:35-79`]. Treat any additional package, including an OpenAPI generator, as `[ASSUMED]` until its official repository/release, checksum and license are added to the version lock and reviewed.

| Package | Registry/source | Verdict | Disposition |
|---|---|---|---|
| `github.com/gin-gonic/gin v1.12.0` | Go checksum database + official release | OK by project lock | Approved; no npm equivalent. |
| `github.com/jackc/pgx/v5 v5.11.0` | Go checksum database + official module | OK by project lock | Approved; run real PostgreSQL transaction tests. |
| `sqlc v1.31.1` | Official sqlc release artifact | OK by project lock | Approved generator; pin binary integrity per architecture. |
| `github.com/pressly/goose/v3 v3.29.0` | Go checksum database + official release | OK by project lock | Approved migration tool; execute separately from app startup. |
| Any OpenAPI codegen tool | Not selected | `[ASSUMED]` | Human-verify and license-review checkpoint before install. |

## Architecture Patterns

### System Architecture Diagram

```text
Bearer REST request
        |
        v
Gin middleware: request_id -> token -> policy/scope -> service
        |
        v
PostgreSQL transaction
  |-- tenant-scoped business rows / immutable revision or snapshot
  |-- source ledger event + rebuildable projection
  |-- audit event (same transaction)
  |-- task row + transactional outbox row
        |
        +--> worker lease (FOR UPDATE SKIP LOCKED)
                 |
                 +--> deterministic rule compiler -> immutable per-node artifact/digest
                 |         |
                 |         +--> NodeControl/HubControl mTLS adapter
                 |                    |
                 |                    +--> Agent/Hub Prepare/Commit/receipt
                 |
                 +--> usage/unknown receipt ingestion -> source ledger transaction
                 |
                 +--> SSE event cursor (durable event row; REST refetch on gap)
```

### Recommended Project Structure

```text
backend/
├── cmd/vortex-backend/       # process entrypoint, config, migrations command
├── internal/auth/            # token verification, refresh rotation, principal
├── internal/policy/          # role + resource/action scope checks
├── internal/repository/      # sqlc generated queries + tenant-scoped wrappers
├── internal/service/         # tenant/node/rule/package/meter/limit workflows
├── internal/compiler/        # pure deterministic revision -> artifact compiler
├── internal/worker/          # task leasing, outbox relay, receipt reconciliation
├── internal/control/         # Agent/Hub mTLS adapter, Protobuf mapping only
├── internal/httpapi/         # Gin handlers, error envelope, cursor, SSE
├── db/migrations/            # Goose ordered SQL migrations
├── db/query/                 # sqlc SQL with explicit tenant predicates/locks
└── openapi/v1/               # source OpenAPI and generated artifacts
```

### Pattern 1: Transaction template for business + audit + task + Outbox

**What:** A service mutation opens one pgx transaction, locks the authority row(s), validates the caller scope and expected revision, writes immutable facts/projections, inserts audit/task/outbox rows, and commits before returning `operation_id`. The outbox worker may publish only committed rows.

**When to use:** Every rule/package/entitlement/port/share/ledger mutation and every receipt that can release authority.

```go
tx, err := pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
if err != nil { return err }
defer tx.Rollback(ctx)
q := queries.WithTx(tx)
// q.GetTenantScopedRuleForUpdate(...) must include tenant_id and FOR UPDATE.
// q.InsertAuditTaskOutbox(...) writes all durable side effects in this tx.
if err := tx.Commit(ctx); err != nil { return err }
```

PostgreSQL documents Read Committed as the default and recommends explicit row locks for concurrent updates; Serializable requires retry on SQLSTATE `40001` [CITED: https://www.postgresql.org/docs/current/transaction-iso.html]. Use Read Committed plus deterministic lock ordering for ordinary flows, and retry `40001`/deadlock `40P01`; reserve Serializable for a narrowly proven cross-row invariant.

### Pattern 2: Tenant-scoped repository boundary

**What:** Every query that can return tenant-owned data accepts a `TenantScope`/principal and includes `tenant_id = $scope` in SQL; platform-owned rows are joined through explicit grants. Service policy checks action/resource ownership before calling the repository. Add CI checks that reject queries missing the scope parameter or an explicitly documented platform-only path.

**When to use:** All user, node, group, rule, package, usage, source, reservation, ledger, audit and operation reads/writes.

Do not rely on Gin middleware or frontend filtering. `BE-004` explicitly requires repository-level tenant filtering [VERIFIED: `.planning/REQUIREMENTS-SOURCE.md:1036-1041`; quote: “所有租户数据查询在仓储层强制按 `tenant_id` 过滤。”]. PostgreSQL RLS may be a defense-in-depth layer, but cannot replace explicit scope in sqlc queries because platform administrators need audited cross-tenant access and workers need a controlled system principal.

### Pattern 3: Deterministic compiler and immutable artifacts

**What:** Load one consistent database snapshot containing `rule_revision`, grants, node-group membership versions, segment permits, package/entitlement restrictions and billing snapshot. Normalize IDs and ordered paths, sort members/targets deterministically, serialize canonical JSON, hash it, and store one immutable artifact per node/role. Store compiler input IDs/versions and diagnostics beside the digest. Only a committed artifact can create a task.

**When to use:** Rule create/edit, group membership or segment-permit changes, package restriction changes, and policy-version changes.

The compiler must preserve `signer_epoch` versus path `generation`; they are separate counters in the existing authorization/control contracts [VERIFIED: `contracts/protobuf/v1/vortex.proto:147-177`; quote: “signer_epoch”, “path_generation”, and “fencing_generation” are distinct fields]. A dependency change schedules recompilation while the previous committed artifact remains the active boundary until the new two-phase application completes.

### Pattern 4: Task lease and Transactional Outbox

**What:** Insert task and outbox event in the originating transaction. A worker atomically selects due pending tasks with `FOR UPDATE SKIP LOCKED`, sets a bounded lease/attempt, commits, then performs external delivery. Receipt handling is a separate idempotent transaction keyed by task/idempotency/target revision. Timeout or lost response becomes `unknown`; reconciliation queries the same operation and never releases occupation on timeout alone.

**When to use:** Rule application, node orchestration, port reservation/release, Cloudflare DNS, credential/revocation, usage acknowledgement, share CAS and notifications.

PostgreSQL explicitly states `SKIP LOCKED` is suitable for queue-like multi-consumer access but gives an inconsistent view unsuitable for general reads [CITED: https://www.postgresql.org/docs/current/sql-select.html#SQL-FOR-UPDATE-SHARE]. Use it only in the worker lease query with stable `ORDER BY`, not for business invariants.

### Pattern 5: Source-level ledger conservation

**What:** Keep immutable `ledger_events` and separate `funding_sources` with `B_s`, `F_s`, `L_s`, plus reservations, final receipts, disputes and unknown-liability records. Lock the source rows in deterministic source-id order, validate `F_s <= B_s + L_s` and per-source increment `<= A_s = max(0, B_s - F_s)`, append the event, then update rebuildable projections in one transaction. Never net across sources or recompute an old event from current path configuration.

**When to use:** Grant issuance, renewal, usage finalization, unknown upper bound, dispute closure, package expiry/downgrade and risk-budget compensation.

`MTR-004`, `MTR-015`, `SUP-B2-001` and `SUP-B5-001` make unknown liability a continuing `F_s` occupation; expiration, timeout, disconnect or stale heartbeat cannot release it [VERIFIED: `.planning/REQUIREMENTS-SOURCE.md:1144-1149,1214-1225`; quote: “U_bill 继续计入 F 并单列” and “不得让不同快照共享原始额度”].

### Pattern 6: Share CAS handoff

**What:** For `(tenant_id,node_id,kind)`, lock the current chain head; reserve `max(old,new)`, insert successor as prepared/non-consumable, close all old renewal authority, send node CAS carrying minimum generation/boot, persist receipt, then mark successor active and release only the proven difference. A missing response keeps the old and new occupation until query/closure/safe deadline proves release.

**When to use:** Rate/connection allocation, downgrade, node boot change, Hub signer change and bootstrap activation.

The backend must use the existing distinction between `signer_epoch`, path `generation`, and fencing floor. A new `boot_id` cannot be treated as an in-chain CAS shortcut [VERIFIED: `.planning/REQUIREMENTS-SOURCE.md:1249-1295`; quote: “新boot_id不是同链无中断CAS对象”].

### Anti-Patterns to Avoid

- **Mutable aggregate-only balance:** destroys event evidence and cannot explain `B_s/F_s/L_s` or unknown debt; use immutable ledger events plus projections.
- **Latest-catalog reads for active subscriptions:** breaks immutable package snapshots and historical settlement; copy the package snapshot at subscription/change time.
- **Last-write-wins rule edits:** loses stale-write detection and artifact lineage; require `expected_revision`/`If-Match` and return stable conflict code.
- **Compiling directly from live mutable rows:** can mix member/policy versions; load a consistent revision/grant/snapshot set and persist input versions/digest.
- **Task send before commit:** emits control side effects for rolled-back business changes; use transactional Outbox.
- **Deleting or reusing a task on timeout:** timeout is `unknown`, not “not executed”; reconcile by operation/task/revision.
- **Using Valkey as authority:** cache loss must not alter balance, ownership, task confirmation or restriction state.
- **One counter for signer and path generation:** allows stale control updates or wrong authorization lineage; persist and compare them separately.
- **RLS-only tenant isolation:** workers/admins can bypass policy accidentally; retain explicit tenant predicates and scope APIs in every repository query.
- **Long-lived database transactions while waiting for Agent/Hub/DNS:** PostgreSQL warns row locks can deadlock and waits are indefinite [CITED: https://www.postgresql.org/docs/current/explicit-locking.html]; commit the lease before external I/O.

## Don't Hand-Roll

| Problem | Don't Build | Use Instead | Why |
|---|---|---|---|
| PostgreSQL driver/pool | custom wire protocol or generic ORM | `pgx/v5` + sqlc | Existing SQL-first baseline; explicit predicates and lock clauses are reviewable. |
| Schema migration | app-startup ad hoc DDL | Goose ordered migrations | Prevents multi-instance migration races and makes rollback/upgrade evidence explicit. |
| Tenant authorization | UI-only filters or duplicated handler checks | policy service + tenant-scoped repositories (+ optional RLS defense) | Prevents cross-tenant reads through alternate endpoints/workers. |
| Canonical artifact hash | map iteration/JSON default encoding | canonical serializer with sorted keys/IDs and SHA-256 | Required for deterministic replay and receipt matching. |
| Queue semantics | in-memory goroutine channel as durable queue | PostgreSQL task table + Outbox + `SKIP LOCKED` lease | Survives restart and matches at-least-once contract. |
| Money/bytes conservation | floating point arithmetic | integer raw bytes, fixed-point milli coefficient and checked `int64`/numeric | Preserves Appendix B rounding and prevents overflow/rounding drift. |
| SSE replay | process-local subscriber list | durable event cursor + `Last-Event-ID` and REST refetch on gap | SSE is a hint; durable REST remains source of truth. |
| JWT/refresh implementation | ad hoc token parsing/rotation | approved standard Go crypto/JWT library after version/license gate, or a minimal reviewed token signer | Token details are security compatibility; do not add an unreviewed package. |

## Database Design and Transaction Boundaries

The draft schema needs to become a normalized authority model. The planner should split migrations so each dependency is reviewable, but each migration remains forward-only and idempotent at the Goose level:

1. **Identity/scope:** `tenants`, `users`, principal roles, refresh-token families, `audit_events`; check `users.status` against the source enum quote `('active', 'suspended', 'banned')` [VERIFIED: `contracts/database/schema.sql:12-18`].
2. **Node/resource ownership:** `nodes`, `edge_hubs`, credentials/revocation revisions, `node_groups`, `node_group_members` (membership version), `segment_permits`, platform-to-tenant `resource_grants`; enforce platform owner `tenant_id IS NULL`, tenant owner `tenant_id IS NOT NULL`, group purposes `entry/relay/exit`, and multiplier rules from the draft [VERIFIED: `contracts/database/schema.sql:20-59`].
3. **Ports/rules:** port ranges and typed service/link/control/metrics rows; reservation identity should include node, listen address, protocol, port and rule revision; rules are stable IDs, `rule_revisions` are immutable with unique `(rule_id, revision)` and an active pointer.
4. **Billing policy:** package catalog, immutable `package_snapshots`, subscriptions, traffic sources, entitlement codes/redemptions, restriction events/current projection, billing snapshots and policy versions. A subscription must point only to a snapshot, never re-read a mutable catalog row.
5. **Accounting:** `funding_sources` (`B_s/F_s/L_s`), immutable ledger events, reservations, usage batches keyed by `(node_id,boot_id,sequence)`, final receipts, unknown liabilities, disputes, and rebuildable balances. Add unique payload digest constraints to turn conflicting replay into a durable dispute.
6. **Shares and execution:** share chain heads/entries keyed by `(tenant_id,node_id,kind)`, boot/fencing floors, operation rows, per-target tasks, execution receipts, immutable node artifacts, and desired/actual status projections.
7. **Reliability:** `tasks`, `outbox_events`, durable domain events for SSE, retry/lease metadata and retention indexes. Outbox payloads contain only a stable event type, aggregate ID, version and idempotency key; sensitive credentials never enter event payloads.

Recommended transaction rules:

- **Rule edit:** lock rule head and all referenced grants/member-version rows; verify `If-Match`; insert revision, compiler-input snapshot, operation, task and audit/outbox; commit. Compiler reads committed revision and writes artifact/task in a separate transaction; active prior revision remains.
- **Port assignment:** lock the fixed member set and all existing reservations in deterministic node order; compute common allowed port; insert service and UDP reservations (same number but separate protocol rows); rollback every reservation if any node Prepare fails. A DNS serving projection changes only after all required configs/ports are confirmed.
- **Entitlement redemption:** lock code by code ID and idempotency key; validate expiry/applicability/uses; insert redemption and subscription/source in one transaction. Replay with same request returns original result; changed payload conflicts.
- **Usage finalization:** lock source and reservation rows; verify node/boot/sequence, grant and `billing_snapshot_id`; append raw/billable/final facts; move reservation to settled or unknown; update `F_s` and release only proven unused amount. Unknown `U_bill` remains in `F_s`.
- **Share CAS:** lock chain head and source availability; create successor prepared; outbox CAS; receipt transaction checks generation/boot/fencing and updates head. Never release old ownership on timeout or API disconnect.
- **Receipt/application:** lock task/operation target; accept only matching revision and idempotency key; duplicate identical payload is no-op, conflicting payload is `disputed`/stable conflict. `failed` is distinct from `unknown`.

Use explicit constraints and indexes: unique natural replay keys, partial unique current-head indexes, `(tenant_id, created_at, id)` cursor indexes, `(status, next_attempt_at, id)` task index, `(aggregate_type, aggregate_id, event_id)` outbox index, source ledger `(source_id, sequence)`, usage `(node_id, boot_id, sequence)`, and revision/artifact `(rule_id, revision, node_id)`. Retain financial/unknown/dispute/audit facts indefinitely or under an owner-approved retention policy; only prune transport duplicates after settlement, receipt and ledger high-water proof.

## REST/OpenAPI/SSE Contract Guidance

`contracts/openapi/v1/openapi.yaml` must grow from empty `paths` into resource-oriented `/api/v1` endpoints. Keep public schemas business-shaped: `Tenant`, `Node`, `NodeGroup`, `ResourceGrant`, `Rule`, `RuleRevision`, `Operation`, `Package`, `Subscription`, `TrafficSource`, `EntitlementCode`, `UsageSummary`, `Restriction`, `Share`, `AuditEvent`, `ApiError`, and cursor page envelopes. Runtime JSON config and Protobuf messages remain internal [VERIFIED: `contracts/openapi/v1/openapi.yaml:28-33`; quote: “Do not expose internal runtime configuration or VLink wire types as public compatibility commitments.”].

Every mutation returns `operation_id` and accepted desired state (normally `202`); synchronous validation conflicts use stable `code`, safe `message`, `request_id`, and optional field errors. `If-Match`/`expected_revision` is required for revision mutations. Cursor tokens must encode immutable sort tuple (for example `created_at,id`), direction and scope; reject a token issued for another tenant/principal. Do not expose credentials, internal target addresses, SQL, stack traces or cross-tenant rows.

SSE should use a durable domain-event table with monotonically ordered event ID, event type, aggregate ID, revision and minimal safe summary. Set `Content-Type: text/event-stream`, send `id:` and `event:` lines, flush periodically, and honor `Last-Event-ID`. If the requested ID is older than retention or has a gap, send a `resync` event or close with a documented signal; the client must refetch REST state. Gin provides only transport plumbing; event durability and authorization belong to PostgreSQL/service layers. The exact SSE helper and OpenAPI generator are not present in the repository and are `[ASSUMED]` decisions requiring Wave 0 tool/license review.

## Common Pitfalls

### Pitfall 1: Scope bypass in generated queries

**What goes wrong:** A new sqlc query omits `tenant_id`, or a worker uses an unrestricted query helper.  
**How to avoid:** Require scope parameter in every tenant query, code-review generated SQL source, add a static query linter, and run cross-tenant negative tests for every resource.  
**Warning signs:** A repository method accepts only `id` for a tenant-owned row, or platform/admin paths are not explicitly named.

### Pitfall 2: Catalog/snapshot drift

**What goes wrong:** Settlement reads current package/rate/group values and changes historical balances.  
**How to avoid:** Copy immutable `package_snapshot` and `billing_snapshot` IDs into every subscription/source/reservation/usage/final/unknown fact.  
**Warning signs:** SQL joins a current catalog table during usage settlement or compiler replay.

### Pitfall 3: Unknown interpreted as failure

**What goes wrong:** A lost receipt or timeout marks task failed and releases ports, shares or reserved funds.  
**How to avoid:** Persist `unknown`, retain occupation, reconcile by task/revision/operation query, and only release on proof-backed receipt/closure/deadline.  
**Warning signs:** Retry code calls `Abort` after context timeout without querying Agent/Hub.

### Pitfall 4: Cross-source netting

**What goes wrong:** Aggregate `SUM(B)-SUM(F)` authorizes new credit while one source has an unresolved liability.  
**How to avoid:** Lock and validate each source independently, enforce `F_s≤B_s+L_s`, and record per-source deltas.  
**Warning signs:** A single account balance is the only input to grant issuance.

### Pitfall 5: Generation collapse

**What goes wrong:** `signer_epoch`, path `generation`, fencing floor or share generation share one column/counter; late messages overwrite current authority.  
**How to avoid:** Persist and compare each boundary independently; reject lower generation/epoch and preserve old liabilities.  
**Warning signs:** Authorization serializer maps all fields to `generation` or backend treats new boot as safe CAS.

### Pitfall 6: Transaction held across network

**What goes wrong:** Database locks are held while waiting on Agent/Hub, DNS or Cloudflare; workers deadlock or starve.  
**How to avoid:** Commit a leased task before external I/O; receipt/reconciliation uses a new short transaction with bounded retries.  
**Warning signs:** Handler calls gRPC/HTTP while a pgx transaction remains open.

### Pitfall 7: SSE treated as truth

**What goes wrong:** Dropped events leave frontend with stale “running”/“released” state.  
**How to avoid:** Durable event IDs, `Last-Event-ID`, gap detection and authoritative REST refetch.  
**Warning signs:** In-memory subscriber map is the only event store.

## Code Examples

### Tenant-scoped lock query

```sql
-- Source model requires tenant_id on users and tenant-owned resources.
-- Keep the predicate in the query source so generated code cannot omit it.
SELECT id, tenant_id, revision, desired_enabled
FROM rule_heads
WHERE id = $1 AND tenant_id = $2
FOR UPDATE;
```

Use this shape with a service-level scope check. The draft source explicitly gives `users.tenant_id` and tenant-owned rows [VERIFIED: `contracts/database/schema.sql:12-18`; `.planning/REQUIREMENTS-SOURCE.md:68-87`].

### Queue lease query

```sql
WITH next_task AS (
  SELECT id
  FROM tasks
  WHERE state = 'pending' AND next_attempt_at <= now()
  ORDER BY next_attempt_at, id
  FOR UPDATE SKIP LOCKED
  LIMIT 1
)
UPDATE tasks t
SET state = 'leased', lease_until = now() + $1, attempts = attempts + 1
FROM next_task n
WHERE t.id = n.id
RETURNING t.*;
```

`SKIP LOCKED` is for queue consumers only; the lease transaction commits before delivery. PostgreSQL documents the queue use and the inconsistent-view caveat [CITED: https://www.postgresql.org/docs/current/sql-select.html#SQL-FOR-UPDATE-SHARE].

### Idempotent usage identity

```sql
INSERT INTO usage_batches (node_id, boot_id, sequence, billing_snapshot_id, payload_sha256, raw_bytes)
VALUES ($1, $2, $3, $4, $5, $6)
ON CONFLICT (node_id, boot_id, sequence) DO NOTHING;
```

After `DO NOTHING`, fetch and compare the stored digest and snapshot. An equal replay is acknowledged without a second ledger event; a mismatch is stored as a dispute and never silently overwritten. This is the database form of `MTR-006` [VERIFIED: `.planning/REQUIREMENTS-SOURCE.md:1158-1163`; quote: “上报按 (`node_id`, `boot_id`, 序号) 幂等入账”].

### Deterministic artifact envelope

```json
{
  "rule_id": "<canonical uuid>",
  "rule_revision": 12,
  "path_generation": 8,
  "billing_snapshot_id": "<canonical uuid>",
  "node_id": "<canonical uuid>",
  "input_versions": {"grant": 4, "member_set": 9, "permit": 3},
  "payload_sha256": "<lowercase sha256>",
  "objects": []
}
```

The envelope must be canonicalized before hashing and persisted with input versions. Do not reuse `signer_epoch` as `path_generation`; both are separate fields in the frozen control contract [VERIFIED: `contracts/protobuf/v1/vortex.proto:147-177`].

## State of the Art

| Old approach | Current approach | When changed | Impact |
|---|---|---|---|
| Mutable aggregate balance | Immutable ledger events + rebuildable projections + source conservation | Locked D-06-13 / `MTR-004` | Recovery can distinguish confirmed usage, reservation and unknown liability. |
| Last-write-wins rule config | Immutable revisions + optimistic concurrency + deterministic per-node artifacts | Locked D-06-05/06 | Late tasks cannot overwrite a newer revision. |
| In-memory side-effect queue | PostgreSQL task table + Transactional Outbox | Locked D-06-17/18 | A committed business mutation is the only source of a consumable event. |
| Timeout means not executed | Persistent `unknown` + query/reconciliation | Locked D-06-19 and Phase 5 activation semantics | Ports, shares and liabilities stay occupied until proof. |
| One global quota | Per-source `B_s/F_s/L_s`, per-snapshot reservations | `MTR-004`, `MTR-015`, `SUP-B5-001` | No cross-source netting or latest-path recalculation. |
| SSE as live truth | SSE cursor as hint + REST resync | Locked D-06-24 | Dropped/reordered events cannot alter authority. |

**Deprecated/outdated for this phase:** GORM/implicit ORM tenant scope, Redis Streams/Kafka as reliability authority, and exposing runtime/VLink wire payloads through public API are explicitly outside the project baseline [VERIFIED: `AGENTS.md` project constraints and `.planning/phases/06-m4-m4-backend/06-CONTEXT.md`].

## Assumptions Log

| # | Claim | Section | Risk if Wrong |
|---|---|---|---|
| A1 | The backend will be a new `backend/` module/package layout as shown; no existing backend directory was found. | Recommended Project Structure | Plan paths and module boundaries would need adjustment. |
| A2 | `oapi-codegen` or equivalent generator will be selected during Wave 0; no generator is currently locked. | Standard Stack / REST | Unreviewed dependency could violate license/version gate. |
| A3 | `pgx.ReadCommitted` plus explicit row locks is sufficient for most flows; Serializable is limited to proven cross-row invariants. | Transaction boundaries | Incorrect isolation could permit a conservation race; concurrency tests must falsify this assumption. |
| A4 | Canonical JSON/SHA-256 is acceptable artifact serialization; the runtime config schema does not yet prescribe a canonical byte encoding. | Compiler | Different serializer could invalidate digest/replay compatibility. |
| A5 | PostgreSQL 18.6 is the intended server candidate; only local PostgreSQL 16.14 client is installed and no server is running. | Environment | SQL feature/image compatibility must be tested in the pinned PostgreSQL 18.6 container. |

## Open Questions

1. **What exact PostgreSQL 18.6 image digest and migration directory will be locked?**
   - What we know: project stack and roadmap select PostgreSQL authority; the local server probe returned no response.
   - What's unclear: image provenance/digest, extensions, backup/restore test image and CI service topology.
   - Recommendation: make Wave 0 record image digest, `SHOW server_version`, extensions and `pg_dump`/restore commands; do not claim integration until the service is running.

2. **Which OpenAPI-to-Go/TypeScript generator is approved?**
   - What we know: OpenAPI is source contract and frontend types must be generated; current `openapi.yaml` has no paths.
   - What's unclear: generator, template, generated-file ownership and license closure.
   - Recommendation: select and lock one tool before API implementation; add schema lint, generated diff and TypeScript compile checks to CI.

3. **What is the exact control adapter deployment boundary?**
   - What we know: Phase 5 `hub.UsageRelay`, `LedgerBackend`, and `TestServer` are fixtures/adapters; Protobuf `NodeControl`/`HubControl` is frozen.
   - What's unclear: production backend server registration, mTLS CA/identity storage and region routing ownership.
   - Recommendation: plan an explicit `internal/control` adapter with identity verification, bounded receive/send sizes and receipt mapping; keep it behind a fake for unit tests and use real Agent/Hub in integration.

4. **Which retention policy applies to non-financial audit/SSE events?**
   - What we know: financial/unknown/dispute evidence cannot be pruned without proof; SSE supports a retention-gap refetch.
   - What's unclear: exact event retention duration and archive destination.
   - Recommendation: lock a minimum window and document gap behavior; retain event IDs needed by frontend caches and audit review.

## Environment Availability

| Dependency | Required By | Available | Version | Fallback |
|---|---|---|---|---|
| Go | backend build/tests | ✓ | `go1.27.3 darwin/arm64` | Linux Docker/Actions for target semantics. |
| PostgreSQL server | migrations/integration/concurrency | ✗ | `pg_isready`: `/tmp:5432 - no response` | Start pinned PostgreSQL 18.6 Compose service; no fallback for authority tests. |
| PostgreSQL client | diagnostics | ✓ | `psql 16.14` | Use `psql` inside pinned PG image for version-matched checks. |
| Docker/Compose | same-SHA integration | ✓ | Docker `29.8.2`, context `orbstack` | GitHub Actions `ubuntu-24.04`; record host/resource differences. |
| sqlc | query generation | ✗ | command not found | Install exact locked `v1.31.1` artifact in tool image; do not use floating latest. |
| Goose | migrations | ✗ | command not found | Build/use exact locked `v3.29.0` binary in tool image. |
| protoc | existing control regeneration | ✓ but wrong lock | `libprotoc 35.1`; lock requires `36.2` | Use pinned protoc 36.2 container/action before regeneration. |
| Node/npm | backend not required; frontend generation later | ✓ | Node `v26.4.0`, npm `11.17.0` | Phase 7 owns frontend runtime. |

**Missing dependencies with no fallback:** running real PostgreSQL integration is currently blocked until the pinned service is started; sqlc/Goose must be installed before query/migration generation. No code implementation should claim these checks passed on the current host.

## Validation Architecture

### Test Framework

| Property | Value |
|---|---|
| Framework | Go `testing`; `go test -race`; add PostgreSQL integration tests in `backend/...` |
| Config file | None for backend; Wave 0 should add a Docker Compose test service and migration fixture |
| Quick run command | `GOTOOLCHAIN=local go test ./...` |
| Full suite command | `GOTOOLCHAIN=local go test -race ./...` plus PostgreSQL Compose integration command recorded by the phase Make target |

### Phase Requirements -> Test Map

| Requirement group | Behavior | Test type | Automated command | File exists? |
|---|---|---|---|---|
| `TEN-001..009`, `BE-004` | tenant scope, fixed roles, platform-admin audit, restriction projection | unit + PostgreSQL integration | `go test ./backend/internal/policy ./backend/internal/repository -run 'Tenant|Scope|Audit|Restriction'` | No; Wave 0 gap |
| `NOD-001..014`, `PRT-001..016` | ownership, lifecycle/drain/delete guards, port conflict/common-port reservation | PostgreSQL integration | `go test ./backend/internal/service -run 'Node|Port|Group'` | No; Wave 0 gap |
| `RUL-001..016`, `BE-002/003` | immutable revision, compiler digest replay, Prepare/Commit/Abort, superseded task | pure unit + integration | `go test ./backend/internal/compiler ./backend/internal/worker -run 'Rule|Compile|Task'` | No; Wave 0 gap |
| `PLN-001..008` | package snapshot, monthly UTC anchor, code redemption idempotency, downgrade preview | PostgreSQL integration/property | `go test ./backend/internal/service -run 'Package|Entitlement|Subscription'` | No; Wave 0 gap |
| `MTR-001..016`, `SUP-B2-001`, `SUP-B5-001` | per-source conservation, snapshot binding, duplicate usage, unknown liability/dispute | PostgreSQL concurrency/fault | `go test -race ./backend/internal/ledger -run 'Source|Usage|Unknown|Dispute'` | No; Wave 0 gap |
| `LIM-001..010` | share chain CAS, boot fencing, activation-unknown, no release on lost receipt | PostgreSQL + real Agent/Hub integration | `go test -race ./backend/internal/limit ./backend/internal/control -run 'CAS|Boot|Activation'` | No; Wave 0 gap |
| `BE-005` | OpenAPI schema, error envelope, cursor and SSE Last-Event-ID/gap refetch | HTTP integration | `go test ./backend/internal/httpapi -run 'OpenAPI|Cursor|SSE|Error'` | No; Wave 0 gap |

### Sampling Rate

- **Per task commit:** `GOTOOLCHAIN=local go test ./...` and SQL migration lint.
- **Per wave merge:** `GOTOOLCHAIN=local go test -race ./...` plus Docker PostgreSQL integration and compiler golden-digest tests.
- **Phase gate:** same source SHA local Docker and GitHub Actions; run migration-from-empty, rollback/replay/fault tests, cross-tenant negative tests, usage/share concurrency, and Agent/Hub receipt recovery before `$gsd-verify-work`.

### Wave 0 Gaps

- [ ] Add PostgreSQL 18.6 Compose test service with pinned image digest and health check.
- [ ] Add `db/migrations/` Goose baseline and `db/query/` sqlc config; verify generated code has no diff.
- [ ] Add repository scope test fixtures for two tenants plus platform principal.
- [ ] Add compiler golden vectors (same inputs -> same canonical bytes/digest) and invalid path/permit cases.
- [ ] Add ledger fixtures for two independent sources, duplicate usage, disputed payload, unknown `U_bill`, and risk-budget closure.
- [ ] Add share CAS/boot fencing integration with the real Phase 5 protocol adapter, including lost response and `activation-unknown`.
- [ ] Add OpenAPI path/schema source, chosen generator lock, schema lint and generated TypeScript compile check.
- [ ] Add same-SHA Make target used identically by local Docker and Actions; record `git rev-parse HEAD`, Go, Docker, PostgreSQL and image digest.

## Security Domain

### Applicable ASVS Categories

| ASVS Category | Applies | Standard Control |
|---|---|---|
| V2 Authentication | yes | Short-lived Bearer access tokens, rotating refresh token families, mTLS separate control identity; reject revoked/expired credentials. |
| V3 Session Management | yes | Bind request principal to tenant/role, rotate refresh token on use, revoke family, request ID/audit all sensitive actions. |
| V4 Access Control | yes | Service/repository scope policy, explicit platform-vs-tenant ownership and grants, cross-tenant admin audit, no frontend authority. |
| V5 Input Validation | yes | OpenAPI schema validation, strict UUID/enum/range checks, canonical cursor validation, bounded payloads, compiler path/target/network validation. |
| V6 Cryptography | yes | TLS 1.3/mTLS via Go standard library, credential encryption at rest, SHA-256 artifact/payload digests; never hand-roll token or TLS primitives. |

### Known Threat Patterns for Go/PostgreSQL backend

| Pattern | STRIDE | Standard Mitigation |
|---|---|---|
| Cross-tenant ID enumeration | Elevation/Tampering | Scope predicates in SQL, return same not-found/forbidden policy as specified, negative tests with two tenants. |
| SQL injection / unsafe dynamic filters | Tampering | sqlc parameters and allow-listed sort/filter columns; no concatenated user SQL. |
| Replay/conflicting usage payload | Tampering/Repudiation | Unique `(node_id,boot_id,sequence)`, digest/snapshot comparison, dispute evidence, idempotent ack. |
| Lost control receipt | Tampering/Denial | `unknown` operation state, durable task lease, reconcile before releasing port/share/reservation. |
| Refresh-token theft | Spoofing | Rotating token family, one-time refresh use, revocation, secure hash storage, short access TTL. |
| Outbox secret leakage | Information disclosure | Payload contains IDs/digests only; fetch secrets under scoped worker transaction; audit summaries redact credentials. |
| Compiler artifact substitution | Tampering | Canonical input versions, SHA-256 digest, target revision/generation match and immutable storage. |
| Deadlock/starvation | Denial | Deterministic lock order, short transactions, bounded retries on `40P01`/`40001`, task `SKIP LOCKED` only in queue consumer. |

## Sources

### Primary (HIGH confidence)

- `.planning/phases/06-m4-m4-backend/06-CONTEXT.md` — all locked D-06 decisions, discretion and out-of-scope boundaries.
- `.planning/REQUIREMENTS-SOURCE.md:64-1295` — exact TEN/NOD/PRT/RUL/BE/PLN/MTR/LIM/SUP-B2/SUP-B5 semantics and quoted conservation rules.
- `contracts/database/schema.sql:1-64` — draft PostgreSQL entities, enum/check values and explicit missing authority tables.
- `contracts/openapi/v1/openapi.yaml:1-33` — OpenAPI 3.1.1, bearer scheme, empty paths and public-model boundary.
- `contracts/protobuf/v1/vortex.proto:1-220` and `contracts/protobuf/v1/lifecycle.md` — frozen Agent/Hub control messages and lifecycle/receipt semantics.
- `.planning/phases/05-m3-agent-edge-hub-m3-agent-and-hub/05-REQUIREMENT-AUDIT.md` — Phase 5 covered-slice versus Phase 6 production transfer boundary.
- `docs/governance/version-lock.yaml:1-109` — approved Go/Gin/pgx/sqlc/Goose versions, checksums and license sources.
- `hub/usage.go`, `hub/recovery.go`, `hub/testhub.go` — protocol-test adapter comments and durable handoff behavior.

### Secondary (MEDIUM confidence)

- [PostgreSQL transaction isolation](https://www.postgresql.org/docs/current/transaction-iso.html) — Read Committed default, Serializable retry and explicit lock guidance.
- [PostgreSQL explicit locking](https://www.postgresql.org/docs/current/explicit-locking.html) — row locks, advisory locks, deadlock and transaction-level advisory semantics.
- [PostgreSQL SELECT locking](https://www.postgresql.org/docs/current/sql-select.html#SQL-FOR-UPDATE-SHARE) — `NOWAIT`/`SKIP LOCKED` queue caveat.
- [sqlc pgx guide](https://docs.sqlc.dev/en/v1.31.1/guides/using-go-and-pgx.html) — versioned pgx integration documentation; query/transaction shape still requires repository-specific tests.
- [Goose documentation](https://pressly.github.io/goose/) — incremental SQL migrations and production sequential-version recommendation.

### Tertiary (LOW confidence)

- None used for normative claims. The proposed `backend/` layout, canonical JSON encoding and OpenAPI generator choice are listed as `[ASSUMED]` until the planner locks them.

## Metadata

**Confidence breakdown:**

- Standard stack: HIGH — exact versions and checksums are in project version lock; server/client and generator integration remain explicit gates.
- Architecture: HIGH — locked context and existing control contracts define the authority, boundary and lifecycle semantics; backend directory does not yet exist.
- Pitfalls: HIGH — grounded in exact requirement text, PostgreSQL official locking documentation and Phase 5 failure/unknown behavior.

**Research date:** 2026-11-06  
**Valid until:** 2026-12-06 for stable contract findings; 2026-11-13 for package/tool versions and Docker/Actions availability.
