---
schema_version: 1
open_count: 1
waived_count: 0
fixed_count: 2
total_count: 3
last_updated: 2026-11-06T23:31:52.608Z
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
  }
]
````
