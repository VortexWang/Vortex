---
phase: 06-m4-m4-backend
plan: 07
subsystem: public-api
tags: [rest, auth, postgres, sse, openapi]
requires:
  - phase: 06-06
    provides: funded runtime and explicit proof-bound recovery
provides:
  - Scoped business REST with persisted current principals, strong CAS and exact operations
  - Durable tenant event streams with authenticated replay and gap resync
  - Owner-approved public API 3.0.0 and database 2.4.0 with exact 99-artifact review
affects: [06-11, phase-7]
tech-stack:
  added: []
  patterns: [SQL-safe business projection, transactional events, independent exact approval]
key-files:
  created: [backend/internal/httpapi/, backend/internal/auth/, backend/db/migrations/00009_domain_events.sql, tools/phase6/rest_container_check.sh, docs/changes/CR-009-APPROVAL.json]
  modified: [contracts/openapi/v1/openapi.yaml, contracts/database/schema.sql, backend/internal/service/, backend/internal/worker/]
key-decisions:
  - "Exact CR-009 ed7ca2f0cf68f46d29984b7ddafbc13fc8de4ad171cd3378b0b9dda8e45bdfe0 explicitly approved; API 3.0.0 breaking and DB 2.4.0 additive frozen."
  - "Port commit proves PG stage; recovery admission and operation success never substitute for actual Node/Hub completion or financial finality."
  - "Final same-SHA Docker/Actions pairing is consolidated in dependent plan 06-11."
requirements-completed: [BE-005]
coverage:
  - id: D1
    description: Current persisted auth, tenant-safe REST and exact mutation operations
    verification:
      - kind: integration
        ref: artifacts/phase6/06-07/task2-unified-acceptance/clean-docker-validation.json
        status: pass
    human_judgment: false
  - id: D2
    description: Durable authenticated SSE replay, stable cursors and REST resync
    verification:
      - kind: integration
        ref: backend/internal/httpapi/sse_test.go
        status: pass
    human_judgment: false
  - id: D3
    description: Exact contract freeze and immutable historical approval protection
    verification:
      - kind: other
        ref: artifacts/phase6/06-07/approval/exact-freeze.log
        status: pass
    human_judgment: false
completed: 2026-11-09
status: complete
---

# 06-07 REST, authentication and durable SSE

41 paths / 45 operations expose authorized business projections. Tokens store hashes only; rotating refresh reuse revokes the family, and current PG roles are rechecked. Strong quoted If-Match, scoped immutable cursors, exact transaction-bound operation identities and safe errors are verified against actual HTTP responses. Initial session issuance remains trusted local provisioning; Agent/Hub mTLS uses an independent boundary.

Business writes append events in the same PG transaction. Committed sequence assignment, signed tenant/principal event cursors, bounded replay, retention floors, gap/resync and shutdown handling do not make SSE authoritative. Node/package/group/port/DNS/recovery adapters retain original unknown occupation and use durable actor-bound consumers. Recovery advancement waits for actual SQLite FULL manifest/fence/root receipts; DNS unknown outcomes park for explicit recompute.

## Commits and verification

Task history is preserved in 06-07-CONTINUATION.md and each evidence unit under artifacts/phase6/06-07. Final implementation commits include b55efab2 (proof-bound recovery and real lock-order correction), 629fe11f (actual OpenAPI/schema alignment), 97966a3a (bounded clean-source producer), 5629543f (exact candidate/checkpoint). This completion commit independently records the subsequent owner approval.

Clean tested source 97966a3a70bff79afd43078ceadf211fe877a6d5 passed full backend normal/race suites with 482 passing test/subtest events each (service 227.988s / 383.572s), two independent sqlc generations of 33 outputs, six Linux amd64/arm64 Backend/Hub/Node builds, source-clean checks and host-side dependency/license/OpenAPI gates. The 99 normative artifacts match that source byte-for-byte except authorized catalog freeze metadata and appended approval notice. Exact owner/proposal/contract/history and evidence tamper negatives pass. Raw evidence and earlier failures remain retained, including a real 40P01 fixed by consistent tenant→task→operation ordering.

## Deviations and remaining scope

Implementation exceeded the original two-task estimate and was split into bounded atomic units; scope was preserved. Generic check.sh still supports only through 06-05; 06-07 uses the dedicated rest_container_check.sh. Final unified producer, same-SHA Actions pairing, cross-domain and external Cloudflare acceptance are explicitly carried into 06-11, not reported passed here. Requirements acceptance remains subject to final phase verification.

Docker evidence is actual Linux arm64 execution; six cross-builds do not prove native amd64 execution. No Ubuntu 24.04 qualification, dedicated performance, complete models, physical power loss, external provider CRUD or Phase 4/7/8 completion is claimed. Phase 7 consumes docs/operations/public-api.md, recovery-advance.md and dns-worker.md.
