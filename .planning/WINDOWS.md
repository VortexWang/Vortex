---
schema_version: 1
open_count: 1
waived_count: 0
fixed_count: 0
total_count: 1
last_updated: 2026-11-06T19:08:40.423Z
---

# Broken Windows Ledger

> Cross-phase defect register. With `workflow.windows_enforce` enabled, `/gsd-ship` blocks while `open_count > 0`.
> Waive with `gsd-tools windows waive <id> "<reason>"` (reason required).
> Mark fixed with `gsd-tools windows fixed <id>`.

| id | phase | kind | file | line | description | status | reason | recorded_at | resolved_at |
|----|-------|------|------|------|-------------|--------|--------|-------------|-------------|
| 1 | 06 | deviation | backend/internal/service/tracer_test.go |  | Source balances do not exist until 06-05; Valkey loss evidence currently covers implemented authority, ownership/occupation and confirmed tasks only. | open |  | 2026-11-06T19:08:40.423Z |  |

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
  }
]
````
