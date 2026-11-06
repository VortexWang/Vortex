# Phase 6: M4 后端业务闭环 (M4 Backend) - Context

**Gathered:** 2026-11-06
**Status:** Ready for planning

<domain>
## Phase Boundary

Phase 6 delivers the authoritative PostgreSQL business backend: tenant and role isolation, platform and tenant node/resource ownership, node/port/rule compilation and orchestration, package and entitlement lifecycle, usage settlement, source-conserving ledger state, quota/share ownership, and durable task delivery to the existing Agent/Hub protocol adapters. The public surface is versioned REST/OpenAPI with SSE status updates. Engine remains an independent data-plane module, and Valkey remains non-authoritative cache/notification storage.

The phase must connect to the real Phase 5 Agent/Hub boundary and preserve its conservative unknown-liability, generation fencing, immutable snapshot, and activation-unknown semantics. It does not add frontend pages (Phase 7), final full-system qualification (Phase 8), online payment/auto-renewal, team invitations, or Phase 4 native performance/D-04 closure.

</domain>

<decisions>
## Implementation Decisions

### Tenant, roles, and resource authorization

- **D-06-01:** V1.0 uses a single-tenant user model. A normal user has one `tenant_id`; platform administrators are platform-scoped principals and cross-tenant actions are audited. Multi-tenant membership switching and team invitations remain out of scope. — **Reversibility:** costly — changing the identity and session model after API and database adoption requires a membership migration and revised authorization semantics.
- **D-06-02:** Use fixed roles plus explicit scope policies: platform `platform_admin`, `operator`, `auditor`; tenant `owner`, `member`. Service and repository layers enforce resource/action policy; the frontend is never the authority. — **Reversibility:** costly — role names and policy checks become API and audit compatibility points.
- **D-06-03:** Sensitive reads and mutations, including cross-tenant reads, permission/credential/node/rule/entitlement/ledger changes, append an immutable audit event in the same PostgreSQL transaction. Record actor, scope, action, resource, `request_id`, before/after summaries, and reason; never record secrets. — **Reversibility:** costly — moving audit out of the transaction would change the durability and compliance contract.
- **D-06-04:** Platform nodes and node groups remain platform-owned. Tenants use them through explicit grant records with validity, status, and version; rule creation and compilation validate the grant, and revocation preserves history. — **Reversibility:** costly — ownership and grant history cannot be reconstructed safely from copied tenant resources.

### Rule revisions, compilation, and application

- **D-06-05:** Rule edits create immutable `rule_revision` records. Mutations require `expected_revision`/`If-Match`; stale writes return a stable conflict error rather than last-write-wins. — **Reversibility:** one-way — revision identifiers and optimistic-concurrency behavior become public API semantics.
- **D-06-06:** The rule compiler is a deterministic pure transformation. It consumes the rule revision, grants, node-group membership, segment permits, and billing snapshot and produces immutable per-node artifacts with input versions, output digest, and diagnostics. Runtime configuration accepts only committed artifacts. — **Reversibility:** one-way — artifact digests and replay inputs become the authority for deployment and audit.
- **D-06-07:** A shared dependency change marks affected rules and schedules recompilation asynchronously. The previously active revision remains in force within its original authorization boundary until the new artifact completes the existing two-phase application. — **Reversibility:** costly — changing this to immediate invalidation would alter availability and authorization behavior during dependency updates.
- **D-06-08:** Persist tasks per rule revision. Pending obsolete tasks may become `superseded`; in-flight tasks are not force-killed. Only a successful receipt for the matching revision can update the current execution state. — **Reversibility:** costly — task history and late receipts depend on revision-scoped identity.

### Packages, entitlements, and restrictions

- **D-06-09:** The package catalog may be edited, but an active subscription keeps an immutable package snapshot. New subscriptions or an explicit package change create a new snapshot; historical settlement and existing sources never read a newly edited catalog value. This preserves `MTR-016`, `PLN-006`, and `PLN-008`. — **Reversibility:** costly — snapshot references are required to explain historical balances and settlements.
- **D-06-10:** Entitlement-code redemption is one idempotent PostgreSQL transaction: lock the code by redemption/request key, validate applicability and remaining uses, write the redemption record, and create the subscription or independent traffic-source record. Any failure rolls back all effects. — **Reversibility:** costly — redemption keys and source creation must remain replay-safe.
- **D-06-11:** Account state remains separate from business restrictions. Each restriction cause produces an immutable event with source and effective time, while a current-active projection supports fast checks; service resumes only after all active restrictions are cleared. — **Reversibility:** costly — retention of first-effective and clear history is needed for the seven-day port and restriction semantics.
- **D-06-12:** Package replacement or downgrade starts with a preview of lost capabilities, invalid rules, discarded amounts, and affected resources. User confirmation then creates the new snapshot and restrictions transactionally, with an explicit `effective_at`. — **Reversibility:** reversible — the preview/confirm boundary can evolve without changing historical source records.

### Metering, ledger, and share ownership

