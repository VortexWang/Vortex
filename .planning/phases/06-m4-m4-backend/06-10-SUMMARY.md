---
phase: 06-m4-m4-backend
plan: 10
subsystem: dns
tags: [postgresql, sqlc, cloudflare, aead, serving, outbox]
requires:
  - phase: 06-02
    provides: node/group authority, port confirmations and encrypted credential schema
provides:
  - Persistent owner-scoped entry domain binding and aggregate confirmed serving membership
  - AEAD encrypted exact-zone provider credentials with audited durable DNS operations
  - Real controlled Cloudflare CRUD and exact-source Docker normal/race evidence
affects: [06-03, 06-07, 06-11, phase-7]
tech-stack:
  added: []
  patterns: [immutable entry domain binding, confirmed-member DNS reconciliation, bounded provider adapter]
key-files:
  created: [backend/db/migrations/00004_entry_group_domains.sql, backend/internal/service/serving.go, backend/internal/control/credential_store.go, backend/internal/control/credential_crypto.go, backend/internal/control/dns_adapter.go, backend/internal/control/cloudflare_integration_test.go, docs/coverage/phase6/DNS-CREDENTIALS.md]
  modified: [backend/db/query/credentials.sql, backend/db/query/ports.sql, backend/internal/service/node.go, backend/internal/repository/port_repository.go, contracts/database/schema.sql, contracts/openapi/v1/openapi.yaml, tools/phase6/cloudflare_integration.sh, tools/phase6/evidence_manifest.sh]
key-decisions:
  - One immutable normalized domain belongs to one entry group; a grant cannot transfer control of a platform group's DNS.
  - Platform DNS aggregates all active tenants' committed rules; health is a per-member live eligibility check outside the configuration digest.
  - Exact token creation-policy attestation remains trusted deployment configuration and is bound to the live verified token ID; it is not independent current-policy introspection.
  - Append migration 00004 for entry domains; only future rules/packages/ledger/shares/events references move to 00005 through 00009.
requirements-completed: [PRT-011, PRT-012, PRT-013, PRT-014, PRT-015]
coverage:
  - id: D1
    description: One durable authorized domain per entry group within a verified zone
    requirement: PRT-011
    verification:
      - kind: integration
        ref: backend/internal/service/serving_test.go#TestDNSEntryGroupDomainBinding
        status: pass
    human_judgment: false
  - id: D2
    description: All committed rules and complete current-incarnation protocol confirmations gate platform serving
    requirement: PRT-012
    verification:
      - kind: integration
        ref: backend/internal/service/serving_test.go#TestServingPlatformAggregatesAllCommittedTenantRules
        status: pass
    human_judgment: false
  - id: D3
    description: Failed new rules preserve existing service and unhealthy members exit without dropping healthy members
    requirement: PRT-013
    verification:
      - kind: integration
        ref: backend/internal/service/serving_test.go#TestServingAllCommittedRulesAndFailedNewRule
        status: pass
      - kind: integration
        ref: backend/internal/service/serving_test.go#TestServingRejectsUnhealthyMember
        status: pass
    human_judgment: false
  - id: D4
    description: DNS changes never release retained sockets or authorize migration of existing flows
    requirement: PRT-014
    verification:
      - kind: integration
        ref: backend/internal/service/serving_test.go#TestDNSDurableServingRecomputeAndUnknownReplay
        status: pass
    human_judgment: false
  - id: D5
    description: Real Cloudflare CRUD verifies TTL60, DNS-only, encrypted storage, token policy identity and cleanup
    requirement: PRT-015
    verification:
      - kind: integration
        ref: artifacts/phase6/06-10/de6f14d6/docker/cloudflare.log#TestCloudflareControlledCRUD
        status: pass
    human_judgment: false
actuals:
  tokens: 82120
  tasks: 2
  commits: 9
  plan_head_before: 872a3120739272097cda6ce015b8611a082a9b3b
  plan_head_after: 6660002e0f56cc96c1354d3fe74b8cf821148270
duration: 124min multi-session
completed: 2026-11-07
status: complete
---

# Phase 6 Plan 10: Serving and Cloudflare Summary

**入口组的持久域名、完整规则 serving 集合和加密 Cloudflare 凭据形成可审计的 DNS 任务边界；真实 CRUD 已通过。**

## Accomplishments and Verification

