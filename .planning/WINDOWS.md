---
schema_version: 1
open_count: 1
waived_count: 0
fixed_count: 6
total_count: 7
last_updated: 2026-11-07T10:12:48.047Z
---

# Broken Windows Ledger

> Cross-phase defect register. With `workflow.windows_enforce` enabled, `/gsd-ship` blocks while `open_count > 0`.
> Waive with `gsd-tools windows waive <id> "<reason>"` (reason required).
> Mark fixed with `gsd-tools windows fixed <id>`.

| id | phase | kind | file | line | description | status | reason | recorded_at | resolved_at |
|----|-------|------|------|------|-------------|--------|--------|-------------|-------------|
| 1 | 06 | deviation | backend/internal/service/tracer_test.go |  | Source balances do not exist until 06-05; Valkey loss evidence currently covers implemented authority, ownership/occupation and confirmed tasks only. | open |  | 2026-11-06T19:08:40.423Z |  |
| 2 | 06 | deviation | tools/phase6/dependency_lock_check.sh |  | sqlc v1.31.1 official generator links go-sql-driver/mysql v1.9.3 MPL-2.0 outside the current license allowlist; CR-003 owner decision required. | fixed |  | 2026-11-06T19:34:36.869Z | 2026-11-06T23:10:22.626Z |
| 3 | 06 | deviation | tools/phase6/sqlc_license_check.py |  | CR-003 exact mysql/MPL exception is approved and implemented; Task 2 still rejects exact sqlc embedded MIT-0 and Bison output-exception expressions pending CR-004. Linked sysroot source closure was reconstructed; no unknown-license waiver requested. | fixed | Owner a approved CR-004 A; 7d43e1d2 enforces exact source/artifact/use scope, full gate exit 0; evidence b1a92d72. | 2026-11-06T23:10:22.700Z | 2026-11-06T23:31:52.608Z |
| 4 | 06 | unrun-verify | .github/workflows/phase6.yml |  | 06-09 real Actions execution and Docker/Actions pairing remain unpassed: nodeox has no push permission; exact source c1e6b766 Docker normal/race passed. | fixed | Browser authentication restored VortexWang; final source a9481584 passed actual Actions 42316152923, Docker and both paired gates. Raw evidence 32698c0d; summary 843d12f6. | 2026-11-06T23:51:43.970Z | 2026-11-07T00:14:19.743Z |
| 5 | 06 | unrun-verify | tools/phase6/cloudflare_integration.sh |  | PRT-015 real controlled Cloudflare CRUD remains unexecuted: dedicated token, zone and reviewed identity-bound policy configuration are absent; fixture tests are not live evidence. | fixed |  | 2026-11-07T01:20:49.407Z | 2026-11-07T03:02:36.326Z |
| 6 | 06 | unmet-truth | backend/internal/worker/rule_worker.go |  | 06-03 Task 3 is awaiting architecture decision: frozen control protocol auto-applies configs and static-destination Agent runtime cannot prove fixed-member Prepare/Commit/query; no two-phase runtime acceptance is claimed. | fixed | Owner approved CR-005 exact proposal; source fc63d574 clean Docker fixed-member real Agent/Engine normal/race, unknown/query/release and rollback tests pass; evidence6b74cc5b and summarya9f2e190. Final Actions pairing remains06-11. | 2026-11-07T04:05:49.488Z | 2026-11-07T10:12:47.942Z |
| 7 | 06 | unrun-verify | .planning/phases/06-m4-m4-backend/06-03-PLAN.md |  | 06-03 Task 3 Apply/Prepare/Commit/Rollback/Unknown/Supersede verification is unrun pending a compatible real Agent control/runtime architecture decision. | fixed | Owner approved CR-005 exact proposal; source fc63d574 clean Docker fixed-member real Agent/Engine normal/race, unknown/query/release and rollback tests pass; evidence6b74cc5b and summarya9f2e190. Final Actions pairing remains06-11. | 2026-11-07T04:05:49.558Z | 2026-11-07T10:12:48.047Z |