- **D-06-13:** PostgreSQL stores immutable ledger events; balance and available-authority views are rebuildable projections. Every non-reusable source tracks its own conservation values, and unknown liabilities remain frozen. — **Reversibility:** one-way — changing to mutable aggregate-only balances would lose the evidence needed for conservative recovery.
- **D-06-14:** Usage ingestion is idempotent on `(node_id, boot_id, sequence)`. Raw cumulative usage, billable usage, final receipts, and unknown upper bounds are separate facts; duplicate reports do not deduct twice. — **Reversibility:** one-way — the tuple is the durable replay identity from Agent/Hub into the authority.
- **D-06-15:** Reservations, consumption, settlement, disputes, and unknown liabilities reference an immutable `billing_snapshot_id`. Each source keeps separate `B_s`, `F_s`, and `L_s` conservation; no cross-source netting and no recalculation from the latest path are allowed. — **Reversibility:** one-way — snapshot and source IDs define accounting provenance.
- **D-06-16:** Rate/connection shares use a linear generation chain keyed by `(tenant_id, node_id, kind)` and bound to `boot_id`. Handoffs follow prepare successor, close all old renewal authority, durable node CAS, receipt confirmation, and only then release the difference; missing closure proof retains old ownership. — **Reversibility:** one-way — generation and fencing semantics are shared with Phase 5 and cannot be safely rewritten by a later aggregate update.

### Tasks, Outbox, and Agent/Hub delivery

- **D-06-17:** Business changes, task rows, and Outbox events commit in one PostgreSQL transaction. An uncommitted business change cannot produce a consumable control event. — **Reversibility:** one-way — the transaction boundary is the reliability contract for all downstream workers.
- **D-06-18:** Delivery is at-least-once. Handlers deduplicate by task key, idempotency key, and target revision; duplicate delivery is acceptable, duplicate business effect is not. Do not claim distributed exactly-once. — **Reversibility:** costly — changing delivery semantics would require changing all workers and replay evidence.
- **D-06-19:** Persist execution states including `pending`, `leased`, `running`, `succeeded`, `failed`, and `unknown`. Timeout, disconnect, or lost receipt enters `unknown` and must converge by query/reconciliation; it is never assumed not executed and cannot release unknown occupation. — **Reversibility:** costly — Phase 5 recovery and activation semantics depend on preserving unknown outcomes.
- **D-06-20:** Rule application, DNS operations, and node orchestration return an asynchronous `operation_id` and accepted desired state. Workers and Agent/Hub receipts update execution status; the backend never carries business traffic. — **Reversibility:** costly — operation IDs and status transitions are consumed by the future frontend and operations phase.

### REST, OpenAPI, and live status

- **D-06-21:** Public API is resource-oriented `/api/v1` REST with OpenAPI as the source contract. It exposes platform business models only; runtime configuration and VLink wire types remain internal. — **Reversibility:** one-way — published route and schema compatibility will constrain later clients.
- **D-06-22:** User API uses short-lived Bearer access tokens with rotating refresh tokens. Agent/Hub control traffic uses a separate mTLS identity boundary; neither credential type is reused for the other plane. — **Reversibility:** costly — token claims, rotation, and control-plane identity are security compatibility points.
- **D-06-23:** Errors use stable machine `code`, user-safe `message`, `request_id`, and optional field errors. Lists use cursor pagination with stable ordering; responses do not reveal internal targets, credentials, SQL, or tenant data outside scope. — **Reversibility:** costly — clients and audit tooling will depend on error and pagination contracts.
- **D-06-24:** SSE emits domain state events with event IDs and supports `Last-Event-ID`. If the event retention window has a gap, the client refetches authoritative REST state; SSE is a delivery hint, not a separate source of truth. — **Reversibility:** costly — replay and recovery behavior must match the durable event cursor and frontend cache invalidation.

### the agent's Discretion

- Research and planning may choose the exact PostgreSQL schema decomposition, Goose migration layout, sqlc query organization, tenant-scope helper API, token implementation details and TTLs, cursor encoding, task lease/backoff values, Outbox polling/notification mechanism, compiler artifact serialization, and concrete API resource names, provided these decisions preserve the locked invariants above.
- Research must define the full database constraints, indexes, retention, transaction isolation, PostgreSQL advisory/row-lock strategy, generated OpenAPI/TypeScript workflow, and same-SHA Docker/Actions evidence. New dependencies require the existing version/license review before adoption.

</decisions>

<canonical_refs>
## Canonical References

**Downstream agents MUST read these before planning or implementing.**

### Project scope and phase boundaries
- `.planning/PROJECT.md` — project restart governance, core value, architecture boundaries, persistence and validation constraints.
- `.planning/ROADMAP.md` — Phase 6 goal, requirements, success criteria, and Phase 7/8 boundaries.
- `.planning/REQUIREMENTS.md` — requirement IDs and current completion status.
- `.planning/REQUIREMENTS-SOURCE.md` — exact reference semantics for TEN, NOD, PRT, RUL, BE, PLN, MTR, and LIM requirements; reference source, not a frozen acceptance baseline.
- `.planning/phases/05-m3-agent-edge-hub-m3-agent-and-hub/05-CONTEXT.md` — Phase 5 identity, recovery, authorization, generation, SQLite, and protocol-test boundary decisions.
- `.planning/phases/05-m3-agent-edge-hub-m3-agent-and-hub/05-REQUIREMENT-AUDIT.md` — Phase 5 coverage and explicit Phase 6 transfer items.