- 最终实现源提交是 `de6f14d60b474d5f62ff0c5b2c1084b168052bf2`。干净检出运行 `check.sh --through-plan 06-10 --produce docker`，exit 0，source_clean=true；原始证据在 `artifacts/phase6/06-10/de6f14d6/docker/`，证据提交 `6660002e`。
- 固定 Go 1.27.3、PostgreSQL 18.6、Valkey 9.1.3 的本机 Linux arm64 Docker 完整普通/race 测试有 122 条 PASS，零 skip、零 race warning；真实 Agent/Hub 进程各保留普通/race 报告。新追加迁移 00004 从空库升级的证据在此前同配置的 `ce643cb4/docker/tracer.log`，最终源复用了该已迁移的独立测试库。
- sqlc v1.31.1 两次独立生成与提交产物逐字节一致；24 项 backend compiled module closure、42 项 sqlc linked module closure 及精确 CR-003/004 许可例外门禁通过；OpenAPI lint 和唯一源复制比对通过。
- `TestCloudflareControlledCRUD` 对真实 `api.cloudflare.com` 执行 create/read/update/delete，5.23 秒 PASS。TTL=60、proxied=false、AEAD 存储、令牌身份与 exact-zone 策略绑定以及精确名称清理全部通过。证据仅保存策略文件摘要，API token、加密密钥和本地配置文件均未提交；完整证据目录通过真实 token 字节泄漏检查。
- `manifest passed: 96 owners; docker producer only; pairing pending`。这不是新的 Actions 或双宿主配对通过结论；06-11 仍须对最终源提交完成适用的 Actions 与配对门禁。

## Task Commits

1. Task 1 RED `a07ed057`；GREEN `ef579127`；完整规则/持久域名修正 `67749431`；健康过滤 `7479b9b7`；共享配置摘要修正 `de6f14d6`。
2. Task 2 工具/门禁 `edafef7d`，配置模板 `2fcb8f6c`，可重现输入修正 `ce643cb4`，最终原始证据 `6660002e`。

`actuals.tokens` 是上述 ledger 基点到证据提交的 decoded diff 328,477 字符除以 4 向上取整，包含原始证据；107 个变更文件。9 次提交由 `git rev-list --count` 实测，不是运行时 token 数。

## Deviations from Plan

- **Rule 2 — 需求完整性：** PRT-011 原实现仅接受请求中的域名，缺少持久绑定。按已授权的该需求追加 00004、域名唯一性和凭据外键、zone_name、组所有者校验；未执行计划的迁移编号移为 00005..00009。已应用的 00001..00003 未改写。
- **Rule 1 — serving 正确性：** 一条成功规则不能掩盖另一条已提交规则的 unknown 端口；未提交新规则不能摘除旧规则成员。平台所有者的 DNS 必须聚合所有有效租户。相应真实 PostgreSQL 断言通过。
- **Rule 1 — 健康信号：** unhealthy 在线节点必须退出解析；原有整行摘要会令一个成员的健康变化使全组失配，因此将 health 从共享配置摘要排除，保留版本、incarnation、能力、端口范围及成员集合检查。`7479b9b7` 的新增测试曾失败，`de6f14d6` 修复后最终完整门禁通过。
- **Rule 3 — 可重现工具输入：** 官方 sqlc 下载曾因 HTTP/2 失败；安装脚本支持 exact-SHA 验证的本地 archive，并对官方下载使用有界 HTTP/1.1 重试。未更换版本或放宽许可。
- **Rule 3 — GSD 状态工具：** `state.advance-plan` 仍拒绝既有 Phase 5 `passed_with_scope`。保留 Phase 5 验证结论，窄范围同步本计划的 Phase 6 进度、指标、决策和 session。

## TDD Gate Compliance

`artifacts/phase6/06-10/red/serving.json` 保存真实 PostgreSQL 的 RED：已删除成员错误进入 serving，目标测试实际执行并在预期断言失败。后续普通/race 和最终 exact-source producer 都包含该断言。未使用 fixture 替代 live Cloudflare 验证。

## User Setup and Evidence Limits

专用 Cloudflare zone、DNS Edit token 和私有策略文件已由本地配置提供。策略 attestation 可信边界及轮换/复核要求见 `docs/coverage/phase6/DNS-CREDENTIALS.md`；它不是独立实时读取 token policy。API 令牌未上传至 GitHub。

早先 `edafef7d` 依赖下载失败和缺配置未执行记录保留，均不表示通过。WINDOWS #5 的真实 CRUD 未执行事项已解决；来源账本/Valkey 债务窗口仍待 06-05/06-11。没有新增源代码 stub 或 skipped test。

## Threat Flags

| Flag | File | Description |
|------|------|-------------|
| threat_flag: persistent_domain_ownership | backend/db/migrations/00004_entry_group_domains.sql | 新增持久域名权威边界；组所有者与验证 zone 后缀校验、全局唯一性、凭据外键和同事务审计/任务，以及绑定/跨租户/回滚测试提供约束。 |

## Next Plan Readiness

Phase 6 为 5/11，下一依赖计划是 06-03（rules 迁移使用 00005）。HTTP handler 属于 06-07，Phase 7 消费异步 operation 契约。NOD-012、PRT-004 的 compiler/真实 Agent port consumer 仍按前序总结待办；Phase 4、Phase 5 历史范围及最终系统验收没有提前通过。

## Self-Check: PASSED

关键实现、迁移、OpenAPI 源/复制与最终 Docker/Cloudflare 证据均存在；上述 9 个提交均为 HEAD 祖先。最终 host manifest 核验 SHA、checksum、clean source、完整测试和真实 CRUD 后 exit 0。当前没有新源 Actions 或 paired gate 通过声明。
