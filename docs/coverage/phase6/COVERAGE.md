# Phase 6 requirement ownership and evidence

All 96 Phase 6 requirements have an explicit primary plan owner below. Ownership
is not acceptance: TEN-002 still requires later domain resource ownership checks.
Through-plan order is 06-01, 06-08, 06-09, 06-02, 06-10, 06-03, 06-04, 06-05,
06-06, 06-07, 06-11. The implemented producer currently supports through 06-09;
later plans extend it with actual producers before their evidence becomes required.
Future files are neither required by 06-09 nor replaced by fixture results.

Run from a clean exact-commit clone (outputs must be outside the source tree):

```sh
./tools/phase6/check.sh --through-plan 06-09 --expected-sha "$SHA" --produce docker --evidence-dir "$EVIDENCE"
# GitHub Actions runs the same command with --produce actions and GITHUB_SHA.
./tools/phase6/check.sh --through-plan 06-09 --expected-sha "$SHA" --evidence-dir "$EVIDENCE"
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
| BE-001 | 06-01 | tracer.log | implemented scope; evidence required |
| SUP-B2-001 | 06-01 | tracer.log | implemented scope; evidence required |
| SUP-B5-001 | 06-01 | tracer.log | implemented scope; evidence required |
| NOD-001 | 06-02 | pending producer in 06-02 | pending |
| NOD-002 | 06-02 | pending producer in 06-02 | pending |
| NOD-003 | 06-02 | pending producer in 06-02 | pending |
| NOD-004 | 06-02 | pending producer in 06-02 | pending |
| NOD-005 | 06-02 | pending producer in 06-02 | pending |
| NOD-006 | 06-02 | pending producer in 06-02 | pending |
| NOD-007 | 06-02 | pending producer in 06-02 | pending |
| NOD-008 | 06-02 | pending producer in 06-02 | pending |
| NOD-009 | 06-02 | pending producer in 06-02 | pending |
| NOD-010 | 06-02 | pending producer in 06-02 | pending |
| NOD-011 | 06-02 | pending producer in 06-02 | pending |
| NOD-012 | 06-02 | pending producer in 06-02 | pending |
| NOD-013 | 06-02 | pending producer in 06-02 | pending |
| NOD-014 | 06-02 | pending producer in 06-02 | pending |
| PRT-001 | 06-02 | pending producer in 06-02 | pending |
| PRT-002 | 06-02 | pending producer in 06-02 | pending |
| PRT-003 | 06-02 | pending producer in 06-02 | pending |
| PRT-004 | 06-02 | pending producer in 06-02 | pending |
| PRT-005 | 06-02 | pending producer in 06-02 | pending |
| PRT-006 | 06-02 | pending producer in 06-02 | pending |
| PRT-007 | 06-02 | pending producer in 06-02 | pending |
| PRT-008 | 06-02 | pending producer in 06-02 | pending |
| PRT-009 | 06-02 | pending producer in 06-02 | pending |
| PRT-010 | 06-02 | pending producer in 06-02 | pending |
| PRT-016 | 06-02 | pending producer in 06-02 | pending |
| RUL-001 | 06-03 | pending producer in 06-03 | pending |
| RUL-002 | 06-03 | pending producer in 06-03 | pending |
| RUL-003 | 06-03 | pending producer in 06-03 | pending |
| RUL-004 | 06-03 | pending producer in 06-03 | pending |
| RUL-005 | 06-03 | pending producer in 06-03 | pending |
| RUL-006 | 06-03 | pending producer in 06-03 | pending |
| RUL-007 | 06-03 | pending producer in 06-03 | pending |
| RUL-008 | 06-03 | pending producer in 06-03 | pending |
| RUL-009 | 06-03 | pending producer in 06-03 | pending |
| RUL-010 | 06-03 | pending producer in 06-03 | pending |
| RUL-011 | 06-03 | pending producer in 06-03 | pending |
| RUL-012 | 06-03 | pending producer in 06-03 | pending |
| RUL-013 | 06-03 | pending producer in 06-03 | pending |
| RUL-014 | 06-03 | pending producer in 06-03 | pending |
| RUL-015 | 06-03 | pending producer in 06-03 | pending |
| RUL-016 | 06-03 | pending producer in 06-03 | pending |
| BE-002 | 06-03 | pending producer in 06-03 | pending |
| BE-003 | 06-03 | pending producer in 06-03 | pending |
| PLN-001 | 06-04 | pending producer in 06-04 | pending |
| PLN-002 | 06-04 | pending producer in 06-04 | pending |
| PLN-003 | 06-04 | pending producer in 06-04 | pending |
| PLN-004 | 06-04 | pending producer in 06-04 | pending |
| PLN-005 | 06-04 | pending producer in 06-04 | pending |
| PLN-006 | 06-04 | pending producer in 06-04 | pending |
| PLN-007 | 06-04 | pending producer in 06-04 | pending |
| PLN-008 | 06-04 | pending producer in 06-04 | pending |
| MTR-001 | 06-05 | pending producer in 06-05 | pending |
| MTR-002 | 06-05 | pending producer in 06-05 | pending |
| MTR-003 | 06-05 | pending producer in 06-05 | pending |
| MTR-004 | 06-05 | pending producer in 06-05 | pending |
| MTR-005 | 06-05 | pending producer in 06-05 | pending |
| MTR-006 | 06-05 | pending producer in 06-05 | pending |
| MTR-007 | 06-05 | pending producer in 06-05 | pending |
| MTR-008 | 06-05 | pending producer in 06-05 | pending |
| MTR-009 | 06-05 | pending producer in 06-05 | pending |
| MTR-010 | 06-05 | pending producer in 06-05 | pending |
| MTR-011 | 06-05 | pending producer in 06-05 | pending |
| MTR-013 | 06-05 | pending producer in 06-05 | pending |
| MTR-014 | 06-05 | pending producer in 06-05 | pending |
| MTR-015 | 06-05 | pending producer in 06-05 | pending |
| MTR-016 | 06-05 | pending producer in 06-05 | pending |
| LIM-001 | 06-06 | pending producer in 06-06 | pending |
| LIM-002 | 06-06 | pending producer in 06-06 | pending |
| LIM-003 | 06-06 | pending producer in 06-06 | pending |
| LIM-004 | 06-06 | pending producer in 06-06 | pending |
| LIM-005 | 06-06 | pending producer in 06-06 | pending |
| LIM-006 | 06-06 | pending producer in 06-06 | pending |
| LIM-007 | 06-06 | pending producer in 06-06 | pending |
| LIM-008 | 06-06 | pending producer in 06-06 | pending |
| LIM-009 | 06-06 | pending producer in 06-06 | pending |
| LIM-010 | 06-06 | pending producer in 06-06 | pending |
| BE-005 | 06-07 | pending producer in 06-07 | pending |
| TEN-001 | 06-08 | tracer.log | implemented scope; evidence required |
| TEN-002 | 06-08 | tracer.log | pending domain ownership coverage |
| TEN-003 | 06-08 | tracer.log | implemented scope; evidence required |
| TEN-004 | 06-08 | tracer.log | implemented scope; evidence required |
| TEN-005 | 06-08 | tracer.log | implemented scope; evidence required |
| TEN-006 | 06-08 | tracer.log | implemented scope; evidence required |
| TEN-007 | 06-08 | tracer.log | implemented scope; evidence required |
| TEN-008 | 06-08 | tracer.log | implemented scope; evidence required |
| BE-004 | 06-08 | tracer.log | implemented scope; evidence required |
| TEN-009 | 06-09 | tracer.log | implemented scope; evidence required |
| PRT-011 | 06-10 | pending producer in 06-10 | pending |
| PRT-012 | 06-10 | pending producer in 06-10 | pending |
| PRT-013 | 06-10 | pending producer in 06-10 | pending |
| PRT-014 | 06-10 | pending producer in 06-10 | pending |
| PRT-015 | 06-10 | pending producer in 06-10 | pending |
| BE-006 | 06-11 | pending producer in 06-11 | pending |

