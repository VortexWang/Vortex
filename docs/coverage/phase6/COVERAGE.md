# Phase 6 requirement ownership and evidence

All 96 Phase6 requirements retain one primary plan owner below. The final producer
now supports through06-11: complete backend and Engine suites plus Agent/Hub/contracts component normal/race, real PG and Valkey,
finite1000 reconciliation, faults, generators, exact approved contracts and live
Cloudflare. Final65a53358 Docker and native amd64/arm64 Actions42572674719 passed;
ownership and implementation alone are not paired acceptance.

Run from a clean exact-commit clone (outputs must be outside the source tree):

```sh
./tools/phase6/check.sh --through-plan 06-11 --expected-sha "$SHA" --produce docker --evidence-dir "$EVIDENCE"
# GitHub Actions runs the same command with --produce actions and GITHUB_SHA.
./tools/phase6/check.sh --through-plan 06-11 --expected-sha "$SHA" --evidence-dir "$EVIDENCE"
```

The last command requires BOTH downloaded Actions and local Docker evidence;
producer success alone never means paired acceptance. Each host retains the
explicit result/commit/checksum files plus openapi.log, dependencies.log,
tracer.log, source status, host/Docker/database/image/build metadata, and real
Agent/Hub process logs. Missing/failed evidence, skips, source dirt, SHA drift,
wrong host identity or mismatched SHA files fail closed. CI outputs use RUNNER_TEMP
and are uploaded with full-SHA pinned actions. No generator is installed for OpenAPI.
No Phase 4 performance, Phase 7 backup/recovery, or Phase 8 scale claim is made.