````json
[
  {
    "id": 1,
    "kind": "deviation",
    "phase": "06",
    "file": "backend/internal/service/tracer_test.go",
    "line": null,
    "description": "Source balances do not exist until 06-05; Valkey loss evidence currently covers implemented authority, ownership/occupation and confirmed tasks only.",
    "status": "open",
    "reason": "",
    "recorded_at": "2026-11-06T19:08:40.423Z",
    "resolved_at": null,
    "milestone": null
  },
  {
    "id": 2,
    "kind": "deviation",
    "phase": "06",
    "file": "tools/phase6/dependency_lock_check.sh",
    "line": null,
    "description": "sqlc v1.31.1 official generator links go-sql-driver/mysql v1.9.3 MPL-2.0 outside the current license allowlist; CR-003 owner decision required.",
    "status": "fixed",
    "reason": "",
    "recorded_at": "2026-11-06T19:34:36.869Z",
    "resolved_at": "2026-11-06T23:10:22.626Z",
    "milestone": null
  },
  {
    "id": 3,
    "kind": "deviation",
    "phase": "06",
    "file": "tools/phase6/sqlc_license_check.py",
    "line": null,
    "description": "CR-003 exact mysql/MPL exception is approved and implemented; Task 2 still rejects exact sqlc embedded MIT-0 and Bison output-exception expressions pending CR-004. Linked sysroot source closure was reconstructed; no unknown-license waiver requested.",
    "status": "fixed",
    "reason": "Owner a approved CR-004 A; 7d43e1d2 enforces exact source/artifact/use scope, full gate exit 0; evidence b1a92d72.",
    "recorded_at": "2026-11-06T23:10:22.700Z",
    "resolved_at": "2026-11-06T23:31:52.608Z",
    "milestone": null
  },
  {
    "id": 4,
    "kind": "unrun-verify",
    "phase": "06",
    "file": ".github/workflows/phase6.yml",
    "line": null,
    "description": "06-09 real Actions execution and Docker/Actions pairing remain unpassed: nodeox has no push permission; exact source c1e6b766 Docker normal/race passed.",
    "status": "fixed",
    "reason": "Browser authentication restored VortexWang; final source a9481584 passed actual Actions 42316152923, Docker and both paired gates. Raw evidence 32698c0d; summary 843d12f6.",
    "recorded_at": "2026-11-06T23:51:43.970Z",
    "resolved_at": "2026-11-07T00:14:19.743Z",
    "milestone": null
  },
  {
    "id": 5,
    "kind": "unrun-verify",
    "phase": "06",
    "file": "tools/phase6/cloudflare_integration.sh",
    "line": null,
    "description": "PRT-015 real controlled Cloudflare CRUD remains unexecuted: dedicated token, zone and reviewed identity-bound policy configuration are absent; fixture tests are not live evidence.",
    "status": "fixed",
    "reason": "",
    "recorded_at": "2026-11-07T01:20:49.407Z",
    "resolved_at": "2026-11-07T03:02:36.326Z",
    "milestone": null
  },
  {
    "id": 6,
    "kind": "unmet-truth",
    "phase": "06",
    "file": "backend/internal/worker/rule_worker.go",
    "line": null,
    "description": "06-03 Task 3 is awaiting architecture decision: frozen control protocol auto-applies configs and static-destination Agent runtime cannot prove fixed-member Prepare/Commit/query; no two-phase runtime acceptance is claimed.",
    "status": "fixed",
    "reason": "Owner approved CR-005 exact proposal; source fc63d574 clean Docker fixed-member real Agent/Engine normal/race, unknown/query/release and rollback tests pass; evidence6b74cc5b and summarya9f2e190. Final Actions pairing remains06-11.",
    "recorded_at": "2026-11-07T04:05:49.488Z",
    "resolved_at": "2026-11-07T10:12:47.942Z",
    "milestone": null
  },
  {
    "id": 7,
    "kind": "unrun-verify",
    "phase": "06",
    "file": ".planning/phases/06-m4-m4-backend/06-03-PLAN.md",
    "line": null,
    "description": "06-03 Task 3 Apply/Prepare/Commit/Rollback/Unknown/Supersede verification is unrun pending a compatible real Agent control/runtime architecture decision.",
    "status": "fixed",
    "reason": "Owner approved CR-005 exact proposal; source fc63d574 clean Docker fixed-member real Agent/Engine normal/race, unknown/query/release and rollback tests pass; evidence6b74cc5b and summarya9f2e190. Final Actions pairing remains06-11.",
    "recorded_at": "2026-11-07T04:05:49.558Z",
    "resolved_at": "2026-11-07T10:12:48.047Z",
    "milestone": null
  }
]
````