### Contracts and schemas
- `contracts/database/schema.sql` — draft PostgreSQL entity schema; contains explicit unresolved gaps that Phase 6 must resolve before freezing.
- `contracts/openapi/v1/openapi.yaml` — draft public API contract; currently has no paths and must be expanded from business models only.
- `contracts/protobuf/v1/vortex.proto` — Agent/Hub control contract consumed by backend delivery; do not use it as the public REST model.
- `contracts/protobuf/v1/lifecycle.md` — lifecycle envelope, allocation, idempotency, and authority binding semantics.
- `contracts/protobuf/v1/lifecycle.schema.json` — lifecycle payload validation shape.
- `contracts/authorization/v1/payload.schema.json` — authorization payload and restriction boundary used by the Phase 5 adapter.
- `contracts/requirements-map.yaml` — contract-to-requirement traceability checks.
- `contracts/versions.yaml` — contract versions and compatibility records.

### Governance and dependency constraints
- `docs/governance/version-lock.yaml` — approved dependency/version evidence and lock policy.
- `docs/governance/license-policy.yaml` — dependency and distribution license gates.
- `docs/changes/CR-001-API压测工具替换.md` — approved Vegeta replacement and related validation boundary.

</canonical_refs>

<code_context>
## Existing Code Insights

### Reusable Assets
- `agent/store/` — durable SQLite schemas, idempotent authorization/usage/recovery records, fencing and share handoff behavior; backend delivery must target these existing protocol semantics.
- `agent/control.go`, `agent/control_apply.go`, `agent/authorization.go`, `agent/recovery.go` — control stream, authorization, apply, and recovery consumers that define backend-facing payload expectations.
- `hub/usage.go`, `hub/recovery.go`, `hub/testhub.go` — Hub usage relay, recovery, and Phase 5 protocol-test fixture boundary; production PostgreSQL authority is the missing Phase 6 integration.
- `contracts/` — generated Protobuf, lifecycle and authorization schemas, database draft, and OpenAPI draft.
- `engine/runtime/`, `engine/apply/`, and `engine/runtime/accounting/` — process-local Engine application, policy, accounting and share interfaces; remain behind the Agent/Engine process boundary.

### Established Patterns
- PostgreSQL is the sole business/desired-configuration authority; Valkey may be used only for rebuildable cache, rate limiting, or ephemeral notification.
- SQL-first access with pgx/v5, sqlc, and Goose is the project baseline; every repository query must carry tenant scope where applicable.
- Agent and Hub use SQLite WAL/FULL for local durable evidence; backend transactions must preserve idempotency, unknown outcomes, and immutable snapshots across the control boundary.
- Reliable side effects use PostgreSQL task tables and Transactional Outbox. Docker and GitHub Actions validate the same source SHA; missing or failed evidence remains unpassed.

### Integration Points
- New backend REST/SSE handlers connect to tenant-scoped services and PostgreSQL repositories.
- Compiler and orchestration services create versioned artifacts and tasks delivered through the existing Protobuf/mTLS Agent/Hub control stream.
- Usage, authorization, recovery, and share receipts flow back into the source-level ledger and operation state; Valkey is never consulted to reconstruct authority.
- Phase 7 frontend will consume the stable REST/OpenAPI and SSE contracts; Phase 8 will exercise full-scale and recovery acceptance.

</code_context>

<specifics>
## Specific Ideas

- The user selected all six discussion areas and then explicitly requested that subsequent questions proceed by the recommended option without pausing. Recommended choices were therefore locked for the remaining areas.
- The initial choice “mutable package + read latest” was rejected as incompatible with `MTR-016` and `PLN-006`; the compatible decision is a mutable catalog with immutable subscription snapshots.
- Preserve the Phase 5 distinction between `signer_epoch` and path `generation`; backend allocation and share ownership must not collapse these into one counter.
- Production backend authority is the Phase 6 goal. The Phase 5 protocol fixture remains test-only, while Phase 4 native performance qualification and D-04 remain separate work.

</specifics>

<deferred>
## Deferred Ideas

- Online payment and automatic renewal remain out of scope per `PROJECT.md`.
- Team invitations and multi-tenant membership remain out of scope for V1.0.
- Frontend pages, Cloudflare DNS operator UX, notifications, backup/recovery operations, and full browser workflows belong to Phase 7 after the backend contracts stabilize.
- Phase 4 native performance qualification, D-04 approval, and final full-scale acceptance belong to their existing qualification/final-acceptance scope, not Phase 6 implementation.

</deferred>

---

*Phase: 6-M4 后端业务闭环 (M4 Backend)*
*Context gathered: 2026-11-06*