| Requirement | Owner | Evidence | Acceptance boundary |
| --- | --- | --- | --- |
| BE-001 | 06-01 | 06-01-SUMMARY.md; real-process reports + backend normal/race | verified; final65a53358 Docker/amd64+arm64 Actions PASS; real backend/Agent/Hub roles, transaction + durable receipt |
| SUP-B2-001 | 06-01 | 06-01-SUMMARY.md; real-process reports + backend normal/race | verified; final65a53358 Docker/amd64+arm64 Actions PASS; real backend/Agent/Hub roles, transaction + durable receipt |
| SUP-B5-001 | 06-01 | 06-01-SUMMARY.md; real-process reports + backend normal/race | verified; final65a53358 Docker/amd64+arm64 Actions PASS; real backend/Agent/Hub roles, transaction + durable receipt |
| NOD-001 | 06-02 | [NODES-PORTS.md](NODES-PORTS.md) | verified; final65a53358 Docker/amd64+arm64 Actions PASS; fixed members, current incarnation, scoped grant/permit, actual sockets + proof-based release |
| NOD-002 | 06-02 | [NODES-PORTS.md](NODES-PORTS.md) | verified; final65a53358 Docker/amd64+arm64 Actions PASS; fixed members, current incarnation, scoped grant/permit, actual sockets + proof-based release |
| NOD-003 | 06-02 | [NODES-PORTS.md](NODES-PORTS.md) | verified; final65a53358 Docker/amd64+arm64 Actions PASS; fixed members, current incarnation, scoped grant/permit, actual sockets + proof-based release |
| NOD-004 | 06-02 | [NODES-PORTS.md](NODES-PORTS.md) | verified; final65a53358 Docker/amd64+arm64 Actions PASS; fixed members, current incarnation, scoped grant/permit, actual sockets + proof-based release |
| NOD-005 | 06-02 | [NODES-PORTS.md](NODES-PORTS.md) | verified; final65a53358 Docker/amd64+arm64 Actions PASS; fixed members, current incarnation, scoped grant/permit, actual sockets + proof-based release |
| NOD-006 | 06-02 | [NODES-PORTS.md](NODES-PORTS.md) | verified; final65a53358 Docker/amd64+arm64 Actions PASS; fixed members, current incarnation, scoped grant/permit, actual sockets + proof-based release |
| NOD-007 | 06-02 | [NODES-PORTS.md](NODES-PORTS.md) | verified; final65a53358 Docker/amd64+arm64 Actions PASS; fixed members, current incarnation, scoped grant/permit, actual sockets + proof-based release |
| NOD-008 | 06-02 | [NODES-PORTS.md](NODES-PORTS.md) | verified; final65a53358 Docker/amd64+arm64 Actions PASS; fixed members, current incarnation, scoped grant/permit, actual sockets + proof-based release |
| NOD-009 | 06-02 | [NODES-PORTS.md](NODES-PORTS.md) | verified; final65a53358 Docker/amd64+arm64 Actions PASS; fixed members, current incarnation, scoped grant/permit, actual sockets + proof-based release |
| NOD-010 | 06-02 | [NODES-PORTS.md](NODES-PORTS.md) | verified; final65a53358 Docker/amd64+arm64 Actions PASS; fixed members, current incarnation, scoped grant/permit, actual sockets + proof-based release |
| NOD-011 | 06-02 | [NODES-PORTS.md](NODES-PORTS.md) | verified; final65a53358 Docker/amd64+arm64 Actions PASS; fixed members, current incarnation, scoped grant/permit, actual sockets + proof-based release |
| NOD-012 | 06-02 | [NODES-PORTS.md](NODES-PORTS.md) | verified; final65a53358 Docker/amd64+arm64 Actions PASS; fixed members, current incarnation, scoped grant/permit, actual sockets + proof-based release |
| NOD-013 | 06-02 | [NODES-PORTS.md](NODES-PORTS.md) | verified; final65a53358 Docker/amd64+arm64 Actions PASS; fixed members, current incarnation, scoped grant/permit, actual sockets + proof-based release |
| NOD-014 | 06-02 | [NODES-PORTS.md](NODES-PORTS.md) | verified; final65a53358 Docker/amd64+arm64 Actions PASS; fixed members, current incarnation, scoped grant/permit, actual sockets + proof-based release |
| PRT-001 | 06-02 | [NODES-PORTS.md](NODES-PORTS.md) | verified; final65a53358 Docker/amd64+arm64 Actions PASS; fixed members, current incarnation, scoped grant/permit, actual sockets + proof-based release |
| PRT-002 | 06-02 | [NODES-PORTS.md](NODES-PORTS.md) | verified; final65a53358 Docker/amd64+arm64 Actions PASS; fixed members, current incarnation, scoped grant/permit, actual sockets + proof-based release |
| PRT-003 | 06-02 | [NODES-PORTS.md](NODES-PORTS.md) | verified; final65a53358 Docker/amd64+arm64 Actions PASS; fixed members, current incarnation, scoped grant/permit, actual sockets + proof-based release |
| PRT-004 | 06-02 | [NODES-PORTS.md](NODES-PORTS.md) | verified; final65a53358 Docker/amd64+arm64 Actions PASS; fixed members, current incarnation, scoped grant/permit, actual sockets + proof-based release |
| PRT-005 | 06-02 | [NODES-PORTS.md](NODES-PORTS.md) | verified; final65a53358 Docker/amd64+arm64 Actions PASS; fixed members, current incarnation, scoped grant/permit, actual sockets + proof-based release |
| PRT-006 | 06-02 | [NODES-PORTS.md](NODES-PORTS.md) | verified; final65a53358 Docker/amd64+arm64 Actions PASS; fixed members, current incarnation, scoped grant/permit, actual sockets + proof-based release |
| PRT-007 | 06-02 | [NODES-PORTS.md](NODES-PORTS.md) | verified; final65a53358 Docker/amd64+arm64 Actions PASS; fixed members, current incarnation, scoped grant/permit, actual sockets + proof-based release |
| PRT-008 | 06-02 | [NODES-PORTS.md](NODES-PORTS.md) | verified; final65a53358 Docker/amd64+arm64 Actions PASS; fixed members, current incarnation, scoped grant/permit, actual sockets + proof-based release |
| PRT-009 | 06-02 | [NODES-PORTS.md](NODES-PORTS.md) | verified; final65a53358 Docker/amd64+arm64 Actions PASS; fixed members, current incarnation, scoped grant/permit, actual sockets + proof-based release |
| PRT-010 | 06-02 | [NODES-PORTS.md](NODES-PORTS.md) | verified; final65a53358 Docker/amd64+arm64 Actions PASS; fixed members, current incarnation, scoped grant/permit, actual sockets + proof-based release |
| PRT-016 | 06-02 | [NODES-PORTS.md](NODES-PORTS.md) | verified; final65a53358 Docker/amd64+arm64 Actions PASS; fixed members, current incarnation, scoped grant/permit, actual sockets + proof-based release |
| RUL-001 | 06-03 | [RULES.md](RULES.md) | verified; final65a53358 Docker/amd64+arm64 Actions PASS; immutable revisions/artifacts, fixed-member apply and unknown occupation |
| RUL-002 | 06-03 | [RULES.md](RULES.md) | verified; final65a53358 Docker/amd64+arm64 Actions PASS; immutable revisions/artifacts, fixed-member apply and unknown occupation |
| RUL-003 | 06-03 | [RULES.md](RULES.md) | verified; final65a53358 Docker/amd64+arm64 Actions PASS; immutable revisions/artifacts, fixed-member apply and unknown occupation |
| RUL-004 | 06-03 | [RULES.md](RULES.md) | verified; final65a53358 Docker/amd64+arm64 Actions PASS; immutable revisions/artifacts, fixed-member apply and unknown occupation |
| RUL-005 | 06-03 | [RULES.md](RULES.md) | verified; final65a53358 Docker/amd64+arm64 Actions PASS; immutable revisions/artifacts, fixed-member apply and unknown occupation |
| RUL-006 | 06-03 | [RULES.md](RULES.md) | verified; final65a53358 Docker/amd64+arm64 Actions PASS; immutable revisions/artifacts, fixed-member apply and unknown occupation |
| RUL-007 | 06-03 | [RULES.md](RULES.md) | verified; final65a53358 Docker/amd64+arm64 Actions PASS; immutable revisions/artifacts, fixed-member apply and unknown occupation |
| RUL-008 | 06-03 | [RULES.md](RULES.md) | verified; final65a53358 Docker/amd64+arm64 Actions PASS; immutable revisions/artifacts, fixed-member apply and unknown occupation |
| RUL-009 | 06-03 | [RULES.md](RULES.md) | verified; final65a53358 Docker/amd64+arm64 Actions PASS; immutable revisions/artifacts, fixed-member apply and unknown occupation |
| RUL-010 | 06-03 | [RULES.md](RULES.md) | verified; final65a53358 Docker/amd64+arm64 Actions PASS; immutable revisions/artifacts, fixed-member apply and unknown occupation |
| RUL-011 | 06-03 | [RULES.md](RULES.md) | verified; final65a53358 Docker/amd64+arm64 Actions PASS; immutable revisions/artifacts, fixed-member apply and unknown occupation |
| RUL-012 | 06-03 | [RULES.md](RULES.md) | verified; final65a53358 Docker/amd64+arm64 Actions PASS; immutable revisions/artifacts, fixed-member apply and unknown occupation |
| RUL-013 | 06-03 | [RULES.md](RULES.md) | verified; final65a53358 Docker/amd64+arm64 Actions PASS; immutable revisions/artifacts, fixed-member apply and unknown occupation |
| RUL-014 | 06-03 | [RULES.md](RULES.md) | verified; final65a53358 Docker/amd64+arm64 Actions PASS; immutable revisions/artifacts, fixed-member apply and unknown occupation |
| RUL-015 | 06-03 | [RULES.md](RULES.md) | verified; final65a53358 Docker/amd64+arm64 Actions PASS; immutable revisions/artifacts, fixed-member apply and unknown occupation |
| RUL-016 | 06-03 | [RULES.md](RULES.md) | verified; final65a53358 Docker/amd64+arm64 Actions PASS; immutable revisions/artifacts, fixed-member apply and unknown occupation |
| BE-002 | 06-03 | [RULES.md](RULES.md) | verified; final65a53358 Docker/amd64+arm64 Actions PASS; immutable revisions/artifacts, fixed-member apply and unknown occupation |
| BE-003 | 06-03 | [RULES.md](RULES.md) | verified; final65a53358 Docker/amd64+arm64 Actions PASS; immutable revisions/artifacts, fixed-member apply and unknown occupation |
| PLN-001 | 06-04 | [PACKAGES.md](PACKAGES.md) | verified; final65a53358 Docker/amd64+arm64 Actions PASS; immutable subscription, atomic redemption, independent restrictions and downgrade |
| PLN-002 | 06-04 | [PACKAGES.md](PACKAGES.md) | verified; final65a53358 Docker/amd64+arm64 Actions PASS; immutable subscription, atomic redemption, independent restrictions and downgrade |
| PLN-003 | 06-04 | [PACKAGES.md](PACKAGES.md) | verified; final65a53358 Docker/amd64+arm64 Actions PASS; immutable subscription, atomic redemption, independent restrictions and downgrade |
| PLN-004 | 06-04 | [PACKAGES.md](PACKAGES.md) | verified; final65a53358 Docker/amd64+arm64 Actions PASS; immutable subscription, atomic redemption, independent restrictions and downgrade |
| PLN-005 | 06-04 | [PACKAGES.md](PACKAGES.md) | verified; final65a53358 Docker/amd64+arm64 Actions PASS; immutable subscription, atomic redemption, independent restrictions and downgrade |
| PLN-006 | 06-04 | [PACKAGES.md](PACKAGES.md) | verified; final65a53358 Docker/amd64+arm64 Actions PASS; immutable subscription, atomic redemption, independent restrictions and downgrade |
| PLN-007 | 06-04 | [PACKAGES.md](PACKAGES.md) | verified; final65a53358 Docker/amd64+arm64 Actions PASS; immutable subscription, atomic redemption, independent restrictions and downgrade |
| PLN-008 | 06-04 | [PACKAGES.md](PACKAGES.md) | verified; final65a53358 Docker/amd64+arm64 Actions PASS; immutable subscription, atomic redemption, independent restrictions and downgrade |
| MTR-001 | 06-05 | [LEDGER.md](LEDGER.md) | verified; final65a53358 Docker/amd64+arm64 Actions PASS; source-local B/F/L, original usage identity, unknown freeze and budget-funded close |
| MTR-002 | 06-05 | [LEDGER.md](LEDGER.md) | verified; final65a53358 Docker/amd64+arm64 Actions PASS; source-local B/F/L, original usage identity, unknown freeze and budget-funded close |
| MTR-003 | 06-05 | [LEDGER.md](LEDGER.md) | verified; final65a53358 Docker/amd64+arm64 Actions PASS; source-local B/F/L, original usage identity, unknown freeze and budget-funded close |
| MTR-004 | 06-05 | [LEDGER.md](LEDGER.md) | verified; final65a53358 Docker/amd64+arm64 Actions PASS; source-local B/F/L, original usage identity, unknown freeze and budget-funded close |
| MTR-005 | 06-05 | [LEDGER.md](LEDGER.md) | verified; final65a53358 Docker/amd64+arm64 Actions PASS; source-local B/F/L, original usage identity, unknown freeze and budget-funded close |
| MTR-006 | 06-05 | [LEDGER.md](LEDGER.md) | verified; final65a53358 Docker/amd64+arm64 Actions PASS; source-local B/F/L, original usage identity, unknown freeze and budget-funded close |
| MTR-007 | 06-05 | [LEDGER.md](LEDGER.md) | verified; final65a53358 Docker/amd64+arm64 Actions PASS; source-local B/F/L, original usage identity, unknown freeze and budget-funded close |
| MTR-008 | 06-05 | [LEDGER.md](LEDGER.md) | verified; final65a53358 Docker/amd64+arm64 Actions PASS; source-local B/F/L, original usage identity, unknown freeze and budget-funded close |
| MTR-009 | 06-05 | [LEDGER.md](LEDGER.md) | verified; final65a53358 Docker/amd64+arm64 Actions PASS; source-local B/F/L, original usage identity, unknown freeze and budget-funded close |
| MTR-010 | 06-05 | [LEDGER.md](LEDGER.md) | verified; final65a53358 Docker/amd64+arm64 Actions PASS; source-local B/F/L, original usage identity, unknown freeze and budget-funded close |
| MTR-011 | 06-05 | [LEDGER.md](LEDGER.md) | verified; final65a53358 Docker/amd64+arm64 Actions PASS; source-local B/F/L, original usage identity, unknown freeze and budget-funded close |
| MTR-013 | 06-05 | [LEDGER.md](LEDGER.md) | verified; final65a53358 Docker/amd64+arm64 Actions PASS; source-local B/F/L, original usage identity, unknown freeze and budget-funded close |
| MTR-014 | 06-05 | [LEDGER.md](LEDGER.md) | verified; final65a53358 Docker/amd64+arm64 Actions PASS; source-local B/F/L, original usage identity, unknown freeze and budget-funded close |
| MTR-015 | 06-05 | [LEDGER.md](LEDGER.md) | verified; final65a53358 Docker/amd64+arm64 Actions PASS; source-local B/F/L, original usage identity, unknown freeze and budget-funded close |
| MTR-016 | 06-05 | [LEDGER.md](LEDGER.md) | verified; final65a53358 Docker/amd64+arm64 Actions PASS; source-local B/F/L, original usage identity, unknown freeze and budget-funded close |
| LIM-001 | 06-06 | [LIMITS.md](LIMITS.md) | verified; final65a53358 Docker/amd64+arm64 Actions PASS; independent shares/leases/credit, FULL closure/CAS/recovery and original funds |
| LIM-002 | 06-06 | [LIMITS.md](LIMITS.md) | verified; final65a53358 Docker/amd64+arm64 Actions PASS; independent shares/leases/credit, FULL closure/CAS/recovery and original funds |
| LIM-003 | 06-06 | [LIMITS.md](LIMITS.md) | verified; final65a53358 Docker/amd64+arm64 Actions PASS; independent shares/leases/credit, FULL closure/CAS/recovery and original funds |
| LIM-004 | 06-06 | [LIMITS.md](LIMITS.md) | verified; final65a53358 Docker/amd64+arm64 Actions PASS; independent shares/leases/credit, FULL closure/CAS/recovery and original funds |
| LIM-005 | 06-06 | [LIMITS.md](LIMITS.md) | verified; final65a53358 Docker/amd64+arm64 Actions PASS; independent shares/leases/credit, FULL closure/CAS/recovery and original funds |
| LIM-006 | 06-06 | [LIMITS.md](LIMITS.md) | verified; final65a53358 Docker/amd64+arm64 Actions PASS; independent shares/leases/credit, FULL closure/CAS/recovery and original funds |
| LIM-007 | 06-06 | [LIMITS.md](LIMITS.md) | verified; final65a53358 Docker/amd64+arm64 Actions PASS; independent shares/leases/credit, FULL closure/CAS/recovery and original funds |
| LIM-008 | 06-06 | [LIMITS.md](LIMITS.md) | verified; final65a53358 Docker/amd64+arm64 Actions PASS; independent shares/leases/credit, FULL closure/CAS/recovery and original funds |
| LIM-009 | 06-06 | [LIMITS.md](LIMITS.md) | verified; final65a53358 Docker/amd64+arm64 Actions PASS; independent shares/leases/credit, FULL closure/CAS/recovery and original funds |
| LIM-010 | 06-06 | [LIMITS.md](LIMITS.md) | verified; final65a53358 Docker/amd64+arm64 Actions PASS; independent shares/leases/credit, FULL closure/CAS/recovery and original funds |
| BE-005 | 06-07 | [REST-SSE.md](REST-SSE.md) | verified; final65a53358 Docker/amd64+arm64 Actions PASS; backend REST/OpenAPI/SSE; frontend type generation belongs to Phase7 |
| TEN-001 | 06-08 | [TENANCY.md](TENANCY.md); tenant/policy/repository/Valkey tests | verified; final65a53358 Docker/amd64+arm64 Actions PASS; cross-domain tenant ownership, fixed roles, transactional audit, PG rebuild |
| TEN-002 | 06-08 | [TENANCY.md](TENANCY.md); tenant/policy/repository/Valkey tests | verified; final65a53358 Docker/amd64+arm64 Actions PASS; cross-domain tenant ownership, fixed roles, transactional audit, PG rebuild |
| TEN-003 | 06-08 | [TENANCY.md](TENANCY.md); tenant/policy/repository/Valkey tests | verified; final65a53358 Docker/amd64+arm64 Actions PASS; cross-domain tenant ownership, fixed roles, transactional audit, PG rebuild |
| TEN-004 | 06-08 | [TENANCY.md](TENANCY.md); tenant/policy/repository/Valkey tests | verified; final65a53358 Docker/amd64+arm64 Actions PASS; cross-domain tenant ownership, fixed roles, transactional audit, PG rebuild |
| TEN-005 | 06-08 | [TENANCY.md](TENANCY.md); tenant/policy/repository/Valkey tests | verified; final65a53358 Docker/amd64+arm64 Actions PASS; cross-domain tenant ownership, fixed roles, transactional audit, PG rebuild |
| TEN-006 | 06-08 | [TENANCY.md](TENANCY.md); tenant/policy/repository/Valkey tests | verified; final65a53358 Docker/amd64+arm64 Actions PASS; cross-domain tenant ownership, fixed roles, transactional audit, PG rebuild |
| TEN-007 | 06-08 | [TENANCY.md](TENANCY.md); tenant/policy/repository/Valkey tests | verified; final65a53358 Docker/amd64+arm64 Actions PASS; cross-domain tenant ownership, fixed roles, transactional audit, PG rebuild |
| TEN-008 | 06-08 | [TENANCY.md](TENANCY.md); tenant/policy/repository/Valkey tests | verified; final65a53358 Docker/amd64+arm64 Actions PASS; cross-domain tenant ownership, fixed roles, transactional audit, PG rebuild |
| BE-004 | 06-08 | [TENANCY.md](TENANCY.md); tenant/policy/repository/Valkey tests | verified; final65a53358 Docker/amd64+arm64 Actions PASS; cross-domain tenant ownership, fixed roles, transactional audit, PG rebuild |
| TEN-009 | 06-09 | [TENANCY.md](TENANCY.md); independent restriction tests + OpenAPI | verified; final65a53358 Docker/amd64+arm64 Actions PASS; all restriction causes clear; deterministic single-source OpenAPI |
| PRT-011 | 06-10 | [DNS-CREDENTIALS.md](DNS-CREDENTIALS.md); controlled Cloudflare CRUD | verified; final65a53358 Docker/amd64+arm64 Actions PASS; current serving + actor-bound DNS, encrypted scoped token; external CRUD mandatory |
| PRT-012 | 06-10 | [DNS-CREDENTIALS.md](DNS-CREDENTIALS.md); controlled Cloudflare CRUD | verified; final65a53358 Docker/amd64+arm64 Actions PASS; current serving + actor-bound DNS, encrypted scoped token; external CRUD mandatory |
| PRT-013 | 06-10 | [DNS-CREDENTIALS.md](DNS-CREDENTIALS.md); controlled Cloudflare CRUD | verified; final65a53358 Docker/amd64+arm64 Actions PASS; current serving + actor-bound DNS, encrypted scoped token; external CRUD mandatory |
| PRT-014 | 06-10 | [DNS-CREDENTIALS.md](DNS-CREDENTIALS.md); controlled Cloudflare CRUD | verified; final65a53358 Docker/amd64+arm64 Actions PASS; current serving + actor-bound DNS, encrypted scoped token; external CRUD mandatory |
| PRT-015 | 06-10 | [DNS-CREDENTIALS.md](DNS-CREDENTIALS.md); controlled Cloudflare CRUD | verified; final65a53358 Docker/amd64+arm64 Actions PASS; current serving + actor-bound DNS, encrypted scoped token; external CRUD mandatory |
| BE-006 | 06-11 | [reconciliation-qualification.md](../../operations/reconciliation-qualification.md) | verified; final65a53358 Docker/amd64+arm64 Actions PASS; finite1000rules/2000members <=60s; deterministic Node adapter, full NFR-019 remains Phase8 |


当前最终验收：实现提交`65a5335894732b7d632e870acc3d8283be224cd3`，Docker及Actions42572674719原生amd64/arm64完整通过（含真实Cloudflare）。原始证据`artifacts/phase6/06-11/final-65a53358`；阶段完整结论与保留范围见06-VERIFICATION.md。以上较早证据仍按当时范围解释。
